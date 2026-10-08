package wallet

import (
	"context"
	"crypto/subtle"
	"fmt"
	"path/filepath"

	"github.com/ethereum/go-ethereum/accounts/keystore"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/google/uuid"
)

type KeystoreV3ExportRequest struct {
	Handle          CapabilityHandle
	Destination     string
	Password        []byte
	ConfirmPassword []byte
}

func (vault *WalletVault) ExportKeystoreV3(ctx context.Context, request KeystoreV3ExportRequest) error {
	if !filepath.IsAbs(request.Destination) {
		return fmt.Errorf("export destination must be absolute")
	}
	if len(request.Password) != len(request.ConfirmPassword) || subtle.ConstantTimeCompare(request.Password, request.ConfirmPassword) != 1 {
		return ErrStoragePasswordConfirmation
	}
	if err := validateNewStoragePassword(request.Password); err != nil {
		return err
	}
	credentialOp, credentialDone, err := vault.beginCredentialMutation(ctx)
	if err != nil {
		return err
	}
	defer credentialDone()
	return vault.withPrivateKey(ctx, request.Handle, func(privateKeyBytes []byte, account *Account) error {
		if account.Capabilities&CapabilityExportSecret == 0 {
			return ErrCapabilityDenied
		}
		privateKey, err := crypto.ToECDSA(privateKeyBytes)
		if err != nil {
			return err
		}
		encoded, err := keystore.EncryptKey(&keystore.Key{
			Id:         uuid.New(),
			Address:    crypto.PubkeyToAddress(privateKey.PublicKey),
			PrivateKey: privateKey,
		}, string(request.Password), keystore.StandardScryptN, keystore.StandardScryptP)
		if err != nil {
			return fmt.Errorf("encrypt Keystore V3 export: %w", err)
		}
		defer clear(encoded)
		if err := verifyKeystoreV3Export(encoded, request.Password, account.Address); err != nil {
			return err
		}
		var prepared *CredentialBackupState
		var artifact credentialArtifact
		if credentialOp != nil {
			artifact = credentialArtifact{
				Kind:       credentialKindKeystoreV3,
				Name:       filepath.Base(request.Destination),
				Path:       request.Destination,
				Ciphertext: encoded,
				Password:   request.Password,
			}
			state, err := vault.registerPreparedArtifact(ctx, credentialOp, account.AccountID, artifact)
			if err != nil {
				return err
			}
			prepared = &state
		}
		writeErr := writeExclusiveAtomic(ctx, request.Destination, encoded, 0600)
		if prepared != nil && (writeErr == nil || IsExportCommitted(writeErr)) {
			key := CredentialBackupKey{TargetID: prepared.TargetID, VaultID: prepared.VaultID, AccountID: prepared.AccountID, ItemID: prepared.ItemID}
			if err := vault.markArtifactPending(ctx, credentialOp, key); err != nil {
				return &ExportCommittedWarning{Cause: err}
			}
			if err := credentialOp.queueFileArtifact(account.AccountID, artifact); err != nil {
				return &ExportCommittedWarning{Cause: err}
			}
		}
		return writeErr
	})
}

func verifyKeystoreV3Export(encoded, password []byte, expectedAddress string) error {
	validated, err := (&KeystoreValidator{}).ValidateKeystoreV3(encoded)
	if err != nil {
		return fmt.Errorf("validate Keystore V3 export: %w", err)
	}
	if !addressesEqual(validated.Address, expectedAddress) {
		return fmt.Errorf("keystore V3 export address mismatch")
	}
	decrypted, err := decryptKeySafely(encoded, string(password))
	if err != nil {
		return fmt.Errorf("verify Keystore V3 export: %w", err)
	}
	decryptedAddress := crypto.PubkeyToAddress(decrypted.PrivateKey.PublicKey).Hex()
	if !addressesEqual(decryptedAddress, expectedAddress) {
		return fmt.Errorf("verified Keystore V3 address mismatch")
	}
	return nil
}
