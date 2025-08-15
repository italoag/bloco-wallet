package localization

// DefaultCryptoMessages retorna mensagens padrão para criptografia em inglês
func DefaultCryptoMessages() map[string]string {
	return map[string]string{
		"error_empty_password":                 "Password cannot be empty",
		"error_generate_salt":                  "Failed to generate salt",
		"error_empty_encrypted_mnemonic":       "Encrypted mnemonic cannot be empty",
		"error_decode_mnemonic":                "Failed to decode encrypted mnemonic",
		"error_invalid_mnemonic_format":        "Invalid encrypted mnemonic format: too short",
		"error_invalid_password":               "Invalid password: hash verification failed",
		"error_crypto_service_not_initialized": "Crypto service not initialized",
		"password_too_short":                   "Password must have at least 8 characters",
		"password_no_lowercase":                "Password must contain at least one lowercase letter",
		"password_no_uppercase":                "Password must contain at least one uppercase letter",
		"password_no_digit_or_special":         "Password must contain at least one digit or special character",
		"password_validation_failed":           "Password validation failed",
		"keystore_file_not_found":              "Keystore file not found at the specified path",
		"keystore_invalid_json":                "The file contains an invalid JSON",
		"keystore_invalid_structure":           "The file isn't a valid KeyStoreV3 file",
		"keystore_invalid_version":             "Invalid keystore version, expected version 3",
		"keystore_incorrect_password":          "Incorrect password for the keystore file",
		"keystore_corrupted_file":              "Keystore file is corrupted or damaged",
		"keystore_address_mismatch":            "Address in keystore doesn't match the derived private key",
		"keystore_missing_fields":              "Keystore file is missing required fields",
		"keystore_invalid_address":             "Invalid Ethereum address format in keystore",
	}
}

// DefaultCryptoMessagesPortuguese retorna mensagens padrão para criptografia em português
func DefaultCryptoMessagesPortuguese() map[string]string {
	return map[string]string{
		"error_empty_password":                 "A senha não pode estar vazia",
		"error_generate_salt":                  "Falha ao gerar salt criptográfico",
		"error_empty_encrypted_mnemonic":       "A frase mnemônica criptografada não pode estar vazia",
		"error_decode_mnemonic":                "Falha ao decodificar a frase mnemônica criptografada",
		"error_invalid_mnemonic_format":        "Formato de frase mnemônica criptografada inválido: muito curta",
		"error_invalid_password":               "Senha inválida: verificação de hash falhou",
		"error_crypto_service_not_initialized": "Serviço de criptografia não inicializado",
		"password_too_short":                   "A senha deve ter pelo menos 8 caracteres",
		"password_no_lowercase":                "A senha deve conter pelo menos uma letra minúscula",
		"password_no_uppercase":                "A senha deve conter pelo menos uma letra maiúscula",
		"password_no_digit_or_special":         "A senha deve conter pelo menos um dígito ou caractere especial",
		"password_validation_failed":           "Falha na validação da senha",
		"keystore_file_not_found":              "Arquivo keystore não encontrado no caminho especificado",
		"keystore_invalid_json":                "O arquivo não contém um JSON válido",
		"keystore_invalid_structure":           "O arquivo não contém um keystore v3 válido",
		"keystore_invalid_version":             "Versão de keystore inválida, esperada versão 3",
		"keystore_incorrect_password":          "Senha incorreta para o arquivo keystore",
		"keystore_corrupted_file":              "Arquivo keystore está corrompido ou danificado",
		"keystore_address_mismatch":            "Endereço no keystore não corresponde à chave privada derivada",
		"keystore_missing_fields":              "Arquivo keystore está faltando campos obrigatórios",
		"keystore_invalid_address":             "Formato de endereço Ethereum inválido no keystore",
	}
}

// DefaultCryptoMessagesSpanish retorna mensagens padrão para criptografia em espanhol
func DefaultCryptoMessagesSpanish() map[string]string {
	return map[string]string{
		"error_empty_password":                 "La contraseña no puede estar vacía",
		"error_generate_salt":                  "Error al generar salt criptográfico",
		"error_empty_encrypted_mnemonic":       "La frase mnemónica cifrada no puede estar vacía",
		"error_decode_mnemonic":                "Error al decodificar la frase mnemónica cifrada",
		"error_invalid_mnemonic_format":        "Formato de frase mnemónica cifrada inválido: demasiado corta",
		"error_invalid_password":               "Contraseña inválida: la verificación del hash falló",
		"error_crypto_service_not_initialized": "Servicio de criptografía no inicializado",
		"password_too_short":                   "La contraseña debe tener al menos 8 caracteres",
		"password_no_lowercase":                "La contraseña debe contener al menos una letra minúscula",
		"password_no_uppercase":                "La contraseña debe contener al menos una letra mayúscula",
		"password_no_digit_or_special":         "La contraseña debe contener al menos un dígito o carácter especial",
		"password_validation_failed":           "Falló la validación de la contraseña",
		"keystore_file_not_found":              "Archivo keystore no encontrado en la ruta especificada",
		"keystore_invalid_json":                "El archivo no contiene un JSON válido",
		"keystore_invalid_structure":           "El archivo no contiene un keystore v3 válido",
		"keystore_invalid_version":             "Versión de keystore inválida, se esperaba versión 3",
		"keystore_incorrect_password":          "Contraseña incorrecta para el archivo keystore",
		"keystore_corrupted_file":              "El archivo keystore está corrupto o dañado",
		"keystore_address_mismatch":            "La dirección en el keystore no corresponde a la clave privada derivada",
		"keystore_missing_fields":              "Al archivo keystore le faltan campos obligatorios",
		"keystore_invalid_address":             "Formato de dirección Ethereum inválido en el keystore",
	}
}
