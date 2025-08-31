package ui

import (
	"testing"

	"blocowallet/internal/wallet"
	"blocowallet/pkg/config"
	"blocowallet/pkg/localization"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWalletDetailsViewConsistency(t *testing.T) {
	// Initialize localization for tests
	cfg := &config.Config{
		Language:  "en",
		LocaleDir: "../../pkg/localization/locales",
	}
	err := localization.InitLocalization(cfg)
	require.NoError(t, err)

	tests := []struct {
		name             string
		importMethod     wallet.ImportMethod
		hasMnemonic      bool
		expectedMethod   string
		expectedMnemonic string
	}{
		{
			name:             "Keystore import shows correct method and mnemonic message",
			importMethod:     wallet.ImportMethodKeystore,
			hasMnemonic:      false,
			expectedMethod:   "Keystore File",
			expectedMnemonic: "Mnemonic not available - imported from keystore file",
		},
		{
			name:             "Private key import shows correct method and mnemonic message",
			importMethod:     wallet.ImportMethodPrivateKey,
			hasMnemonic:      false,
			expectedMethod:   "Private Key",
			expectedMnemonic: "Mnemonic not available (imported via private key)",
		},
		{
			name:             "Mnemonic import shows correct method",
			importMethod:     wallet.ImportMethodMnemonic,
			hasMnemonic:      true,
			expectedMethod:   "Mnemonic Phrase",
			expectedMnemonic: "test mnemonic phrase",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock wallet details
			mockWallet := &wallet.Wallet{
				Name:         "Test Wallet",
				Address:      "0x1234567890123456789012345678901234567890",
				ImportMethod: string(tt.importMethod),
			}

			var mnemonicPtr *string
			if tt.hasMnemonic {
				mnemonic := "test mnemonic phrase"
				mnemonicPtr = &mnemonic
			}

			walletDetails := &wallet.WalletDetails{
				Wallet:       mockWallet,
				Mnemonic:     mnemonicPtr,
				ImportMethod: tt.importMethod,
				HasMnemonic:  tt.hasMnemonic,
			}

			// Create CLI model with wallet details
			model := &CLIModel{
				walletDetails: walletDetails,
			}

			// Get the wallet details view
			view := model.viewWalletDetails()

			// Verify the view contains expected information
			assert.Contains(t, view, tt.expectedMethod, "View should contain correct import method")

			if !tt.hasMnemonic {
				assert.Contains(t, view, tt.expectedMnemonic, "View should contain correct mnemonic message for non-mnemonic imports")
			}

			// Verify consistent terminology
			if tt.importMethod == wallet.ImportMethodKeystore {
				assert.Contains(t, view, "Keystore", "View should use consistent keystore terminology")
				assert.NotContains(t, view, "private key)", "View should not show private key message for keystore imports")
			}
		})
	}
}

func TestWalletDetailsViewMnemonicHandling(t *testing.T) {
	// Initialize localization for tests
	cfg := &config.Config{
		Language:  "en",
		LocaleDir: "../../pkg/localization/locales",
	}
	err := localization.InitLocalization(cfg)
	require.NoError(t, err)

	t.Run("Keystore import without mnemonic shows appropriate message", func(t *testing.T) {
		mockWallet := &wallet.Wallet{
			Name:         "Keystore Wallet",
			Address:      "0x1234567890123456789012345678901234567890",
			ImportMethod: string(wallet.ImportMethodKeystore),
		}

		walletDetails := &wallet.WalletDetails{
			Wallet:       mockWallet,
			Mnemonic:     nil,
			ImportMethod: wallet.ImportMethodKeystore,
			HasMnemonic:  false,
		}

		model := &CLIModel{
			walletDetails: walletDetails,
		}

		view := model.viewWalletDetails()

		// Should show keystore-specific message
		assert.Contains(t, view, "imported from keystore file", "Should show keystore-specific mnemonic message")
		assert.NotContains(t, view, "imported via private key", "Should not show private key message")
	})

	t.Run("Private key import without mnemonic shows appropriate message", func(t *testing.T) {
		mockWallet := &wallet.Wallet{
			Name:         "Private Key Wallet",
			Address:      "0x1234567890123456789012345678901234567890",
			ImportMethod: string(wallet.ImportMethodPrivateKey),
		}

		walletDetails := &wallet.WalletDetails{
			Wallet:       mockWallet,
			Mnemonic:     nil,
			ImportMethod: wallet.ImportMethodPrivateKey,
			HasMnemonic:  false,
		}

		model := &CLIModel{
			walletDetails: walletDetails,
		}

		view := model.viewWalletDetails()

		// Should show private key-specific message
		assert.Contains(t, view, "imported via private key", "Should show private key-specific mnemonic message")
		assert.NotContains(t, view, "imported from keystore file", "Should not show keystore message")
	})

	t.Run("Mnemonic import with mnemonic shows actual mnemonic", func(t *testing.T) {
		testMnemonic := "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"

		mockWallet := &wallet.Wallet{
			Name:         "Mnemonic Wallet",
			Address:      "0x1234567890123456789012345678901234567890",
			ImportMethod: string(wallet.ImportMethodMnemonic),
		}

		walletDetails := &wallet.WalletDetails{
			Wallet:       mockWallet,
			Mnemonic:     &testMnemonic,
			ImportMethod: wallet.ImportMethodMnemonic,
			HasMnemonic:  true,
		}

		model := &CLIModel{
			walletDetails: walletDetails,
		}

		view := model.viewWalletDetails()

		// Should show actual mnemonic
		assert.Contains(t, view, testMnemonic, "Should show actual mnemonic phrase")
		assert.NotContains(t, view, "not available", "Should not show 'not available' message when mnemonic exists")
	})
}

func TestWalletDetailsViewImportMethodDisplay(t *testing.T) {
	// Initialize localization for tests
	cfg := &config.Config{
		Language:  "en",
		LocaleDir: "../../pkg/localization/locales",
	}
	err := localization.InitLocalization(cfg)
	require.NoError(t, err)

	t.Run("Import method labels are displayed correctly", func(t *testing.T) {
		testCases := []struct {
			importMethod wallet.ImportMethod
			expectedText string
		}{
			{wallet.ImportMethodKeystore, "Keystore"},
			{wallet.ImportMethodPrivateKey, "Private Key"},
			{wallet.ImportMethodMnemonic, "Mnemonic"},
		}

		for _, tc := range testCases {
			mockWallet := &wallet.Wallet{
				Name:         "Test Wallet",
				Address:      "0x1234567890123456789012345678901234567890",
				ImportMethod: string(tc.importMethod),
			}

			walletDetails := &wallet.WalletDetails{
				Wallet:       mockWallet,
				ImportMethod: tc.importMethod,
				HasMnemonic:  tc.importMethod == wallet.ImportMethodMnemonic,
			}

			model := &CLIModel{
				walletDetails: walletDetails,
			}

			view := model.viewWalletDetails()
			assert.Contains(t, view, tc.expectedText, "View should contain correct import method label for %s", tc.importMethod)
		}
	})
}
