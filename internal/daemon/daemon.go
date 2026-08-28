package daemon

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// MethodFunc handles one daemon RPC method.
type MethodFunc func(ctx context.Context, params json.RawMessage) (any, error)

// Server is the private local IPC server.
type Server struct {
	options   Options
	listener  net.Listener
	address   string
	token     atomic.Value
	approvals *approvalQueue
	methods   map[string]MethodFunc
	mu        sync.RWMutex
	shutdown  chan struct{}
	once      sync.Once
	done      chan struct{}
	connWG    sync.WaitGroup
	started   time.Time
}

// Options configures the daemon server.
type Options struct {
	// Address is the user-private socket path (Unix) or loopback target.
	Address string
	// Now returns the current time; defaults to time.Now.
	Now func() time.Time
	// NewID generates unique identifiers; defaults to a secure random hex ID.
	NewID func() (string, error)
	// PeerUID returns the peer's UID for a connection (Unix only).
	PeerUID func(conn net.Conn) (uint32, bool)
	// Logger receives redacted lifecycle messages; optional.
	Logger func(format string, args ...any)
}

// NewServer creates a daemon server with the given options.
func NewServer(options Options) (*Server, error) {
	if options.Address == "" {
		return nil, fmt.Errorf("daemon: address is required")
	}
	server := &Server{
		options:   options,
		approvals: newApprovalQueue(options.Now, options.NewID),
		methods:   make(map[string]MethodFunc),
		shutdown:  make(chan struct{}),
		done:      make(chan struct{}),
	}
	server.rotateToken()
	server.registerDefaultMethods()
	return server, nil
}

// Start binds the listener and begins accepting connections.
func (server *Server) Start() error {
	listener, address, err := listen(server.options.Address)
	if err != nil {
		return err
	}
	server.listener = listener
	server.address = address
	server.started = time.Now().UTC()
	server.connWG.Add(1)
	go server.acceptLoop()
	server.log("daemon listening at %s", redactAddress(address))
	return nil
}

// Address returns the effective bound address.
func (server *Server) Address() string {
	return server.address
}

// Token returns the current capability token.
func (server *Server) Token() string {
	return server.token.Load().(string)
}

// RotateToken replaces the capability token and returns the new one.
func (server *Server) RotateToken() string {
	server.rotateToken()
	return server.Token()
}

func (server *Server) rotateToken() {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		panic(fmt.Sprintf("daemon: secure token generation failed: %v", err))
	}
	server.token.Store(hex.EncodeToString(raw))
}

// RegisterMethod registers a handler. It panics on duplicate methods.
func (server *Server) RegisterMethod(name string, handler MethodFunc) {
	server.mu.Lock()
	defer server.mu.Unlock()
	if _, exists := server.methods[name]; exists {
		panic("daemon: duplicate method " + name)
	}
	server.methods[name] = handler
}

// PendingApprovals returns the current pending approvals.
func (server *Server) PendingApprovals() PendingApprovals {
	return server.approvals.poll(server.now())
}

// SubmitApproval enqueues a single-use approval and returns it.
func (server *Server) SubmitApproval(kind ApprovalKind, accountID string, chainID uint64, summary string) (Approval, error) {
	return server.approvals.submit(kind, accountID, chainID, summary)
}

// DecideApproval consumes an approval exactly once.
func (server *Server) DecideApproval(approvalID string, approve bool) error {
	return server.approvals.decide(approvalID, approve, server.now())
}

// Shutdown closes the listener and waits for in-flight connections.
func (server *Server) Shutdown(ctx context.Context) error {
	server.once.Do(func() { close(server.shutdown) })
	if server.listener != nil {
		_ = server.listener.Close()
	}
	select {
	case <-server.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (server *Server) now() time.Time {
	if server.options.Now != nil {
		return server.options.Now()
	}
	return time.Now()
}

func (server *Server) log(format string, args ...any) {
	if server.options.Logger != nil {
		server.options.Logger(format, args...)
	}
}

func (server *Server) acceptLoop() {
	defer server.connWG.Done()
	defer close(server.done)
	for {
		conn, err := server.listener.Accept()
		if err != nil {
			select {
			case <-server.shutdown:
				return
			default:
			}
			if errors.Is(err, net.ErrClosed) {
				return
			}
			server.log("daemon accept failed: %v", err)
			time.Sleep(100 * time.Millisecond)
			continue
		}
		server.connWG.Add(1)
		go func() {
			defer server.connWG.Done()
			server.handleConnection(conn)
		}()
	}
}

func (server *Server) handleConnection(conn net.Conn) {
	defer func() { _ = conn.Close() }()
	if server.options.PeerUID != nil {
		if uid, ok := server.options.PeerUID(conn); ok && uid != currentUID() {
			_ = writeFrame(conn, NewErrorResponse("", CodeUnauthorized, "daemon: peer uid rejected"))
			return
		}
	}
	for {
		frame, err := readFrame(conn)
		if err != nil {
			return
		}
		var request Request
		if err := json.Unmarshal(frame, &request); err != nil {
			_ = writeFrame(conn, NewErrorResponse("", CodeInvalidRequest, "daemon: malformed request"))
			continue
		}
		if err := requestID(request.ID); err != nil {
			_ = writeFrame(conn, NewErrorResponse("", CodeInvalidRequest, "daemon: invalid request id"))
			continue
		}
		if !server.authorized(request.Token) {
			_ = writeFrame(conn, NewErrorResponse(request.ID, CodeUnauthorized, "daemon: unauthorized"))
			continue
		}
		response := server.dispatch(request)
		if err := writeFrame(conn, response); err != nil {
			return
		}
	}
}

func (server *Server) authorized(token string) bool {
	if token == "" {
		return false
	}
	expected := server.Token()
	if len(token) != len(expected) {
		return false
	}
	return subtleConstantTimeEqual([]byte(token), []byte(expected))
}

func (server *Server) dispatch(request Request) Response {
	server.mu.RLock()
	handler, exists := server.methods[request.Method]
	server.mu.RUnlock()
	if !exists {
		return NewErrorResponse(request.ID, CodeUnknownMethod, "daemon: unknown method")
	}
	ctx, cancel := context.WithTimeout(context.Background(), MaxRequestDuration)
	defer cancel()
	result, err := handler(ctx, request.Params)
	if err != nil {
		server.log("daemon method %s failed: %v", request.Method, err)
		switch {
		case errors.Is(err, ErrApprovalDone):
			return NewErrorResponse(request.ID, CodeApprovalDone, "daemon: approval already decided")
		case errors.Is(err, ErrApprovalGone):
			return NewErrorResponse(request.ID, CodeApprovalGone, "daemon: approval not found")
		case errors.Is(err, ErrInvalidRequest):
			return NewErrorResponse(request.ID, CodeInvalidRequest, "daemon: invalid request")
		case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
			return NewErrorResponse(request.ID, CodeInternal, "daemon: request timed out")
		default:
			return NewErrorResponse(request.ID, CodeInternal, "daemon: request failed")
		}
	}
	response, err := NewResponse(request.ID, result)
	if err != nil {
		return NewErrorResponse(request.ID, CodeInternal, "daemon: result encoding failed")
	}
	return response
}

// registerDefaultMethods installs the always-available RPC methods.
func (server *Server) registerDefaultMethods() {
	server.RegisterMethod("status", func(ctx context.Context, _ json.RawMessage) (any, error) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		type status struct {
			Version         string `json:"version"`
			PID             int    `json:"pid"`
			StartedAt       string `json:"started_at"`
			PendingApproval int    `json:"pending_approvals"`
		}
		return status{
			Version: versionValue(), PID: currentPID(), StartedAt: server.started.Format(time.RFC3339),
			PendingApproval: len(server.approvals.poll(server.now()).Approvals),
		}, nil
	})
	server.RegisterMethod("approval.submit", func(ctx context.Context, params json.RawMessage) (any, error) {
		var request struct {
			Kind      ApprovalKind `json:"kind"`
			AccountID string       `json:"account_id,omitempty"`
			ChainID   uint64       `json:"chain_id,omitempty"`
			Summary   string       `json:"summary"`
		}
		if err := json.Unmarshal(params, &request); err != nil {
			return nil, ErrInvalidRequest
		}
		switch request.Kind {
		case ApprovalKindSignMessage, ApprovalKindSignTyped, ApprovalKindTransaction, ApprovalKindSession, ApprovalKindGeneric:
		default:
			return nil, ErrInvalidRequest
		}
		approval, err := server.approvals.submit(request.Kind, request.AccountID, request.ChainID, request.Summary)
		if err != nil {
			return nil, err
		}
		return approval, nil
	})
	server.RegisterMethod("approval.poll", func(ctx context.Context, _ json.RawMessage) (any, error) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		return server.approvals.poll(server.now()), nil
	})
	server.RegisterMethod("approval.decide", func(ctx context.Context, params json.RawMessage) (any, error) {
		var decision ApprovalDecision
		if err := json.Unmarshal(params, &decision); err != nil || decision.ApprovalID == "" || len(decision.ApprovalID) > 64 {
			return nil, ErrInvalidRequest
		}
		if err := server.approvals.decide(decision.ApprovalID, decision.Approve, server.now()); err != nil {
			return nil, err
		}
		return map[string]bool{"decided": true}, nil
	})
	server.RegisterMethod("shutdown", func(ctx context.Context, _ json.RawMessage) (any, error) {
		go func() { _ = server.Shutdown(context.Background()) }()
		return map[string]bool{"shutting_down": true}, nil
	})
}

func subtleConstantTimeEqual(left, right []byte) bool {
	if len(left) != len(right) {
		return false
	}
	var difference byte
	for index := range left {
		difference |= left[index] ^ right[index]
	}
	return difference == 0
}

func redactAddress(address string) string {
	if len(address) > 24 {
		return address[:8] + "…"
	}
	return address
}

func randomID() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}
