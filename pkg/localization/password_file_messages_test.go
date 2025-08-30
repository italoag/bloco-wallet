package localization

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAddPasswordFileMessages(t *testing.T) {
	// Initialize Labels map
	Labels = make(map[string]string)

	// Set current language to English for testing
	SetCurrentLanguage("en")

	// Add password file messages
	AddPasswordFileMessages()

	// Test that English messages are added
	expectedMessages := []string{
		"password_file_not_found",
		"password_file_unreadable",
		"password_file_empty",
		"password_file_invalid",
		"password_file_oversized",
		"password_file_corrupted",
		"password_file_unknown_error",
		"password_file_recovery_not_found",
		"password_file_recovery_unreadable",
		"password_file_recovery_empty",
		"password_file_recovery_invalid",
		"password_file_recovery_oversized",
		"password_file_recovery_corrupted",
		"password_file_recovery_general",
	}

	for _, key := range expectedMessages {
		t.Run("should have message for "+key, func(t *testing.T) {
			message, exists := Labels[key]
			assert.True(t, exists, "Message key %s should exist", key)
			assert.NotEmpty(t, message, "Message for %s should not be empty", key)
		})
	}
}

func TestAddPasswordFileMessages_Portuguese(t *testing.T) {
	// Initialize Labels map
	Labels = make(map[string]string)

	// Set current language to Portuguese
	SetCurrentLanguage("pt")

	// Add password file messages
	AddPasswordFileMessages()

	// Test specific Portuguese messages
	tests := []struct {
		key      string
		expected string
	}{
		{"password_file_not_found", "Arquivo de senha não encontrado"},
		{"password_file_empty", "Arquivo de senha está vazio"},
		{"password_file_invalid", "Arquivo de senha inválido"},
	}

	for _, test := range tests {
		t.Run("should have Portuguese message for "+test.key, func(t *testing.T) {
			message, exists := Labels[test.key]
			assert.True(t, exists, "Message key %s should exist", test.key)
			assert.Equal(t, test.expected, message)
		})
	}
}

func TestAddPasswordFileMessages_Spanish(t *testing.T) {
	// Initialize Labels map
	Labels = make(map[string]string)

	// Set current language to Spanish
	SetCurrentLanguage("es")

	// Add password file messages
	AddPasswordFileMessages()

	// Test specific Spanish messages
	tests := []struct {
		key      string
		expected string
	}{
		{"password_file_not_found", "Archivo de contraseña no encontrado"},
		{"password_file_empty", "El archivo de contraseña está vacío"},
		{"password_file_invalid", "Archivo de contraseña inválido"},
	}

	for _, test := range tests {
		t.Run("should have Spanish message for "+test.key, func(t *testing.T) {
			message, exists := Labels[test.key]
			assert.True(t, exists, "Message key %s should exist", test.key)
			assert.Equal(t, test.expected, message)
		})
	}
}

func TestGetPasswordFileErrorMessage(t *testing.T) {
	// Initialize Labels map with test data
	Labels = map[string]string{
		"password_file_not_found": "Password file not found",
		"password_file_empty":     "Password file is empty",
	}

	t.Run("should return message for existing key", func(t *testing.T) {
		message := GetPasswordFileErrorMessage("password_file_not_found")
		assert.Equal(t, "Password file not found", message)
	})

	t.Run("should return key for non-existing key", func(t *testing.T) {
		message := GetPasswordFileErrorMessage("non_existing_key")
		assert.Equal(t, "non_existing_key", message)
	})
}

func TestFormatPasswordFileErrorWithFile(t *testing.T) {
	// Initialize Labels map with test data
	Labels = map[string]string{
		"password_file_not_found": "Password file not found",
	}

	t.Run("should format message with file name", func(t *testing.T) {
		message := FormatPasswordFileErrorWithFile("password_file_not_found", "wallet.pwd")
		expected := "Password file not found (wallet.pwd)"
		assert.Equal(t, expected, message)
	})

	t.Run("should return message without file name when file is empty", func(t *testing.T) {
		message := FormatPasswordFileErrorWithFile("password_file_not_found", "")
		expected := "Password file not found"
		assert.Equal(t, expected, message)
	})
}
