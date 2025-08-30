package localization

// AddEnhancedImportMessages adds comprehensive error messages for enhanced import functionality
func AddEnhancedImportMessages() {
	// Add English messages
	englishMessages := map[string]string{
		// Password file error messages
		"password_file_not_found":  "Password file not found",
		"password_file_unreadable": "Cannot read password file",
		"password_file_empty":      "Password file is empty",
		"password_file_invalid":    "Invalid password file format",
		"password_file_oversized":  "Password file is too large",
		"password_file_corrupted":  "Password file is corrupted",

		// Batch import error messages
		"batch_import_failed":            "Batch import operation failed",
		"import_job_validation_failed":   "Import job validation failed",
		"directory_scan_failed":          "Directory scan failed",
		"password_input_timeout":         "Password input timed out",
		"password_input_cancelled":       "Password input was cancelled",
		"password_input_skipped":         "Password input was skipped",
		"max_password_attempts_exceeded": "Maximum password attempts exceeded",

		// Recovery and aggregation error messages
		"partial_import_failure": "Some imports failed",
		"import_interrupted":     "Import process was interrupted",
		"cleanup_failed":         "Cleanup operation failed",

		// Error category messages
		"error_category_filesystem":  "File System",
		"error_category_validation":  "Validation",
		"error_category_password":    "Password",
		"error_category_user_action": "User Action",
		"error_category_system":      "System",
		"error_category_network":     "Network",
		"error_category_unknown":     "Unknown",

		// Recovery hint messages
		"password_file_recovery_not_found":    "Try entering the password manually or create a .pwd file",
		"password_file_recovery_unreadable":   "Check file permissions and try again",
		"password_file_recovery_empty":        "Add the password to the .pwd file or enter it manually",
		"password_file_recovery_invalid":      "Ensure the password file contains only the password text",
		"batch_import_recovery_failed":        "Review individual file errors and retry",
		"directory_scan_recovery_failed":      "Check directory permissions and file formats",
		"password_input_recovery_timeout":     "Try again with a shorter response time",
		"password_attempts_recovery_exceeded": "Reset and try with the correct password",
		"generic_error_recovery":              "Please check the file and try again",

		// Retry description messages
		"retry_description_password_file_not_found":  "Enter passwords manually for files without .pwd files",
		"retry_description_password_input_timeout":   "Respond more quickly to password prompts",
		"retry_description_file_not_found":           "Select valid keystore files that exist",
		"retry_description_directory_scan_failed":    "Choose a different directory or fix permissions",
		"retry_description_incorrect_password":       "Use the correct password for each keystore",
		"retry_description_password_file_unreadable": "Fix file permissions or recreate password files",
		"retry_description_password_file_empty":      "Add content to empty password files",
		"retry_description_generic":                  "Review and fix the identified issues",

		// User-friendly error messages (no sensitive info)
		"error_user_friendly_file_access":       "Unable to access the selected file",
		"error_user_friendly_invalid_format":    "The file format is not supported",
		"error_user_friendly_password_required": "A password is required for this operation",
		"error_user_friendly_operation_failed":  "The operation could not be completed",
		"error_user_friendly_user_cancelled":    "Operation cancelled by user",
		"error_user_friendly_timeout":           "Operation timed out",
		"error_user_friendly_permission_denied": "Permission denied",
		"error_user_friendly_generic":           "An error occurred during the operation",

		// Progress and status messages
		"import_status_scanning_directory": "Scanning directory for keystore files...",
		"import_status_validating_files":   "Validating selected files...",
		"import_status_processing_batch":   "Processing batch import...",
		"import_status_waiting_password":   "Waiting for password input...",
		"import_status_completing":         "Completing import operation...",
		"import_status_cleaning_up":        "Cleaning up resources...",

		// Summary messages
		"import_summary_all_successful":        "All imports completed successfully",
		"import_summary_partial_success":       "Some imports completed successfully",
		"import_summary_all_failed":            "All imports failed",
		"import_summary_user_cancelled":        "Import cancelled by user",
		"import_summary_recoverable_errors":    "Some errors can be fixed and retried",
		"import_summary_no_recoverable_errors": "No errors can be automatically recovered",

		// Action messages
		"action_retry_failed_imports":        "Retry Failed Imports",
		"action_retry_with_manual_passwords": "Retry with Manual Passwords",
		"action_select_different_files":      "Select Different Files",
		"action_fix_file_permissions":        "Fix File Permissions",
		"action_return_to_menu":              "Return to Main Menu",
		"action_view_error_details":          "View Error Details",
		"action_export_error_report":         "Export Error Report",
	}

	// Add Portuguese messages
	portugueseMessages := map[string]string{
		// Password file error messages
		"password_file_not_found":  "Arquivo de senha não encontrado",
		"password_file_unreadable": "Não é possível ler o arquivo de senha",
		"password_file_empty":      "Arquivo de senha está vazio",
		"password_file_invalid":    "Formato de arquivo de senha inválido",
		"password_file_oversized":  "Arquivo de senha é muito grande",
		"password_file_corrupted":  "Arquivo de senha está corrompido",

		// Batch import error messages
		"batch_import_failed":            "Operação de importação em lote falhou",
		"import_job_validation_failed":   "Validação do trabalho de importação falhou",
		"directory_scan_failed":          "Varredura do diretório falhou",
		"password_input_timeout":         "Entrada de senha expirou",
		"password_input_cancelled":       "Entrada de senha foi cancelada",
		"password_input_skipped":         "Entrada de senha foi pulada",
		"max_password_attempts_exceeded": "Máximo de tentativas de senha excedido",

		// Recovery and aggregation error messages
		"partial_import_failure": "Algumas importações falharam",
		"import_interrupted":     "Processo de importação foi interrompido",
		"cleanup_failed":         "Operação de limpeza falhou",

		// Error category messages
		"error_category_filesystem":  "Sistema de Arquivos",
		"error_category_validation":  "Validação",
		"error_category_password":    "Senha",
		"error_category_user_action": "Ação do Usuário",
		"error_category_system":      "Sistema",
		"error_category_network":     "Rede",
		"error_category_unknown":     "Desconhecido",

		// Recovery hint messages
		"password_file_recovery_not_found":    "Tente inserir a senha manualmente ou criar um arquivo .pwd",
		"password_file_recovery_unreadable":   "Verifique as permissões do arquivo e tente novamente",
		"password_file_recovery_empty":        "Adicione a senha ao arquivo .pwd ou insira manualmente",
		"password_file_recovery_invalid":      "Certifique-se de que o arquivo de senha contém apenas o texto da senha",
		"batch_import_recovery_failed":        "Revise os erros de arquivos individuais e tente novamente",
		"directory_scan_recovery_failed":      "Verifique as permissões do diretório e formatos de arquivo",
		"password_input_recovery_timeout":     "Tente novamente com um tempo de resposta menor",
		"password_attempts_recovery_exceeded": "Reinicie e tente com a senha correta",
		"generic_error_recovery":              "Por favor, verifique o arquivo e tente novamente",

		// Retry description messages
		"retry_description_password_file_not_found":  "Insira senhas manualmente para arquivos sem .pwd",
		"retry_description_password_input_timeout":   "Responda mais rapidamente aos prompts de senha",
		"retry_description_file_not_found":           "Selecione arquivos keystore válidos que existam",
		"retry_description_directory_scan_failed":    "Escolha um diretório diferente ou corrija permissões",
		"retry_description_incorrect_password":       "Use a senha correta para cada keystore",
		"retry_description_password_file_unreadable": "Corrija permissões de arquivo ou recrie arquivos de senha",
		"retry_description_password_file_empty":      "Adicione conteúdo aos arquivos de senha vazios",
		"retry_description_generic":                  "Revise e corrija os problemas identificados",

		// User-friendly error messages
		"error_user_friendly_file_access":       "Não é possível acessar o arquivo selecionado",
		"error_user_friendly_invalid_format":    "O formato do arquivo não é suportado",
		"error_user_friendly_password_required": "Uma senha é necessária para esta operação",
		"error_user_friendly_operation_failed":  "A operação não pôde ser concluída",
		"error_user_friendly_user_cancelled":    "Operação cancelada pelo usuário",
		"error_user_friendly_timeout":           "Operação expirou",
		"error_user_friendly_permission_denied": "Permissão negada",
		"error_user_friendly_generic":           "Ocorreu um erro durante a operação",

		// Progress and status messages
		"import_status_scanning_directory": "Verificando diretório para arquivos keystore...",
		"import_status_validating_files":   "Validando arquivos selecionados...",
		"import_status_processing_batch":   "Processando importação em lote...",
		"import_status_waiting_password":   "Aguardando entrada de senha...",
		"import_status_completing":         "Completando operação de importação...",
		"import_status_cleaning_up":        "Limpando recursos...",

		// Summary messages
		"import_summary_all_successful":        "Todas as importações foram concluídas com sucesso",
		"import_summary_partial_success":       "Algumas importações foram concluídas com sucesso",
		"import_summary_all_failed":            "Todas as importações falharam",
		"import_summary_user_cancelled":        "Importação cancelada pelo usuário",
		"import_summary_recoverable_errors":    "Alguns erros podem ser corrigidos e repetidos",
		"import_summary_no_recoverable_errors": "Nenhum erro pode ser recuperado automaticamente",

		// Action messages
		"action_retry_failed_imports":        "Repetir Importações Falhadas",
		"action_retry_with_manual_passwords": "Repetir com Senhas Manuais",
		"action_select_different_files":      "Selecionar Arquivos Diferentes",
		"action_fix_file_permissions":        "Corrigir Permissões de Arquivo",
		"action_return_to_menu":              "Retornar ao Menu Principal",
		"action_view_error_details":          "Ver Detalhes do Erro",
		"action_export_error_report":         "Exportar Relatório de Erro",
	}

	// Add Spanish messages
	spanishMessages := map[string]string{
		// Password file error messages
		"password_file_not_found":  "Archivo de contraseña no encontrado",
		"password_file_unreadable": "No se puede leer el archivo de contraseña",
		"password_file_empty":      "El archivo de contraseña está vacío",
		"password_file_invalid":    "Formato de archivo de contraseña inválido",
		"password_file_oversized":  "El archivo de contraseña es demasiado grande",
		"password_file_corrupted":  "El archivo de contraseña está dañado",

		// Batch import error messages
		"batch_import_failed":            "La operación de importación por lotes falló",
		"import_job_validation_failed":   "La validación del trabajo de importación falló",
		"directory_scan_failed":          "El escaneo del directorio falló",
		"password_input_timeout":         "La entrada de contraseña expiró",
		"password_input_cancelled":       "La entrada de contraseña fue cancelada",
		"password_input_skipped":         "La entrada de contraseña fue omitida",
		"max_password_attempts_exceeded": "Máximo de intentos de contraseña excedido",

		// Recovery and aggregation error messages
		"partial_import_failure": "Algunas importaciones fallaron",
		"import_interrupted":     "El proceso de importación fue interrumpido",
		"cleanup_failed":         "La operación de limpieza falló",

		// Error category messages
		"error_category_filesystem":  "Sistema de Archivos",
		"error_category_validation":  "Validación",
		"error_category_password":    "Contraseña",
		"error_category_user_action": "Acción del Usuario",
		"error_category_system":      "Sistema",
		"error_category_network":     "Red",
		"error_category_unknown":     "Desconocido",

		// Recovery hint messages
		"password_file_recovery_not_found":    "Intente ingresar la contraseña manualmente o crear un archivo .pwd",
		"password_file_recovery_unreadable":   "Verifique los permisos del archivo e intente nuevamente",
		"password_file_recovery_empty":        "Agregue la contraseña al archivo .pwd o ingrésela manualmente",
		"password_file_recovery_invalid":      "Asegúrese de que el archivo de contraseña contenga solo el texto de la contraseña",
		"batch_import_recovery_failed":        "Revise los errores de archivos individuales e intente nuevamente",
		"directory_scan_recovery_failed":      "Verifique los permisos del directorio y formatos de archivo",
		"password_input_recovery_timeout":     "Intente nuevamente con un tiempo de respuesta menor",
		"password_attempts_recovery_exceeded": "Reinicie e intente con la contraseña correcta",
		"generic_error_recovery":              "Por favor, verifique el archivo e intente nuevamente",

		// Retry description messages
		"retry_description_password_file_not_found":  "Ingrese contraseñas manualmente para archivos sin .pwd",
		"retry_description_password_input_timeout":   "Responda más rápidamente a las solicitudes de contraseña",
		"retry_description_file_not_found":           "Seleccione archivos keystore válidos que existan",
		"retry_description_directory_scan_failed":    "Elija un directorio diferente o corrija permisos",
		"retry_description_incorrect_password":       "Use la contraseña correcta para cada keystore",
		"retry_description_password_file_unreadable": "Corrija permisos de archivo o recree archivos de contraseña",
		"retry_description_password_file_empty":      "Agregue contenido a archivos de contraseña vacíos",
		"retry_description_generic":                  "Revise y corrija los problemas identificados",

		// User-friendly error messages
		"error_user_friendly_file_access":       "No se puede acceder al archivo seleccionado",
		"error_user_friendly_invalid_format":    "El formato del archivo no es compatible",
		"error_user_friendly_password_required": "Se requiere una contraseña para esta operación",
		"error_user_friendly_operation_failed":  "La operación no se pudo completar",
		"error_user_friendly_user_cancelled":    "Operación cancelada por el usuario",
		"error_user_friendly_timeout":           "La operación expiró",
		"error_user_friendly_permission_denied": "Permiso denegado",
		"error_user_friendly_generic":           "Ocurrió un error durante la operación",

		// Progress and status messages
		"import_status_scanning_directory": "Escaneando directorio para archivos keystore...",
		"import_status_validating_files":   "Validando archivos seleccionados...",
		"import_status_processing_batch":   "Procesando importación por lotes...",
		"import_status_waiting_password":   "Esperando entrada de contraseña...",
		"import_status_completing":         "Completando operación de importación...",
		"import_status_cleaning_up":        "Limpiando recursos...",

		// Summary messages
		"import_summary_all_successful":        "Todas las importaciones se completaron exitosamente",
		"import_summary_partial_success":       "Algunas importaciones se completaron exitosamente",
		"import_summary_all_failed":            "Todas las importaciones fallaron",
		"import_summary_user_cancelled":        "Importación cancelada por el usuario",
		"import_summary_recoverable_errors":    "Algunos errores pueden ser corregidos y reintentados",
		"import_summary_no_recoverable_errors": "Ningún error puede ser recuperado automáticamente",

		// Action messages
		"action_retry_failed_imports":        "Reintentar Importaciones Fallidas",
		"action_retry_with_manual_passwords": "Reintentar con Contraseñas Manuales",
		"action_select_different_files":      "Seleccionar Archivos Diferentes",
		"action_fix_file_permissions":        "Corregir Permisos de Archivo",
		"action_return_to_menu":              "Regresar al Menú Principal",
		"action_view_error_details":          "Ver Detalles del Error",
		"action_export_error_report":         "Exportar Reporte de Error",
	}

	// Add to global Labels map based on current language
	currentLang := GetCurrentLanguage()

	// Always add English as fallback
	for key, value := range englishMessages {
		Labels[key] = value
	}

	// Override with localized messages if available
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

// GetEnhancedImportErrorMessage returns a localized error message for enhanced import errors
func GetEnhancedImportErrorMessage(key string) string {
	if value, ok := Labels[key]; ok {
		return value
	}
	return key
}

// FormatErrorWithRecoveryHint formats an error message with recovery hint
func FormatErrorWithRecoveryHint(errorKey string, recoveryKey string) string {
	errorMsg := GetEnhancedImportErrorMessage(errorKey)
	recoveryMsg := GetEnhancedImportErrorMessage(recoveryKey)

	if recoveryMsg != recoveryKey {
		return errorMsg + "\n" + recoveryMsg
	}
	return errorMsg
}
