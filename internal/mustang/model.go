package mustang

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode"
)

// Value holds a string, time.Time, bool or float64.
type Value any

type (
	Row   map[string]Value // column -> value
	Table map[string]Row   // rowKey -> row
)

const sep byte = 0x00

// Value tags; stored as the first byte of every encoded value.
const (
	tagString byte = iota + 1
	tagTime
	tagBool
	tagNumber
)

var (
	ErrNotFound = errors.New("mustang: not found")
	ErrName     = errors.New("mustang: invalid name")
	ErrType     = errors.New("mustang: unsupported value type")
	ErrCorrupt  = errors.New("mustang: corrupt value")
)

// checkName rejects empty names and names with whitespace or the key separator.
func checkName(s string) error {
	if len(s) == 0 {
		return fmt.Errorf("%w: empty", ErrName)
	}
	if strings.IndexFunc(s, func(r rune) bool { return unicode.IsSpace(r) || r == rune(sep) }) >= 0 {
		return fmt.Errorf("%w: %q", ErrName, s)
	}
	return nil
}

func checkNames(names ...string) error {
	for _, n := range names {
		if err := checkName(n); err != nil {
			return err
		}
	}
	return nil
}

// key joins names with sep. A trailing sep is added when fewer
// than three names are given, so the result is a prefix that
// cannot match a longer sibling name (e.g. "users" vs "users2").
func key(names ...string) []byte {
	var buf bytes.Buffer
	for i, n := range names {
		if i > 0 {
			buf.WriteByte(sep)
		}
		buf.WriteString(n)
	}
	if len(names) < 3 {
		buf.WriteByte(sep)
	}
	return buf.Bytes()
}

// Name levels within a cell key.
const (
	lvlTable = iota
	lvlRow
	lvlCol
)

// field returns the name at level lvl of a cell key without allocating.
func field(k []byte, lvl int) []byte {
	for ; lvl > 0; lvl-- {
		i := bytes.IndexByte(k, sep)
		if i < 0 {
			return nil
		}
		k = k[i+1:]
	}
	if i := bytes.IndexByte(k, sep); i >= 0 {
		return k[:i]
	}
	return k
}

func encode(v Value) ([]byte, error) {
	switch v := v.(type) {
	case string:
		return append([]byte{tagString}, v...), nil
	case time.Time:
		return append([]byte{tagTime}, v.Format(time.RFC3339Nano)...), nil
	case bool:
		if v {
			return []byte{tagBool, 1}, nil
		}
		return []byte{tagBool, 0}, nil
	case float64:
		buf := make([]byte, 9)
		buf[0] = tagNumber
		binary.BigEndian.PutUint64(buf[1:], math.Float64bits(v))
		return buf, nil
	}
	return nil, fmt.Errorf("%w: %T", ErrType, v)
}

func decode(b []byte) (Value, error) {
	if len(b) == 0 {
		return nil, ErrCorrupt
	}
	tag, p := b[0], b[1:]
	switch tag {
	case tagString:
		return string(p), nil
	case tagTime:
		t, err := time.Parse(time.RFC3339Nano, string(p))
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrCorrupt, err)
		}
		return t, nil
	case tagBool:
		if len(p) != 1 {
			return nil, ErrCorrupt
		}
		return p[0] == 1, nil
	case tagNumber:
		if len(p) != 8 {
			return nil, ErrCorrupt
		}
		return math.Float64frombits(binary.BigEndian.Uint64(p)), nil
	}
	return nil, fmt.Errorf("%w: tag %d", ErrCorrupt, tag)
}
