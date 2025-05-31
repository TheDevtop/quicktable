package server

import (
	"crypto/rand"
	"errors"
	"fmt"

	"github.com/TheDevtop/quicktable/pkg/shared/core"
	"github.com/dgraph-io/badger/v4"
)

func Index(dbPtr *badger.DB, key core.Key) (core.Key, error) {
	var err = dbPtr.View(func(txn *badger.Txn) error {
		if item, err := txn.Get([]byte(key)); err != nil {
			return err
		} else {
			key = core.Key(item.Key())
			return nil
		}
	})
	return key, err
}

func IndexRanged(dbPtr *badger.DB, key core.Key) []core.Key {
	var keyList = make([]core.Key, 0, 64)
	dbPtr.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = false

		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Seek([]byte(key)); it.ValidForPrefix([]byte(key)); it.Next() {
			keyList = append(keyList, core.Key(it.Item().Key()))
		}
		return nil
	})
	return keyList
}

func Insert(dbPtr *badger.DB, key core.Key, values core.List) (core.Key, error) {
	var (
		buf []byte
		err error
	)
	if buf, err = encodeList(values); err != nil {
		return "", err
	}
	err = dbPtr.Update(func(txn *badger.Txn) error {
		return txn.Set([]byte(key), buf)
	})
	if err != nil {
		return "", err
	}
	return key, nil
}

func InsertRanged(dbPtr *badger.DB, key core.Key, values core.List) ([]core.Key, error) {
	var (
		keyList = make([]core.Key, 0, 64)
		buf     []byte
		err     error
	)
	if buf, err = encodeList(values); err != nil {
		return nil, err
	}

	err = dbPtr.Update(func(txn *badger.Txn) error {
		var (
			opts = badger.DefaultIteratorOptions
			it   *badger.Iterator
			err  error
		)
		opts.PrefetchValues = false
		it = txn.NewIterator(opts)

		for it.Seek([]byte(key)); it.ValidForPrefix([]byte(key)); it.Next() {
			keyList = append(keyList, core.Key(it.Item().Key()))
		}
		it.Close()

		for _, k := range keyList {
			err = txn.Set([]byte(k), buf)
		}
		return err
	})

	return keyList, err
}

func Append(dbPtr *badger.DB, key core.Key, values core.List) (core.Key, error) {
	err := dbPtr.Update(func(txn *badger.Txn) error {
		var (
			item      *badger.Item
			oldValues core.List
			buf       []byte
			err       error
		)
		if item, err = txn.Get([]byte(key)); err != nil {
			return err
		}
		key = core.Key(item.Key())
		if buf, err = item.ValueCopy(nil); err != nil {
			return err
		}
		if oldValues, err = decodeList(buf); err != nil {
			return err
		}
		values = append(oldValues, values...)
		if buf, err = encodeList(values); err != nil {
			return err
		}
		if err = txn.Set([]byte(key), buf); err != nil {
			return err
		}
		return nil
	})
	return key, err
}

func Move(dbPtr *badger.DB, oldKey core.Key, newKey core.Key) (core.Key, error) {
	err := dbPtr.Update(func(txn *badger.Txn) error {
		var (
			item *badger.Item
			buf  []byte
			err  error
		)
		if item, err = txn.Get([]byte(oldKey)); err != nil {
			return err
		}
		if buf, err = item.ValueCopy(nil); err != nil {
			return err
		}
		if err = txn.Set([]byte(newKey), buf); err != nil {
			return err
		}
		if err = txn.Delete([]byte(oldKey)); err != nil {
			return err
		}
		return nil
	})
	return newKey, err
}

func Delete(dbPtr *badger.DB, key core.Key) (core.Key, error) {
	var err = dbPtr.Update(func(txn *badger.Txn) error {
		return txn.Delete([]byte(key))
	})
	if err != nil {
		return "", err
	}
	return key, nil
}

func DeleteRanged(dbPtr *badger.DB, key core.Key) error {
	return dbPtr.DropPrefix([]byte(key))
}

func Query(dbPtr *badger.DB, key core.Key) (core.List, error) {
	var (
		buf []byte
		err error
	)

	err = dbPtr.View(func(txn *badger.Txn) error {
		var (
			item *badger.Item
			err  error
		)
		if item, err = txn.Get([]byte(key)); err != nil {
			return err
		}
		if buf, err = item.ValueCopy(nil); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return decodeList(buf)
}

func QueryRanged(dbPtr *badger.DB, key core.Key) (core.Pair, error) {
	var (
		bufList [][]byte   = make([][]byte, 0, 64)
		keyList []core.Key = make([]core.Key, 0, 64)
		err     error
		list    core.List
		pair    = make(core.Pair)
	)

	dbPtr.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = true

		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Seek([]byte(key)); it.ValidForPrefix([]byte(key)); it.Next() {
			item := it.Item()
			keyList = append(keyList, core.Key(item.Key()))
			buf, _ := item.ValueCopy(nil)
			bufList = append(bufList, buf)
		}
		return nil
	})

	if len(keyList) != len(bufList) {
		return nil, errors.New("Mismatch in keys/values")
	}
	for i, k := range keyList {
		if list, err = decodeList(bufList[i]); err != nil {
			return nil, err
		}
		pair[k] = list
	}
	return pair, nil
}

func GenerateId(dbPtr *badger.DB, key core.Key) (core.Key, error) {
	var (
		id  uint64
		err error
		seq *badger.Sequence
	)
	if seq, err = dbPtr.GetSequence([]byte(key), 64); err != nil {
		return "", err
	}
	defer seq.Release()
	if id, err = seq.Next(); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s:%05d", key, id), nil
}

func GenerateHash(key core.Key) (core.Key, error) {
	var (
		charBuf = make([]byte, 8)
		err     error
	)
	if _, err = rand.Read(charBuf); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s:%x", key, charBuf[:]), nil
}
