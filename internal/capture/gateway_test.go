package capture

import (
	"bytes"
	"compress/zlib"
	"diskord/internal/config"
	"encoding/binary"
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
func etfEvent(v any) []byte {
	b := bytes.NewBuffer([]byte{131})
	var write func(any)
	write = func(v any) {
		switch x := v.(type) {
		case string:
			b.WriteByte(109)
			_ = binary.Write(b, binary.BigEndian, uint32(len(x)))
			b.WriteString(x)
		case int:
			b.WriteByte(97)
			b.WriteByte(byte(x))
		case []any:
			b.WriteByte(108)
			_ = binary.Write(b, binary.BigEndian, uint32(len(x)))
			for _, v := range x {
				write(v)
			}
			b.WriteByte(106)
		case map[string]any:
			b.WriteByte(116)
			_ = binary.Write(b, binary.BigEndian, uint32(len(x)))
			for k, v := range x {
				write(k)
				write(v)
			}
		default:
			panic("unsupported test ETF value")
		}
	}
	write(v)
	return b.Bytes()
}
func TestETFReadyGuildsAndChannels(t *testing.T) {
	ready := etfEvent(map[string]any{"op": 0, "t": "READY", "d": map[string]any{
		"token": "DO-NOT-SAVE", "guilds": []any{map[string]any{
			"id": 123, "properties": map[string]any{"name": "Example"},
			"channels": []any{map[string]any{"id": 45, "name": "general", "type": 0}},
		}},
	}})
	var payload bytes.Buffer
	z := zlib.NewWriter(&payload)
	_, _ = z.Write(ready)
	_ = z.Flush()
	e := &Engine{Metrics: &Metrics{}, Config: config.Default, writeQ: make(chan writeJob, 8)}
	observer := e.ObserveGateway(url.Values{"encoding": {"etf"}, "compress": {"zlib-stream"}})
	for raw := payload.Bytes(); len(raw) > 0; {
		n := min(len(raw), 17)
		observer.Push(raw[:n])
		raw = raw[n:]
	}
	observer.Close()
	if len(e.writeQ) != 1 || e.Metrics.GatewayEvents.Load() != 1 {
		t.Fatalf("ETF event not captured: %+v", e.Metrics.Snapshot())
	}
	b := (<-e.writeQ).batch
	if len(b.Guilds) != 1 || len(b.Channels) != 1 || b.Channels[0].GuildID == nil || *b.Channels[0].GuildID != "123" {
		t.Fatalf("guild/channel projection failed: %+v", b)
	}
}
func TestUnknownGatewayEncodingIsSkipped(t *testing.T) {
	e := &Engine{Metrics: &Metrics{}, Config: config.Default}
	if e.ObserveGateway(url.Values{"encoding": {"unknown"}}) != nil || e.Metrics.UnsupportedGateway.Load() != 1 {
		t.Fatal("unknown encoding silently accepted")
	}
}
