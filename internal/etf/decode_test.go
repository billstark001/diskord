package etf

import (
	"bytes"
	"io"
	"testing"
)

func TestNumericMapKeyDoesNotAbortEvent(t *testing.T) {
	// {1 => true}, followed by another ETF term in the same byte stream.
	stream := bytes.NewReader([]byte{131, 116, 0, 0, 0, 1, 97, 1, 119, 4, 't', 'r', 'u', 'e', 131, 97, 2})
	first, err := Decode(stream, 64)
	if err != nil {
		t.Fatal(err)
	}
	m, ok := first.(map[string]any)
	if !ok || m["1"] != true {
		t.Fatalf("unexpected term: %#v", first)
	}
	second, err := Decode(stream, 64)
	if err != nil || second != int64(2) {
		t.Fatalf("second term: %#v, %v", second, err)
	}
	_, err = Decode(stream, 64)
	if err != io.EOF {
		t.Fatalf("want EOF, got %v", err)
	}
}
