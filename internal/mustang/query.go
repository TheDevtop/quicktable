package mustang

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"time"

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
	Prefix  // string value starts with val
	Exists  // cell is present; val is ignored
	Missing // cell is absent; val is ignored
)

// cond is a test on one column, shared by queries and batches.
type cond struct {
	col string
	op  Op
	val Value
}

func newCond(col string, op Op, val Value) (cond, error) {
	if err := checkName(col); err != nil {
		return cond{}, err
	}
	switch {
	case op < Eq || op > Missing:
		return cond{}, fmt.Errorf("mustang: unknown op %d", op)
	case op == Exists || op == Missing:
		return cond{col, op, nil}, nil
	}
	if _, ok := val.(string); op == Prefix && !ok {
		return cond{}, fmt.Errorf("%w: prefix needs a string", ErrType)
	}
	if _, err := encode(val); err != nil {
		return cond{}, err
	}
	return cond{col, op, val}, nil
}

// holds reports whether the condition is met by a cell that is
// present with value v, or absent. Only Missing matches an absent cell.
func (c cond) holds(v Value, present bool) bool {
	if c.op == Missing || !present {
		return c.op == Missing && !present
	}
	if c.op == Exists {
		return true
	}
	if c.op == Prefix {
		s, ok := v.(string)
		return ok && strings.HasPrefix(s, c.val.(string))
	}
	n, ok := compare(v, c.val)
	if !ok {
		return false
	}
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

// compare orders a and b; ok is false when they differ in type.
// Bools order false before true.
func compare(a, b Value) (int, bool) {
	switch a := a.(type) {
	case string:
		b, ok := b.(string)
		return strings.Compare(a, b), ok
	case float64:
		b, ok := b.(float64)
		switch {
		case !ok:
			return 0, false
		case a < b:
			return -1, true
		case a > b:
			return 1, true
		}
		return 0, true
	case time.Time:
		b, ok := b.(time.Time)
		return a.Compare(b), ok
	case bool:
		b, ok := b.(bool)
		switch {
		case !ok:
			return 0, false
		case a == b:
			return 0, true
		case b:
			return -1, true
		}
		return 1, true
	}
	return 0, false
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
	cols     []string
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
func (q *Query) Select(cols ...string) *Query {
	if err := checkNames(cols...); err != nil {
		return q.fail(err)
	}
	q.cols = append(q.cols, cols...)
	return q
}

// Limit stops after n matching rows; 0 means no limit.
func (q *Query) Limit(n int) *Query {
	q.limit = n
	return q
}

// need returns the set of columns whose values must be read: the
// condition columns plus cols.
func (q *Query) need(cols ...string) map[string]bool {
	need := map[string]bool{}
	for _, c := range cols {
		need[c] = true
	}
	for _, c := range q.conds {
		need[c.col] = true
	}
	return need
}

func (q *Query) Run() ([]Result, error) {
	// Without Select every column is returned, so every value is read.
	var need map[string]bool
	if len(q.cols) > 0 {
		need = q.need(q.cols...)
	}
	var out []Result
	err := q.scan(need, func(k []byte, row Row) error {
		out = append(out, Result{string(k), q.project(row)})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (q *Query) project(row Row) Row {
	if len(q.cols) == 0 {
		return row
	}
	out := make(Row, len(q.cols))
	for _, c := range q.cols {
		if v, ok := row[c]; ok {
			out[c] = v
		}
	}
	return out
}

// Count returns the number of matching rows. Without conditions it
// reads keys only.
func (q *Query) Count() (int, error) {
	n := 0
	err := q.scan(q.need(), func([]byte, Row) error {
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
	err := q.fold(col, func(v Value) error {
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

// extreme keeps the value v for which compare(v, best) == sign.
// Values of mixed types cannot be ordered and are an error.
func (q *Query) extreme(col string, sign int) (Value, error) {
	var best Value
	err := q.fold(col, func(v Value) error {
		if best == nil {
			best = v
			return nil
		}
		n, ok := compare(v, best)
		if !ok {
			return fmt.Errorf("%w: %s mixes %T and %T", ErrType, col, v, best)
		}
		if n == sign {
			best = v
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if best == nil {
		return nil, ErrNotFound
	}
	return best, nil
}

// fold calls fn with the value of col for every matching row that has it.
func (q *Query) fold(col string, fn func(Value) error) error {
	if err := checkName(col); err != nil {
		return err
	}
	return q.scan(q.need(col), func(_ []byte, row Row) error {
		v, ok := row[col]
		if !ok {
			return nil
		}
		return fn(v)
	})
}

// errStop ends a scan early without failing it.
var errStop = errors.New("stop")

// scan calls fn with the key and needed columns of every matching row,
// up to the limit. A nil need reads every column; an empty one reads
// keys only. The key passed to fn is only valid during the call.
func (q *Query) scan(need map[string]bool, fn func(k []byte, row Row) error) error {
	if q.err != nil {
		return q.err
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
			cur  []byte // rowKey being read
			row  Row    // nil once the row has failed a condition
			got  int    // needed columns seen in row
			skip []byte // reused seek key
		)
		// flush hands a finished row to fn when it passes.
		flush := func() error {
			if row == nil || !q.complete(row) {
				return nil
			}
			if err := fn(cur, row); err != nil {
				return err
			}
			n++
			if q.limit > 0 && n >= q.limit {
				return errStop
			}
			return nil
		}
		// next seeks past the rest of row r; its keys end before sep+1.
		next := func(r []byte) {
			skip = append(append(append(skip[:0], tab...), r...), sep+1)
			it.Seek(skip)
		}

		for it.Seek(start); it.Valid(); {
			k := it.Item().Key()
			r := field(k, lvlRow)
			if !bytes.HasPrefix(r, pre) || (len(to) > 0 && bytes.Compare(r, to) >= 0) {
				break
			}
			if !bytes.Equal(r, cur) {
				if err := flush(); err != nil {
					return err
				}
				cur = append(cur[:0], r...)
				row, got = Row{}, 0
			}

			c := field(k, lvlCol)
			if need == nil || need[string(c)] {
				v, err := value(it.Item())
				if err != nil {
					return err
				}
				if !q.test(c, v) {
					row = nil
					next(r)
					continue
				}
				row[string(c)] = v
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

// test checks every condition on column c against its present value v.
func (q *Query) test(c []byte, v Value) bool {
	for _, cd := range q.conds {
		if cd.col == string(c) && !cd.holds(v, true) {
			return false
		}
	}
	return true
}

// complete checks conditions on the row as a whole; present values
// were already tested, so this settles absent cells.
func (q *Query) complete(row Row) bool {
	for _, cd := range q.conds {
		if _, ok := row[cd.col]; !ok && !cd.holds(nil, false) {
			return false
		}
	}
	return true
}
