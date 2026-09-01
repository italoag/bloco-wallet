package evm_test

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"blocowallet/internal/evm"
	"blocowallet/internal/wallet"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
)

type messageRepositoryStub struct {
	approval  evm.MessageApproval
	record    evm.MessageSigningRecord
	completed evm.CompleteMessageSigningRequest
	failed    evm.FailMessageSigningRequest
}

func (repository *messageRepositoryStub) IssueMessageApproval(_ context.Context, approval evm.MessageApproval) error {
	repository.approval = approval
	return nil
}

func (repository *messageRepositoryStub) AuthorizeMessageSigning(_ context.Context, request evm.AuthorizeMessageSigningRequest) (evm.MessageSigningRecord, error) {
	repository.approval.State = evm.MessageApprovalConsumed
	repository.record = evm.MessageSigningRecord{
		SigningID: request.SigningID, ApprovalID: request.ApprovalID, AccountID: request.AccountID,
		Signer: request.Signer, Scheme: request.Scheme, ChainID: request.ChainID,
		Digest: request.Digest, IntentHash: request.IntentHash, State: evm.MessageSigningInProgress,
		CreatedAt: request.AuthorizedAt,
	}
	return repository.record, nil
}

func (repository *messageRepositoryStub) CompleteMessageSigning(_ context.Context, request evm.CompleteMessageSigningRequest) error {
	repository.completed = request
	return nil
}

func (repository *messageRepositoryStub) FailMessageSigning(_ context.Context, request evm.FailMessageSigningRequest) error {
	repository.failed = request
	return nil
}

type personalSignerStub struct {
	key     []byte
	request wallet.SoftwareSigningRequest
	err     error
}

func (signer *personalSignerStub) Sign(_ context.Context, _ wallet.CapabilityHandle, request wallet.SoftwareSigningRequest) (wallet.SoftwareSigningResult, error) {
	signer.request = request
	if signer.err != nil {
		return wallet.SoftwareSigningResult{}, signer.err
	}
	key, err := crypto.ToECDSA(signer.key)
	if err != nil {
		return wallet.SoftwareSigningResult{}, err
	}
	signature, err := crypto.Sign(request.Digest[:], key)
	if err != nil {
		return wallet.SoftwareSigningResult{}, err
	}
	return wallet.SoftwareSigningResult{
		AccountID: request.AccountID, Purpose: request.Purpose, MessageScheme: request.MessageScheme,
		ChainID: request.ChainID, Digest: request.Digest, IntentHash: request.IntentHash, Signature: signature,
	}, nil
}

func TestPreparePersonalSignMatchesOfficialEIP191Vector(t *testing.T) {
	prepared, err := evm.PreparePersonalSign(evm.PreparePersonalSignRequest{
		AccountID: "11111111-1111-4111-8111-111111111111",
		Signer:    common.HexToAddress("0x1563915e194D8CfBA1943570603F7606A3115508"),
		Message:   []byte("Hello Joe"),
		Origin:    "local-user",
	})
	if err != nil {
		t.Fatal(err)
	}
	preview := prepared.Preview()
	expected := hexutil.MustDecode("0xa080337ae51c4e064c189e113edd0ba391df9206e2f49db658bb32cf2911730b")
	if !bytes.Equal(preview.Digest[:], expected) || preview.MessageLength != len("Hello Joe") || !preview.UTF8 || preview.IntentHash == ([32]byte{}) {
		t.Fatalf("unexpected EIP-191 preview: %+v", preview)
	}
	preview.Message[0] = 'X'
	if string(prepared.Preview().Message) != "Hello Joe" {
		t.Fatal("personal-sign preview exposed mutable message bytes")
	}
	changed, err := evm.PreparePersonalSign(evm.PreparePersonalSignRequest{
		AccountID: "11111111-1111-4111-8111-111111111111", Signer: preview.Signer,
		Message: []byte("Hello Joe"), Origin: "different-origin",
	})
	if err != nil {
		t.Fatal(err)
	}
	if changed.Preview().IntentHash == preview.IntentHash || changed.Preview().Digest != preview.Digest {
		t.Fatal("intent commitment did not bind origin independently from EIP-191 digest")
	}
}

func TestPersonalSignServiceBindsApprovalAndReturnsEthereumSignature(t *testing.T) {
	privateKey := hexutil.MustDecode("0x4646464646464646464646464646464646464646464646464646464646464646")
	key, err := crypto.ToECDSA(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	signerAddress := crypto.PubkeyToAddress(key.PublicKey)
	prepared, err := evm.PreparePersonalSign(evm.PreparePersonalSignRequest{
		AccountID: "11111111-1111-4111-8111-111111111111", Signer: signerAddress,
		Message: []byte("Approve exact bytes"), Origin: "local-user",
	})
	if err != nil {
		t.Fatal(err)
	}
	repository := &messageRepositoryStub{}
	signer := &personalSignerStub{key: privateKey}
	ids := []string{"51111111-1111-4111-8111-111111111111", "71111111-1111-4111-8111-111111111111"}
	index := 0
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
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
	preview := prepared.Preview()
	result, err := service.ApproveAndSignPersonal(context.Background(), wallet.CapabilityHandle{}, prepared, evm.PersonalSignApprovalRequest{
		AuthorizationEpoch: 1, ConfirmedIntentHash: preview.IntentHash, ConfirmationLevel: evm.ConfirmationReinforced,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Signature) != crypto.SignatureLength || (result.Signature[64] != 27 && result.Signature[64] != 28) {
		t.Fatalf("personal signature has incompatible V: %x", result.Signature)
	}
	recoverable := append([]byte(nil), result.Signature...)
	recoverable[64] -= 27
	publicKey, err := crypto.SigToPub(result.Digest[:], recoverable)
	if err != nil || crypto.PubkeyToAddress(*publicKey) != signerAddress {
		t.Fatalf("personal signature recovered wrong account: %v", err)
	}
	if signer.request.MessageScheme != wallet.MessageSigningEIP191Personal || signer.request.IntentHash != preview.IntentHash || repository.approval.State != evm.MessageApprovalConsumed || repository.completed.SignatureHash == (common.Hash{}) {
		t.Fatalf("personal approval was not durably bound: request=%+v approval=%+v complete=%+v", signer.request, repository.approval, repository.completed)
	}
	if repository.completed.SigningID != result.SigningID || repository.failed.SigningID != "" {
		t.Fatal("personal signing completion state is inconsistent")
	}
}

func TestPersonalSignServiceRejectsUnboundOrWeakApproval(t *testing.T) {
	prepared, err := evm.PreparePersonalSign(evm.PreparePersonalSignRequest{
		AccountID: "11111111-1111-4111-8111-111111111111", Signer: common.HexToAddress("0x1563915e194D8CfBA1943570603F7606A3115508"),
		Message: []byte("message"), Origin: "local-user",
	})
	if err != nil {
		t.Fatal(err)
	}
	service, err := evm.NewMessageSigningService(&messageRepositoryStub{}, &personalSignerStub{err: errors.New("rejected")}, evm.MessageSigningOptions{
		Now: time.Now, NewID: func() (string, error) { return "51111111-1111-4111-8111-111111111111", nil }, ApprovalTTL: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	preview := prepared.Preview()
	for _, request := range []evm.PersonalSignApprovalRequest{
		{AuthorizationEpoch: 1, ConfirmedIntentHash: preview.IntentHash, ConfirmationLevel: evm.ConfirmationStandard},
		{AuthorizationEpoch: 1, ConfirmedIntentHash: [32]byte{1}, ConfirmationLevel: evm.ConfirmationReinforced},
	} {
		if _, err := service.ApproveAndSignPersonal(context.Background(), wallet.CapabilityHandle{}, prepared, request); err == nil {
			t.Fatal("personal signing accepted weak or mismatched approval")
		}
	}
}
