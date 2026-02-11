package server

import (
	"crypto/rand"
	"fmt"

	"github.com/TheDevtop/quicktable/pkg/dkey"
	"github.com/dgraph-io/badger/v4"
)

// Locate an existing key
func Index(dbPtr *badger.DB, key string) (string, error) {
	var err = dbPtr.View(func(txn *badger.Txn) error {
		if item, err := txn.Get([]byte(key)); err != nil {
			return err
		} else {
			key = string(item.Key())
			return nil
		}
	})
	return key, err
}

// Locate a set of keys by prefix
func IndexPrefixed(dbPtr *badger.DB, key string) []string {
	var keyList = make([]string, 0, 64)
	dbPtr.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = false

		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Seek([]byte(key)); it.ValidForPrefix([]byte(key)); it.Next() {
			keyList = append(keyList, dkey.Strip(key, string(it.Item().Key())))
		}
		return nil
	})
	return keyList
}

// Insert a key/value pair
func Insert(dbPtr *badger.DB, key, value string) (string, error) {
	var err error
	err = dbPtr.Update(func(txn *badger.Txn) error {
		return txn.Set([]byte(key), []byte(value))
	})
	if err != nil {
		return "", err
	}
	return key, nil
}

// Insert prefiltered key/value pairs via prefix
func InsertPrefiltered(dbPtr *badger.DB, prefix string, pairs map[string]string) error {
	var err error
	err = dbPtr.Update(func(txn *badger.Txn) error {
		var err error
		for key, val := range pairs {
			err = txn.Set([]byte(dkey.Fuse(prefix, key)), []byte(val))
		}
		return err
	})

	if err != nil {
		return err
	}
	return nil
}

// Copy an existing value to a new key
func Copy(dbPtr *badger.DB, srcKey string, destKey string) (string, error) {
	err := dbPtr.Update(func(txn *badger.Txn) error {
		var (
			item *badger.Item
			buf  []byte
			err  error
		)
		if item, err = txn.Get([]byte(srcKey)); err != nil {
			return err
		}
		if buf, err = item.ValueCopy(nil); err != nil {
			return err
		}
		if err = txn.Set([]byte(destKey), buf); err != nil {
			return err
		}
		return nil
	})
	return destKey, err
}

// Move an existing value to a new key
func Move(dbPtr *badger.DB, srcKey string, destKey string) (string, error) {
	err := dbPtr.Update(func(txn *badger.Txn) error {
		var (
			item *badger.Item
			buf  []byte
			err  error
		)
		if item, err = txn.Get([]byte(srcKey)); err != nil {
			return err
		}
		if buf, err = item.ValueCopy(nil); err != nil {
			return err
		}
		if err = txn.Set([]byte(destKey), buf); err != nil {
			return err
		}
		if err = txn.Delete([]byte(srcKey)); err != nil {
			return err
		}
		return nil
	})
	return destKey, err
}

// Delete a key/value pair
func Delete(dbPtr *badger.DB, key string) (string, error) {
	var err = dbPtr.Update(func(txn *badger.Txn) error {
		return txn.Delete([]byte(key))
	})
	if err != nil {
		return "", err
	}
	return key, nil
}

// Delete prefiltered key/value pairs via prefix
func DeletePrefiltered(dbPtr *badger.DB, prefix string, keys []string) error {
	var err = dbPtr.Update(func(txn *badger.Txn) error {
		var err error
		for _, key := range keys {
			err = txn.Delete([]byte(dkey.Fuse(prefix, key)))
		}
		return err
	})
	return err
}

// Delete key/value pairs by prefix
func DeletePrefixed(dbPtr *badger.DB, key string) error {
	return dbPtr.DropPrefix([]byte(key))
}

// Query for key/value pair
func Query(dbPtr *badger.DB, key string) (string, error) {
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
		return "", err
	}

	return string(buf), nil
}

// Query prefiltered key/value pairs via prefix
func QueryPrefiltered(dbPtr *badger.DB, prefix string, keys []string) (map[string]string, error) {
	var (
		err   error
		pairs = make(map[string]string, len(keys))
	)

	err = dbPtr.View(func(txn *badger.Txn) error {
		var (
			buf []byte
			err error
		)
		for _, key := range keys {
			item, err := txn.Get([]byte(dkey.Fuse(prefix, key)))
			if err != nil {
				pairs[key] = ""
				continue
			}
			buf, err = item.ValueCopy(nil)
			pairs[key] = string(buf)
		}
		return err
	})

	return pairs, err
}

// Query key/value pairs via prefix
func QueryPrefixed(dbPtr *badger.DB, prefix string) (map[string]string, error) {
	var (
		err   error
		pairs = make(map[string]string)
	)

	err = dbPtr.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = true

		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Seek([]byte(prefix)); it.ValidForPrefix([]byte(prefix)); it.Next() {
			item := it.Item()
			key := item.Key()
			val, _ := item.ValueCopy(nil)
			pairs[dkey.Strip(prefix, string(key))] = string(val)
		}
		return nil
	})

	return pairs, err
}

// Generate a random hashed key
func GenerateHash(key string) (string, error) {
	var (
		charBuf = make([]byte, 8)
		err     error
	)
	if _, err = rand.Read(charBuf); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s:%x", key, charBuf[:]), nil
}
