package logger

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileLoggerRejectsSymlinkLogFiles(t *testing.T) {
	if os.PathSeparator == '\\' {
		t.Skip("symlink permissions vary on Windows")
	}
	directoryTarget := t.TempDir()
	directoryLink := filepath.Join(t.TempDir(), "logs-link")
	if err := os.Symlink(directoryTarget, directoryLink); err != nil {
		t.Fatal(err)
	}
	if _, err := NewFileLogger(LoggingConfig{LogDir: directoryLink}); err == nil {
		t.Fatal("symlink log directory was accepted")
	}
	root := t.TempDir()
	victim := filepath.Join(t.TempDir(), "victim.log")
	if err := os.WriteFile(victim, []byte("unchanged"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(victim, filepath.Join(root, "app.log")); err != nil {
		t.Fatal(err)
	}
	if _, err := NewFileLogger(LoggingConfig{LogDir: root}); err == nil {
		t.Fatal("symlink log file was accepted")
	}
	data, err := os.ReadFile(victim)
	if err != nil || string(data) != "unchanged" {
		t.Fatal("symlink target was modified")
	}
}

func TestFileLoggerSanitizesControlsURLsAndSecrets(t *testing.T) {
	root := t.TempDir()
	log, err := NewFileLogger(LoggingConfig{LogDir: root, LogLevel: "info"})
	if err != nil {
		t.Fatal(err)
	}
	log.Info("remote\r\nforged\x1b]52;c;secret\x07 https://user:pass@rpc.example.com/v3/token?key=secret",
		String("api_token", "super-secret"),
		String("endpoint", "https://rpc.example.com/v3/token?key=secret"),
		String("value", `endpoint="https://rpc.example.com/v3/another-token?key=secret"`),
		String("authorization", "Bearer credential"),
		Error(errors.New("bad\r\nline\x1b[31m password=error-canary")),
	)
	_ = log.Sync()
	data, err := os.ReadFile(filepath.Join(root, "app.log"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(string(data), "\x1b\a\r") || strings.Contains(string(data), "super-secret") || strings.Contains(string(data), "/v3/token") || strings.Contains(string(data), "key=secret") || strings.Contains(string(data), "another-token") || strings.Contains(string(data), "Bearer credential") || strings.Contains(string(data), "error-canary") {
		t.Fatalf("log retained unsafe data: %s", data)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 1 {
		t.Fatalf("log forging created %d records", len(lines))
	}
	var record map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &record); err != nil {
		t.Fatalf("log record is not valid JSON: %v", err)
	}
}

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
