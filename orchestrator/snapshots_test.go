package orchestrator

import (
	"github.com/streamingfast/substreams/block"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSnapshots_LastCompleted(t *testing.T) {
	assert.Equal(t, 300, int((&Snapshots{
		Completes: parseRanges("100-200,100-300"),
		Partials:  parseRanges("300-400"),
	}).LastCompletedBlock()))

	assert.Equal(t, 0, int((&Snapshots{
		Completes: parseRanges(""),
		Partials:  parseRanges("200-300"),
	}).LastCompletedBlock()))
}

func TestSnapshots_LastCompleteBefore(t *testing.T) {
	tests := []struct {
		name         string
		snapshot     *Snapshots
		blockNum     uint64
		expectBrange *block.Range
	}{
		{
			name: "no complete range covering block",
			snapshot: &Snapshots{
				Completes: parseRanges("10-20,10-50,10-1000"),
			},
			blockNum:     0,
			expectBrange: nil,
		},
		{
			name: "no complete range covering block",
			snapshot: &Snapshots{
				Completes: parseRanges("10-20,10-50,10-1000"),
			},
			blockNum:     19,
			expectBrange: nil,
		},
		{
			name: "complete range ending on block",
			snapshot: &Snapshots{
				Completes: parseRanges("10-20,10-50,10-1000"),
			},
			blockNum:     20,
			expectBrange: block.NewRange(10, 20),
		},
		{
			name: "complete range ending just before lookup block",
			snapshot: &Snapshots{
				Completes: parseRanges("10-20,10-50,10-1000"),
			},
			blockNum:     21,
			expectBrange: block.NewRange(10, 20),
		},
		{
			name: "complete range ending before lookup block",
			snapshot: &Snapshots{
				Completes: parseRanges("10-20,10-50,10-1000"),
			},
			blockNum:     49,
			expectBrange: block.NewRange(10, 20),
		},
		{
			name: "better complete range ending on block",
			snapshot: &Snapshots{
				Completes: parseRanges("10-20,10-50,10-1000"),
			},
			blockNum:     50,
			expectBrange: block.NewRange(10, 50),
		},
		{
			name: "another test 1",
			snapshot: &Snapshots{
				Completes: parseRanges("10-20,10-50,10-1000"),
			},
			blockNum:     51,
			expectBrange: block.NewRange(10, 50),
		},
		{
			name: "another test 2",
			snapshot: &Snapshots{
				Completes: parseRanges("10-20,10-50,10-1000"),
			},
			blockNum:     1003,
			expectBrange: block.NewRange(10, 1000),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			blockRange := test.snapshot.LastCompleteSnapshotBefore(test.blockNum)
			assert.Equal(t, test.expectBrange, blockRange)
		})
	}
}
