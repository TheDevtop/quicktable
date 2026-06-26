package engine

/*
	Quicktable
	Database engine
*/

import (
	"github.com/TheDevtop/quicktable/pkg/logwrap"
	badger "github.com/dgraph-io/badger/v4"
)

var enginePtr *badger.DB

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

func Stop() error {
	return enginePtr.Close()
}
