package storage

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"blocowallet/internal/walletconnect"
	"blocowallet/pkg/config"
)

func TestWalletConnectSessionStoreDurableFlow(t *testing.T) {
	root := t.TempDir()
	cfg := &config.Config{
		AppDir:       root,
		DatabasePath: filepath.Join(root, "wc.db"),
		Database:     config.DatabaseConfig{Type: "sqlite"},
	}
	repository, err := NewVaultRepository(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = repository.Close() }()
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC).UnixMilli()
	session := &walletconnect.Session{
		Topic:        "1111111111111111111111111111111111111111111111111111111111111111",
		PeerName:     "Dapp",
		PeerMetadata: map[string]any{"name": "Dapp", "url": "https://dapp.example"},
		AccountID:    "11111111-1111-4111-8111-111111111111",
		Namespaces: walletconnect.Namespaces{
			"eip155": {Chains: []string{"eip155:1"}, Methods: []string{"personal_sign"}, Events: []string{"chainChanged"}, Accounts: []string{"eip155:1:0x1111111111111111111111111111111111111111"}},
		},
		ExpiresAt: now + 3600000,
		CreatedAt: now,
	}
	if err := repository.SaveSession(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	loaded, err := repository.GetSession(context.Background(), session.Topic)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.PeerName != "Dapp" || loaded.Namespaces["eip155"].Accounts[0] != "eip155:1:0x1111111111111111111111111111111111111111" {
		t.Fatalf("session did not survive round-trip: %+v", loaded)
	}
	if err := repository.TouchSession(context.Background(), session.Topic, now+1000); err != nil {
		t.Fatal(err)
	}
	if err := repository.RevokeSession(context.Background(), session.Topic, now+2000); err != nil {
		t.Fatal(err)
	}
	if err := repository.RevokeSession(context.Background(), session.Topic, now+3000); err == nil {
		t.Fatal("session was revoked twice")
	}
	sessions, err := repository.ListSessions(context.Background(), session.AccountID, false)
	if err != nil || len(sessions) != 0 {
		t.Fatalf("revoked session remained visible: %+v %v", sessions, err)
	}
	all, err := repository.ListSessions(context.Background(), session.AccountID, true)
	if err != nil || len(all) != 1 {
		t.Fatalf("revoked session missing from full listing: %+v %v", all, err)
	}
	// Immutability of the session binding.
	if err := repository.db.Model(&wcSessionRow{}).Where("topic = ?", session.Topic).Update("namespaces", "[]").Error; err == nil {
		t.Fatal("walletconnect session binding was mutable")
	}
}
