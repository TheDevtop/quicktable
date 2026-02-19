package logwrap

/*
	Quicktable
	Wrapper for logging
*/

import (
	"os"

	"github.com/charmbracelet/log"
)

type Logger struct {
	*log.Logger
}

func (l *Logger) Errorf(format string, v ...interface{}) {
	log.Errorf(format, v...)
}

func (l *Logger) Warningf(format string, v ...interface{}) {
	log.Warnf(format, v...)
}

func (l *Logger) Infof(format string, v ...interface{}) {
	log.Infof(format, v...)
}

func (l *Logger) Debugf(format string, v ...interface{}) {
	log.Debugf(format, v...)
}

func NewLogger() *Logger {
	return &Logger{log.New(os.Stderr)}
}
