package wallet

import (
	"context"
	"regexp"
	"sync"
	"testing"
)

func newCredentialBackupVault(t *testing.T) *WalletVault {
	t.Helper()
	codec, err := NewSecretEnvelopeCodec(testEnvelopePolicy())
	if err != nil {
		t.Fatal(err)
	}
	vault, err := NewWalletVault(newMemoryAccountRepository(), codec, VaultOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return vault
}

func TestCredentialVaultIDStableAndCanonical(t *testing.T) {
	vault := newCredentialBackupVault(t)
	defer vault.Close()
	first, err := vault.CredentialVaultID(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	matched, _ := regexp.MatchString(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`, first)
	if !matched {
		t.Fatalf("credential vault ID is not canonical UUIDv4: %q", first)
	}
	second, err := vault.CredentialVaultID(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("credential vault ID changed between reads")
	}
	repository := newMemoryAccountRepository()
	codec, err := NewSecretEnvelopeCodec(testEnvelopePolicy())
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := NewWalletVault(repository, codec, VaultOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if err := repository.PutVaultMetadata(context.Background(), credentialVaultIDMetadataKey, first); err != nil {
		t.Fatal(err)
	}
	value, err := reopened.CredentialVaultID(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if value != first {
		t.Fatal("credential vault ID not preserved across vaults sharing storage")
	}
}

func TestCredentialVaultIDConcurrentCreation(t *testing.T) {
	vault := newCredentialBackupVault(t)
	defer vault.Close()
	var wait sync.WaitGroup
	results := make([]string, 8)
	errs := make([]error, 8)
	for index := range results {
		wait.Add(1)
		go func(slot int) {
			defer wait.Done()
			value, err := vault.CredentialVaultID(context.Background())
			results[slot] = value
			errs[slot] = err
		}(index)
	}
	wait.Wait()
	for index := range results {
		if errs[index] != nil {
			t.Fatalf("concurrent vault ID lookup failed: %v", errs[index])
		}
		if results[index] != results[0] {
			t.Fatal("concurrent vault ID lookups diverged")
		}
	}
}

func TestCredentialVaultIDClosedAndCorrupt(t *testing.T) {
	vault := newCredentialBackupVault(t)
	vault.Close()
	if _, err := vault.CredentialVaultID(context.Background()); err == nil {
		t.Fatal("closed vault returned a credential vault ID")
	}
	repository := newMemoryAccountRepository()
	codec, err := NewSecretEnvelopeCodec(testEnvelopePolicy())
	if err != nil {
		t.Fatal(err)
	}
	vault2, err := NewWalletVault(repository, codec, VaultOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer vault2.Close()
	repository.metadata[credentialVaultIDMetadataKey] = "not-a-uuid"
	if _, err := vault2.CredentialVaultID(context.Background()); err == nil {
		t.Fatal("corrupt stored credential vault ID accepted")
	}
}

func TestCredentialBackupStateValidation(t *testing.T) {
	valid := CredentialBackupState{
		TargetID:    "11111111-1111-4111-8111-111111111111",
		VaultID:     "22222222-2222-4222-8222-222222222222",
		AccountID:   "33333333-3333-4333-8333-333333333333",
		ItemID:      "account",
		OperationID: "44444444-4444-4444-8444-444444444444",
		Operation:   CredentialBackupOperationUpsert,
		State:       CredentialBackupStatePending,
		Generation:  1,
		Revision:    1,
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid state rejected: %v", err)
	}
	fileDigest := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	fileItem := valid
	fileItem.ItemID = "file:keystore_v3:" + fileDigest
	fileItem.ArtifactKind = "keystore_v3"
	fileItem.ArtifactName = "keystore.json"
	fileItem.ArtifactDigest = fileDigest
	fileItem.Generation = 0
	if err := fileItem.Validate(); err != nil {
		t.Fatalf("valid file item rejected: %v", err)
	}
	preparedFile := fileItem
	preparedFile.State = CredentialBackupStatePrepared
	if err := preparedFile.Validate(); err != nil {
		t.Fatalf("valid prepared file row rejected: %v", err)
	}
	cases := []struct {
		name   string
		mutate func(*CredentialBackupState)
	}{
		{"bad target", func(s *CredentialBackupState) { s.TargetID = "x" }},
		{"uppercase vault", func(s *CredentialBackupState) { s.VaultID = "22222222-2222-4222-8222-22222222222A" }},
		{"non v4 account", func(s *CredentialBackupState) { s.AccountID = "33333333-3333-3333-8333-333333333333" }},
		{"bad item", func(s *CredentialBackupState) { s.ItemID = "file:unknown:aa" }},
		{"bad operation id", func(s *CredentialBackupState) { s.OperationID = "zz" }},
		{"bad operation", func(s *CredentialBackupState) { s.Operation = "nuke" }},
		{"bad state", func(s *CredentialBackupState) { s.State = "gone" }},
		{"zero account generation", func(s *CredentialBackupState) { s.Generation = 0 }},
		{"zero revision", func(s *CredentialBackupState) { s.Revision = 0 }},
		{"overflow generation", func(s *CredentialBackupState) { s.Generation = 1 << 63 }},
		{"bad digest", func(s *CredentialBackupState) {
			s.ItemID = "file:keystore_v3:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
			s.ArtifactKind = "keystore_v3"
			s.ArtifactName = "f"
			s.ArtifactDigest = "xyz"
		}},
		{"artifact on account", func(s *CredentialBackupState) { s.ArtifactName = "file.json" }},
		{"file item mismatch", func(s *CredentialBackupState) {
			s.ItemID = "file:keystore_v3:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
			s.ArtifactKind = "keystore_v3"
			s.ArtifactName = "f"
			s.ArtifactDigest = "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
		}},
		{"prepared account", func(s *CredentialBackupState) { s.State = CredentialBackupStatePrepared }},
		{"pending delete op", func(s *CredentialBackupState) {
			s.State = CredentialBackupStatePending
			s.Operation = CredentialBackupOperationDelete
		}},
		{"delete_pending upsert op", func(s *CredentialBackupState) {
			s.State = CredentialBackupStateDeletePending
			s.Operation = CredentialBackupOperationUpsert
		}},
		{"synced ahead", func(s *CredentialBackupState) { s.SyncedGeneration = 5 }},
		{"synced not applied", func(s *CredentialBackupState) { s.State = CredentialBackupStateSynced }},
		{"secret-like artifact", func(s *CredentialBackupState) { s.ArtifactPath = "a\x00b" }},
	}
	for _, tc := range cases {
		state := valid
		tc.mutate(&state)
		if err := state.Validate(); err == nil {
			t.Fatalf("%s: invalid state accepted", tc.name)
		}
	}
}
