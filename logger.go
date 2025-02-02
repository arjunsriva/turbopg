package turbopg

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

// noopLogger is a logger that does nothing
type noopLogger struct{}

func (l *noopLogger) Error(msg string, fields ...Field) {}
func (l *noopLogger) Info(msg string, fields ...Field)  {}
func (l *noopLogger) Debug(msg string, fields ...Field) {}
