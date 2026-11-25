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

	"github.com/TheDevtop/quicktable/pkg/logwrap"
	"github.com/TheDevtop/quicktable/pkg/qte"
)

func main() {
	if err := run(); err != nil {
		os.Exit(1)
	}
}

// run serves until a signal or a server error; returning lets the deferred
// closes run before main exits.
func run() error {
	addr := flag.String("addr", ":16000", "Server listen address")
	dataDir := flag.String("dir", "./data", "Storage directory")
	inMemory := flag.Bool("in-memory", false, "Run database completely in-memory")
	debug := flag.Bool("debug", false, "Enable debug level logging")
	flag.Parse()

	// 1. Setup unified logwrap Logger (wraps charm.sh logger and implements badger.Logger)
	logger := logwrap.NewWithOptions(os.Stderr, log.Options{
		ReportTimestamp: true,
		TimeFormat:      "2006-01-02 15:04:05",
	})
	if *debug {
		logger.SetLevel(log.DebugLevel)
	} else {
		logger.SetLevel(log.InfoLevel)
	}

	logger.Info("starting quicktable server", "addr", *addr, "inMemory", *inMemory, "dir", *dataDir)

	// 2. Open QTE Database with the same logger instance
	db, err := qte.Open(qte.Options{
		Dir:      *dataDir,
		InMemory: *inMemory,
		Logger:   logger,
	})
	if err != nil {
		logger.Error("failed to open wide-column engine", "err", err)
		return err
	}
	defer func() {
		if err := db.Close(); err != nil {
			logger.Error("error closing database", "err", err)
		} else {
			logger.Info("database closed cleanly")
		}
	}()

	// 3. Register HTTP Routes (REST + MCP)
	mux := http.NewServeMux()
	RegisterRESTRoutes(mux, db)
	mcpHandler := RegisterMCPRoutes(mux, db)

	srv := &http.Server{
		Addr:         *addr,
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}
	// SSE streams never go idle, so end them or Shutdown waits for its full timeout.
	srv.RegisterOnShutdown(mcpHandler.Close)

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
