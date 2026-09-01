package storage

import (
	"context"
	"math/big"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"blocowallet/internal/evm"
	"blocowallet/internal/wallet"
	"blocowallet/pkg/config"

	"github.com/ethereum/go-ethereum/common"
)

func TestEVMRepositoryAllowsEOAHardwareButRejectsWatchOnly(t *testing.T) {
	repository := newAccountTestRepository(t)
	account := testAccount("11111111-1111-4111-8111-111111111111", "watch-only-test")
	account.SignerKind = wallet.SignerKindWatchOnly
	account.SignerReference = "watch-only:v1:" + account.Address
	account.SecretType = ""
	account.DerivationScheme = ""
	account.DerivationPath = ""
	account.BIP39Language = ""
	account.Capabilities = 0
	account.State = wallet.AccountStateActive
	account.SecretEnvelope = nil
	if err := repository.CreateAccount(context.Background(), account); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	_, err := repository.ReserveNonce(context.Background(), evm.ReserveNonceRequest{
		ReservationID: "31111111-1111-4111-8111-111111111111", OperationID: "41111111-1111-4111-8111-111111111111",
		AccountID: account.AccountID, Sender: common.HexToAddress(account.Address), ChainID: 1, PendingNonce: 7, PlanGeneration: 1,
		ReservedAt: now, ExpiresAt: now.Add(time.Minute),
	})
	if !evm.IsErrorCode(err, evm.ErrorPolicyDenied) {
		t.Fatalf("watch-only nonce reservation returned %v", err)
	}
	external := testAccount("22222222-2222-4222-8222-222222222222", "hardware-test")
	external.SignerKind = wallet.SignerKindHardware
	external.SignerReference = "hardware:test"
	external.SecretType = ""
	external.DerivationScheme = ""
	external.DerivationPath = ""
	external.BIP39Language = ""
	external.State = wallet.AccountStateActive
	external.SecretEnvelope = nil
	if err := repository.CreateAccount(context.Background(), external); err != nil {
		t.Fatal(err)
	}
	reservation, err := repository.ReserveNonce(context.Background(), evm.ReserveNonceRequest{
		ReservationID: "32222222-2222-4222-8222-222222222222", OperationID: "42222222-2222-4222-8222-222222222222",
		AccountID: external.AccountID, Sender: common.HexToAddress(external.Address), ChainID: 1, PendingNonce: 7, PlanGeneration: 1,
		ReservedAt: now, ExpiresAt: now.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("hardware EOA nonce reservation failed: %v", err)
	}
	if reservation.AccountID != external.AccountID || reservation.Nonce != 7 {
		t.Fatalf("unexpected hardware reservation: %+v", reservation)
	}
}

func TestEVMRepositoryRecoversSignedBroadcastAfterRestart(t *testing.T) {
	root := t.TempDir()
	cfg := &config.Config{AppDir: root, DatabasePath: filepath.Join(root, "recovery.db"), Database: config.DatabaseConfig{Type: "sqlite"}}
	repository, err := NewVaultRepository(cfg)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	record := createAuthorizedTestTransaction(t, repository, now)
	raw := []byte{1, 2, 3, 4}
	if _, err := repository.BeginFirstBroadcast(context.Background(), evm.FirstBroadcastRequest{
		TransactionID: record.TransactionID, SignedPayload: raw, StartedAt: now.Add(time.Second),
	}); err != nil {
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
	recoverable, err := reopened.ListRecoverableTransactions(context.Background(), 10)
	if err != nil || len(recoverable) != 1 || recoverable[0].TransactionID != record.TransactionID || string(recoverable[0].SignedPayload) != string(raw) {
		t.Fatalf("signed broadcast did not survive restart: %+v %v", recoverable, err)
	}
}

func TestEVMRepositoryPersistsSignedPayloadBeforeIdempotentBroadcast(t *testing.T) {
	repository := newAccountTestRepository(t)
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	record := createAuthorizedTestTransaction(t, repository, now)
	raw := []byte{1, 2, 3, 4}
	first, err := repository.BeginFirstBroadcast(context.Background(), evm.FirstBroadcastRequest{
		TransactionID: record.TransactionID, SignedPayload: raw, StartedAt: now.Add(2 * time.Second),
	})
	if err != nil {
		t.Fatal(err)
	}
	first.SignedPayload[0] = 9
	if err := repository.db.Exec("UPDATE evm_transactions SET signed_payload = ? WHERE transaction_id = ?", []byte{9}, record.TransactionID).Error; err == nil {
		t.Fatal("database allowed signed payload mutation")
	}
	if err := repository.db.Exec("UPDATE evm_approvals SET plan_hash = ? WHERE approval_id = ?", make([]byte, 32), record.ApprovalID).Error; err == nil {
		t.Fatal("database allowed approval binding mutation")
	}
	second, err := repository.BeginRebroadcast(context.Background(), record.TransactionID, now.Add(3*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if second.Attempt != 2 || string(second.SignedPayload) != string(raw) || second.Hash != first.Hash {
		t.Fatalf("rebroadcast changed signed payload: first=%+v second=%+v", first, second)
	}
	if err := repository.RecordBroadcastResult(context.Background(), evm.BroadcastResult{
		TransactionID: record.TransactionID, Hash: second.Hash, Accepted: true,
		ResultCode: "accepted", CompletedAt: now.Add(4 * time.Second),
	}); err != nil {
		t.Fatal(err)
	}
	stored, err := repository.GetTransaction(context.Background(), record.TransactionID)
	if err != nil || stored.State != evm.TransactionSubmitted || stored.TransactionHash != second.Hash {
		t.Fatalf("accepted broadcast was not persisted: %+v %v", stored, err)
	}
	receipt := evm.Receipt{
		TransactionHash: second.Hash,
		Block:           evm.BlockIdentity{Number: 10, Hash: common.HexToHash("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")},
		Status:          1, GasUsed: 21_000, EffectiveGasPrice: big.NewInt(1_000_000_000),
	}
	if err := repository.RecordReceipt(context.Background(), evm.ReceiptObservation{
		TransactionID: record.TransactionID, Receipt: receipt, Confirmations: 2, ConfirmationTarget: 2,
		ObservedAt: now.Add(5 * time.Second), State: evm.TransactionConfirmed,
	}); err != nil {
		t.Fatal(err)
	}
	if err := repository.MarkReorged(context.Background(), evm.ReorgObservation{
		TransactionID: record.TransactionID, Reason: "canonical_hash_changed", ObservedAt: now.Add(6 * time.Second),
	}); err != nil {
		t.Fatal(err)
	}
	stored, err = repository.GetTransaction(context.Background(), record.TransactionID)
	if err != nil || stored.State != evm.TransactionReorged || stored.ReorgCount != 1 || stored.Receipt == nil {
		t.Fatalf("reorg transition was not persisted: %+v %v", stored, err)
	}
	recoverable, err := repository.ListRecoverableTransactions(context.Background(), 10)
	if err != nil || len(recoverable) != 1 || recoverable[0].TransactionID != record.TransactionID || string(recoverable[0].SignedPayload) != string(raw) {
		t.Fatalf("recoverable transaction was lost: %+v %v", recoverable, err)
	}
	if _, err := repository.BeginFirstBroadcast(context.Background(), evm.FirstBroadcastRequest{
		TransactionID: record.TransactionID, SignedPayload: []byte{5}, StartedAt: now.Add(4 * time.Second),
	}); !evm.IsErrorCode(err, evm.ErrorSigningFailed) {
		t.Fatalf("second first-broadcast attempt returned %v", err)
	}
}

func TestEVMRepositoryConcurrentAuthorizationHasOneWinner(t *testing.T) {
	root := t.TempDir()
	cfg := &config.Config{AppDir: root, DatabasePath: filepath.Join(root, "approval.db"), Database: config.DatabaseConfig{Type: "sqlite"}}
	firstRepository, err := NewVaultRepository(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = firstRepository.Close() }()
	account := testAccount("11111111-1111-4111-8111-111111111111", "source-concurrent-approval")
	account.State = wallet.AccountStateActive
	if err := firstRepository.CreateAccount(context.Background(), account); err != nil {
		t.Fatal(err)
	}
	secondRepository, err := NewVaultRepository(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = secondRepository.Close() }()
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	sender := common.HexToAddress(account.Address)
	reservation, err := firstRepository.ReserveNonce(context.Background(), evm.ReserveNonceRequest{
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
		PlanHash: planHash, TransactionDigest: digest, RiskLevel: evm.RiskNormal, ConfirmationLevel: evm.ConfirmationStandard,
		CreatedAt: now, ConfirmedAt: now, ExpiresAt: now.Add(time.Minute),
	}
	if err := firstRepository.IssueApproval(context.Background(), approval); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	errorsChannel := make(chan error, 2)
	for _, item := range []struct {
		repository    *GORMRepository
		transactionID string
	}{{firstRepository, "61111111-1111-4111-8111-111111111111"}, {secondRepository, "62222222-2222-4222-8222-222222222222"}} {
		item := item
		go func() {
			<-start
			_, err := item.repository.AuthorizeSigning(context.Background(), evm.AuthorizeSigningRequest{
				TransactionID: item.transactionID, ApprovalID: approval.ApprovalID, ReservationID: reservation.ReservationID,
				AccountID: account.AccountID, Sender: sender, ChainID: 1, Nonce: reservation.Nonce, AuthorizationEpoch: 1,
				PlanHash: planHash, TransactionDigest: digest, Operation: evm.OperationNativeTransfer,
				Counterparty: common.HexToAddress("0x2222222222222222222222222222222222222222"), AssetAmount: big.NewInt(1), AuthorizedAt: now.Add(time.Second),
			})
			errorsChannel <- err
		}()
	}
	close(start)
	successes := 0
	consumed := 0
	for range 2 {
		err := <-errorsChannel
		if err == nil {
			successes++
		} else if evm.IsErrorCode(err, evm.ErrorApprovalConsumed) {
			consumed++
		} else {
			t.Fatalf("unexpected concurrent authorization error: %v", err)
		}
	}
	if successes != 1 || consumed != 1 {
		t.Fatalf("authorization winners=%d consumed=%d", successes, consumed)
	}
}

func TestEVMRepositoryAuthorizesSigningExactlyOnce(t *testing.T) {
	repository := newAccountTestRepository(t)
	ctx := context.Background()
	account := testAccount("11111111-1111-4111-8111-111111111111", "source-approval")
	account.State = wallet.AccountStateActive
	if err := repository.CreateAccount(ctx, account); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	sender := common.HexToAddress(account.Address)
	reservation, err := repository.ReserveNonce(ctx, evm.ReserveNonceRequest{
		ReservationID: "31111111-1111-4111-8111-111111111111", OperationID: "41111111-1111-4111-8111-111111111111",
		AccountID: account.AccountID, Sender: sender, ChainID: 1, PendingNonce: 7, PlanGeneration: 1,
		ReservedAt: now, ExpiresAt: now.Add(time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	planHash := [32]byte{1, 2, 3}
	digest := [32]byte{4, 5, 6}
	approval := evm.SigningApproval{
		ApprovalID: "51111111-1111-4111-8111-111111111111", ReservationID: reservation.ReservationID,
		AccountID: account.AccountID, Sender: sender, ChainID: 1, Nonce: reservation.Nonce,
		AuthorizationEpoch: 1, PlanHash: planHash, TransactionDigest: digest,
		RiskLevel: evm.RiskNormal, ConfirmationLevel: evm.ConfirmationStandard,
		CreatedAt: now, ConfirmedAt: now, ExpiresAt: now.Add(time.Minute),
	}
	if err := repository.IssueApproval(ctx, approval); err != nil {
		t.Fatal(err)
	}
	request := evm.AuthorizeSigningRequest{
		TransactionID: "61111111-1111-4111-8111-111111111111", ApprovalID: approval.ApprovalID,
		ReservationID: reservation.ReservationID, AccountID: account.AccountID, Sender: sender,
		ChainID: 1, Nonce: reservation.Nonce, AuthorizationEpoch: 1,
		PlanHash: planHash, TransactionDigest: digest, Operation: evm.OperationNativeTransfer,
		Counterparty: common.HexToAddress("0x2222222222222222222222222222222222222222"), AssetAmount: big.NewInt(1), AuthorizedAt: now.Add(time.Second),
	}
	record, err := repository.AuthorizeSigning(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if record.TransactionID != request.TransactionID || record.State != evm.TransactionSigning {
		t.Fatalf("unexpected authorized transaction: %+v", record)
	}
	if err := repository.VerifyTransactionApproval(ctx, wallet.TransactionApprovalBinding{
		AccountID: account.AccountID, ChainID: 1, Digest: digest, ApprovalID: approval.ApprovalID,
	}); err != nil {
		t.Fatalf("consumed approval was not verifiable: %v", err)
	}
	if err := repository.db.Model(&wallet.Account{}).Where("account_id = ?", account.AccountID).Update("authorization_epoch", 2).Error; err != nil {
		t.Fatal(err)
	}
	if err := repository.VerifyTransactionApproval(ctx, wallet.TransactionApprovalBinding{
		AccountID: account.AccountID, ChainID: 1, Digest: digest, ApprovalID: approval.ApprovalID,
	}); err == nil {
		t.Fatal("approval survived account epoch rotation")
	}
	if err := repository.db.Model(&wallet.Account{}).Where("account_id = ?", account.AccountID).Update("authorization_epoch", 1).Error; err != nil {
		t.Fatal(err)
	}
	wrongDigest := digest
	wrongDigest[0] ^= 0xff
	if err := repository.VerifyTransactionApproval(ctx, wallet.TransactionApprovalBinding{
		AccountID: account.AccountID, ChainID: 1, Digest: wrongDigest, ApprovalID: approval.ApprovalID,
	}); err == nil {
		t.Fatal("approval verifier accepted a different digest")
	}
	if _, err := repository.AuthorizeSigning(ctx, request); !evm.IsErrorCode(err, evm.ErrorApprovalConsumed) {
		t.Fatalf("reused approval returned %T: %v", err, err)
	}
	if _, err := repository.ReserveNonce(ctx, evm.ReserveNonceRequest{
		ReservationID: "32222222-2222-4222-8222-222222222222", OperationID: "42222222-2222-4222-8222-222222222222",
		AccountID: account.AccountID, Sender: sender, ChainID: 1, PendingNonce: 7, PlanGeneration: 2,
		ReservedAt: now.Add(time.Second), ExpiresAt: now.Add(time.Minute),
	}); !evm.IsErrorCode(err, evm.ErrorNonceConflict) {
		t.Fatalf("new nonce bypassed in-flight signing recovery: %v", err)
	}
	if err := repository.RecordSigningFailure(ctx, evm.SigningFailureRequest{
		TransactionID: request.TransactionID, FailedAt: now.Add(2 * time.Second), ResultCode: "signer_rejected",
	}); err != nil {
		t.Fatal(err)
	}
	stored, err := repository.GetTransaction(ctx, request.TransactionID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.State != evm.TransactionSigningFailed || stored.Revision != 2 {
		t.Fatalf("signing failure transition was not persisted: %+v", stored)
	}
	reused, err := repository.ReserveNonce(ctx, evm.ReserveNonceRequest{
		ReservationID: "32222222-2222-4222-8222-222222222222", OperationID: "42222222-2222-4222-8222-222222222222",
		AccountID: account.AccountID, Sender: sender, ChainID: 1, PendingNonce: 7, PlanGeneration: 2,
		ReservedAt: now.Add(3 * time.Second), ExpiresAt: now.Add(time.Minute),
	})
	if err != nil || reused.Nonce != reservation.Nonce {
		t.Fatalf("signing failure left a nonce gap: %+v %v", reused, err)
	}
}

func TestEVMRepositoryExpiresUnsignedReservationAndReusesNonce(t *testing.T) {
	repository := newAccountTestRepository(t)
	ctx := context.Background()
	account := testAccount("11111111-1111-4111-8111-111111111111", "source-expiry")
	account.State = wallet.AccountStateActive
	if err := repository.CreateAccount(ctx, account); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	sender := common.HexToAddress(account.Address)
	first, err := repository.ReserveNonce(ctx, evm.ReserveNonceRequest{
		ReservationID: "31111111-1111-4111-8111-111111111111", OperationID: "41111111-1111-4111-8111-111111111111",
		AccountID: account.AccountID, Sender: sender, ChainID: 1, PendingNonce: 7, PlanGeneration: 1,
		ReservedAt: now, ExpiresAt: now.Add(time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := repository.ReserveNonce(ctx, evm.ReserveNonceRequest{
		ReservationID: "32222222-2222-4222-8222-222222222222", OperationID: "42222222-2222-4222-8222-222222222222",
		AccountID: account.AccountID, Sender: sender, ChainID: 1, PendingNonce: 7, PlanGeneration: 1,
		ReservedAt: now.Add(2 * time.Minute), ExpiresAt: now.Add(3 * time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.Nonce != second.Nonce {
		t.Fatalf("expired nonce was not reused: %d then %d", first.Nonce, second.Nonce)
	}
}

func TestEVMRepositoryInvalidatesUnsignedReservationAndReusesNonce(t *testing.T) {
	repository := newAccountTestRepository(t)
	ctx := context.Background()
	account := testAccount("11111111-1111-4111-8111-111111111111", "source-invalidate")
	account.State = wallet.AccountStateActive
	if err := repository.CreateAccount(ctx, account); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	sender := common.HexToAddress(account.Address)
	request := evm.ReserveNonceRequest{
		ReservationID: "31111111-1111-4111-8111-111111111111", OperationID: "41111111-1111-4111-8111-111111111111",
		AccountID: account.AccountID, Sender: sender, ChainID: 1, PendingNonce: 7, PlanGeneration: 1,
		ReservedAt: now, ExpiresAt: now.Add(time.Minute),
	}
	first, err := repository.ReserveNonce(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.InvalidateUnsignedReservation(ctx, evm.InvalidateReservationRequest{
		ReservationID: first.ReservationID, AccountID: account.AccountID, PlanGeneration: 1,
		InvalidatedAt: now.Add(time.Second), Reason: "user_cancelled",
	}); err != nil {
		t.Fatal(err)
	}
	request.ReservationID = "32222222-2222-4222-8222-222222222222"
	request.OperationID = "42222222-2222-4222-8222-222222222222"
	request.PlanGeneration = 2
	second, err := repository.ReserveNonce(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if second.Nonce != first.Nonce {
		t.Fatalf("invalidated unsigned nonce was not reusable: %d then %d", first.Nonce, second.Nonce)
	}
}

func TestEVMRepositoryReservationRetryIsIdempotent(t *testing.T) {
	repository := newAccountTestRepository(t)
	ctx := context.Background()
	account := testAccount("11111111-1111-4111-8111-111111111111", "source-idempotent")
	account.State = wallet.AccountStateActive
	if err := repository.CreateAccount(ctx, account); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	request := evm.ReserveNonceRequest{
		ReservationID:  "31111111-1111-4111-8111-111111111111",
		OperationID:    "41111111-1111-4111-8111-111111111111",
		AccountID:      account.AccountID,
		Sender:         common.HexToAddress(account.Address),
		ChainID:        1,
		PendingNonce:   7,
		PlanGeneration: 1,
		ReservedAt:     now,
		ExpiresAt:      now.Add(time.Minute),
	}
	first, err := repository.ReserveNonce(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	request.ReservationID = "32222222-2222-4222-8222-222222222222"
	second, err := repository.ReserveNonce(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if first.ReservationID != second.ReservationID || first.Nonce != second.Nonce {
		t.Fatalf("idempotent retry changed reservation: first=%+v second=%+v", first, second)
	}
}

func TestEVMRepositorySerializesNonceAcrossInstances(t *testing.T) {
	root := t.TempDir()
	cfg := &config.Config{AppDir: root, DatabasePath: filepath.Join(root, "evm.db"), Database: config.DatabaseConfig{Type: "sqlite"}}
	firstRepository, err := NewVaultRepository(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = firstRepository.Close() }()
	account := testAccount("11111111-1111-4111-8111-111111111111", "source-concurrent-reserve")
	account.State = wallet.AccountStateActive
	if err := firstRepository.CreateAccount(context.Background(), account); err != nil {
		t.Fatal(err)
	}
	secondRepository, err := NewVaultRepository(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = secondRepository.Close() }()

	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	start := make(chan struct{})
	type result struct {
		reservation evm.NonceReservation
		err         error
	}
	results := make(chan result, 2)
	for index, repository := range []*GORMRepository{firstRepository, secondRepository} {
		index, repository := index, repository
		go func() {
			<-start
			reservation, err := repository.ReserveNonce(context.Background(), evm.ReserveNonceRequest{
				ReservationID:  []string{"31111111-1111-4111-8111-111111111111", "32222222-2222-4222-8222-222222222222"}[index],
				OperationID:    []string{"41111111-1111-4111-8111-111111111111", "42222222-2222-4222-8222-222222222222"}[index],
				AccountID:      account.AccountID,
				Sender:         common.HexToAddress(account.Address),
				ChainID:        1,
				PendingNonce:   7,
				PlanGeneration: 1,
				ReservedAt:     now,
				ExpiresAt:      now.Add(time.Minute),
			})
			results <- result{reservation: reservation, err: err}
		}()
	}
	close(start)
	nonces := make([]int, 0, 2)
	for range 2 {
		result := <-results
		if result.err != nil {
			t.Fatalf("concurrent reservation failed: %v", result.err)
		}
		nonces = append(nonces, int(result.reservation.Nonce))
	}
	sort.Ints(nonces)
	if nonces[0] != 7 || nonces[1] != 8 {
		t.Fatalf("concurrent reservations collided: %v", nonces)
	}
}

func createAuthorizedTestTransaction(t *testing.T, repository *GORMRepository, now time.Time) evm.TransactionRecord {
	t.Helper()
	ctx := context.Background()
	account := testAccount("11111111-1111-4111-8111-111111111111", "source-broadcast")
	account.State = wallet.AccountStateActive
	if err := repository.CreateAccount(ctx, account); err != nil {
		t.Fatal(err)
	}
	sender := common.HexToAddress(account.Address)
	reservation, err := repository.ReserveNonce(ctx, evm.ReserveNonceRequest{
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
		PlanHash: planHash, TransactionDigest: digest, RiskLevel: evm.RiskNormal, ConfirmationLevel: evm.ConfirmationStandard, ConfirmationTarget: 2,
		CreatedAt: now, ConfirmedAt: now, ExpiresAt: now.Add(time.Minute),
	}
	if err := repository.IssueApproval(ctx, approval); err != nil {
		t.Fatal(err)
	}
	record, err := repository.AuthorizeSigning(ctx, evm.AuthorizeSigningRequest{
		TransactionID: "61111111-1111-4111-8111-111111111111", ApprovalID: approval.ApprovalID,
		ReservationID: reservation.ReservationID, AccountID: account.AccountID, Sender: sender,
		ChainID: 1, Nonce: reservation.Nonce, AuthorizationEpoch: 1,
		PlanHash: planHash, TransactionDigest: digest, Operation: evm.OperationNativeTransfer,
		Counterparty: common.HexToAddress("0x2222222222222222222222222222222222222222"), AssetAmount: big.NewInt(1), ConfirmationTarget: 2, AuthorizedAt: now.Add(time.Second),
	})
	if err != nil {
		t.Fatal(err)
	}
	return record
}

func TestEVMRepositoryReservesNonceAcrossAccountAliases(t *testing.T) {
	repository := newAccountTestRepository(t)
	ctx := context.Background()
	firstAccount := testAccount("11111111-1111-4111-8111-111111111111", "source-reserve-1")
	secondAccount := testAccount("22222222-2222-4222-8222-222222222222", "source-reserve-2")
	firstAccount.State = wallet.AccountStateActive
	secondAccount.State = wallet.AccountStateActive
	if err := repository.CreateAccount(ctx, firstAccount); err != nil {
		t.Fatal(err)
	}
	if err := repository.CreateAccount(ctx, secondAccount); err != nil {
		t.Fatal(err)
	}
	var transactionRepository evm.TransactionRepository = repository
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	sender := common.HexToAddress(firstAccount.Address)
	first, err := transactionRepository.ReserveNonce(ctx, evm.ReserveNonceRequest{
		ReservationID:  "31111111-1111-4111-8111-111111111111",
		OperationID:    "41111111-1111-4111-8111-111111111111",
		AccountID:      firstAccount.AccountID,
		Sender:         sender,
		ChainID:        1,
		PendingNonce:   7,
		PlanGeneration: 1,
		ReservedAt:     now,
		ExpiresAt:      now.Add(time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := transactionRepository.ReserveNonce(ctx, evm.ReserveNonceRequest{
		ReservationID:  "32222222-2222-4222-8222-222222222222",
		OperationID:    "42222222-2222-4222-8222-222222222222",
		AccountID:      secondAccount.AccountID,
		Sender:         sender,
		ChainID:        1,
		PendingNonce:   7,
		PlanGeneration: 1,
		ReservedAt:     now,
		ExpiresAt:      now.Add(time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.Nonce != 7 || second.Nonce != 8 {
		t.Fatalf("address aliases received colliding nonces: %d and %d", first.Nonce, second.Nonce)
	}
}
