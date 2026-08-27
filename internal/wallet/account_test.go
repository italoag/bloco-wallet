package wallet

import "testing"

func validAccountForValidation() *Account {
	return &Account{
		AccountID:          "018f76c1-04e7-4d55-8db4-f57c7ff9e3b2",
		Name:               "Account",
		Address:            "0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266",
		SignerKind:         SignerKindSoftware,
		SignerReference:    "software:018f76c1-04e7-4d55-8db4-f57c7ff9e3b2",
		SecretType:         SecretTypeMnemonic,
		Capabilities:       CapabilitySignMessage,
		State:              AccountStateActive,
		SecretEnvelope:     []byte("encrypted"),
		EnvelopeGeneration: 1,
		AuthorizationEpoch: 1,
		BackupGeneration:   1,
		SourceIdentity:     "generated:018f76c1-04e7-4d55-8db4-f57c7ff9e3b2",
		Revision:           1,
	}
}

func TestAccountValidation(t *testing.T) {
	if (*Account)(nil).Validate() == nil {
		t.Fatal("nil account was accepted")
	}
	valid := validAccountForValidation()
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	mutations := []func(*Account){
		func(account *Account) { account.AccountID = "invalid" },
		func(account *Account) { account.Name = "" },
		func(account *Account) { account.Address = "0xf39fd6e51aad88f6f4ce6ab8827279cfffb92266" },
		func(account *Account) { account.SignerReference = "" },
		func(account *Account) { account.State = "unknown" },
		func(account *Account) { account.SecretEnvelope = nil },
		func(account *Account) { account.SignerKind = "unknown" },
	}
	for _, mutate := range mutations {
		account := *valid
		account.SecretEnvelope = append([]byte(nil), valid.SecretEnvelope...)
		mutate(&account)
		if err := account.Validate(); err == nil {
			t.Fatal("invalid account was accepted")
		}
	}
	for _, signerKind := range []SignerKind{SignerKindWatchOnly, SignerKindHardware, SignerKindCloud, SignerKindMultisig} {
		account := *valid
		account.SignerKind = signerKind
		account.SecretEnvelope = nil
		account.SecretType = ""
		if signerKind == SignerKindWatchOnly {
			account.Capabilities = 0
		}
		if err := account.Validate(); err != nil {
			t.Fatalf("external signer %s was rejected: %v", signerKind, err)
		}
	}
}
