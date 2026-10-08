package keepass

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	kp "github.com/tobischo/gokeepasslib/v3"
)

func runKeePassXC(t *testing.T, cli, password string, args ...string) (string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, cli, args...)
	cmd.Stdin = strings.NewReader(password + "\n")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = io.Discard
	err := cmd.Run()
	if ctx.Err() != nil {
		t.Fatal("keepassxc-cli timed out")
	}
	return stdout.String(), err
}

func TestKeePassStoreInterop(t *testing.T) {
	cli := os.Getenv("KEEPASSXC_CLI")
	if cli == "" {
		t.Skip("KEEPASSXC_CLI not set")
	}
	store := testStore()
	dir := t.TempDir()
	path := filepath.Join(dir, "interop.kdbx")
	binding, err := store.Create(context.Background(), path, testVaultID, []byte(testMaster))
	if err != nil {
		t.Fatal(err)
	}
	op, err := store.Open(context.Background(), binding, []byte(testMaster))
	if err != nil {
		t.Fatal(err)
	}
	digest := testDigest(0x55)
	record := accountRecord(testVaultID, testAccountID, "interop-account", 1)
	record.Mnemonic = []byte("synthetic interop mnemonic")
	record.PrivateKey = []byte("0x" + testDigest(0x66))
	record.Passphrase = []byte(" synthetic passphrase ")
	file := fileRecord(testVaultID, testAccountID, kindKeystoreV3, digest, []byte("file-password"))
	if _, err := op.Upsert(context.Background(), []Record{record, file}); err != nil {
		t.Fatal(err)
	}
	op.Close()
	out, err := runKeePassXC(t, cli, testMaster, "export", "-f", "xml", path)
	if err != nil {
		t.Fatalf("keepassxc export failed: %v", err)
	}
	for _, needle := range []string{"interop-account", "Bloco.Mnemonic", "Bloco.VaultID", testVaultID, digest} {
		if !strings.Contains(out, needle) {
			t.Fatalf("keepassxc export missing %s", needle)
		}
	}
	out, err = runKeePassXC(t, cli, testMaster, "show", "-s", "-a", "Password", "-a", "Bloco.PrivateKey", path, "interop-account")
	if err != nil {
		t.Fatalf("keepassxc show failed: %v", err)
	}
	if !strings.Contains(out, string(record.Password)) || !strings.Contains(out, string(record.PrivateKey)) {
		t.Fatal("keepassxc did not decode protected record fields")
	}
	if _, err := runKeePassXC(t, cli, testMaster, "edit", "-t", "interop-renamed", path, "interop-account"); err != nil {
		t.Fatalf("keepassxc resave failed: %v", err)
	}
	op2, err := store.Open(context.Background(), binding, []byte(testMaster))
	if err != nil {
		t.Fatalf("store cannot open keepassxc-resaved vault: %v", err)
	}
	defer op2.Close()
	var password []byte
	err = op2.WithPassword(context.Background(), record.Ref, 1, func(value []byte) error {
		password = append([]byte(nil), value...)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(password, record.Password) {
		t.Fatal("password not preserved across keepassxc resave")
	}
	list, err := op2.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	foundFile := false
	for _, meta := range list {
		if meta.Ref.ItemID != itemIDAccount && meta.Digest == digest {
			foundFile = true
		}
	}
	if !foundFile {
		t.Fatal("file record missing after keepassxc resave")
	}
	var filePassword []byte
	err = op2.WithFilePassword(context.Background(), kindKeystoreV3, digest, func(value []byte) error {
		filePassword = append([]byte(nil), value...)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(filePassword) != "file-password" {
		t.Fatal("file password not preserved across keepassxc resave")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	decoded := kp.NewDatabase()
	decoded.Credentials = passwordCredentials([]byte(testMaster))
	if err := kp.NewDecoderWithLimits(bytes.NewReader(raw), kp.DefaultDecodeLimits()).Decode(decoded); err != nil {
		t.Fatalf("resaved vault failed bounded decode: %v", err)
	}
	defer destroyDatabase(decoded)
	var entry *kp.Entry
	for _, group := range decoded.Content.Root.Groups[0].Groups {
		for i := range group.Entries {
			if group.Entries[i].GetContent("Bloco.AccountID") == testAccountID &&
				group.Entries[i].GetContent("Bloco.ItemID") == itemIDAccount {
				entry = &group.Entries[i]
			}
		}
	}
	if entry == nil {
		t.Fatal("account entry missing from resaved database")
	}
	for key, want := range map[string]string{
		"Password":              string(record.Password),
		"Bloco.Mnemonic":        string(record.Mnemonic),
		"Bloco.PrivateKey":      string(record.PrivateKey),
		"Bloco.BIP39Passphrase": string(record.Passphrase),
	} {
		value := entry.Get(key)
		if value == nil {
			t.Fatalf("protected attribute %s missing after resave", key)
		}
		if !value.Value.Protected.Bool {
			t.Fatalf("attribute %s lost protected flag", key)
		}
		if err := decoded.UnlockProtectedEntries(); err != nil {
			t.Fatalf("unlock protected entries: %v", err)
		}
		got := value.Value.Content
		if err := decoded.LockProtectedEntries(); err != nil {
			t.Fatalf("lock protected entries: %v", err)
		}
		if got != want {
			t.Fatalf("attribute %s value mismatch", key)
		}
	}
}
