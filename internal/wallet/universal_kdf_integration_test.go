package wallet

import (
	"encoding/json"
	"testing"
)

// Test keystores for integration testing
var testKeystores = map[string]string{
	"geth_standard": `{
		"address": "3cc7dc4096856c6e8fa5a179ff6acf7cdbb72772",
		"crypto": {
			"cipher": "aes-128-ctr",
			"cipherparams": {
				"iv": "6087dab2f9fdbbfaddc31a909735c1e6"
			},
			"ciphertext": "5318b4d5bcd28de64ee5559e671353e16f075ecae9f99c7a79a38af5f869aa46",
			"kdf": "scrypt",
			"kdfparams": {
				"dklen": 32,
				"n": 262144,
				"p": 1,
				"r": 8,
				"salt": "2103ac29920d71da29f15d75b4a16dbe95cfd7ff8faea1056c33131d846e3097"
			},
			"mac": "517ead924a9d0dc3124507e3393d175ce3ff7c1e96529c6c555ce9e51205e9b2"
		},
		"id": "3198bc9c-6672-5ab3-d995-4942343ae5b6",
		"version": 3
	}`,
	"trust_wallet_mobile": `{
		"address": "d8da6bf26964af9d7eed9e03e53415d37aa96045",
		"crypto": {
			"cipher": "aes-128-ctr",
			"cipherparams": {
				"iv": "1234567890abcdef1234567890abcdef"
			},
			"ciphertext": "fedcba0987654321fedcba0987654321fedcba0987654321fedcba0987654321",
			"kdf": "SCRYPT",
			"kdfparams": {
				"cost": 32768,
				"blocksize": 8,
				"parallel": 1,
				"keylen": 32,
				"SALT": "0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20"
			},
			"mac": "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
		},
		"id": "12345678-90ab-cdef-1234-567890abcdef",
		"version": 3
	}`,
}

func TestUniversalKDF_RealWorldKeystoreCompatibility(t *testing.T) {
	analyzer := NewKDFCompatibilityAnalyzer()

	testCases := []struct {
		name             string
		keystoreJSON     string
		expectCompatible bool
		expectedSecurity string
		description      string
	}{
		{
			name:             "Geth Standard",
			keystoreJSON:     testKeystores["geth_standard"],
			expectCompatible: true,
			expectedSecurity: "Medium",
			description:      "Standard Geth keystore with scrypt",
		},
		{
			name:             "Trust Wallet Mobile",
			keystoreJSON:     testKeystores["trust_wallet_mobile"],
			expectCompatible: true,
			expectedSecurity: "Medium",
			description:      "Trust Wallet mobile with alternative parameter names",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Parse the keystore JSON
			var keystoreData map[string]interface{}
			err := json.Unmarshal([]byte(tc.keystoreJSON), &keystoreData)
			if err != nil {
				t.Fatalf("Failed to parse keystore JSON: %v", err)
			}

			// Analyze compatibility
			report := analyzer.AnalyzeKeyStoreCompatibility(keystoreData)

			if report.Compatible != tc.expectCompatible {
				t.Errorf("Expected compatibility %v, got %v for %s",
					tc.expectCompatible, report.Compatible, tc.description)
			}

			if tc.expectCompatible {
				if report.SecurityLevel != tc.expectedSecurity {
					t.Errorf("Expected security level %s, got %s for %s",
						tc.expectedSecurity, report.SecurityLevel, tc.description)
				}

				// Verify we have meaningful analysis
				if len(report.Suggestions) == 0 {
					t.Errorf("Expected security suggestions for %s", tc.description)
				}

				t.Logf("✅ %s: Compatible (Security: %s)", tc.name, report.SecurityLevel)
				t.Logf("   KDF: %s → %s", report.KDFType, report.NormalizedKDF)
				for _, suggestion := range report.Suggestions {
					t.Logf("   💡 %s", suggestion)
				}
			} else {
				if len(report.Issues) == 0 {
					t.Errorf("Expected issues for incompatible keystore %s", tc.description)
				}

				t.Logf("❌ %s: Incompatible", tc.name)
				for _, issue := range report.Issues {
					t.Logf("   ⚠️ %s", issue)
				}
			}
		})
	}
}
