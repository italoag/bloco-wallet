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

func TestEVMRepositoryPersistsERC1155TokenAndBatchEffects(t *testing.T) {
	repository := newAccountTestRepository(t)
	account := testAccount("11111111-1111-4111-8111-111111111111", "erc1155-effects")
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
	planHash := [32]byte{1}
	digest := [32]byte{2}
	approval := evm.SigningApproval{
		ApprovalID: "51111111-1111-4111-8111-111111111111", ReservationID: reservation.ReservationID,
		AccountID: account.AccountID, Sender: sender, ChainID: 1, Nonce: reservation.Nonce, AuthorizationEpoch: 1,
		PlanHash: planHash, TransactionDigest: digest, RiskLevel: evm.RiskNormal, ConfirmationLevel: evm.ConfirmationStandard, ConfirmationTarget: 1,
		CreatedAt: now, ConfirmedAt: now, ExpiresAt: now.Add(time.Minute),
	}
	if err := repository.IssueApproval(context.Background(), approval); err != nil {
		t.Fatal(err)
	}
	singleRecord, err := repository.AuthorizeSigning(context.Background(), evm.AuthorizeSigningRequest{
		TransactionID: "61111111-1111-4111-8111-111111111111", ApprovalID: approval.ApprovalID,
		ReservationID: reservation.ReservationID, AccountID: account.AccountID, Sender: sender,
		ChainID: 1, Nonce: reservation.Nonce, AuthorizationEpoch: 1,
		PlanHash: planHash, TransactionDigest: digest, Operation: evm.OperationERC1155SafeTransfer,
		Counterparty: common.HexToAddress("0x2222222222222222222222222222222222222222"), AssetContract: contract,
		AssetAmount: big.NewInt(3), TokenID: big.NewInt(7), ConfirmationTarget: 1, AuthorizedAt: now.Add(time.Second),
	})
	if err != nil {
		t.Fatal(err)
	}
	if singleRecord.TokenID == nil || singleRecord.TokenID.Cmp(big.NewInt(7)) != 0 || singleRecord.AssetAmount.Cmp(big.NewInt(3)) != 0 {
		t.Fatalf("ERC-1155 single token was not persisted: %+v", singleRecord)
	}
	loaded, err := repository.GetTransaction(context.Background(), singleRecord.TransactionID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.TokenID == nil || loaded.TokenID.Cmp(big.NewInt(7)) != 0 || len(loaded.Effects) != 0 {
		t.Fatalf("ERC-1155 single token did not survive round-trip: %+v", loaded)
	}
	if err := repository.RecordSigningFailure(context.Background(), evm.SigningFailureRequest{
		TransactionID: singleRecord.TransactionID, FailedAt: now.Add(2 * time.Second), ResultCode: "cancelled",
	}); err != nil {
		t.Fatal(err)
	}

	batchReservation, err := repository.ReserveNonce(context.Background(), evm.ReserveNonceRequest{
		ReservationID: "32222222-2222-4222-8222-222222222222", OperationID: "42222222-2222-4222-8222-222222222222",
		AccountID: account.AccountID, Sender: sender, ChainID: 1, PendingNonce: 8, PlanGeneration: 2,
		ReservedAt: now, ExpiresAt: now.Add(time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	batchApproval := approval
	batchApproval.ApprovalID = "52222222-2222-4222-8222-222222222222"
	batchApproval.ReservationID = batchReservation.ReservationID
	batchApproval.Nonce = batchReservation.Nonce
	batchApproval.PlanHash = [32]byte{3}
	batchApproval.TransactionDigest = [32]byte{4}
	if err := repository.IssueApproval(context.Background(), batchApproval); err != nil {
		t.Fatal(err)
	}
	effects := []evm.EffectEntry{{TokenID: big.NewInt(7), Amount: big.NewInt(3)}, {TokenID: big.NewInt(9), Amount: big.NewInt(5)}}
	batchRecord, err := repository.AuthorizeSigning(context.Background(), evm.AuthorizeSigningRequest{
		TransactionID: "62222222-2222-4222-8222-222222222222", ApprovalID: batchApproval.ApprovalID,
		ReservationID: batchReservation.ReservationID, AccountID: account.AccountID, Sender: sender,
		ChainID: 1, Nonce: batchReservation.Nonce, AuthorizationEpoch: 1,
		PlanHash: [32]byte{3}, TransactionDigest: [32]byte{4}, Operation: evm.OperationERC1155BatchTransfer,
		Counterparty: common.HexToAddress("0x2222222222222222222222222222222222222222"), AssetContract: contract,
		AssetAmount: new(big.Int), Effects: effects, ConfirmationTarget: 1, AuthorizedAt: now.Add(2 * time.Second),
	})
	if err != nil {
		t.Fatal(err)
	}
	loadedBatch, err := repository.GetTransaction(context.Background(), batchRecord.TransactionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(loadedBatch.Effects) != 2 || loadedBatch.Effects[0].TokenID.Cmp(big.NewInt(7)) != 0 || loadedBatch.Effects[1].Amount.Cmp(big.NewInt(5)) != 0 || loadedBatch.TokenID != nil {
		t.Fatalf("ERC-1155 batch effects did not survive round-trip: %+v", loadedBatch)
	}
	if err := repository.db.Model(&evmTransactionEffectRow{}).Where("transaction_id = ?", loadedBatch.TransactionID).Update("amount", []byte{1}).Error; err == nil {
		t.Fatal("database allowed immutable ERC-1155 effect mutation")
	}
}

func TestERC1155RepositoryBacksDurableEngineFlow(t *testing.T) {
	root := t.TempDir()
	cfg := &config.Config{AppDir: root, DatabasePath: filepath.Join(root, "erc1155.db"), Database: config.DatabaseConfig{Type: "sqlite"}}
	repository, err := NewVaultRepository(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = repository.Close() }()
	record := createAuthorizedTestTransaction(t, repository, time.Now().UTC())
	raw := []byte{1, 2, 3, 4}
	if _, err := repository.BeginFirstBroadcast(context.Background(), evm.FirstBroadcastRequest{
		TransactionID: record.TransactionID, SignedPayload: raw, StartedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	recoverable, err := repository.ListRecoverableTransactions(context.Background(), 10)
	if err != nil || len(recoverable) != 1 || recoverable[0].TransactionID != record.TransactionID {
		t.Fatalf("ERC-1155 schema broke recovery: %+v %v", recoverable, err)
	}
}
