// Package streamjson frames concatenated JSON objects from a persistent
// decompression stream. Limits apply to decoded bytes, not just compressed input.
package streamjson

import (
	"errors"
	"io"
)

var ErrLimit = errors.New("decoded Gateway object exceeds configured limit")
var ErrSyntax = errors.New("invalid Gateway JSON stream")

func Read(r io.Reader, max int, emit func([]byte) error) error {
	if max < 2 {
		return ErrLimit
	}
	buf := make([]byte, 32*1024)
	var obj []byte
	depth := 0
	quoted, escape := false, false
	for {
		n, err := r.Read(buf)
		for _, c := range buf[:n] {
			if depth == 0 {
				if c == ' ' || c == '\n' || c == '\r' || c == '\t' {
					continue
				}
				if c != '{' {
					return ErrSyntax
				}
				obj = make([]byte, 0, min(max, 4096))
				depth = 1
				obj = append(obj, c)
				continue
			}
			if len(obj) >= max {
				return ErrLimit
			}
			obj = append(obj, c)
			if quoted {
				if escape {
					escape = false
				} else if c == '\\' {
					escape = true
				} else if c == '"' {
					quoted = false
				}
			} else {
				switch c {
				case '"':
					quoted = true
				case '{', '[':
					depth++
				case '}', ']':
					depth--
				}
				if depth < 0 {
					return ErrSyntax
				}
				if depth == 0 {
					if e := emit(obj); e != nil {
						return e
					}
					obj = nil
				}
			}
		}
		if err != nil {
			if err == io.EOF && depth != 0 {
				return io.ErrUnexpectedEOF
			}
			return err
		}
	}
}
