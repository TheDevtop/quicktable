package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/dgraph-io/badger/v4"

	"github.com/TheDevtop/quicktable/internal/mustang"
)

// badgerLogger routes Badger's printf-style logging to the default slog logger.
type badgerLogger struct{}

func (badgerLogger) Errorf(format string, args ...any)   { slog.Error(msg(format, args)) }
func (badgerLogger) Warningf(format string, args ...any) { slog.Warn(msg(format, args)) }
func (badgerLogger) Infof(format string, args ...any)    { slog.Info(msg(format, args)) }
func (badgerLogger) Debugf(format string, args ...any)   { slog.Debug(msg(format, args)) }

func msg(format string, args []any) string {
	return strings.TrimRight(fmt.Sprintf(format, args...), "\n")
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
	level := slog.LevelInfo
	if *debug {
		level = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})))

	if *token == "" {
		slog.Warn("bearer token not set, this is insecure")
	}

	slog.Info("starting quicktable server", "addr", *addr, "inMemory", *inMemory, "dir", *dataDir)

	// 2. Open the Mustang store, logging through slog
	opt := badger.DefaultOptions(*dataDir)
	if *inMemory {
		opt = badger.DefaultOptions("").WithInMemory(true)
	}
	s, err := mustang.Open(opt.WithLogger(badgerLogger{}))
	if err != nil {
		slog.Error("failed to open mustang store", "err", err)
		return err
	}
	defer func() {
		if err := s.Close(); err != nil {
			slog.Error("error closing database", "err", err)
		} else {
			slog.Info("database closed cleanly")
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
		slog.Info("HTTP server listening", "addr", *addr)
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
		slog.Info("shutdown signal received", "signal", sig.String())
	case serveErr = <-serverErrCh:
		slog.Error("server error", "err", serveErr)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("server shutdown forced", "err", err)
	} else {
		slog.Info("server exited gracefully")
	}
	return serveErr
}
