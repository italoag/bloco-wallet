package signer_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"testing"

	"blocowallet/internal/signer"
	"blocowallet/internal/wallet"

	"github.com/ethereum/go-ethereum/crypto"
)

type stubApprovedSigner struct {
	calls int
	err   error
}

func (stub *stubApprovedSigner) Sign(ctx context.Context, handle wallet.CapabilityHandle, request wallet.SoftwareSigningRequest) (wallet.SoftwareSigningResult, error) {
	stub.calls++
	if stub.err != nil {
		return wallet.SoftwareSigningResult{}, stub.err
	}
	return wallet.SoftwareSigningResult{AccountID: request.AccountID, Signature: []byte{1}}, nil
}

type mapAccountLookup struct {
	accounts map[string]*wallet.Account
}

func (lookup mapAccountLookup) GetAccount(_ context.Context, accountID string) (*wallet.Account, error) {
	account, exists := lookup.accounts[accountID]
	if !exists {
		return nil, &lookupError{"not found"}
	}
	return account, nil
}

type lookupError struct{ message string }

func (err *lookupError) Error() string { return err.message }

func TestSignerDispatcherRoutesByAccountKind(t *testing.T) {
	privateKey, err := ecdsa.GenerateKey(crypto.S256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	software := &stubApprovedSigner{}
	cloud := &stubApprovedSigner{}
	lookup := mapAccountLookup{accounts: map[string]*wallet.Account{
		"11111111-1111-4111-8111-111111111111": {
			AccountID:  "11111111-1111-4111-8111-111111111111",
			Address:    crypto.PubkeyToAddress(privateKey.PublicKey).Hex(),
			SignerKind: wallet.SignerKindSoftware, State: wallet.AccountStateActive,
		},
		"22222222-2222-4222-8222-222222222222": {
			AccountID:  "22222222-2222-4222-8222-222222222222",
			Address:    crypto.PubkeyToAddress(privateKey.PublicKey).Hex(),
			SignerKind: wallet.SignerKindCloud, State: wallet.AccountStateActive,
		},
		"33333333-3333-4333-8333-333333333333": {
			AccountID:  "33333333-3333-4333-8333-333333333333",
			Address:    crypto.PubkeyToAddress(privateKey.PublicKey).Hex(),
			SignerKind: wallet.SignerKindWatchOnly, State: wallet.AccountStateActive,
		},
	}}
	dispatcher, err := signer.NewSignerDispatcher(software, cloud, lookup)
	if err != nil {
		t.Fatal(err)
	}
	request := wallet.SoftwareSigningRequest{
		AccountID: "11111111-1111-4111-8111-111111111111",
		Purpose:   wallet.SigningPurposeTransaction, ChainID: 1,
	}
	if _, err := dispatcher.Sign(context.Background(), wallet.CapabilityHandle{}, request); err != nil {
		t.Fatal(err)
	}
	request.AccountID = "22222222-2222-4222-8222-222222222222"
	if _, err := dispatcher.Sign(context.Background(), wallet.CapabilityHandle{}, request); err != nil {
		t.Fatal(err)
	}
	if software.calls != 1 || cloud.calls != 1 {
		t.Fatalf("dispatch counts: software=%d cloud=%d", software.calls, cloud.calls)
	}
	request.AccountID = "33333333-3333-4333-8333-333333333333"
	if _, err := dispatcher.Sign(context.Background(), wallet.CapabilityHandle{}, request); err == nil {
		t.Fatal("watch-only account was routed to a signer")
	}
	// Without a cloud adapter, cloud accounts fail closed.
	dispatcherNoCloud, err := signer.NewSignerDispatcher(software, nil, lookup)
	if err != nil {
		t.Fatal(err)
	}
	request.AccountID = "22222222-2222-4222-8222-222222222222"
	if _, err := dispatcherNoCloud.Sign(context.Background(), wallet.CapabilityHandle{}, request); err == nil {
		t.Fatal("cloud account signed without a cloud adapter")
	}
	// Hardware accounts route to the hardware adapter when present and fail
	// closed otherwise.
	hardwareStub := &stubApprovedSigner{}
	hardwareLookup := mapAccountLookup{accounts: map[string]*wallet.Account{
		"44444444-4444-4444-8444-444444444444": {
			AccountID:  "44444444-4444-4444-8444-444444444444",
			Address:    crypto.PubkeyToAddress(privateKey.PublicKey).Hex(),
			SignerKind: wallet.SignerKindHardware, State: wallet.AccountStateActive,
		},
	}}
	dispatcherWithHardware, err := signer.NewSignerDispatcherWithHardware(software, cloud, hardwareStub, hardwareLookup)
	if err != nil {
		t.Fatal(err)
	}
	request.AccountID = "44444444-4444-4444-8444-444444444444"
	if _, err := dispatcherWithHardware.Sign(context.Background(), wallet.CapabilityHandle{}, request); err != nil {
		t.Fatal(err)
	}
	if hardwareStub.calls != 1 {
		t.Fatalf("hardware dispatch count: %d", hardwareStub.calls)
	}
	dispatcherWithoutHardware, err := signer.NewSignerDispatcher(software, cloud, hardwareLookup)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := dispatcherWithoutHardware.Sign(context.Background(), wallet.CapabilityHandle{}, request); err == nil {
		t.Fatal("hardware account signed without a hardware adapter")
	}
}
