package block

import "github.com/streamingfast/substreams/utils"

// TODO(abourget): The Segmenter is a new SegmentedRange system, that takes an index so
// the caller can always keep track of just one number, and we can obtain the corresponding
// Range for the segment. We can obtain info on the Segment too (if it's Partial, Complete, etc..)

type Segmenter struct {
	interval          uint64
	initialBlock      uint64
	exclusiveEndBlock uint64

	count int
}

func NewSegmenter(interval uint64, initialBlock uint64, exclusiveEndBlock uint64) *Segmenter {
	s := &Segmenter{
		interval:          interval,
		initialBlock:      initialBlock,
		exclusiveEndBlock: exclusiveEndBlock,
	}
	s.count = s.computeCount()
	return s
}

func (s *Segmenter) Count() int { return s.count }

func (s *Segmenter) computeCount() int {
	initSegment := s.initialBlock / s.interval
	lastSegment := s.exclusiveEndBlock / s.interval
	return int(lastSegment - initSegment + 1)
}

func (s *Segmenter) Range(idx int) *Range {
	if idx < 0 {
		return nil
	}
	if idx == 0 {
		return s.firstRange()
	}
	return s.rangeFromBegin(idx)
}

func (s *Segmenter) firstRange() *Range {
	if s.exclusiveEndBlock < s.initialBlock {
		return nil
	}
	floorLowerBound := s.initialBlock - s.initialBlock%s.interval
	upperBound := floorLowerBound + s.interval
	return NewRange(s.initialBlock, utils.MinOf(upperBound, s.exclusiveEndBlock))
}

func (s *Segmenter) rangeFromBegin(idx int) *Range {
	if idx >= s.count {
		return nil
	}
	baseBlock := s.initialBlock - s.initialBlock%s.interval
	baseBlock += uint64(idx) * s.interval
	upperBound := baseBlock + s.interval
	return NewRange(baseBlock, utils.MinOf(upperBound, s.exclusiveEndBlock))
}

func (s *Segmenter) IndexForBlock(blockNum uint64) int {
	blockSegment := blockNum / s.interval
	initSegment := s.initialBlock / s.interval
	return int(blockSegment - initSegment)
}

func (s *Segmenter) IsPartial(segmentIndex int) bool {
	if segmentIndex >= s.count {
		panic("segment index out of range")
	}
	return s.Range(segmentIndex).ExclusiveEndBlock%s.interval != 0
}