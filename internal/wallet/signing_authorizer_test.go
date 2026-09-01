package wallet

import (
	"context"
	"testing"
)

type signingAuthorizerAccountLookup struct {
	account *Account
	err     error
}

func (lookup signingAuthorizerAccountLookup) GetAccount(context.Context, string) (*Account, error) {
	return lookup.account, lookup.err
}

func TestSigningAuthorizerExternalEOA(t *testing.T) {
	account := &Account{
		AccountID:  "11111111-1111-4111-8111-111111111111",
		SignerKind: SignerKindHardware, State: AccountStateActive,
		Capabilities: CapabilitySignTransaction | CapabilitySignMessage, AuthorizationEpoch: 7,
	}
	authorizer, err := NewSigningAuthorizer(&TransactionAuthorizer{}, signingAuthorizerAccountLookup{account: account})
	if err != nil {
		t.Fatal(err)
	}
	if !authorizer.HasActiveSession(context.Background(), account.AccountID) {
		t.Fatal("active hardware signer still required a storage password")
	}
	called := false
	err = authorizer.Authorize(context.Background(), account.AccountID, nil, func(handle CapabilityHandle, epoch uint64) error {
		called = true
		if handle != (CapabilityHandle{}) || epoch != account.AuthorizationEpoch {
			t.Fatalf("unexpected external authorization: %+v %d", handle, epoch)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("external signing operation was not authorized")
	}
	if err := authorizer.Authorize(context.Background(), account.AccountID, []byte("password"), func(CapabilityHandle, uint64) error { return nil }); err == nil {
		t.Fatal("external signer accepted a storage password")
	}
	account.State = AccountStateUnavailable
	if authorizer.HasActiveSession(context.Background(), account.AccountID) {
		t.Fatal("unavailable hardware signer reported an active session")
	}
}

func TestSigningAuthorizerRejectsNonSigningKind(t *testing.T) {
	account := &Account{
		AccountID:  "11111111-1111-4111-8111-111111111111",
		SignerKind: SignerKindWatchOnly, State: AccountStateActive,
		AuthorizationEpoch: 1,
	}
	authorizer, err := NewSigningAuthorizer(&TransactionAuthorizer{}, signingAuthorizerAccountLookup{account: account})
	if err != nil {
		t.Fatal(err)
	}
	if authorizer.HasActiveSession(context.Background(), account.AccountID) {
		t.Fatal("watch-only account reported a signing session")
	}
	if err := authorizer.Authorize(context.Background(), account.AccountID, nil, func(CapabilityHandle, uint64) error { return nil }); err == nil {
		t.Fatal("watch-only account was authorized")
	}
}
