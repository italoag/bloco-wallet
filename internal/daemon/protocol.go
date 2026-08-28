// Package daemon implements a private local IPC daemon for the wallet.
//
// The daemon listens on a user-private Unix socket (or a loopback listener on
// Windows) and authenticates every request with an in-memory capability
// token. Tokens never touch disk, approvals are single-use with a short TTL,
// and responses never include secret material.
package daemon

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
)

const (
	// MaxFrameBytes bounds a single request or response frame.
	MaxFrameBytes = 1 << 20
	// MaxApprovals bounds the pending approval queue.
	MaxApprovals = 64
	// ApprovalTTL bounds how long a submitted approval stays actionable.
	ApprovalTTL = 5 * time.Minute
	// MaxRequestDuration bounds a single request processing time.
	MaxRequestDuration = 30 * time.Second
	// MaxAccounts bounds the accounts.list result size.
	MaxAccounts = 256
)

var (
	ErrUnauthorized   = errors.New("daemon: unauthorized")
	ErrUnknownMethod  = errors.New("daemon: unknown method")
	ErrInvalidRequest = errors.New("daemon: invalid request")
	ErrApprovalDone   = errors.New("daemon: approval already decided")
	ErrApprovalGone   = errors.New("daemon: approval not found")
	ErrShuttingDown   = errors.New("daemon: shutting down")
)

// ErrorCode is a stable machine-readable error code.
type ErrorCode int

const (
	CodeInvalidRequest ErrorCode = -32600
	CodeUnauthorized   ErrorCode = -32001
	CodeUnknownMethod  ErrorCode = -32601
	CodeInternal       ErrorCode = -32603
	CodeApprovalDone   ErrorCode = -32002
	CodeApprovalGone   ErrorCode = -32003
)

// Request is a single daemon RPC request.
type Request struct {
	ID     string          `json:"id"`
	Token  string          `json:"token"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

// Response is a single daemon RPC response.
type Response struct {
	ID     string          `json:"id"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *ResponseError  `json:"error,omitempty"`
}

// ResponseError mirrors ErrorCode with a human-readable message.
type ResponseError struct {
	Code    ErrorCode `json:"code"`
	Message string    `json:"message"`
}

// NewResponse builds a response carrying result.
func NewResponse(id string, result any) (Response, error) {
	encoded, err := json.Marshal(result)
	if err != nil {
		return Response{}, fmt.Errorf("encode daemon result: %w", err)
	}
	return Response{ID: id, Result: encoded}, nil
}

// NewErrorResponse builds an error response.
func NewErrorResponse(id string, code ErrorCode, message string) Response {
	return Response{ID: id, Error: &ResponseError{Code: code, Message: message}}
}

// ApprovalKind classifies a pending approval for the queue consumer.
type ApprovalKind string

const (
	ApprovalKindSignMessage ApprovalKind = "sign_message"
	ApprovalKindSignTyped   ApprovalKind = "sign_typed_data"
	ApprovalKindTransaction ApprovalKind = "transaction"
	ApprovalKindSession     ApprovalKind = "walletconnect_session"
	ApprovalKindGeneric     ApprovalKind = "generic"
)

// Approval is a single-use approval request submitted to the daemon queue.
type Approval struct {
	ID        string       `json:"id"`
	Kind      ApprovalKind `json:"kind"`
	AccountID string       `json:"account_id,omitempty"`
	ChainID   uint64       `json:"chain_id,omitempty"`
	Summary   string       `json:"summary"`
	ExpiresAt time.Time    `json:"expires_at"`
}

// ApprovalDecision records a user decision on an approval.
type ApprovalDecision struct {
	ApprovalID string `json:"approval_id"`
	Approve    bool   `json:"approve"`
}

// PendingApprovals is the poll result.
type PendingApprovals struct {
	Approvals []Approval `json:"approvals"`
}

// approvalEntry is the internal queue item with single-use state.
type approvalEntry struct {
	approval Approval
	decided  bool
	approve  bool
}

// approvalQueue is a bounded, TTL-scanned, single-use approval queue.
type approvalQueue struct {
	mu      sync.Mutex
	entries map[string]*approvalEntry
	order   []string
	now     func() time.Time
	nextID  func() (string, error)
	ttl     time.Duration
}

func newApprovalQueue(now func() time.Time, nextID func() (string, error)) *approvalQueue {
	if now == nil {
		now = time.Now
	}
	if nextID == nil {
		nextID = randomID
	}
	return &approvalQueue{entries: make(map[string]*approvalEntry), now: now, nextID: nextID, ttl: ApprovalTTL}
}

func (queue *approvalQueue) submit(kind ApprovalKind, accountID string, chainID uint64, summary string) (Approval, error) {
	queue.mu.Lock()
	defer queue.mu.Unlock()
	queue.sweepLocked()
	if len(queue.entries) >= MaxApprovals {
		return Approval{}, fmt.Errorf("daemon: approval queue is full")
	}
	if summary == "" || len(summary) > 512 {
		return Approval{}, fmt.Errorf("daemon: invalid approval summary")
	}
	id, err := queue.nextID()
	if err != nil {
		return Approval{}, fmt.Errorf("daemon: approval ID: %w", err)
	}
	approval := Approval{
		ID: id, Kind: kind, AccountID: accountID, ChainID: chainID,
		Summary: summary, ExpiresAt: queue.now().Add(queue.ttl).UTC(),
	}
	queue.entries[id] = &approvalEntry{approval: approval}
	queue.order = append(queue.order, id)
	return approval, nil
}

func (queue *approvalQueue) poll(now time.Time) PendingApprovals {
	queue.mu.Lock()
	defer queue.mu.Unlock()
	queue.sweepLocked()
	now = now.UTC()
	approvals := make([]Approval, 0, len(queue.entries))
	for _, id := range queue.order {
		entry, exists := queue.entries[id]
		if !exists || entry.decided {
			continue
		}
		if entry.approval.ExpiresAt.Before(now) {
			delete(queue.entries, id)
			continue
		}
		approvals = append(approvals, entry.approval)
	}
	return PendingApprovals{Approvals: approvals}
}

// decide consumes an approval exactly once and returns its decision.
func (queue *approvalQueue) decide(approvalID string, approve bool, now time.Time) error {
	queue.mu.Lock()
	defer queue.mu.Unlock()
	queue.sweepLocked()
	entry, exists := queue.entries[approvalID]
	if !exists {
		return ErrApprovalGone
	}
	if entry.decided {
		return ErrApprovalDone
	}
	if entry.approval.ExpiresAt.Before(now.UTC()) {
		delete(queue.entries, approvalID)
		return ErrApprovalGone
	}
	entry.decided = true
	entry.approve = approve
	return nil
}

func (queue *approvalQueue) sweepLocked() {
	now := queue.now().UTC()
	kept := queue.order[:0]
	for _, id := range queue.order {
		entry, exists := queue.entries[id]
		if !exists || entry.approval.ExpiresAt.Before(now) {
			delete(queue.entries, id)
			continue
		}
		kept = append(kept, id)
	}
	queue.order = kept
}

// requestID validates a client-supplied request ID.
func requestID(id string) error {
	if id == "" || len(id) > 64 || strings.ContainsAny(id, "\r\n\x00") {
		return ErrInvalidRequest
	}
	return nil
}

// writeFrame writes a length-bounded JSON frame.
func writeFrame(writer io.Writer, value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode daemon frame: %w", err)
	}
	if len(encoded) > MaxFrameBytes {
		return fmt.Errorf("daemon frame exceeds %d bytes", MaxFrameBytes)
	}
	encoded = append(encoded, '\n')
	if _, err := writer.Write(encoded); err != nil {
		return fmt.Errorf("write daemon frame: %w", err)
	}
	return nil
}

// readFrame reads one bounded JSON frame.
func readFrame(reader io.Reader) ([]byte, error) {
	limited := io.LimitReader(reader, MaxFrameBytes+1)
	frame, err := readLine(limited)
	if err != nil {
		return nil, err
	}
	if len(frame) > MaxFrameBytes {
		return nil, ErrInvalidRequest
	}
	return frame, nil
}

func readLine(reader io.Reader) ([]byte, error) {
	line := make([]byte, 0, 512)
	buffer := make([]byte, 1)
	for {
		if _, err := reader.Read(buffer); err != nil {
			return nil, err
		}
		if buffer[0] == '\n' {
			return line, nil
		}
		line = append(line, buffer[0])
	}
}
