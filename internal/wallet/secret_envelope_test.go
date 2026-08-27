package wallet

import (
	"bytes"
	"encoding/json"
	"testing"

	"golang.org/x/crypto/chacha20poly1305"
)

func testEnvelopePolicy() Argon2idPolicy {
	return Argon2idPolicy{
		Time:           1,
		MemoryKiB:      64,
		Parallelism:    1,
		KeyLength:      32,
		SaltLength:     16,
		MaxTime:        4,
		MaxMemoryKiB:   256 * 1024,
		MaxParallelism: 8,
		MaxKeyLength:   32,
		MaxSaltLength:  64,
	}
}

func testEnvelopeMetadata() EnvelopeMetadata {
	return EnvelopeMetadata{
		AccountID:          "018f76c1-04e7-4d55-8db4-f57c7ff9e3b2",
		SecretType:         SecretTypeMnemonic,
		Address:            "0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266",
		EnvelopeGeneration: 1,
		Derivation: DerivationMetadata{
			Scheme:       "bip44",
			Path:         "m/44'/60'/0'/0/0",
			AccountIndex: 0,
			ChangeIndex:  0,
			AddressIndex: 0,
			Language:     "english",
		},
	}
}

func TestSecretEnvelopeRoundTripPreservesExactPassword(t *testing.T) {
	codec, err := NewSecretEnvelopeCodec(testEnvelopePolicy())
	if err != nil {
		t.Fatal(err)
	}
	metadata := testEnvelopeMetadata()
	plaintext := []byte("test test test test test test test test test test test junk")
	password := []byte("  exact 密碼 password  ")

	envelope, err := codec.Seal(password, metadata, plaintext)
	if err != nil {
		t.Fatal(err)
	}
	opened, err := codec.Open(password, metadata, envelope)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(plaintext, opened) {
		t.Fatal("opened secret differs")
	}
	if _, err := codec.Open([]byte("exact 密碼 password"), metadata, envelope); err == nil {
		t.Fatal("trimmed password unexpectedly opened envelope")
	}
}

func TestSecretEnvelopeUsesStoredKDFParameters(t *testing.T) {
	firstPolicy := testEnvelopePolicy()
	first, err := NewSecretEnvelopeCodec(firstPolicy)
	if err != nil {
		t.Fatal(err)
	}
	metadata := testEnvelopeMetadata()
	password := []byte("Strong envelope password 1!")
	envelope, err := first.Seal(password, metadata, []byte("secret"))
	if err != nil {
		t.Fatal(err)
	}

	secondPolicy := testEnvelopePolicy()
	secondPolicy.Time = 2
	secondPolicy.MemoryKiB = 128
	second, err := NewSecretEnvelopeCodec(secondPolicy)
	if err != nil {
		t.Fatal(err)
	}
	opened, err := second.Open(password, metadata, envelope)
	if err != nil {
		t.Fatal(err)
	}
	if string(opened) != "secret" {
		t.Fatal("stored KDF parameters were not used")
	}
}

func TestSecretEnvelopeRejectsCorruption(t *testing.T) {
	codec, err := NewSecretEnvelopeCodec(testEnvelopePolicy())
	if err != nil {
		t.Fatal(err)
	}
	metadata := testEnvelopeMetadata()
	password := []byte("Strong envelope password 1!")
	encoded, err := codec.Seal(password, metadata, []byte("secret"))
	if err != nil {
		t.Fatal(err)
	}

	mutations := map[string]func(*SecretEnvelopeV1){
		"version":          func(envelope *SecretEnvelopeV1) { envelope.Version++ },
		"kdf algorithm":    func(envelope *SecretEnvelopeV1) { envelope.KDF.Algorithm = "scrypt" },
		"kdf time":         func(envelope *SecretEnvelopeV1) { envelope.KDF.Time++ },
		"kdf memory":       func(envelope *SecretEnvelopeV1) { envelope.KDF.MemoryKiB++ },
		"kdf parallelism":  func(envelope *SecretEnvelopeV1) { envelope.KDF.Parallelism++ },
		"salt":             func(envelope *SecretEnvelopeV1) { envelope.KDF.Salt[0] ^= 0xff },
		"cipher algorithm": func(envelope *SecretEnvelopeV1) { envelope.Cipher.Algorithm = "aes-gcm" },
		"nonce":            func(envelope *SecretEnvelopeV1) { envelope.Cipher.Nonce[0] ^= 0xff },
		"ciphertext":       func(envelope *SecretEnvelopeV1) { envelope.Ciphertext[0] ^= 0xff },
		"tag":              func(envelope *SecretEnvelopeV1) { envelope.Ciphertext[len(envelope.Ciphertext)-1] ^= 0xff },
	}

	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			var envelope SecretEnvelopeV1
			if err := json.Unmarshal(encoded, &envelope); err != nil {
				t.Fatal(err)
			}
			mutate(&envelope)
			corrupted, err := json.Marshal(envelope)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := codec.Open(password, metadata, corrupted); err == nil {
				t.Fatal("corrupted envelope opened")
			}
		})
	}
}

func TestSecretEnvelopeValidationErrors(t *testing.T) {
	production := ProductionArgon2idPolicy()
	if _, err := NewSecretEnvelopeCodec(production); err != nil {
		t.Fatal(err)
	}
	invalidPolicies := []Argon2idPolicy{
		{},
		{Time: 2, MaxTime: 1, MemoryKiB: 64, MaxMemoryKiB: 64, Parallelism: 1, MaxParallelism: 1, KeyLength: 32, MaxKeyLength: 32, SaltLength: 16, MaxSaltLength: 16},
		{Time: 1, MaxTime: 1, MemoryKiB: 4, MaxMemoryKiB: 4, Parallelism: 1, MaxParallelism: 1, KeyLength: 32, MaxKeyLength: 32, SaltLength: 16, MaxSaltLength: 16},
		{Time: 1, MaxTime: 1, MemoryKiB: 64, MaxMemoryKiB: 64, Parallelism: 0, MaxParallelism: 1, KeyLength: 32, MaxKeyLength: 32, SaltLength: 16, MaxSaltLength: 16},
		{Time: 1, MaxTime: 1, MemoryKiB: 64, MaxMemoryKiB: 64, Parallelism: 1, MaxParallelism: 1, KeyLength: 16, MaxKeyLength: 32, SaltLength: 16, MaxSaltLength: 16},
		{Time: 1, MaxTime: 1, MemoryKiB: 64, MaxMemoryKiB: 64, Parallelism: 1, MaxParallelism: 1, KeyLength: 32, MaxKeyLength: 32, SaltLength: 8, MaxSaltLength: 16},
	}
	for _, policy := range invalidPolicies {
		if _, err := NewSecretEnvelopeCodec(policy); err == nil {
			t.Fatal("invalid policy was accepted")
		}
	}

	codec, err := NewSecretEnvelopeCodec(testEnvelopePolicy())
	if err != nil {
		t.Fatal(err)
	}
	metadata := testEnvelopeMetadata()
	validPassword := []byte("Strong envelope password 1!")
	if _, err := (*SecretEnvelopeCodec)(nil).Seal(validPassword, metadata, []byte("secret")); err == nil {
		t.Fatal("nil codec sealed secret")
	}
	if _, err := codec.Seal([]byte("short"), metadata, []byte("secret")); err == nil {
		t.Fatal("short password was accepted")
	}
	if _, err := codec.Seal(bytes.Repeat([]byte{'p'}, 4097), metadata, []byte("secret")); err == nil {
		t.Fatal("oversized password was accepted")
	}
	if _, err := codec.Seal(append([]byte("valid password 123"), 0xff), metadata, []byte("secret")); err == nil {
		t.Fatal("invalid UTF-8 password was accepted")
	}
	if _, err := codec.Seal([]byte("valid password\n123"), metadata, []byte("secret")); err == nil {
		t.Fatal("control character password was accepted")
	}
	invalidMetadata := metadata
	invalidMetadata.AccountID = ""
	if _, err := codec.Seal(validPassword, invalidMetadata, []byte("secret")); err == nil {
		t.Fatal("missing account metadata was accepted")
	}
	invalidMetadata = metadata
	invalidMetadata.SecretType = "unknown"
	if _, err := codec.Seal(validPassword, invalidMetadata, []byte("secret")); err == nil {
		t.Fatal("invalid secret type was accepted")
	}
	if _, err := codec.Seal(validPassword, metadata, nil); err == nil {
		t.Fatal("empty plaintext was accepted")
	}
	if _, err := codec.Seal(validPassword, metadata, make([]byte, maxSecretPlaintextSize+1)); err == nil {
		t.Fatal("oversized plaintext was accepted")
	}
	oversizedMetadata := metadata
	oversizedMetadata.AccountID = string(bytes.Repeat([]byte{'a'}, 1<<20+1))
	if _, err := codec.Seal(validPassword, oversizedMetadata, []byte("secret")); err == nil {
		t.Fatal("oversized metadata was accepted")
	}
	if _, err := (*SecretEnvelopeCodec)(nil).Open(validPassword, metadata, []byte("{}")); err == nil {
		t.Fatal("nil codec opened secret")
	}
	if _, err := codec.Open(validPassword, invalidMetadata, []byte("{}")); err == nil {
		t.Fatal("invalid metadata reached envelope parsing")
	}
	if _, err := codec.Open(validPassword, metadata, []byte("not-json")); err == nil {
		t.Fatal("invalid JSON was accepted")
	}
	if _, err := codec.Open(validPassword, metadata, make([]byte, maxEncodedEnvelopeSize+1)); err == nil {
		t.Fatal("oversized envelope was accepted")
	}
	validEnvelope, err := codec.Seal(validPassword, metadata, []byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := codec.Open(validPassword, metadata, append(validEnvelope, []byte("{}")...)); err == nil {
		t.Fatal("trailing envelope data was accepted")
	}

	mutations := []func(*SecretEnvelopeV1){
		func(value *SecretEnvelopeV1) { value.KDF.Time = 0 },
		func(value *SecretEnvelopeV1) { value.KDF.Time = codec.policy.MaxTime + 1 },
		func(value *SecretEnvelopeV1) { value.KDF.MemoryKiB = 1 },
		func(value *SecretEnvelopeV1) { value.KDF.MemoryKiB = codec.policy.MaxMemoryKiB + 1 },
		func(value *SecretEnvelopeV1) { value.KDF.Parallelism = 0 },
		func(value *SecretEnvelopeV1) { value.KDF.Parallelism = codec.policy.MaxParallelism + 1 },
		func(value *SecretEnvelopeV1) { value.KDF.KeyLength = 16 },
		func(value *SecretEnvelopeV1) { value.KDF.Salt = []byte("short") },
		func(value *SecretEnvelopeV1) { value.Cipher.Nonce = []byte("short") },
		func(value *SecretEnvelopeV1) { value.Ciphertext = []byte("short") },
		func(value *SecretEnvelopeV1) {
			value.Ciphertext = make([]byte, maxSecretPlaintextSize+chacha20poly1305.Overhead+1)
		},
	}
	for _, mutate := range mutations {
		var envelope SecretEnvelopeV1
		if err := json.Unmarshal(validEnvelope, &envelope); err != nil {
			t.Fatal(err)
		}
		mutate(&envelope)
		encoded, err := json.Marshal(envelope)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := codec.Open(validPassword, metadata, encoded); err == nil {
			t.Fatal("invalid envelope bounds were accepted")
		}
	}
}

func FuzzSecretEnvelopeOpen(f *testing.F) {
	policy := testEnvelopePolicy()
	policy.MaxMemoryKiB = 256
	codec, err := NewSecretEnvelopeCodec(policy)
	if err != nil {
		f.Fatal(err)
	}
	metadata := testEnvelopeMetadata()
	password := []byte("Strong envelope password 1!")
	seed, err := codec.Seal(password, metadata, []byte("secret"))
	if err != nil {
		f.Fatal(err)
	}
	f.Add(seed, string(password))
	f.Add([]byte(`{"version":1}`), "password")

	f.Fuzz(func(t *testing.T, encoded []byte, fuzzPassword string) {
		if len(encoded) > 1<<20 || len(fuzzPassword) > 1024 {
			return
		}
		defer func() {
			if recovered := recover(); recovered != nil {
				t.Fatalf("envelope open panicked: %v", recovered)
			}
		}()
		plaintext, _ := codec.Open([]byte(fuzzPassword), metadata, encoded)
		clear(plaintext)
	})
}

func TestSecretEnvelopeAuthenticatesMetadata(t *testing.T) {
	codec, err := NewSecretEnvelopeCodec(testEnvelopePolicy())
	if err != nil {
		t.Fatal(err)
	}
	metadata := testEnvelopeMetadata()
	password := []byte("Strong envelope password 1!")
	encoded, err := codec.Seal(password, metadata, []byte("secret"))
	if err != nil {
		t.Fatal(err)
	}

	mutations := map[string]func(*EnvelopeMetadata){
		"account id":  func(value *EnvelopeMetadata) { value.AccountID = "different" },
		"secret type": func(value *EnvelopeMetadata) { value.SecretType = SecretTypePrivateKey },
		"address":     func(value *EnvelopeMetadata) { value.Address = "0x0000000000000000000000000000000000000001" },
		"generation":  func(value *EnvelopeMetadata) { value.EnvelopeGeneration++ },
		"passphrase":  func(value *EnvelopeMetadata) { value.PassphrasePresent = !value.PassphrasePresent },
		"path":        func(value *EnvelopeMetadata) { value.Derivation.Path = "m/44'/60'/1'/0/0" },
		"language":    func(value *EnvelopeMetadata) { value.Derivation.Language = "spanish" },
	}

	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			changed := metadata
			mutate(&changed)
			if _, err := codec.Open(password, changed, encoded); err == nil {
				t.Fatal("envelope opened with changed metadata")
			}
		})
	}
}
