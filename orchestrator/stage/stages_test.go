package stage

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/streamingfast/substreams/block"
	"github.com/streamingfast/substreams/pipeline/outputmodules"
)

func TestStages(t *testing.T) {
	s := &Stages{
		stages: []*Stage{
			&Stage{kind: KindStore},
			&Stage{kind: KindStore},
			&Stage{kind: KindMap},
		},
		Segmenter: block.NewSegmenter(10, 5, 35),
	}

	assert.Equal(t, true, s.dependenciesCompleted(0, 1))
	segID := s.NextJob()
	require.NotNil(t, segID)
	assert.Equal(t, 1, segID.Stage)
	assert.Equal(t, 2, segID.Segment)
	assert.Equal(t, block.ParseRange("10-20"), segID.Range)
}

func TestNewStages(t *testing.T) {
	stages := NewStages(outputmodules.TestGraphStagedModules(5, 7, 12, 22, 25), 10, 75)
	assert.Equal(t, 8, stages.Count()) // from 5 to 75
	assert.Equal(t, true, stages.IsPartial(7))
	assert.Equal(t, 6, stages.IndexForBlock(60))
	assert.Equal(t, 6, stages.IndexForBlock(60))
	assert.Panics(t, func() { stages.IndexForBlock(80) })
	assert.Equal(t, block.ParseRange("5-10"), stages.Range(0))
	assert.Equal(t, block.ParseRange("10-20"), stages.Range(1))
	assert.Equal(t, block.ParseRange("70-75"), stages.Range(7))
	assert.Panics(t, func() { stages.Range(8) })
	assert.Equal(t, 0, stages.completedSegments)
}

func TestNewStagesNextJobs(t *testing.T) {
	stages := NewStages(outputmodules.TestGraphStagedModules(5, 5, 5, 5, 5), 10, 50)

	j1 := stages.NextJob()
	assert.Equal(t, 2, j1.Stage)
	assert.Equal(t, 0, j1.Segment)
	assert.Equal(t, block.ParseRange("5-10"), j1.Range)

	segmentStateEquals(t, stages, `
..
..
S.`)

	stages.forceTransition(0, 2, SegmentCompleted)
	stages.NextJob()

	segmentStateEquals(t, stages, `
..
S.
C.`)

	stages.forceTransition(0, 1, SegmentCompleted)

	segmentStateEquals(t, stages, `
..
C.
C.`)

	stages.NextJob()

	segmentStateEquals(t, stages, `
S.
C.
C.`)

	stages.NextJob()

	segmentStateEquals(t, stages, `
SS
C.
C.`)

	stages.forceTransition(0, 0, SegmentCompleted)
	stages.NextJob()

	segmentStateEquals(t, stages, `
CS
C.
CS`)

	stages.forceTransition(1, 0, SegmentCompleted)
	stages.NextJob()

	segmentStateEquals(t, stages, `
CC
CS
CS`)

	stages.NextJob()

	segmentStateEquals(t, stages, `
CC..
CSS.
CS..`)

	stages.MarkSegmentPartialPresent(1, 2)

	segmentStateEquals(t, stages, `
CC..
CSS.
CP..`)

	stages.MarkSegmentMerging(1, 2)

	segmentStateEquals(t, stages, `
CC..
CSS.
CM..`)

	stages.MarkSegmentCompleted(1, 2)
	stages.NextJob()

	segmentStateEquals(t, stages, `
CCS.
CSS.
CC..`)

	stages.NextJob()

	segmentStateEquals(t, stages, `
CCSS
CSS.
CC..`)

	stages.NextJob()

	segmentStateEquals(t, stages, `
CCSSS...
CSS.....
CC......`)

	stages.NextJob()

	segmentStateEquals(t, stages, `
CCSSSS..
CSS.....
CC......`)

	assert.Nil(t, stages.NextJob())
	stages.MarkSegmentPartialPresent(2, 0)

	segmentStateEquals(t, stages, `
CCPSSS..
CSS.....
CC......`)

	assert.Nil(t, stages.NextJob())
	stages.MarkSegmentMerging(2, 0)

	segmentStateEquals(t, stages, `
CCMSSS..
CSS.....
CC......`)

	assert.Nil(t, stages.NextJob())
	stages.MarkSegmentCompleted(2, 0)

	segmentStateEquals(t, stages, `
CCCSSS..
CSS.....
CC......`)

	stages.NextJob()

	segmentStateEquals(t, stages, `
CCCSSS..
CSSS....
CC......`)

	stages.forceTransition(1, 1, SegmentCompleted)
	stages.NextJob()

	segmentStateEquals(t, stages, `
CCCSSS..
CCSS....
CCS.....`)

}

func segmentStateEquals(t *testing.T, s *Stages, segments string) {
	t.Helper()

	out := strings.Builder{}
	for i := 0; i < len(s.stages); i++ {
		for _, segment := range s.state {
			out.WriteString(map[SegmentState]string{
				SegmentPending:        ".",
				SegmentPartialPresent: "P",
				SegmentScheduled:      "S",
				SegmentMerging:        "M",
				SegmentCompleted:      "C",
			}[segment[i]])
		}
		out.WriteString("\n")
	}

	assert.Equal(t, strings.TrimSpace(segments), strings.TrimSpace(out.String()))
}