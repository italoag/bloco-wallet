package storage

import (
	"context"
	"math/big"
	"path/filepath"
	"testing"
	"time"

	"blocowallet/internal/evm"
	"blocowallet/internal/wallet"
	"blocowallet/pkg/config"

	"github.com/ethereum/go-ethereum/common"
)

func TestVaultRepositoryUpgradesV11ToV12PreservingERC1155BatchEffects(t *testing.T) {
	root := t.TempDir()
	cfg := &config.Config{
		AppDir:       root,
		DatabasePath: filepath.Join(root, "upgrade.db"),
		Database:     config.DatabaseConfig{Type: "sqlite"},
	}
	repository, err := NewVaultRepository(cfg)
	if err != nil {
		t.Fatal(err)
	}
	account := testAccount("11111111-1111-4111-8111-111111111111", "erc1155-v12-upgrade")
	account.State = wallet.AccountStateActive
	if err := repository.CreateAccount(context.Background(), account); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	sender := common.HexToAddress(account.Address)
	contract := common.HexToAddress("0x3333333333333333333333333333333333333333")
	reservation, err := repository.ReserveNonce(context.Background(), evm.ReserveNonceRequest{
		ReservationID: "31111111-1111-4111-8111-111111111111", OperationID: "41111111-1111-4111-8111-111111111111",
		AccountID: account.AccountID, Sender: sender, ChainID: 1, PendingNonce: 7, PlanGeneration: 1,
		ReservedAt: now, ExpiresAt: now.Add(time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	approval := evm.SigningApproval{
		ApprovalID: "51111111-1111-4111-8111-111111111111", ReservationID: reservation.ReservationID,
		AccountID: account.AccountID, Sender: sender, ChainID: 1, Nonce: reservation.Nonce, AuthorizationEpoch: 1,
		PlanHash: [32]byte{1}, TransactionDigest: [32]byte{2}, RiskLevel: evm.RiskNormal, ConfirmationLevel: evm.ConfirmationStandard, ConfirmationTarget: 1,
		CreatedAt: now, ConfirmedAt: now, ExpiresAt: now.Add(time.Minute),
	}
	if err := repository.IssueApproval(context.Background(), approval); err != nil {
		t.Fatal(err)
	}
	effects := []evm.EffectEntry{{TokenID: big.NewInt(7), Amount: big.NewInt(3)}, {TokenID: big.NewInt(9), Amount: big.NewInt(5)}}
	if _, err := repository.AuthorizeSigning(context.Background(), evm.AuthorizeSigningRequest{
		TransactionID: "61111111-1111-4111-8111-111111111111", ApprovalID: approval.ApprovalID,
		ReservationID: reservation.ReservationID, AccountID: account.AccountID, Sender: sender,
		ChainID: 1, Nonce: reservation.Nonce, AuthorizationEpoch: 1,
		PlanHash: [32]byte{1}, TransactionDigest: [32]byte{2}, Operation: evm.OperationERC1155BatchTransfer,
		Counterparty: common.HexToAddress("0x2222222222222222222222222222222222222222"), AssetContract: contract,
		AssetAmount: new(big.Int), Effects: effects, ConfirmationTarget: 1, AuthorizedAt: now.Add(time.Second),
	}); err != nil {
		t.Fatal(err)
	}
	// Simulate a v11 database: erase the v12 migration and rebuild it from v11.
	if err := repository.db.Exec("DROP TRIGGER trg_evm_effect_immutable").Error; err != nil {
		t.Fatal(err)
	}
	if err := repository.db.Where("version = ?", 12).Delete(&schemaMigration{}).Error; err != nil {
		t.Fatal(err)
	}
	if err := repository.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewVaultRepository(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reopened.Close() }()
	loaded, err := reopened.GetTransaction(context.Background(), "61111111-1111-4111-8111-111111111111")
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Effects) != 2 || loaded.Effects[0].TokenID.Cmp(big.NewInt(7)) != 0 || loaded.Effects[1].Amount.Cmp(big.NewInt(5)) != 0 {
		t.Fatalf("v11->v12 upgrade lost ERC-1155 batch effects: %+v", loaded)
	}
	if err := reopened.db.Model(&evmTransactionEffectRow{}).Where("transaction_id = ?", loaded.TransactionID).Update("amount", []byte{1}).Error; err == nil {
		t.Fatal("upgrade dropped the effect immutability trigger")
	}
}
