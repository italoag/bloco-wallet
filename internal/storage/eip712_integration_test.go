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

const eip712IntegrationFixture = `{
	"types": {
		"EIP712Domain": [
			{"name": "name", "type": "string"},
			{"name": "version", "type": "string"},
			{"name": "chainId", "type": "uint256"},
			{"name": "verifyingContract", "type": "address"}
		],
		"Person": [{"name": "name", "type": "string"}, {"name": "wallet", "type": "address"}],
		"Mail": [
			{"name": "from", "type": "Person"},
			{"name": "to", "type": "Person"},
			{"name": "contents", "type": "string"}
		]
	},
	"primaryType": "Mail",
	"domain": {
		"name": "Ether Mail",
		"version": "1",
		"chainId": 1,
		"verifyingContract": "0xCcCCccccCCCCcCCCCCCcCcCccCcCCCcCcccccccC"
	},
	"message": {
		"from": {"name": "Cow", "wallet": "0xCD2a3d9F938E13CD947Ec05AbC7FE734Df8DD826"},
		"to": {"name": "Bob", "wallet": "0xbBbBBBBbbBBBbbbBbbBbbbbBBbBbbbbBbBbbBBbB"},
		"contents": "Hello, Bob!"
	}
}`

func TestEIP712ServiceDurableFlowWithRealVault(t *testing.T) {
	root := t.TempDir()
	repository, err := NewVaultRepository(&config.Config{
		AppDir: root, DatabasePath: root + "/eip712.db", Database: config.DatabaseConfig{Type: "sqlite"},
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
	password := []byte("Strong eip712 password 1!")
	summary, challenge, err := vault.Create(context.Background(), wallet.CreateAccountRequest{Name: "Typed", Password: password})
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
	ids := []string{"51111111-1111-4111-8111-111111111111", "71111111-1111-4111-8111-111111111111"}
	index := 0
	now := time.Now().UTC()
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
	prepared, err := evm.PrepareEIP712Sign(evm.PrepareEIP712SignRequest{
		AccountID: summary.AccountID, Signer: common.HexToAddress(summary.Address),
		ChainID: 1, TypedData: []byte(eip712IntegrationFixture), Origin: "local-user",
	})
	if err != nil {
		t.Fatal(err)
	}
	preview := prepared.Preview()
	result, err := service.ApproveAndSignEIP712(context.Background(), handle, prepared, evm.PersonalSignApprovalRequest{
		AuthorizationEpoch: 1, ConfirmedIntentHash: preview.IntentHash, ConfirmationLevel: evm.ConfirmationReinforced,
	})
	if err != nil {
		t.Fatal(err)
	}
	recoverable := append([]byte(nil), result.Signature...)
	recoverable[64] -= 27
	publicKey, err := crypto.SigToPub(result.Digest[:], recoverable)
	if err != nil {
		t.Fatal(err)
	}
	if crypto.PubkeyToAddress(*publicKey).Hex() != summary.Address {
		t.Fatal("EIP-712 signature recovered the wrong account")
	}
	if err := repository.VerifyMessageApproval(context.Background(), wallet.MessageApprovalBinding{
		AccountID: summary.AccountID, Scheme: wallet.MessageSigningEIP712, ChainID: 1,
		Digest: preview.Digest, IntentHash: preview.IntentHash, ApprovalID: result.ApprovalID,
	}); err == nil {
		t.Fatal("completed durable EIP-712 approval remained consumable")
	}
}
