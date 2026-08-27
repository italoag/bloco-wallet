package wallet

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
)

type EncryptedAccountImportRequest struct {
	Name                   string
	ExportJSON             []byte
	ExportPassword         []byte
	StoragePassword        []byte
	ConfirmStoragePassword []byte
}

func (vault *WalletVault) PreviewEncryptedAccountImport(ctx context.Context, data, exportPassword []byte) (ImportPreview, error) {
	if err := vault.beginOperation(); err != nil {
		return ImportPreview{}, err
	}
	defer vault.endOperation()
	exported, secret, err := vault.openEncryptedAccountExport(ctx, data, exportPassword)
	if err != nil {
		return ImportPreview{}, err
	}
	defer clear(secret.PrivateKey)
	privateKey, address, err := deriveCanonicalSecretIdentity(secret)
	if err != nil {
		return ImportPreview{}, err
	}
	clear(privateKey)
	if !addressesEqual(address, exported.Address) {
		return ImportPreview{}, fmt.Errorf("encrypted account export identity mismatch")
	}
	return previewForCanonicalSecret(secret, address, "bloco_encrypted_v1"), nil
}

func (vault *WalletVault) ImportEncryptedAccount(ctx context.Context, request EncryptedAccountImportRequest) (AccountSummary, error) {
	if err := vault.beginOperation(); err != nil {
		return AccountSummary{}, err
	}
	_, secret, err := vault.openEncryptedAccountExport(ctx, request.ExportJSON, request.ExportPassword)
	vault.endOperation()
	if err != nil {
		return AccountSummary{}, err
	}
	defer clear(secret.PrivateKey)
	return vault.importCanonicalSecret(ctx, request.Name, secret, request.StoragePassword, request.ConfirmStoragePassword)
}

func (vault *WalletVault) openEncryptedAccountExport(ctx context.Context, data, exportPassword []byte) (EncryptedAccountExportV1, canonicalSecretV1, error) {
	if err := ctx.Err(); err != nil {
		return EncryptedAccountExportV1{}, canonicalSecretV1{}, err
	}
	if len(data) == 0 || len(data) > maxKeystoreImportSize {
		return EncryptedAccountExportV1{}, canonicalSecretV1{}, fmt.Errorf("encrypted account export size is outside policy")
	}
	var exported EncryptedAccountExportV1
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&exported); err != nil {
		return EncryptedAccountExportV1{}, canonicalSecretV1{}, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			err = fmt.Errorf("encrypted account export contains trailing data")
		}
		return EncryptedAccountExportV1{}, canonicalSecretV1{}, err
	}
	if exported.Version != encryptedAccountExportVersion || exported.SignerKind != SignerKindSoftware || len(exported.SecretEnvelope) == 0 {
		return EncryptedAccountExportV1{}, canonicalSecretV1{}, fmt.Errorf("unsupported encrypted account export")
	}
	plaintext, err := vault.codec.Open(exportPassword, exported.Metadata(), exported.SecretEnvelope)
	if err != nil {
		return EncryptedAccountExportV1{}, canonicalSecretV1{}, err
	}
	defer clear(plaintext)
	secret, err := decodeCanonicalSecret(plaintext)
	if err != nil {
		return EncryptedAccountExportV1{}, canonicalSecretV1{}, err
	}
	if secret.Kind != exported.SecretType {
		clear(secret.PrivateKey)
		return EncryptedAccountExportV1{}, canonicalSecretV1{}, fmt.Errorf("encrypted account export secret type mismatch")
	}
	return exported, secret, nil
}
