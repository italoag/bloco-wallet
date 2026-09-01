package evm_test

import (
	"context"
	"testing"
	"time"

	"blocowallet/internal/evm"
	"blocowallet/internal/wallet"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

type eip712ServiceStub struct {
	approval  evm.MessageApproval
	completed bool
}

func (stub *eip712ServiceStub) IssueMessageApproval(_ context.Context, approval evm.MessageApproval) error {
	stub.approval = approval
	return nil
}

func (stub *eip712ServiceStub) AuthorizeMessageSigning(_ context.Context, request evm.AuthorizeMessageSigningRequest) (evm.MessageSigningRecord, error) {
	stub.approval.State = evm.MessageApprovalConsumed
	return evm.MessageSigningRecord{
		SigningID: request.SigningID, ApprovalID: request.ApprovalID, AccountID: request.AccountID,
		Signer: request.Signer, Scheme: request.Scheme, ChainID: request.ChainID,
		Digest: request.Digest, IntentHash: request.IntentHash, State: evm.MessageSigningInProgress,
		CreatedAt: request.AuthorizedAt,
	}, nil
}

func (stub *eip712ServiceStub) CompleteMessageSigning(_ context.Context, request evm.CompleteMessageSigningRequest) error {
	stub.completed = true
	return nil
}

func (stub *eip712ServiceStub) FailMessageSigning(context.Context, evm.FailMessageSigningRequest) error {
	return nil
}

type eip712SignerStub struct{}

func (eip712SignerStub) Sign(_ context.Context, _ wallet.CapabilityHandle, request wallet.SoftwareSigningRequest) (wallet.SoftwareSigningResult, error) {
	key, err := crypto.HexToECDSA("4646464646464646464646464646464646464646464646464646464646464646")
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

func TestEIP712SigningBindsChainAndRejectsPersonalSchemeRequests(t *testing.T) {
	prepared, err := evm.PrepareEIP712Sign(evm.PrepareEIP712SignRequest{
		AccountID: "11111111-1111-4111-8111-111111111111",
		Signer:    common.HexToAddress("0x9d8A62f656a8d1615C1294fd71e9CFb3E4855A4F"),
		ChainID:   1, TypedData: []byte(eip712MailFixture), Origin: "local-user",
	})
	if err != nil {
		t.Fatal(err)
	}
	repository := &eip712ServiceStub{}
	ids := []string{"51111111-1111-4111-8111-111111111111", "71111111-1111-4111-8111-111111111111"}
	index := 0
	now := time.Now().UTC()
	service, err := evm.NewMessageSigningService(repository, eip712SignerStub{}, evm.MessageSigningOptions{
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
	result, err := service.ApproveAndSignEIP712(context.Background(), wallet.CapabilityHandle{}, prepared, evm.PersonalSignApprovalRequest{
		AuthorizationEpoch: 1, ConfirmedIntentHash: preview.IntentHash, ConfirmationLevel: evm.ConfirmationReinforced,
	})
	if err != nil {
		t.Fatal(err)
	}
	if repository.approval.Scheme != wallet.MessageSigningEIP712 || repository.approval.ChainID != 1 || repository.approval.PayloadSize != uint64(len(preview.CanonicalJSON)) || !repository.completed {
		t.Fatalf("EIP-712 approval was not chain-bound: %+v", repository.approval)
	}
	recoverable := append([]byte(nil), result.Signature...)
	recoverable[64] -= 27
	publicKey, err := crypto.SigToPub(result.Digest[:], recoverable)
	if err != nil {
		t.Fatal(err)
	}
	if crypto.PubkeyToAddress(*publicKey).Hex() != "0x9d8A62f656a8d1615C1294fd71e9CFb3E4855A4F" {
		t.Fatal("EIP-712 signature recovered the wrong account")
	}
	if _, err := service.ApproveAndSignEIP712(context.Background(), wallet.CapabilityHandle{}, prepared, evm.PersonalSignApprovalRequest{
		AuthorizationEpoch: 1, ConfirmedIntentHash: [32]byte{1}, ConfirmationLevel: evm.ConfirmationReinforced,
	}); err == nil {
		t.Fatal("EIP-712 signing accepted a mismatched intent hash")
	}
	personal := evm.PreparePersonalSignRequest{AccountID: "11111111-1111-4111-8111-111111111111", Signer: prepared.Preview().Signer, Message: []byte("x"), Origin: "local-user"}
	preparedPersonal, err := evm.PreparePersonalSign(personal)
	if err != nil {
		t.Fatal(err)
	}
	preparedPersonalPreview := preparedPersonal.Preview()
	if _, err := service.ApproveAndSignEIP712(context.Background(), wallet.CapabilityHandle{}, nil, evm.PersonalSignApprovalRequest{
		AuthorizationEpoch: 1, ConfirmedIntentHash: preparedPersonalPreview.IntentHash, ConfirmationLevel: evm.ConfirmationReinforced,
	}); err == nil {
		t.Fatal("EIP-712 signing accepted a personal-sign prepared request")
	}
}
