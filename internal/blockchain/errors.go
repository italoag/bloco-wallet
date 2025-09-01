package blockchain

import "fmt"

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
	if e.Message != "" && e.Cause != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Cause)
	}
	if e.Message != "" {
		return e.Message
	}
	if e.Cause != nil {
		return e.Cause.Error()
	}
	return "network operation error"
}

func (e *NetworkOperationError) Unwrap() error { return e.Cause }

func NewNetworkOperationError(operation, message string, cause error) *NetworkOperationError {
	return &NetworkOperationError{Operation: operation, Message: message, Cause: cause}
}
