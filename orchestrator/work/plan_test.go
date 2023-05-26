package work

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/streamingfast/substreams/block"
	"github.com/streamingfast/substreams/manifest"
	pbsubstreamsrpc "github.com/streamingfast/substreams/pb/sf/substreams/rpc/v2"
	pbsubstreams "github.com/streamingfast/substreams/pb/sf/substreams/v1"
	"github.com/streamingfast/substreams/pipeline/outputmodules"
	"github.com/streamingfast/substreams/storage"
	"github.com/streamingfast/substreams/storage/store"
	"github.com/streamingfast/substreams/storage/store/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestWorkPlanning(t *testing.T) {
	tests := []struct {
		name           string
		upToBlock      uint64
		subreqSplit    int
		state          storage.ModuleStorageStateMap
		outMod         string
		productionMode bool

		expectWaitingJobs []*Job
		expectReadyJobs   []*Job
	}{
		{
			name:        "single",
			upToBlock:   85,
			subreqSplit: 20,
			state: TestModStateMap(
				TestStoreStatePartialsMissing("B", "0-10"),
			),
			productionMode: false,
			outMod:         "B",
			expectReadyJobs: []*Job{
				TestJob("B", "0-10", 5),
			},
		},
		{
			name:        "double",
			upToBlock:   85,
			subreqSplit: 20,
			state: TestModStateMap(
				TestStoreStatePartialsMissing("As", "0-10,10-20,30-40,40-50,50-60"),
				TestStoreStatePartialsMissing("B", "0-10"),
			),
			productionMode: false,
			outMod:         "As",
			expectReadyJobs: []*Job{
				TestJob("As", "0-20", 5),
				TestJob("As", "30-50", 4),
				TestJob("As", "50-60", 3),
			},
		},
		{
			name:        "missing 1 full kv",
			upToBlock:   500,
			subreqSplit: 100,
			state: TestModStateMap(
				TestStoreStateCompleteRangesAndMissingRanges(
					"As",
					store.NewCompleteFileInfo(0, 500),
					"0-200", // missing ranges
				),
			),
			productionMode: false,
			outMod:         "As",
			expectReadyJobs: []*Job{
				TestJob("As", "0-200", 6),
			},
		},
		{
			name:        "missing 1 full kv with single partial",
			upToBlock:   500,
			subreqSplit: 100,
			state: TestModStateMap(
				TestStoreStateCompleteRangesMissingRangesPartialsMissing(
					"As",
					store.NewCompleteFileInfo(0, 500),
					"0-200", // missing ranges
					"200-250,250-300",
				),
			),
			productionMode: false,
			outMod:         "As",
			expectReadyJobs: []*Job{
				TestJob("As", "0-200", 6),
				TestJob("As", "200-300", 4),
			},
		},
		{
			name:        "missing 1 full kv with double partial",
			upToBlock:   700,
			subreqSplit: 100,
			state: TestModStateMap(
				TestStoreStateCompleteRangesMissingRangesPartialsMissing(
					"As",
					store.NewCompleteFileInfo(0, 700),
					"0-200", // missing ranges
					"200-250,250-300,400-450,450-500",
				),
			),
			productionMode: false,
			outMod:         "As",
			expectReadyJobs: []*Job{
				TestJob("As", "0-200", 8),
				TestJob("As", "200-300", 6),
				TestJob("As", "400-500", 4),
			},
		},
		{
			name:        "missing 2 full kv",
			upToBlock:   500,
			subreqSplit: 100,
			state: TestModStateMap(
				TestStoreStateCompleteRangesAndMissingRanges(
					"As",
					store.NewCompleteFileInfo(0, 500),
					"0-200,0-300", // missing ranges
				),
			),
			productionMode: false,
			outMod:         "As",
			expectReadyJobs: []*Job{
				TestJob("As", "0-200", 6),
				TestJob("As", "0-300", 6),
			},
		},
		{
			name:        "missing 2 full kv with single partial",
			upToBlock:   500,
			subreqSplit: 100,
			state: TestModStateMap(
				TestStoreStateCompleteRangesMissingRangesPartialsMissing(
					"As",
					store.NewCompleteFileInfo(0, 500),
					"0-200,0-300", // missing ranges
					"200-250,250-300",
				),
			),
			productionMode: false,
			outMod:         "As",
			expectReadyJobs: []*Job{
				TestJob("As", "0-200", 6),
				TestJob("As", "0-300", 6),
				TestJob("As", "200-300", 4),
			},
		},
		{
			name:        "missing 2 full kv with double partial",
			upToBlock:   700,
			subreqSplit: 100,
			state: TestModStateMap(
				TestStoreStateCompleteRangesMissingRangesPartialsMissing(
					"As",
					store.NewCompleteFileInfo(0, 700),
					"0-200,0-300", // missing ranges
					"200-250,250-300,400-450,450-500",
				),
			),
			productionMode: false,
			outMod:         "As",
			expectReadyJobs: []*Job{
				TestJob("As", "0-200", 8),
				TestJob("As", "0-300", 8),
				TestJob("As", "200-300", 6),
				TestJob("As", "400-500", 4),
			},
		},
		{
			name:        "missing 2 full kv at different intervals",
			upToBlock:   500,
			subreqSplit: 100,
			state: TestModStateMap(
				TestStoreStateCompleteRangesAndMissingRanges(
					"As",
					store.NewCompleteFileInfo(0, 500),
					"0-200,0-400", // missing ranges
				),
			),
			productionMode: false,
			outMod:         "As",
			expectReadyJobs: []*Job{
				TestJob("As", "0-200", 6),
				TestJob("As", "0-400", 6),
			},
		},
		{
			name:        "missing 2 full kv at different intervals with single partial",
			upToBlock:   500,
			subreqSplit: 100,
			state: TestModStateMap(
				TestStoreStateCompleteRangesMissingRangesPartialsMissing(
					"As",
					store.NewCompleteFileInfo(0, 500),
					"0-200,0-400", // missing ranges
					"200-250,250-300",
				),
			),
			productionMode: false,
			outMod:         "As",
			expectReadyJobs: []*Job{
				TestJob("As", "0-200", 6),
				TestJob("As", "0-400", 6),
				TestJob("As", "200-300", 4),
			},
		},
		{
			name:        "missing 2 full kv at different intervals with double partial",
			upToBlock:   700,
			subreqSplit: 100,
			state: TestModStateMap(
				TestStoreStateCompleteRangesMissingRangesPartialsMissing(
					"As",
					store.NewCompleteFileInfo(0, 700),
					"0-200,0-400", // missing ranges
					"200-250,250-300,500-550,550-600",
				),
			),
			productionMode: false,
			outMod:         "As",
			expectReadyJobs: []*Job{
				TestJob("As", "0-200", 8),
				TestJob("As", "0-400", 8),
				TestJob("As", "200-300", 6),
				TestJob("As", "500-600", 3),
			},
		},
		{
			name:        "missing multiple full kvs at different intervals",
			upToBlock:   1000,
			subreqSplit: 100,
			state: TestModStateMap(
				TestStoreStateCompleteRangesAndMissingRanges(
					"As",
					store.NewCompleteFileInfo(0, 900),
					"0-200,0-300,0-800,0-1000", // missing ranges
				),
			),
			productionMode: false,
			outMod:         "As",
			expectReadyJobs: []*Job{
				TestJob("As", "0-200", 11),
				TestJob("As", "0-300", 11),
				TestJob("As", "0-800", 11),
				TestJob("As", "0-1000", 11),
			},
		},
		{
			name:        "missing multiple full kvs at different intervals with single partial",
			upToBlock:   1000,
			subreqSplit: 100,
			state: TestModStateMap(
				TestStoreStateCompleteRangesMissingRangesPartialsMissing(
					"As",
					store.NewCompleteFileInfo(0, 900),
					"0-200,0-300,0-800,0-1000", // missing ranges
					"500-550,550-600",
				),
			),
			productionMode: false,
			outMod:         "As",
			expectReadyJobs: []*Job{
				TestJob("As", "0-200", 11),
				TestJob("As", "0-300", 11),
				TestJob("As", "0-800", 11),
				TestJob("As", "0-1000", 11),
				TestJob("As", "500-600", 6),
			},
		},
		{
			name:        "missing 2 full kv at different intervals with double partial",
			upToBlock:   1000,
			subreqSplit: 100,
			state: TestModStateMap(
				TestStoreStateCompleteRangesMissingRangesPartialsMissing(
					"As",
					store.NewCompleteFileInfo(0, 900),
					"0-200,0-300,0-800,0-1000", // missing ranges
					"100-150,150-200,500-550,550-600",
				),
			),
			productionMode: false,
			outMod:         "As",
			expectReadyJobs: []*Job{
				TestJob("As", "0-200", 11),
				TestJob("As", "0-300", 11),
				TestJob("As", "0-800", 11),
				TestJob("As", "0-1000", 11),
				TestJob("As", "100-200", 10),
				TestJob("As", "500-600", 6),
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mods := manifest.NewTestModules()
			outputGraph, err := outputmodules.NewOutputModuleGraph(test.outMod, test.productionMode, &pbsubstreams.Modules{Modules: mods, Binaries: []*pbsubstreams.Binary{{}}})
			require.NoError(t, err)

			plan, err := BuildNewPlan(context.Background(), test.state, uint64(test.subreqSplit), test.upToBlock, 0, outputGraph)
			require.NoError(t, err)

			assert.Equal(t, jobList(test.expectWaitingJobs), jobList(plan.waitingJobs), "waiting jobs") // these are not sorted by the engine
			assert.Equal(t, jobList(test.expectReadyJobs), jobList(plan.readyJobs), "ready jobs")
		})
	}
}

func jobList(jobs []*Job) string {
	var out []string
	for _, job := range jobs {
		out = append(out, job.String())
	}
	return strings.Join(out, "\n")
}

func TestPlan_MarkDependencyComplete(t *testing.T) {
	type fields struct {
		ModulesStateMap       storage.ModuleStorageStateMap
		upToBlock             uint64
		waitingJobs           []*Job
		readyJobs             []*Job
		modulesReadyUpToBlock map[string]uint64
	}
	type args struct {
		modName   string
		upToBlock uint64
	}
	tests := []struct {
		name   string
		fields fields
		args   args
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Plan{
				ModulesStateMap:       tt.fields.ModulesStateMap,
				upToBlock:             tt.fields.upToBlock,
				waitingJobs:           tt.fields.waitingJobs,
				readyJobs:             tt.fields.readyJobs,
				modulesReadyUpToBlock: tt.fields.modulesReadyUpToBlock,
			}
			p.MarkDependencyComplete(tt.args.modName, tt.args.upToBlock)
		})
	}
}

func TestPlan_NextJob(t *testing.T) {
	mkJob := func(el string) (out *Job) {
		return &Job{ModuleName: el, RequestRange: block.NewRange(0, 100)}
	}
	mkJobs := func(rng string) (out []*Job) {
		for _, el := range strings.Split(rng, ",") {
			if el != "" {
				out = append(out, mkJob(el))
			}
		}
		return
	}

	tests := []struct {
		name                  string
		waitingJobs           []*Job
		readyJobs             []*Job
		modulesReadyUpToBlock map[string]uint64
		expectJob             *Job
		expectMore            bool
	}{
		{
			name:                  "one ready,one waiting",
			waitingJobs:           mkJobs("As"),
			readyJobs:             mkJobs("B"),
			modulesReadyUpToBlock: map[string]uint64{"As": 0, "B": 0},
			expectJob:             mkJob("B"),
			expectMore:            true,
		},
		{
			name:                  "none ready,one waiting",
			waitingJobs:           mkJobs("As"),
			readyJobs:             mkJobs(""),
			modulesReadyUpToBlock: map[string]uint64{"As": 0},
			expectJob:             nil,
			expectMore:            true,
		},
		{
			name:                  "one ready,none waiting",
			waitingJobs:           mkJobs(""),
			readyJobs:             mkJobs("As"),
			modulesReadyUpToBlock: map[string]uint64{"As": 0},
			expectJob:             mkJob("As"),
			expectMore:            false,
		},
		{
			name:                  "none ready,none waiting",
			waitingJobs:           mkJobs(""),
			readyJobs:             mkJobs(""),
			modulesReadyUpToBlock: map[string]uint64{},
			expectJob:             nil,
			expectMore:            false,
		},
		{
			name:                  "two ready,none waiting",
			waitingJobs:           mkJobs(""),
			readyJobs:             mkJobs("As,B,C"),
			modulesReadyUpToBlock: map[string]uint64{"As": 0, "B": 0, "C": 0},
			expectJob:             mkJob("As"),
			expectMore:            true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			p := &Plan{
				waitingJobs:               test.waitingJobs,
				readyJobs:                 test.readyJobs,
				highestModuleRunningBlock: test.modulesReadyUpToBlock,
				modulesReadyUpToBlock:     test.modulesReadyUpToBlock,
				logger:                    zap.NewNop(),
			}

			gotJob, gotMore := p.NextJob()

			assert.Equalf(t, test.expectJob, gotJob, "NextJob()")
			assert.Equalf(t, test.expectMore, gotMore, "NextJob()")
		})
	}
}

func TestPlan_allDependenciesMet(t *testing.T) {
	type fields struct {
		modulesReadyUpToBlock map[string]uint64
	}
	type args struct {
		job *Job
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   bool
	}{
		{
			name: "all deps met",
			fields: fields{
				modulesReadyUpToBlock: map[string]uint64{
					"foo": 100,
					"bar": 50,
				},
			},
			args: args{
				job: &Job{
					ModuleName: "foo",
					RequestRange: &block.Range{
						StartBlock:        10,
						ExclusiveEndBlock: 20,
					},
					requiredModules: []string{
						"foo",
						"bar",
					},
				},
			},
			want: true,
		},
		{
			name: "some deps met",
			fields: fields{
				modulesReadyUpToBlock: map[string]uint64{
					"foo": 100,
					"bar": 0,
				},
			},
			args: args{
				job: &Job{
					ModuleName: "foo",
					RequestRange: &block.Range{
						StartBlock:        10,
						ExclusiveEndBlock: 20,
					},
					requiredModules: []string{
						"foo",
						"bar",
					},
				},
			},
			want: false,
		},
		{
			name: "no deps met",
			fields: fields{
				modulesReadyUpToBlock: map[string]uint64{
					"foo": 0,
					"bar": 0,
				},
			},
			args: args{
				job: &Job{
					ModuleName: "foo",
					RequestRange: &block.Range{
						StartBlock:        10,
						ExclusiveEndBlock: 20,
					},
					requiredModules: []string{
						"foo",
						"bar",
					},
				},
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Plan{
				modulesReadyUpToBlock: tt.fields.modulesReadyUpToBlock,
			}
			assert.Equalf(t, tt.want, p.allDependenciesMet(tt.args.job), "allDependenciesMet(%v)", tt.args.job)
		})
	}
}

func TestPlan_prioritize(t *testing.T) {
	tests := []struct {
		name      string
		readyJobs []*Job
		expected  []*Job
	}{
		{
			name:      "no jobs",
			readyJobs: []*Job{},
			expected:  []*Job{},
		},
		{
			name:      "one job",
			readyJobs: []*Job{{ModuleName: "A", priority: 1}},
			expected:  []*Job{{ModuleName: "A", priority: 1}},
		},
		{
			name:      "sorted highest priority to lowest",
			readyJobs: []*Job{{ModuleName: "B", priority: 2}, {ModuleName: "C", priority: 1}, {ModuleName: "A", priority: 3}},
			expected:  []*Job{{ModuleName: "A", priority: 3}, {ModuleName: "B", priority: 2}, {ModuleName: "C", priority: 1}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Plan{
				readyJobs: tt.readyJobs,
			}
			p.prioritize()
			assert.Equalf(t, tt.expected, p.readyJobs, "prioritize()")
		})
	}
}

func TestPlan_initModulesReadyUpToBlock(t *testing.T) {
	type fields struct {
		ModulesStateMap       storage.ModuleStorageStateMap
		modulesReadyUpToBlock map[string]uint64
	}
	tests := []struct {
		name     string
		fields   fields
		expected map[string]uint64
	}{
		{
			name: "no modules",
			fields: fields{
				ModulesStateMap: storage.ModuleStorageStateMap{},
			},
			expected: map[string]uint64{},
		},
		{
			name: "one module,initial block",
			fields: fields{
				ModulesStateMap: storage.ModuleStorageStateMap{
					"A": &state.StoreStorageState{
						ModuleInitialBlock: 1,
					},
				},
			},
			expected: map[string]uint64{
				"A": 1,
			},
		},
		{
			name: "one module,complete range",
			fields: fields{
				ModulesStateMap: storage.ModuleStorageStateMap{
					"A": &state.StoreStorageState{
						InitialCompleteFile: store.CompleteFile("1-20"),
					},
				},
			},
			expected: map[string]uint64{
				"A": 20,
			},
		},
		{
			name: "mixed modules",
			fields: fields{
				ModulesStateMap: storage.ModuleStorageStateMap{
					"A": &state.StoreStorageState{
						InitialCompleteFile: store.CompleteFile("1-20"),
					},
					"B": &state.StoreStorageState{
						ModuleInitialBlock: 1,
					},
				},
			},
			expected: map[string]uint64{
				"A": 20,
				"B": 1,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Plan{
				ModulesStateMap:       tt.fields.ModulesStateMap,
				modulesReadyUpToBlock: tt.fields.modulesReadyUpToBlock,
			}
			p.initModulesReadyUpToBlock()
			assert.Equalf(t, tt.expected, p.modulesReadyUpToBlock, "initModulesReadyUpToBlock()")
		})
	}
}

func TestPlan_bumpModuleUpToBlock(t *testing.T) {
	type fields struct {
		modulesReadyUpToBlock map[string]uint64
	}
	type args struct {
		modName   string
		upToBlock uint64
	}
	tests := []struct {
		name          string
		initialFields fields
		expected      map[string]uint64
		args          args
	}{
		{
			name: "bump up to block",
			initialFields: fields{
				modulesReadyUpToBlock: map[string]uint64{
					"A": 10,
				},
			},
			args: args{
				modName:   "A",
				upToBlock: 20,
			},
			expected: map[string]uint64{
				"A": 20,
			},
		},
		{
			name: "bump up to block, no existing",
			initialFields: fields{
				modulesReadyUpToBlock: map[string]uint64{},
			},
			args: args{
				modName:   "A",
				upToBlock: 20,
			},
			expected: map[string]uint64{
				"A": 20,
			},
		},
		{
			name: "bump less than current",
			initialFields: fields{
				modulesReadyUpToBlock: map[string]uint64{
					"A": 20,
				},
			},
			args: args{
				modName:   "A",
				upToBlock: 10,
			},
			expected: map[string]uint64{
				"A": 20,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Plan{
				modulesReadyUpToBlock: tt.initialFields.modulesReadyUpToBlock,
			}
			p.bumpModuleUpToBlock(tt.args.modName, tt.args.upToBlock)
			assert.Equal(t, tt.expected, p.modulesReadyUpToBlock)
		})
	}
}

func TestPlan_promoteWaitingJobs(t *testing.T) {
	type fields struct {
		waitingJobs           []*Job
		readyJobs             []*Job
		modulesReadyUpToBlock map[string]uint64
	}
	tests := []struct {
		name                   string
		fields                 fields
		expectedReadyJobsLen   int
		expectedWaitingJobsLen int
	}{
		{
			name: "one job,ready",
			fields: fields{
				modulesReadyUpToBlock: map[string]uint64{
					"B": 10,
				},
				waitingJobs: []*Job{
					{
						ModuleName: "A",
						RequestRange: &block.Range{
							StartBlock:        10,
							ExclusiveEndBlock: 20,
						},
						requiredModules: []string{"B"},
					},
				},
			},
			expectedReadyJobsLen:   1,
			expectedWaitingJobsLen: 0,
		},
		{
			name: "two jobs,one ready",
			fields: fields{
				modulesReadyUpToBlock: map[string]uint64{
					"B": 10,
					"C": 0,
				},
				waitingJobs: []*Job{
					{
						ModuleName: "A",
						RequestRange: &block.Range{
							StartBlock:        10,
							ExclusiveEndBlock: 20,
						},
						requiredModules: []string{"B"},
					},
					{
						ModuleName: "B",
						RequestRange: &block.Range{
							StartBlock:        10,
							ExclusiveEndBlock: 20,
						},
						requiredModules: []string{"C"},
					},
				},
			},
			expectedReadyJobsLen:   1,
			expectedWaitingJobsLen: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Plan{
				waitingJobs:           tt.fields.waitingJobs,
				readyJobs:             tt.fields.readyJobs,
				modulesReadyUpToBlock: tt.fields.modulesReadyUpToBlock,
			}
			p.promoteWaitingJobs()
			assert.Equal(t, tt.expectedReadyJobsLen, len(p.readyJobs))
			assert.Equal(t, tt.expectedWaitingJobsLen, len(p.waitingJobs))
		})
	}
}

func TestPlan_splitWorkIntoJobs(t *testing.T) {
	t.Skip("not implemented")
	type fields struct {
		ModulesStateMap       storage.ModuleStorageStateMap
		upToBlock             uint64
		waitingJobs           []*Job
		readyJobs             []*Job
		modulesReadyUpToBlock map[string]uint64
		mu                    sync.Mutex
	}
	type args struct {
		subrequestSplitSize uint64
		graph               *manifest.ModuleGraph
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		wantErr assert.ErrorAssertionFunc
	}{
		{
			name: "a to b to c",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Plan{
				ModulesStateMap:       tt.fields.ModulesStateMap,
				upToBlock:             tt.fields.upToBlock,
				waitingJobs:           tt.fields.waitingJobs,
				readyJobs:             tt.fields.readyJobs,
				modulesReadyUpToBlock: tt.fields.modulesReadyUpToBlock,
				mu:                    tt.fields.mu,
			}
			//tt.wantErr(t, p.splitWorkIntoJobs(tt.args.subrequestSplitSize), fmt.Sprintf("splitWorkIntoJobs(%v, %v)", tt.args.subrequestSplitSize, tt.args.graph))
			_ = p
		})
	}
}

func TestPlan_initialProgressMessages(t *testing.T) {
	tests := []struct {
		name             string
		modState         storage.ModuleStorageState
		expectedProgress string
	}{
		{
			modState: &state.StoreStorageState{
				ModuleName:          "A",
				InitialCompleteFile: store.CompleteFile("1-10"),
			},
			expectedProgress: "A:r1-10",
		},
		{
			modState: &state.StoreStorageState{
				ModuleName:          "A",
				InitialCompleteFile: store.CompleteFile("1-10"),
			},
			expectedProgress: "A:r1-10",
		},
		{
			modState: &state.StoreStorageState{
				ModuleName: "A",
			},
			expectedProgress: "",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			p := &Plan{ModulesStateMap: TestModStateMap(test.modState)}

			out := p.initialProgressMessages()

			assert.Equal(t, test.expectedProgress, reduceProgressMessages(out))
		})
	}
}

func TestAncestorsDepth(t *testing.T) {
	tests := []struct {
		name               string
		ancestorsMap       map[string][]string
		outputModule       string
		expectDepth        int
		expectHighestDepth int
	}{
		{
			"simple",
			map[string][]string{
				"A": {},
				"B": {"A"},
			},
			"B",
			2,
			2,
		},
		{
			"simple_mod_A",
			map[string][]string{
				"A": {},
				"B": {"A"},
			},
			"A",
			1,
			2,
		},
		{
			"3-deep",
			map[string][]string{
				"A": {},
				"B": {"A"},
				"C": {"A", "B"},
			},
			"C",
			3,
			3,
		},
		{
			"3-deep-very-wide",
			map[string][]string{
				"A": {},
				"B": {"A"},
				"C": {"A", "B"},
				"D": {"A", "B"},
				"E": {"A", "B"},
				"F": {"A", "B", "C", "D", "E"},
			},
			"F",
			4,
			4,
		},
		{
			"3-deep-very-wide-request-simple",
			map[string][]string{
				"A": {},
				"B": {"A"},
				"C": {"A", "B"},
				"D": {"A", "B"},
				"E": {"A", "B"},
				"F": {"A", "B", "C", "D", "E"},
			},
			"B",
			2,
			4,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f := func(in string) []string {
				return test.ancestorsMap[in]
			}

			depth := ancestorsDepth(test.outputModule, f)
			assert.Equal(t, test.expectDepth, depth)

			var modulesList []string
			stateMap := make(map[string]storage.ModuleStorageState)
			for k := range test.ancestorsMap {
				modulesList = append(modulesList, k)
				stateMap[k] = &state.StoreStorageState{}
			}
			highestDepth := calculateHighestDependencyDepth(
				modulesList,
				stateMap,
				f,
			)
			assert.Equal(t, test.expectHighestDepth, highestDepth)
		})
	}
}

func reduceProgressMessages(in []*pbsubstreamsrpc.ModuleProgress) string {
	var out []string
	for _, prog := range in {
		entry := fmt.Sprintf("%s:", prog.Name)
		switch t := prog.Type.(type) {
		case *pbsubstreamsrpc.ModuleProgress_ProcessedRanges_:
			var rngs []string
			for _, rng := range t.ProcessedRanges.ProcessedRanges {
				rngs = append(rngs, fmt.Sprintf("%d-%d", rng.StartBlock, rng.EndBlock))
			}
			entry += "r" + strings.Join(rngs, ",")
		case *pbsubstreamsrpc.ModuleProgress_InitialState_:
			entry += "up" + fmt.Sprintf("%d", t.InitialState.AvailableUpToBlock)
		default:
			panic("unsupported here")
		}
		out = append(out, entry)
	}
	return strings.Join(out, ";")
}
