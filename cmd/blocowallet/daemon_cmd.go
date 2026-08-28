package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"blocowallet/internal/daemon"
	"blocowallet/internal/storage"
	"blocowallet/pkg/config"
)

// runDaemon starts the private local IPC daemon. The capability token is
// printed exactly once on stdout; it never touches disk.
func runDaemon(cfg *config.Config) {
	if err := ensurePrivateDirectory(cfg.AppDir, cfg.AppDir); err != nil {
		log.Printf("Failed to secure application directory: %v", err)
		os.Exit(1)
	}
	repo, err := storage.NewVaultRepository(cfg)
	if err != nil {
		log.Printf("Failed to create wallet repository: %v", err)
		os.Exit(1)
	}
	defer func() {
		if err := repo.Close(); err != nil {
			log.Printf("Error closing repository: %v", err)
		}
	}()
	socketPath := filepath.Join(cfg.AppDir, "bloco-daemon.sock")
	server, err := daemon.NewServer(daemon.Options{
		Address: socketPath,
		Logger:  func(format string, args ...any) { log.Printf(format, args...) },
	})
	if err != nil {
		log.Printf("Failed to initialize daemon: %v", err)
		os.Exit(1)
	}
	server.RegisterMethod("accounts.list", func(ctx context.Context, _ json.RawMessage) (any, error) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		accounts, err := repo.ListAccounts(context.Background())
		if err != nil {
			return nil, err
		}
		type accountSummary struct {
			AccountID    string `json:"account_id"`
			Name         string `json:"name"`
			Address      string `json:"address"`
			SignerKind   string `json:"signer_kind"`
			State        string `json:"state"`
			Capabilities uint64 `json:"capabilities"`
		}
		summary := make([]accountSummary, 0, len(accounts))
		for _, account := range accounts {
			if account.Address == "" {
				continue
			}
			summary = append(summary, accountSummary{
				AccountID:    account.AccountID,
				Name:         sanitizeDaemonString(account.Name),
				Address:      account.Address,
				SignerKind:   string(account.SignerKind),
				State:        string(account.State),
				Capabilities: uint64(account.Capabilities),
			})
		}
		if len(summary) > daemon.MaxAccounts {
			summary = summary[:daemon.MaxAccounts]
		}
		return map[string]any{"accounts": summary}, nil
	})
	daemon.SetVersion(version)
	if err := server.Start(); err != nil {
		log.Printf("Failed to start daemon: %v", err)
		os.Exit(1)
	}
	fmt.Printf("daemon address: %s\n", server.Address())
	fmt.Printf("daemon token: %s\n", server.Token())
	fmt.Println("token is ephemeral and never persisted")

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	select {
	case <-signals:
		_ = server.Shutdown(context.Background())
	case <-server.Done():
	}
}

func sanitizeDaemonString(value string) string {
	if len(value) > 64 {
		return value[:64]
	}
	return value
}
