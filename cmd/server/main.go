package main

import (
	"context"
	"errors"
	"flag"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/charmbracelet/log"
	"github.com/dgraph-io/badger/v4"

	"github.com/TheDevtop/quicktable/internal/mustang"
)

// charmLogger adds Warningf to Charm's logger so it also serves as a badger.Logger.
type charmLogger struct {
	*log.Logger
}

func (l charmLogger) Warningf(format string, args ...any) {
	l.Warnf(format, args...)
}

func main() {
	if err := run(); err != nil {
		os.Exit(1)
	}
}

// run serves until a signal or a server error; returning lets the deferred
// closes run before main exits.
func run() error {
	addr := flag.String("a", ":16000", "Server listen address")
	dataDir := flag.String("d", "./data", "Storage directory")
	inMemory := flag.Bool("m", false, "Run database completely in-memory")
	debug := flag.Bool("v", false, "Enable debug level logging")
	token := flag.String("t", "", "Bearer token")
	flag.Parse()

	// 1. Setup one logger for the server and Badger
	logger := charmLogger{log.NewWithOptions(os.Stderr, log.Options{
		ReportTimestamp: true,
		TimeFormat:      "2006-01-02 15:04:05",
	})}
	logger.SetLevel(log.InfoLevel)
	if *debug {
		logger.SetLevel(log.DebugLevel)
	}

	if *token == "" {
		logger.Warn("bearer token not set, this is insecure")
	}

	logger.Info("starting quicktable server", "addr", *addr, "inMemory", *inMemory, "dir", *dataDir)

	// 2. Open the Mustang store with the same logger instance
	opt := badger.DefaultOptions(*dataDir)
	if *inMemory {
		opt = badger.DefaultOptions("").WithInMemory(true)
	}
	s, err := mustang.Open(opt.WithLogger(logger))
	if err != nil {
		logger.Error("failed to open mustang store", "err", err)
		return err
	}
	defer func() {
		if err := s.Close(); err != nil {
			logger.Error("error closing database", "err", err)
		} else {
			logger.Info("database closed cleanly")
		}
	}()

	// 3. Register HTTP Routes (REST + MCP)
	mux := http.NewServeMux()
	RegisterRESTRoutes(mux, s)
	RegisterMCPRoutes(mux, s)

	srv := &http.Server{
		Addr:         *addr,
		Handler:      requireToken(*token, mux),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// 4. Start HTTP Server in background
	serverErrCh := make(chan error, 1)
	go func() {
		logger.Info("HTTP server listening", "addr", *addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErrCh <- err
		}
	}()

	// 5. Graceful shutdown on SIGINT / SIGTERM
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	var serveErr error
	select {
	case sig := <-sigCh:
		logger.Info("shutdown signal received", "signal", sig.String())
	case serveErr = <-serverErrCh:
		logger.Error("server error", "err", serveErr)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		logger.Error("server shutdown forced", "err", err)
	} else {
		logger.Info("server exited gracefully")
	}
	return serveErr
}
