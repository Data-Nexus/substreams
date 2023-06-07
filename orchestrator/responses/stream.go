package responses

import (
	"github.com/streamingfast/substreams"
	pbsubstreamsrpc "github.com/streamingfast/substreams/pb/sf/substreams/rpc/v2"
)

type Stream struct {
	respFunc substreams.ResponseFunc
}

func New(respFunc substreams.ResponseFunc) *Stream {
	return &Stream{
		respFunc: respFunc,
	}
}

func (s *Stream) InitialProgressMessages(in []*pbsubstreamsrpc.ModuleProgress) error {
	return s.respFunc(substreams.NewModulesProgressResponse(in))
}

func (s *Stream) RPCFailedProgressResponse(moduleName, reason string, logs []string, logsTruncated bool) error {
	return s.respFunc(&pbsubstreamsrpc.Response{
		Message: &pbsubstreamsrpc.Response_Progress{
			Progress: &pbsubstreamsrpc.ModulesProgress{
				Modules: []*pbsubstreamsrpc.ModuleProgress{
					{
						Name: moduleName,
						Type: &pbsubstreamsrpc.ModuleProgress_Failed_{
							Failed: &pbsubstreamsrpc.ModuleProgress_Failed{
								Reason:        reason,
								Logs:          logs,
								LogsTruncated: logsTruncated,
							},
						},
					},
				},
			},
		},
	})
}

func (s *Stream) RPCRangeProgressResponse(moduleName string, start, end uint64) error {
	return s.respFunc(&pbsubstreamsrpc.Response{
		Message: &pbsubstreamsrpc.Response_Progress{
			Progress: &pbsubstreamsrpc.ModulesProgress{
				Modules: []*pbsubstreamsrpc.ModuleProgress{
					{
						Name: moduleName,
						Type: &pbsubstreamsrpc.ModuleProgress_ProcessedRanges_{
							ProcessedRanges: &pbsubstreamsrpc.ModuleProgress_ProcessedRanges{
								ProcessedRanges: []*pbsubstreamsrpc.BlockRange{
									{
										StartBlock: start,
										EndBlock:   end,
									},
								},
							},
						},
					},
				},
			},
		},
	})
}

func (s *Stream) RPCProcessedBytes(
	moduleName string,
	bytesReadDelta uint64,
	bytesWrittenDelta uint64,
	totalBytesRead uint64,
	totalBytesWritten uint64,
	nanoSeconds uint64,
) error {
	s.respFunc(&pbsubstreamsrpc.Response{
		Message: &pbsubstreamsrpc.Response_Progress{
			Progress: &pbsubstreamsrpc.ModulesProgress{
				Modules: []*pbsubstreamsrpc.ModuleProgress{
					{
						Name: moduleName,
						Type: &pbsubstreamsrpc.ModuleProgress_ProcessedBytes_{
							ProcessedBytes: &pbsubstreamsrpc.ModuleProgress_ProcessedBytes{
								BytesReadDelta:    bytesReadDelta,
								BytesWrittenDelta: bytesWrittenDelta,
								TotalBytesRead:    totalBytesRead,
								TotalBytesWritten: totalBytesWritten,
								NanoSecondsDelta:  nanoSeconds,
							},
						},
					},
				},
			},
		},
	})
}

/*
outputstream.Walker
orchestrator/execout/stream.go Stream
orchestrator/execout/walker.go Walker
orchestrator/linear/reader.go Reader
orchestrator/execout/linearreader.go LinearReader
orchestrator/execout/walker.go execout.Walker


*/