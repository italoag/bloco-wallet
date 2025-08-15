package wallet

import (
	"encoding/json"
	"testing"
)

func TestUniversalKDFService_ScryptParameterConversion(t *testing.T) {
	service := NewUniversalKDFService()
	
	testCases := []struct {
		name        string
		kdfParams   map[string]interface{}
		expectError bool
		description string
	}{
		{
			name: "Standard Geth parameters (float64)",
			kdfParams: map[string]interface{}{
				"n":     262144.0,
				"r":     8.0,
				"p":     1.0,
				"dklen": 32.0,
				"salt":  "2103ac29920d71da29f15d75b4a16dbe95cfd7ff8faea1056c33131d846e3097",
			},
			expectError: false,
			description: "Should handle standard Geth float64 parameters",
		},
		{
			name: "Mixed types (string, int, float)",
			kdfParams: map[string]interface{}{
				"n":     "262144", // string
				"r":     8,        // int
				"p":     1.0,      // float64
				"dklen": 32,       // int
				"salt":  "2103ac29920d71da29f15b4a16dbe95cfd7ff8faea1056c33131d846e3097",
			},
			expectError: false,
			description: "Should handle mixed parameter types",
		},
		{
			name: "Alternative parameter names (uppercase)",
			kdfParams: map[string]interface{}{
				"N":     262144,
				"R":     8,
				"P":     1,
				"dkLen": 32, // camelCase
				"Salt":  "2103ac29920d71da29f15b4a16dbe95cfd7ff8faea1056c33131d846e3097",
			},
			expectError: false,
			description: "Should handle alternative parameter names",
		},
		{
			name: "JSON Number type",
			kdfParams: map[string]interface{}{
				"n":     json.Number("262144"),
				"r":     json.Number("8"),
				"p":     json.Number("1"),
				"dklen": json.Number("32"),
				"salt":  "2103ac29920d71da29f15b4a16dbe95cfd7ff8faea1056c33131d846e3097",
			},
			expectError: false,
			description: "Should handle json.Number types",
		},
		{
			name: "Invalid N parameter (not power of 2)",
			kdfParams: map[string]interface{}{
				"n":     262143, // Not power of 2
				"r":     8,
				"p":     1,
				"dklen": 32,
				"salt":  "2103ac29920d71da29f15b75b4a16dbe95cfd7ff8faea1056c33131d846e3097",
			},
			expectError: true,
			description: "Should reject N parameter that's not power of 2",
		},
		{
			name: "N parameter too low",
			kdfParams: map[string]interface{}{
				"n":     512, // Too low
				"r":     8,
				"p":     1,
				"dklen": 32,
				"salt":  "2103ac29920d71da29f15b75b4a16dbe95cfd7ff8faea1056c33131d846e3097",
			},
			expectError: true,
			description: "Should reject N parameter below minimum",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cryptoParams := &CryptoParams{
				KDF:       "scrypt",
				KDFParams: tc.kdfParams,
			}

			_, err := service.DeriveKey("testpassword", cryptoParams)
			
			if tc.expectError && err == nil {
				t.Errorf("Expected error for %s, but got none", tc.description)
			}
			
			if !tc.expectError && err != nil {
				t.Errorf("Unexpected error for %s: %v", tc.description, err)
			}
		})
	}
}

func TestUniversalKDFService_KDFNameNormalization(t *testing.T) {
	service := NewUniversalKDFService()
	
	testCases := []struct {
		inputKDF     string
		expectedKDF  string
		expectError  bool
	}{
		{"scrypt", "scrypt", false},
		{"Scrypt", "scrypt", false},
		{"SCRYPT", "scrypt", false},
		{"pbkdf2", "pbkdf2", false},
		{"PBKDF2", "pbkdf2", false},
		{"pbkdf2-sha256", "pbkdf2-sha256", false},
		{"pbkdf2-sha512", "pbkdf2-sha512", false},
		{"pbkdf2_sha256", "pbkdf2-sha256", false},
		{"unsupported", "unsupported", true},
	}

	for _, tc := range testCases {
		t.Run(tc.inputKDF, func(t *testing.T) {
			normalized := service.normalizeKDFName(tc.inputKDF)
			
			if normalized != tc.expectedKDF {
				t.Errorf("Expected normalized KDF %s, got %s", tc.expectedKDF, normalized)
			}

			// Test if the normalized KDF is supported
			_, exists := service.supportedKDFs[normalized]
			if !tc.expectError && !exists {
				t.Errorf("Normalized KDF %s should be supported", normalized)
			}
			if tc.expectError && exists {
				t.Errorf("KDF %s should not be supported", normalized)
			}
		})
	}
}

func TestUniversalKDFService_SaltFormatConversion(t *testing.T) {
	handler := &ScryptHandler{}
	
	testCases := []struct {
		name        string
		saltValue   interface{}
		expectError bool
		description string
	}{
		{
			name:        "Hex string salt",
			saltValue:   "2103ac29920d71da29f15d75b4a16dbe95cfd7ff8faea1056c33131d846e3097",
			expectError: false,
			description: "Should handle hex string salt",
		},
		{
			name:        "Direct string salt",
			saltValue:   "direct_string_salt",
			expectError: false,
			description: "Should handle direct string salt",
		},
		{
			name:        "Byte array salt",
			saltValue:   []interface{}{33.0, 3.0, 172.0, 41.0, 146.0, 13.0, 113.0, 218.0},
			expectError: false,
			description: "Should handle byte array salt",
		},
		{
			name:        "Case variation (Salt vs salt)",
			saltValue:   "2103ac29920d71da29f15d75b4a16dbe95cfd7ff8faea1056c33131d846e3097",
			expectError: false,
			description: "Should handle case variations in salt parameter name",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			params := map[string]interface{}{
				"n":     262144,
				"r":     8,
				"p":     1,
				"dklen": 32,
				"salt":  tc.saltValue,
			}

			_, err := handler.convertToBytes(tc.saltValue)
			
			if tc.expectError && err == nil {
				t.Errorf("Expected error for %s, but got none", tc.description)
			}
			
			if !tc.expectError && err != nil {
				t.Errorf("Unexpected error for %s: %v", tc.description, err)
			}

			// Test parameter extraction with different names
			if !tc.expectError {
				saltNames := []string{"salt", "Salt", "SALT"}
				for _, name := range saltNames {
					testParams := make(map[string]interface{})
					for k, v := range params {
						testParams[k] = v
					}
					delete(testParams, "salt")
					testParams[name] = tc.saltValue

					_, err := handler.getSaltParam(testParams)
					if err != nil {
						t.Errorf("Failed to extract salt with name %s: %v", name, err)
					}
				}
			}
		})
	}
}

func TestUniversalKDFService_PBKDF2Support(t *testing.T) {
	service := NewUniversalKDFService()
	
	testCases := []struct {
		name        string
		kdfParams   map[string]interface{}
		expectError bool
		description string
	}{
		{
			name: "Standard PBKDF2 parameters",
			kdfParams: map[string]interface{}{
				"c":     262144,
				"dklen": 32,
				"prf":   "hmac-sha256",
				"salt":  "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789",
			},
			expectError: false,
			description: "Should handle standard PBKDF2 parameters",
		},
		{
			name: "PBKDF2 with SHA512",
			kdfParams: map[string]interface{}{
				"c":     262144,
				"dklen": 32,
				"prf":   "hmac-sha512",
				"salt":  "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789",
			},
			expectError: false,
			description: "Should handle PBKDF2 with SHA512",
		},
		{
			name: "PBKDF2 with alternative parameter names",
			kdfParams: map[string]interface{}{
				"iter":   262144, // Alternative to 'c'
				"keylen": 32,     // Alternative to 'dklen'
				"prf":    "hmac-sha256",
				"salt":   "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789",
			},
			expectError: false,
			description: "Should handle alternative PBKDF2 parameter names",
		},
		{
			name: "PBKDF2 with low iterations",
			kdfParams: map[string]interface{}{
				"c":     500, // Too low
				"dklen": 32,
				"prf":   "hmac-sha256",
				"salt":  "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789",
			},
			expectError: true,
			description: "Should reject PBKDF2 with too few iterations",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cryptoParams := &CryptoParams{
				KDF:       "pbkdf2",
				KDFParams: tc.kdfParams,
			}

			_, err := service.DeriveKey("testpassword", cryptoParams)
			
			if tc.expectError && err == nil {
				t.Errorf("Expected error for %s, but got none", tc.description)
			}
			
			if !tc.expectError && err != nil {
				t.Errorf("Unexpected error for %s: %v", tc.description, err)
			}
		})
	}
}

func TestKDFCompatibilityAnalyzer(t *testing.T) {
	analyzer := NewKDFCompatibilityAnalyzer()
	
	testCases := []struct {
		name           string
		keystoreData   map[string]interface{}
		expectCompatible bool
		expectedSecurity string
		description    string
	}{
		{
			name: "Geth standard keystore",
			keystoreData: map[string]interface{}{
				"version": 3.0,
				"crypto": map[string]interface{}{
					"kdf": "scrypt",
					"kdfparams": map[string]interface{}{
						"n":     262144.0,
						"r":     8.0,
						"p":     1.0,
						"dklen": 32.0,
						"salt":  "2103ac29920d71da29f15d75b4a16dbe95cfd7ff8faea1056c33131d846e3097",
					},
				},
			},
			expectCompatible: true,
			expectedSecurity: "Medium",
			description:      "Should be compatible with standard Geth keystore",
		},
		{
			name: "Trust Wallet mobile keystore",
			keystoreData: map[string]interface{}{
				"version": 3.0,
				"crypto": map[string]interface{}{
					"kdf": "Scrypt", // Case variation
					"kdfparams": map[string]interface{}{
						"N":     "32768", // String number, uppercase
						"R":     8,       // Mixed types
						"P":     1.0,     // Float
						"dkLen": 32,      // Different capitalization
						"Salt":  "1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
					},
				},
			},
			expectCompatible: true,
			expectedSecurity: "Medium", // 32768 * 8 * 1 = 262144 operations = Medium
			description:      "Should be compatible with Trust Wallet variations",
		},
		{
			name: "High security configuration",
			keystoreData: map[string]interface{}{
				"version": 3.0,
				"crypto": map[string]interface{}{
					"kdf": "scrypt",
					"kdfparams": map[string]interface{}{
						"n":     1048576, // High security
						"r":     8,
						"p":     2,       // Increased parallelization
						"dklen": 32,
						"salt":  "0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20",
					},
				},
			},
			expectCompatible: true,
			expectedSecurity: "High",
			description:      "Should handle high security configurations",
		},
		{
			name: "Unsupported KDF",
			keystoreData: map[string]interface{}{
				"version": 3.0,
				"crypto": map[string]interface{}{
					"kdf": "argon2",
					"kdfparams": map[string]interface{}{
						"time":   3,
						"memory": 65536,
						"threads": 4,
						"keylen": 32,
						"salt":   "somesalt",
					},
				},
			},
			expectCompatible: false,
			expectedSecurity: "",
			description:      "Should reject unsupported KDF",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			report := analyzer.AnalyzeKeyStoreCompatibility(tc.keystoreData)
			
			if report.Compatible != tc.expectCompatible {
				t.Errorf("Expected compatibility %v, got %v for %s", 
					tc.expectCompatible, report.Compatible, tc.description)
			}
			
			if tc.expectCompatible && report.SecurityLevel != tc.expectedSecurity {
				t.Errorf("Expected security level %s, got %s for %s", 
					tc.expectedSecurity, report.SecurityLevel, tc.description)
			}
			
			// Check that we have meaningful feedback
			if tc.expectCompatible {
				if len(report.Suggestions) == 0 {
					t.Errorf("Expected security suggestions for compatible keystore")
				}
			} else {
				if len(report.Issues) == 0 {
					t.Errorf("Expected issues for incompatible keystore")
				}
			}
		})
	}
}

func TestSourceHashGenerator(t *testing.T) {
	generator := &SourceHashGenerator{}
	
	testCases := []struct {
		name        string
		input1      string
		input2      string
		shouldEqual bool
		description string
	}{
		{
			name:        "Same mnemonic should produce same hash",
			input1:      "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about",
			input2:      "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about",
			shouldEqual: true,
			description: "Identical mnemonics should produce identical hashes",
		},
		{
			name:        "Different mnemonics should produce different hashes",
			input1:      "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about",
			input2:      "legal winner thank year wave sausage worth useful legal winner thank yellow",
			shouldEqual: false,
			description: "Different mnemonics should produce different hashes",
		},
		{
			name:        "Same private key should produce same hash",
			input1:      "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			input2:      "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			shouldEqual: true,
			description: "Identical private keys should produce identical hashes",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var hash1, hash2 string
			
			// Test mnemonic hashing
			if tc.name == "Same mnemonic should produce same hash" || tc.name == "Different mnemonics should produce different hashes" {
				hash1 = generator.GenerateFromMnemonic(tc.input1)
				hash2 = generator.GenerateFromMnemonic(tc.input2)
			} else {
				// Test private key hashing
				hash1 = generator.GenerateFromPrivateKey(tc.input1)
				hash2 = generator.GenerateFromPrivateKey(tc.input2)
			}
			
			if tc.shouldEqual && hash1 != hash2 {
				t.Errorf("Expected equal hashes for %s, got %s != %s", tc.description, hash1, hash2)
			}
			
			if !tc.shouldEqual && hash1 == hash2 {
				t.Errorf("Expected different hashes for %s, got %s == %s", tc.description, hash1, hash2)
			}
			
			// Verify hash format (should be 64 character hex string)
			if len(hash1) != 64 {
				t.Errorf("Expected hash length 64, got %d", len(hash1))
			}
		})
	}
}

func TestEnhancedKeyStoreService_Integration(t *testing.T) {
	// This test would require actual keystore files
	// For now, we'll test the service creation and basic functionality
	
	service := NewEnhancedKeyStoreService()
	
	if service == nil {
		t.Fatal("Failed to create EnhancedKeyStoreService")
	}
	
	if service.kdfService == nil {
		t.Fatal("KDF service not initialized")
	}
	
	if service.logger == nil {
		t.Fatal("Logger not initialized")
	}
	
	// Test that the service has the expected KDF handlers
	expectedKDFs := []string{"scrypt", "pbkdf2", "pbkdf2-sha256", "pbkdf2-sha512"}
	for _, kdf := range expectedKDFs {
		if _, exists := service.kdfService.supportedKDFs[kdf]; !exists {
			t.Errorf("Expected KDF %s to be supported", kdf)
		}
	}
}