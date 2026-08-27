package wallet

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

const maxKeystoreImportSize = 1 << 20

var (
	ErrSourceIdentityKeyUnavailable = errors.New("source identity key is unavailable")
	ErrStoragePasswordConfirmation  = errors.New("storage password confirmation does not match")
)

type ImportPreview struct {
	Address            string
	SecretType         SecretType
	DerivationPath     string
	BIP39Language      BIP39Language
	HasBIP39Passphrase bool
	SourceFormat       string
}

type MnemonicImportRequest struct {
	Name                   string
	Mnemonic               string
	BIP39Passphrase        string
	BIP39Language          BIP39Language
	DerivationPath         string
	StoragePassword        []byte
	ConfirmStoragePassword []byte
}

type PrivateKeyImportRequest struct {
	Name                   string
	PrivateKey             string
	StoragePassword        []byte
	ConfirmStoragePassword []byte
}

type KeystoreImportRequest struct {
	Name                   string
	KeystoreJSON           []byte
	SourcePassword         []byte
	StoragePassword        []byte
	ConfirmStoragePassword []byte
}

func PreviewMnemonicImport(request MnemonicImportRequest) (ImportPreview, error) {
	secret, err := canonicalMnemonicFromImport(request)
	if err != nil {
		return ImportPreview{}, err
	}
	privateKey, address, err := deriveCanonicalSecretIdentity(secret)
	if err != nil {
		return ImportPreview{}, err
	}
	clear(privateKey)
	return previewForCanonicalSecret(secret, address, "bip39"), nil
}

func PreviewPrivateKeyImport(request PrivateKeyImportRequest) (ImportPreview, error) {
	secret, err := canonicalPrivateKeyFromHex(request.PrivateKey)
	if err != nil {
		return ImportPreview{}, err
	}
	defer clear(secret.PrivateKey)
	privateKey, address, err := deriveCanonicalSecretIdentity(secret)
	if err != nil {
		return ImportPreview{}, err
	}
	clear(privateKey)
	return previewForCanonicalSecret(secret, address, "private_key"), nil
}

func PreviewKeystoreImport(data, sourcePassword []byte) (ImportPreview, error) {
	return PreviewKeystoreImportContext(context.Background(), data, sourcePassword)
}

func PreviewKeystoreImportContext(ctx context.Context, data, sourcePassword []byte) (ImportPreview, error) {
	secret, address, err := canonicalPrivateKeyFromKeystoreContext(ctx, data, sourcePassword)
	if err != nil {
		return ImportPreview{}, err
	}
	defer clear(secret.PrivateKey)
	return previewForCanonicalSecret(secret, address, "keystore_v3"), nil
}

func (vault *WalletVault) ImportMnemonic(ctx context.Context, request MnemonicImportRequest) (AccountSummary, error) {
	secret, err := canonicalMnemonicFromImport(request)
	if err != nil {
		return AccountSummary{}, err
	}
	return vault.importCanonicalSecret(ctx, strings.TrimSpace(request.Name), secret, request.StoragePassword, request.ConfirmStoragePassword)
}

func (vault *WalletVault) ImportPrivateKey(ctx context.Context, request PrivateKeyImportRequest) (AccountSummary, error) {
	secret, err := canonicalPrivateKeyFromHex(request.PrivateKey)
	if err != nil {
		return AccountSummary{}, err
	}
	defer clear(secret.PrivateKey)
	return vault.importCanonicalSecret(ctx, strings.TrimSpace(request.Name), secret, request.StoragePassword, request.ConfirmStoragePassword)
}

func (vault *WalletVault) ImportKeystore(ctx context.Context, request KeystoreImportRequest) (AccountSummary, error) {
	secret, _, err := canonicalPrivateKeyFromKeystoreContext(ctx, request.KeystoreJSON, request.SourcePassword)
	if err != nil {
		return AccountSummary{}, err
	}
	defer clear(secret.PrivateKey)
	return vault.importCanonicalSecret(ctx, strings.TrimSpace(request.Name), secret, request.StoragePassword, request.ConfirmStoragePassword)
}

func (vault *WalletVault) importCanonicalSecret(ctx context.Context, name string, secret canonicalSecretV1, storagePassword, confirmation []byte) (AccountSummary, error) {
	if err := vault.beginOperation(); err != nil {
		return AccountSummary{}, err
	}
	defer vault.endOperation()
	if err := ctx.Err(); err != nil {
		return AccountSummary{}, err
	}
	if name == "" {
		return AccountSummary{}, fmt.Errorf("account name is required")
	}
	if len(storagePassword) != len(confirmation) || subtle.ConstantTimeCompare(storagePassword, confirmation) != 1 {
		return AccountSummary{}, ErrStoragePasswordConfirmation
	}
	if err := validateNewStoragePassword(storagePassword); err != nil {
		return AccountSummary{}, err
	}
	if len(vault.sourceIdentityKey) != 32 {
		return AccountSummary{}, ErrSourceIdentityKeyUnavailable
	}
	canonical, err := encodeCanonicalSecret(secret)
	if err != nil {
		return AccountSummary{}, err
	}
	defer clear(canonical)
	privateKey, address, err := deriveCanonicalSecretIdentity(secret)
	if err != nil {
		return AccountSummary{}, err
	}
	clear(privateKey)
	sourceIdentity := vault.deriveSourceIdentity(canonical)
	if existing, err := vault.repository.FindAccountBySourceIdentity(ctx, sourceIdentity); err != nil && !errors.Is(err, ErrAccountNotFound) {
		return AccountSummary{}, err
	} else if err == nil && existing != nil {
		return AccountSummary{}, ErrAccountConflict
	}
	accountID, err := newUUID(vault.options.Random)
	if err != nil {
		return AccountSummary{}, err
	}
	metadata, err := metadataForCanonicalSecret(accountID, address, secret, 1)
	if err != nil {
		return AccountSummary{}, err
	}
	envelope, err := vault.codec.Seal(storagePassword, metadata, canonical)
	if err != nil {
		return AccountSummary{}, err
	}
	now := vault.options.Now().UTC()
	account := &Account{
		AccountID:          accountID,
		Name:               name,
		Address:            address,
		SignerKind:         SignerKindSoftware,
		SignerReference:    accountID,
		SecretType:         secret.Kind,
		DerivationScheme:   metadata.Derivation.Scheme,
		DerivationPath:     metadata.Derivation.Path,
		AccountIndex:       metadata.Derivation.AccountIndex,
		ChangeIndex:        metadata.Derivation.ChangeIndex,
		AddressIndex:       metadata.Derivation.AddressIndex,
		BIP39Language:      metadata.Derivation.Language,
		HasBIP39Passphrase: secret.Kind == SecretTypeMnemonic && secret.BIP39Passphrase != "",
		Capabilities:       CapabilitySignTransaction | CapabilitySignMessage | CapabilityExportSecret,
		State:              AccountStateActive,
		SecretEnvelope:     envelope,
		EnvelopeGeneration: 1,
		AuthorizationEpoch: 1,
		BackupGeneration:   1,
		SourceIdentity:     sourceIdentity,
		Revision:           1,
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	var persisted *Account
	if err := vault.repository.WithAccountTransaction(ctx, func(transaction AccountRepository) error {
		if existing, err := transaction.FindAccountBySourceIdentity(ctx, sourceIdentity); err != nil && !errors.Is(err, ErrAccountNotFound) {
			return err
		} else if err == nil && existing != nil {
			return ErrAccountConflict
		}
		related, err := transaction.FindAccountsByAddress(ctx, account.Address)
		if err != nil {
			return err
		}
		if len(related) > 0 {
			account.RelatedAccountID = related[0].AccountID
		}
		if err := transaction.CreateAccount(ctx, account); err != nil {
			return err
		}
		stored, err := transaction.GetAccount(ctx, accountID)
		if err != nil {
			return err
		}
		reopened, err := vault.codec.Open(storagePassword, metadataForAccount(stored), stored.SecretEnvelope)
		if err != nil {
			return err
		}
		defer clear(reopened)
		if !bytes.Equal(reopened, canonical) {
			return fmt.Errorf("persisted import envelope mismatch")
		}
		verificationKey, verificationAddress, err := deriveStoredSecretIdentity(stored, reopened)
		if err != nil {
			return err
		}
		clear(verificationKey)
		if !addressesEqual(verificationAddress, stored.Address) {
			return fmt.Errorf("persisted import identity mismatch")
		}
		persisted = stored
		return ctx.Err()
	}); err != nil {
		return AccountSummary{}, err
	}
	return summaryFromAccount(persisted), nil
}

func canonicalMnemonicFromImport(request MnemonicImportRequest) (canonicalSecretV1, error) {
	language := request.BIP39Language
	if language == "" {
		detected, err := DetectBIP39Language(request.Mnemonic)
		if err != nil {
			return canonicalSecretV1{}, err
		}
		language = detected
	}
	pathValue := request.DerivationPath
	if pathValue == "" {
		pathValue = "m/44'/60'/0'/0/0"
	}
	path, err := ParseDerivationPath(pathValue)
	if err != nil {
		return canonicalSecretV1{}, err
	}
	secret := canonicalSecretV1{
		Version:         canonicalSecretVersion,
		Kind:            SecretTypeMnemonic,
		Mnemonic:        request.Mnemonic,
		BIP39Passphrase: request.BIP39Passphrase,
		BIP39Language:   language,
		DerivationPath:  path.String(),
	}
	encoded, err := encodeCanonicalSecret(secret)
	if err != nil {
		return canonicalSecretV1{}, err
	}
	defer clear(encoded)
	return decodeCanonicalSecret(encoded)
}

func canonicalPrivateKeyFromHex(value string) (canonicalSecretV1, error) {
	if value == "" || strings.TrimSpace(value) != value {
		return canonicalSecretV1{}, fmt.Errorf("private key must not contain surrounding whitespace")
	}
	value = strings.TrimPrefix(value, "0x")
	if len(value) != 64 {
		return canonicalSecretV1{}, fmt.Errorf("private key must contain exactly 32 bytes")
	}
	privateKey, err := hex.DecodeString(value)
	if err != nil {
		return canonicalSecretV1{}, fmt.Errorf("decode private key: %w", err)
	}
	secret := canonicalSecretV1{Version: canonicalSecretVersion, Kind: SecretTypePrivateKey, PrivateKey: privateKey}
	if err := validateCanonicalSecret(secret); err != nil {
		clear(privateKey)
		return canonicalSecretV1{}, err
	}
	return secret, nil
}

func canonicalPrivateKeyFromKeystore(data, sourcePassword []byte) (canonicalSecretV1, string, error) {
	return canonicalPrivateKeyFromKeystoreContext(context.Background(), data, sourcePassword)
}

func canonicalPrivateKeyFromKeystoreContext(ctx context.Context, data, sourcePassword []byte) (canonicalSecretV1, string, error) {
	if len(data) == 0 || len(data) > maxKeystoreImportSize {
		return canonicalSecretV1{}, "", fmt.Errorf("keystore size is outside policy")
	}
	validator := &KeystoreValidator{}
	validated, err := validator.ValidateKeystoreV3(data)
	if err != nil {
		return canonicalSecretV1{}, "", err
	}
	key, err := decryptKeySafelyContext(ctx, data, string(sourcePassword))
	if err != nil {
		return canonicalSecretV1{}, "", fmt.Errorf("decrypt keystore: %w", err)
	}
	privateKey := crypto.FromECDSA(key.PrivateKey)
	address := crypto.PubkeyToAddress(key.PrivateKey.PublicKey).Hex()
	key.PrivateKey.D.SetInt64(0)
	if validated.Address != "" {
		expectedAddress := common.HexToAddress(validated.Address).Hex()
		if !addressesEqual(expectedAddress, address) {
			clear(privateKey)
			return canonicalSecretV1{}, "", fmt.Errorf("keystore address does not match decrypted key")
		}
	}
	return canonicalSecretV1{Version: canonicalSecretVersion, Kind: SecretTypePrivateKey, PrivateKey: privateKey}, address, nil
}

func metadataForCanonicalSecret(accountID, address string, secret canonicalSecretV1, generation uint64) (EnvelopeMetadata, error) {
	metadata := EnvelopeMetadata{
		AccountID:          accountID,
		SecretType:         secret.Kind,
		Address:            address,
		EnvelopeGeneration: generation,
		PassphrasePresent:  secret.Kind == SecretTypeMnemonic && secret.BIP39Passphrase != "",
	}
	if secret.Kind == SecretTypeMnemonic {
		path, err := ParseDerivationPath(secret.DerivationPath)
		if err != nil {
			return EnvelopeMetadata{}, err
		}
		metadata.Derivation = derivationMetadataForPath(path, secret.BIP39Language)
	}
	return metadata, nil
}

func previewForCanonicalSecret(secret canonicalSecretV1, address, sourceFormat string) ImportPreview {
	return ImportPreview{
		Address:            address,
		SecretType:         secret.Kind,
		DerivationPath:     secret.DerivationPath,
		BIP39Language:      secret.BIP39Language,
		HasBIP39Passphrase: secret.BIP39Passphrase != "",
		SourceFormat:       sourceFormat,
	}
}

func (vault *WalletVault) deriveSourceIdentity(canonical []byte) string {
	mac := hmac.New(sha256.New, vault.sourceIdentityKey)
	_, _ = mac.Write([]byte("bloco-wallet-source-v1\x00"))
	_, _ = mac.Write(canonical)
	return "hmac-sha256:" + hex.EncodeToString(mac.Sum(nil))
}
