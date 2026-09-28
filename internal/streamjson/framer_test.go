package streamjson

import (
	"bytes"
	"compress/zlib"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

func TestConcatenatedAndEscaped(t *testing.T) {
	in := ` {"d":{"content":"hello } [ \\\""}}{"op":0,"d":[1,2]}`
	var out [][]byte
	e := Read(strings.NewReader(in), 1000, func(b []byte) error { out = append(out, append([]byte(nil), b...)); return nil })
	if !errors.Is(e, io.EOF) || len(out) != 2 {
		t.Fatal(e, len(out))
	}
}
func TestLimits(t *testing.T) {
	if e := Read(strings.NewReader(`{"big":"123456789"}`), 8, func([]byte) error { return nil }); !errors.Is(e, ErrLimit) {
		t.Fatal(e)
	}
	if e := Read(strings.NewReader(`{"x":`), 100, func([]byte) error { return nil }); !errors.Is(e, io.ErrUnexpectedEOF) {
		t.Fatal(e)
	}
}
func TestPersistentZlibDictionary(t *testing.T) {
	var encoded bytes.Buffer
	w := zlib.NewWriter(&encoded)
	for _, s := range []string{`{"op":0,"t":"MESSAGE_CREATE","d":{"content":"same words same words"}}`, `{"op":0,"t":"MESSAGE_UPDATE","d":{"content":"same words changed"}}`} {
		w.Write([]byte(s))
		w.Flush()
	}
	w.Close()
	r, e := zlib.NewReader(&encoded)
	if e != nil {
		t.Fatal(e)
	}
	defer r.Close()
	count := 0
	e = Read(r, 1000, func([]byte) error { count++; return nil })
	if e != io.EOF || count != 2 {
		t.Fatal(e, count)
	}
}

// A flushed dispatch must be visible while the zlib stream is still open.
func TestLiveZlibFlush(t *testing.T) {
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	received := make(chan struct{}, 1)
	done := make(chan error, 1)
	go func() {
		zr, err := zlib.NewReader(reader)
		if err != nil {
			done <- err
			return
		}
		defer zr.Close()
		done <- Read(zr, 1024, func([]byte) error { received <- struct{}{}; return nil })
	}()
	zw := zlib.NewWriter(writer)
	if _, err := zw.Write([]byte(`{"op":0,"d":{"content":"live"}}`)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Flush(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-received:
	case err := <-done:
		t.Fatalf("reader ended before dispatch: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("dispatch was withheld until stream closure")
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, io.EOF) {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("reader did not finish")
	}
}
