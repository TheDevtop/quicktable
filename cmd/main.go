package main

import (
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	badger "github.com/dgraph-io/badger/v4"
)

// Environment variables
const (
	envDefaultPath = "QTAB_PATH"
	envDefaultAddr = "QTAB_ADDR"
)

var (
	dbPtr  *badger.DB
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
		log.Printf("%s not specified, running in memory\n", envDefaultPath)
	} else {
		opts = badger.DefaultOptions(path)
		log.Printf("Running at %s\n", path)
	}

	if dbPtr, err = badger.Open(opts); err != nil {
		log.Fatalf("Fatal error (%s)\n", err)
	}
}

// Starts and configures HTTP server
func startServer() {
	var err error
	if srvPtr, err = net.Listen("tcp", os.Getenv(envDefaultAddr)); err != nil {
		log.Printf("Can't serve on %s (%s)\n", os.Getenv(envDefaultAddr), err)
	}
	for route, fn := range apiTable {
		http.Handle(route, fn)
	}
	log.Printf("Serving on %s\n", os.Getenv(envDefaultAddr))
}

// Signal handler and shutdown function
func sigHandler() {
	var (
		ch  = make(chan os.Signal, 1)
		err error
	)

	signal.Notify(ch, syscall.SIGTERM, syscall.SIGINT, syscall.SIGQUIT)
	<-ch

	log.Println("Caught signal, halting system")
	if err = srvPtr.Close(); err != nil {
		log.Printf("Error (%s)\n", err)
	}
	if err = dbPtr.Close(); err != nil {
		log.Fatalf("Fatal error (%s)\n", err)
	}
	log.Println("Halted!")
	os.Exit(0)
}

func main() {
	log.Println("\033[97;1mQuicktable\033[0m")
	startDatabase()
	startServer()
	go func() {
		if err := http.Serve(srvPtr, nil); err != nil {
			log.Println(err)
		}
	}()
	sigHandler()
}
