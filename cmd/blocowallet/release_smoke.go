package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"blocowallet/internal/storage"
	"blocowallet/pkg/config"
)

func runReleaseSmoke() error {
	root, err := os.MkdirTemp("", "blocowallet-release-smoke-")
	if err != nil {
		return fmt.Errorf("create smoke directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(root) }()
	if err := os.Chmod(root, 0o700); err != nil {
		return fmt.Errorf("secure smoke directory: %w", err)
	}
	configuration := &config.Config{
		AppDir: root, DatabasePath: filepath.Join(root, "smoke.db"),
		Database: config.DatabaseConfig{Type: "sqlite"},
	}
	repository, err := storage.NewVaultRepository(configuration)
	if err != nil {
		return fmt.Errorf("open smoke repository: %w", err)
	}
	if err := repository.PutVaultMetadata(context.Background(), "release_smoke", "ok"); err != nil {
		_ = repository.Close()
		return fmt.Errorf("write smoke repository: %w", err)
	}
	if err := repository.Close(); err != nil {
		return fmt.Errorf("close smoke repository: %w", err)
	}
	reopened, err := storage.NewVaultRepository(configuration)
	if err != nil {
		return fmt.Errorf("reopen smoke repository: %w", err)
	}
	defer func() { _ = reopened.Close() }()
	value, err := reopened.GetVaultMetadata(context.Background(), "release_smoke")
	if err != nil {
		return fmt.Errorf("read smoke repository: %w", err)
	}
	if value != "ok" {
		return fmt.Errorf("smoke repository round-trip mismatch")
	}
	return nil
}
