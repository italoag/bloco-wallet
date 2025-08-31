package ui

import (
	"blocowallet/internal/wallet"
	"blocowallet/pkg/localization"
	"testing"
)

func TestDetermineWalletType(t *testing.T) {
	// Initialize localization for testing
	localization.Labels = map[string]string{
		"imported_mnemonic":    "Mnemonic",
		"imported_private_key": "Private Key",
		"imported_keystore":    "Keystore (Private Key)",
	}

	tests := []struct {
		name         string
		wallet       wallet.Wallet
		expectedType string
		description  string
	}{
		{
			name: "Keystore import method",
			wallet: wallet.Wallet{
				ImportMethod: string(wallet.ImportMethodKeystore),
				Mnemonic:     nil,
			},
			expectedType: "Keystore (Private Key)",
			description:  "Should return keystore type for keystore import method",
		},
		{
			name: "Mnemonic import method",
			wallet: wallet.Wallet{
				ImportMethod: string(wallet.ImportMethodMnemonic),
				Mnemonic:     stringPtr("test mnemonic"),
			},
			expectedType: "Mnemonic",
			description:  "Should return mnemonic type for mnemonic import method",
		},
		{
			name: "Private key import method",
			wallet: wallet.Wallet{
				ImportMethod: string(wallet.ImportMethodPrivateKey),
				Mnemonic:     nil,
			},
			expectedType: "Private Key",
			description:  "Should return private key type for private key import method",
		},
		{
			name: "Fallback with mnemonic",
			wallet: wallet.Wallet{
				ImportMethod: "", // Empty import method for backward compatibility
				Mnemonic:     stringPtr("test mnemonic"),
			},
			expectedType: "Mnemonic",
			description:  "Should fallback to mnemonic type when import method is empty but mnemonic exists",
		},
		{
			name: "Fallback without mnemonic",
			wallet: wallet.Wallet{
				ImportMethod: "", // Empty import method for backward compatibility
				Mnemonic:     nil,
			},
			expectedType: "Private Key",
			description:  "Should fallback to private key type when import method is empty and no mnemonic",
		},
		{
			name: "Unknown import method with mnemonic",
			wallet: wallet.Wallet{
				ImportMethod: "unknown_method",
				Mnemonic:     stringPtr("test mnemonic"),
			},
			expectedType: "Mnemonic",
			description:  "Should fallback to mnemonic type for unknown import method when mnemonic exists",
		},
		{
			name: "Unknown import method without mnemonic",
			wallet: wallet.Wallet{
				ImportMethod: "unknown_method",
				Mnemonic:     nil,
			},
			expectedType: "Private Key",
			description:  "Should fallback to private key type for unknown import method when no mnemonic",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := determineWalletType(tt.wallet)
			if result != tt.expectedType {
				t.Errorf("determineWalletType() = %v, want %v. %s", result, tt.expectedType, tt.description)
			}
		})
	}
}

// Helper function to create string pointers
func stringPtr(s string) *string {
	return &s
}
