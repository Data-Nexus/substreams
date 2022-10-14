package pipeline

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/streamingfast/bstream/stream"
	pbsubstreams "github.com/streamingfast/substreams/pb/sf/substreams/v1"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// OnStreamTerminated performs flush of store and setting trailers when the stream terminated gracefully from our point of view.
// If the stream terminated gracefully, we return `nil` otherwise, the original is returned.
func (p *Pipeline) OnStreamTerminated(streamSrv pbsubstreams.Stream_BlocksServer, err error) error {
	isStopBlockReachedErr := errors.Is(err, stream.ErrStopBlockReached)

	if isStopBlockReachedErr || errors.Is(err, io.EOF) {
		if isStopBlockReachedErr {
			p.reqCtx.Logger().Debug("stream of blocks reached end block, triggering StoreSave",
				zap.Uint64("stop_block_num", p.reqCtx.StopBlockNum()),
			)

			// We use `StopBlockNum` as the argument to flush stores as possible boundaries (if chain has holes...)
			//
			// `OnStreamTerminated` is invoked by the service when an error occurs with the connection, in this case,
			// we are outside any active span and we want to attach the event to the root span of the pipeline
			// which should always be set.
			if err := p.flushStores(p.reqCtx.StopBlockNum(), p.rootSpan); err != nil {
				return status.Errorf(codes.Internal, "handling store save boundaries: %s", err)
			}
		}

		partialRanges := make([]string, len(p.partialsWritten))
		for i, rng := range p.partialsWritten {
			partialRanges[i] = fmt.Sprintf("%d-%d", rng.StartBlock, rng.ExclusiveEndBlock)
		}

		p.reqCtx.Logger().Info("setting trailer", zap.Strings("ranges", partialRanges))
		streamSrv.SetTrailer(metadata.MD{"substreams-partials-written": []string{strings.Join(partialRanges, ",")}})

		// It was an ok error, so let's
		return nil
	}

	// We are not responsible of doing any other error handling here, caller will deal with them
	return err
}