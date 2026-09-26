package localization

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAddPasswordFileMessages(t *testing.T) {
	// Set current language to English for testing
	useLang(t, "en")

	// Add password file messages

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
			message := Get(key)
			assert.NotEqual(t, key, message)
			assert.NotEmpty(t, message, "Message for %s should not be empty", key)
		})
	}
}

func TestAddPasswordFileMessages_Portuguese(t *testing.T) {
	// Set current language to Portuguese
	useLang(t, "pt")

	// Add password file messages

	// Test specific Portuguese messages
	tests := []struct {
		key      string
		expected string
	}{
		{"password_file_not_found", "Arquivo de senha não encontrado"},
		{"password_file_empty", "Arquivo de senha está vazio"},
		{"password_file_invalid", "Formato de arquivo de senha inválido"},
	}

	for _, test := range tests {
		t.Run("should have Portuguese message for "+test.key, func(t *testing.T) {
			message := Get(test.key)
			assert.NotEqual(t, test.key, message)
			assert.Equal(t, test.expected, message)
		})
	}
}

func TestAddPasswordFileMessages_Spanish(t *testing.T) {
	// Set current language to Spanish
	useLang(t, "es")

	// Add password file messages

	// Test specific Spanish messages
	tests := []struct {
		key      string
		expected string
	}{
		{"password_file_not_found", "Archivo de contraseña no encontrado"},
		{"password_file_empty", "El archivo de contraseña está vacío"},
		{"password_file_invalid", "Formato de archivo de contraseña inválido"},
	}

	for _, test := range tests {
		t.Run("should have Spanish message for "+test.key, func(t *testing.T) {
			message := Get(test.key)
			assert.NotEqual(t, test.key, message)
			assert.Equal(t, test.expected, message)
		})
	}
}

func TestGetPasswordFileErrorMessage(t *testing.T) {
	useLang(t, "en")

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
	useLang(t, "en")

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
