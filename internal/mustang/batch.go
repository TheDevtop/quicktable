package mustang

/*
	Batches apply writes atomically in one Badger transaction, in the
	order they were added. Large batches fail with badger.ErrTxnTooBig.
	All writes in Mustang go through a batch.
*/

import (
	"errors"

	"github.com/dgraph-io/badger/v4"
)

const (
	opSet = iota
	opDel
	opDrop // delete every key under key, limited to column col when set
)

type op struct {
	kind int
	key  []byte
	val  []byte
	col  string
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
			item, err := txn.Get(key(c.Table, c.Row, c.Col))
			if errors.Is(err, badger.ErrKeyNotFound) {
				continue
			}
			if err != nil {
				return err
			}
			if out[i], err = value(item); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
