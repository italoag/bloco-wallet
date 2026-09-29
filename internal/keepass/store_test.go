package keepass

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	kp "github.com/tobischo/gokeepasslib/v3"
)

const (
	testVaultID    = "11111111-1111-4111-8111-111111111111"
	testVaultID2   = "22222222-2222-4222-8222-222222222222"
	testAccountID  = "33333333-3333-4333-8333-333333333333"
	testAccountID2 = "44444444-4444-4444-8444-444444444444"
	testMaster     = "synthetic-master-password"
)

func testStore() *Store {
	return NewStore(Options{})
}

func openVault(t *testing.T, store *Store, binding Binding) *Operation {
	t.Helper()
	op, err := store.Open(context.Background(), binding, []byte(testMaster))
	if err != nil {
		t.Fatal(err)
	}
	return op
}

func accountRecord(vaultID, accountID, title string, generation uint64) Record {
	return Record{
		Ref:        Ref{VaultID: vaultID, AccountID: accountID, ItemID: itemIDAccount},
		Title:      title,
		Address:    "0x00000000000000000000000000000000000000aa",
		SecretType: "mnemonic",
		Generation: generation,
		Kind:       kindAccount,
		Password:   []byte("account-password-" + title),
		Mnemonic:   []byte("synthetic mnemonic for " + title),
	}
}

func fileRecord(vaultID, accountID, kind, digest string, password []byte) Record {
	return Record{
		Ref:      Ref{VaultID: vaultID, AccountID: accountID, ItemID: fileItemPrefix + kind + ":" + digest},
		FileName: "artifact-" + kind + ".json",
		Kind:     kind,
		Digest:   digest,
		Password: password,
	}
}

func testDigest(seed byte) string {
	return hex.EncodeToString(bytes.Repeat([]byte{seed}, 32))
}

func createVault(t *testing.T, store *Store) (string, Binding) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "vault.kdbx")
	binding, err := store.Create(context.Background(), path, testVaultID, []byte(testMaster))
	if err != nil {
		t.Fatalf("create vault: %v", err)
	}
	return path, binding
}

func decodeVaultFile(t *testing.T, path string) *kp.Database {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	database := kp.NewDatabase()
	database.Credentials = kp.NewPasswordCredentials(testMaster)
	if err := kp.NewDecoderWithLimits(bytes.NewReader(data), kp.DefaultDecodeLimits()).Decode(database); err != nil {
		t.Fatalf("decode vault: %v", err)
	}
	if err := database.UnlockProtectedEntries(); err != nil {
		t.Fatal(err)
	}
	return database
}

func TestStoreCreateInspectOpen(t *testing.T) {
	store := testStore()
	path, binding := createVault(t, store)
	targetID, err := store.Inspect(context.Background(), path, []byte(testMaster))
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	if targetID != binding.TargetID {
		t.Fatal("inspect returned wrong target")
	}
	if _, err := store.Inspect(context.Background(), path, []byte("wrong-master")); !errors.Is(err, ErrAuthentication) {
		t.Fatalf("expected ErrAuthentication, got %v", err)
	}
	op, err := store.Open(context.Background(), binding, []byte(testMaster))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	op.Close()
	op.Close()
	wrong := binding
	wrong.TargetID = testAccountID2
	if _, err := store.Open(context.Background(), wrong, []byte(testMaster)); !errors.Is(err, ErrWrongDatabase) {
		t.Fatalf("expected ErrWrongDatabase, got %v", err)
	}
	if _, err := store.Open(context.Background(), binding, []byte("wrong-master")); !errors.Is(err, ErrAuthentication) {
		t.Fatalf("expected ErrAuthentication, got %v", err)
	}
}

func TestStoreCreateRefusesExisting(t *testing.T) {
	store := testStore()
	path, _ := createVault(t, store)
	if _, err := store.Create(context.Background(), path, testVaultID, []byte(testMaster)); err == nil {
		t.Fatal("create overwrote an existing vault file")
	}
}

func TestStoreInspectRejectsPersonalFile(t *testing.T) {
	database := kp.NewDatabase(kp.WithDatabaseKDBXVersion40())
	database.Credentials = kp.NewPasswordCredentials(testMaster)
	var encoded bytes.Buffer
	if err := database.LockProtectedEntries(); err != nil {
		t.Fatal(err)
	}
	if err := kp.NewEncoder(&encoded).Encode(database); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "personal.kdbx")
	if err := os.WriteFile(path, encoded.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	store := testStore()
	if _, err := store.Inspect(context.Background(), path, []byte(testMaster)); !errors.Is(err, ErrUnsupportedDatabase) {
		t.Fatalf("expected ErrUnsupportedDatabase, got %v", err)
	}
	binding := Binding{Path: path, TargetID: testVaultID2, VaultID: testVaultID}
	if _, err := store.Open(context.Background(), binding, []byte(testMaster)); !errors.Is(err, ErrUnsupportedDatabase) {
		t.Fatalf("expected ErrUnsupportedDatabase for open, got %v", err)
	}
}

func TestStorePathValidation(t *testing.T) {
	store := testStore()
	root := t.TempDir()
	if _, err := store.Create(context.Background(), "relative/vault.kdbx", testVaultID, []byte(testMaster)); err == nil {
		t.Fatal("relative path accepted")
	}
	if _, err := store.Create(context.Background(), filepath.Join(root, "vault.bin"), testVaultID, []byte(testMaster)); err == nil {
		t.Fatal("non-kdbx extension accepted")
	}
	if _, err := store.Create(context.Background(), filepath.Join(root, "missing", "vault.kdbx"), testVaultID, []byte(testMaster)); err == nil {
		t.Fatal("missing parent accepted")
	}
	if _, err := store.Create(context.Background(), filepath.Join(root, "vault.kdbx"), "not-a-uuid", []byte(testMaster)); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("expected ErrInvalidRecord, got %v", err)
	}
}

func TestStoreRejectsSymlinkAndHardlink(t *testing.T) {
	store := testStore()
	path, binding := createVault(t, store)
	dir := t.TempDir()
	link := filepath.Join(dir, "linked.kdbx")
	if err := os.Symlink(path, link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	linked := binding
	linked.Path = link
	if _, err := store.Open(context.Background(), linked, []byte(testMaster)); err == nil {
		t.Fatal("symlink vault opened")
	}
	hard := filepath.Join(dir, "hard.kdbx")
	if err := os.Link(path, hard); err != nil {
		t.Skipf("hardlink unsupported: %v", err)
	}
	hardBinding := binding
	hardBinding.Path = hard
	if _, err := store.Open(context.Background(), hardBinding, []byte(testMaster)); err == nil {
		t.Fatal("hardlinked vault opened")
	}
}

func TestStoreUpsertListWithPassword(t *testing.T) {
	store := testStore()
	_, binding := createVault(t, store)
	op, err := store.Open(context.Background(), binding, []byte(testMaster))
	if err != nil {
		t.Fatal(err)
	}
	defer op.Close()
	first := accountRecord(testVaultID, testAccountID, "café account\nline", 1)
	second := accountRecord(testVaultID, testAccountID2, "other account", 1)
	second.Address = first.Address
	if _, err := op.Upsert(context.Background(), []Record{first, second}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	list, err := op.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 records, got %d", len(list))
	}
	byAccount := map[string]Metadata{}
	for _, meta := range list {
		byAccount[meta.Ref.AccountID] = meta
	}
	if byAccount[testAccountID].Title != first.Title || byAccount[testAccountID].Generation != 1 {
		t.Fatalf("metadata mismatch: %+v", byAccount[testAccountID])
	}
	if byAccount[testAccountID].Address != first.Address || byAccount[testAccountID2].Address != first.Address {
		t.Fatal("shared address accounts not preserved")
	}
	var retrieved []byte
	err = op.WithPassword(context.Background(), first.Ref, 1, func(password []byte) error {
		retrieved = append([]byte(nil), password...)
		return nil
	})
	if err != nil {
		t.Fatalf("with password: %v", err)
	}
	if !bytes.Equal(retrieved, first.Password) {
		t.Fatal("password round-trip mismatch")
	}
	updated := accountRecord(testVaultID, testAccountID, "renamed", 1)
	if _, err := op.Upsert(context.Background(), []Record{updated}); err != nil {
		t.Fatalf("same-generation correction: %v", err)
	}
	list, err = op.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, meta := range list {
		if meta.Ref.AccountID == testAccountID && meta.Title != "renamed" {
			t.Fatal("title update not applied")
		}
	}
}

func TestStoreGenerationAndDuplicates(t *testing.T) {
	store := testStore()
	_, binding := createVault(t, store)
	op, err := store.Open(context.Background(), binding, []byte(testMaster))
	if err != nil {
		t.Fatal(err)
	}
	defer op.Close()
	record := accountRecord(testVaultID, testAccountID, "gen test", 3)
	if _, err := op.Upsert(context.Background(), []Record{record}); err != nil {
		t.Fatal(err)
	}
	stale := accountRecord(testVaultID, testAccountID, "gen test", 2)
	if _, err := op.Upsert(context.Background(), []Record{stale}); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected ErrConflict for stale generation, got %v", err)
	}
	batch := []Record{accountRecord(testVaultID, testAccountID, "a", 4), accountRecord(testVaultID, testAccountID, "b", 4)}
	if _, err := op.Upsert(context.Background(), batch); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("expected ErrInvalidRecord for duplicate ref, got %v", err)
	}
	err = op.WithPassword(context.Background(), record.Ref, 2, func([]byte) error { return nil })
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("expected ErrConflict for stale read generation, got %v", err)
	}
	err = op.WithPassword(context.Background(), Ref{VaultID: testVaultID, AccountID: testAccountID2, ItemID: itemIDAccount}, 1, func([]byte) error { return nil })
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestStoreFileRecords(t *testing.T) {
	store := testStore()
	_, binding := createVault(t, store)
	op, err := store.Open(context.Background(), binding, []byte(testMaster))
	if err != nil {
		t.Fatal(err)
	}
	defer op.Close()
	digest := testDigest(0x11)
	if _, err := op.Upsert(context.Background(), []Record{
		accountRecord(testVaultID, testAccountID, "account", 1),
		fileRecord(testVaultID, testAccountID, kindKeystoreV3, digest, nil),
	}); err != nil {
		t.Fatal(err)
	}
	var got []byte
	called := false
	err = op.WithFilePassword(context.Background(), kindKeystoreV3, digest, func(password []byte) error {
		called = true
		got = append([]byte(nil), password...)
		return nil
	})
	if err != nil || !called {
		t.Fatalf("with file password: %v", err)
	}
	if len(got) != 0 {
		t.Fatal("empty file password not preserved")
	}
	if _, err := op.Upsert(context.Background(), []Record{
		fileRecord(testVaultID2, testAccountID2, kindKeystoreV3, digest, []byte("different")),
	}); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("expected ErrInvalidRecord for foreign vault write, got %v", err)
	}
	op.Close()
	second := binding
	second.VaultID = testVaultID2
	op2, err := store.Open(context.Background(), second, []byte(testMaster))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := op2.Upsert(context.Background(), []Record{
		fileRecord(testVaultID2, testAccountID2, kindKeystoreV3, digest, []byte("different")),
	}); err != nil {
		t.Fatalf("second namespace upsert: %v", err)
	}
	op2.Close()
	op, err = store.Open(context.Background(), binding, []byte(testMaster))
	if err != nil {
		t.Fatal(err)
	}
	defer op.Close()
	if err := op.WithFilePassword(context.Background(), kindKeystoreV3, digest, func([]byte) error { return nil }); !errors.Is(err, ErrAmbiguous) {
		t.Fatalf("expected ErrAmbiguous, got %v", err)
	}
	if err := op.WithFilePassword(context.Background(), kindKeystoreV3, testDigest(0x99), func([]byte) error { return nil }); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	list, err := op.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	fileMeta := 0
	for _, meta := range list {
		if meta.Ref.ItemID != itemIDAccount {
			fileMeta++
			if meta.Kind != kindKeystoreV3 || meta.Digest != digest || meta.FileName == "" {
				t.Fatalf("file metadata mismatch: %+v", meta)
			}
		}
	}
	if fileMeta != 2 {
		t.Fatalf("expected 2 file records, got %d", fileMeta)
	}
	sameDigest := testDigest(0x77)
	if _, err := op.Upsert(context.Background(), []Record{
		fileRecord(testVaultID, testAccountID, kindBlocoCipher, sameDigest, []byte("shared")),
	}); err != nil {
		t.Fatal(err)
	}
	op.Close()
	op2b, err := store.Open(context.Background(), second, []byte(testMaster))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := op2b.Upsert(context.Background(), []Record{
		fileRecord(testVaultID2, testAccountID2, kindBlocoCipher, sameDigest, []byte("shared")),
	}); err != nil {
		t.Fatal(err)
	}
	op2b.Close()
	op, err = store.Open(context.Background(), binding, []byte(testMaster))
	if err != nil {
		t.Fatal(err)
	}
	defer op.Close()
	var shared []byte
	if err := op.WithFilePassword(context.Background(), kindBlocoCipher, sameDigest, func(p []byte) error {
		shared = append([]byte(nil), p...)
		return nil
	}); err != nil {
		t.Fatalf("deduplicated cross-namespace file password: %v", err)
	}
	if string(shared) != "shared" {
		t.Fatal("shared file password mismatch")
	}
}

func TestStoreAtomicBatch(t *testing.T) {
	store := testStore()
	path, binding := createVault(t, store)
	op := openVault(t, store, binding)
	if _, err := op.Upsert(context.Background(), []Record{accountRecord(testVaultID, testAccountID, "first", 3)}); err != nil {
		t.Fatal(err)
	}
	mixed := []Record{
		accountRecord(testVaultID, testAccountID2, "new-account", 1),
		accountRecord(testVaultID, testAccountID, "stale-update", 2),
	}
	if _, err := op.Upsert(context.Background(), mixed); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected ErrConflict on stale generation, got %v", err)
	}
	list, err := op.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("partial batch visible in session, %d records", len(list))
	}
	op.Close()
	op2, err := store.Open(context.Background(), binding, []byte(testMaster))
	if err != nil {
		t.Fatal(err)
	}
	list, err = op2.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("partial batch persisted, %d records", len(list))
	}
	op2.Close()
	dataBefore, _ := os.ReadFile(path)
	op3 := openVault(t, store, binding)
	defer op3.Close()
	retry := []Record{
		accountRecord(testVaultID, testAccountID2, "new-account", 1),
		accountRecord(testVaultID, testAccountID, "fresh-update", 4),
	}
	if _, err := op3.Upsert(context.Background(), retry); err != nil {
		t.Fatalf("retry after clean state failed: %v", err)
	}
	dataAfter, _ := os.ReadFile(path)
	if bytes.Equal(dataBefore, dataAfter) {
		t.Fatal("retry did not persist")
	}
}

func TestStoreUpsertOrderRegression(t *testing.T) {
	store := testStore()
	_, binding := createVault(t, store)
	op := openVault(t, store, binding)
	if _, err := op.Upsert(context.Background(), []Record{accountRecord(testVaultID, testAccountID, "orig", 1)}); err != nil {
		t.Fatal(err)
	}
	updated := accountRecord(testVaultID, testAccountID, "updated", 2)
	updated.Password = []byte("rotated-password")
	added := accountRecord(testVaultID, testAccountID2, "new", 1)
	if _, err := op.Upsert(context.Background(), []Record{added, updated}); err != nil {
		t.Fatal(err)
	}
	op.Close()
	op2 := openVault(t, store, binding)
	defer op2.Close()
	list, err := op2.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 records, got %d", len(list))
	}
	var password []byte
	ref := Ref{VaultID: testVaultID, AccountID: testAccountID, ItemID: itemIDAccount}
	if err := op2.WithPassword(context.Background(), ref, 2, func(value []byte) error {
		password = append([]byte(nil), value...)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if string(password) != "rotated-password" {
		t.Fatal("existing entry update lost")
	}
	foundNew := false
	for _, meta := range list {
		if meta.Ref.AccountID == testAccountID2 {
			foundNew = true
		}
	}
	if !foundNew {
		t.Fatal("new entry not appended")
	}
}

func TestStorePreservesEntryFields(t *testing.T) {
	store := testStore()
	_, binding := createVault(t, store)
	op := openVault(t, store, binding)
	record := accountRecord(testVaultID, testAccountID, "account", 1)
	record.Mnemonic = []byte("seed words")
	if _, err := op.Upsert(context.Background(), []Record{record}); err != nil {
		t.Fatal(err)
	}
	op.mu.Lock()
	var entry *kp.Entry
	forEachEntry(&op.database.Content.Root.Groups[0], func(e *kp.Entry) {
		if managedEntry(e) && e.GetContent("Bloco.AccountID") == testAccountID {
			entry = e
		}
	})
	if entry == nil {
		op.mu.Unlock()
		t.Fatal("managed entry missing")
	}
	entry.Values = append(entry.Values,
		kp.ValueData{Key: "Notes", Value: kp.V{Content: "keep me"}},
		kp.ValueData{Key: "URL", Value: kp.V{Content: "https://example.test"}})
	entry.Tags = "important"
	entry.Histories = append(entry.Histories, kp.History{Entries: []kp.Entry{kp.NewEntry()}})
	op.mu.Unlock()
	if _, err := op.commit(context.Background()); err != nil {
		t.Fatal(err)
	}
	op.Close()
	op2, err := store.Open(context.Background(), binding, []byte(testMaster))
	if err != nil {
		t.Fatal(err)
	}
	op2.mu.Lock()
	var updated *kp.Entry
	forEachEntry(&op2.database.Content.Root.Groups[0], func(e *kp.Entry) {
		if managedEntry(e) && e.GetContent("Bloco.AccountID") == testAccountID {
			updated = e
		}
	})
	if updated == nil {
		op2.mu.Unlock()
		t.Fatal("managed entry missing after commit")
	}
	if got := updated.GetContent("Notes"); got != "keep me" {
		t.Fatalf("Notes field lost: %q", got)
	}
	if got := updated.GetContent("URL"); got != "https://example.test" {
		t.Fatalf("URL field lost: %q", got)
	}
	if updated.Tags != "important" {
		t.Fatal("tags lost")
	}
	if len(updated.Histories) != 1 {
		t.Fatal("history lost")
	}
	op2.mu.Unlock()
	updatedRecord := accountRecord(testVaultID, testAccountID, "renamed", 2)
	updatedRecord.Mnemonic = nil
	if _, err := op2.Upsert(context.Background(), []Record{updatedRecord}); err != nil {
		t.Fatal(err)
	}
	op2.Close()
	op3, err := store.Open(context.Background(), binding, []byte(testMaster))
	if err != nil {
		t.Fatal(err)
	}
	defer op3.Close()
	op3.mu.Lock()
	forEachEntry(&op3.database.Content.Root.Groups[0], func(e *kp.Entry) {
		if !managedEntry(e) || e.GetContent("Bloco.AccountID") != testAccountID {
			return
		}
		if e.GetContent("Notes") != "keep me" {
			t.Error("Notes lost on update")
		}
		if e.GetContent("URL") != "https://example.test" {
			t.Error("URL lost on update")
		}
		if e.Get("Bloco.Mnemonic") != nil {
			t.Error("removed mnemonic field retained")
		}
	})
	op3.mu.Unlock()
}

func TestStoreRNGFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rng.kdbx")
	store := NewStore(Options{Random: &failReader{calls: 0, failAfter: 2}})
	if _, err := store.Create(context.Background(), path, testVaultID, []byte(testMaster)); err == nil {
		t.Fatal("create ignored RNG failure")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("vault file created despite RNG failure")
	}
	store = testStore()
	_, binding := createVault(t, store)
	before, _ := os.ReadFile(binding.Path)
	op := openVault(t, store, binding)
	store.options.Random = &failReader{calls: 0, failAfter: 0}
	if _, err := op.Upsert(context.Background(), []Record{accountRecord(testVaultID, testAccountID2, "x", 1)}); err == nil {
		t.Fatal("upsert ignored RNG failure")
	}
	after, _ := os.ReadFile(binding.Path)
	if !bytes.Equal(before, after) {
		t.Fatal("vault bytes changed after RNG failure")
	}
}

type failReader struct {
	calls     int
	failAfter int
}

func (f *failReader) Read(b []byte) (int, error) {
	f.calls++
	if f.calls > f.failAfter {
		return 0, errors.New("injected RNG failure")
	}
	for i := range b {
		b[i] = byte(f.calls + i)
	}
	return len(b), nil
}

func TestStoreMasterPasswordValidation(t *testing.T) {
	dir := t.TempDir()
	for _, master := range [][]byte{
		[]byte("short"),
		bytes.Repeat([]byte(" "), 16),
		bytes.Repeat([]byte("x"), 1025),
		[]byte(""),
	} {
		if err := ValidateMasterPassword(master); err == nil {
			t.Fatalf("weak master accepted: len %d", len(master))
		}
	}
	store := testStore()
	if _, err := store.Create(context.Background(), filepath.Join(dir, "weak.kdbx"), testVaultID, []byte("tiny")); err == nil {
		t.Fatal("create accepted weak master")
	}
	spaced := []byte("  master with spaces!  ")
	binding, err := store.Create(context.Background(), filepath.Join(dir, "spaced.kdbx"), testVaultID, spaced)
	if err != nil {
		t.Fatalf("strong spaced master rejected: %v", err)
	}
	op, err := store.Open(context.Background(), binding, spaced)
	if err != nil {
		t.Fatalf("open with spaced master failed: %v", err)
	}
	op.Close()
}

func TestStoreReadOnlyFile(t *testing.T) {
	store := testStore()
	path, binding := createVault(t, store)
	if err := os.Chmod(path, 0400); err != nil {
		t.Skip("chmod unsupported")
	}
	defer func() { _ = os.Chmod(path, 0600) }()
	op, err := store.Open(context.Background(), binding, []byte(testMaster))
	if err != nil {
		t.Fatalf("open read-only vault: %v", err)
	}
	defer op.Close()
	if _, err := op.List(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := op.WithFilePassword(context.Background(), kindKeystoreV3, testDigest(0x01), func([]byte) error { return nil }); !errors.Is(err, ErrNotFound) {
		t.Fatalf("read-only consult failed: %v", err)
	}
}

func TestStoreFakeClockExpiry(t *testing.T) {
	now := time.Now()
	clock := &now
	store := NewStore(Options{
		Now: func() time.Time { return *clock },
		TTL: time.Minute,
	})
	_, binding := createVault(t, store)
	op := openVault(t, store, binding)
	*clock = now.Add(2 * time.Minute)
	if _, err := op.List(context.Background()); !errors.Is(err, ErrExpired) {
		t.Fatalf("expected expiry with fake clock, got %v", err)
	}
	*clock = now
	op2 := openVault(t, store, binding)
	defer op2.Close()
	store.hooks.secure = func(path string) error {
		*clock = now.Add(2 * time.Minute)
		return secureVaultFile(path)
	}
	before, _ := os.ReadFile(binding.Path)
	if _, err := op2.Upsert(context.Background(), []Record{accountRecord(testVaultID, testAccountID, "a", 1)}); !errors.Is(err, ErrExpired) {
		t.Fatalf("expired operation committed: %v", err)
	}
	after, _ := os.ReadFile(binding.Path)
	if !bytes.Equal(before, after) {
		t.Fatal("expired operation modified vault")
	}
}

func TestStoreCreateDirSyncWarning(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "warn.kdbx")
	store := testStore()
	store.hooks.syncDir = func(string) error { return errors.New("injected sync failure") }
	binding, err := store.Create(context.Background(), path, testVaultID, []byte(testMaster))
	if err == nil {
		t.Fatal("expected committed warning")
	}
	if !IsCommitted(err) {
		t.Fatalf("expected CommittedWarning, got %v", err)
	}
	if binding.Path != path || binding.TargetID == "" || binding.VaultID != testVaultID {
		t.Fatalf("binding not returned with warning: %+v", binding)
	}
	store2 := testStore()
	op, err := store2.Open(context.Background(), binding, []byte(testMaster))
	if err != nil {
		t.Fatalf("committed vault unreadable: %v", err)
	}
	op.Close()
}

func TestStoreRemoveAccount(t *testing.T) {
	store := testStore()
	path, binding := createVault(t, store)
	op, err := store.Open(context.Background(), binding, []byte(testMaster))
	if err != nil {
		t.Fatal(err)
	}
	digest := testDigest(0x22)
	if _, err := op.Upsert(context.Background(), []Record{
		accountRecord(testVaultID, testAccountID, "mine", 1),
		accountRecord(testVaultID, testAccountID2, "other", 1),
		fileRecord(testVaultID, testAccountID, kindBlocoCipher, digest, []byte("pw")),
		fileRecord(testVaultID, testAccountID2, kindBlocoCipher, testDigest(0x33), []byte("pw")),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := op.RemoveAccount(context.Background(), testAccountID); err != nil {
		t.Fatal(err)
	}
	list, err := op.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 remaining records, got %d", len(list))
	}
	for _, meta := range list {
		if meta.Ref.AccountID == testAccountID {
			t.Fatal("removed account still listed")
		}
	}
	op.Close()
	reopened, err := store.Open(context.Background(), binding, []byte(testMaster))
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	database := decodeVaultFile(t, path)
	if len(database.Content.Root.DeletedObjects) == 0 {
		t.Fatal("no deletion tombstones recorded")
	}
	if _, err := reopened.RemoveAccount(context.Background(), testAccountID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound re-removing account, got %v", err)
	}
}

func TestStoreCommitRotatesCrypto(t *testing.T) {
	store := testStore()
	path, binding := createVault(t, store)
	readSeeds := func() (masterSeed, iv, salt, inner string) {
		database := decodeVaultFile(t, path)
		return hex.EncodeToString(database.Header.FileHeaders.MasterSeed),
			hex.EncodeToString(database.Header.FileHeaders.EncryptionIV),
			hex.EncodeToString(database.Header.FileHeaders.KdfParameters.Salt[:]),
			hex.EncodeToString(database.Content.InnerHeader.InnerRandomStreamKey)
	}
	m1, i1, s1, k1 := readSeeds()
	op, err := store.Open(context.Background(), binding, []byte(testMaster))
	if err != nil {
		t.Fatal(err)
	}
	defer op.Close()
	if _, err := op.Upsert(context.Background(), []Record{accountRecord(testVaultID, testAccountID, "a", 1)}); err != nil {
		t.Fatal(err)
	}
	m2, i2, s2, k2 := readSeeds()
	if m1 == m2 || i1 == i2 || s1 == s2 || k1 == k2 {
		t.Fatal("crypto material was not rotated on commit")
	}
	database := decodeVaultFile(t, path)
	kdf := database.Header.FileHeaders.KdfParameters
	if kdf.Memory != kdfBaselineMemory || kdf.Iterations != kdfBaselineRounds || kdf.Parallelism != kdfBaselineLanes || kdf.Version != kdfArgon2Version {
		t.Fatalf("unexpected KDF profile: %+v", kdf)
	}
	if !bytes.Equal(database.Header.FileHeaders.CipherID, kp.CipherAES) {
		t.Fatal("cipher not AES")
	}
}

func TestStoreBackupRoundTrip(t *testing.T) {
	store := testStore()
	path, binding := createVault(t, store)
	op, err := store.Open(context.Background(), binding, []byte(testMaster))
	if err != nil {
		t.Fatal(err)
	}
	defer op.Close()
	if _, err := op.Upsert(context.Background(), []Record{accountRecord(testVaultID, testAccountID, "a", 1)}); err != nil {
		t.Fatal(err)
	}
	backups, err := filepath.Glob(filepath.Join(filepath.Dir(path), "*.pre-update-*.bak"))
	if err != nil || len(backups) != 1 {
		t.Fatalf("expected one backup file, got %v", backups)
	}
	pattern := regexp.MustCompile(`\.pre-update-[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}\.bak$`)
	if !pattern.MatchString(backups[0]) {
		t.Fatalf("unexpected backup name %s", backups[0])
	}
	data, err := os.ReadFile(backups[0])
	if err != nil {
		t.Fatal(err)
	}
	database := kp.NewDatabase()
	database.Credentials = kp.NewPasswordCredentials(testMaster)
	if err := kp.NewDecoderWithLimits(bytes.NewReader(data), kp.DefaultDecodeLimits()).Decode(database); err != nil {
		t.Fatalf("backup does not decrypt: %v", err)
	}
	if got := len(database.Content.Root.Groups); got == 0 {
		t.Fatal("backup missing content")
	}
}

func TestStoreSecretsNotPlaintext(t *testing.T) {
	store := testStore()
	path, binding := createVault(t, store)
	op, err := store.Open(context.Background(), binding, []byte(testMaster))
	if err != nil {
		t.Fatal(err)
	}
	secret := "synthetic-secret-9f8e7d6c5b"
	record := accountRecord(testVaultID, testAccountID, "t", 1)
	record.Password = []byte(secret)
	record.Mnemonic = []byte(secret + "-mnemonic")
	if _, err := op.Upsert(context.Background(), []Record{record}); err != nil {
		t.Fatal(err)
	}
	op.Close()
	dir := filepath.Dir(path)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(data, []byte(secret)) {
			t.Fatalf("plaintext secret found in %s", entry.Name())
		}
	}
}

func TestStoreExpiryAndCancel(t *testing.T) {
	store := NewStore(Options{TTL: 30 * time.Millisecond})
	_, binding := createVault(t, store)
	op, err := store.Open(context.Background(), binding, []byte(testMaster))
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(80 * time.Millisecond)
	if _, err := op.List(context.Background()); !errors.Is(err, ErrExpired) {
		t.Fatalf("expected ErrExpired, got %v", err)
	}
	if _, err := op.Upsert(context.Background(), []Record{accountRecord(testVaultID, testAccountID, "a", 1)}); !errors.Is(err, ErrExpired) {
		t.Fatalf("expected ErrExpired on write, got %v", err)
	}
	op.Close()
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.Inspect(canceled, binding.Path, []byte(testMaster)); err == nil {
		t.Fatal("canceled context accepted")
	}
	op2, err := store.Open(context.Background(), binding, []byte(testMaster))
	if err != nil {
		t.Fatal(err)
	}
	op2.Close()
	if _, err := op2.List(context.Background()); !errors.Is(err, ErrExpired) {
		t.Fatalf("expected ErrExpired after close, got %v", err)
	}
}

func TestStoreLockBusy(t *testing.T) {
	store := testStore()
	_, binding := createVault(t, store)
	op, err := store.Open(context.Background(), binding, []byte(testMaster))
	if err != nil {
		t.Fatal(err)
	}
	defer op.Close()
	if _, err := store.Open(context.Background(), binding, []byte(testMaster)); !errors.Is(err, ErrBusy) {
		t.Fatalf("expected ErrBusy, got %v", err)
	}
}

func TestStoreExternalEditConflict(t *testing.T) {
	store := testStore()
	path, binding := createVault(t, store)
	op, err := store.Open(context.Background(), binding, []byte(testMaster))
	if err != nil {
		t.Fatal(err)
	}
	defer op.Close()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data[len(data)-1] ^= 0xff
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := op.Preflight(context.Background()); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected ErrConflict in preflight, got %v", err)
	}
	if _, err := op.Upsert(context.Background(), []Record{accountRecord(testVaultID, testAccountID, "a", 1)}); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected ErrConflict on commit, got %v", err)
	}
}

func TestStoreCommitFailureInjection(t *testing.T) {
	store := testStore()
	path, binding := createVault(t, store)
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	op, err := store.Open(context.Background(), binding, []byte(testMaster))
	if err != nil {
		t.Fatal(err)
	}
	defer op.Close()
	store.hooks.install = func(string, string, bool) (bool, error) { return false, errors.New("injected install failure") }
	if _, err := op.Upsert(context.Background(), []Record{accountRecord(testVaultID, testAccountID, "a", 1)}); err == nil || IsCommitted(err) {
		t.Fatalf("expected non-committed install failure, got %v", err)
	}
	current, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(original, current) {
		t.Fatal("vault bytes changed after failed commit")
	}
	if sha256.Sum256(original) != sha256.Sum256(current) {
		t.Fatal("vault digest changed")
	}
	if _, err := op.List(context.Background()); !errors.Is(err, ErrExpired) {
		t.Fatalf("operation must not serve unsaved state after failed commit, got %v", err)
	}
	err = op.WithPassword(context.Background(), Ref{VaultID: testVaultID, AccountID: testAccountID, ItemID: itemIDAccount}, 1, func([]byte) error { return nil })
	if !errors.Is(err, ErrExpired) {
		t.Fatalf("expected ErrExpired on dead operation, got %v", err)
	}
	op.Close()
	store.hooks.install = installVaultFile
	store.hooks.syncDir = func(string) error { return errors.New("injected sync failure") }
	op2, err := store.Open(context.Background(), binding, []byte(testMaster))
	if err != nil {
		t.Fatal(err)
	}
	result, err := op2.Upsert(context.Background(), []Record{accountRecord(testVaultID, testAccountID, "a", 1)})
	if err == nil || !IsCommitted(err) || !result.Committed {
		t.Fatalf("expected committed warning, got result=%+v err=%v", result, err)
	}
	var warning *CommittedWarning
	if !errors.As(err, &warning) {
		t.Fatal("missing committed warning")
	}
	op2.Close()
	store.hooks.syncDir = syncVaultDirectory
	op3, err := store.Open(context.Background(), binding, []byte(testMaster))
	if err != nil {
		t.Fatalf("committed file not readable: %v", err)
	}
	defer op3.Close()
	list, err := op3.List(context.Background())
	if err != nil || len(list) != 1 {
		t.Fatalf("expected committed record, got %v %v", list, err)
	}
}

func TestStoreRecordValidation(t *testing.T) {
	store := testStore()
	_, binding := createVault(t, store)
	op, err := store.Open(context.Background(), binding, []byte(testMaster))
	if err != nil {
		t.Fatal(err)
	}
	defer op.Close()
	base := accountRecord(testVaultID, testAccountID, "ok", 1)
	cases := []struct {
		name   string
		mutate func(*Record)
	}{
		{"bad vault", func(r *Record) { r.Ref.VaultID = "x" }},
		{"bad account", func(r *Record) { r.Ref.AccountID = "notuuid" }},
		{"bad item", func(r *Record) { r.Ref.ItemID = "unknown" }},
		{"zero generation", func(r *Record) { r.Generation = 0 }},
		{"empty password", func(r *Record) { r.Password = nil }},
		{"file meta on account", func(r *Record) { r.Digest = testDigest(1) }},
		{"bad private key", func(r *Record) { r.PrivateKey = []byte("zz") }},
		{"huge secret", func(r *Record) { r.Password = make([]byte, maxSecretBytes+1) }},
		{"huge metadata", func(r *Record) { r.Title = strings.Repeat("t", maxMetadataBytes+1) }},
	}
	for _, tc := range cases {
		record := base
		tc.mutate(&record)
		if _, err := op.Upsert(context.Background(), []Record{record}); !errors.Is(err, ErrInvalidRecord) {
			t.Fatalf("%s: expected ErrInvalidRecord, got %v", tc.name, err)
		}
	}
	if _, err := op.Upsert(context.Background(), []Record{base}); err != nil {
		t.Fatalf("baseline record rejected: %v", err)
	}
	badFile := fileRecord(testVaultID, testAccountID, kindKeystoreV3, testDigest(1), nil)
	badFile.Ref.ItemID = "file:keystore_v3:" + testDigest(2)
	if _, err := op.Upsert(context.Background(), []Record{badFile}); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("expected ErrInvalidRecord for mismatched digest, got %v", err)
	}
}

func TestStoreRedaction(t *testing.T) {
	record := accountRecord(testVaultID, testAccountID, "redact", 1)
	record.Password = []byte("hidden-secret-material")
	for _, rendered := range []string{record.String(), record.GoString(), fmt.Sprintf("%v", record), fmt.Sprintf("%+v", record)} {
		if strings.Contains(rendered, "hidden-secret-material") {
			t.Fatalf("secret leaked in rendering: %s", rendered)
		}
	}
	if _, err := record.MarshalJSON(); err == nil {
		t.Fatal("record marshaled to JSON")
	}
	store := testStore()
	_, binding := createVault(t, store)
	op, err := store.Open(context.Background(), binding, []byte(testMaster))
	if err != nil {
		t.Fatal(err)
	}
	defer op.Close()
	if rendered := fmt.Sprintf("%v", op); strings.Contains(rendered, testMaster) {
		t.Fatal("operation rendering leaked material")
	}
	if _, err := op.MarshalJSON(); err == nil {
		t.Fatal("operation marshaled to JSON")
	}
}
