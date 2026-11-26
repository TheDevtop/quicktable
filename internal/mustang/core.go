package mustang

/*
	The Mustang wide-column engine,
	optimized for read and query performance.

	Writes go through Batch; Store only reads.
*/

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode"

	"github.com/dgraph-io/badger/v4"
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

// encode tags v and lays it out so that, for values of one type,
// bytes.Compare on the encodings orders them like the values.
// Conditions and Min/Max therefore work on raw cells without decoding.
// Times are stored as a UTC instant; the zone is not kept.
func encode(v Value) ([]byte, error) {
	switch v := v.(type) {
	case string:
		return append([]byte{tagString}, v...), nil
	case time.Time:
		buf := make([]byte, 13)
		buf[0] = tagTime
		binary.BigEndian.PutUint64(buf[1:], uint64(v.Unix())^(1<<63))
		binary.BigEndian.PutUint32(buf[9:], uint32(v.Nanosecond()))
		return buf, nil
	case bool:
		if v {
			return []byte{tagBool, 1}, nil
		}
		return []byte{tagBool, 0}, nil
	case float64:
		if math.IsNaN(v) {
			return nil, fmt.Errorf("%w: NaN", ErrType)
		}
		if v == 0 {
			v = 0 // -0 == 0, so store both alike
		}
		bits := math.Float64bits(v)
		if bits>>63 == 1 {
			bits = ^bits
		} else {
			bits |= 1 << 63
		}
		buf := make([]byte, 9)
		buf[0] = tagNumber
		binary.BigEndian.PutUint64(buf[1:], bits)
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
		if len(p) != 12 {
			return nil, ErrCorrupt
		}
		sec := int64(binary.BigEndian.Uint64(p) ^ (1 << 63))
		return time.Unix(sec, int64(binary.BigEndian.Uint32(p[8:]))).UTC(), nil
	case tagBool:
		if len(p) != 1 {
			return nil, ErrCorrupt
		}
		return p[0] == 1, nil
	case tagNumber:
		if len(p) != 8 {
			return nil, ErrCorrupt
		}
		bits := binary.BigEndian.Uint64(p)
		if bits>>63 == 1 {
			bits &^= 1 << 63
		} else {
			bits = ^bits
		}
		return math.Float64frombits(bits), nil
	}
	return nil, fmt.Errorf("%w: tag %d", ErrCorrupt, tag)
}

// cell is an encoded value kept by a scan.
type cell struct {
	col, val []byte
}

func decodeRow(cells []cell) (Row, error) {
	row := make(Row, len(cells))
	for _, c := range cells {
		v, err := decode(c.val)
		if err != nil {
			return nil, err
		}
		row[string(c.col)] = v
	}
	return row, nil
}

type Store struct {
	db *badger.DB
}

func Open(opt badger.Options) (*Store, error) {
	db, err := badger.Open(opt)
	if err != nil {
		return nil, err
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

// iter calls fn for every item under prefix. Values are prefetched
// only when the caller reads most of them.
func iter(txn *badger.Txn, prefix []byte, vals bool, fn func(it *badger.Iterator) error) error {
	opt := badger.DefaultIteratorOptions
	opt.PrefetchValues = vals
	opt.Prefix = prefix
	it := txn.NewIterator(opt)
	defer it.Close()
	for it.Rewind(); it.Valid(); it.Next() {
		if err := fn(it); err != nil {
			return err
		}
	}
	return nil
}

// names lists distinct names at level lvl under prefix. After each name
// it seeks past every key sharing it: one seek per name, not per cell.
func (s *Store) names(prefix []byte, lvl int) ([]string, error) {
	var out []string
	err := s.db.View(func(txn *badger.Txn) error {
		opt := badger.DefaultIteratorOptions
		opt.PrefetchValues = false
		opt.Prefix = prefix
		it := txn.NewIterator(opt)
		defer it.Close()
		next := append([]byte{}, prefix...)
		for it.Rewind(); it.Valid(); {
			n := field(it.Item().Key(), lvl)
			out = append(out, string(n))
			next = append(append(next[:len(prefix)], n...), sep+1)
			it.Seek(next)
		}
		return nil
	})
	return out, err
}

// Scan table
func (s *Store) ScanTables() ([]string, error) {
	return s.names(nil, lvlTable)
}

// Get table
func (s *Store) GetTable(t string) (Table, error) {
	tab := Table{}
	err := s.Query(t).scan(nil, func(k []byte, cells []cell) error {
		row, err := decodeRow(cells)
		if err != nil {
			return err
		}
		tab[string(k)] = row
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(tab) == 0 {
		return nil, ErrNotFound
	}
	return tab, nil
}

// Scan rowKey
func (s *Store) ScanRows(t string) ([]string, error) {
	if err := checkName(t); err != nil {
		return nil, err
	}
	return s.names(key(t), lvlRow)
}

// Get rowKey
func (s *Store) GetRow(t, r string) (Row, error) {
	if err := checkNames(t, r); err != nil {
		return nil, err
	}
	row := Row{}
	err := s.db.View(func(txn *badger.Txn) error {
		return iter(txn, key(t, r), true, func(it *badger.Iterator) error {
			return it.Item().Value(func(b []byte) error {
				v, err := decode(b)
				if err != nil {
					return err
				}
				row[string(field(it.Item().Key(), lvlCol))] = v
				return nil
			})
		})
	})
	if err != nil {
		return nil, err
	}
	if len(row) == 0 {
		return nil, ErrNotFound
	}
	return row, nil
}

// Scan column; walks every key of the table, as there is no column index.
func (s *Store) ScanColumns(t string) ([]string, error) {
	if err := checkName(t); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var out []string
	err := s.db.View(func(txn *badger.Txn) error {
		return iter(txn, key(t), false, func(it *badger.Iterator) error {
			c := field(it.Item().Key(), lvlCol)
			if !seen[string(c)] {
				seen[string(c)] = true
				out = append(out, string(c))
			}
			return nil
		})
	})
	return out, err
}

// Get column; returns rowKey -> value for rows that have the column.
func (s *Store) GetColumn(t, c string) (map[string]Value, error) {
	out := map[string]Value{}
	err := s.Query(t).fold(c, func(k, b []byte) error {
		v, err := decode(b)
		if err != nil {
			return err
		}
		out[string(k)] = v
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, ErrNotFound
	}
	return out, nil
}

// Get value
func (s *Store) GetValue(t, r, c string) (Value, error) {
	vs, err := s.GetValues([]Cell{{t, r, c}})
	if err != nil {
		return nil, err
	}
	if vs[0] == nil {
		return nil, ErrNotFound
	}
	return vs[0], nil
}
