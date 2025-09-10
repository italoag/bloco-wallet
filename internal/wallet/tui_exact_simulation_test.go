package wallet

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/keystore"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// TestTUIExactSimulation simula exatamente o que acontece na TUI
func TestTUIExactSimulation(t *testing.T) {
	fmt.Printf("=== TUI Exact Simulation Test ===\n")

	// Step 1: Initialize crypto service like main.go does
	cfg := CreateMockConfig()
	InitCryptoService(cfg)
	fmt.Printf("✅ CryptoService initialized\n")

	// Step 2: Setup keystore like TUI does
	tempDir := t.TempDir()
	keystoreDir := filepath.Join(tempDir, "keystore")
	err := os.MkdirAll(keystoreDir, 0755)
	require.NoError(t, err)

	ks := keystore.NewKeyStore(keystoreDir, keystore.StandardScryptN, keystore.StandardScryptP)
	fmt.Printf("✅ KeyStore created at: %s\n", keystoreDir)

	// Step 3: Setup repository like TUI does
	repo := &MockWalletRepository{}
	repo.On("AddWallet", mock.AnythingOfType("*wallet.Wallet")).Return(nil)
	repo.On("GetAllWallets").Return([]Wallet{}, nil)

	// Step 4: Create service like TUI does
	service := NewWalletService(repo, ks)
	fmt.Printf("✅ WalletService created\n")

	// Step 5: Simulate TUI flow exactly
	// This is what happens in updateImportKeystore:
	keystorePath := "testdata/keystores/real_keystore_v3_complex_password.json"

	// Check if file exists (like TUI does)
	if _, err := os.Stat(keystorePath); os.IsNotExist(err) {
		t.Fatalf("Keystore file not found: %s", keystorePath)
	}
	fmt.Printf("✅ File exists: %s\n", keystorePath)

	// Check file size (like TUI does)
	fileInfo, err := os.Stat(keystorePath)
	require.NoError(t, err)
	const maxKeystoreSize = 100 * 1024 // 100KB
	if fileInfo.Size() > maxKeystoreSize {
		t.Fatalf("File too large: %d bytes", fileInfo.Size())
	}
	fmt.Printf("✅ File size OK: %d bytes\n", fileInfo.Size())

	// Store the keystore path in mnemonic field (like TUI does)
	mnemonic := keystorePath // This is what TUI does: m.mnemonic = keystorePath
	fmt.Printf("✅ Keystore path stored in mnemonic field: %s\n", mnemonic)

	// Step 6: Simulate updateImportWalletPassword
	password := strings.TrimSpace("ComplexPassword123!@#")
	fmt.Printf("✅ Password trimmed: '%s'\n", password)

	// Validate password (like TUI does)
	validationErr, isValid := ValidatePassword(password)
	if !isValid {
		t.Fatalf("Password validation failed: %s", validationErr.GetErrorMessage())
	}
	fmt.Printf("✅ Password validation passed\n")

	// Determine import method (like TUI does)
	var name string
	var walletDetails *WalletDetails

	// This is the exact logic from updateImportWalletPassword:
	if mnemonic != "" {
		// Import from a keystore file
		name = "Imported Keystore Wallet"
		keystorePathFromMnemonic := mnemonic // We stored the keystore path in the mnemonic field
		fmt.Printf("✅ Using keystore import method\n")
		fmt.Printf("   Name: %s\n", name)
		fmt.Printf("   Keystore Path: %s\n", keystorePathFromMnemonic)

		// This is the exact call that TUI makes:
		walletDetails, err = service.ImportWalletFromKeystore(name, keystorePathFromMnemonic, password)
	} else {
		t.Fatal("Should not reach here in this test")
	}

	// Step 7: Check a result
	if err != nil {
		fmt.Printf("❌ ImportWalletFromKeystore failed: %v\n", err)

		// Detailed error analysis
		var keystoreErr *KeystoreImportError
		if errors.As(err, &keystoreErr) {
			fmt.Printf("   Error Type: %v\n", keystoreErr.Type)
			fmt.Printf("   Message: %s\n", keystoreErr.Message)
			fmt.Printf("   Field: %s\n", keystoreErr.Field)
			if keystoreErr.Cause != nil {
				fmt.Printf("   Cause: %v\n", keystoreErr.Cause)
			}
		}

		t.Fatalf("Import failed: %v", err)
	}

	fmt.Printf("✅ ImportWalletFromKeystore successful!\n")
	fmt.Printf("   Wallet Address: %s\n", walletDetails.Wallet.Address)
	fmt.Printf("   Keystore Path: %s\n", walletDetails.Wallet.KeyStorePath)
	fmt.Printf("   Import Method: %v\n", walletDetails.ImportMethod)
	fmt.Printf("   Has Mnemonic: %v\n", walletDetails.HasMnemonic)

	if walletDetails.KDFInfo != nil {
		fmt.Printf("   KDF Type: %s\n", walletDetails.KDFInfo.Type)
		fmt.Printf("   KDF Security Level: %s\n", walletDetails.KDFInfo.SecurityLevel)
	}

	fmt.Printf("✅ TUI Exact Simulation completed successfully!\n")
}

// TestTUIWithRealApplication tests with the actual application setup
func TestTUIWithRealApplication(t *testing.T) {
	fmt.Printf("=== TUI Real Application Test ===\n")

	// Use the same setup as the real application
	homeDir, err := os.UserHomeDir()
	require.NoError(t, err)

	appDir := filepath.Join(homeDir, ".wallets")
	keystoreDir := filepath.Join(appDir, "keystore")

	// Ensure directories exist
	err = os.MkdirAll(keystoreDir, 0700)
	require.NoError(t, err)

	fmt.Printf("✅ Using real application directories:\n")
	fmt.Printf("   App Dir: %s\n", appDir)
	fmt.Printf("   Keystore Dir: %s\n", keystoreDir)

	// Load real config (or create mock that matches real config structure)
	cfg := CreateMockConfig()
	cfg.AppDir = appDir
	InitCryptoService(cfg)
	fmt.Printf("✅ CryptoService initialized with real config\n")

	// Create a keystore like the real application
	ks := keystore.NewKeyStore(keystoreDir, keystore.StandardScryptN, keystore.StandardScryptP)

	// Use a mock repository for testing
	repo := &MockWalletRepository{}
	repo.On("AddWallet", mock.AnythingOfType("*wallet.Wallet")).Return(nil)
	repo.On("GetAllWallets").Return([]Wallet{}, nil)

	// Create service
	service := NewWalletService(repo, ks)

	// Test import
	keystorePath := "testdata/keystores/real_keystore_v3_complex_password.json"
	password := "ComplexPassword123!@#"
	name := "Real App Test Wallet"

	walletDetails, err := service.ImportWalletFromKeystore(name, keystorePath, password)
	require.NoError(t, err, "Import should succeed with real app setup")
	require.NotNil(t, walletDetails)

	fmt.Printf("✅ Real application test successful!\n")
	fmt.Printf("   Wallet Address: %s\n", walletDetails.Wallet.Address)
	fmt.Printf("   Keystore Path: %s\n", walletDetails.Wallet.KeyStorePath)
}
