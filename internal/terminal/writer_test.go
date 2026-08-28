package terminal

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

type shortWriter struct{}

func (shortWriter) Write(data []byte) (int, error) { return len(data) / 2, nil }

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }

func TestSanitizingWriterReportsDestinationFailures(t *testing.T) {
	if _, err := NewSanitizingWriter(shortWriter{}).Write([]byte("value")); !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("short write returned %v", err)
	}
	if _, err := NewSanitizingWriter(failingWriter{}).Write([]byte("value")); err == nil {
		t.Fatal("destination failure was ignored")
	}
	if written, err := NewSanitizingWriter(nil).Write([]byte("value")); err != nil || written != len("value") {
		t.Fatal("nil destination violated discard writer contract")
	}
}

func TestSanitizingWriterPreventsLogForging(t *testing.T) {
	var output bytes.Buffer
	writer := NewSanitizingWriter(&output)
	input := []byte("first\r\nforged\x1b]52;c;secret\x07")
	written, err := writer.Write(input)
	if err != nil || written != len(input) {
		t.Fatalf("writer contract failed: %d, %v", written, err)
	}
	if strings.Count(output.String(), "\n") != 1 || strings.ContainsAny(output.String(), "\x1b\a\r") {
		t.Fatalf("unsafe writer output: %q", output.String())
	}
}
