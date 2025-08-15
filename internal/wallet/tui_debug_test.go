package wallet

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/keystore"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// TestTUIDebugWithLogging adiciona logging detalhado para debug
func TestTUIDebugWithLogging(t *testing.T) {
	fmt.Printf("=== TUI Debug with Detailed Logging ===\n")

	// Initialize crypto service
	cfg := CreateMockConfig()
	InitCryptoService(cfg)
	fmt.Printf("✅ CryptoService initialized\n")

	// Setup
	tempDir := t.TempDir()
	keystoreDir := filepath.Join(tempDir, "keystore")
	err := os.MkdirAll(keystoreDir, 0755)
	require.NoError(t, err)

	ks := keystore.NewKeyStore(keystoreDir, keystore.StandardScryptN, keystore.StandardScryptP)

	repo := &MockWalletRepository{}
	repo.On("AddWallet", mock.AnythingOfType("*wallet.Wallet")).Return(nil)
	repo.On("GetAllWallets").Return([]Wallet{}, nil)

	service := NewWalletService(repo, ks)

	// Test with different keystore files to see which ones work
	testFiles := []struct {
		name     string
		path     string
		password string
	}{
		{
			name:     "Complex Password",
			path:     "testdata/keystores/real_keystore_v3_complex_password.json",
			password: "ComplexPassword123!@#",
		},
		{
			name:     "Simple Password",
			path:     "testdata/keystores/real_keystore_v3_simple_password.json",
			password: "password123",
		},
	}

	for _, test := range testFiles {
		fmt.Printf("\n--- Testing %s ---\n", test.name)
		fmt.Printf("File: %s\n", test.path)
		fmt.Printf("Password: %s\n", test.password)

		// Check if file exists
		if _, err := os.Stat(test.path); os.IsNotExist(err) {
			fmt.Printf("❌ File not found, skipping\n")
			continue
		}

		// Read and validate file
		keyJSON, err := os.ReadFile(test.path)
		if err != nil {
			fmt.Printf("❌ Error reading file: %v\n", err)
			continue
		}
		fmt.Printf("✅ File read: %d bytes\n", len(keyJSON))

		// Test direct keystore decryption first
		key, err := keystore.DecryptKey(keyJSON, test.password)
		if err != nil {
			fmt.Printf("❌ Direct keystore decryption failed: %v\n", err)
			continue
		}
		fmt.Printf("✅ Direct keystore decryption successful: %s\n", key.Address.Hex())

		// Now test our import function
		walletDetails, err := service.ImportWalletFromKeystore(
			fmt.Sprintf("Test %s", test.name),
			test.path,
			test.password,
		)

		if err != nil {
			fmt.Printf("❌ ImportWalletFromKeystore failed: %v\n", err)

			// Detailed error analysis
			if keystoreErr, ok := err.(*KeystoreImportError); ok {
				fmt.Printf("   Error Type: %v\n", keystoreErr.Type)
				fmt.Printf("   Message: %s\n", keystoreErr.Message)
				fmt.Printf("   Field: %s\n", keystoreErr.Field)
				if keystoreErr.Cause != nil {
					fmt.Printf("   Cause: %v\n", keystoreErr.Cause)
				}
			}
		} else {
			fmt.Printf("✅ ImportWalletFromKeystore successful!\n")
			fmt.Printf("   Address: %s\n", walletDetails.Wallet.Address)
			fmt.Printf("   Path: %s\n", walletDetails.Wallet.KeyStorePath)
		}
	}
}

// TestCreateValidKeystoreFiles cria arquivos keystore válidos para teste
func TestCreateValidKeystoreFiles(t *testing.T) {
	t.Skip("Only run when needed to create test files")

	tempDir := t.TempDir()
	ks := keystore.NewKeyStore(tempDir, keystore.StandardScryptN, keystore.StandardScryptP)

	// Create keystore with simple password
	account1, err := ks.NewAccount("password123")
	require.NoError(t, err)

	keyJSON1, err := os.ReadFile(account1.URL.Path)
	require.NoError(t, err)

	// Save to test it directory
	err = os.WriteFile("testdata/keystores/real_keystore_v3_simple_password.json", keyJSON1, 0644)
	require.NoError(t, err)

	fmt.Printf("Created simple password keystore:\n")
	fmt.Printf("Address: %s\n", account1.Address.Hex())
	fmt.Printf("Password: password123\n")
	fmt.Printf("Content: %s\n", string(keyJSON1))

	// Test decryption
	key1, err := keystore.DecryptKey(keyJSON1, "password123")
	require.NoError(t, err)
	require.Equal(t, account1.Address, key1.Address)

	fmt.Printf("✅ Simple password keystore verified\n")
}

// TestDebugSpecificError testa cenários específicos que podem causar o erro
func TestDebugSpecificError(t *testing.T) {
	fmt.Printf("=== Debug Specific Error Scenarios ===\n")

	// Test 1: CryptoService not initialized
	fmt.Printf("\n--- Test 1: Without CryptoService ---\n")
	// Don't initialize CryptoService

	tempDir := t.TempDir()
	keystoreDir := filepath.Join(tempDir, "keystore")
	err := os.MkdirAll(keystoreDir, 0755)
	require.NoError(t, err)

	ks := keystore.NewKeyStore(keystoreDir, keystore.StandardScryptN, keystore.StandardScryptP)

	repo := &MockWalletRepository{}
	repo.On("AddWallet", mock.AnythingOfType("*wallet.Wallet")).Return(nil)

	service := NewWalletService(repo, ks)

	keystorePath := "testdata/keystores/real_keystore_v3_complex_password.json"
	password := "ComplexPassword123!@#"

	_, err = service.ImportWalletFromKeystore("Test Without Crypto", keystorePath, password)
	if err != nil {
		fmt.Printf("❌ Expected error without CryptoService: %v\n", err)
		if keystoreErr, ok := err.(*KeystoreImportError); ok {
			fmt.Printf("   Error Type: %v\n", keystoreErr.Type)
			if keystoreErr.Cause != nil {
				fmt.Printf("   Cause: %v\n", keystoreErr.Cause)
			}
		}
	} else {
		fmt.Printf("❌ Unexpected success without CryptoService\n")
	}

	// Test 2: With CryptoService initialized
	fmt.Printf("\n--- Test 2: With CryptoService ---\n")
	cfg := CreateMockConfig()
	InitCryptoService(cfg)
	fmt.Printf("✅ CryptoService initialized\n")

	_, err = service.ImportWalletFromKeystore("Test With Crypto", keystorePath, password)
	if err != nil {
		fmt.Printf("❌ Unexpected error with CryptoService: %v\n", err)
	} else {
		fmt.Printf("✅ Success with CryptoService\n")
	}
}
