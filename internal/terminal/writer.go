package terminal

import (
	"io"
	"sync"
)

type SanitizingWriter struct {
	destination io.Writer
	mu          sync.Mutex
}

func NewSanitizingWriter(destination io.Writer) *SanitizingWriter {
	return &SanitizingWriter{destination: destination}
}

func (writer *SanitizingWriter) Write(data []byte) (int, error) {
	if writer == nil || writer.destination == nil {
		return len(data), nil
	}
	message := SanitizeInline(string(data), 2048) + "\n"
	writer.mu.Lock()
	written, err := io.WriteString(writer.destination, message)
	writer.mu.Unlock()
	if err != nil {
		return 0, err
	}
	if written != len(message) {
		return 0, io.ErrShortWrite
	}
	return len(data), nil
}
