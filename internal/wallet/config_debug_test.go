package wallet

import (
	"blocowallet/pkg/config"
	"blocowallet/pkg/localization"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestConfigInitialization testa a inicialização da configuração como main.go
func TestConfigInitialization(t *testing.T) {
	fmt.Printf("=== Config Initialization Test ===\n")

	// Step 1: Get home directory like main.go
	homeDir, err := os.UserHomeDir()
	require.NoError(t, err)
	fmt.Printf("✅ Home directory: %s\n", homeDir)

	// Step 2: Create app directory like main.go
	appDir := filepath.Join(homeDir, ".wallets-config-test")
	defer os.RemoveAll(appDir) // Clean up

	// Create directory if it doesn't exist
	if _, err := os.Stat(appDir); os.IsNotExist(err) {
		err := os.MkdirAll(appDir, os.ModePerm)
		require.NoError(t, err)
	}
	fmt.Printf("✅ App directory created: %s\n", appDir)

	// Step 3: Load configuration like main.go
	cfg, err := config.LoadConfig(appDir)
	require.NoError(t, err)
	fmt.Printf("✅ Configuration loaded successfully\n")
	fmt.Printf("   AppDir: %s\n", cfg.AppDir)
	fmt.Printf("   WalletsDir: %s\n", cfg.WalletsDir)
	fmt.Printf("   DatabasePath: %s\n", cfg.DatabasePath)

	// Step 4: Initialize localization like main.go
	err = localization.InitLocalization(cfg)
	require.NoError(t, err)
	fmt.Printf("✅ Localization initialized successfully\n")

	// Step 5: Test some localization labels
	testLabels := []string{
		"error_crypto_service_not_initialized",
		"error_empty_password",
		"version",
		"welcome_message",
	}

	for _, label := range testLabels {
		value := localization.Labels[label]
		fmt.Printf("   Label '%s': '%s'\n", label, value)
	}

	// Step 6: Initialize crypto service like main.go
	InitCryptoService(cfg)
	fmt.Printf("✅ CryptoService initialized successfully\n")

	// Step 7: Test crypto service functions
	testMnemonic := "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"
	testPassword := "test123"

	encrypted, err := EncryptMnemonic(testMnemonic, testPassword)
	require.NoError(t, err)
	fmt.Printf("✅ Mnemonic encryption successful\n")

	decrypted, err := DecryptMnemonic(encrypted, testPassword)
	require.NoError(t, err)
	require.Equal(t, testMnemonic, decrypted)
	fmt.Printf("✅ Mnemonic decryption successful\n")

	fmt.Printf("\n✅ All initialization steps completed successfully!\n")
}

// TestConfigWithRealPaths testa com os caminhos reais da aplicação
func TestConfigWithRealPaths(t *testing.T) {
	fmt.Printf("=== Config with Real Paths Test ===\n")

	homeDir, err := os.UserHomeDir()
	require.NoError(t, err)

	// Use the real app directory (but don't modify anything)
	appDir := filepath.Join(homeDir, ".wallets")
	fmt.Printf("Real app directory: %s\n", appDir)

	// Check if it exists
	if _, err := os.Stat(appDir); os.IsNotExist(err) {
		fmt.Printf("⚠️ Real app directory doesn't exist, creating it\n")
		err := os.MkdirAll(appDir, os.ModePerm)
		require.NoError(t, err)
	} else {
		fmt.Printf("✅ Real app directory exists\n")
	}

	// Try to load real config
	cfg, err := config.LoadConfig(appDir)
	if err != nil {
		fmt.Printf("❌ Failed to load real config: %v\n", err)
		t.Fatalf("Config loading failed: %v", err)
	}
	fmt.Printf("✅ Real configuration loaded\n")

	// Try to initialize localization
	err = localization.InitLocalization(cfg)
	if err != nil {
		fmt.Printf("❌ Failed to initialize localization: %v\n", err)
		t.Fatalf("Localization failed: %v", err)
	}
	fmt.Printf("✅ Real localization initialized\n")

	// Try to initialize crypto service
	InitCryptoService(cfg)
	fmt.Printf("✅ Real CryptoService initialized\n")

	// Test if it works
	testMnemonic := "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"
	testPassword := "test123"

	_, err = EncryptMnemonic(testMnemonic, testPassword)
	if err != nil {
		fmt.Printf("❌ Mnemonic encryption failed: %v\n", err)
		t.Fatalf("Encryption failed: %v", err)
	}
	fmt.Printf("✅ Real mnemonic encryption works\n")

	fmt.Printf("\n✅ Real application setup works correctly!\n")
}

// TestErrorMessages testa as mensagens de erro específicas
func TestErrorMessages(t *testing.T) {
	fmt.Printf("=== Error Messages Test ===\n")

	// Test without initialization
	fmt.Printf("\n--- Without CryptoService ---\n")
	_, err := EncryptMnemonic("test", "password")
	require.Error(t, err)
	fmt.Printf("Error without init: %v\n", err)

	// Test with initialization
	fmt.Printf("\n--- With CryptoService ---\n")
	cfg := CreateMockConfig()
	InitCryptoService(cfg)

	encrypted, err := EncryptMnemonic("test", "password")
	require.NoError(t, err)
	fmt.Printf("Success with init: encrypted length = %d\n", len(encrypted))
}
