package logger

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewFileLoggerCreatesFiles(t *testing.T) {
	tdir := t.TempDir()
	lgr, err := NewFileLogger(LoggingConfig{
		LogDir:      tdir,
		LogLevel:    "info",
		MaxFileSize: 1,
		MaxBackups:  1,
		MaxAge:      1,
	})
	if err != nil {
		t.Fatalf("NewFileLogger returned error: %v", err)
	}

	// Write logs
	lgr.Info("info message")
	lgr.Error("error message")
	_ = lgr.Sync()

	appPath := filepath.Join(tdir, "app.log")
	errPath := filepath.Join(tdir, "error.log")

	if _, err := os.Stat(appPath); err != nil {
		t.Fatalf("expected app.log to be created, got error: %v", err)
	}
	if _, err := os.Stat(errPath); err != nil {
		t.Fatalf("expected error.log to be created, got error: %v", err)
	}
}
