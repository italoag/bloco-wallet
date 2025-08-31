package localization

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestKeystoreImportLabels(t *testing.T) {
	// Initialize Labels map
	Labels = make(map[string]string)

	tests := []struct {
		name     string
		language string
		key      string
		expected string
	}{
		// English labels
		{
			name:     "English imported_keystore label",
			language: "en",
			key:      "imported_keystore",
			expected: "Keystore (Private Key)",
		},
		{
			name:     "English method_keystore label",
			language: "en",
			key:      "method_keystore",
			expected: "Keystore File",
		},
		{
			name:     "English no_mnemonic_keystore label",
			language: "en",
			key:      "no_mnemonic_keystore",
			expected: "Mnemonic not available - imported from keystore file",
		},
		{
			name:     "English keystore import stage validating",
			language: "en",
			key:      "keystore_import_stage_validating",
			expected: "Validating keystore file...",
		},
		{
			name:     "English keystore import stage parsing",
			language: "en",
			key:      "keystore_import_stage_parsing",
			expected: "Parsing keystore structure...",
		},
		{
			name:     "English keystore import stage decrypting",
			language: "en",
			key:      "keystore_import_stage_decrypting",
			expected: "Decrypting private key...",
		},
		{
			name:     "English keystore import stage saving",
			language: "en",
			key:      "keystore_import_stage_saving",
			expected: "Saving wallet...",
		},

		// Portuguese labels
		{
			name:     "Portuguese imported_keystore label",
			language: "pt",
			key:      "imported_keystore",
			expected: "Keystore (Chave Privada)",
		},
		{
			name:     "Portuguese method_keystore label",
			language: "pt",
			key:      "method_keystore",
			expected: "Arquivo Keystore",
		},
		{
			name:     "Portuguese no_mnemonic_keystore label",
			language: "pt",
			key:      "no_mnemonic_keystore",
			expected: "Mnemônica não disponível - importada de arquivo keystore",
		},
		{
			name:     "Portuguese keystore import stage validating",
			language: "pt",
			key:      "keystore_import_stage_validating",
			expected: "Validando arquivo keystore...",
		},
		{
			name:     "Portuguese keystore import stage parsing",
			language: "pt",
			key:      "keystore_import_stage_parsing",
			expected: "Analisando estrutura do keystore...",
		},
		{
			name:     "Portuguese keystore import stage decrypting",
			language: "pt",
			key:      "keystore_import_stage_decrypting",
			expected: "Descriptografando chave privada...",
		},
		{
			name:     "Portuguese keystore import stage saving",
			language: "pt",
			key:      "keystore_import_stage_saving",
			expected: "Salvando carteira...",
		},

		// Spanish labels
		{
			name:     "Spanish imported_keystore label",
			language: "es",
			key:      "imported_keystore",
			expected: "Keystore (Clave Privada)",
		},
		{
			name:     "Spanish method_keystore label",
			language: "es",
			key:      "method_keystore",
			expected: "Archivo Keystore",
		},
		{
			name:     "Spanish no_mnemonic_keystore label",
			language: "es",
			key:      "no_mnemonic_keystore",
			expected: "Mnemónica no disponible - importada desde archivo keystore",
		},
		{
			name:     "Spanish keystore import stage validating",
			language: "es",
			key:      "keystore_import_stage_validating",
			expected: "Validando archivo keystore...",
		},
		{
			name:     "Spanish keystore import stage parsing",
			language: "es",
			key:      "keystore_import_stage_parsing",
			expected: "Analizando estructura del keystore...",
		},
		{
			name:     "Spanish keystore import stage decrypting",
			language: "es",
			key:      "keystore_import_stage_decrypting",
			expected: "Descifrando clave privada...",
		},
		{
			name:     "Spanish keystore import stage saving",
			language: "es",
			key:      "keystore_import_stage_saving",
			expected: "Guardando cartera...",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set the current language
			SetCurrentLanguage(tt.language)

			// Add wallet import messages for the current language
			AddWalletImportMessages()

			// Check if the label exists and has the expected value
			actual, exists := Labels[tt.key]
			assert.True(t, exists, "Label %s should exist for language %s", tt.key, tt.language)
			assert.Equal(t, tt.expected, actual, "Label %s should have correct value for language %s", tt.key, tt.language)
		})
	}
}

func TestGetNoMnemonicAvailableMessageForKeystore(t *testing.T) {
	// Initialize Labels map
	Labels = make(map[string]string)

	tests := []struct {
		name         string
		language     string
		importMethod string
		expected     string
	}{
		{
			name:         "English keystore import method",
			language:     "en",
			importMethod: "keystore",
			expected:     "Mnemonic not available - imported from keystore file",
		},
		{
			name:         "Portuguese keystore import method",
			language:     "pt",
			importMethod: "keystore",
			expected:     "Mnemônica não disponível - importada de arquivo keystore",
		},
		{
			name:         "Spanish keystore import method",
			language:     "es",
			importMethod: "keystore",
			expected:     "Mnemónica no disponible - importada desde archivo keystore",
		},
		{
			name:         "English private key import method",
			language:     "en",
			importMethod: "private_key",
			expected:     "Mnemonic not available (imported via private key)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set the current language
			SetCurrentLanguage(tt.language)

			// Add wallet import messages for the current language
			AddWalletImportMessages()

			// Test the function
			actual := GetNoMnemonicAvailableMessage(tt.importMethod)
			assert.Equal(t, tt.expected, actual)
		})
	}
}

func TestGetKeystoreImportStageMessage(t *testing.T) {
	// Initialize Labels map
	Labels = make(map[string]string)

	tests := []struct {
		name     string
		language string
		stage    string
		expected string
	}{
		{
			name:     "English validating stage",
			language: "en",
			stage:    "validating",
			expected: "Validating keystore file...",
		},
		{
			name:     "English parsing stage",
			language: "en",
			stage:    "parsing",
			expected: "Parsing keystore structure...",
		},
		{
			name:     "English decrypting stage",
			language: "en",
			stage:    "decrypting",
			expected: "Decrypting private key...",
		},
		{
			name:     "English saving stage",
			language: "en",
			stage:    "saving",
			expected: "Saving wallet...",
		},
		{
			name:     "Portuguese validating stage",
			language: "pt",
			stage:    "validating",
			expected: "Validando arquivo keystore...",
		},
		{
			name:     "Portuguese parsing stage",
			language: "pt",
			stage:    "parsing",
			expected: "Analisando estrutura do keystore...",
		},
		{
			name:     "Portuguese decrypting stage",
			language: "pt",
			stage:    "decrypting",
			expected: "Descriptografando chave privada...",
		},
		{
			name:     "Portuguese saving stage",
			language: "pt",
			stage:    "saving",
			expected: "Salvando carteira...",
		},
		{
			name:     "Spanish validating stage",
			language: "es",
			stage:    "validating",
			expected: "Validando archivo keystore...",
		},
		{
			name:     "Spanish parsing stage",
			language: "es",
			stage:    "parsing",
			expected: "Analizando estructura del keystore...",
		},
		{
			name:     "Spanish decrypting stage",
			language: "es",
			stage:    "decrypting",
			expected: "Descifrando clave privada...",
		},
		{
			name:     "Spanish saving stage",
			language: "es",
			stage:    "saving",
			expected: "Guardando cartera...",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set the current language
			SetCurrentLanguage(tt.language)

			// Add wallet import messages for the current language
			AddWalletImportMessages()

			// Test the function
			actual := GetKeystoreImportStageMessage(tt.stage)
			assert.Equal(t, tt.expected, actual)
		})
	}
}
