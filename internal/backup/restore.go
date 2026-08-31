package backup

import (
	"fmt"

	"blocowallet/internal/wallet"
)

// EnvelopeVerifier reopens a secret envelope during restore staging.
type EnvelopeVerifier interface {
	VerifyEnvelope(entry AccountEntry) error
}

// ValidateRestore runs the staging pass: every account must satisfy the
// wallet custody rules and every secret envelope must reopen before the
// active vault is touched.
func ValidateRestore(manifest *Manifest, verifier EnvelopeVerifier) ([]wallet.Account, error) {
	if manifest == nil {
		return nil, fmt.Errorf("backup: nil manifest")
	}
	if verifier == nil {
		return nil, fmt.Errorf("backup: envelope verifier required")
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
		}
		if entry.Derivation != "" {
			parts := splitDerivation(entry.Derivation)
			if len(parts) != 2 {
				return nil, fmt.Errorf("backup: malformed derivation for %s", entry.AccountID)
			}
			account.DerivationScheme = parts[0]
			account.DerivationPath = parts[1]
		}
		if err := account.Validate(); err != nil {
			return nil, fmt.Errorf("backup: account %s: %w", entry.AccountID, err)
		}
		if err := verifier.VerifyEnvelope(entry); err != nil {
			return nil, fmt.Errorf("backup: envelope %s: %w", entry.AccountID, err)
		}
		accounts = append(accounts, account)
	}
	return accounts, nil
}

func splitDerivation(value string) []string {
	parts := make([]string, 0, 2)
	start := 0
	for index := 0; index < len(value); index++ {
		if value[index] == ':' {
			parts = append(parts, value[start:index])
			start = index + 1
		}
	}
	parts = append(parts, value[start:])
	return parts
}
