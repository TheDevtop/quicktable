package engine

/*
	Quicktable
	Database engine
*/

import (
	"strings"

	"github.com/TheDevtop/quicktable/pkg/logwrap"
	badger "github.com/dgraph-io/badger/v4"
)

// The instance of the database engine
var enginePtr *badger.DB

// Start database engine
func Start(path string, logger *logwrap.Logger) error {
	var (
		opts badger.Options
		err  error
	)

	if path == "" {
		opts = badger.DefaultOptions("").WithInMemory(true)
		logger.Warnf("%s not specified, running in memory\n", path)
	} else {
		opts = badger.DefaultOptions(path)
		logger.Infof("Running at %s\n", path)
	}

	opts = opts.WithLogger(logger)

	if enginePtr, err = badger.Open(opts); err != nil {
		return err
	}
	return nil
}

// Stop database engine
func Stop() error {
	return enginePtr.Close()
}

// Lookup a specific key
func IndexExact(key string) (string, error) {
	var err = enginePtr.View(func(txn *badger.Txn) error {
		if item, err := txn.Get([]byte(key)); err != nil {
			return err
		} else {
			key = string(item.Key())
			return nil
		}
	})
	return key, err
}

// Lookup a prefix
func IndexPrefix(prefix string) map[string]struct{} {
	var keyMap = make(map[string]struct{})

	enginePtr.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = false

		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Seek([]byte(prefix)); it.ValidForPrefix([]byte(prefix)); it.Next() {
			keyMap[strings.TrimPrefix(string(it.Item().Key()), prefix)] = struct{}{}
		}
		return nil
	})
	return keyMap
}

// Query an exact key
func QueryExact(key string) (string, error) {
	var (
		buf []byte
		err error
	)

	err = enginePtr.View(func(txn *badger.Txn) error {
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

// Query a prefix
func QueryPrefix(prefix string) (map[string]string, error) {
	var (
		err   error
		pairs = make(map[string]string)
	)

	err = enginePtr.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = true

		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Seek([]byte(prefix)); it.ValidForPrefix([]byte(prefix)); it.Next() {
			item := it.Item()
			key := item.Key()
			val, _ := item.ValueCopy(nil)
			pairs[strings.TrimPrefix(string(key), prefix)] = string(val)
		}
		return nil
	})

	return pairs, err
}

// Insert an exact key/value combination
func InsertExact(key, value string) (string, error) {
	var err error
	err = enginePtr.Update(func(txn *badger.Txn) error {
		return txn.Set([]byte(key), []byte(value))
	})
	if err != nil {
		return "", err
	}
	return key, nil
}

// Insert a map of key/value combinations, based on prefix
func InsertPrefix(prefix string, pairs map[string]string) error {
	var err error
	err = enginePtr.Update(func(txn *badger.Txn) error {
		for key, val := range pairs {
			err = txn.Set([]byte(strings.Join([]string{prefix, key}, "")), []byte(val))
		}
		return err
	})
	return err
}

// Delete an exact key
func DeleteExact(key string) (string, error) {
	var err = enginePtr.Update(func(txn *badger.Txn) error {
		return txn.Delete([]byte(key))
	})
	if err != nil {
		return "", err
	}
	return key, nil
}

// Delete multiple keys, based on prefix
func DeletePrefix(prefix string) error {
	return enginePtr.DropPrefix([]byte(prefix))
}
