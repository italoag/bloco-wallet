package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadOrCreateSourceIdentityKeyIsStableAndPrivate(t *testing.T) {
	root := t.TempDir()
	first, err := loadOrCreateSourceIdentityKey(root)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(first)
	second, err := loadOrCreateSourceIdentityKey(root)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(second)
	if !bytes.Equal(first, second) {
		t.Fatal("source identity key changed across loads")
	}
	info, err := os.Stat(filepath.Join(root, sourceIdentityKeyFile))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("source identity key mode is %04o", info.Mode().Perm())
	}
}

func TestLoadOrCreateSourceIdentityKeyRejectsUnsafeFiles(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, sourceIdentityKeyFile)
	if err := os.WriteFile(path, []byte("short"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadOrCreateSourceIdentityKey(root); err == nil {
		t.Fatal("short source identity key was accepted")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "target")
	if err := os.WriteFile(target, make([]byte, 32), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err == nil {
		if _, err := loadOrCreateSourceIdentityKey(root); err == nil {
			t.Fatal("symlink source identity key was accepted")
		}
	}
}
