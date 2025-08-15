package localization

// AddKeystoreValidationMessages adds keystore validation messages to the existing message maps
func AddKeystoreValidationMessages() {
	// Add English messages
	englishMessages := map[string]string{
		// Keystore file validation feedback
		"keystore_file_valid":                  "✓ Valid keystore file detected",
		"keystore_access_error":                "✗ Error accessing file",
		"keystore_is_directory":                "✗ Path points to a directory, not a file",
		"keystore_not_json":                    "✗ File does not contain valid JSON",
		"keystore_file_not_found":              "Keystore file not found",
		"keystore_invalid_json":                "Invalid JSON in keystore file",
		"keystore_invalid_structure":           "Invalid keystore structure",
		"keystore_invalid_version":             "Invalid keystore version",
		"keystore_incorrect_password":          "Incorrect password for keystore",
		"keystore_corrupted_file":              "Corrupted keystore file",
		"keystore_address_mismatch":            "Address mismatch in keystore",
		"keystore_missing_fields":              "Missing required fields in keystore",
		"keystore_invalid_address":             "Invalid address in keystore",
		"unknown_error":                        "Unknown error",
		"keystore_recovery_file_not_found":     "Try checking the file path and ensure the file exists",
		"keystore_recovery_invalid_json":       "The file may be corrupted or not contain valid JSON. Try with a different keystore file",
		"keystore_recovery_invalid_structure":  "Make sure this is a valid Ethereum keystore v3 file",
		"keystore_recovery_incorrect_password": "Please try again with the correct password",
		"keystore_recovery_general":            "Please check the file and try again",
	}

	// Add Portuguese messages
	portugueseMessages := map[string]string{
		"keystore_file_valid":                  "✓ Arquivo keystore válido detectado",
		"keystore_access_error":                "✗ Erro ao acessar o arquivo",
		"keystore_is_directory":                "✗ O caminho aponta para um diretório, não um arquivo",
		"keystore_not_json":                    "✗ O arquivo não contém um JSON válido",
		"keystore_file_not_found":              "Arquivo keystore não encontrado",
		"keystore_invalid_json":                "JSON inválido no arquivo keystore",
		"keystore_invalid_structure":           "Estrutura de keystore inválida",
		"keystore_invalid_version":             "Versão de keystore inválida",
		"keystore_incorrect_password":          "Senha incorreta para o keystore",
		"keystore_corrupted_file":              "Arquivo keystore corrompido",
		"keystore_address_mismatch":            "Endereço não confere no keystore",
		"keystore_missing_fields":              "Campos obrigatórios ausentes no keystore",
		"keystore_invalid_address":             "Endereço inválido no keystore",
		"unknown_error":                        "Erro desconhecido",
		"keystore_recovery_file_not_found":     "Verifique o caminho do arquivo e certifique-se de que ele existe",
		"keystore_recovery_invalid_json":       "O arquivo pode estar corrompido ou não conter JSON válido. Tente com um arquivo keystore diferente",
		"keystore_recovery_invalid_structure":  "Certifique-se de que este é um arquivo keystore v3 Ethereum válido",
		"keystore_recovery_incorrect_password": "Por favor, tente novamente com a senha correta",
		"keystore_recovery_general":            "Por favor, verifique o arquivo e tente novamente",
	}

	// Add Spanish messages
	spanishMessages := map[string]string{
		"keystore_file_valid":                  "✓ Archivo keystore válido detectado",
		"keystore_access_error":                "✗ Error al acceder al archivo",
		"keystore_is_directory":                "✗ La ruta apunta a un directorio, no a un archivo",
		"keystore_not_json":                    "✗ El archivo no contiene un JSON válido",
		"keystore_file_not_found":              "Archivo keystore no encontrado",
		"keystore_invalid_json":                "JSON inválido en archivo keystore",
		"keystore_invalid_structure":           "Estructura de keystore inválida",
		"keystore_invalid_version":             "Versión de keystore inválida",
		"keystore_incorrect_password":          "Contraseña incorrecta para keystore",
		"keystore_corrupted_file":              "Archivo keystore dañado",
		"keystore_address_mismatch":            "Dirección no coincide en keystore",
		"keystore_missing_fields":              "Campos requeridos faltantes en keystore",
		"keystore_invalid_address":             "Dirección inválida en keystore",
		"unknown_error":                        "Error desconocido",
		"keystore_recovery_file_not_found":     "Verifique la ruta del archivo y asegúrese de que existe",
		"keystore_recovery_invalid_json":       "El archivo puede estar dañado o no contener JSON válido. Intente con un archivo keystore diferente",
		"keystore_recovery_invalid_structure":  "Asegúrese de que este es un archivo keystore v3 de Ethereum válido",
		"keystore_recovery_incorrect_password": "Por favor, intente nuevamente con la contraseña correcta",
		"keystore_recovery_general":            "Por favor, verifique el archivo e intente nuevamente",
	}

	// Add to global Labels map
	for key, value := range englishMessages {
		Labels[key] = value
	}

	// Add Portuguese and Spanish messages based on current language
	currentLang := GetCurrentLanguage()
	if currentLang == "pt" {
		for key, value := range portugueseMessages {
			Labels[key] = value
		}
	} else if currentLang == "es" {
		for key, value := range spanishMessages {
			Labels[key] = value
		}
	}
}
