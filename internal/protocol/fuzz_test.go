package protocol

import "testing"

func FuzzGateway(f *testing.F) {
	f.Add([]byte(`{"op":0,"t":"MESSAGE_CREATE","d":{"id":"1","content":"x"}}`))
	f.Add([]byte(`{"op":2,"d":{"token":"synthetic-not-a-real-token"}}`))
	f.Fuzz(func(t *testing.T, b []byte) {
		if len(b) > 1024*1024 {
			return
		}
		_, _ = Gateway(b)
	})
}
