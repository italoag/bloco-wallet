package config

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestAtomicConfigCreateAndReplace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := writeAtomicConfig(path, []byte("value = 1\n"), false); err != nil {
		t.Fatal(err)
	}
	if err := writeAtomicConfig(path, []byte("value = 2\n"), false); !errors.Is(err, os.ErrExist) {
		t.Fatalf("exclusive create returned %v", err)
	}
	if err := writeAtomicConfig(path, []byte("value = 2\n"), true); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "value = 2\n" {
		t.Fatalf("atomic replace wrote %q", data)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0600 {
			t.Fatalf("config mode is %o", info.Mode().Perm())
		}
	}
}

func TestAtomicConfigRejectsUnsafeInputs(t *testing.T) {
	for _, test := range []struct {
		path string
		data []byte
	}{
		{"relative.toml", []byte("value = 1\n")},
		{filepath.Join(t.TempDir(), "empty.toml"), nil},
		{filepath.Join(t.TempDir(), "large.toml"), make([]byte, (1<<20)+1)},
	} {
		if err := writeAtomicConfig(test.path, test.data, false); err == nil {
			t.Fatalf("unsafe config write was accepted: %s", test.path)
		}
	}
	if IsConfigCommitted(&ConfigCommittedWarning{}) != true {
		t.Fatal("committed warning was not detectable")
	}
}
