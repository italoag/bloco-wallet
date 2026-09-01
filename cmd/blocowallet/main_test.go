package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestEnsurePrivateDirectoryDoesNotChmodExternalParent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission semantics")
	}
	appDir := t.TempDir()
	externalDir := t.TempDir()
	if err := os.Chmod(externalDir, 0755); err != nil {
		t.Fatal(err)
	}

	if err := ensurePrivateDirectory(externalDir, appDir); err == nil {
		t.Fatal("expected insecure external directory to be rejected")
	}
	info, err := os.Stat(externalDir)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0755 {
		t.Fatalf("external directory mode changed to %o", info.Mode().Perm())
	}
}

func TestEnsurePrivateDirectorySecuresAppOwnedDirectory(t *testing.T) {
	appDir := t.TempDir()
	walletDir := filepath.Join(appDir, "keystore")
	if err := os.MkdirAll(walletDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := ensurePrivateDirectory(walletDir, appDir); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(walletDir)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0700 {
			t.Fatalf("wallet directory mode is %o", info.Mode().Perm())
		}
	}
}

func TestReleaseSmokeOpensAndReopensSQLite(t *testing.T) {
	if err := runReleaseSmoke(); err != nil {
		t.Fatal(err)
	}
}
