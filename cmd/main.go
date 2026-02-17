package main

/*
	Quicktable
	Program entrypoint
*/

import (
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/TheDevtop/quicktable/pkg/logwrap"
	"github.com/TheDevtop/quicktable/pkg/shared"
	badger "github.com/dgraph-io/badger/v4"
)

// Environment variables
const (
	envDefaultPath = "QT_PATH"
	envDefaultAddr = "QT_ADDR"
)

var (
	dbPtr  *badger.DB
	logPtr *logwrap.Logger
	srvPtr net.Listener
)

// Starts and configures database
func startDatabase() {
	var (
		path = os.Getenv(envDefaultPath)
		opts badger.Options
		err  error
	)

	if path == "" {
		opts = badger.DefaultOptions("").WithInMemory(true)
		logPtr.Warnf("%s not specified, running in memory\n", envDefaultPath)
	} else {
		opts = badger.DefaultOptions(path)
		logPtr.Infof("Running at %s\n", path)
	}

	opts = opts.WithLogger(logPtr)

	if dbPtr, err = badger.Open(opts); err != nil {
		logPtr.Fatalf("Fatal error (%s)\n", err)
	}
}

// Starts and configures HTTP server
func startServer() {
	var err error
	if srvPtr, err = net.Listen("tcp", os.Getenv(envDefaultAddr)); err != nil {
		logPtr.Errorf("Can't serve on %s (%s)\n", os.Getenv(envDefaultAddr), err)
	}

	http.HandleFunc(shared.RouteHealth, apiHealth)
	http.HandleFunc(shared.RouteQuery, apiQuery)
	http.HandleFunc(shared.RouteHash, apiHash)

	logPtr.Infof("Serving on %s\n", os.Getenv(envDefaultAddr))
}

// Signal handler and shutdown function
func sigHandler() {
	var (
		ch  = make(chan os.Signal, 1)
		err error
	)

	signal.Notify(ch, syscall.SIGTERM, syscall.SIGINT, syscall.SIGQUIT)
	<-ch

	logPtr.Infof("Caught signal, halting system")
	if err = srvPtr.Close(); err != nil {
		logPtr.Errorf("Error (%s)\n", err)
	}
	if err = dbPtr.Close(); err != nil {
		logPtr.Fatalf("Fatal error (%s)\n", err)
	}
	logPtr.Infof("Stopped Quicktable\n")
	os.Exit(0)
}

// Program entrypoint
func main() {
	logPtr = logwrap.NewLogger()
	logPtr.Print("Quicktable")

	startDatabase()
	startServer()
	go func() {
		if err := http.Serve(srvPtr, nil); err != nil {
			logPtr.Error(err)
		}
	}()
	sigHandler()
}
