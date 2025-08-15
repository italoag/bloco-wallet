package wallet

import (
	"strings"
)

// ValidateUniqueSourceHash checks if the provided source hash is unique in the repository.
// Returns:
//   - InvalidImportDataError if the hash is empty/invalid
//   - DuplicateWalletError if a wallet with the same source hash already exists
//   - underlying repository error, if any
func ValidateUniqueSourceHash(repo WalletRepository, sourceHash string, importType ImportMethod) error {
	if strings.TrimSpace(sourceHash) == "" {
		return NewInvalidImportDataError(string(importType), "empty source hash")
	}

	existing, err := repo.FindBySourceHash(sourceHash)
	if err != nil {
		return err
	}
	if existing != nil {
		return NewDuplicateWalletError(string(importType), existing.Address, "A wallet with this source already exists")
	}
	return nil
}
