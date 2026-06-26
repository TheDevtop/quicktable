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

	"github.com/TheDevtop/quicktable/internal/engine"
	"github.com/TheDevtop/quicktable/pkg/api"
	"github.com/TheDevtop/quicktable/pkg/logwrap"
)

// Environment variables
const (
	envPath = "DIR"
	envAddr = "ADDR"
)

var (
	logPtr *logwrap.Logger
	srvPtr net.Listener
)

// Starts and configures HTTP server
func startServer() {
	var err error
	if srvPtr, err = net.Listen("tcp", os.Getenv(envAddr)); err != nil {
		logPtr.Errorf("Can't serve on %s (%s)\n", os.Getenv(envAddr), err)
	}

	// Bind api functions to routes
	http.HandleFunc(api.RouteHealth, apiHealth)

	http.HandleFunc(api.RouteIndexExact, apiIndexExact)
	http.HandleFunc(api.RouteIndexPrefix, apiIndexPrefix)

	http.HandleFunc(api.RouteQueryExact, apiQueryExact)
	http.HandleFunc(api.RouteQueryPrefix, apiQueryPrefix)

	http.HandleFunc(api.RouteInsertExact, apiInsertExact)
	http.HandleFunc(api.RouteInsertPrefix, apiInsertPrefix)

	http.HandleFunc(api.RouteDeleteExact, apiDeleteExact)
	http.HandleFunc(api.RouteDeletePrefix, apiDeletePrefix)

	logPtr.Infof("Serving on %s\n", os.Getenv(envAddr))
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
	if err = engine.Stop(); err != nil {
		logPtr.Fatalf("Fatal error (%s)\n", err)
	}
	logPtr.Infof("Stopped Quicktable\n")
	os.Exit(0)
}

// Program entrypoint
func main() {
	logPtr = logwrap.NewLogger()
	logPtr.Print("Quicktable")

	// Start the database engine
	if err := engine.Start(os.Getenv(envPath), logPtr); err != nil {
		logPtr.Fatal("Could not start engine", "err", err)
	}

	startServer()
	go func() {
		if err := http.Serve(srvPtr, nil); err != nil {
			logPtr.Error(err)
		}
	}()
	sigHandler()
}
