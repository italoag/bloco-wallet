package wallet

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

type fakeExportFile struct {
	name     string
	chmodErr error
	writeErr error
	syncErr  error
	closeErr error
	onClose  func()
}

func (file *fakeExportFile) Name() string            { return file.name }
func (file *fakeExportFile) Chmod(os.FileMode) error { return file.chmodErr }
func (file *fakeExportFile) Write(data []byte) (int, error) {
	if file.writeErr != nil {
		return 0, file.writeErr
	}
	return len(data), nil
}
func (file *fakeExportFile) Sync() error { return file.syncErr }
func (file *fakeExportFile) Close() error {
	if file.onClose != nil {
		file.onClose()
	}
	return file.closeErr
}

type fakeExportDirectory struct {
	syncErr  error
	closeErr error
}

func (directory *fakeExportDirectory) Sync() error  { return directory.syncErr }
func (directory *fakeExportDirectory) Close() error { return directory.closeErr }

func TestEncryptedAccountExportRejectsUnsafeRequests(t *testing.T) {
	vault, repository, _ := newTestVault(t)
	password := []byte("Strong vault password 1!")
	newPassword := []byte("Strong export password 2!")
	active := activateTestAccount(t, vault, password)
	handle, err := vault.Unlock(context.Background(), active.AccountID, password)
	if err != nil {
		t.Fatal(err)
	}
	request := EncryptedAccountExportRequest{
		Handle:             handle,
		Destination:        filepath.Join(t.TempDir(), "account.bloco"),
		CurrentPassword:    password,
		NewPassword:        newPassword,
		ConfirmNewPassword: append([]byte(nil), newPassword...),
	}
	invalid := request
	invalid.Destination = "relative.bloco"
	if err := vault.ExportEncryptedAccount(context.Background(), invalid); err == nil {
		t.Fatal("relative export path was accepted")
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := vault.ExportEncryptedAccount(cancelled, request); !errors.Is(err, context.Canceled) {
		t.Fatalf("export ignored context: %v", err)
	}
	invalid = request
	invalid.ConfirmNewPassword = []byte("Different export password 3!")
	if err := vault.ExportEncryptedAccount(context.Background(), invalid); err == nil {
		t.Fatal("mismatched password confirmation was accepted")
	}
	invalid = request
	invalid.CurrentPassword = []byte("Wrong vault password 1!")
	if err := vault.ExportEncryptedAccount(context.Background(), invalid); err == nil {
		t.Fatal("wrong storage password authenticated export")
	}
	invalid = request
	invalid.NewPassword = []byte("short")
	invalid.ConfirmNewPassword = []byte("short")
	if err := vault.ExportEncryptedAccount(context.Background(), invalid); err == nil {
		t.Fatal("short export password was accepted")
	}
	account, err := repository.GetAccount(context.Background(), active.AccountID)
	if err != nil {
		t.Fatal(err)
	}
	account.Capabilities &^= CapabilityExportSecret
	if err := repository.UpdateAccount(context.Background(), account); err != nil {
		t.Fatal(err)
	}
	if err := vault.ExportEncryptedAccount(context.Background(), request); !errors.Is(err, ErrCapabilityDenied) {
		t.Fatalf("export ignored capability policy: %v", err)
	}
}

func TestWriteExclusiveAtomicFailurePaths(t *testing.T) {
	fault := errors.New("filesystem fault")
	parent := t.TempDir()
	destination := filepath.Join(parent, "export.bloco")
	baseOperations := func() atomicExportOperations {
		return atomicExportOperations{
			lstat: os.Lstat,
			createTemp: func(string, string) (atomicExportFile, error) {
				return &fakeExportFile{name: filepath.Join(parent, "temporary")}, nil
			},
			link:   func(string, string) error { return nil },
			remove: func(string) error { return nil },
			openDir: func(string) (atomicExportDirectory, error) {
				return &fakeExportDirectory{}, nil
			},
		}
	}
	tests := []func(*atomicExportOperations, context.CancelFunc){
		func(operations *atomicExportOperations, _ context.CancelFunc) {
			operations.lstat = func(path string) (os.FileInfo, error) {
				if path == destination {
					return nil, fault
				}
				return os.Lstat(path)
			}
		},
		func(operations *atomicExportOperations, _ context.CancelFunc) {
			operations.createTemp = func(string, string) (atomicExportFile, error) { return nil, fault }
		},
		func(operations *atomicExportOperations, _ context.CancelFunc) {
			operations.createTemp = func(string, string) (atomicExportFile, error) {
				return &fakeExportFile{name: "temporary", chmodErr: fault}, nil
			}
		},
		func(operations *atomicExportOperations, _ context.CancelFunc) {
			operations.createTemp = func(string, string) (atomicExportFile, error) {
				return &fakeExportFile{name: "temporary", writeErr: fault}, nil
			}
		},
		func(operations *atomicExportOperations, _ context.CancelFunc) {
			operations.createTemp = func(string, string) (atomicExportFile, error) {
				return &fakeExportFile{name: "temporary", syncErr: fault}, nil
			}
		},
		func(operations *atomicExportOperations, _ context.CancelFunc) {
			operations.createTemp = func(string, string) (atomicExportFile, error) {
				return &fakeExportFile{name: "temporary", closeErr: fault}, nil
			}
		},
		func(operations *atomicExportOperations, cancel context.CancelFunc) {
			operations.createTemp = func(string, string) (atomicExportFile, error) {
				return &fakeExportFile{name: "temporary", onClose: cancel}, nil
			}
		},
		func(operations *atomicExportOperations, _ context.CancelFunc) {
			operations.link = func(string, string) error { return fault }
		},
		func(operations *atomicExportOperations, _ context.CancelFunc) {
			operations.remove = func(string) error { return fault }
		},
		func(operations *atomicExportOperations, _ context.CancelFunc) {
			operations.openDir = func(string) (atomicExportDirectory, error) { return nil, fault }
		},
		func(operations *atomicExportOperations, _ context.CancelFunc) {
			operations.openDir = func(string) (atomicExportDirectory, error) { return &fakeExportDirectory{syncErr: fault}, nil }
		},
		func(operations *atomicExportOperations, _ context.CancelFunc) {
			operations.openDir = func(string) (atomicExportDirectory, error) { return &fakeExportDirectory{closeErr: fault}, nil }
		},
	}
	for index, mutate := range tests {
		t.Run(string(rune('a'+index)), func(t *testing.T) {
			operations := baseOperations()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			mutate(&operations, cancel)
			if err := writeExclusiveAtomicWithOperations(ctx, destination, []byte("data"), 0600, operations); err == nil {
				t.Fatal("filesystem fault was ignored")
			}
		})
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := writeExclusiveAtomic(cancelled, destination, []byte("data"), 0600); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled atomic write returned %v", err)
	}
}

func TestWriteExclusiveAtomicRejectsInvalidParents(t *testing.T) {
	missingParent := filepath.Join(t.TempDir(), "missing", "export.bloco")
	if err := writeExclusiveAtomic(context.Background(), missingParent, []byte("data"), 0600); err == nil {
		t.Fatal("missing parent was accepted")
	}
	parentFile := filepath.Join(t.TempDir(), "parent")
	if err := os.WriteFile(parentFile, []byte("file"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := writeExclusiveAtomic(context.Background(), filepath.Join(parentFile, "export.bloco"), []byte("data"), 0600); err == nil {
		t.Fatal("non-directory parent was accepted")
	}
	realParent := t.TempDir()
	symlinkParent := filepath.Join(t.TempDir(), "linked")
	if err := os.Symlink(realParent, symlinkParent); err == nil {
		if err := writeExclusiveAtomic(context.Background(), filepath.Join(symlinkParent, "export.bloco"), []byte("data"), 0600); err == nil {
			t.Fatal("symlink parent was accepted")
		}
	}
}
