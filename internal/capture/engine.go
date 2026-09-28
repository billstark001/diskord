package capture

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"diskord/internal/config"
	"diskord/internal/model"
	"diskord/internal/protocol"
	"diskord/internal/store"
	"github.com/andybalholm/brotli"
	"github.com/klauspost/compress/zstd"
)

type Metrics struct {
	HTTPBodies, GatewayEvents, BatchesCommitted, DroppedBatches, HTTPDropped, GatewayLost, UnsupportedGateway, DecodeErrors, StoreErrors, Assets, AssetSkipped atomic.Uint64
	ActiveGateway                                                                                                                                              atomic.Int64
}
type Snapshot struct {
	HTTPBodies, GatewayEvents, BatchesCommitted, DroppedBatches, HTTPDropped, GatewayLost, UnsupportedGateway, DecodeErrors, StoreErrors, Assets, AssetSkipped uint64
	ActiveGateway                                                                                                                                              int64
}

func (m *Metrics) Snapshot() Snapshot {
	return Snapshot{HTTPBodies: m.HTTPBodies.Load(), GatewayEvents: m.GatewayEvents.Load(), BatchesCommitted: m.BatchesCommitted.Load(), DroppedBatches: m.DroppedBatches.Load(), HTTPDropped: m.HTTPDropped.Load(), GatewayLost: m.GatewayLost.Load(), UnsupportedGateway: m.UnsupportedGateway.Load(), DecodeErrors: m.DecodeErrors.Load(), StoreErrors: m.StoreErrors.Load(), Assets: m.Assets.Load(), AssetSkipped: m.AssetSkipped.Load(), ActiveGateway: m.ActiveGateway.Load()}
}

type HTTPJob struct {
	Body                       []byte
	Host, Path, Mime, Encoding string
	Resource                   bool
}
type writeJob struct {
	batch *model.Batch
	asset *HTTPJob
}
type Engine struct {
	Store                   *store.Store
	Config                  func() config.Config
	Metrics                 *Metrics
	httpMu, batchMu         sync.RWMutex
	httpClosed, batchClosed bool
	httpQ                   chan HTTPJob
	writeQ                  chan writeJob
	httpWG, writeWG         sync.WaitGroup
	writeCtx                context.Context
	writeCancel             context.CancelFunc
}

func New(s *store.Store, current func() config.Config) *Engine {
	e := &Engine{Store: s, Config: current, Metrics: &Metrics{}, httpQ: make(chan HTTPJob, 8), writeQ: make(chan writeJob, current().Capture.Queue)}
	e.writeCtx, e.writeCancel = context.WithCancel(context.Background())
	for i := 0; i < 2; i++ {
		e.httpWG.Add(1)
		go e.httpWorker()
	}
	e.writeWG.Add(1)
	go e.writeWorker()
	return e
}
func (e *Engine) HTTP(j HTTPJob) {
	e.httpMu.RLock()
	defer e.httpMu.RUnlock()
	if e.httpClosed {
		return
	}
	select {
	case e.httpQ <- j:
	default:
		e.Metrics.HTTPDropped.Add(1)
	}
}
func (e *Engine) Submit(b model.Batch) {
	if b.Empty() {
		return
	}
	e.submit(writeJob{batch: &b})
}
func (e *Engine) submit(j writeJob) {
	e.batchMu.RLock()
	defer e.batchMu.RUnlock()
	if e.batchClosed {
		return
	}
	select {
	case e.writeQ <- j:
	default:
		e.Metrics.DroppedBatches.Add(1)
	}
}
func (e *Engine) Close() {
	e.httpMu.Lock()
	if !e.httpClosed {
		e.httpClosed = true
		close(e.httpQ)
	}
	e.httpMu.Unlock()
	e.httpWG.Wait()
	e.batchMu.Lock()
	if !e.batchClosed {
		e.batchClosed = true
		close(e.writeQ)
	}
	e.batchMu.Unlock()
	timer := time.AfterFunc(30*time.Second, e.writeCancel)
	e.writeWG.Wait()
	timer.Stop()
	e.writeCancel()
}
func Decode(data []byte, encoding string, max int) ([]byte, error) {
	var r io.Reader = bytes.NewReader(data)
	var closeReader func()
	switch strings.ToLower(strings.TrimSpace(encoding)) {
	case "", "identity":
	case "gzip":
		g, err := gzip.NewReader(r)
		if err != nil {
			return nil, err
		}
		r = g
		closeReader = func() { g.Close() }
	case "deflate":
		z, err := zlib.NewReader(r)
		if err == nil {
			r = z
			closeReader = func() { z.Close() }
		} else {
			f := flate.NewReader(bytes.NewReader(data))
			r = f
			closeReader = func() { f.Close() }
		}
	case "br":
		r = brotli.NewReader(r)
	case "zstd":
		z, err := zstd.NewReader(r, zstd.WithDecoderConcurrency(1), zstd.WithDecoderMaxMemory(uint64(max)*4))
		if err != nil {
			return nil, err
		}
		r = z
		closeReader = z.Close
	default:
		return nil, errors.New("unsupported HTTP content encoding")
	}
	if closeReader != nil {
		defer closeReader()
	}
	out, err := io.ReadAll(io.LimitReader(r, int64(max)+1))
	if err != nil {
		return nil, err
	}
	if len(out) > max {
		return nil, errors.New("decoded HTTP body exceeds limit")
	}
	return out, nil
}
func (e *Engine) httpWorker() {
	defer e.httpWG.Done()
	for j := range e.httpQ {
		c := e.Config()
		limit := c.Capture.MaxHTTPBytes
		if j.Resource {
			if !c.Resources.Enabled {
				continue
			}
			limit = c.Resources.MaxBytes
		}
		body, err := Decode(j.Body, j.Encoding, limit)
		j.Body = nil
		if err != nil {
			e.Metrics.DecodeErrors.Add(1)
			continue
		}
		if j.Resource {
			j.Body = body
			e.submit(writeJob{asset: &j})
			continue
		}
		b, err := protocol.HTTP(j.Path, body)
		if err != nil {
			e.Metrics.DecodeErrors.Add(1)
			continue
		}
		e.Metrics.HTTPBodies.Add(1)
		e.Submit(b)
	}
}
func (e *Engine) writeWorker() {
	defer e.writeWG.Done()
	for j := range e.writeQ {
		if e.writeCtx.Err() != nil {
			e.Metrics.DroppedBatches.Add(1)
			continue
		}
		var err error
		skipped := false
		for attempt := 0; attempt < 3; attempt++ {
			ctx, cancel := context.WithTimeout(e.writeCtx, 10*time.Second)
			if j.batch != nil {
				err = e.Store.Apply(ctx, *j.batch)
			} else if j.asset != nil {
				c := e.Config()
				if !c.Resources.Enabled {
					cancel()
					err = nil
					skipped = true
					break
				}
				a := j.asset
				err = e.Store.SaveAsset(ctx, a.Host, a.Path, a.Mime, a.Body, c.Resources.MaxTotalBytes)
			}
			cancel()
			if err == nil || errors.Is(err, store.ErrQuota) || e.writeCtx.Err() != nil {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		if skipped {
			continue
		}
		if errors.Is(err, store.ErrQuota) {
			e.Metrics.AssetSkipped.Add(1)
		} else if err != nil {
			e.Metrics.StoreErrors.Add(1)
		} else if j.batch != nil {
			e.Metrics.BatchesCommitted.Add(1)
		} else {
			e.Metrics.Assets.Add(1)
		}
	}
}
