package blockchain

import (
	"errors"
	"testing"
)

func TestValidateRPCEndpoint_EmptyURL(t *testing.T) {
	s := NewChainListService()
	err := s.ValidateRPCEndpoint("")
	var opErr *NetworkOperationError
	if !errors.As(err, &opErr) {
		t.Fatalf("expected NetworkOperationError, got %T: %v", err, err)
	}
	if opErr.Operation != "validate" {
		t.Fatalf("expected operation 'validate', got %q", opErr.Operation)
	}
	if opErr.Message == "" {
		t.Fatalf("expected non-empty message")
	}
}

func TestNewNetworkOperationError_ErrorString(t *testing.T) {
	err := NewNetworkOperationError("search", "failed to fetch", assertErr("timeout"))
	if got := err.Error(); got == "" || !containsAll(got, []string{"failed to fetch", "timeout"}) {
		t.Fatalf("unexpected Error(): %q", got)
	}
}

// helpers copied from UI tests

type simpleErr string

func (e simpleErr) Error() string { return string(e) }

func assertErr(s string) error { return simpleErr(s) }

func containsAll(hay string, needles []string) bool {
	for _, n := range needles {
		if !stringsContains(hay, n) {
			return false
		}
	}
	return true
}

func stringsContains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
