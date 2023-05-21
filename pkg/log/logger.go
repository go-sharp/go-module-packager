// Copyright © 2023 The Go-Sharp Authors
//
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file.

// Package log contains global logging functions.
package log

import (
	"io"
	stdLog "log"
	"os"
	"sync"
)

type Level int

const (
	DisabledLvl Level = iota
	ErrorLvl
	InfoLvl
	DebugLvl
)

var (
	// Configuration fields
	mux                = &sync.RWMutex{}
	currentLevel Level = DisabledLvl

	// Logger fields
	debugLogger   *stdLog.Logger
	infoLogger    *stdLog.Logger
	errorLogger   *stdLog.Logger
	discardLogger *stdLog.Logger
)

func init() {
	debugLogger = stdLog.New(os.Stdout, "[DBG]", stdLog.Flags())
	infoLogger = stdLog.New(os.Stdout, "[INF]", stdLog.Flags())
	errorLogger = stdLog.New(os.Stderr, "[ERR]", stdLog.Flags())
	discardLogger = stdLog.New(io.Discard, "", 0)
}

// SetPrefix sets the output prefix for the loggers.
func SetPrefix(prefix string) {
	debugLogger.SetPrefix("[DBG]" + prefix)
	infoLogger.SetPrefix("[INF]" + prefix)
	errorLogger.SetPrefix("[ERR]" + prefix)
}

// SetFlags sets the output flags for the loggers.
// The flag bits are Ldate, Ltime, and so on.
func SetFlags(flag int) {
	debugLogger.SetFlags(flag)
	infoLogger.SetFlags(flag)
	errorLogger.SetFlags(flag)
}

// SetOutput sets the output destination for the loggers.
// DebugLvl logs everything, InfoLvl logs InfoLvl and ErrorLvl, ErrorLvl logs only errors.
func SetOutput(w io.Writer) {
	debugLogger.SetOutput(w)
	infoLogger.SetOutput(w)
	errorLogger.SetOutput(w)
}

// SetLevel sets which minimum levels to log.
func SetLevel(l Level) {
	mux.Lock()
	defer mux.Unlock()

	currentLevel = l
}

// Info returns the logger for info messages.
func Info() *stdLog.Logger {
	return getLogger(InfoLvl, infoLogger)
}

// Debug returns the logger for debug messages.
func Debug() *stdLog.Logger {
	return getLogger(DebugLvl, debugLogger)
}

// Error returns the logger for error messages.
func Error() *stdLog.Logger {
	return getLogger(ErrorLvl, errorLogger)
}

func getLogger(l Level, logger *stdLog.Logger) *stdLog.Logger {
	mux.RLock()
	defer mux.RUnlock()

	if currentLevel >= l {
		return logger
	}

	return discardLogger
}
