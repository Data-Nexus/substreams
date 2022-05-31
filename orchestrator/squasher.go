package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"go.uber.org/zap"

	"github.com/streamingfast/substreams/block"
	"github.com/streamingfast/substreams/pipeline/outputs"
	"github.com/streamingfast/substreams/state"
)

// Squasher produces _complete_ stores, by merging backing partial stores.
type Squasher struct {
	squashables          map[string]*Squashable
	storeSaveInterval    uint64
	targetExclusiveBlock uint64

	notifier Notifier

	lock sync.Mutex
}

type SquasherOption func(s *Squasher)

func WithNotifier(notifier Notifier) SquasherOption {
	return func(s *Squasher) {
		s.notifier = notifier
	}
}

func NewSquasher(ctx context.Context, builders []*state.Store, outputCaches map[string]*outputs.OutputCache, storeSaveInterval uint64, targetExclusiveBlock uint64, opts ...SquasherOption) (*Squasher, error) {
	squashables := map[string]*Squashable{}
	for _, builder := range builders {
		info, err := builder.Info(ctx)
		if err != nil {
			return nil, fmt.Errorf("getting info for %s: %w", builder.Name, err)
		}

		storagePresent := info.LastKVSavedBlock != 0
		if !storagePresent {
			squashables[builder.Name] = NewSquashable(builder.Clone(builder.ModuleInitialBlock), targetExclusiveBlock, storeSaveInterval, builder.ModuleInitialBlock)
		} else {
			r := &block.Range{
				StartBlock:        builder.ModuleInitialBlock,
				ExclusiveEndBlock: info.LastKVSavedBlock, // This ASSUMES we have scheduled jobs that are going to pipe us new results in.
			}
			squish, err := builder.LoadFrom(ctx, r)
			if err != nil {
				return nil, fmt.Errorf("loading store %q: range %s: %w", builder.Name, r, err)
			}
			squashables[builder.Name] = NewSquashable(squish, targetExclusiveBlock, storeSaveInterval, info.LastKVSavedBlock)
		}
	}

	squasher := &Squasher{
		squashables:          squashables,
		storeSaveInterval:    storeSaveInterval,
		targetExclusiveBlock: targetExclusiveBlock,
	}

	for _, opt := range opts {
		opt(squasher)
	}

	return squasher, nil
}

func (s *Squasher) Squash(ctx context.Context, moduleName string, outgoingReqRange *block.Range) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	squashable, ok := s.squashables[moduleName]
	if !ok {
		return fmt.Errorf("module %q was not found in squashables module registry", moduleName)
	}

	return squashable.squash(ctx, outgoingReqRange, s.notifier)
}

func (s *Squasher) StoresReady() error {
	var errs []string
	for _, v := range s.squashables {
		if !v.targetReached {
			errs = append(errs, fmt.Sprintf("module %q target not reached", v.name))
		}
		if !v.IsEmpty() {
			errs = append(errs, fmt.Sprintf("module %q missing ranges %s", v.name, v.ranges))
		}
	}
	if len(errs) != 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

type Squashable struct {
	name                   string
	builder                *state.Store
	ranges                 block.Ranges // Ranges split in `storeSaveInterval` chunks
	storeSaveInterval      uint64
	targetExclusiveBlock   uint64
	nextExpectedStartBlock uint64

	targetReached bool
}

func NewSquashable(initialBuilder *state.Store, targetExclusiveBlock, storeSaveInterval, nextExpectedStartBlock uint64) *Squashable {
	return &Squashable{
		name:                   initialBuilder.Name,
		builder:                initialBuilder,
		storeSaveInterval:      storeSaveInterval,
		targetExclusiveBlock:   targetExclusiveBlock,
		nextExpectedStartBlock: nextExpectedStartBlock,
	}
}

func (s *Squashable) squash(ctx context.Context, blockRange *block.Range, notifier Notifier) error {
	splitBlockRanges := blockRange.Split(s.storeSaveInterval)
	for _, br := range splitBlockRanges {
		if br.StartBlock < s.builder.ModuleInitialBlock {
			return fmt.Errorf("module %q: received a squash request for a start block %d prior to the module's initial block %d", s.name, br.StartBlock, s.builder.ModuleInitialBlock)
		}
		s.cumulateBoundedRange(br)
	}

	zlog.Info("squashing request range", zap.String("module", s.name), zap.Object("request_range", blockRange))

	for _, br := range splitBlockRanges {
		zlog.Info("squashing range", zap.String("module", s.name), zap.Object("range", br))
		err := s.mergeAvailablePartials(ctx, notifier)
		if err != nil {
			return fmt.Errorf("squashing range %d-%d: %w", br.StartBlock, br.ExclusiveEndBlock, err)
		}
	}

	return nil
}

func (s *Squashable) cumulateBoundedRange(blockRange *block.Range) {
	if blockRange.ExclusiveEndBlock <= s.builder.StoreInitialBlock {
		// Otherwise, risks stalling the merging (as ranges are
		// sorted, and only the first is checked for contiguousness)
		return
	}
	s.ranges = append(s.ranges, blockRange)
	sort.Sort(s.ranges)
}

func (s *Squashable) mergeAvailablePartials(ctx context.Context, notifier Notifier) error {
	zlog.Info("squashing", zap.String("module_name", s.builder.Name))

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if len(s.ranges) == 0 {
			break
		}

		squashableRange := s.ranges[0]

		if s.nextExpectedStartBlock != squashableRange.StartBlock {
			break
		}

		zlog.Debug("found range to merge", zap.Stringer("squashable", s))

		nextStore, err := s.builder.LoadFrom(ctx, squashableRange)
		if err != nil {
			return fmt.Errorf("initializing next partial builder %q: %w", s.name, err)
		}

		err = s.builder.Merge(nextStore)
		if err != nil {
			return fmt.Errorf("merging: %s", err)
		}

		s.nextExpectedStartBlock = squashableRange.ExclusiveEndBlock

		endsOnBoundary := squashableRange.ExclusiveEndBlock%s.storeSaveInterval == 0
		if endsOnBoundary {
			err = s.builder.WriteState(ctx, squashableRange.ExclusiveEndBlock)
			if err != nil {
				return fmt.Errorf("writing state: %w", err)
			}
		} else {
			err = nextStore.DeletePartialFile(ctx, squashableRange.ExclusiveEndBlock)
			if err != nil {
				zlog.Warn("deleting partial file", zap.Error(err))
			}
		}

		s.ranges = s.ranges[1:]

		if squashableRange.ExclusiveEndBlock == s.targetExclusiveBlock {
			s.targetReached = true
			if notifier != nil {
				notifier.Notify(s.builder.Name, squashableRange.ExclusiveEndBlock)
			}
		}
	}

	return nil
}

func (s *Squashable) IsEmpty() bool {
	return len(s.ranges) == 0
}

func (s *Squashable) String() string {
	var add string
	if s.targetReached {
		add = " (target reached)"
	}
	return fmt.Sprintf("%s%s: [%s]", s.name, add, s.ranges)
}

type Squashables []*Squashable

func (s Squashables) String() string {
	var rs []string
	for _, i := range s {
		rs = append(rs, i.String())
	}
	return strings.Join(rs, ", ")
}
