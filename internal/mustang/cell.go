package mustang

import (
	"bytes"

	"github.com/dgraph-io/badger/v4"
)

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

func value(item *badger.Item) (Value, error) {
	var v Value
	err := item.Value(func(b []byte) error {
		var err error
		v, err = decode(b)
		return err
	})
	return v, err
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

// Upsert table
func (s *Store) UpsertTable(t string, tab Table) error {
	b := s.Batch()
	for r, row := range tab {
		for c, v := range row {
			b.Set(t, r, c, v)
		}
	}
	return b.Commit()
}

// Get table
func (s *Store) GetTable(t string) (Table, error) {
	if err := checkName(t); err != nil {
		return nil, err
	}
	tab := Table{}
	err := s.db.View(func(txn *badger.Txn) error {
		var row Row
		var last []byte
		return iter(txn, key(t), true, func(it *badger.Iterator) error {
			k := it.Item().Key()
			r := field(k, lvlRow)
			// Keys are sorted, so a row's cells are contiguous.
			if !bytes.Equal(r, last) {
				row = Row{}
				tab[string(r)] = row
				last = append(last[:0], r...)
			}
			v, err := value(it.Item())
			if err != nil {
				return err
			}
			row[string(field(k, lvlCol))] = v
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	if len(tab) == 0 {
		return nil, ErrNotFound
	}
	return tab, nil
}

// Delete table
func (s *Store) DeleteTable(t string) error {
	return s.Batch().DeleteTable(t).Commit()
}

// Scan rowKey
func (s *Store) ScanRows(t string) ([]string, error) {
	if err := checkName(t); err != nil {
		return nil, err
	}
	return s.names(key(t), lvlRow)
}

// Upsert rowKey
func (s *Store) UpsertRow(t, r string, row Row) error {
	b := s.Batch()
	for c, v := range row {
		b.Set(t, r, c, v)
	}
	return b.Commit()
}

// Get rowKey
func (s *Store) GetRow(t, r string) (Row, error) {
	if err := checkNames(t, r); err != nil {
		return nil, err
	}
	row := Row{}
	err := s.db.View(func(txn *badger.Txn) error {
		return iter(txn, key(t, r), true, func(it *badger.Iterator) error {
			v, err := value(it.Item())
			if err != nil {
				return err
			}
			row[string(field(it.Item().Key(), lvlCol))] = v
			return nil
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

// Delete rowKey
func (s *Store) DeleteRow(t, r string) error {
	return s.Batch().DeleteRow(t, r).Commit()
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

// Upsert column; vals maps rowKey -> value.
func (s *Store) UpsertColumn(t, c string, vals map[string]Value) error {
	b := s.Batch()
	for r, v := range vals {
		b.Set(t, r, c, v)
	}
	return b.Commit()
}

// Get column; returns rowKey -> value for rows that have the column.
// Keys are filtered before values are read, so only matches cost a value fetch.
func (s *Store) GetColumn(t, c string) (map[string]Value, error) {
	if err := checkNames(t, c); err != nil {
		return nil, err
	}
	out := map[string]Value{}
	err := s.db.View(func(txn *badger.Txn) error {
		return iter(txn, key(t), false, func(it *badger.Iterator) error {
			k := it.Item().Key()
			if string(field(k, lvlCol)) != c {
				return nil
			}
			v, err := value(it.Item())
			if err != nil {
				return err
			}
			out[string(field(k, lvlRow))] = v
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, ErrNotFound
	}
	return out, nil
}

// Delete column
func (s *Store) DeleteColumn(t, c string) error {
	return s.Batch().DeleteColumn(t, c).Commit()
}

// Upsert value
func (s *Store) UpsertValue(t, r, c string, v Value) error {
	return s.Batch().Set(t, r, c, v).Commit()
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

// Delete value
func (s *Store) DeleteValue(t, r, c string) error {
	return s.Batch().Delete(t, r, c).Commit()
}
