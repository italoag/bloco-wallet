package storage

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"
	"time"

	"blocowallet/internal/evm"
	"blocowallet/internal/wallet"
	"blocowallet/pkg/config"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

type restartApprovalVerifier struct{}

func (*restartApprovalVerifier) VerifyTransactionApproval(context.Context, wallet.TransactionApprovalBinding) error {
	return nil
}

func (*restartApprovalVerifier) VerifyMessageApproval(context.Context, wallet.MessageApprovalBinding) error {
	return nil
}

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
	signer, err := wallet.NewSoftwareSignerWithApprovalVerifier(reopenedVault, &restartApprovalVerifier{})
	if err != nil {
		t.Fatal(err)
	}
	digest := crypto.Keccak256([]byte("restart"))
	var digestValue [32]byte
	copy(digestValue[:], digest)
	intentHash := crypto.Keccak256Hash([]byte("restart-intent"), digest)
	now := time.Now().UTC()
	messageApproval := evm.MessageApproval{
		ApprovalID: "51111111-1111-4111-8111-111111111111", AccountID: summary.AccountID,
		Signer: common.HexToAddress(summary.Address), Scheme: wallet.MessageSigningEIP191Personal,
		Digest: digestValue, IntentHash: intentHash, PayloadSize: uint64(len(digest)),
		AuthorizationEpoch: 1, ConfirmationLevel: evm.ConfirmationReinforced,
		CreatedAt: now, ConfirmedAt: now, ExpiresAt: now.Add(time.Minute), State: evm.MessageApprovalPending, Revision: 1,
	}
	if err := reopenedRepository.IssueMessageApproval(context.Background(), messageApproval); err != nil {
		t.Fatal(err)
	}
	if _, err := reopenedRepository.AuthorizeMessageSigning(context.Background(), evm.AuthorizeMessageSigningRequest{
		SigningID: "71111111-1111-4111-8111-111111111111", ApprovalID: messageApproval.ApprovalID,
		AccountID: summary.AccountID, Signer: messageApproval.Signer, Scheme: messageApproval.Scheme,
		Digest: messageApproval.Digest, IntentHash: messageApproval.IntentHash, AuthorizationEpoch: 1, AuthorizedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	result, err := signer.Sign(context.Background(), handle, wallet.SoftwareSigningRequest{
		AccountID:     summary.AccountID,
		Purpose:       wallet.SigningPurposeMessage,
		MessageScheme: wallet.MessageSigningEIP191Personal,
		Digest:        digestValue,
		IntentHash:    intentHash,
		ApprovalID:    messageApproval.ApprovalID,
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
