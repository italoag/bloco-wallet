package wallet_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"blocowallet/internal/storage"
	"blocowallet/internal/wallet"
	"blocowallet/pkg/config"

	"github.com/ethereum/go-ethereum/accounts/keystore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestImportWalletFromKeystoreV3WithProgress(t *testing.T) {
	// Setup test environment
	tempDir := t.TempDir()
	keystoreDir := filepath.Join(tempDir, "keystore")
	err := os.MkdirAll(keystoreDir, 0755)
	require.NoError(t, err)

	// Initialize crypto service
	cfg := &config.Config{
		AppDir:       tempDir,
		WalletsDir:   keystoreDir,
		DatabasePath: filepath.Join(tempDir, "wallets.db"),
		Database: config.DatabaseConfig{
			Type: "sqlite",
			DSN:  ":memory:",
		},
		Security: config.SecurityConfig{
			Argon2Time:    1,
			Argon2Memory:  64 * 1024,
			Argon2Threads: 4,
			Argon2KeyLen:  32,
			SaltLength:    16,
		},
	}
	wallet.InitCryptoService(cfg)

	// Create a test repository and service
	repo, err := storage.NewWalletRepository(cfg)
	require.NoError(t, err)
	defer func() {
		if err := repo.Close(); err != nil {
			t.Logf("Failed to close repository: %v", err)
		}
	}()

	ks := keystore.NewKeyStore(keystoreDir, keystore.LightScryptN, keystore.LightScryptP)
	service := wallet.NewWalletService(repo, ks)

	// Use a test keystore file
	keystorePath := filepath.Join("testdata", "keystores", "real_keystore_v3_standard.json")
	password := "testpassword"

	t.Run("Progress updates are sent during import", func(t *testing.T) {
		// Create progress channel
		progressChan := make(chan wallet.ImportProgress, 20)
		var progressUpdates []wallet.ImportProgress

		// Start goroutine to collect progress updates
		done := make(chan bool)
		go func() {
			for progress := range progressChan {
				progressUpdates = append(progressUpdates, progress)
			}
			done <- true
		}()

		// Perform import with progress tracking
		walletDetails, err := service.ImportWalletFromKeystoreV3WithProgress(
			"test-wallet-progress",
			keystorePath,
			password,
			progressChan,
		)

		// Close the progress channel and wait for a collection to complete
		close(progressChan)
		<-done

		// Verify import was successful
		require.NoError(t, err)
		require.NotNil(t, walletDetails)
		assert.Equal(t, wallet.ImportMethodKeystore, walletDetails.ImportMethod)
		assert.False(t, walletDetails.HasMnemonic)
		assert.Nil(t, walletDetails.Mnemonic)

		// Verify progress updates were sent
		assert.Greater(t, len(progressUpdates), 5, "Should have received multiple progress updates")

		// Verify progress percentages increase over time
		var lastPercentage float64 = -1
		for i, progress := range progressUpdates {
			t.Logf("Progress update %d: %.1f%% - %s", i, progress.Percentage, progress.CurrentFile)

			// Percentage should generally increase (allow for some tolerance)
			if progress.Percentage > 0 {
				assert.GreaterOrEqual(t, progress.Percentage, lastPercentage,
					"Progress percentage should not decrease")
				lastPercentage = progress.Percentage
			}

			// Verify basic progress structure
			assert.Equal(t, keystorePath, progress.CurrentFile)
			assert.Equal(t, 1, progress.TotalFiles)
			assert.LessOrEqual(t, progress.ProcessedFiles, 1)
			assert.False(t, progress.PendingPassword)
		}

		// Verify final progress shows completion
		finalProgress := progressUpdates[len(progressUpdates)-1]
		assert.Equal(t, 100.0, finalProgress.Percentage)
		assert.Equal(t, 1, finalProgress.ProcessedFiles)
	})

	t.Run("Import works without progress channel", func(t *testing.T) {
		// Test that import still works when no progress channel is provided
		// Use a different keystore file to avoid source hash conflicts
		keystorePath2 := filepath.Join("testdata", "keystores", "real_keystore_v3_light.json")
		walletDetails, err := service.ImportWalletFromKeystoreV3WithProgress(
			"test-wallet-no-progress-2",
			keystorePath2,
			password,
			nil, // No progress channel
		)

		require.NoError(t, err)
		require.NotNil(t, walletDetails)
		assert.Equal(t, wallet.ImportMethodKeystore, walletDetails.ImportMethod)
		assert.False(t, walletDetails.HasMnemonic)
		assert.Nil(t, walletDetails.Mnemonic)
	})

	t.Run("Progress updates don't block on slow consumer", func(t *testing.T) {
		// Create a small buffer channel to test non-blocking behavior
		progressChan := make(chan wallet.ImportProgress, 1)

		// Start import without consuming progress updates (to test timeout)
		// Use a different keystore file to avoid source hash conflicts
		keystorePath3 := filepath.Join("testdata", "keystores", "real_keystore_v3_pbkdf2.json")
		start := time.Now()
		walletDetails, err := service.ImportWalletFromKeystoreV3WithProgress(
			"test-wallet-timeout-3",
			keystorePath3,
			password,
			progressChan,
		)
		elapsed := time.Since(start)

		// Import should complete quickly even with blocked progress channel
		require.NoError(t, err)
		require.NotNil(t, walletDetails)
		assert.Less(t, elapsed, 10*time.Second, "Import should not be significantly delayed by blocked progress channel")

		// Clean up channel
		close(progressChan)
		// Drain any remaining messages
		for range progressChan {
		}
	})

	t.Run("Backward compatibility with original function", func(t *testing.T) {
		// Test that the original function still works
		// Use a different keystore file to avoid source hash conflicts
		keystorePath4 := filepath.Join("testdata", "keystores", "real_keystore_v3_empty_password.json")
		walletDetails, err := service.ImportWalletFromKeystoreV3(
			"test-wallet-compat-4",
			keystorePath4,
			"", // Empty password for this keystore
		)

		require.NoError(t, err)
		require.NotNil(t, walletDetails)
		assert.Equal(t, wallet.ImportMethodKeystore, walletDetails.ImportMethod)
		assert.False(t, walletDetails.HasMnemonic)
		assert.Nil(t, walletDetails.Mnemonic)
	})
}
