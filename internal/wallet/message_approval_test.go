package wallet

import (
	"context"
	"errors"
	"testing"

	"github.com/ethereum/go-ethereum/crypto"
)

type approvalVerifierStub struct {
	messageBinding MessageApprovalBinding
	messageErr     error
}

func (verifier *approvalVerifierStub) VerifyTransactionApproval(context.Context, TransactionApprovalBinding) error {
	return nil
}

func (verifier *approvalVerifierStub) VerifyMessageApproval(_ context.Context, binding MessageApprovalBinding) error {
	verifier.messageBinding = binding
	return verifier.messageErr
}

func TestSoftwareSignerRequiresBoundMessageApproval(t *testing.T) {
	vault, _, _ := newTestVault(t)
	password := []byte("Strong message password 1!")
	account := activateTestAccount(t, vault, password)
	handle, err := vault.Unlock(context.Background(), account.AccountID, password)
	if err != nil {
		t.Fatal(err)
	}
	digest := crypto.Keccak256([]byte("approved message"))
	request := messageSigningRequest(account.AccountID, digest)
	unverified, err := NewSoftwareSigner(vault)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := unverified.Sign(context.Background(), handle, request); err == nil {
		t.Fatal("message signer accepted an approval without a verifier")
	}
	verifier := &approvalVerifierStub{}
	signer, err := NewSoftwareSignerWithApprovalVerifier(vault, verifier)
	if err != nil {
		t.Fatal(err)
	}
	result, err := signer.Sign(context.Background(), handle, request)
	if err != nil {
		t.Fatal(err)
	}
	if result.MessageScheme != MessageSigningEIP191Personal || result.IntentHash != request.IntentHash || verifier.messageBinding.IntentHash != request.IntentHash || verifier.messageBinding.Digest != request.Digest {
		t.Fatalf("message approval binding was not preserved: result=%+v binding=%+v", result, verifier.messageBinding)
	}
	verifier.messageErr = errors.New("approval consumed")
	if _, err := signer.Sign(context.Background(), handle, request); err == nil {
		t.Fatal("message signer ignored verifier rejection")
	}
}

func TestSoftwareSignerRejectsMalformedMessageApprovalBinding(t *testing.T) {
	vault, _, _ := newTestVault(t)
	password := []byte("Strong message password 1!")
	account := activateTestAccount(t, vault, password)
	handle, err := vault.Unlock(context.Background(), account.AccountID, password)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := NewSoftwareSignerWithApprovalVerifier(vault, &approvalVerifierStub{})
	if err != nil {
		t.Fatal(err)
	}
	request := messageSigningRequest(account.AccountID, crypto.Keccak256([]byte("message")))
	mutations := []func(*SoftwareSigningRequest){
		func(value *SoftwareSigningRequest) { value.MessageScheme = "" },
		func(value *SoftwareSigningRequest) { value.IntentHash = [32]byte{} },
		func(value *SoftwareSigningRequest) { value.ChainID = 1 },
	}
	for _, mutate := range mutations {
		invalid := request
		mutate(&invalid)
		if _, err := signer.Sign(context.Background(), handle, invalid); err == nil {
			t.Fatal("malformed message approval binding was signed")
		}
	}
}
