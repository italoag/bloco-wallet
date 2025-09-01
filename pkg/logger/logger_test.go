package logger

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewFileLogger_WritesToFilesAndSyncs(t *testing.T) {
	tmp := t.TempDir()
	l, err := NewFileLogger(LoggingConfig{LogDir: tmp, LogLevel: "info", MaxFileSize: 1, MaxBackups: 1, MaxAge: 1})
	if err != nil {
		t.Fatalf("NewFileLogger error: %v", err)
	}

	// Write one info and one error
	l.Info("test-info", String("k", "v"))
	l.Error("test-error", String("k", "v"))
	if err := l.Sync(); err != nil {
		t.Fatalf("Sync should not return error, got: %v", err)
	}

	appPath := filepath.Join(tmp, "app.log")
	errPath := filepath.Join(tmp, "error.log")

	// app.log should exist and contain test-info, but not necessarily test-error
	appBytes, err := os.ReadFile(appPath)
	if err != nil {
		t.Fatalf("failed reading app.log: %v", err)
	}
	app := string(appBytes)
	if !strings.Contains(app, "test-info") {
		t.Fatalf("app.log missing info entry: %s", app)
	}

	// error.log should contain test-error
	errBytes, err := os.ReadFile(errPath)
	if err != nil {
		t.Fatalf("failed reading error.log: %v", err)
	}
	errStr := string(errBytes)
	if !strings.Contains(errStr, "test-error") {
		t.Fatalf("error.log missing error entry: %s", errStr)
	}
}
