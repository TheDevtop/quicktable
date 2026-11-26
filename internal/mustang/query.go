package mustang

/*
	The Mustang wide-column engine,
	optimized for read and query performance.
*/

import (
	"bytes"
	"errors"
	"fmt"
	"maps"

	"github.com/dgraph-io/badger/v4"
)

type Op int

const (
	Eq Op = iota
	Ne
	Lt
	Le
	Gt
	Ge
	Prefix   // string value starts with val
	Exists   // cell is present; val is ignored
	Missing  // cell is absent; val is ignored
	Contains // string value contains val
)

// cond is a test on one column, shared by queries and batches.
// val holds the encoded operand, so cells are tested without decoding.
type cond struct {
	col string
	op  Op
	val []byte
}

func newCond(col string, op Op, val Value) (cond, error) {
	if err := checkName(col); err != nil {
		return cond{}, err
	}
	switch {
	case op < Eq || op > Contains:
		return cond{}, fmt.Errorf("mustang: unknown op %d", op)
	case op == Exists || op == Missing:
		return cond{col: col, op: op}, nil
	}
	enc, err := encode(val)
	if err != nil {
		return cond{}, err
	}
	if (op == Prefix || op == Contains) && enc[0] != tagString {
		return cond{}, fmt.Errorf("%w: prefix and contains need a string", ErrType)
	}
	return cond{col: col, op: op, val: enc}, nil
}

// holds reports whether the condition is met by encoded cell b; an
// empty b is an absent cell, as encoded values are never empty.
// Values of another type than the operand never match.
func (c cond) holds(b []byte) bool {
	switch {
	case len(b) == 0:
		return c.op == Missing
	case c.op == Missing:
		return false
	case c.op == Exists:
		return true
	case b[0] != c.val[0]:
		return false
	case c.op == Prefix:
		return bytes.HasPrefix(b, c.val)
	case c.op == Contains:
		return bytes.Contains(b[1:], c.val[1:])
	}
	n := bytes.Compare(b, c.val)
	switch c.op {
	case Eq:
		return n == 0
	case Ne:
		return n != 0
	case Lt:
		return n < 0
	case Le:
		return n <= 0
	case Gt:
		return n > 0
	}
	return n >= 0 // Ge
}

// Result is one matching row; results are returned in rowKey order.
type Result struct {
	Key string
	Row Row
}

type Query struct {
	s        *Store
	table    string
	prefix   string
	from, to string
	conds    []cond
	cols     map[string]bool
	limit    int
	err      error
}

func (s *Store) Query(t string) *Query {
	return &Query{s: s, table: t, err: checkName(t)}
}

func (q *Query) fail(err error) *Query {
	if q.err == nil {
		q.err = err
	}
	return q
}

// Prefix limits the scan to rowKeys starting with p.
func (q *Query) Prefix(p string) *Query {
	q.prefix = p
	return q
}

// From limits the scan to rowKeys >= k.
func (q *Query) From(k string) *Query {
	q.from = k
	return q
}

// To limits the scan to rowKeys < k.
func (q *Query) To(k string) *Query {
	q.to = k
	return q
}

func (q *Query) Where(col string, op Op, val Value) *Query {
	c, err := newCond(col, op, val)
	if err != nil {
		return q.fail(err)
	}
	q.conds = append(q.conds, c)
	return q
}

// Select limits the returned columns; by default all are returned.
// As in Bigtable, a row with none of the selected columns is dropped.
func (q *Query) Select(cols ...string) *Query {
	if err := checkNames(cols...); err != nil {
		return q.fail(err)
	}
	if q.cols == nil {
		q.cols = map[string]bool{}
	}
	for _, c := range cols {
		q.cols[c] = true
	}
	return q
}

// Limit stops after n returned rows; 0 means no limit.
func (q *Query) Limit(n int) *Query {
	q.limit = n
	return q
}

func (q *Query) Run() ([]Result, error) {
	var out []Result
	err := q.scan(q.cols, func(k []byte, cells []cell) error {
		row, err := decodeRow(cells)
		if err != nil {
			return err
		}
		out = append(out, Result{string(k), row})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Count returns the number of matching rows. Without conditions it
// reads keys only.
func (q *Query) Count() (int, error) {
	n := 0
	err := q.scan(map[string]bool{}, func([]byte, []cell) error {
		n++
		return nil
	})
	return n, err
}

// Sum adds col over matching rows; rows without col are skipped,
// and a non-number value is an error.
func (q *Query) Sum(col string) (float64, error) {
	sum, _, err := q.sum(col)
	return sum, err
}

// Avg is Sum divided by the number of rows that have col.
func (q *Query) Avg(col string) (float64, error) {
	sum, n, err := q.sum(col)
	if err != nil {
		return 0, err
	}
	if n == 0 {
		return 0, ErrNotFound
	}
	return sum / float64(n), nil
}

func (q *Query) sum(col string) (float64, int, error) {
	var sum float64
	n := 0
	err := q.fold(col, func(_, b []byte) error {
		v, err := decode(b)
		if err != nil {
			return err
		}
		f, ok := v.(float64)
		if !ok {
			return fmt.Errorf("%w: %s is %T, not a number", ErrType, col, v)
		}
		sum += f
		n++
		return nil
	})
	return sum, n, err
}

// Min returns the smallest value of col over matching rows.
func (q *Query) Min(col string) (Value, error) {
	return q.extreme(col, -1)
}

// Max returns the largest value of col over matching rows.
func (q *Query) Max(col string) (Value, error) {
	return q.extreme(col, 1)
}

// extreme keeps the value whose encoding compares to the best so
// far as sign. Values of mixed types cannot be ordered and are an error.
func (q *Query) extreme(col string, sign int) (Value, error) {
	var best []byte
	err := q.fold(col, func(_, b []byte) error {
		switch {
		case best == nil:
		case b[0] != best[0]:
			return fmt.Errorf("%w: %s mixes types", ErrType, col)
		case bytes.Compare(b, best) != sign:
			return nil
		}
		best = append(best[:0], b...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	if best == nil {
		return nil, ErrNotFound
	}
	return decode(best)
}

// fold calls fn with the key and encoded value of col for every
// matching row that has it. Both are only valid during the call.
func (q *Query) fold(col string, fn func(k, b []byte) error) error {
	if err := checkName(col); err != nil {
		return err
	}
	return q.scan(map[string]bool{col: true}, func(k []byte, cells []cell) error {
		return fn(k, cells[0].val)
	})
}

// seekAfter is how many keys a scan steps over before it seeks
// past the rest of a row instead.
const seekAfter = 8

// errStop ends a scan early without failing it.
var errStop = errors.New("stop")

// scan calls fn with the key of every matching row, up to the limit,
// and the cells of its columns in out; a nil out keeps every column.
// A row with none of the out columns is skipped and not counted, but
// an empty out keeps every row with no cells. Only condition and out
// columns are read. Arguments to fn are only
// valid during the call.
func (q *Query) scan(out map[string]bool, fn func(k []byte, cells []cell) error) error {
	if q.err != nil {
		return q.err
	}
	// need is every column to read; nil reads them all.
	var need map[string]bool
	if out != nil {
		need = maps.Clone(out)
		for _, cd := range q.conds {
			need[cd.col] = true
		}
	}
	tab := key(q.table)
	pre, to := []byte(q.prefix), []byte(q.to)
	start := append(append([]byte{}, tab...), max(q.prefix, q.from)...)
	n := 0

	err := q.s.db.View(func(txn *badger.Txn) error {
		opt := badger.DefaultIteratorOptions
		opt.PrefetchValues = false
		opt.Prefix = tab
		it := txn.NewIterator(opt)
		defer it.Close()

		var (
			cur   []byte                       // rowKey being read
			cells []cell                       // out cells of cur; buffers are reused
			hit   = make([]bool, len(q.conds)) // conditions whose cell was seen
			dead  bool                         // row has failed a condition
			got   int                          // needed columns seen in row
			skip  []byte                       // reused seek key
		)
		// flush hands a finished row to fn when it passes. Present
		// cells were tested as they were read; this settles absent ones.
		flush := func() error {
			if cur == nil || dead {
				return nil
			}
			for i, cd := range q.conds {
				if !hit[i] && !cd.holds(nil) {
					return nil
				}
			}
			if len(out) > 0 && len(cells) == 0 {
				return nil
			}
			if err := fn(cur, cells); err != nil {
				return err
			}
			n++
			if q.limit > 0 && n >= q.limit {
				return errStop
			}
			return nil
		}
		// read tests the conditions on cell b of column c, then keeps
		// a copy of it when c is an out column.
		read := func(c, b []byte) {
			for i, cd := range q.conds {
				if cd.col != string(c) {
					continue
				}
				hit[i] = true
				if !cd.holds(b) {
					dead = true
					return
				}
			}
			if out != nil && !out[string(c)] {
				return
			}
			if len(cells) < cap(cells) {
				cells = cells[:len(cells)+1]
			} else {
				cells = append(cells, cell{})
			}
			p := &cells[len(cells)-1]
			p.col = append(p.col[:0], c...)
			p.val = append(p.val[:0], b...)
		}
		// next moves past the rest of the current row. A Seek costs far
		// more than a step, so narrow rows are stepped over and only
		// wide rows are sought past; a row's keys end before sep+1.
		next := func(r []byte) {
			skip = append(append(append(skip[:0], tab...), r...), sep+1)
			for range seekAfter {
				it.Next()
				if !it.Valid() || bytes.Compare(it.Item().Key(), skip) >= 0 {
					return
				}
			}
			it.Seek(skip)
		}

		for it.Seek(start); it.Valid(); {
			// Keys under tab are row, sep, col.
			k := it.Item().Key()[len(tab):]
			i := bytes.IndexByte(k, sep)
			if i < 0 {
				return fmt.Errorf("%w: key %q", ErrCorrupt, k)
			}
			r, c := k[:i], k[i+1:]
			if !bytes.HasPrefix(r, pre) || (len(to) > 0 && bytes.Compare(r, to) >= 0) {
				break
			}
			if !bytes.Equal(r, cur) {
				if err := flush(); err != nil {
					return err
				}
				cur = append(cur[:0], r...)
				cells, dead, got = cells[:0], false, 0
				clear(hit)
			}

			if need == nil || need[string(c)] {
				err := it.Item().Value(func(b []byte) error {
					read(c, b)
					return nil
				})
				if err != nil {
					return err
				}
				if dead {
					next(r)
					continue
				}
				got++
			}
			// Once every needed column is in, the rest of the row is irrelevant.
			if need != nil && got == len(need) {
				next(r)
				continue
			}
			it.Next()
		}
		return flush()
	})
	if errors.Is(err, errStop) {
		return nil
	}
	return err
}
