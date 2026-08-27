package wallet

import (
	"encoding/hex"
	"strings"
	"testing"
)

func TestBIP39OfficialEnglishVector(t *testing.T) {
	entropy, _ := hex.DecodeString("00000000000000000000000000000000")
	mnemonic, err := mnemonicFromEntropy(entropy, BIP39English)
	if err != nil {
		t.Fatal(err)
	}
	expectedMnemonic := "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"
	if mnemonic != expectedMnemonic {
		t.Fatalf("mnemonic mismatch: %s", mnemonic)
	}
	seed, err := bip39Seed(mnemonic, "TREZOR", BIP39English)
	if err != nil {
		t.Fatal(err)
	}
	expectedSeed := "c55257c360c07c72029aebc1b53c05ed0362ada38ead3e3e9efa3708e53495531f09a6987599d18264c1e1c92f2cf141630c7a3c4ab7c81b2f001698e7463b04"
	if hex.EncodeToString(seed) != expectedSeed {
		t.Fatal("seed does not match official BIP39 vector")
	}
}

func TestBIP39IndependentMultilingualVectors(t *testing.T) {
	vectors := []struct {
		language BIP39Language
		mnemonic string
		seed     string
	}{
		{BIP39French, "abaisser abaisser abaisser abaisser abaisser abaisser abaisser abaisser abaisser abaisser abaisser abeille", "3bf3366c40256d7e2fca716fddf8673425c7c7e444af290ee1edf1bbf095e6e78a7190253f3e46f1e2069345d4b05ac17b242faa225c0a3e4d268976744e0698"},
		{BIP39Japanese, "あいこくしん　あいこくしん　あいこくしん　あいこくしん　あいこくしん　あいこくしん　あいこくしん　あいこくしん　あいこくしん　あいこくしん　あいこくしん　あおぞら", "5a6c23b5abdd5c3e1f7d77ad25ecd715647bdafb44dab324c730a76a45d7421daccee1a4ff0739715a2c56a8a9f1e527a5e3496224d91293bfcd9b5393bfff83"},
		{BIP39Portuguese, "abacate abacate abacate abacate abacate abacate abacate abacate abacate abacate abacate abater", "ab9742b024a1e8bd241b76f8b3a157e9d442da60277bc8f36b8b23afe163de79414fb49fd1a8dd26f4ea7f0dc965c760b3b80727557bdca61e1f0b0f069952f2"},
		{BIP39Spanish, "ábaco ábaco ábaco ábaco ábaco ábaco ábaco ábaco ábaco ábaco ábaco abierto", "29a2ee16de47d07025de37e7d9c596869439f9bcd26a702d2bae64db2bf0f68383841c5444b5b3bd39dd720d2ebe59969e110e5955c8e6d32c6c3294fd87439b"},
	}
	entropy := make([]byte, 16)
	for _, vector := range vectors {
		mnemonic, err := mnemonicFromEntropy(entropy, vector.language)
		if err != nil {
			t.Fatal(err)
		}
		if normalizedMnemonic(mnemonic) != normalizedMnemonic(vector.mnemonic) {
			t.Fatalf("%s mnemonic does not match independent vector", vector.language)
		}
		seed, err := bip39Seed(vector.mnemonic, "TREZOR", vector.language)
		if err != nil {
			t.Fatal(err)
		}
		if hex.EncodeToString(seed) != vector.seed {
			t.Fatalf("%s seed does not match independent vector", vector.language)
		}
		clear(seed)
	}
}

func TestBIP39SupportsOfficialWordCountsAndLanguages(t *testing.T) {
	languages := SupportedBIP39Languages()
	if len(languages) != 10 {
		t.Fatalf("expected 10 languages, got %d", len(languages))
	}
	for _, language := range languages {
		for _, wordCount := range []int{12, 15, 18, 21, 24} {
			mnemonic, err := generateMnemonicForLanguage(wordCount, language)
			if err != nil {
				t.Fatalf("%s/%d: %v", language, wordCount, err)
			}
			if len(strings.Fields(mnemonic)) != wordCount {
				t.Fatalf("%s generated wrong word count", language)
			}
			if err := ValidateBIP39Mnemonic(mnemonic, language); err != nil {
				t.Fatalf("%s/%d validation failed: %v", language, wordCount, err)
			}
			detected, err := DetectBIP39Language(mnemonic)
			if err != nil && !strings.Contains(err.Error(), "ambiguous") {
				t.Fatalf("detect %s: %v", language, err)
			}
			if err == nil && detected != language {
				t.Fatalf("detected %s as %s", language, detected)
			}
		}
	}
}

func TestBIP39RejectsOversizedAndUnsupportedInputs(t *testing.T) {
	if _, err := generateMnemonicForLanguage(13, BIP39English); err == nil {
		t.Fatal("unsupported mnemonic word count was accepted")
	}
	if _, err := mnemonicFromEntropy(make([]byte, 15), BIP39English); err == nil {
		t.Fatal("invalid entropy length was accepted")
	}
	if err := ValidateBIP39Mnemonic(strings.Repeat("a", maxMnemonicInputLength+1), BIP39English); err == nil {
		t.Fatal("oversized mnemonic was accepted")
	}
	mnemonic := "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"
	if _, err := bip39Seed(mnemonic, strings.Repeat("p", maxBIP39PassphraseLength+1), BIP39English); err == nil {
		t.Fatal("oversized BIP39 passphrase was accepted")
	}
	if _, err := ParseDerivationPath("m/" + strings.Repeat("1/", maxDerivationPathLength)); err == nil {
		t.Fatal("oversized derivation path was accepted")
	}
}

func TestBIP39RejectsChecksumWordAndLanguageErrors(t *testing.T) {
	valid := "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"
	if err := ValidateBIP39Mnemonic(valid, BIP39English); err != nil {
		t.Fatal(err)
	}
	invalidChecksum := "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon"
	if err := ValidateBIP39Mnemonic(invalidChecksum, BIP39English); err == nil {
		t.Fatal("invalid checksum was accepted")
	}
	if err := ValidateBIP39Mnemonic(valid, BIP39Spanish); err == nil {
		t.Fatal("wrong wordlist was accepted")
	}
	if err := ValidateBIP39Mnemonic("abandon abandon", BIP39English); err == nil {
		t.Fatal("invalid word count was accepted")
	}
}

func TestBIP39AppliesNFKDToMnemonicAndPassphrase(t *testing.T) {
	mnemonic := "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"
	composed := "pássphrase"
	decomposed := "pa\u0301ssphrase"
	first, err := bip39Seed(mnemonic, composed, BIP39English)
	if err != nil {
		t.Fatal(err)
	}
	second, err := bip39Seed(mnemonic, decomposed, BIP39English)
	if err != nil {
		t.Fatal(err)
	}
	if hex.EncodeToString(first) != hex.EncodeToString(second) {
		t.Fatal("NFKD-equivalent passphrases produced different seeds")
	}
}

func FuzzValidateBIP39Mnemonic(f *testing.F) {
	f.Add("abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about", string(BIP39English))
	f.Add("invalid", "unknown")
	f.Fuzz(func(t *testing.T, mnemonic, language string) {
		if len(mnemonic) > 4096 || len(language) > 64 {
			return
		}
		defer func() {
			if recovered := recover(); recovered != nil {
				t.Fatalf("BIP39 validation panicked: %v", recovered)
			}
		}()
		_ = ValidateBIP39Mnemonic(mnemonic, BIP39Language(language))
	})
}

func FuzzParseDerivationPath(f *testing.F) {
	f.Add("m/44'/60'/0'/0/0")
	f.Add("m//")
	f.Fuzz(func(t *testing.T, value string) {
		if len(value) > 1024 {
			return
		}
		defer func() {
			if recovered := recover(); recovered != nil {
				t.Fatalf("derivation path parsing panicked: %v", recovered)
			}
		}()
		_, _ = ParseDerivationPath(value)
	})
}

func TestParseAndDeriveEVMPath(t *testing.T) {
	path, err := ParseDerivationPath("m/44'/60'/0'/0/0")
	if err != nil {
		t.Fatal(err)
	}
	if path.String() != "m/44'/60'/0'/0/0" {
		t.Fatalf("path round-trip mismatch: %s", path.String())
	}
	mnemonic := "test test test test test test test test test test test junk"
	privateKey, address, err := deriveEVMAccount(mnemonic, "", BIP39English, path)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(privateKey)
	if address != "0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266" {
		t.Fatalf("unexpected derived address: %s", address)
	}
	invalidPaths := []string{"", "44'/60'/0'/0/0", "m//0", "m/2147483648", "m/44'/60'/x/0/0", "m/44'/61'/0'/0/0"}
	for _, value := range invalidPaths {
		if _, err := ParseDerivationPath(value); err == nil {
			t.Fatalf("invalid path accepted: %s", value)
		}
	}
}
