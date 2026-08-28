package storage

import (
	"bytes"
	"context"
	"testing"
	"time"

	"blocowallet/internal/evm"
	"blocowallet/internal/wallet"
	"blocowallet/pkg/config"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

func TestPersonalSignServiceDurableFlowWithRealVault(t *testing.T) {
	root := t.TempDir()
	repository, err := NewVaultRepository(&config.Config{
		AppDir: root, DatabasePath: root + "/personal.db", Database: config.DatabaseConfig{Type: "sqlite"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = repository.Close() }()
	codec, err := wallet.NewSecretEnvelopeCodec(wallet.Argon2idPolicy{
		Time: 1, MemoryKiB: 64, Parallelism: 1, KeyLength: 32, SaltLength: 16,
		MaxTime: 4, MaxMemoryKiB: 256 * 1024, MaxParallelism: 8, MaxKeyLength: 32, MaxSaltLength: 64,
	})
	if err != nil {
		t.Fatal(err)
	}
	vault, err := wallet.NewWalletVault(repository, codec, wallet.VaultOptions{SourceIdentityKey: bytes.Repeat([]byte{0x42}, 32)})
	if err != nil {
		t.Fatal(err)
	}
	defer vault.Close()
	password := []byte("Strong personal password 1!")
	summary, challenge, err := vault.Create(context.Background(), wallet.CreateAccountRequest{Name: "Personal", Password: password})
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
	handle, err := vault.Unlock(context.Background(), summary.AccountID, password)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = vault.Lock(handle) }()
	signer, err := wallet.NewSoftwareSignerWithApprovalVerifier(vault, repository)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	ids := []string{"51111111-1111-4111-8111-111111111111", "71111111-1111-4111-8111-111111111111"}
	index := 0
	service, err := evm.NewMessageSigningService(repository, signer, evm.MessageSigningOptions{
		Now: func() time.Time { return now }, NewID: func() (string, error) {
			value := ids[index]
			index++
			return value, nil
		}, ApprovalTTL: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := evm.PreparePersonalSign(evm.PreparePersonalSignRequest{
		AccountID: summary.AccountID, Signer: common.HexToAddress(summary.Address),
		Message: []byte("I agree to the exact terms"), Origin: "local-user",
	})
	if err != nil {
		t.Fatal(err)
	}
	preview := prepared.Preview()
	result, err := service.ApproveAndSignPersonal(context.Background(), handle, prepared, evm.PersonalSignApprovalRequest{
		AuthorizationEpoch: 1, ConfirmedIntentHash: preview.IntentHash, ConfirmationLevel: evm.ConfirmationReinforced,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Signature[64] < 27 || result.Signature[64] > 28 {
		t.Fatalf("personal signature V is outside 27/28: %x", result.Signature)
	}
	recoverable := append([]byte(nil), result.Signature...)
	recoverable[64] -= 27
	publicKey, err := crypto.SigToPub(result.Digest[:], recoverable)
	if err != nil {
		t.Fatal(err)
	}
	if crypto.PubkeyToAddress(*publicKey).Hex() != summary.Address {
		t.Fatal("personal signature recovered the wrong account")
	}
	if err := repository.VerifyMessageApproval(context.Background(), wallet.MessageApprovalBinding{
		AccountID: summary.AccountID, Scheme: wallet.MessageSigningEIP191Personal,
		Digest: preview.Digest, IntentHash: preview.IntentHash, ApprovalID: result.ApprovalID,
	}); err == nil {
		t.Fatal("completed durable personal approval remained consumable")
	}
}
