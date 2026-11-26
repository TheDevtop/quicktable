package mustang

/*
	The Mustang wide-column engine,
	optimized for read and query performance.

	Batches apply writes atomically in one Badger transaction, in the
	order they were added. Large batches fail with badger.ErrTxnTooBig.
	All writes in Mustang go through a batch.

	If adds a condition, checked in order with the writes: a failing
	condition aborts the batch with ErrCondition and nothing is written.
	When a checked cell is changed by another writer before Commit,
	Commit returns badger.ErrConflict; the caller decides whether to retry.
*/

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"time"

	"github.com/dgraph-io/badger/v4"
)

var ErrCondition = errors.New("mustang: condition failed")

// incrTries bounds Store.Incr retries on conflict; each retry waits a
// random, growing pause so contending writers spread out.
const incrTries = 32

const (
	opSet = iota
	opDel
	opDrop // delete every key under key, limited to column col when set
	opIf
	opIncr
)

type op struct {
	kind  int
	key   []byte
	val   []byte
	col   string
	cond  cond
	delta float64
}

type Batch struct {
	s   *Store
	ops []op
	err error
}

// Cell addresses one value, for GetValues.
type Cell struct {
	Table, Row, Col string
}

func (s *Store) Batch() *Batch {
	return &Batch{s: s}
}

// add records an op unless this or an earlier call failed;
// the first error is reported by Commit.
func (b *Batch) add(err error, o op) *Batch {
	if b.err == nil && err != nil {
		b.err = err
	}
	if b.err == nil {
		b.ops = append(b.ops, o)
	}
	return b
}

func (b *Batch) Set(t, r, c string, v Value) *Batch {
	val, err := encode(v)
	if err == nil {
		err = checkNames(t, r, c)
	}
	return b.add(err, op{kind: opSet, key: key(t, r, c), val: val})
}

func (b *Batch) Delete(t, r, c string) *Batch {
	return b.add(checkNames(t, r, c), op{kind: opDel, key: key(t, r, c)})
}

func (b *Batch) DeleteRow(t, r string) *Batch {
	return b.add(checkNames(t, r), op{kind: opDrop, key: key(t, r)})
}

func (b *Batch) DeleteColumn(t, c string) *Batch {
	return b.add(checkNames(t, c), op{kind: opDrop, key: key(t), col: c})
}

func (b *Batch) DeleteTable(t string) *Batch {
	return b.add(checkName(t), op{kind: opDrop, key: key(t)})
}

// If requires cell t/r/c to satisfy op val when the batch commits.
func (b *Batch) If(t, r, c string, o Op, val Value) *Batch {
	cd, err := newCond(c, o, val)
	if err == nil {
		err = checkNames(t, r)
	}
	return b.add(err, op{kind: opIf, key: key(t, r, c), cond: cd})
}

// Incr adds delta to the number in cell t/r/c; a missing cell counts as 0.
func (b *Batch) Incr(t, r, c string, delta float64) *Batch {
	return b.add(checkNames(t, r, c), op{kind: opIncr, key: key(t, r, c), delta: delta})
}

func (b *Batch) Commit() error {
	if b.err != nil {
		return b.err
	}
	return b.s.db.Update(func(txn *badger.Txn) error {
		for _, o := range b.ops {
			if err := apply(txn, o); err != nil {
				return err
			}
		}
		return nil
	})
}

func apply(txn *badger.Txn, o op) error {
	switch o.kind {
	case opSet:
		return txn.Set(o.key, o.val)
	case opDel:
		return txn.Delete(o.key)
	case opIf:
		v, ok, err := get(txn, o.key)
		if err != nil {
			return err
		}
		if !o.cond.holds(v, ok) {
			return fmt.Errorf("%w: %q %v", ErrCondition, o.key, o.cond.op)
		}
		return nil
	case opIncr:
		_, err := incr(txn, o.key, o.delta)
		return err
	}
	// Collect first: deleting while iterating would invalidate the iterator.
	var keys [][]byte
	err := iter(txn, o.key, false, func(it *badger.Iterator) error {
		k := it.Item().Key()
		if len(o.col) == 0 || string(field(k, lvlCol)) == o.col {
			keys = append(keys, it.Item().KeyCopy(nil))
		}
		return nil
	})
	if err != nil {
		return err
	}
	for _, k := range keys {
		if err := txn.Delete(k); err != nil {
			return err
		}
	}
	return nil
}

// get reads one cell; ok is false when it is absent.
func get(txn *badger.Txn, k []byte) (Value, bool, error) {
	item, err := txn.Get(k)
	if errors.Is(err, badger.ErrKeyNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	v, err := value(item)
	return v, err == nil, err
}

func incr(txn *badger.Txn, k []byte, delta float64) (float64, error) {
	v, ok, err := get(txn, k)
	if err != nil {
		return 0, err
	}
	n := 0.0
	if ok {
		f, isNum := v.(float64)
		if !isNum {
			return 0, fmt.Errorf("%w: counter %q is %T", ErrType, k, v)
		}
		n = f
	}
	n += delta
	b, err := encode(n)
	if err != nil {
		return 0, err
	}
	return n, txn.Set(k, b)
}

// Incr atomically adds delta to cell t/r/c and returns the new value.
// A conflicting concurrent write is retried, as an increment is always
// safe to redo.
func (s *Store) Incr(t, r, c string, delta float64) (float64, error) {
	if err := checkNames(t, r, c); err != nil {
		return 0, err
	}
	k := key(t, r, c)
	var n float64
	var err error
	for i := range incrTries {
		if i > 0 {
			time.Sleep(time.Duration(rand.IntN(1<<min(i, 10))) * time.Microsecond)
		}
		err = s.db.Update(func(txn *badger.Txn) error {
			var e error
			n, e = incr(txn, k, delta)
			return e
		})
		if !errors.Is(err, badger.ErrConflict) {
			return n, err
		}
	}
	return 0, err
}

// GetValues reads many cells from one consistent snapshot. The result
// lines up with cells; missing cells are nil.
func (s *Store) GetValues(cells []Cell) ([]Value, error) {
	for _, c := range cells {
		if err := checkNames(c.Table, c.Row, c.Col); err != nil {
			return nil, err
		}
	}
	out := make([]Value, len(cells))
	err := s.db.View(func(txn *badger.Txn) error {
		for i, c := range cells {
			v, _, err := get(txn, key(c.Table, c.Row, c.Col))
			if err != nil {
				return err
			}
			out[i] = v
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
