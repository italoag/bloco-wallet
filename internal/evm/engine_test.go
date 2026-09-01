package evm_test

import (
	"context"
	"fmt"
	"math/big"
	"testing"
	"time"

	"blocowallet/internal/evm"
	"blocowallet/internal/wallet"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

type engineRPC struct {
	simulationRPC
	header       evm.BlockHeader
	pendingNonce uint64
	sent         []byte
	sendError    error
	sendHash     common.Hash
	binding      evm.ProviderBinding
	headerError  error
	pendingError error
}

func (rpc *engineRPC) ProviderBinding() evm.ProviderBinding {
	if rpc.binding != (evm.ProviderBinding{}) {
		return rpc.binding
	}
	return evm.ProviderBinding{1}
}
func (rpc *engineRPC) LatestHeader(context.Context) (evm.BlockHeader, error) {
	return rpc.header, nil
}
func (rpc *engineRPC) PendingNonceAt(context.Context, common.Address) (uint64, error) {
	return rpc.pendingNonce, rpc.pendingError
}
func (rpc *engineRPC) HeaderByNumber(_ context.Context, number uint64) (evm.BlockHeader, bool, error) {
	return rpc.header, rpc.header.Number == number, rpc.headerError
}
func (rpc *engineRPC) SendRawTransaction(_ context.Context, raw []byte) (common.Hash, error) {
	rpc.sent = append([]byte(nil), raw...)
	if rpc.sendError != nil {
		return common.Hash{}, rpc.sendError
	}
	if rpc.sendHash != (common.Hash{}) {
		return rpc.sendHash, nil
	}
	return crypto.Keccak256Hash(raw), nil
}

func optionalBigIntClone(value *big.Int) *big.Int {
	if value == nil {
		return nil
	}
	return new(big.Int).Set(value)
}

type engineRepository struct {
	reservation      evm.NonceReservation
	approval         evm.SigningApproval
	record           evm.TransactionRecord
	attempt          evm.BroadcastAttempt
	result           evm.BroadcastResult
	invalidated      []evm.InvalidateReservationRequest
	recoverable      []evm.TransactionRecord
	signingFailures  []evm.SigningFailureRequest
	issueError       error
	authorizeError   error
	beginError       error
	resultError      error
	rebroadcastError error
}

func (repository *engineRepository) ReserveNonce(_ context.Context, request evm.ReserveNonceRequest) (evm.NonceReservation, error) {
	repository.reservation = evm.NonceReservation{
		ReservationID: request.ReservationID, OperationID: request.OperationID, AccountID: request.AccountID,
		Sender: request.Sender, ChainID: request.ChainID, Nonce: request.PendingNonce,
		PlanGeneration: request.PlanGeneration, State: evm.NonceReserved,
		ReservedAt: request.ReservedAt, ExpiresAt: request.ExpiresAt, Revision: 1,
	}
	return repository.reservation, nil
}
func (repository *engineRepository) InvalidateUnsignedReservation(_ context.Context, request evm.InvalidateReservationRequest) error {
	repository.invalidated = append(repository.invalidated, request)
	return nil
}
func (repository *engineRepository) IssueApproval(_ context.Context, approval evm.SigningApproval) error {
	repository.approval = approval
	return repository.issueError
}
func (repository *engineRepository) AuthorizeSigning(_ context.Context, request evm.AuthorizeSigningRequest) (evm.TransactionRecord, error) {
	repository.record = evm.TransactionRecord{
		TransactionID: request.TransactionID, ApprovalID: request.ApprovalID, ReservationID: request.ReservationID,
		AccountID: request.AccountID, Sender: request.Sender, ChainID: request.ChainID, Nonce: request.Nonce,
		PlanHash: request.PlanHash, TransactionDigest: request.TransactionDigest,
		Operation: request.Operation, Counterparty: request.Counterparty, AssetContract: request.AssetContract, AssetAmount: new(big.Int).Set(request.AssetAmount),
		TokenID: optionalBigIntClone(request.TokenID), Effects: evm.CloneEffectEntries(request.Effects),
		State: evm.TransactionSigning, CreatedAt: request.AuthorizedAt, UpdatedAt: request.AuthorizedAt, Revision: 1,
	}
	return repository.record, repository.authorizeError
}
func (repository *engineRepository) RecordSigningFailure(_ context.Context, request evm.SigningFailureRequest) error {
	repository.signingFailures = append(repository.signingFailures, request)
	return nil
}
func (repository *engineRepository) GetTransaction(context.Context, string) (evm.TransactionRecord, error) {
	return repository.record, nil
}
func (repository *engineRepository) BeginFirstBroadcast(_ context.Context, request evm.FirstBroadcastRequest) (evm.BroadcastAttempt, error) {
	if repository.beginError != nil {
		return evm.BroadcastAttempt{}, repository.beginError
	}
	repository.attempt = evm.BroadcastAttempt{
		TransactionID: request.TransactionID, SignedPayload: append([]byte(nil), request.SignedPayload...),
		Hash: crypto.Keccak256Hash(request.SignedPayload), Attempt: 1, StartedAt: request.StartedAt,
	}
	return repository.attempt, nil
}
func (repository *engineRepository) BeginRebroadcast(context.Context, string, time.Time) (evm.BroadcastAttempt, error) {
	return repository.attempt, repository.rebroadcastError
}
func (repository *engineRepository) RecordBroadcastResult(_ context.Context, result evm.BroadcastResult) error {
	repository.result = result
	return repository.resultError
}
func (repository *engineRepository) RecordReceipt(context.Context, evm.ReceiptObservation) error {
	return nil
}
func (repository *engineRepository) MarkReorged(context.Context, evm.ReorgObservation) error {
	return nil
}
func (repository *engineRepository) ListRecoverableTransactions(context.Context, int) ([]evm.TransactionRecord, error) {
	return append([]evm.TransactionRecord(nil), repository.recoverable...), nil
}

type erc20EngineRPC struct {
	metadataRPC
	header          evm.BlockHeader
	pendingNonce    uint64
	operationResult []byte
	sent            []byte
}

func (rpc *erc20EngineRPC) LatestHeader(context.Context) (evm.BlockHeader, error) {
	return rpc.header, nil
}
func (rpc *erc20EngineRPC) PendingNonceAt(context.Context, common.Address) (uint64, error) {
	return rpc.pendingNonce, nil
}
func (rpc *erc20EngineRPC) HeaderByNumber(_ context.Context, number uint64) (evm.BlockHeader, bool, error) {
	return rpc.header, rpc.header.Number == number, nil
}
func (rpc *erc20EngineRPC) CallContract(ctx context.Context, call evm.TransactionCall, block evm.BlockIdentity) ([]byte, error) {
	if len(call.Input) == 68 {
		selector := fmt.Sprintf("%x", call.Input[:4])
		if selector == "a9059cbb" || selector == "095ea7b3" {
			if rpc.operationResult != nil {
				return append([]byte(nil), rpc.operationResult...), nil
			}
			return common.LeftPadBytes([]byte{1}, 32), nil
		}
	}
	return rpc.metadataRPC.CallContract(ctx, call, block)
}
func (rpc *erc20EngineRPC) EstimateGas(context.Context, evm.TransactionCall, evm.BlockIdentity) (uint64, error) {
	return 65_000, nil
}
func (rpc *erc20EngineRPC) SuggestGasPrice(context.Context) (*big.Int, error) {
	return big.NewInt(1_000_000_000), nil
}
func (rpc *erc20EngineRPC) SendRawTransaction(_ context.Context, raw []byte) (common.Hash, error) {
	rpc.sent = append([]byte(nil), raw...)
	return crypto.Keccak256Hash(raw), nil
}

func TestEngineApprovalRejectsInvalidContext(t *testing.T) {
	baseRequest := evm.ApprovalRequest{AuthorizationEpoch: 1, ConfirmationLevel: evm.ConfirmationStandard}
	tests := []struct {
		name      string
		configure func(*engineRPC, *evm.ApprovalRequest)
		code      evm.ErrorCode
	}{
		{"authorization epoch", func(_ *engineRPC, request *evm.ApprovalRequest) { request.AuthorizationEpoch = 0 }, evm.ErrorInvalidIntent},
		{"confirmation target", func(_ *engineRPC, request *evm.ApprovalRequest) { request.ConfirmationTarget = 10_001 }, evm.ErrorInvalidIntent},
		{"provider binding", func(rpc *engineRPC, _ *evm.ApprovalRequest) { rpc.binding = evm.ProviderBinding{2} }, evm.ErrorPlanStale},
		{"header failure", func(rpc *engineRPC, _ *evm.ApprovalRequest) { rpc.headerError = fmt.Errorf("header failure") }, evm.ErrorProviderUnavailable},
		{"nonce failure", func(rpc *engineRPC, _ *evm.ApprovalRequest) { rpc.pendingError = fmt.Errorf("nonce failure") }, evm.ErrorProviderUnavailable},
		{"advanced nonce", func(rpc *engineRPC, _ *evm.ApprovalRequest) { rpc.pendingNonce = 1 }, evm.ErrorPlanStale},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository := &engineRepository{}
			rpc := &engineRPC{simulationRPC: simulationRPC{estimate: 21_000}, header: evm.BlockHeader{
				BlockIdentity: evm.BlockIdentity{Number: 100, Hash: common.HexToHash("0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")}, GasLimit: 30_000_000,
			}}
			engine, prepared := prepareNativeEngineTest(t, repository, rpc, vectorSigner{})
			request := baseRequest
			test.configure(rpc, &request)
			if _, err := engine.ApproveSignAndBroadcast(context.Background(), wallet.CapabilityHandle{}, prepared, request); !evm.IsErrorCode(err, test.code) {
				t.Fatalf("got %v, want %s", err, test.code)
			}
		})
	}
}

func TestEngineBroadcastFailureBoundaries(t *testing.T) {
	remoteFailure := fmt.Errorf("injected failure")
	tests := []struct {
		name           string
		configure      func(*engineRepository, *engineRPC) evm.ApprovedDigestSigner
		wantLocalHash  bool
		wantResultCode string
	}{
		{"issue approval", func(repository *engineRepository, _ *engineRPC) evm.ApprovedDigestSigner {
			repository.issueError = remoteFailure
			return vectorSigner{}
		}, false, ""},
		{"authorize", func(repository *engineRepository, _ *engineRPC) evm.ApprovedDigestSigner {
			repository.authorizeError = remoteFailure
			return vectorSigner{}
		}, false, ""},
		{"signer", func(_ *engineRepository, _ *engineRPC) evm.ApprovedDigestSigner {
			return signerFunc(func(context.Context, wallet.CapabilityHandle, wallet.SoftwareSigningRequest) (wallet.SoftwareSigningResult, error) {
				return wallet.SoftwareSigningResult{}, remoteFailure
			})
		}, false, ""},
		{"persist payload", func(repository *engineRepository, _ *engineRPC) evm.ApprovedDigestSigner {
			repository.beginError = remoteFailure
			return vectorSigner{}
		}, false, ""},
		{"ambiguous broadcast", func(_ *engineRepository, rpc *engineRPC) evm.ApprovedDigestSigner {
			rpc.sendError = &evm.BroadcastError{Kind: evm.BroadcastFailureAmbiguous, Cause: remoteFailure}
			return vectorSigner{}
		}, true, "transport_unknown"},
		{"hash mismatch", func(_ *engineRepository, rpc *engineRPC) evm.ApprovedDigestSigner {
			rpc.sendHash = common.HexToHash("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
			return vectorSigner{}
		}, true, "remote_rejected"},
		{"persist result", func(repository *engineRepository, _ *engineRPC) evm.ApprovedDigestSigner {
			repository.resultError = remoteFailure
			return vectorSigner{}
		}, true, "accepted"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository := &engineRepository{}
			rpc := &engineRPC{simulationRPC: simulationRPC{estimate: 21_000}, header: evm.BlockHeader{
				BlockIdentity: evm.BlockIdentity{Number: 100, Hash: common.HexToHash("0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")}, GasLimit: 30_000_000,
			}}
			signer := test.configure(repository, rpc)
			engine, prepared := prepareNativeEngineTest(t, repository, rpc, signer)
			result, err := engine.ApproveSignAndBroadcast(context.Background(), wallet.CapabilityHandle{}, prepared, evm.ApprovalRequest{AuthorizationEpoch: 1, ConfirmationLevel: evm.ConfirmationStandard})
			if err == nil {
				t.Fatal("injected failure was ignored")
			}
			if test.wantLocalHash != (result.Hash != (common.Hash{})) {
				t.Fatalf("unexpected local hash exposure: %+v", result)
			}
			if test.wantResultCode != "" && repository.result.ResultCode != test.wantResultCode {
				t.Fatalf("result code=%q want %q", repository.result.ResultCode, test.wantResultCode)
			}
		})
	}
}

func prepareNativeEngineTest(t *testing.T, repository *engineRepository, rpc *engineRPC, signer evm.ApprovedDigestSigner) (*evm.Engine, *evm.PreparedNativeTransfer) {
	t.Helper()
	key, err := crypto.HexToECDSA("4646464646464646464646464646464646464646464646464646464646464646")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	ids := []string{"31111111-1111-4111-8111-111111111111", "51111111-1111-4111-8111-111111111111", "61111111-1111-4111-8111-111111111111"}
	index := 0
	engine, err := evm.NewEngine(repository, rpc, signer, evm.EngineOptions{
		Now: func() time.Time { return now }, NewID: func() (string, error) { value := ids[index]; index++; return value, nil },
		ReservationTTL: time.Minute, ApprovalTTL: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := engine.PrepareNative(context.Background(), evm.PrepareNativeRequest{
		OperationID: "41111111-1111-4111-8111-111111111111", PlanGeneration: 1,
		AccountID: "8b9b0587-388e-4fca-bba4-bf544ebe53ca", ChainID: 1,
		From: crypto.PubkeyToAddress(key.PublicKey), To: common.HexToAddress("0x2222222222222222222222222222222222222222"), Amount: big.NewInt(1),
	})
	if err != nil {
		t.Fatal(err)
	}
	return engine, prepared
}

type recoveryTrackerStub struct {
	state        evm.TransactionState
	rebroadcasts int
}

func (tracker *recoveryTrackerStub) TrackTransaction(context.Context, string, uint64, time.Time) (evm.TrackingResult, error) {
	return evm.TrackingResult{State: tracker.state}, nil
}

func (tracker *recoveryTrackerStub) Rebroadcast(context.Context, string) (evm.ExecutionResult, error) {
	tracker.rebroadcasts++
	return evm.ExecutionResult{}, nil
}

func TestRecoverySupervisorDoesNotRetryRemoteRejection(t *testing.T) {
	now := time.Now().UTC()
	record := evm.TransactionRecord{
		TransactionID: "61111111-1111-4111-8111-111111111111", ChainID: 1,
		State: evm.TransactionBroadcastFailed, TransactionHash: common.HexToHash("0x01"),
		BroadcastAttempts: 1, ConfirmationTarget: 1, LastResultCode: "remote_rejected", UpdatedAt: now,
	}
	repository := &engineRepository{recoverable: []evm.TransactionRecord{record}}
	tracker := &recoveryTrackerStub{state: evm.TransactionBroadcastFailed}
	supervisor, err := evm.NewRecoverySupervisor(repository, func(context.Context, uint64) (evm.RecoveryTracker, error) { return tracker, nil }, 2*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := supervisor.RecoverOnce(context.Background(), 10, now); err != nil {
		t.Fatal(err)
	}
	if tracker.rebroadcasts != 0 {
		t.Fatal("remote rejection was automatically rebroadcast")
	}
	repository.recoverable[0].LastResultCode = "transport_unknown"
	if err := supervisor.RecoverOnce(context.Background(), 10, now); err != nil {
		t.Fatal(err)
	}
	if tracker.rebroadcasts != 1 {
		t.Fatal("ambiguous transport failure was not recovered")
	}
}

func TestRecoverySupervisorReleasesStaleSigningNonce(t *testing.T) {
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	repository := &engineRepository{recoverable: []evm.TransactionRecord{{
		TransactionID: "61111111-1111-4111-8111-111111111111", State: evm.TransactionSigning, UpdatedAt: now.Add(-3 * time.Minute),
	}}}
	supervisor, err := evm.NewRecoverySupervisor(repository, func(context.Context, uint64) (evm.RecoveryTracker, error) {
		return nil, fmt.Errorf("tracker should not be needed")
	}, 2*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := supervisor.RecoverOnce(context.Background(), 10, now); err != nil {
		t.Fatal(err)
	}
	if len(repository.signingFailures) != 1 || repository.signingFailures[0].ResultCode != "persistence_failed" {
		t.Fatalf("stale signing transaction was not reconciled: %+v", repository.signingFailures)
	}
}

func TestEngineRejectsExpiredOrReorgedPreparedPlan(t *testing.T) {
	key, err := crypto.HexToECDSA("4646464646464646464646464646464646464646464646464646464646464646")
	if err != nil {
		t.Fatal(err)
	}
	clock := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	rpc := &engineRPC{simulationRPC: simulationRPC{estimate: 21_000}, header: evm.BlockHeader{
		BlockIdentity: evm.BlockIdentity{Number: 100, Hash: common.HexToHash("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")}, GasLimit: 30_000_000,
	}}
	repository := &engineRepository{}
	ids := []string{"31111111-1111-4111-8111-111111111111", "51111111-1111-4111-8111-111111111111", "61111111-1111-4111-8111-111111111111"}
	index := 0
	engine, err := evm.NewEngine(repository, rpc, vectorSigner{}, evm.EngineOptions{
		Now: func() time.Time { return clock }, NewID: func() (string, error) { value := ids[index]; index++; return value, nil },
		ReservationTTL: time.Minute, ApprovalTTL: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := engine.PrepareNative(context.Background(), evm.PrepareNativeRequest{
		OperationID: "41111111-1111-4111-8111-111111111111", PlanGeneration: 1,
		AccountID: "8b9b0587-388e-4fca-bba4-bf544ebe53ca", ChainID: 1,
		From: crypto.PubkeyToAddress(key.PublicKey), To: common.HexToAddress("0x2222222222222222222222222222222222222222"), Amount: big.NewInt(1),
	})
	if err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(2 * time.Minute)
	if _, err := engine.ApproveSignAndBroadcast(context.Background(), wallet.CapabilityHandle{}, prepared, evm.ApprovalRequest{AuthorizationEpoch: 1, ConfirmationLevel: evm.ConfirmationStandard}); !evm.IsErrorCode(err, evm.ErrorPlanStale) {
		t.Fatalf("expired plan was accepted: %v", err)
	}
	if len(repository.invalidated) != 1 {
		t.Fatalf("expired plan did not invalidate reservation: %+v", repository.invalidated)
	}

	clock = clock.Add(-2 * time.Minute)
	prepared, err = engine.PrepareNative(context.Background(), evm.PrepareNativeRequest{
		OperationID: "42222222-2222-4222-8222-222222222222", PlanGeneration: 2,
		AccountID: "8b9b0587-388e-4fca-bba4-bf544ebe53ca", ChainID: 1,
		From: crypto.PubkeyToAddress(key.PublicKey), To: common.HexToAddress("0x2222222222222222222222222222222222222222"), Amount: big.NewInt(1),
	})
	if err != nil {
		t.Fatal(err)
	}
	rpc.header.Hash = common.HexToHash("0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	if _, err := engine.ApproveSignAndBroadcast(context.Background(), wallet.CapabilityHandle{}, prepared, evm.ApprovalRequest{AuthorizationEpoch: 1, ConfirmationLevel: evm.ConfirmationStandard}); !evm.IsErrorCode(err, evm.ErrorPlanStale) {
		t.Fatalf("reorged plan was accepted: %v", err)
	}
}

func TestEngineRejectsERC20FalseSimulationResult(t *testing.T) {
	key, err := crypto.HexToECDSA("4646464646464646464646464646464646464646464646464646464646464646")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	rpc := &erc20EngineRPC{
		header:          evm.BlockHeader{BlockIdentity: evm.BlockIdentity{Number: 100, Hash: common.HexToHash("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")}, GasLimit: 30_000_000},
		operationResult: make([]byte, 32),
	}
	repository := &engineRepository{}
	engine, err := evm.NewEngine(repository, rpc, vectorSigner{}, evm.EngineOptions{
		Now: func() time.Time { return now }, NewID: func() (string, error) { return "31111111-1111-4111-8111-111111111111", nil },
		ReservationTTL: time.Minute, ApprovalTTL: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = engine.PrepareERC20Transfer(context.Background(), evm.PrepareERC20TransferRequest{
		OperationID: "41111111-1111-4111-8111-111111111111", PlanGeneration: 1,
		AccountID: "8b9b0587-388e-4fca-bba4-bf544ebe53ca", ChainID: 1, From: crypto.PubkeyToAddress(key.PublicKey),
		Contract: common.HexToAddress("0x3333333333333333333333333333333333333333"),
		To:       common.HexToAddress("0x2222222222222222222222222222222222222222"), Amount: big.NewInt(1),
	})
	if !evm.IsErrorCode(err, evm.ErrorSimulationFailed) || len(repository.invalidated) != 1 {
		t.Fatalf("ERC-20 false simulation was accepted or reservation leaked: %v invalidations=%d", err, len(repository.invalidated))
	}
}

func TestEngineRejectsEconomicallyUnsafeFeeSuggestion(t *testing.T) {
	key, err := crypto.HexToECDSA("4646464646464646464646464646464646464646464646464646464646464646")
	if err != nil {
		t.Fatal(err)
	}
	rpc := &engineRPC{
		simulationRPC: simulationRPC{estimate: 21_000, gasPrice: big.NewInt(2)},
		header:        evm.BlockHeader{BlockIdentity: evm.BlockIdentity{Number: 1, Hash: common.HexToHash("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")}, GasLimit: 30_000_000},
	}
	engine, err := evm.NewEngine(&engineRepository{}, rpc, vectorSigner{}, evm.EngineOptions{
		Now:            func() time.Time { return time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC) },
		NewID:          func() (string, error) { return "31111111-1111-4111-8111-111111111111", nil },
		ReservationTTL: time.Minute, ApprovalTTL: time.Minute,
		EconomicPolicy: evm.EconomicPolicy{MaxGasPrice: big.NewInt(1)},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = engine.PrepareNative(context.Background(), evm.PrepareNativeRequest{
		OperationID: "41111111-1111-4111-8111-111111111111", PlanGeneration: 1,
		AccountID: "8b9b0587-388e-4fca-bba4-bf544ebe53ca", ChainID: 1,
		From: crypto.PubkeyToAddress(key.PublicKey), To: common.HexToAddress("0x2222222222222222222222222222222222222222"), Amount: big.NewInt(1),
	})
	if !evm.IsErrorCode(err, evm.ErrorPolicyDenied) {
		t.Fatalf("unsafe fee was accepted: %v", err)
	}
}

func TestEngineRebroadcastsOnlyPersistedSignedPayload(t *testing.T) {
	raw := []byte{1, 2, 3, 4}
	transactionID := "61111111-1111-4111-8111-111111111111"
	repository := &engineRepository{attempt: evm.BroadcastAttempt{
		TransactionID: transactionID, ChainID: 1, SignedPayload: raw, Hash: crypto.Keccak256Hash(raw), Attempt: 2,
	}}
	rpc := &engineRPC{}
	engine, err := evm.NewEngine(repository, rpc, vectorSigner{}, evm.EngineOptions{
		ReservationTTL: time.Minute, ApprovalTTL: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := engine.Rebroadcast(context.Background(), transactionID)
	if err != nil {
		t.Fatal(err)
	}
	if string(rpc.sent) != string(raw) || result.Hash != crypto.Keccak256Hash(raw) || repository.result.ResultCode != "accepted" {
		t.Fatalf("rebroadcast did not reuse persisted payload: result=%+v sent=%x", result, rpc.sent)
	}
}

func TestEngineRebroadcastFailureBoundaries(t *testing.T) {
	raw := []byte{1, 2, 3, 4}
	transactionID := "61111111-1111-4111-8111-111111111111"
	localHash := crypto.Keccak256Hash(raw)
	tests := []struct {
		name          string
		configure     func(*engineRepository, *engineRPC)
		wantLocalHash bool
		resultCode    string
	}{
		{"load persisted bytes", func(repository *engineRepository, _ *engineRPC) {
			repository.rebroadcastError = fmt.Errorf("load failure")
		}, false, ""},
		{"ambiguous transport", func(_ *engineRepository, rpc *engineRPC) {
			rpc.sendError = &evm.BroadcastError{Kind: evm.BroadcastFailureAmbiguous}
		}, true, "transport_unknown"},
		{"hash mismatch", func(_ *engineRepository, rpc *engineRPC) {
			rpc.sendHash = common.HexToHash("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
		}, true, "remote_rejected"},
		{"persist result", func(repository *engineRepository, _ *engineRPC) {
			repository.resultError = fmt.Errorf("persist failure")
		}, true, "accepted"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository := &engineRepository{attempt: evm.BroadcastAttempt{TransactionID: transactionID, ChainID: 1, SignedPayload: raw, Hash: localHash, Attempt: 2}}
			rpc := &engineRPC{}
			test.configure(repository, rpc)
			engine, err := evm.NewEngine(repository, rpc, vectorSigner{}, evm.EngineOptions{ReservationTTL: time.Minute, ApprovalTTL: time.Minute})
			if err != nil {
				t.Fatal(err)
			}
			result, err := engine.Rebroadcast(context.Background(), transactionID)
			if err == nil {
				t.Fatal("injected rebroadcast failure was ignored")
			}
			if test.wantLocalHash != (result.Hash == localHash) {
				t.Fatalf("unexpected rebroadcast result: %+v", result)
			}
			if test.resultCode != "" && repository.result.ResultCode != test.resultCode {
				t.Fatalf("result code=%q want=%q", repository.result.ResultCode, test.resultCode)
			}
		})
	}
}

func TestEngineRequiresReinforcedConfirmationForFiniteApproval(t *testing.T) {
	privateKey, err := crypto.HexToECDSA("4646464646464646464646464646464646464646464646464646464646464646")
	if err != nil {
		t.Fatal(err)
	}
	from := crypto.PubkeyToAddress(privateKey.PublicKey)
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	rpc := &erc20EngineRPC{header: evm.BlockHeader{
		BlockIdentity: evm.BlockIdentity{Number: 100, Hash: common.HexToHash("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")},
		GasLimit:      30_000_000,
		BaseFeePerGas: big.NewInt(1_000_000_000),
	}, pendingNonce: 4}
	repository := &engineRepository{}
	ids := []string{"31111111-1111-4111-8111-111111111111", "51111111-1111-4111-8111-111111111111", "61111111-1111-4111-8111-111111111111"}
	index := 0
	engine, err := evm.NewEngine(repository, rpc, vectorSigner{}, evm.EngineOptions{
		Now: func() time.Time { return now }, NewID: func() (string, error) {
			value := ids[index]
			index++
			return value, nil
		}, ReservationTTL: time.Minute, ApprovalTTL: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	prepareRequest := evm.PrepareERC20ApproveRequest{
		OperationID: "41111111-1111-4111-8111-111111111111", PlanGeneration: 1,
		AccountID: "8b9b0587-388e-4fca-bba4-bf544ebe53ca", ChainID: 1, From: from,
		Contract: common.HexToAddress("0x3333333333333333333333333333333333333333"),
		Spender:  common.HexToAddress("0x2222222222222222222222222222222222222222"), Amount: big.NewInt(1_500_000),
	}
	rpc.allowance = big.NewInt(1)
	if _, err := engine.PrepareERC20Approve(context.Background(), prepareRequest); !evm.IsErrorCode(err, evm.ErrorPolicyDenied) {
		t.Fatalf("nonzero-to-nonzero allowance change was accepted: %v", err)
	}
	rpc.allowance = nil
	prepared, err := engine.PrepareERC20Approve(context.Background(), prepareRequest)
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Plan().Operation() != evm.OperationERC20Approve || prepared.Plan().Transaction().Type() != 2 {
		t.Fatalf("unexpected prepared operation: %s", prepared.Plan().Operation())
	}
	if _, err := engine.ApproveSignAndBroadcast(context.Background(), wallet.CapabilityHandle{}, prepared, evm.ApprovalRequest{
		AuthorizationEpoch: 1, RiskLevel: evm.RiskNormal, ConfirmationLevel: evm.ConfirmationStandard,
	}); !evm.IsErrorCode(err, evm.ErrorPolicyDenied) {
		t.Fatalf("approve without reinforced confirmation was accepted: %v", err)
	}
	result, err := engine.ApproveSignAndBroadcast(context.Background(), wallet.CapabilityHandle{}, prepared, evm.ApprovalRequest{
		AuthorizationEpoch: 1, RiskLevel: evm.RiskCritical, ConfirmationLevel: evm.ConfirmationReinforced,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Hash == (common.Hash{}) || result.Hash != crypto.Keccak256Hash(rpc.sent) {
		t.Fatalf("finite approval broadcast mismatch: %+v", result)
	}
}

func TestEngineExecutesERC20TransferWithOnChainMetadata(t *testing.T) {
	privateKey, err := crypto.HexToECDSA("4646464646464646464646464646464646464646464646464646464646464646")
	if err != nil {
		t.Fatal(err)
	}
	from := crypto.PubkeyToAddress(privateKey.PublicKey)
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	rpc := &erc20EngineRPC{
		header: evm.BlockHeader{
			BlockIdentity: evm.BlockIdentity{Number: 100, Hash: common.HexToHash("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")},
			GasLimit:      30_000_000,
			BaseFeePerGas: big.NewInt(1_000_000_000),
		},
		pendingNonce: 3,
	}
	repository := &engineRepository{}
	ids := []string{"31111111-1111-4111-8111-111111111111", "51111111-1111-4111-8111-111111111111", "61111111-1111-4111-8111-111111111111"}
	index := 0
	engine, err := evm.NewEngine(repository, rpc, vectorSigner{}, evm.EngineOptions{
		Now: func() time.Time { return now }, NewID: func() (string, error) {
			value := ids[index]
			index++
			return value, nil
		}, ReservationTTL: time.Minute, ApprovalTTL: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := engine.PrepareERC20Transfer(context.Background(), evm.PrepareERC20TransferRequest{
		OperationID: "41111111-1111-4111-8111-111111111111", PlanGeneration: 1,
		AccountID: "8b9b0587-388e-4fca-bba4-bf544ebe53ca", ChainID: 1, From: from,
		Contract: common.HexToAddress("0x3333333333333333333333333333333333333333"),
		To:       common.HexToAddress("0x2222222222222222222222222222222222222222"), Amount: big.NewInt(1_500_000),
	})
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Plan().Asset().Symbol != "USDC" || prepared.Plan().Transaction().Type() != 2 || prepared.Plan().Transaction().Gas() != 65_000 || len(prepared.Plan().Transaction().Data()) != 68 {
		t.Fatalf("unexpected prepared ERC-20 transfer: asset=%+v tx=%+v", prepared.Plan().Asset(), prepared.Plan().Transaction())
	}
	result, err := engine.ApproveSignAndBroadcast(context.Background(), wallet.CapabilityHandle{}, prepared, evm.ApprovalRequest{
		AuthorizationEpoch: 1, RiskLevel: evm.RiskNormal, ConfirmationLevel: evm.ConfirmationStandard,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Hash == (common.Hash{}) || result.Hash != crypto.Keccak256Hash(rpc.sent) {
		t.Fatalf("ERC-20 broadcast mismatch: %+v sent=%x", result, rpc.sent)
	}
}

func TestEngineExecutesNativeTransferWithoutTransactionMutation(t *testing.T) {
	privateKey, err := crypto.HexToECDSA("4646464646464646464646464646464646464646464646464646464646464646")
	if err != nil {
		t.Fatal(err)
	}
	from := crypto.PubkeyToAddress(privateKey.PublicKey)
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	rpc := &engineRPC{
		simulationRPC: simulationRPC{callResult: []byte{}, estimate: 21_000},
		header: evm.BlockHeader{
			BlockIdentity: evm.BlockIdentity{Number: 100, Hash: common.HexToHash("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")},
			GasLimit:      30_000_000,
		},
		pendingNonce: 7,
	}
	repository := &engineRepository{}
	ids := []string{
		"31111111-1111-4111-8111-111111111111",
		"51111111-1111-4111-8111-111111111111",
		"61111111-1111-4111-8111-111111111111",
	}
	idIndex := 0
	engine, err := evm.NewEngine(repository, rpc, vectorSigner{}, evm.EngineOptions{
		Now: func() time.Time { return now },
		NewID: func() (string, error) {
			value := ids[idIndex]
			idIndex++
			return value, nil
		},
		ReservationTTL: time.Minute,
		ApprovalTTL:    time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := engine.PrepareNative(context.Background(), evm.PrepareNativeRequest{
		OperationID: "41111111-1111-4111-8111-111111111111", PlanGeneration: 1,
		AccountID: "8b9b0587-388e-4fca-bba4-bf544ebe53ca", ChainID: 1, From: from,
		To:     common.HexToAddress("0x3535353535353535353535353535353535353535"),
		Amount: big.NewInt(1_000_000_000_000_000_000),
	})
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Plan().Transaction().Nonce() != 7 || prepared.Plan().Transaction().Gas() != 21_000 {
		t.Fatalf("unexpected prepared transaction: %+v", prepared.Plan().Transaction())
	}
	if prepared.Plan().SimulationResultHash() == (common.Hash{}) || len(prepared.Findings()) == 0 || prepared.Findings()[0].ID != evm.RiskFindingNewRecipient {
		t.Fatalf("simulation and risk findings were not bound: %+v", prepared.Findings())
	}
	result, err := engine.ApproveSignAndBroadcast(context.Background(), wallet.CapabilityHandle{}, prepared, evm.ApprovalRequest{
		AuthorizationEpoch: 1, RiskLevel: evm.RiskNormal, ConfirmationLevel: evm.ConfirmationStandard,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Hash == (common.Hash{}) || result.Hash != crypto.Keccak256Hash(rpc.sent) || repository.result.ResultCode != "accepted" || !repository.result.Accepted {
		t.Fatalf("unexpected execution result: %+v repository=%+v", result, repository.result)
	}
	if repository.approval.PlanHash != prepared.Plan().PlanHash() || repository.approval.TransactionDigest != prepared.Plan().TransactionDigest() {
		t.Fatal("approval was not bound to frozen plan and transaction digest")
	}
}
