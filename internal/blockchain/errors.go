package blockchain

import (
	"fmt"

	"blocowallet/internal/terminal"
)

// NetworkOperationError represents errors during network operations with context
// Operation: "search", "validate", "add", etc.
// Message: a brief description suitable for logs/UI before localization
// Cause: the underlying error (preserved with Unwrap)
type NetworkOperationError struct {
	Operation string
	Message   string
	Cause     error
}

func (e *NetworkOperationError) Error() string {
	if e == nil {
		return ""
	}
	message := terminal.SanitizeInline(e.Message, 256)
	if message != "" && e.Cause != nil {
		return fmt.Sprintf("%s: %s", message, terminal.SanitizeInline(e.Cause.Error(), 256))
	}
	if message != "" {
		return message
	}
	if e.Cause != nil {
		return terminal.SanitizeInline(e.Cause.Error(), 256)
	}
	return "network operation error"
}

func (e *NetworkOperationError) Unwrap() error { return e.Cause }

func NewNetworkOperationError(operation, message string, cause error) *NetworkOperationError {
	return &NetworkOperationError{Operation: operation, Message: message, Cause: cause}
}
