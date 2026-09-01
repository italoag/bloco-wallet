package wallet

import (
	"bytes"
	"testing"

	"github.com/ethereum/go-ethereum/crypto"
)

func TestCanonicalSecretRoundTrip(t *testing.T) {
	values := []canonicalSecretV1{
		{
			Version:         canonicalSecretVersion,
			Kind:            SecretTypeMnemonic,
			Mnemonic:        "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about",
			BIP39Passphrase: "pássphrase",
			BIP39Language:   BIP39English,
			DerivationPath:  "m/44'/60'/0'/0/0",
		},
		{
			Version:    canonicalSecretVersion,
			Kind:       SecretTypePrivateKey,
			PrivateKey: bytes.Repeat([]byte{1}, 32),
		},
	}
	for _, value := range values {
		encoded, err := encodeCanonicalSecret(value)
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := decodeCanonicalSecret(encoded)
		if err != nil {
			t.Fatal(err)
		}
		if decoded.Kind != value.Kind {
			t.Fatal("canonical secret kind changed")
		}
		if decoded.Kind == SecretTypePrivateKey && !bytes.Equal(decoded.PrivateKey, value.PrivateKey) {
			t.Fatal("private key changed")
		}
	}
}

func FuzzDecodeCanonicalSecret(f *testing.F) {
	seed, err := encodeCanonicalSecret(canonicalSecretV1{
		Version:    canonicalSecretVersion,
		Kind:       SecretTypePrivateKey,
		PrivateKey: bytes.Repeat([]byte{1}, 32),
	})
	if err != nil {
		f.Fatal(err)
	}
	f.Add(seed)
	f.Add([]byte("not-json"))
	f.Fuzz(func(t *testing.T, encoded []byte) {
		if len(encoded) > maxSecretPlaintextSize+1 {
			return
		}
		defer func() {
			if recovered := recover(); recovered != nil {
				t.Fatalf("canonical secret decode panicked: %v", recovered)
			}
		}()
		secret, _ := decodeCanonicalSecret(encoded)
		clear(secret.PrivateKey)
	})
}

func TestCanonicalSecretCompatibilityAndErrorPaths(t *testing.T) {
	if _, err := decodeCanonicalSecret(nil); err == nil {
		t.Fatal("empty canonical secret was accepted")
	}
	if _, err := decodeCanonicalSecret(make([]byte, maxSecretPlaintextSize+1)); err == nil {
		t.Fatal("oversized canonical secret was accepted")
	}
	if _, err := decodeCanonicalSecret([]byte(`{"version":1}{"version":1}`)); err == nil {
		t.Fatal("trailing canonical JSON was accepted")
	}
	if _, err := decodeCanonicalSecret([]byte(`{"version":1} trailing`)); err == nil {
		t.Fatal("malformed trailing canonical data was accepted")
	}
	invalidEncoded := [][]byte{
		[]byte(`{"version":2,"kind":"private_key","private_key":"AQ=="}`),
		[]byte(`{"version":1,"kind":"unknown"}`),
		[]byte(`{"version":1,"kind":"private_key","private_key":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="}`),
		[]byte(`{"version":1,"kind":"mnemonic","mnemonic":"invalid","bip39_language":"english","derivation_path":"m/44'/60'/0'/0/0"}`),
	}
	for _, encoded := range invalidEncoded {
		if _, err := decodeCanonicalSecret(encoded); err == nil {
			t.Fatal("invalid decoded canonical secret was accepted")
		}
	}

	mnemonic := "test test test test test test test test test test test junk"
	defaultLegacy, err := canonicalSecretFromStored(&Account{SecretType: SecretTypeMnemonic}, []byte(mnemonic))
	if err != nil || defaultLegacy.BIP39Language != BIP39English || defaultLegacy.DerivationPath != "m/44'/60'/0'/0/0" {
		t.Fatal("default legacy mnemonic metadata was not canonicalized")
	}
	if _, err := canonicalSecretFromStored(&Account{SecretType: SecretTypeMnemonic}, []byte("invalid")); err == nil {
		t.Fatal("invalid legacy mnemonic was canonicalized")
	}
	if _, err := canonicalSecretFromStored(&Account{SecretType: SecretTypePrivateKey}, []byte("short")); err == nil {
		t.Fatal("short legacy private key was canonicalized")
	}
	if _, err := canonicalSecretFromStored(&Account{SecretType: SecretTypePrivateKey}, make([]byte, 32)); err == nil {
		t.Fatal("invalid legacy private key was canonicalized")
	}
	if _, err := canonicalSecretFromStored(&Account{SecretType: "unknown"}, []byte("secret")); err == nil {
		t.Fatal("unknown legacy secret was canonicalized")
	}
	legacyAccount := &Account{SecretType: SecretTypeMnemonic, DerivationPath: "m/44'/60'/0'/0/0"}
	privateKey, _, err := deriveStoredSecretIdentity(legacyAccount, []byte(mnemonic))
	if err != nil {
		t.Fatal(err)
	}
	clear(privateKey)
	words, err := mnemonicWordsFromStoredSecret(legacyAccount, []byte(mnemonic))
	if err != nil || len(words) != 12 {
		t.Fatal("legacy mnemonic compatibility failed")
	}
	invalidLegacy := *legacyAccount
	invalidLegacy.DerivationPath = "invalid"
	if _, _, err := deriveStoredSecretIdentity(&invalidLegacy, []byte(mnemonic)); err == nil {
		t.Fatal("invalid legacy derivation path was accepted")
	}
	if _, _, err := deriveStoredSecretIdentity(legacyAccount, []byte("invalid")); err == nil {
		t.Fatal("invalid legacy mnemonic was accepted")
	}

	secret := canonicalSecretV1{
		Version:        canonicalSecretVersion,
		Kind:           SecretTypeMnemonic,
		Mnemonic:       mnemonic,
		BIP39Language:  BIP39English,
		DerivationPath: "m/44'/60'/0'/0/0",
	}
	encoded, err := encodeCanonicalSecret(secret)
	if err != nil {
		t.Fatal(err)
	}
	mismatch := &Account{SecretType: SecretTypePrivateKey}
	if _, _, err := deriveStoredSecretIdentity(mismatch, encoded); err == nil {
		t.Fatal("canonical type mismatch was accepted")
	}
	mismatch = &Account{SecretType: SecretTypeMnemonic, DerivationPath: "m/44'/60'/1'/0/0", BIP39Language: string(BIP39English)}
	if _, _, err := deriveStoredSecretIdentity(mismatch, encoded); err == nil {
		t.Fatal("canonical metadata mismatch was accepted")
	}

	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	keyBytes := crypto.FromECDSA(key)
	defer clear(keyBytes)
	legacyKeyAccount := &Account{SecretType: SecretTypePrivateKey}
	derived, _, err := deriveStoredSecretIdentity(legacyKeyAccount, keyBytes)
	if err != nil {
		t.Fatal(err)
	}
	clear(derived)
	if _, err := mnemonicWordsFromStoredSecret(legacyKeyAccount, keyBytes); err == nil {
		t.Fatal("private key returned mnemonic words")
	}
	if _, _, err := deriveCanonicalSecretIdentity(canonicalSecretV1{Kind: "unknown"}); err == nil {
		t.Fatal("unknown canonical identity kind was accepted")
	}
	if _, _, err := deriveCanonicalSecretIdentity(canonicalSecretV1{Kind: SecretTypePrivateKey, PrivateKey: make([]byte, 32)}); err == nil {
		t.Fatal("invalid canonical private key was accepted")
	}
}

func TestCanonicalSecretRejectsMixedAndInvalidPayloads(t *testing.T) {
	invalid := []canonicalSecretV1{
		{},
		{Version: 2, Kind: SecretTypePrivateKey, PrivateKey: bytes.Repeat([]byte{1}, 32)},
		{Version: 1, Kind: SecretTypePrivateKey, PrivateKey: []byte("short")},
		{Version: 1, Kind: SecretTypePrivateKey, PrivateKey: bytes.Repeat([]byte{1}, 32), Mnemonic: "mixed"},
		{Version: 1, Kind: SecretTypeMnemonic, Mnemonic: "invalid", BIP39Language: BIP39English, DerivationPath: "m/44'/60'/0'/0/0"},
	}
	for _, value := range invalid {
		if _, err := encodeCanonicalSecret(value); err == nil {
			t.Fatal("invalid canonical secret was encoded")
		}
	}
	if _, err := decodeCanonicalSecret([]byte(`{"version":1,"kind":"private_key","private_key":"AQ==","extra":true}`)); err == nil {
		t.Fatal("unknown canonical field was accepted")
	}
}
