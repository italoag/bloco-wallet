package wallet

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/ethereum/go-ethereum/crypto"
	"golang.org/x/text/unicode/norm"
)

const canonicalSecretVersion = 1

type canonicalSecretV1 struct {
	Version         uint32        `json:"version"`
	Kind            SecretType    `json:"kind"`
	Mnemonic        string        `json:"mnemonic,omitempty"`
	BIP39Passphrase string        `json:"bip39_passphrase,omitempty"`
	BIP39Language   BIP39Language `json:"bip39_language,omitempty"`
	DerivationPath  string        `json:"derivation_path,omitempty"`
	PrivateKey      []byte        `json:"private_key,omitempty"`
}

func encodeCanonicalSecret(secret canonicalSecretV1) ([]byte, error) {
	if err := validateCanonicalSecret(secret); err != nil {
		return nil, err
	}
	secret.Mnemonic = normalizedMnemonic(secret.Mnemonic)
	secret.BIP39Passphrase = norm.NFKD.String(secret.BIP39Passphrase)
	encoded, err := json.Marshal(secret)
	if err != nil {
		return nil, fmt.Errorf("encode canonical secret: %w", err)
	}
	return encoded, nil
}

func decodeCanonicalSecret(encoded []byte) (canonicalSecretV1, error) {
	if len(encoded) == 0 || len(encoded) > maxSecretPlaintextSize {
		return canonicalSecretV1{}, fmt.Errorf("canonical secret size is outside policy")
	}
	var secret canonicalSecretV1
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&secret); err != nil {
		return canonicalSecretV1{}, fmt.Errorf("decode canonical secret: %w", err)
	}
	if err := ensureCanonicalJSONEOF(decoder); err != nil {
		return canonicalSecretV1{}, err
	}
	if err := validateCanonicalSecret(secret); err != nil {
		return canonicalSecretV1{}, err
	}
	secret.Mnemonic = normalizedMnemonic(secret.Mnemonic)
	secret.BIP39Passphrase = norm.NFKD.String(secret.BIP39Passphrase)
	secret.PrivateKey = append([]byte(nil), secret.PrivateKey...)
	return secret, nil
}

func validateCanonicalSecret(secret canonicalSecretV1) error {
	if secret.Version != canonicalSecretVersion {
		return fmt.Errorf("unsupported canonical secret version")
	}
	switch secret.Kind {
	case SecretTypeMnemonic:
		if len(secret.PrivateKey) != 0 {
			return fmt.Errorf("mnemonic secret cannot contain a private key")
		}
		if err := ValidateBIP39Mnemonic(secret.Mnemonic, secret.BIP39Language); err != nil {
			return err
		}
		if _, err := ParseDerivationPath(secret.DerivationPath); err != nil {
			return err
		}
	case SecretTypePrivateKey:
		if secret.Mnemonic != "" || secret.BIP39Passphrase != "" || secret.BIP39Language != "" || secret.DerivationPath != "" {
			return fmt.Errorf("private key secret contains mnemonic metadata")
		}
		if len(secret.PrivateKey) != 32 {
			return fmt.Errorf("private key secret must contain 32 bytes")
		}
		key, err := crypto.ToECDSA(secret.PrivateKey)
		if err != nil {
			return fmt.Errorf("invalid private key secret: %w", err)
		}
		key.D.SetInt64(0)
	default:
		return fmt.Errorf("unsupported canonical secret kind")
	}
	return nil
}

func deriveCanonicalSecretIdentity(secret canonicalSecretV1) ([]byte, string, error) {
	switch secret.Kind {
	case SecretTypeMnemonic:
		path, err := ParseDerivationPath(secret.DerivationPath)
		if err != nil {
			return nil, "", err
		}
		return deriveEVMAccount(secret.Mnemonic, secret.BIP39Passphrase, secret.BIP39Language, path)
	case SecretTypePrivateKey:
		privateKey := append([]byte(nil), secret.PrivateKey...)
		key, err := crypto.ToECDSA(privateKey)
		if err != nil {
			clear(privateKey)
			return nil, "", err
		}
		address := crypto.PubkeyToAddress(key.PublicKey).Hex()
		key.D.SetInt64(0)
		return privateKey, address, nil
	default:
		return nil, "", fmt.Errorf("unsupported canonical secret kind")
	}
}

func canonicalSecretFromStored(account *Account, plaintext []byte) (canonicalSecretV1, error) {
	secret, err := decodeCanonicalSecret(plaintext)
	if err == nil {
		return secret, nil
	}
	switch account.SecretType {
	case SecretTypeMnemonic:
		language := BIP39Language(account.BIP39Language)
		if language == "" {
			language = BIP39English
		}
		if validationErr := ValidateBIP39Mnemonic(string(plaintext), language); validationErr != nil {
			return canonicalSecretV1{}, err
		}
		path := account.DerivationPath
		if path == "" {
			path = "m/44'/60'/0'/0/0"
		}
		secret = canonicalSecretV1{
			Version:        canonicalSecretVersion,
			Kind:           SecretTypeMnemonic,
			Mnemonic:       string(plaintext),
			BIP39Language:  language,
			DerivationPath: path,
		}
	case SecretTypePrivateKey:
		if len(plaintext) != 32 {
			return canonicalSecretV1{}, err
		}
		secret = canonicalSecretV1{Version: canonicalSecretVersion, Kind: SecretTypePrivateKey, PrivateKey: append([]byte(nil), plaintext...)}
	default:
		return canonicalSecretV1{}, err
	}
	if validationErr := validateCanonicalSecret(secret); validationErr != nil {
		clear(secret.PrivateKey)
		return canonicalSecretV1{}, validationErr
	}
	return secret, nil
}

func deriveStoredSecretIdentity(account *Account, plaintext []byte) ([]byte, string, error) {
	secret, err := decodeCanonicalSecret(plaintext)
	if err == nil {
		defer clear(secret.PrivateKey)
		if secret.Kind != account.SecretType {
			return nil, "", fmt.Errorf("canonical secret type does not match account")
		}
		if secret.Kind == SecretTypeMnemonic && (secret.DerivationPath != account.DerivationPath || string(secret.BIP39Language) != account.BIP39Language || (secret.BIP39Passphrase != "") != account.HasBIP39Passphrase) {
			return nil, "", fmt.Errorf("canonical derivation metadata does not match account")
		}
		return deriveCanonicalSecretIdentity(secret)
	}
	if account.SecretType == SecretTypeMnemonic {
		language := BIP39Language(account.BIP39Language)
		if language == "" {
			language = BIP39English
		}
		if validationErr := ValidateBIP39Mnemonic(string(plaintext), language); validationErr != nil {
			return nil, "", err
		}
		path, pathErr := ParseDerivationPath(account.DerivationPath)
		if pathErr != nil {
			return nil, "", pathErr
		}
		return deriveEVMAccount(string(plaintext), "", language, path)
	}
	if account.SecretType == SecretTypePrivateKey && len(plaintext) == 32 {
		return deriveSecretIdentity(SecretTypePrivateKey, plaintext)
	}
	return nil, "", err
}

type mnemonicBackupMaterial struct {
	words      []string
	passphrase string
	path       string
	language   BIP39Language
}

func mnemonicBackupMaterialFromStoredSecret(account *Account, plaintext []byte) (mnemonicBackupMaterial, error) {
	secret, err := decodeCanonicalSecret(plaintext)
	if err == nil {
		defer clear(secret.PrivateKey)
		if secret.Kind != SecretTypeMnemonic {
			return mnemonicBackupMaterial{}, fmt.Errorf("account does not contain a mnemonic")
		}
		return mnemonicBackupMaterial{
			words:      strings.Fields(secret.Mnemonic),
			passphrase: secret.BIP39Passphrase,
			path:       secret.DerivationPath,
			language:   secret.BIP39Language,
		}, nil
	}
	if account.SecretType != SecretTypeMnemonic {
		return mnemonicBackupMaterial{}, fmt.Errorf("account does not contain a mnemonic")
	}
	language := BIP39Language(account.BIP39Language)
	if language == "" {
		language = BIP39English
	}
	if validationErr := ValidateBIP39Mnemonic(string(plaintext), language); validationErr != nil {
		return mnemonicBackupMaterial{}, err
	}
	return mnemonicBackupMaterial{
		words:    strings.Fields(string(plaintext)),
		path:     account.DerivationPath,
		language: language,
	}, nil
}

func mnemonicWordsFromStoredSecret(account *Account, plaintext []byte) ([]string, error) {
	material, err := mnemonicBackupMaterialFromStoredSecret(account, plaintext)
	return material.words, err
}

func ensureCanonicalJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err == io.EOF {
		return nil
	} else if err != nil {
		return fmt.Errorf("decode trailing canonical secret data: %w", err)
	}
	return fmt.Errorf("canonical secret contains trailing data")
}

func normalizedMnemonic(value string) string {
	return strings.Join(strings.Fields(norm.NFKD.String(value)), " ")
}
