package localization

// AddPasswordFileMessages adds password file error messages to the existing message maps
func AddPasswordFileMessages() {
	// Add English messages
	englishMessages := map[string]string{
		"password_file_not_found":     "Password file not found",
		"password_file_unreadable":    "Cannot read password file",
		"password_file_empty":         "Password file is empty",
		"password_file_invalid":       "Invalid password file",
		"password_file_oversized":     "Password file is too large",
		"password_file_corrupted":     "Password file contains invalid characters",
		"password_file_unknown_error": "Unknown password file error",

		// Recovery suggestions
		"password_file_recovery_not_found":  "Check if the .pwd file exists in the same directory as the keystore file",
		"password_file_recovery_unreadable": "Check file permissions and ensure the file is accessible",
		"password_file_recovery_empty":      "Add the password to the .pwd file or use manual password input",
		"password_file_recovery_invalid":    "Ensure the password file contains only the password text",
		"password_file_recovery_oversized":  "Password files should contain only the password (max 256 characters)",
		"password_file_recovery_corrupted":  "Ensure the password file is saved with UTF-8 encoding",
		"password_file_recovery_general":    "Check the password file and try again",
	}

	// Add Portuguese messages
	portugueseMessages := map[string]string{
		"password_file_not_found":     "Arquivo de senha não encontrado",
		"password_file_unreadable":    "Não é possível ler o arquivo de senha",
		"password_file_empty":         "Arquivo de senha está vazio",
		"password_file_invalid":       "Arquivo de senha inválido",
		"password_file_oversized":     "Arquivo de senha é muito grande",
		"password_file_corrupted":     "Arquivo de senha contém caracteres inválidos",
		"password_file_unknown_error": "Erro desconhecido no arquivo de senha",

		// Recovery suggestions
		"password_file_recovery_not_found":  "Verifique se o arquivo .pwd existe no mesmo diretório do arquivo keystore",
		"password_file_recovery_unreadable": "Verifique as permissões do arquivo e certifique-se de que está acessível",
		"password_file_recovery_empty":      "Adicione a senha ao arquivo .pwd ou use entrada manual de senha",
		"password_file_recovery_invalid":    "Certifique-se de que o arquivo de senha contém apenas o texto da senha",
		"password_file_recovery_oversized":  "Arquivos de senha devem conter apenas a senha (máx 256 caracteres)",
		"password_file_recovery_corrupted":  "Certifique-se de que o arquivo de senha está salvo com codificação UTF-8",
		"password_file_recovery_general":    "Verifique o arquivo de senha e tente novamente",
	}

	// Add Spanish messages
	spanishMessages := map[string]string{
		"password_file_not_found":     "Archivo de contraseña no encontrado",
		"password_file_unreadable":    "No se puede leer el archivo de contraseña",
		"password_file_empty":         "El archivo de contraseña está vacío",
		"password_file_invalid":       "Archivo de contraseña inválido",
		"password_file_oversized":     "El archivo de contraseña es demasiado grande",
		"password_file_corrupted":     "El archivo de contraseña contiene caracteres inválidos",
		"password_file_unknown_error": "Error desconocido en archivo de contraseña",

		// Recovery suggestions
		"password_file_recovery_not_found":  "Verifique si el archivo .pwd existe en el mismo directorio que el archivo keystore",
		"password_file_recovery_unreadable": "Verifique los permisos del archivo y asegúrese de que sea accesible",
		"password_file_recovery_empty":      "Agregue la contraseña al archivo .pwd o use entrada manual de contraseña",
		"password_file_recovery_invalid":    "Asegúrese de que el archivo de contraseña contenga solo el texto de la contraseña",
		"password_file_recovery_oversized":  "Los archivos de contraseña deben contener solo la contraseña (máx 256 caracteres)",
		"password_file_recovery_corrupted":  "Asegúrese de que el archivo de contraseña esté guardado con codificación UTF-8",
		"password_file_recovery_general":    "Verifique el archivo de contraseña e intente nuevamente",
	}

	// Add to global Labels map
	for key, value := range englishMessages {
		Labels[key] = value
	}

	// Add Portuguese and Spanish messages based on current language
	currentLang := GetCurrentLanguage()
	switch currentLang {
	case "pt":
		for key, value := range portugueseMessages {
			Labels[key] = value
		}
	case "es":
		for key, value := range spanishMessages {
			Labels[key] = value
		}
	}
}

// GetPasswordFileErrorMessage returns a localized error message for a password file error key
func GetPasswordFileErrorMessage(key string) string {
	// Use Labels map directly for simplicity
	if value, ok := Labels[key]; ok {
		return value
	}
	return key
}

// FormatPasswordFileErrorWithFile formats a password file error message with a file name
func FormatPasswordFileErrorWithFile(key string, file string) string {
	message := GetPasswordFileErrorMessage(key)
	if file != "" {
		return message + " (" + file + ")"
	}
	return message
}
