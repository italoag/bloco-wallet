package storage

import (
	"bytes"
	"context"
	"math/big"
	"path/filepath"
	"testing"
	"time"

	"blocowallet/internal/evm"
	"blocowallet/internal/wallet"
	"blocowallet/pkg/config"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

type integrationEVMRPC struct {
	sent []byte
}

func (rpc *integrationEVMRPC) ProviderBinding() evm.ProviderBinding { return evm.ProviderBinding{1} }
func (rpc *integrationEVMRPC) ChainID() uint64                      { return 1 }
func (rpc *integrationEVMRPC) LatestHeader(context.Context) (evm.BlockHeader, error) {
	return evm.BlockHeader{
		BlockIdentity: evm.BlockIdentity{Number: 100, Hash: common.HexToHash("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")},
		GasLimit:      30_000_000,
	}, nil
}
func (rpc *integrationEVMRPC) PendingNonceAt(context.Context, common.Address) (uint64, error) {
	return 0, nil
}
func (rpc *integrationEVMRPC) CallContract(context.Context, evm.TransactionCall, evm.BlockIdentity) ([]byte, error) {
	return []byte{}, nil
}
func (rpc *integrationEVMRPC) EstimateGas(context.Context, evm.TransactionCall, evm.BlockIdentity) (uint64, error) {
	return 21_000, nil
}
func (rpc *integrationEVMRPC) SuggestGasPrice(context.Context) (*big.Int, error) {
	return big.NewInt(1_000_000_000), nil
}
func (rpc *integrationEVMRPC) SuggestGasTipCap(context.Context) (*big.Int, error) {
	return big.NewInt(1), nil
}
func (rpc *integrationEVMRPC) SendRawTransaction(_ context.Context, raw []byte) (common.Hash, error) {
	rpc.sent = append([]byte(nil), raw...)
	return crypto.Keccak256Hash(raw), nil
}
func (rpc *integrationEVMRPC) CodeAt(context.Context, common.Address, evm.BlockIdentity) ([]byte, error) {
	return []byte{1}, nil
}
func (rpc *integrationEVMRPC) TransactionReceipt(context.Context, common.Hash) (evm.Receipt, bool, error) {
	return evm.Receipt{}, false, nil
}
func (rpc *integrationEVMRPC) HeaderByNumber(_ context.Context, number uint64) (evm.BlockHeader, bool, error) {
	header, err := rpc.LatestHeader(context.Background())
	return header, number == header.Number, err
}

func TestEVMEngineUsesVaultApprovalAndDurableBroadcastRecord(t *testing.T) {
	root := t.TempDir()
	repository, err := NewVaultRepository(&config.Config{
		AppDir: root, DatabasePath: filepath.Join(root, "engine.db"), Database: config.DatabaseConfig{Type: "sqlite"},
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
	password := []byte("Strong transaction password 1!")
	summary, challenge, err := vault.Create(context.Background(), wallet.CreateAccountRequest{Name: "EVM", Password: password})
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
	account, err := repository.GetAccount(context.Background(), summary.AccountID)
	if err != nil {
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
	rpc := &integrationEVMRPC{}
	ids := []string{
		"31111111-1111-4111-8111-111111111111",
		"51111111-1111-4111-8111-111111111111",
		"61111111-1111-4111-8111-111111111111",
	}
	idIndex := 0
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	engine, err := evm.NewEngine(repository, rpc, signer, evm.EngineOptions{
		Now: func() time.Time { return now }, NewID: func() (string, error) {
			value := ids[idIndex]
			idIndex++
			return value, nil
		}, ReservationTTL: time.Minute, ApprovalTTL: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := engine.PrepareNative(context.Background(), evm.PrepareNativeRequest{
		OperationID: "41111111-1111-4111-8111-111111111111", PlanGeneration: 1,
		AccountID: summary.AccountID, ChainID: 1, From: common.HexToAddress(summary.Address),
		To: common.HexToAddress("0x3535353535353535353535353535353535353535"), Amount: big.NewInt(1),
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := engine.ApproveSignAndBroadcast(context.Background(), handle, prepared, evm.ApprovalRequest{
		AuthorizationEpoch: account.AuthorizationEpoch, RiskLevel: evm.RiskNormal, ConfirmationLevel: evm.ConfirmationStandard,
	})
	if err != nil {
		t.Fatal(err)
	}
	stored, err := repository.GetTransaction(context.Background(), result.TransactionID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.State != evm.TransactionSubmitted || stored.TransactionHash != result.Hash || string(stored.SignedPayload) != string(rpc.sent) || len(rpc.sent) == 0 {
		t.Fatalf("engine result was not durably bound to broadcast bytes: stored=%+v sent=%x", stored, rpc.sent)
	}
}
