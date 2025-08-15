package wallet

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/keystore"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
)

// TestSimpleKeystoreImport testa a importação simples do keystore
func TestSimpleKeystoreImport(t *testing.T) {
	// Initialize crypto service with mock config
	mockConfig := CreateMockConfig()
	InitCryptoService(mockConfig)

	keystorePath := "testdata/keystores/real_keystore_v3_complex_password.json"
	password := "ComplexPassword123!@#"

	fmt.Printf("=== Simple Keystore Import Debug ===\n")
	fmt.Printf("File: %s\n", keystorePath)
	fmt.Printf("Password: %s\n", password)

	// Step 1: Test direct keystore decryption (this should work)
	keyJSON, err := os.ReadFile(keystorePath)
	require.NoError(t, err)

	key, err := keystore.DecryptKey(keyJSON, password)
	require.NoError(t, err)
	fmt.Printf("✅ Direct keystore decryption successful\n")
	fmt.Printf("   Address: %s\n", key.Address.Hex())
	fmt.Printf("   Private Key Length: %d bytes\n", len(crypto.FromECDSA(key.PrivateKey)))
	fmt.Printf("   Private Key (hex): %s\n", hex.EncodeToString(crypto.FromECDSA(key.PrivateKey)))

	// Step 2: Test our Universal KDF approach step by step
	fmt.Printf("\n=== Testing Universal KDF Step by Step ===\n")

	// Parse keystore
	var keystoreData map[string]interface{}
	err = json.Unmarshal(keyJSON, &keystoreData)
	require.NoError(t, err)

	// Initialize services
	kdfService := NewUniversalKDFService()
	compatAnalyzer := NewKDFCompatibilityAnalyzer()

	// Compatibility analysis
	compatReport := compatAnalyzer.AnalyzeKeyStoreCompatibility(keystoreData)
	require.True(t, compatReport.Compatible)
	fmt.Printf("✅ Compatibility check passed\n")

	// Validate keystore structure
	validator := &KeystoreValidator{}
	keystoreValidated, err := validator.ValidateKeystoreV3(keyJSON)
	require.NoError(t, err)
	fmt.Printf("✅ Keystore validation passed\n")

	// Prepare crypto params
	cryptoData := keystoreData["crypto"].(map[string]interface{})
	kdfParams := cryptoData["kdfparams"].(map[string]interface{})

	cryptoParams := &CryptoParams{
		KDF:          keystoreValidated.Crypto.KDF,
		KDFParams:    kdfParams,
		Cipher:       keystoreValidated.Crypto.Cipher,
		CipherText:   keystoreValidated.Crypto.CipherText,
		CipherParams: map[string]interface{}{"iv": keystoreValidated.Crypto.CipherParams.IV},
		MAC:          keystoreValidated.Crypto.MAC,
	}

	// Derive key using Universal KDF
	derivedKey, err := kdfService.DeriveKey(password, cryptoParams)
	require.NoError(t, err)
	fmt.Printf("✅ Key derivation successful\n")
	fmt.Printf("   Derived Key Length: %d bytes\n", len(derivedKey))
	fmt.Printf("   Derived Key (hex): %s\n", hex.EncodeToString(derivedKey))

	// Verify MAC
	enhancedService := NewEnhancedKeyStoreService()
	err = enhancedService.verifyMAC(derivedKey, cryptoParams)
	require.NoError(t, err)
	fmt.Printf("✅ MAC verification passed\n")

	// Decrypt private key
	privateKeyBytes, err := enhancedService.decryptPrivateKey(derivedKey, cryptoParams)
	require.NoError(t, err)
	fmt.Printf("✅ Private key decryption successful\n")
	fmt.Printf("   Decrypted Key Length: %d bytes\n", len(privateKeyBytes))
	fmt.Printf("   Decrypted Key (hex): %s\n", hex.EncodeToString(privateKeyBytes))

	// Convert to ECDSA
	privateKey, err := crypto.ToECDSA(privateKeyBytes)
	require.NoError(t, err)
	fmt.Printf("✅ ECDSA conversion successful\n")

	// Compare with direct decryption
	directPrivateKeyBytes := crypto.FromECDSA(key.PrivateKey)
	fmt.Printf("\n=== Comparison ===\n")
	fmt.Printf("Direct decryption key:    %s\n", hex.EncodeToString(directPrivateKeyBytes))
	fmt.Printf("Universal KDF key:        %s\n", hex.EncodeToString(privateKeyBytes))
	fmt.Printf("Keys match: %v\n", hex.EncodeToString(directPrivateKeyBytes) == hex.EncodeToString(privateKeyBytes))

	// Compare addresses
	directAddress := key.Address.Hex()
	universalAddress := crypto.PubkeyToAddress(privateKey.PublicKey).Hex()
	fmt.Printf("Direct address:    %s\n", directAddress)
	fmt.Printf("Universal address: %s\n", universalAddress)
	fmt.Printf("Addresses match: %v\n", directAddress == universalAddress)

	require.Equal(t, directAddress, universalAddress, "Addresses should match")
	fmt.Printf("\n✅ All tests passed!\n")
}
