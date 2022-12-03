package tracking

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/streamingfast/substreams"
	pbsubstreams "github.com/streamingfast/substreams/pb/sf/substreams/v1"
	"github.com/streamingfast/substreams/reqctx"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type BytesMeter interface {
	AddBytesWritten(n int)
	AddBytesRead(n int)

	BytesWritten() uint64
	BytesRead() uint64

	Launch(ctx context.Context, respFunc substreams.ResponseFunc)
	Send(respFunc substreams.ResponseFunc) error
}

type bytesMeter struct {
	bytesWritten uint64
	bytesRead    uint64

	mu     sync.RWMutex
	logger *zap.Logger
}

func NewBytesMeter(ctx context.Context) BytesMeter {
	return &bytesMeter{
		logger: reqctx.Logger(ctx),
	}
}

func (b *bytesMeter) String() string {
	b.mu.RLock()
	defer b.mu.RUnlock()

	return fmt.Sprintf("bytes written: %d, bytes read: %d", b.bytesWritten, b.bytesRead)
}

func (b *bytesMeter) MarshalLogObject(enc zapcore.ObjectEncoder) error {
	b.mu.RLock()
	defer b.mu.RUnlock()

	enc.AddUint64("bytes_written", b.bytesWritten)
	enc.AddUint64("bytes_read", b.bytesRead)

	return nil
}

func (b *bytesMeter) Start(ctx context.Context, respFunc substreams.ResponseFunc) {
	logger := reqctx.Logger(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Second):
			err := b.Send(respFunc)
			if err != nil {
				logger.Error("unable to send bytes meter", zap.Error(err))
			}
		}
	}
}

func (b *bytesMeter) Launch(ctx context.Context, respFunc substreams.ResponseFunc) {
	go b.Start(ctx, respFunc)
}

func (b *bytesMeter) Send(respFunc substreams.ResponseFunc) error {
	defer func() {
		b.logger.Info("bytes meter", zap.Object("bytes_meter", b))
	}()

	b.mu.RLock()
	defer b.mu.RUnlock()

	var in []*pbsubstreams.ModuleProgress

	in = append(in, &pbsubstreams.ModuleProgress{
		Name: "",
		Type: &pbsubstreams.ModuleProgress_ProcessedBytes_{
			ProcessedBytes: &pbsubstreams.ModuleProgress_ProcessedBytes{
				TotalBytesWritten: b.bytesWritten,
				TotalBytesRead:    b.bytesRead,
			},
		},
	})

	resp := substreams.NewModulesProgressResponse(in)
	err := respFunc(resp)
	if err != nil {
		return err
	}

	return nil
}

func (b *bytesMeter) AddBytesWritten(n int) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if n < 0 {
		panic("negative value")
	}

	b.bytesWritten += uint64(n)
}

func (b *bytesMeter) AddBytesRead(n int) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.bytesRead += uint64(n)
}

func (b *bytesMeter) BytesWritten() uint64 {
	b.mu.RLock()
	defer b.mu.RUnlock()

	return b.bytesWritten
}

func (b *bytesMeter) BytesRead() uint64 {
	b.mu.RLock()
	defer b.mu.RUnlock()

	return b.bytesRead
}

type noopBytesMeter struct{}

func (_ *noopBytesMeter) AddBytesWritten(n int)                                        { return }
func (_ *noopBytesMeter) AddBytesRead(n int)                                           { return }
func (_ *noopBytesMeter) BytesWritten() uint64                                         { return 0 }
func (_ *noopBytesMeter) BytesRead() uint64                                            { return 0 }
func (_ *noopBytesMeter) Launch(ctx context.Context, respFunc substreams.ResponseFunc) {}
func (_ *noopBytesMeter) Send(respFunc substreams.ResponseFunc) error                  { return nil }

var NoopBytesMeter BytesMeter = &noopBytesMeter{}
