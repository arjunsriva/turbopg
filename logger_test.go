package turbopg

import "testing"

func TestNoOpLogger(t *testing.T) {
	var l NoOpLogger
	l.Error("e")
	l.Info("i")
	l.Debug("d")
}

func TestStdLoggerLevel(t *testing.T) {
	l := &StdLogger{MinLevel: "error"}
	l.Info("should skip")
	l.Debug("should skip")
	l.Error("ok")
}
