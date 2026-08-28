package evm_test

import (
	"math"
	"math/big"
	"testing"
	"time"

	"blocowallet/internal/evm"

	"github.com/ethereum/go-ethereum/common"
)

func TestRepositoryCommandsRejectInvalidBindings(t *testing.T) {
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	accountID := "11111111-1111-4111-8111-111111111111"
	reservationID := "31111111-1111-4111-8111-111111111111"
	operationID := "41111111-1111-4111-8111-111111111111"
	approvalID := "51111111-1111-4111-8111-111111111111"
	transactionID := "61111111-1111-4111-8111-111111111111"
	sender := common.HexToAddress("0x1111111111111111111111111111111111111111")
	planHash := [32]byte{1}
	digest := [32]byte{2}

	validReservation := evm.ReserveNonceRequest{
		ReservationID: reservationID, OperationID: operationID, AccountID: accountID, Sender: sender,
		ChainID: 1, PendingNonce: 1, PlanGeneration: 1, ReservedAt: now, ExpiresAt: now.Add(time.Minute),
	}
	if err := evm.ValidateReserveNonceRequest(validReservation); err != nil {
		t.Fatal(err)
	}
	reservationMutations := []func(*evm.ReserveNonceRequest){
		func(value *evm.ReserveNonceRequest) { value.ReservationID = "bad" },
		func(value *evm.ReserveNonceRequest) { value.OperationID = "bad" },
		func(value *evm.ReserveNonceRequest) { value.AccountID = "bad" },
		func(value *evm.ReserveNonceRequest) { value.Sender = common.Address{} },
		func(value *evm.ReserveNonceRequest) { value.ChainID = 0 },
		func(value *evm.ReserveNonceRequest) { value.ChainID = math.MaxUint64 },
		func(value *evm.ReserveNonceRequest) { value.PendingNonce = math.MaxUint64 },
		func(value *evm.ReserveNonceRequest) { value.PlanGeneration = 0 },
		func(value *evm.ReserveNonceRequest) { value.ReservedAt = time.Time{} },
		func(value *evm.ReserveNonceRequest) { value.ExpiresAt = value.ReservedAt },
	}
	for _, mutate := range reservationMutations {
		value := validReservation
		mutate(&value)
		if err := evm.ValidateReserveNonceRequest(value); err == nil {
			t.Fatal("invalid nonce reservation was accepted")
		}
	}

	validApproval := evm.SigningApproval{
		ApprovalID: approvalID, ReservationID: reservationID, AccountID: accountID, Sender: sender,
		ChainID: 1, Nonce: 1, AuthorizationEpoch: 1, PlanHash: planHash, TransactionDigest: digest,
		RiskLevel: evm.RiskNormal, ConfirmationLevel: evm.ConfirmationStandard,
		CreatedAt: now, ConfirmedAt: now, ExpiresAt: now.Add(time.Minute),
	}
	if err := evm.ValidateSigningApproval(validApproval); err != nil {
		t.Fatal(err)
	}
	approvalMutations := []func(*evm.SigningApproval){
		func(value *evm.SigningApproval) { value.ApprovalID = "bad" },
		func(value *evm.SigningApproval) { value.Sender = common.Address{} },
		func(value *evm.SigningApproval) { value.PlanHash = [32]byte{} },
		func(value *evm.SigningApproval) { value.TransactionDigest = [32]byte{} },
		func(value *evm.SigningApproval) { value.RiskLevel = "unknown" },
		func(value *evm.SigningApproval) { value.ConfirmationLevel = "unknown" },
		func(value *evm.SigningApproval) { value.RiskLevel = evm.RiskCritical },
		func(value *evm.SigningApproval) { value.ConfirmedAt = now.Add(-time.Second) },
		func(value *evm.SigningApproval) { value.ExpiresAt = now },
	}
	for _, mutate := range approvalMutations {
		value := validApproval
		mutate(&value)
		if err := evm.ValidateSigningApproval(value); err == nil {
			t.Fatal("invalid signing approval was accepted")
		}
	}

	validAuthorization := evm.AuthorizeSigningRequest{
		TransactionID: transactionID, ApprovalID: approvalID, ReservationID: reservationID, AccountID: accountID,
		Sender: sender, ChainID: 1, Nonce: 1, AuthorizationEpoch: 1,
		PlanHash: planHash, TransactionDigest: digest, Operation: evm.OperationNativeTransfer,
		Counterparty: common.HexToAddress("0x2222222222222222222222222222222222222222"), AssetAmount: big.NewInt(1), AuthorizedAt: now,
	}
	if err := evm.ValidateAuthorizeSigningRequest(validAuthorization); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*evm.AuthorizeSigningRequest){
		func(value *evm.AuthorizeSigningRequest) { value.TransactionID = "bad" },
		func(value *evm.AuthorizeSigningRequest) { value.Sender = common.Address{} },
		func(value *evm.AuthorizeSigningRequest) { value.PlanHash = [32]byte{} },
		func(value *evm.AuthorizeSigningRequest) { value.AuthorizedAt = time.Time{} },
	} {
		value := validAuthorization
		mutate(&value)
		if err := evm.ValidateAuthorizeSigningRequest(value); err == nil {
			t.Fatal("invalid signing authorization was accepted")
		}
	}

	if err := evm.ValidateInvalidateReservationRequest(evm.InvalidateReservationRequest{
		ReservationID: reservationID, AccountID: accountID, PlanGeneration: 1, InvalidatedAt: now, Reason: "user_cancelled",
	}); err != nil {
		t.Fatal(err)
	}
	if err := evm.ValidateInvalidateReservationRequest(evm.InvalidateReservationRequest{ReservationID: "bad"}); err == nil {
		t.Fatal("invalid reservation invalidation was accepted")
	}
	if err := evm.ValidateInvalidateReservationRequest(evm.InvalidateReservationRequest{ReservationID: reservationID, AccountID: accountID, PlanGeneration: 1, InvalidatedAt: now, Reason: "arbitrary"}); err == nil {
		t.Fatal("arbitrary invalidation reason was accepted")
	}

	if err := evm.ValidateSigningFailureRequest(evm.SigningFailureRequest{TransactionID: transactionID, FailedAt: now, ResultCode: "signer_rejected"}); err != nil {
		t.Fatal(err)
	}
	if err := evm.ValidateSigningFailureRequest(evm.SigningFailureRequest{TransactionID: transactionID, FailedAt: now, ResultCode: "raw remote message"}); err == nil {
		t.Fatal("unbounded signing failure code was accepted")
	}

	if err := evm.ValidateFirstBroadcastRequest(evm.FirstBroadcastRequest{TransactionID: transactionID, SignedPayload: []byte{1}, StartedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := evm.ValidateFirstBroadcastRequest(evm.FirstBroadcastRequest{TransactionID: transactionID, SignedPayload: nil, StartedAt: now}); err == nil {
		t.Fatal("empty first broadcast was accepted")
	}

	hash := common.HexToHash("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if err := evm.ValidateBroadcastResult(evm.BroadcastResult{TransactionID: transactionID, Hash: hash, Accepted: true, ResultCode: "accepted", CompletedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := evm.ValidateBroadcastResult(evm.BroadcastResult{TransactionID: transactionID, Hash: hash, Accepted: true, ResultCode: "remote_rejected", CompletedAt: now}); err == nil {
		t.Fatal("inconsistent broadcast result was accepted")
	}

	receipt := evm.Receipt{
		TransactionHash: hash, Block: evm.BlockIdentity{Number: 1, Hash: hash}, Status: 1,
		GasUsed: 21_000, EffectiveGasPrice: big.NewInt(1),
	}
	if err := evm.ValidateReceiptObservation(evm.ReceiptObservation{
		TransactionID: transactionID, Receipt: receipt, Confirmations: 1, ConfirmationTarget: 1, ObservedAt: now, State: evm.TransactionConfirmed,
	}); err != nil {
		t.Fatal(err)
	}
	if err := evm.ValidateReceiptObservation(evm.ReceiptObservation{TransactionID: transactionID}); err == nil {
		t.Fatal("empty receipt observation was accepted")
	}
	if err := evm.ValidateReorgObservation(evm.ReorgObservation{TransactionID: transactionID, Reason: "canonical_hash_changed", ObservedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := evm.ValidateReorgObservation(evm.ReorgObservation{TransactionID: transactionID, Reason: "remote text", ObservedAt: now}); err == nil {
		t.Fatal("arbitrary reorg reason was accepted")
	}
}
