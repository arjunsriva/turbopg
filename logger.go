package turbopg

import (
	"fmt"
	"log"
	"strings"
)

// Field represents a structured logging field
type Field struct {
	Key   string
	Value interface{}
}

// Logger is the interface that wraps the basic logging methods
type Logger interface {
	Error(msg string, fields ...Field)
	Info(msg string, fields ...Field)
	Debug(msg string, fields ...Field)
}

// NoOpLogger is a logger that discards all output.
type NoOpLogger struct{}

func (l *NoOpLogger) Error(msg string, fields ...Field) {}
func (l *NoOpLogger) Info(msg string, fields ...Field)  {}
func (l *NoOpLogger) Debug(msg string, fields ...Field) {}

type noopLogger = NoOpLogger

func formatFields(fields []Field) string {
	if len(fields) == 0 {
		return ""
	}
	parts := make([]string, len(fields))
	for i, f := range fields {
		parts[i] = fmt.Sprintf("%s=%v", f.Key, f.Value)
	}
	return strings.Join(parts, " ")
}

// StdLogger writes Info/Error/Debug lines to the standard library logger.
type StdLogger struct {
	// MinLevel is error, info, or debug. Empty means info.
	MinLevel string
}

func (l *StdLogger) allows(level string) bool {
	min := "info"
	if l != nil && l.MinLevel != "" {
		min = strings.ToLower(l.MinLevel)
	}
	rank := func(s string) int {
		switch s {
		case "debug":
			return 0
		case "info":
			return 1
		default:
			return 2
		}
	}
	return rank(level) >= rank(min)
}

func (l *StdLogger) Error(msg string, fields ...Field) {
	log.Printf("ERROR %s %s", msg, formatFields(fields))
}

func (l *StdLogger) Info(msg string, fields ...Field) {
	if !l.allows("info") {
		return
	}
	log.Printf("INFO %s %s", msg, formatFields(fields))
}

func (l *StdLogger) Debug(msg string, fields ...Field) {
	if !l.allows("debug") {
		return
	}
	log.Printf("DEBUG %s %s", msg, formatFields(fields))
}
