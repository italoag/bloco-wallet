package wallet

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const encryptedAccountExportVersion = 1

type EncryptedAccountExportRequest struct {
	Handle             CapabilityHandle
	Destination        string
	CurrentPassword    []byte
	NewPassword        []byte
	ConfirmNewPassword []byte
}

type EncryptedAccountExportV1 struct {
	Version            uint32            `json:"version"`
	AccountID          string            `json:"account_id"`
	Name               string            `json:"name"`
	Address            string            `json:"address"`
	SignerKind         SignerKind        `json:"signer_kind"`
	SignerReference    string            `json:"signer_reference"`
	SecretType         SecretType        `json:"secret_type"`
	DerivationScheme   string            `json:"derivation_scheme"`
	DerivationPath     string            `json:"derivation_path"`
	AccountIndex       uint32            `json:"account_index"`
	ChangeIndex        uint32            `json:"change_index"`
	AddressIndex       uint32            `json:"address_index"`
	BIP39Language      string            `json:"bip39_language"`
	Capabilities       AccountCapability `json:"capabilities"`
	EnvelopeGeneration uint64            `json:"envelope_generation"`
	AuthorizationEpoch uint64            `json:"authorization_epoch"`
	SecretEnvelope     []byte            `json:"secret_envelope"`
	ExportedAt         time.Time         `json:"exported_at"`
}

func (export EncryptedAccountExportV1) Metadata() EnvelopeMetadata {
	return EnvelopeMetadata{
		AccountID:          export.AccountID,
		SecretType:         export.SecretType,
		Address:            export.Address,
		EnvelopeGeneration: export.EnvelopeGeneration,
		Derivation: DerivationMetadata{
			Scheme:       export.DerivationScheme,
			Path:         export.DerivationPath,
			AccountIndex: export.AccountIndex,
			ChangeIndex:  export.ChangeIndex,
			AddressIndex: export.AddressIndex,
			Language:     export.BIP39Language,
		},
	}
}

func (vault *WalletVault) ExportEncryptedAccount(ctx context.Context, request EncryptedAccountExportRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !filepath.IsAbs(request.Destination) {
		return fmt.Errorf("export destination must be absolute")
	}
	if len(request.NewPassword) != len(request.ConfirmNewPassword) || subtle.ConstantTimeCompare(request.NewPassword, request.ConfirmNewPassword) != 1 {
		return fmt.Errorf("new export password confirmation does not match")
	}
	return vault.withPrivateKey(ctx, request.Handle, func(_ []byte, account *Account) error {
		if account.Capabilities&CapabilityExportSecret == 0 {
			return ErrCapabilityDenied
		}
		if account.SignerKind != SignerKindSoftware || len(account.SecretEnvelope) == 0 {
			return fmt.Errorf("account has no exportable encrypted secret")
		}
		plaintext, err := vault.codec.Open(request.CurrentPassword, metadataForAccount(account), account.SecretEnvelope)
		if err != nil {
			return fmt.Errorf("authenticate export source: %w", err)
		}
		defer clear(plaintext)
		exportMetadata := metadataForAccount(account)
		exportMetadata.EnvelopeGeneration++
		exportedEnvelope, err := vault.codec.Seal(request.NewPassword, exportMetadata, plaintext)
		if err != nil {
			return err
		}
		reopened, err := vault.codec.Open(request.NewPassword, exportMetadata, exportedEnvelope)
		if err != nil {
			return fmt.Errorf("verify exported envelope: %w", err)
		}
		if !bytes.Equal(reopened, plaintext) {
			clear(reopened)
			return fmt.Errorf("exported envelope mismatch")
		}
		clear(reopened)
		export := EncryptedAccountExportV1{
			Version:            encryptedAccountExportVersion,
			AccountID:          account.AccountID,
			Name:               account.Name,
			Address:            account.Address,
			SignerKind:         account.SignerKind,
			SignerReference:    account.SignerReference,
			SecretType:         account.SecretType,
			DerivationScheme:   account.DerivationScheme,
			DerivationPath:     account.DerivationPath,
			AccountIndex:       account.AccountIndex,
			ChangeIndex:        account.ChangeIndex,
			AddressIndex:       account.AddressIndex,
			BIP39Language:      account.BIP39Language,
			Capabilities:       account.Capabilities,
			EnvelopeGeneration: exportMetadata.EnvelopeGeneration,
			AuthorizationEpoch: account.AuthorizationEpoch,
			SecretEnvelope:     exportedEnvelope,
			ExportedAt:         vault.options.Now().UTC(),
		}
		encoded, err := json.Marshal(export)
		if err != nil {
			return fmt.Errorf("encode encrypted account export: %w", err)
		}
		return writeExclusiveAtomic(ctx, request.Destination, encoded, 0600)
	})
}

type atomicExportFile interface {
	Name() string
	Chmod(os.FileMode) error
	Write([]byte) (int, error)
	Sync() error
	Close() error
}

type atomicExportDirectory interface {
	Sync() error
	Close() error
}

type atomicExportOperations struct {
	lstat      func(string) (os.FileInfo, error)
	createTemp func(string, string) (atomicExportFile, error)
	link       func(string, string) error
	remove     func(string) error
	openDir    func(string) (atomicExportDirectory, error)
}

var defaultAtomicExportOperations = atomicExportOperations{
	lstat: os.Lstat,
	createTemp: func(directory, pattern string) (atomicExportFile, error) {
		return os.CreateTemp(directory, pattern)
	},
	link:   os.Link,
	remove: os.Remove,
	openDir: func(path string) (atomicExportDirectory, error) {
		return os.Open(path)
	},
}

func writeExclusiveAtomic(ctx context.Context, destination string, data []byte, mode os.FileMode) error {
	return writeExclusiveAtomicWithOperations(ctx, destination, data, mode, defaultAtomicExportOperations)
}

func writeExclusiveAtomicWithOperations(ctx context.Context, destination string, data []byte, mode os.FileMode, operations atomicExportOperations) (err error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	parent := filepath.Dir(destination)
	parentInfo, err := operations.lstat(parent)
	if err != nil {
		return err
	}
	if parentInfo.Mode()&os.ModeSymlink != 0 || !parentInfo.IsDir() {
		return fmt.Errorf("export parent must be a regular directory")
	}
	if _, err := operations.lstat(destination); err == nil {
		return fmt.Errorf("%w: export destination exists", os.ErrExist)
	} else if !os.IsNotExist(err) {
		return err
	}
	temporary, err := operations.createTemp(parent, ".bloco-export-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	keepTemporary := true
	defer func() {
		if keepTemporary {
			_ = operations.remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(mode); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := operations.link(temporaryPath, destination); err != nil {
		return err
	}
	if err := operations.remove(temporaryPath); err != nil {
		return err
	}
	keepTemporary = false
	directory, err := operations.openDir(parent)
	if err != nil {
		return err
	}
	if err := directory.Sync(); err != nil {
		_ = directory.Close()
		return err
	}
	if err := directory.Close(); err != nil {
		return err
	}
	return nil
}
