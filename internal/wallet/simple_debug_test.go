package wallet

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/keystore"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
)

// TestSimpleKeystoreImport testa a importação simples do keystore
func TestSimpleKeystoreImport(t *testing.T) {
	// Initialize crypto service with mock config
	mockConfig := CreateMockConfig(t)
	InitCryptoService(mockConfig)

	keystorePath := "testdata/keystores/real_keystore_v3_complex_password.json"
	password := "ComplexPassword123!@#"

	// Step 1: Test direct keystore decryption (this should work)
	keyJSON, err := os.ReadFile(keystorePath)
	require.NoError(t, err)

	key, err := keystore.DecryptKey(keyJSON, password)
	require.NoError(t, err)

	// Step 2: Test our Universal KDF approach step by step

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

	// Validate keystore structure
	validator := &KeystoreValidator{}
	keystoreValidated, err := validator.ValidateKeystoreV3(keyJSON)
	require.NoError(t, err)

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

	// Verify MAC
	enhancedService := NewEnhancedKeyStoreService()
	err = enhancedService.verifyMAC(derivedKey, cryptoParams)
	require.NoError(t, err)

	// Decrypt private key
	privateKeyBytes, err := enhancedService.decryptPrivateKey(derivedKey, cryptoParams)
	require.NoError(t, err)

	// Convert to ECDSA
	privateKey, err := crypto.ToECDSA(privateKeyBytes)
	require.NoError(t, err)

	// Compare with direct decryption
	directPrivateKeyBytes := crypto.FromECDSA(key.PrivateKey)
	require.Equal(t, directPrivateKeyBytes, privateKeyBytes)

	// Compare addresses
	directAddress := key.Address.Hex()
	universalAddress := crypto.PubkeyToAddress(privateKey.PublicKey).Hex()
	require.Equal(t, directAddress, universalAddress, "Addresses should match")
}
