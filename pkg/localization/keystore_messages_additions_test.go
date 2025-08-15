package localization

import (
	"testing"
)

func TestAddKeystoreValidationMessages(t *testing.T) {
	// Initialize the Labels map
	if Labels == nil {
		Labels = make(map[string]string)
	}

	// Test cases for different languages
	testCases := []struct {
		name     string
		language string
		key      string
		expected string
	}{
		{
			name:     "English message",
			language: "en",
			key:      "keystore_file_not_found",
			expected: "Keystore file not found",
		},
		{
			name:     "Portuguese message",
			language: "pt",
			key:      "keystore_file_not_found",
			expected: "Arquivo keystore não encontrado",
		},
		{
			name:     "Spanish message",
			language: "es",
			key:      "keystore_file_not_found",
			expected: "Archivo keystore no encontrado",
		},
		{
			name:     "English recovery suggestion",
			language: "en",
			key:      "keystore_recovery_incorrect_password",
			expected: "Please try again with the correct password",
		},
		{
			name:     "Portuguese recovery suggestion",
			language: "pt",
			key:      "keystore_recovery_incorrect_password",
			expected: "Por favor, tente novamente com a senha correta",
		},
		{
			name:     "Spanish recovery suggestion",
			language: "es",
			key:      "keystore_recovery_incorrect_password",
			expected: "Por favor, intente nuevamente con la contraseña correcta",
		},
	}

	// Run tests for each language
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Set the language
			SetCurrentLanguage(tc.language)

			// Clear the Labels map to ensure we're testing the correct language
			for k := range Labels {
				delete(Labels, k)
			}

			// Add the keystore validation messages
			AddKeystoreValidationMessages()

			// Check if the message is correct
			if Labels[tc.key] != tc.expected {
				t.Errorf("Expected message for key %q in language %q to be %q, but got %q",
					tc.key, tc.language, tc.expected, Labels[tc.key])
			}
		})
	}
}

func TestKeystoreValidationMessagesConsistency(t *testing.T) {
	// Initialize the Labels map
	if Labels == nil {
		Labels = make(map[string]string)
	}

	// Define the keys that should be present in all languages
	requiredKeys := []string{
		"keystore_file_valid",
		"keystore_file_not_found",
		"keystore_access_error",
		"keystore_is_directory",
		"keystore_not_json",
		"keystore_recovery_file_not_found",
		"keystore_recovery_invalid_json",
		"keystore_recovery_invalid_structure",
		"keystore_recovery_incorrect_password",
		"keystore_recovery_general",
	}

	// Test each language for consistency
	languages := []string{"en", "pt", "es"}

	for _, lang := range languages {
		t.Run("Consistency check for "+lang, func(t *testing.T) {
			// Set the language
			SetCurrentLanguage(lang)

			// Clear the Labels map
			for k := range Labels {
				delete(Labels, k)
			}

			// Add the keystore validation messages
			AddKeystoreValidationMessages()

			// Check if all required keys are present
			for _, key := range requiredKeys {
				if _, ok := Labels[key]; !ok {
					t.Errorf("Missing key %q in language %q", key, lang)
				} else if Labels[key] == "" {
					t.Errorf("Empty message for key %q in language %q", key, lang)
				}
			}
		})
	}
}
