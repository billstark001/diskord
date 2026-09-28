package capture

import (
	"compress/zlib"
	"errors"
	"io"
	"net/url"
	"sync"
	"sync/atomic"

	"diskord/internal/protocol"
	"diskord/internal/streamjson"
	"github.com/klauspost/compress/zstd"
)

// Observer accepts ONLY bytes read from the server. There is intentionally no
// client-frame observer and no parameter for headers, session IDs or tokens.
type Observer struct {
	engine *Engine
	chunks chan []byte
	closed atomic.Bool
	mu     sync.RWMutex
	wg     sync.WaitGroup
}
type chunksReader struct {
	chunks  <-chan []byte
	current []byte
}

func (r *chunksReader) Read(p []byte) (int, error) {
	for len(r.current) == 0 {
		b, ok := <-r.chunks
		if !ok {
			return 0, io.EOF
		}
		r.current = b
	}
	n := copy(p, r.current)
	r.current = r.current[n:]
	return n, nil
}
func (e *Engine) ObserveGateway(query url.Values) *Observer {
	encoding := query.Get("encoding")
	compression := query.Get("compress")
	if (encoding != "" && encoding != "json") || (compression != "" && compression != "zlib-stream" && compression != "zstd-stream") {
		e.Metrics.UnsupportedGateway.Add(1)
		return nil
	}
	o := &Observer{engine: e, chunks: make(chan []byte, 64)}
	o.wg.Add(1)
	go func() {
		defer o.wg.Done()
		defer o.disable()
		var reader io.Reader = &chunksReader{chunks: o.chunks}
		var err error
		switch compression {
		case "zlib-stream":
			var z io.ReadCloser
			z, err = zlib.NewReader(reader)
			if err == nil {
				reader = z
				defer z.Close()
			}
		case "zstd-stream":
			var z *zstd.Decoder
			z, err = zstd.NewReader(reader, zstd.WithDecoderConcurrency(1), zstd.WithDecoderMaxMemory(uint64(e.Config().Capture.MaxEventBytes)*4))
			if err == nil {
				reader = z
				defer z.Close()
			}
		}
		if err == nil {
			err = streamjson.Read(reader, e.Config().Capture.MaxEventBytes, func(data []byte) error {
				b, err := protocol.Gateway(data)
				if err != nil {
					return err
				}
				e.Metrics.GatewayEvents.Add(1)
				e.Submit(b)
				return nil
			})
		}
		// Continuous compression streams usually have no final checksum on disconnect.
		if err != nil && !errors.Is(err, io.EOF) && !o.closed.Load() {
			e.Metrics.DecodeErrors.Add(1)
			e.Metrics.GatewayLost.Add(1)
		}
	}()
	return o
}
func (o *Observer) Push(b []byte) {
	if o == nil || len(b) == 0 {
		return
	}
	o.mu.RLock()
	if o.closed.Load() {
		o.mu.RUnlock()
		return
	}
	// Caller supplies <= 32 KiB; bound each queued chunk even if reused elsewhere.
	if len(b) > 32*1024 {
		o.mu.RUnlock()
		o.engine.Metrics.GatewayLost.Add(1)
		o.disable()
		return
	}
	data := append([]byte(nil), b...)
	select {
	case o.chunks <- data:
		o.mu.RUnlock()
	default:
		o.mu.RUnlock()
		o.engine.Metrics.GatewayLost.Add(1)
		o.disable()
	}
}
func (o *Observer) disable() {
	if o == nil {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if !o.closed.Swap(true) {
		close(o.chunks)
	}
}
func (o *Observer) Close() {
	if o == nil {
		return
	}
	o.disable()
	o.wg.Wait()
}
