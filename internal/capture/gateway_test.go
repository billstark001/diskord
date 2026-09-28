package capture

import (
	"bytes"
	"compress/zlib"
	"diskord/internal/config"
	"github.com/klauspost/compress/zstd"
	"net/url"
	"testing"
)

func TestGatewayPersistentStreams(t *testing.T) {
	for _, kind := range []string{"", "zlib-stream", "zstd-stream"} {
		t.Run(kind, func(t *testing.T) {
			event := []byte(`{"op":0,"t":"MESSAGE_CREATE","d":{"id":"123","channel_id":"456","content":"hello"}}`)
			var payload bytes.Buffer
			switch kind {
			case "zlib-stream":
				w := zlib.NewWriter(&payload)
				for i := 0; i < 2; i++ {
					w.Write(event)
					w.Flush()
				} // intentionally no zlib Close: Discord stream is persistent
			case "zstd-stream":
				w, e := zstd.NewWriter(&payload)
				if e != nil {
					t.Fatal(e)
				}
				for i := 0; i < 2; i++ {
					w.Write(event)
					w.Flush()
				}
				w.Close()
			default:
				payload.Write(event)
				payload.Write(event)
			}
			e := &Engine{Metrics: &Metrics{}, Config: config.Default, writeQ: make(chan writeJob, 8)}
			observer := e.ObserveGateway(url.Values{"encoding": {"json"}, "compress": {kind}})
			raw := payload.Bytes()
			for len(raw) > 0 {
				n := min(len(raw), 17)
				observer.Push(raw[:n])
				raw = raw[n:]
			}
			observer.Close()
			if len(e.writeQ) != 2 {
				t.Fatalf("got %d events, metrics %+v", len(e.writeQ), e.Metrics.Snapshot())
			}
		})
	}
}
func TestETFIsVisibleAndSkipped(t *testing.T) {
	e := &Engine{Metrics: &Metrics{}, Config: config.Default}
	if e.ObserveGateway(url.Values{"encoding": {"etf"}}) != nil || e.Metrics.UnsupportedGateway.Load() != 1 {
		t.Fatal("ETF silently accepted")
	}
}
