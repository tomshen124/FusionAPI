package logger

import (
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"sync"
	"time"
)

// Level represents log severity.
type Level int

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
)

func (l Level) String() string {
	switch l {
	case LevelDebug:
		return "DEBUG"
	case LevelInfo:
		return "INFO"
	case LevelWarn:
		return "WARN"
	case LevelError:
		return "ERROR"
	default:
		return "INFO"
	}
}

// ParseLevel parses a level string (case-insensitive).
func ParseLevel(s string) Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return LevelDebug
	case "info":
		return LevelInfo
	case "warn", "warning":
		return LevelWarn
	case "error":
		return LevelError
	default:
		return LevelInfo
	}
}

// Logger is a leveled structured logger.
type Logger struct {
	mu    sync.RWMutex
	level Level
	out   *log.Logger
}

var defaultLogger = &Logger{
	level: LevelInfo,
	out:   log.New(os.Stdout, "", 0),
}

// Default returns the package-level logger.
func Default() *Logger {
	return defaultLogger
}

// SetLevel changes the minimum log level.
func (l *Logger) SetLevel(level Level) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.level = level
}

// SetOutput changes the writer.
func (l *Logger) SetOutput(w io.Writer) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.out = log.New(w, "", 0)
}

func (l *Logger) enabled(level Level) bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return level >= l.level
}

func (l *Logger) logf(level Level, format string, args ...any) {
	if !l.enabled(level) {
		return
	}
	msg := fmt.Sprintf(format, args...)
	ts := time.Now().Format("2006-01-02T15:04:05.000Z07:00")
	l.mu.RLock()
	out := l.out
	l.mu.RUnlock()
	out.Printf("%s [%s] %s", ts, level, msg)
}

func (l *Logger) log(level Level, msg string, kvs ...any) {
	if !l.enabled(level) {
		return
	}
	ts := time.Now().Format("2006-01-02T15:04:05.000Z07:00")
	var b strings.Builder
	fmt.Fprintf(&b, "%s [%s] %s", ts, level, msg)
	for i := 0; i+1 < len(kvs); i += 2 {
		fmt.Fprintf(&b, " %v=%v", kvs[i], kvs[i+1])
	}
	l.mu.RLock()
	out := l.out
	l.mu.RUnlock()
	out.Println(b.String())
}

// Debug logs a debug message.
func (l *Logger) Debug(msg string, kvs ...any) { l.log(LevelDebug, msg, kvs...) }

// Info logs an info message.
func (l *Logger) Info(msg string, kvs ...any) { l.log(LevelInfo, msg, kvs...) }

// Warn logs a warning message.
func (l *Logger) Warn(msg string, kvs ...any) { l.log(LevelWarn, msg, kvs...) }

// Error logs an error message.
func (l *Logger) Error(msg string, kvs ...any) { l.log(LevelError, msg, kvs...) }

// Infof logs a formatted info message.
func (l *Logger) Infof(format string, args ...any) { l.logf(LevelInfo, format, args...) }

// Warnf logs a formatted warning message.
func (l *Logger) Warnf(format string, args ...any) { l.logf(LevelWarn, format, args...) }

// Errorf logs a formatted error message.
func (l *Logger) Errorf(format string, args ...any) { l.logf(LevelError, format, args...) }

// Debugf logs a formatted debug message.
func (l *Logger) Debugf(format string, args ...any) { l.logf(LevelDebug, format, args...) }

// Package-level convenience functions.

func SetLevel(level Level) { defaultLogger.SetLevel(level) }
func Debug(msg string, kvs ...any) { defaultLogger.Debug(msg, kvs...) }
func Info(msg string, kvs ...any)  { defaultLogger.Info(msg, kvs...) }
func Warn(msg string, kvs ...any)  { defaultLogger.Warn(msg, kvs...) }
func Error(msg string, kvs ...any) { defaultLogger.Error(msg, kvs...) }
func Infof(format string, args ...any)  { defaultLogger.Infof(format, args...) }
func Warnf(format string, args ...any)  { defaultLogger.Warnf(format, args...) }
func Errorf(format string, args ...any) { defaultLogger.Errorf(format, args...) }
func Debugf(format string, args ...any) { defaultLogger.Debugf(format, args...) }
