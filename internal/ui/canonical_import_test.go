package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"blocowallet/internal/wallet"

	"github.com/ethereum/go-ethereum/accounts/keystore"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/google/uuid"
)

func TestReadCanonicalKeystoreBatchUsesRegularJSONFiles(t *testing.T) {
	root := t.TempDir()
	password := []byte("source password")
	for index := range 2 {
		key, err := crypto.GenerateKey()
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := keystore.EncryptKey(&keystore.Key{
			Id:         uuid.New(),
			Address:    crypto.PubkeyToAddress(key.PublicKey),
			PrivateKey: key,
		}, string(password), keystore.LightScryptN, keystore.LightScryptP)
		if err != nil {
			t.Fatal(err)
		}
		keystorePath := filepath.Join(root, string(rune('a'+index))+".json")
		if err := os.WriteFile(keystorePath, encoded, 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(keystorePath+".password", password, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "ignored.txt"), []byte("ignored"), 0600); err != nil {
		t.Fatal(err)
	}
	items, err := readCanonicalKeystoreBatch(root, "Imported", nil, true)
	if err != nil {
		t.Fatal(err)
	}
	defer clearCanonicalBatchItems(items)
	if len(items) != 3 {
		t.Fatalf("expected three classified batch items, got %d", len(items))
	}
	valid := 0
	invalid := 0
	for _, item := range items {
		if item.PreflightErr != nil {
			invalid++
			continue
		}
		if _, err := wallet.PreviewKeystoreImport(item.KeystoreJSON, item.SourcePassword); err != nil {
			invalid++
		} else {
			valid++
		}
	}
	if valid != 2 || invalid != 1 {
		t.Fatalf("unexpected content classification: %d valid, %d invalid", valid, invalid)
	}
	emptyPasswordItems, err := readCanonicalKeystoreBatch(root, "Imported", nil, false)
	if err != nil {
		t.Fatal(err)
	}
	defer clearCanonicalBatchItems(emptyPasswordItems)
	if len(emptyPasswordItems) != 3 || len(emptyPasswordItems[0].SourcePassword) != 0 {
		t.Fatal("batch did not preserve empty common source password")
	}
}

func TestCanonicalImportViewSanitizesUntrustedResults(t *testing.T) {
	model := &CLIModel{canonicalImport: &canonicalImportState{
		resultLines: []string{"evil\x1b]52;c;secret\x07\r\nnext"},
	}}
	view := model.viewCanonicalImport()
	if strings.ContainsAny(view, "\x1b\a\r") || strings.Contains(view, "\u009b") {
		t.Fatalf("canonical import view retained terminal controls: %q", view)
	}
}

func TestReadCanonicalKeystoreBatchRejectsSymlinkDirectory(t *testing.T) {
	root := t.TempDir()
	link := filepath.Join(t.TempDir(), "linked")
	if err := os.Symlink(root, link); err == nil {
		if _, err := readCanonicalKeystoreBatch(link, "Imported", []byte("password"), false); err == nil {
			t.Fatal("symlink batch directory was accepted")
		}
	}
}
