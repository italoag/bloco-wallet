package localization

import (
	"testing"
)

func TestGetKeystoreErrorMessage(t *testing.T) {
	// Initialize localization for testing
	InitCryptoMessagesForTesting()

	tests := []struct {
		name     string
		key      string
		expected string
	}{
		{
			name:     "File not found error",
			key:      "keystore_file_not_found",
			expected: "Keystore file not found at the specified path",
		},
		{
			name:     "Invalid JSON error",
			key:      "keystore_invalid_json",
			expected: "The file contains an invalid JSON",
		},
		{
			name:     "Invalid keystore structure error",
			key:      "keystore_invalid_structure",
			expected: "The file isn't a valid KeyStoreV3 file",
		},
		{
			name:     "Invalid version error",
			key:      "keystore_invalid_version",
			expected: "Invalid keystore version, expected version 3",
		},
		{
			name:     "Incorrect password error",
			key:      "keystore_incorrect_password",
			expected: "Incorrect password for the keystore file",
		},
		{
			name:     "Corrupted file error",
			key:      "keystore_corrupted_file",
			expected: "Keystore file is corrupted or damaged",
		},
		{
			name:     "Address mismatch error",
			key:      "keystore_address_mismatch",
			expected: "Address in keystore doesn't match the derived private key",
		},
		{
			name:     "Missing required fields error",
			key:      "keystore_missing_fields",
			expected: "Keystore file is missing required fields",
		},
		{
			name:     "Invalid address error",
			key:      "keystore_invalid_address",
			expected: "Invalid Ethereum address format in keystore",
		},
		{
			name:     "Unknown error key",
			key:      "unknown_error",
			expected: "unknown_error", // Should return the key itself if not found
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetKeystoreErrorMessage(tt.key)
			if result != tt.expected {
				t.Errorf("GetKeystoreErrorMessage(%q) = %q, want %q", tt.key, result, tt.expected)
			}
		})
	}
}

func TestFormatKeystoreErrorWithField(t *testing.T) {
	// Initialize localization for testing
	InitCryptoMessagesForTesting()

	tests := []struct {
		name     string
		key      string
		field    string
		expected string
	}{
		{
			name:     "Error without field",
			key:      "keystore_file_not_found",
			field:    "",
			expected: "Keystore file not found at the specified path",
		},
		{
			name:     "Error with field",
			key:      "keystore_missing_fields",
			field:    "crypto.cipher",
			expected: "Keystore file is missing required fields (crypto.cipher)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FormatKeystoreErrorWithField(tt.key, tt.field)
			if result != tt.expected {
				t.Errorf("FormatKeystoreErrorWithField(%q, %q) = %q, want %q", tt.key, tt.field, result, tt.expected)
			}
		})
	}
}
