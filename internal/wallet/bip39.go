package wallet

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/sha512"
	"fmt"
	"strconv"
	"strings"

	"github.com/ethereum/go-ethereum/crypto"
	bitcoinbip39 "github.com/mrtnetwork/bitcoin/bip39"
	"github.com/tyler-smith/go-bip32"
	"github.com/tyler-smith/go-bip39/wordlists"
	"golang.org/x/crypto/pbkdf2"
	"golang.org/x/text/unicode/norm"
)

const (
	maxMnemonicInputLength   = 4096
	maxBIP39PassphraseLength = 1024
	maxDerivationPathLength  = 255
)

type BIP39Language string

const (
	BIP39English            BIP39Language = "english"
	BIP39ChineseSimplified  BIP39Language = "chinese_simplified"
	BIP39ChineseTraditional BIP39Language = "chinese_traditional"
	BIP39Czech              BIP39Language = "czech"
	BIP39French             BIP39Language = "french"
	BIP39Italian            BIP39Language = "italian"
	BIP39Japanese           BIP39Language = "japanese"
	BIP39Korean             BIP39Language = "korean"
	BIP39Portuguese         BIP39Language = "portuguese"
	BIP39Spanish            BIP39Language = "spanish"
)

var bip39WordLists = map[BIP39Language][]string{
	BIP39English:            wordlists.English,
	BIP39ChineseSimplified:  wordlists.ChineseSimplified,
	BIP39ChineseTraditional: wordlists.ChineseTraditional,
	BIP39Czech:              wordlists.Czech,
	BIP39French:             wordlists.French,
	BIP39Italian:            wordlists.Italian,
	BIP39Japanese:           wordlists.Japanese,
	BIP39Korean:             wordlists.Korean,
	BIP39Spanish:            wordlists.Spanish,
}

var bip39WordIndexes = buildBIP39WordIndexes()

func IsSupportedBIP39Language(language BIP39Language) bool {
	if language == BIP39Portuguese {
		return true
	}
	_, exists := bip39WordLists[language]
	return exists
}

func SupportedBIP39Languages() []BIP39Language {
	return []BIP39Language{
		BIP39English,
		BIP39ChineseSimplified,
		BIP39ChineseTraditional,
		BIP39Czech,
		BIP39French,
		BIP39Italian,
		BIP39Japanese,
		BIP39Korean,
		BIP39Portuguese,
		BIP39Spanish,
	}
}

func generateMnemonicForLanguage(wordCount int, language BIP39Language) (string, error) {
	entropyBytes, ok := map[int]int{12: 16, 15: 20, 18: 24, 21: 28, 24: 32}[wordCount]
	if !ok {
		return "", fmt.Errorf("unsupported BIP39 word count: %d", wordCount)
	}
	entropy := make([]byte, entropyBytes)
	if _, err := rand.Read(entropy); err != nil {
		return "", fmt.Errorf("generate BIP39 entropy: %w", err)
	}
	defer clear(entropy)
	return mnemonicFromEntropy(entropy, language)
}

func mnemonicFromEntropy(entropy []byte, language BIP39Language) (string, error) {
	if language == BIP39Portuguese {
		provider := &bitcoinbip39.Bip39{Language: bitcoinbip39.Portuguese}
		if err := provider.LoadLanguages(); err != nil {
			return "", err
		}
		return provider.EntropyToMnemonic(entropy)
	}
	wordList, exists := bip39WordLists[language]
	if !exists || len(wordList) != 2048 {
		return "", fmt.Errorf("unsupported BIP39 language: %s", language)
	}
	entropyBits := len(entropy) * 8
	if entropyBits < 128 || entropyBits > 256 || entropyBits%32 != 0 {
		return "", fmt.Errorf("BIP39 entropy must contain 128-256 bits in 32-bit increments")
	}
	checksumBits := entropyBits / 32
	checksum := sha256.Sum256(entropy)
	wordCount := (entropyBits + checksumBits) / 11
	words := make([]string, wordCount)
	for wordIndex := range words {
		value := 0
		for bitOffset := 0; bitOffset < 11; bitOffset++ {
			bitIndex := wordIndex*11 + bitOffset
			value <<= 1
			if bitIndex < entropyBits {
				value |= int(bitAt(entropy, bitIndex))
			} else {
				value |= int(bitAt(checksum[:], bitIndex-entropyBits))
			}
		}
		words[wordIndex] = wordList[value]
	}
	separator := " "
	if language == BIP39Japanese {
		separator = "\u3000"
	}
	return strings.Join(words, separator), nil
}

func ValidateBIP39Mnemonic(mnemonic string, language BIP39Language) error {
	if len(mnemonic) == 0 || len(mnemonic) > maxMnemonicInputLength {
		return fmt.Errorf("BIP39 mnemonic size is outside policy")
	}
	if language == BIP39Portuguese {
		provider := &bitcoinbip39.Bip39{Language: bitcoinbip39.Portuguese}
		if err := provider.LoadLanguages(); err != nil {
			return err
		}
		if !provider.ValidateMnemonic(norm.NFKD.String(mnemonic)) {
			return fmt.Errorf("invalid Portuguese BIP39 mnemonic")
		}
		return nil
	}
	wordList, exists := bip39WordLists[language]
	if !exists || len(wordList) != 2048 {
		return fmt.Errorf("unsupported BIP39 language: %s", language)
	}
	normalized := norm.NFKD.String(mnemonic)
	if len(normalized) > maxMnemonicInputLength {
		return fmt.Errorf("normalized BIP39 mnemonic size is outside policy")
	}
	words := strings.Fields(normalized)
	wordCount := len(words)
	if wordCount != 12 && wordCount != 15 && wordCount != 18 && wordCount != 21 && wordCount != 24 {
		return fmt.Errorf("unsupported BIP39 word count: %d", wordCount)
	}
	indexes := bip39WordIndexes[language]
	totalBits := wordCount * 11
	entropyBits := totalBits * 32 / 33
	checksumBits := totalBits - entropyBits
	bits := make([]byte, totalBits)
	for wordPosition, word := range words {
		index, exists := indexes[word]
		if !exists {
			return fmt.Errorf("word %d is not in the %s BIP39 wordlist", wordPosition+1, language)
		}
		for bitOffset := 0; bitOffset < 11; bitOffset++ {
			bits[wordPosition*11+bitOffset] = byte((index >> (10 - bitOffset)) & 1)
		}
	}
	entropy := make([]byte, entropyBits/8)
	defer clear(entropy)
	for bitIndex := 0; bitIndex < entropyBits; bitIndex++ {
		if bits[bitIndex] == 1 {
			entropy[bitIndex/8] |= 1 << (7 - (bitIndex % 8))
		}
	}
	checksum := sha256.Sum256(entropy)
	for bitIndex := 0; bitIndex < checksumBits; bitIndex++ {
		if bits[entropyBits+bitIndex] != bitAt(checksum[:], bitIndex) {
			return fmt.Errorf("invalid BIP39 checksum")
		}
	}
	return nil
}

func DetectBIP39Language(mnemonic string) (BIP39Language, error) {
	var detected BIP39Language
	matches := 0
	for _, language := range SupportedBIP39Languages() {
		if ValidateBIP39Mnemonic(mnemonic, language) == nil {
			detected = language
			matches++
		}
	}
	if matches == 0 {
		return "", fmt.Errorf("mnemonic does not match a supported BIP39 language")
	}
	if matches > 1 {
		return "", fmt.Errorf("mnemonic is ambiguous across BIP39 languages")
	}
	return detected, nil
}

func normalizeBIP39Passphrase(passphrase string) string {
	return norm.NFKD.String(passphrase)
}

func bip39Seed(mnemonic, passphrase string, language BIP39Language) ([]byte, error) {
	if len(passphrase) > maxBIP39PassphraseLength {
		return nil, fmt.Errorf("BIP39 passphrase size is outside policy")
	}
	if err := ValidateBIP39Mnemonic(mnemonic, language); err != nil {
		return nil, err
	}
	normalizedMnemonic := norm.NFKD.String(strings.Join(strings.Fields(mnemonic), " "))
	normalizedPassphrase := normalizeBIP39Passphrase(passphrase)
	if len(normalizedPassphrase) > maxBIP39PassphraseLength {
		return nil, fmt.Errorf("normalized BIP39 passphrase size is outside policy")
	}
	return pbkdf2.Key([]byte(normalizedMnemonic), []byte("mnemonic"+normalizedPassphrase), 2048, 64, sha512.New), nil
}

type DerivationPath struct {
	components []uint32
}

func ParseDerivationPath(value string) (DerivationPath, error) {
	if len(value) == 0 || len(value) > maxDerivationPathLength {
		return DerivationPath{}, fmt.Errorf("derivation path size is outside policy")
	}
	parts := strings.Split(value, "/")
	if len(parts) < 3 || len(parts) > 11 || parts[0] != "m" {
		return DerivationPath{}, fmt.Errorf("invalid derivation path")
	}
	components := make([]uint32, 0, len(parts)-1)
	for position, part := range parts[1:] {
		if part == "" {
			return DerivationPath{}, fmt.Errorf("empty derivation component")
		}
		hardened := strings.HasSuffix(part, "'") || strings.HasSuffix(strings.ToLower(part), "h")
		if hardened {
			part = part[:len(part)-1]
		}
		index, err := strconv.ParseUint(part, 10, 31)
		if err != nil {
			return DerivationPath{}, fmt.Errorf("invalid derivation component %d: %w", position+1, err)
		}
		component := uint32(index)
		if hardened {
			component += bip32.FirstHardenedChild
		}
		components = append(components, component)
	}
	if len(components) < 2 || components[0] != bip32.FirstHardenedChild+44 || components[1] != bip32.FirstHardenedChild+60 {
		return DerivationPath{}, fmt.Errorf("derivation path is not an EVM BIP44 path")
	}
	return DerivationPath{components: components}, nil
}

func (path DerivationPath) String() string {
	parts := make([]string, 1, len(path.components)+1)
	parts[0] = "m"
	for _, component := range path.components {
		hardened := component >= bip32.FirstHardenedChild
		if hardened {
			component -= bip32.FirstHardenedChild
		}
		part := strconv.FormatUint(uint64(component), 10)
		if hardened {
			part += "'"
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, "/")
}

func (path DerivationPath) Components() []uint32 {
	return append([]uint32(nil), path.components...)
}

func derivationMetadataForPath(path DerivationPath, language BIP39Language) DerivationMetadata {
	metadata := DerivationMetadata{Scheme: "bip44", Path: path.String(), Language: string(language)}
	if len(path.components) > 2 {
		metadata.AccountIndex = path.components[2] & (bip32.FirstHardenedChild - 1)
	}
	if len(path.components) > 3 {
		metadata.ChangeIndex = path.components[3] & (bip32.FirstHardenedChild - 1)
	}
	if len(path.components) > 4 {
		metadata.AddressIndex = path.components[4] & (bip32.FirstHardenedChild - 1)
	}
	return metadata
}

func deriveEVMAccount(mnemonic, passphrase string, language BIP39Language, path DerivationPath) ([]byte, string, error) {
	seed, err := bip39Seed(mnemonic, passphrase, language)
	if err != nil {
		return nil, "", err
	}
	defer clear(seed)
	key, err := bip32.NewMasterKey(seed)
	if err != nil {
		return nil, "", err
	}
	for _, component := range path.components {
		key, err = key.NewChildKey(component)
		if err != nil {
			return nil, "", err
		}
	}
	privateKey := append([]byte(nil), key.Key...)
	ecdsaKey, err := crypto.ToECDSA(privateKey)
	if err != nil {
		clear(privateKey)
		return nil, "", err
	}
	address := crypto.PubkeyToAddress(ecdsaKey.PublicKey).Hex()
	ecdsaKey.D.SetInt64(0)
	return privateKey, address, nil
}

func buildBIP39WordIndexes() map[BIP39Language]map[string]int {
	indexes := make(map[BIP39Language]map[string]int, len(bip39WordLists))
	for language, words := range bip39WordLists {
		languageIndexes := make(map[string]int, len(words))
		for index, word := range words {
			languageIndexes[norm.NFKD.String(word)] = index
		}
		indexes[language] = languageIndexes
	}
	return indexes
}

func bitAt(value []byte, bitIndex int) byte {
	return value[bitIndex/8] >> (7 - (bitIndex % 8)) & 1
}
