package wallet

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
)

type RecoverySecretKind string

const (
	RecoveryMnemonic   RecoverySecretKind = "mnemonic"
	RecoveryPrivateKey RecoverySecretKind = "private_key"
	RecoveryPassphrase RecoverySecretKind = "bip39_passphrase"
)

var (
	ErrRecoveryConfirmation   = errors.New("recovery confirmation does not match account")
	ErrRecoveryUnavailable    = errors.New("requested recovery material is unavailable")
	ErrRecoveryAuthentication = errors.New("recovery authentication failed")
	ErrRecoverySerialization  = errors.New("recovery material cannot be serialized")
)

type RecoverySecretRequest struct {
	AccountID        string
	ConfirmAccountID string
	Password         []byte
	Kind             RecoverySecretKind
}

type RecoveryExportRequest struct {
	RecoverySecretRequest
	Destination string
}

type RecoveryMaterial struct {
	AccountID          string
	Address            string
	Kind               RecoverySecretKind
	DerivationPath     string
	BIP39Language      BIP39Language
	HasBIP39Passphrase bool
	data               []byte
}

func (material *RecoveryMaterial) Bytes() []byte {
	if material == nil {
		return nil
	}
	return material.data
}

func (material *RecoveryMaterial) Destroy() {
	if material == nil {
		return
	}
	clear(material.data)
	material.data = nil
}

func (RecoveryMaterial) String() string {
	return "[redacted recovery material]"
}

func (RecoveryMaterial) GoString() string {
	return "[redacted recovery material]"
}

func (RecoveryMaterial) MarshalJSON() ([]byte, error) {
	return nil, ErrRecoverySerialization
}

func (vault *WalletVault) RevealRecoverySecret(ctx context.Context, request RecoverySecretRequest) (*RecoveryMaterial, error) {
	var result *RecoveryMaterial
	err := vault.withRecoveryMaterial(ctx, request, func(material *RecoveryMaterial) error {
		result = &RecoveryMaterial{
			AccountID:          material.AccountID,
			Address:            material.Address,
			Kind:               material.Kind,
			DerivationPath:     material.DerivationPath,
			BIP39Language:      material.BIP39Language,
			HasBIP39Passphrase: material.HasBIP39Passphrase,
			data:               append([]byte(nil), material.data...),
		}
		return nil
	})
	if err != nil {
		if result != nil {
			result.Destroy()
		}
		return nil, err
	}
	return result, nil
}

func (vault *WalletVault) ExportRecoverySecret(ctx context.Context, request RecoveryExportRequest) error {
	if !filepath.IsAbs(request.Destination) {
		return fmt.Errorf("recovery export destination must be absolute")
	}
	return vault.withRecoveryMaterial(ctx, request.RecoverySecretRequest, func(material *RecoveryMaterial) error {
		extra := 0
		if material.Kind == RecoveryMnemonic || material.Kind == RecoveryPrivateKey {
			extra = 1
		}
		payload := make([]byte, len(material.data)+extra)
		copy(payload, material.data)
		if extra == 1 {
			payload[len(payload)-1] = '\n'
		}
		defer clear(payload)
		return writeExclusiveAtomic(ctx, request.Destination, payload, 0600)
	})
}

func (vault *WalletVault) withRecoveryMaterial(ctx context.Context, request RecoverySecretRequest, operation func(*RecoveryMaterial) error) error {
	vault.lifecycle.Lock()
	defer vault.lifecycle.Unlock()
	if vault.closed {
		return ErrVaultClosed
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if request.AccountID == "" || request.AccountID != request.ConfirmAccountID {
		return ErrRecoveryConfirmation
	}
	switch request.Kind {
	case RecoveryMnemonic, RecoveryPrivateKey, RecoveryPassphrase:
	default:
		return ErrRecoveryUnavailable
	}
	if len(request.Password) == 0 {
		return ErrRecoveryAuthentication
	}
	account, err := vault.repository.GetAccount(ctx, request.AccountID)
	if err != nil {
		return err
	}
	if err := account.Validate(); err != nil {
		return ErrRecoveryUnavailable
	}
	if account.SignerKind != SignerKindSoftware {
		return ErrRecoveryUnavailable
	}
	if account.Capabilities&CapabilityExportSecret == 0 {
		return ErrCapabilityDenied
	}
	if account.State != AccountStateActive && account.State != AccountStateLocked {
		return ErrRecoveryUnavailable
	}
	plaintext, err := vault.codec.Open(request.Password, metadataForAccount(account), account.SecretEnvelope)
	if err != nil {
		return ErrRecoveryAuthentication
	}
	defer clear(plaintext)
	privateKey, address, err := deriveStoredSecretIdentity(account, plaintext)
	if err != nil {
		return ErrRecoveryAuthentication
	}
	defer clear(privateKey)
	if !addressesEqual(address, account.Address) {
		return ErrRecoveryAuthentication
	}
	secret, err := canonicalSecretFromStored(account, plaintext)
	if err != nil {
		return ErrRecoveryAuthentication
	}
	defer clear(secret.PrivateKey)
	var data []byte
	switch request.Kind {
	case RecoveryMnemonic:
		if secret.Kind != SecretTypeMnemonic {
			return ErrRecoveryUnavailable
		}
		data = []byte(normalizedMnemonic(secret.Mnemonic))
	case RecoveryPrivateKey:
		data = make([]byte, 66)
		copy(data, "0x")
		hex.Encode(data[2:], privateKey)
	case RecoveryPassphrase:
		if secret.Kind != SecretTypeMnemonic || !account.HasBIP39Passphrase || secret.BIP39Passphrase == "" {
			return ErrRecoveryUnavailable
		}
		data = []byte(secret.BIP39Passphrase)
	}
	if err := ctx.Err(); err != nil {
		clear(data)
		return err
	}
	latest, err := vault.repository.GetAccount(ctx, request.AccountID)
	if err != nil {
		clear(data)
		return err
	}
	if latest.AccountID != account.AccountID || latest.Revision != account.Revision || latest.AuthorizationEpoch != account.AuthorizationEpoch || latest.Capabilities != account.Capabilities || latest.State != account.State || latest.SignerKind != account.SignerKind || !addressesEqual(latest.Address, account.Address) {
		clear(data)
		return ErrCapabilityExpired
	}
	if err := ctx.Err(); err != nil {
		clear(data)
		return err
	}
	material := &RecoveryMaterial{
		AccountID:          account.AccountID,
		Address:            account.Address,
		Kind:               request.Kind,
		DerivationPath:     account.DerivationPath,
		BIP39Language:      BIP39Language(account.BIP39Language),
		HasBIP39Passphrase: account.HasBIP39Passphrase,
		data:               data,
	}
	defer material.Destroy()
	return operation(material)
}
