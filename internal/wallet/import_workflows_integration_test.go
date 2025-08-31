package wallet_test

import (
	"blocowallet/internal/storage"
	"blocowallet/internal/wallet"
	"blocowallet/pkg/config"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/keystore"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIntegration_MnemonicImportWithDuplicateDetection verifies end-to-end duplicate detection for mnemonic imports
func TestIntegration_MnemonicImportWithDuplicateDetection(t *testing.T) {
	// temp dirs
	tempDir, err := os.MkdirTemp("", "mnemonic-import-integration")
	require.NoError(t, err)
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Logf("Failed to clean up temp directory: %v", err)
		}
	}()

	keystoreDir := filepath.Join(tempDir, "keystore")
	require.NoError(t, os.MkdirAll(keystoreDir, 0o700))

	// real DB file to persist state across repo reopen
	dbPath := filepath.Join(tempDir, "wallets.db")
	cfg := &config.Config{
		AppDir:       tempDir,
		WalletsDir:   keystoreDir,
		DatabasePath: dbPath,
		Database:     config.DatabaseConfig{Type: "sqlite", DSN: dbPath},
		Security:     config.SecurityConfig{Argon2Time: 1, Argon2Memory: 64 * 1024, Argon2Threads: 4, Argon2KeyLen: 32, SaltLength: 16},
	}

	wallet.InitCryptoService(cfg)

	repo, err := storage.NewWalletRepository(cfg)
	require.NoError(t, err)
	defer func() {
		if err := repo.Close(); err != nil {
			t.Logf("Failed to close repository: %v", err)
		}
	}()

	ks := keystore.NewKeyStore(keystoreDir, keystore.LightScryptN, keystore.LightScryptP)
	ws := wallet.NewWalletService(repo, ks)

	mnemonic := "legal winner thank year wave sausage worth useful legal winner thank yellow"

	// First import should succeed
	first, err := ws.ImportWallet("Mnemonic A", mnemonic, "pwd1")
	require.NoError(t, err)
	require.NotNil(t, first)
	assert.Equal(t, wallet.ImportMethodMnemonic, first.ImportMethod)

	// Reopen repo to ensure persistence
	if err := repo.Close(); err != nil {
		t.Logf("Warning: error closing repo: %v", err)
	}
	newRepo, err := storage.NewWalletRepository(cfg)
	require.NoError(t, err)
	defer func() {
		if err := newRepo.Close(); err != nil {
			t.Logf("Warning: error closing newRepo: %v", err)
		}
	}()
	newWS := wallet.NewWalletService(newRepo, ks)

	// Second import with same mnemonic should fail with DuplicateWalletError
	_, err = newWS.ImportWallet("Mnemonic B", mnemonic, "pwd2")
	require.Error(t, err)
	_, ok := err.(*wallet.DuplicateWalletError)
	assert.True(t, ok, "expected DuplicateWalletError")
}

// TestIntegration_PrivateKeyImport_NoMnemonic verifies end-to-end private key import stores no mnemonic
func TestIntegration_PrivateKeyImport_NoMnemonic(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "pk-import-integration")
	require.NoError(t, err)
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Logf("Failed to remove temp directory: %v", err)
		}
	}()

	keystoreDir := filepath.Join(tempDir, "keystore")
	require.NoError(t, os.MkdirAll(keystoreDir, 0o700))

	dbPath := filepath.Join(tempDir, "wallets.db")
	cfg := &config.Config{
		AppDir:       tempDir,
		WalletsDir:   keystoreDir,
		DatabasePath: dbPath,
		Database:     config.DatabaseConfig{Type: "sqlite", DSN: dbPath},
		Security:     config.SecurityConfig{Argon2Time: 1, Argon2Memory: 64 * 1024, Argon2Threads: 4, Argon2KeyLen: 32, SaltLength: 16},
	}
	wallet.InitCryptoService(cfg)

	repo, err := storage.NewWalletRepository(cfg)
	require.NoError(t, err)
	defer func() {
		if err := repo.Close(); err != nil {
			t.Logf("Warning: error closing repo: %v", err)
		}
	}()

	ks := keystore.NewKeyStore(keystoreDir, keystore.LightScryptN, keystore.LightScryptP)
	ws := wallet.NewWalletService(repo, ks)

	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	pkHex := hex.EncodeToString(crypto.FromECDSA(key))

	dets, err := ws.ImportWalletFromPrivateKey("PK Wallet", pkHex, "pwd")
	require.NoError(t, err)
	require.NotNil(t, dets)
	assert.Equal(t, wallet.ImportMethodPrivateKey, dets.ImportMethod)
	assert.False(t, dets.HasMnemonic)
	if dets.Wallet != nil {
		assert.Nil(t, dets.Wallet.Mnemonic)
		assert.Equal(t, string(wallet.ImportMethodPrivateKey), dets.Wallet.ImportMethod)
	}

	// Reopen and check persisted record
	if err := repo.Close(); err != nil {
		t.Logf("Warning: error closing repo: %v", err)
	}
	newRepo, err := storage.NewWalletRepository(cfg)
	require.NoError(t, err)
	defer func() {
		if err := newRepo.Close(); err != nil {
			t.Logf("Warning: error closing newRepo: %v", err)
		}
	}()
	saved, err := newRepo.GetAllWallets()
	require.NoError(t, err)
	require.Len(t, saved, 1)
	assert.Nil(t, saved[0].Mnemonic)
	assert.Equal(t, string(wallet.ImportMethodPrivateKey), saved[0].ImportMethod)
}

// TestIntegration_CoexistSameAddressDifferentMethods verifies that wallets with same address but different import methods can coexist
func TestIntegration_CoexistSameAddressDifferentMethods(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "coexist-integration")
	require.NoError(t, err)
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Logf("Failed to remove temp directory: %v", err)
		}
	}()

	keystoreDir := filepath.Join(tempDir, "keystore")
	// separate source dir to generate external keystore file
	sourceDir := filepath.Join(tempDir, "source")
	require.NoError(t, os.MkdirAll(keystoreDir, 0o700))
	require.NoError(t, os.MkdirAll(sourceDir, 0o700))

	dbPath := filepath.Join(tempDir, "wallets.db")
	cfg := &config.Config{
		AppDir:       tempDir,
		WalletsDir:   keystoreDir,
		DatabasePath: dbPath,
		Database:     config.DatabaseConfig{Type: "sqlite", DSN: dbPath},
		Security:     config.SecurityConfig{Argon2Time: 1, Argon2Memory: 64 * 1024, Argon2Threads: 4, Argon2KeyLen: 32, SaltLength: 16},
	}
	wallet.InitCryptoService(cfg)

	repo, err := storage.NewWalletRepository(cfg)
	require.NoError(t, err)
	defer func() {
		if err := repo.Close(); err != nil {
			t.Logf("Warning: error closing repo: %v", err)
		}
	}()

	ks := keystore.NewKeyStore(keystoreDir, keystore.LightScryptN, keystore.LightScryptP)
	ws := wallet.NewWalletService(repo, ks)

	// Create a source keystore file
	password := "testpassword"
	// Reuse helper from other test file not exported, so recreate here
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	tempKS := keystore.NewKeyStore(sourceDir, keystore.LightScryptN, keystore.LightScryptP)
	acct, err := tempKS.ImportECDSA(key, password)
	require.NoError(t, err)
	sourceKeystorePath := acct.URL.Path

	// Import via keystore (ImportMethodKeystore)
	kd, err := ws.ImportWalletFromKeystoreV3("KS", sourceKeystorePath, password)
	require.NoError(t, err)
	require.NotNil(t, kd)

	addr1 := kd.Wallet.Address
	// Import via private key (same underlying key)
	pkHex := hex.EncodeToString(crypto.FromECDSA(key))
	pd, err := ws.ImportWalletFromPrivateKey("PK", pkHex, "pwd2")
	require.NoError(t, err)
	require.NotNil(t, pd)
	addr2 := pd.Wallet.Address

	assert.Equal(t, addr1, addr2, "addresses should match")
	assert.Equal(t, wallet.ImportMethodKeystore, kd.ImportMethod)
	assert.Equal(t, wallet.ImportMethodPrivateKey, pd.ImportMethod)

	// Verify both persisted
	if err := repo.Close(); err != nil {
		t.Logf("Warning: error closing repo: %v", err)
	}
	newRepo, err := storage.NewWalletRepository(cfg)
	require.NoError(t, err)
	defer func() {
		if err := newRepo.Close(); err != nil {
			t.Logf("Warning: error closing newRepo: %v", err)
		}
	}()
	wallets, err := newRepo.GetAllWallets()
	require.NoError(t, err)
	assert.Len(t, wallets, 2)

	// Ensure we have one per import method
	methods := map[string]int{}
	for _, w := range wallets {
		methods[w.ImportMethod]++
		// Both should share address
		assert.Equal(t, addr1, w.Address)
	}
	assert.Equal(t, 1, methods[string(wallet.ImportMethodKeystore)])
	assert.Equal(t, 1, methods[string(wallet.ImportMethodPrivateKey)])
}
