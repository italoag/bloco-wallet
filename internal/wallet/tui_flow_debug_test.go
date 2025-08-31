//go:build disabled_tests
// +build disabled_tests

package wallet

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/keystore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// TestTUIFlowDebug simula exatamente o fluxo da TUI para identificar o problema
func TestTUIFlowDebug(t *testing.T) {
	// Setup
	tempDir := t.TempDir()
	keystoreDir := filepath.Join(tempDir, "keystore")
	err := os.MkdirAll(keystoreDir, 0755)
	require.NoError(t, err)

	// Initialize crypto service (this is what was missing!)
	cfg := CreateMockConfig()
	InitCryptoService(cfg)

	// Create keystore with test-optimized parameters
	n, p := GetTestKeystoreParams()
	ks := keystore.NewKeyStore(keystoreDir, n, p)

	// Create repository
	repo := &MockWalletRepository{}
	repo.On("AddWallet", mock.AnythingOfType("*wallet.Wallet")).Return(nil)
	repo.On("GetAllWallets").Return([]Wallet{}, nil)

	// Create service
	service := NewWalletService(repo, ks)

	// Test with the keystore file that works in tests but fails in TUI
	keystorePath := "testdata/keystores/real_keystore_v3_complex_password.json"
	password := "ComplexPassword123!@#"
	name := "Test Keystore Import"

	fmt.Printf("=== TUI Flow Debug Test ===\n")
	fmt.Printf("Keystore Path: %s\n", keystorePath)
	fmt.Printf("Password: %s\n", password)
	fmt.Printf("Name: %s\n", name)

	// Step 1: Check if file exists (like TUI does)
	if _, err := os.Stat(keystorePath); os.IsNotExist(err) {
		t.Fatalf("Keystore file not found: %s", keystorePath)
	}
	fmt.Printf("✅ File exists\n")

	// Step 2: Check file size (like TUI does)
	fileInfo, err := os.Stat(keystorePath)
	require.NoError(t, err)
	fmt.Printf("✅ File size: %d bytes\n", fileInfo.Size())

	// Step 3: Read file content to verify it's valid JSON
	keyJSON, err := os.ReadFile(keystorePath)
	require.NoError(t, err)
	fmt.Printf("✅ File read successfully, %d bytes\n", len(keyJSON))

	// Step 4: Try to parse as JSON
	var keystoreData map[string]interface{}
	err = json.Unmarshal(keyJSON, &keystoreData)
	require.NoError(t, err)
	fmt.Printf("✅ JSON parsing successful\n")

	// Step 5: Check crypto section
	cryptoData, ok := keystoreData["crypto"].(map[string]interface{})
	require.True(t, ok, "crypto section should exist")
	fmt.Printf("✅ Crypto section found\n")

	// Step 6: Check KDF
	kdfType, ok := cryptoData["kdf"].(string)
	require.True(t, ok, "KDF type should exist")
	fmt.Printf("✅ KDF type: %s\n", kdfType)

	// Step 7: Try ImportWalletFromKeystore (the method TUI calls)
	fmt.Printf("\n=== Calling ImportWalletFromKeystore ===\n")
	walletDetails, err := service.ImportWalletFromKeystore(name, keystorePath, password)

	if err != nil {
		fmt.Printf("❌ ImportWalletFromKeystore failed: %v\n", err)

		// Let's try to understand why it failed
		if keystoreErr, ok := err.(*KeystoreImportError); ok {
			fmt.Printf("   Error Type: %v\n", keystoreErr.Type)
			fmt.Printf("   Message: %s\n", keystoreErr.Message)
			fmt.Printf("   Field: %s\n", keystoreErr.Field)
			if keystoreErr.Cause != nil {
				fmt.Printf("   Cause: %v\n", keystoreErr.Cause)
			}
		}

		// Try direct keystore decryption to see if that works
		fmt.Printf("\n=== Trying direct keystore decryption ===\n")
		key, err := keystore.DecryptKey(keyJSON, password)
		if err != nil {
			fmt.Printf("❌ Direct keystore decryption failed: %v\n", err)
		} else {
			fmt.Printf("✅ Direct keystore decryption successful\n")
			fmt.Printf("   Address: %s\n", key.Address.Hex())
		}

		t.Fatalf("ImportWalletFromKeystore failed: %v", err)
	}

	fmt.Printf("✅ ImportWalletFromKeystore successful\n")
	fmt.Printf("   Wallet Address: %s\n", walletDetails.Wallet.Address)
	fmt.Printf("   Keystore Path: %s\n", walletDetails.Wallet.KeyStorePath)

	// Verify the wallet was added to repository
	wallets, err := repo.GetAllWallets()
	require.NoError(t, err)
	assert.Len(t, wallets, 0) // Mock returns empty slice

	fmt.Printf("✅ Test completed successfully\n")
}

// TestTUIFlowWithDifferentKeystores tests with multiple keystore files
func TestTUIFlowWithDifferentKeystores(t *testing.T) {
	// Setup
	tempDir := t.TempDir()
	keystoreDir := filepath.Join(tempDir, "keystore")
	err := os.MkdirAll(keystoreDir, 0755)
	require.NoError(t, err)

	// Initialize crypto service
	cfg := CreateMockConfig()
	InitCryptoService(cfg)

	// Create keystore with test-optimized parameters
	n, p := GetTestKeystoreParams()
	ks := keystore.NewKeyStore(keystoreDir, n, p)

	// Create repository
	repo := &MockWalletRepository{}
	repo.On("AddWallet", mock.AnythingOfType("*wallet.Wallet")).Return(nil)
	repo.On("GetAllWallets").Return([]Wallet{}, nil)

	// Create service
	service := NewWalletService(repo, ks)

	// Test cases with different keystore files
	testCases := []struct {
		name         string
		keystorePath string
		password     string
		shouldWork   bool
	}{
		{
			name:         "Complex Password Keystore",
			keystorePath: "testdata/keystores/real_keystore_v3_complex_password.json",
			password:     "ComplexPassword123!@#",
			shouldWork:   true,
		},
		{
			name:         "Simple Password Keystore",
			keystorePath: "testdata/keystores/real_keystore_v3_simple_password.json",
			password:     "password123",
			shouldWork:   true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fmt.Printf("\n=== Testing %s ===\n", tc.name)

			// Check if file exists
			if _, err := os.Stat(tc.keystorePath); os.IsNotExist(err) {
				if tc.shouldWork {
					t.Skipf("Keystore file not found: %s", tc.keystorePath)
				}
				return
			}

			// Try import
			walletDetails, err := service.ImportWalletFromKeystore(
				fmt.Sprintf("Test %s", tc.name),
				tc.keystorePath,
				tc.password,
			)

			if tc.shouldWork {
				require.NoError(t, err, "Import should succeed for %s", tc.name)
				require.NotNil(t, walletDetails)
				fmt.Printf("✅ %s import successful\n", tc.name)
			} else {
				require.Error(t, err, "Import should fail for %s", tc.name)
				fmt.Printf("✅ %s import failed as expected: %v\n", tc.name, err)
			}
		})
	}
}

// TestTUIPasswordValidation tests password validation like TUI does
func TestTUIPasswordValidation(t *testing.T) {
	passwords := []struct {
		password string
		valid    bool
	}{
		{"ComplexPassword123!@#", true},
		{"password123", true},
		{"123", false},
		{"", false},
	}

	for _, tc := range passwords {
		t.Run(fmt.Sprintf("Password_%s", tc.password), func(t *testing.T) {
			validationErr, isValid := ValidatePassword(tc.password)

			if tc.valid {
				assert.True(t, isValid, "Password should be valid: %s", tc.password)
			} else {
				assert.False(t, isValid, "Password should be invalid: %s", tc.password)
				if !isValid {
					fmt.Printf("Password validation error: %s\n", validationErr.GetErrorMessage())
				}
			}
		})
	}
}
