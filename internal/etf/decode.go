// Package etf decodes the bounded subset of Erlang's External Term Format
// used by Discord Gateway server events. It never retains or writes raw frames.
package etf

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"strconv"
	"strings"
	"unicode/utf8"
)

const maxDepth = 64
const maxItems = 200000

type decoder struct {
	r         io.Reader
	remaining int
}

// Decode reads exactly one versioned term from a continuous stream. Calls may
// be repeated for the next term; each one has an independent size limit.
func Decode(r io.Reader, maxBytes int) (any, error) {
	if maxBytes < 2 {
		return nil, errors.New("ETF event limit too small")
	}
	d := &decoder{r: r, remaining: maxBytes}
	version, err := d.byte()
	if err != nil {
		return nil, err
	}
	if version != 131 {
		return nil, errors.New("invalid ETF version")
	}
	v, err := d.term(0)
	if errors.Is(err, io.EOF) {
		return nil, io.ErrUnexpectedEOF
	}
	return v, err
}

func (d *decoder) bytes(n int) ([]byte, error) {
	if n < 0 || n > d.remaining {
		return nil, errors.New("ETF event exceeds size limit")
	}
	b := make([]byte, n)
	_, err := io.ReadFull(d.r, b)
	if err != nil {
		return nil, err
	}
	d.remaining -= n
	return b, nil
}
func (d *decoder) byte() (byte, error) {
	b, err := d.bytes(1)
	if err != nil {
		return 0, err
	}
	return b[0], nil
}
func (d *decoder) uint16() (int, error) {
	b, err := d.bytes(2)
	if err != nil {
		return 0, err
	}
	return int(binary.BigEndian.Uint16(b)), nil
}
func (d *decoder) uint32() (int, error) {
	b, err := d.bytes(4)
	if err != nil {
		return 0, err
	}
	return int(binary.BigEndian.Uint32(b)), nil
}
func (d *decoder) text(n int) (string, error) {
	b, err := d.bytes(n)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
func (d *decoder) term(depth int) (any, error) {
	if depth >= maxDepth {
		return nil, errors.New("ETF nesting limit exceeded")
	}
	tag, err := d.byte()
	if err != nil {
		return nil, err
	}
	switch tag {
	case 97: // SMALL_INTEGER_EXT
		v, err := d.byte()
		return int64(v), err
	case 98: // INTEGER_EXT
		b, err := d.bytes(4)
		if err != nil {
			return nil, err
		}
		return int64(int32(binary.BigEndian.Uint32(b))), nil
	case 70: // NEW_FLOAT_EXT
		b, err := d.bytes(8)
		if err != nil {
			return nil, err
		}
		v := math.Float64frombits(binary.BigEndian.Uint64(b))
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return nil, errors.New("non-finite ETF float")
		}
		return v, nil
	case 99: // FLOAT_EXT
		b, err := d.bytes(31)
		if err != nil {
			return nil, err
		}
		v, err := strconv.ParseFloat(strings.TrimRight(string(b), "\x00"), 64)
		if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
			return nil, errors.New("invalid ETF float")
		}
		return v, nil
	case 100, 118: // ATOM_EXT / ATOM_UTF8_EXT
		n, err := d.uint16()
		if err != nil {
			return nil, err
		}
		return d.atom(n, tag == 100)
	case 115, 119: // SMALL_ATOM_EXT / SMALL_ATOM_UTF8_EXT
		n, err := d.byte()
		if err != nil {
			return nil, err
		}
		return d.atom(int(n), tag == 115)
	case 106: // NIL_EXT
		return []any{}, nil
	case 107: // STRING_EXT
		n, err := d.uint16()
		if err != nil {
			return nil, err
		}
		return d.text(n)
	case 109: // BINARY_EXT
		n, err := d.uint32()
		if err != nil {
			return nil, err
		}
		return d.text(n)
	case 77: // BIT_BINARY_EXT
		n, err := d.uint32()
		if err != nil {
			return nil, err
		}
		bits, err := d.byte()
		if err != nil {
			return nil, err
		}
		if bits < 1 || bits > 8 {
			return nil, errors.New("invalid ETF bit string")
		}
		return d.text(n)
	case 104, 105, 108: // tuples and LIST_EXT
		var n int
		if tag == 104 {
			b, e := d.byte()
			n, err = int(b), e
		} else {
			n, err = d.uint32()
		}
		if err != nil {
			return nil, err
		}
		if n > maxItems || n > d.remaining {
			return nil, errors.New("ETF list exceeds item limit")
		}
		items := make([]any, 0, n)
		for i := 0; i < n; i++ {
			v, err := d.term(depth + 1)
			if err != nil {
				return nil, err
			}
			items = append(items, v)
		}
		if tag == 108 {
			tail, err := d.byte()
			if err != nil {
				return nil, err
			}
			if tail != 106 {
				return nil, errors.New("improper ETF list")
			}
		}
		return items, nil
	case 116: // MAP_EXT
		n, err := d.uint32()
		if err != nil {
			return nil, err
		}
		if n > maxItems || n > d.remaining/2 {
			return nil, errors.New("ETF map exceeds item limit")
		}
		m := make(map[string]any, n)
		for i := 0; i < n; i++ {
			key, err := d.term(depth + 1)
			if err != nil {
				return nil, err
			}
			name, ok := key.(string)
			if !ok {
				// Undisclosed READY fields may use numeric map keys. They are
				// ignored by the projection, but must not abort the event.
				name = fmt.Sprint(key)
			}
			value, err := d.term(depth + 1)
			if err != nil {
				return nil, err
			}
			m[name] = value
		}
		return m, nil
	case 110, 111: // SMALL_BIG_EXT / LARGE_BIG_EXT
		var n int
		if tag == 110 {
			b, e := d.byte()
			n, err = int(b), e
		} else {
			n, err = d.uint32()
		}
		if err != nil {
			return nil, err
		}
		sign, err := d.byte()
		if err != nil {
			return nil, err
		}
		if sign > 1 {
			return nil, errors.New("invalid ETF big integer sign")
		}
		b, err := d.bytes(n)
		if err != nil {
			return nil, err
		}
		for i, j := 0, len(b)-1; i < j; i, j = i+1, j-1 {
			b[i], b[j] = b[j], b[i]
		}
		v := new(big.Int).SetBytes(b)
		if sign == 1 {
			v.Neg(v)
		}
		if v.IsInt64() {
			return v.Int64(), nil
		}
		return v.String(), nil
	default:
		return nil, fmt.Errorf("unsupported ETF tag %d", tag)
	}
}
func (d *decoder) atom(n int, latin1 bool) (any, error) {
	b, err := d.bytes(n)
	if err != nil {
		return nil, err
	}
	var s string
	if latin1 {
		runes := make([]rune, len(b))
		for i, c := range b {
			runes[i] = rune(c)
		}
		s = string(runes)
	} else {
		if !utf8.Valid(b) {
			return nil, errors.New("invalid ETF atom")
		}
		s = string(b)
	}
	switch s {
	case "true":
		return true, nil
	case "false":
		return false, nil
	case "nil", "null", "undefined":
		return nil, nil
	default:
		return s, nil
	}
}
