package backup

import (
	"fmt"

	"blocowallet/internal/wallet"
)

// EnvelopeVerifier reopens a secret envelope during restore staging.
type EnvelopeVerifier interface {
	// VerifyEnvelope reopens the envelope and reports corruption.
	VerifyEnvelope(entry AccountEntry) error
	// VerifyDerivedAddress re-derives the account address from the secret
	// and rejects mismatches.
	VerifyDerivedAddress(entry AccountEntry) error
}

// ValidateRestore runs the staging pass: every account must satisfy the
// wallet custody rules, every secret envelope must reopen, and the derived
// address must match before the active vault is touched.
func ValidateRestore(manifest *Manifest, verifier EnvelopeVerifier) ([]wallet.Account, error) {
	return ValidateRestoreWithSchema(manifest, 0, verifier)
}

// ValidateRestoreWithSchema additionally requires the archive schema to
// match the application schema exactly.
func ValidateRestoreWithSchema(manifest *Manifest, expectedSchema int, verifier EnvelopeVerifier) ([]wallet.Account, error) {
	if manifest == nil {
		return nil, fmt.Errorf("backup: nil manifest")
	}
	if verifier == nil {
		return nil, fmt.Errorf("backup: envelope verifier required")
	}
	if expectedSchema > 0 && manifest.Schema != expectedSchema {
		return nil, fmt.Errorf("backup: schema mismatch (archive %d, application %d)", manifest.Schema, expectedSchema)
	}
	accounts := make([]wallet.Account, 0, len(manifest.Accounts))
	seen := make(map[string]struct{}, len(manifest.Accounts))
	for _, entry := range manifest.Accounts {
		if _, duplicate := seen[entry.AccountID]; duplicate {
			return nil, fmt.Errorf("backup: duplicate account %s", entry.AccountID)
		}
		seen[entry.AccountID] = struct{}{}
		account := wallet.Account{
			AccountID: entry.AccountID, Name: entry.Name, Address: entry.Address,
			SignerKind: wallet.SignerKind(entry.SignerKind), SignerReference: entry.SignerReference,
			State: wallet.AccountState(entry.State), Capabilities: wallet.AccountCapability(entry.Capabilities),
			SecretType:         wallet.SecretType(entry.SecretType),
			SecretEnvelope:     append([]byte(nil), entry.SecretEnvelope...),
			SourceIdentity:     entry.SourceIdentity,
			AuthorizationEpoch: entry.AuthorizationEpoch,
			EnvelopeGeneration: entry.EnvelopeGeneration,
			BackupGeneration:   entry.BackupGeneration,
			BIP39Language:      entry.BIP39Language,
			DerivationScheme:   entry.DerivationScheme,
			DerivationPath:     entry.DerivationPath,
			AccountIndex:       entry.AccountIndex,
			ChangeIndex:        entry.ChangeIndex,
			AddressIndex:       entry.AddressIndex,
			HasBIP39Passphrase: entry.HasBIP39Passphrase,
		}
		if err := account.Validate(); err != nil {
			return nil, fmt.Errorf("backup: account %s: %w", entry.AccountID, err)
		}
		if err := verifier.VerifyEnvelope(entry); err != nil {
			return nil, fmt.Errorf("backup: envelope %s: %w", entry.AccountID, err)
		}
		if err := verifier.VerifyDerivedAddress(entry); err != nil {
			return nil, fmt.Errorf("backup: address %s: %w", entry.AccountID, err)
		}
		accounts = append(accounts, account)
	}
	return accounts, nil
}
