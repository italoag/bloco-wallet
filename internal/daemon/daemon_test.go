package daemon_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"blocowallet/internal/daemon"
)

func startTestDaemon(t *testing.T, now func() time.Time) (*daemon.Server, string) {
	t.Helper()
	name := strings.NewReplacer("/", "_", " ", "_").Replace(t.Name())
	_ = name
	address := filepath.Join(os.TempDir(), fmt.Sprintf("bw-%d.sock", time.Now().UnixNano()))
	t.Cleanup(func() { _ = os.Remove(address) })
	server, err := daemon.NewServer(daemon.Options{
		Address: address,
		Now:     now,
		NewID: func() (string, error) {
			return fmt.Sprintf("approval-%d", time.Now().UnixNano()), nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Shutdown(context.Background()) })
	return server, server.Token()
}

func TestDaemonStatusAndAuthentication(t *testing.T) {
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	server, token := startTestDaemon(t, func() time.Time { return now })
	client, err := daemon.Dial(context.Background(), server.Address(), token)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	var status struct {
		Version         string `json:"version"`
		PID             int    `json:"pid"`
		StartedAt       string `json:"started_at"`
		PendingApproval int    `json:"pending_approvals"`
	}
	if err := client.Call(context.Background(), "status", nil, &status); err != nil {
		t.Fatal(err)
	}
	if status.PID <= 0 || status.PendingApproval != 0 || status.Version == "" {
		t.Fatalf("unexpected daemon status: %+v", status)
	}
	unauthorized, err := daemon.Dial(context.Background(), server.Address(), "0000000000000000000000000000000000000000000000000000000000000000")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = unauthorized.Close() }()
	if err := unauthorized.Call(context.Background(), "status", nil, &status); !errors.Is(err, daemon.ErrUnauthorized) {
		t.Fatalf("wrong token was accepted: %v", err)
	}
	if err := client.Call(context.Background(), "unknown.method", nil, nil); err == nil {
		t.Fatal("unknown method was accepted")
	}
	if err := client.Call(context.Background(), "status", nil, &status); err != nil {
		t.Fatalf("valid client stopped working after rotation-free flow: %v", err)
	}
}

func TestDaemonApprovalQueueSingleUseAndTTL(t *testing.T) {
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	clock := &now
	server, token := startTestDaemon(t, func() time.Time { return *clock })
	client, err := daemon.Dial(context.Background(), server.Address(), token)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()

	submit := func(kind daemon.ApprovalKind) daemon.Approval {
		t.Helper()
		var approval daemon.Approval
		params := map[string]any{"kind": string(kind), "summary": "sign 1 ETH to 0x1111"}
		if err := client.Call(context.Background(), "approval.submit", params, &approval); err != nil {
			t.Fatal(err)
		}
		if approval.ID == "" || approval.ExpiresAt.IsZero() {
			t.Fatalf("approval was not materialized: %+v", approval)
		}
		return approval
	}

	approval := submit(daemon.ApprovalKindSignMessage)
	var polled daemon.PendingApprovals
	if err := client.Call(context.Background(), "approval.poll", nil, &polled); err != nil {
		t.Fatal(err)
	}
	if len(polled.Approvals) != 1 || polled.Approvals[0].ID != approval.ID {
		t.Fatalf("approval was not visible in the queue: %+v", polled)
	}

	var decided map[string]bool
	if err := client.Call(context.Background(), "approval.decide", map[string]any{"approval_id": approval.ID, "approve": true}, &decided); err != nil {
		t.Fatal(err)
	}
	if err := client.Call(context.Background(), "approval.decide", map[string]any{"approval_id": approval.ID, "approve": true}, &decided); !errors.Is(err, daemon.ErrApprovalDone) {
		t.Fatalf("approval was decided twice: %v", err)
	}
	if err := client.Call(context.Background(), "approval.decide", map[string]any{"approval_id": "missing", "approve": true}, &decided); !errors.Is(err, daemon.ErrApprovalGone) {
		t.Fatalf("missing approval returned %v", err)
	}

	// Expired approvals disappear from polls and cannot be decided.
	expired := submit(daemon.ApprovalKindTransaction)
	*clock = now.Add(daemon.ApprovalTTL + time.Minute)
	if err := client.Call(context.Background(), "approval.poll", nil, &polled); err != nil {
		t.Fatal(err)
	}
	if len(polled.Approvals) != 0 {
		t.Fatalf("expired approvals remained visible: %+v", polled)
	}
	if err := client.Call(context.Background(), "approval.decide", map[string]any{"approval_id": expired.ID, "approve": true}, &decided); !errors.Is(err, daemon.ErrApprovalGone) {
		t.Fatalf("expired approval was decidable: %v", err)
	}
}

func TestDaemonRejectsOversizedAndMalformedFrames(t *testing.T) {
	server, token := startTestDaemon(t, nil)
	client, err := daemon.Dial(context.Background(), server.Address(), token)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	// Oversized params must be rejected server-side without closing the client.
	huge := make([]byte, daemon.MaxFrameBytes+16)
	if err := client.Call(context.Background(), "status", map[string]any{"blob": string(huge)}, nil); err == nil {
		t.Fatal("oversized frame was accepted")
	}
	var status struct{ PID int }
	if err := client.Call(context.Background(), "status", nil, &status); err != nil {
		t.Fatalf("client broke after oversized frame: %v", err)
	}
	// Malformed summary and unknown approval kinds are rejected.
	if err := client.Call(context.Background(), "approval.submit", map[string]any{"kind": "unknown", "summary": "x"}, nil); err == nil {
		t.Fatal("unknown approval kind was accepted")
	}
	if err := client.Call(context.Background(), "approval.submit", map[string]any{"kind": "generic", "summary": ""}, nil); err == nil {
		t.Fatal("empty approval summary was accepted")
	}
}

func TestDaemonShutdownCommandTerminatesServer(t *testing.T) {
	server, token := startTestDaemon(t, nil)
	client, err := daemon.Dial(context.Background(), server.Address(), token)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	var result map[string]bool
	if err := client.Call(context.Background(), "shutdown", nil, &result); err != nil {
		t.Fatal(err)
	}
	select {
	case <-time.After(5 * time.Second):
		t.Fatal("daemon did not terminate after shutdown request")
	case <-shutdownDone(server):
	}
}

func shutdownDone(server *daemon.Server) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		_ = server.Shutdown(context.Background())
		close(done)
	}()
	return done
}
