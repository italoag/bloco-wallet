package storage

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"

	"blocowallet/internal/wallet"
	"blocowallet/pkg/config"

	"github.com/ethereum/go-ethereum/crypto"
)

func TestVaultSurvivesRepositoryRestart(t *testing.T) {
	root := t.TempDir()
	cfg := &config.Config{
		AppDir:       root,
		DatabasePath: filepath.Join(root, "vault.db"),
		Database:     config.DatabaseConfig{Type: "sqlite"},
	}
	codec, err := wallet.NewSecretEnvelopeCodec(wallet.Argon2idPolicy{
		Time: 1, MemoryKiB: 64, Parallelism: 1, KeyLength: 32, SaltLength: 16,
		MaxTime: 4, MaxMemoryKiB: 256 * 1024, MaxParallelism: 8, MaxKeyLength: 32, MaxSaltLength: 64,
	})
	if err != nil {
		t.Fatal(err)
	}
	repository, err := NewVaultRepository(cfg)
	if err != nil {
		t.Fatal(err)
	}
	identityKey := bytes.Repeat([]byte{0x42}, 32)
	vault, err := wallet.NewWalletVault(repository, codec, wallet.VaultOptions{SourceIdentityKey: identityKey})
	if err != nil {
		t.Fatal(err)
	}
	password := []byte("Strong restart password 1!")
	summary, challenge, err := vault.Create(context.Background(), wallet.CreateAccountRequest{Name: "Restart", Password: password})
	if err != nil {
		t.Fatal(err)
	}
	answers := make(map[int]string, len(challenge.RequiredWordIndices))
	for _, index := range challenge.RequiredWordIndices {
		answers[index] = challenge.Words[index]
	}
	if _, err := vault.ConfirmBackup(context.Background(), challenge.ChallengeID, answers); err != nil {
		t.Fatal(err)
	}
	vault.Close()
	if err := repository.Close(); err != nil {
		t.Fatal(err)
	}

	reopenedRepository, err := NewVaultRepository(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := reopenedRepository.Close(); err != nil {
			t.Error(err)
		}
	}()
	if _, err := wallet.NewWalletVault(reopenedRepository, codec, wallet.VaultOptions{SourceIdentityKey: bytes.Repeat([]byte{0x43}, 32)}); err == nil {
		t.Fatal("reopened vault accepted a different source identity key")
	}
	reopenedVault, err := wallet.NewWalletVault(reopenedRepository, codec, wallet.VaultOptions{SourceIdentityKey: identityKey})
	if err != nil {
		t.Fatal(err)
	}
	defer reopenedVault.Close()
	handle, err := reopenedVault.Unlock(context.Background(), summary.AccountID, password)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := wallet.NewSoftwareSigner(reopenedVault)
	if err != nil {
		t.Fatal(err)
	}
	digest := crypto.Keccak256([]byte("restart"))
	var digestValue [32]byte
	copy(digestValue[:], digest)
	result, err := signer.Sign(context.Background(), handle, wallet.SoftwareSigningRequest{
		AccountID:  summary.AccountID,
		Purpose:    wallet.SigningPurposeMessage,
		Digest:     digestValue,
		ApprovalID: "restart-test",
	})
	if err != nil {
		t.Fatal(err)
	}
	publicKey, err := crypto.SigToPub(digest, result.Signature)
	if err != nil {
		t.Fatal(err)
	}
	if crypto.PubkeyToAddress(*publicKey).Hex() != summary.Address {
		t.Fatal("restarted vault signed with the wrong account")
	}
}
