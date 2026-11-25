package logwrap

import (
	"io"

	"github.com/charmbracelet/log"
	"github.com/dgraph-io/badger/v4"
)

// Logger wraps Charm's logger by embedding *log.Logger and adding
// the Warningf method to satisfy badger.Logger.
type Logger struct {
	*log.Logger
}

// New creates a new Logger wrapping Charm's logger writing to w.
func New(w io.Writer) *Logger {
	return Wrap(log.New(w))
}

// NewWithOptions creates a new Logger with Charm Options.
func NewWithOptions(w io.Writer, opts log.Options) *Logger {
	return Wrap(log.NewWithOptions(w, opts))
}

// Wrap wraps an existing *log.Logger.
func Wrap(l *log.Logger) *Logger {
	return &Logger{Logger: l}
}

// Warningf satisfies badger.Logger by forwarding to Charm's Warnf.
func (l *Logger) Warningf(format string, args ...any) {
	l.Warnf(format, args...)
}

// Ensure Logger satisfies badger.Logger at compile time.
var _ badger.Logger = (*Logger)(nil)
