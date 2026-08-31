package backup

import (
	"fmt"
	"testing"

	"blocowallet/internal/wallet"
)

type fakeEnvelopeVerifier struct {
	acceptAll bool
	rejectIDs map[string]struct{}
}

func (verifier fakeEnvelopeVerifier) VerifyEnvelope(entry AccountEntry) error {
	if _, reject := verifier.rejectIDs[entry.AccountID]; reject {
		return fmt.Errorf("envelope corrupt")
	}
	if len(entry.SecretEnvelope) > 0 && !verifier.acceptAll {
		return fmt.Errorf("envelope not verified")
	}
	return nil
}

func TestValidateRestoreAcceptsValidStaging(t *testing.T) {
	manifest := &Manifest{Accounts: []AccountEntry{
		{
			AccountID: "11111111-1111-4111-8111-111111111111", Name: "Alpha",
			Address:    "0x9d8A62f656a8d1615C1294fd71e9CFb3E4855A4F",
			SignerKind: string(wallet.SignerKindSoftware), SignerReference: "11111111-1111-4111-8111-111111111111",
			SecretType: string(wallet.SecretTypeMnemonic), SecretEnvelope: []byte("e"),
			Derivation: "bip44:m/44'/60'/0'/0/0", State: string(wallet.AccountStateActive),
			Capabilities:   uint64(wallet.CapabilitySignTransaction),
			SourceIdentity: "alpha-source", AuthorizationEpoch: 1, EnvelopeGeneration: 1, BackupGeneration: 1,
			BIP39Language: "english",
		},
		{
			AccountID: "22222222-2222-4222-8222-222222222222", Name: "Vault",
			Address:    "0xCcCCccccCCCCcCCCCCCcCcCccCcCCCcCcccccccC",
			SignerKind: string(wallet.SignerKindCloud), SignerReference: "cloud:v1:https://vault.example",
			State: string(wallet.AccountStateActive), SourceIdentity: "vault-source", AuthorizationEpoch: 1, EnvelopeGeneration: 1, BackupGeneration: 1,
		},
	}}
	accounts, err := ValidateRestore(manifest, fakeEnvelopeVerifier{acceptAll: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(accounts) != 2 || accounts[0].DerivationScheme != "bip44" || accounts[1].Capabilities != 0 {
		t.Fatalf("unexpected restore: %+v", accounts)
	}
}

func TestValidateRestoreRejectsCorruption(t *testing.T) {
	base := AccountEntry{
		AccountID: "11111111-1111-4111-8111-111111111111", Name: "Alpha",
		Address:    "0x9d8A62f656a8d1615C1294fd71e9CFb3E4855A4F",
		SignerKind: string(wallet.SignerKindSoftware), SignerReference: "11111111-1111-4111-8111-111111111111",
		SecretType: string(wallet.SecretTypeMnemonic), SecretEnvelope: []byte("e"),
		State: string(wallet.AccountStateActive),
	}
	cases := []struct {
		name   string
		mutate func(*AccountEntry)
	}{
		{"watch-only with envelope", func(entry *AccountEntry) {
			entry.SignerKind = string(wallet.SignerKindWatchOnly)
			entry.SecretType = string(wallet.SecretTypeMnemonic)
		}},
		{"cloud with capability", func(entry *AccountEntry) {
			entry.SignerKind = string(wallet.SignerKindCloud)
			entry.Capabilities = uint64(wallet.CapabilitySignTransaction)
		}},
		{"bad address", func(entry *AccountEntry) { entry.Address = "0x9d8a62f656a8d1615c1294fd71e9cfb3e4855a4f" }},
		{"missing reference", func(entry *AccountEntry) { entry.SignerReference = "" }},
		{"duplicate", func(entry *AccountEntry) { entry.AccountID = "22222222-2222-4222-8222-222222222222" }},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			entry := base
			test.mutate(&entry)
			manifest := &Manifest{Accounts: []AccountEntry{
				entry,
				{AccountID: "22222222-2222-4222-8222-222222222222", Name: "Vault", Address: "0xCcCCccccCCCCcCCCCCCcCcCccCcCCCcCcccccccC", SignerKind: string(wallet.SignerKindCloud), SignerReference: "cloud:v1:https://vault.example", State: string(wallet.AccountStateActive), SourceIdentity: "vault-source", AuthorizationEpoch: 1, EnvelopeGeneration: 1, BackupGeneration: 1},
			}}
			if _, err := ValidateRestore(manifest, fakeEnvelopeVerifier{acceptAll: true}); err == nil {
				t.Fatal("corrupt restore was accepted")
			}
		})
	}
	// A failed envelope verification blocks the whole restore.
	manifest := &Manifest{Accounts: []AccountEntry{base}}
	if _, err := ValidateRestore(manifest, fakeEnvelopeVerifier{rejectIDs: map[string]struct{}{base.AccountID: {}}}); err == nil {
		t.Fatal("corrupt envelope did not block restore")
	}
	if _, err := ValidateRestore(nil, fakeEnvelopeVerifier{}); err == nil {
		t.Fatal("nil manifest was accepted")
	}
}
