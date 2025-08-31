package localization

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAddEnhancedImportMessages(t *testing.T) {
	// Initialize Labels map
	Labels = make(map[string]string)

	// Set current language to English
	SetCurrentLanguage("en")

	// Add enhanced import messages
	AddEnhancedImportMessages()

	// Test that English messages are added
	assert.Equal(t, "Password file not found", Labels["password_file_not_found"])
	assert.Equal(t, "Cannot read password file", Labels["password_file_unreadable"])
	assert.Equal(t, "Password file is empty", Labels["password_file_empty"])
	assert.Equal(t, "Invalid password file format", Labels["password_file_invalid"])
	assert.Equal(t, "Password file is too large", Labels["password_file_oversized"])
	assert.Equal(t, "Password file is corrupted", Labels["password_file_corrupted"])

	// Test batch import messages
	assert.Equal(t, "Batch import operation failed", Labels["batch_import_failed"])
	assert.Equal(t, "Import job validation failed", Labels["import_job_validation_failed"])
	assert.Equal(t, "Directory scan failed", Labels["directory_scan_failed"])
	assert.Equal(t, "Password input timed out", Labels["password_input_timeout"])
	assert.Equal(t, "Password input was cancelled", Labels["password_input_cancelled"])
	assert.Equal(t, "Password input was skipped", Labels["password_input_skipped"])
	assert.Equal(t, "Maximum password attempts exceeded", Labels["max_password_attempts_exceeded"])

	// Test error category messages
	assert.Equal(t, "File System", Labels["error_category_filesystem"])
	assert.Equal(t, "Validation", Labels["error_category_validation"])
	assert.Equal(t, "Password", Labels["error_category_password"])
	assert.Equal(t, "User Action", Labels["error_category_user_action"])
	assert.Equal(t, "System", Labels["error_category_system"])
	assert.Equal(t, "Network", Labels["error_category_network"])
	assert.Equal(t, "Unknown", Labels["error_category_unknown"])

	// Test recovery hint messages
	assert.Equal(t, "Try entering the password manually or create a .pwd file", Labels["password_file_recovery_not_found"])
	assert.Equal(t, "Check file permissions and try again", Labels["password_file_recovery_unreadable"])
	assert.Equal(t, "Add the password to the .pwd file or enter it manually", Labels["password_file_recovery_empty"])

	// Test user-friendly error messages
	assert.Equal(t, "Unable to access the selected file", Labels["error_user_friendly_file_access"])
	assert.Equal(t, "The file format is not supported", Labels["error_user_friendly_invalid_format"])
	assert.Equal(t, "A password is required for this operation", Labels["error_user_friendly_password_required"])
	assert.Equal(t, "The operation could not be completed", Labels["error_user_friendly_operation_failed"])
	assert.Equal(t, "Operation cancelled by user", Labels["error_user_friendly_user_cancelled"])

	// Test progress and status messages
	assert.Equal(t, "Scanning directory for keystore files...", Labels["import_status_scanning_directory"])
	assert.Equal(t, "Validating selected files...", Labels["import_status_validating_files"])
	assert.Equal(t, "Processing batch import...", Labels["import_status_processing_batch"])
	assert.Equal(t, "Waiting for password input...", Labels["import_status_waiting_password"])

	// Test summary messages
	assert.Equal(t, "All imports completed successfully", Labels["import_summary_all_successful"])
	assert.Equal(t, "Some imports completed successfully", Labels["import_summary_partial_success"])
	assert.Equal(t, "All imports failed", Labels["import_summary_all_failed"])
	assert.Equal(t, "Import cancelled by user", Labels["import_summary_user_cancelled"])

	// Test action messages
	assert.Equal(t, "Retry Failed Imports", Labels["action_retry_failed_imports"])
	assert.Equal(t, "Retry with Manual Passwords", Labels["action_retry_with_manual_passwords"])
	assert.Equal(t, "Select Different Files", Labels["action_select_different_files"])
	assert.Equal(t, "Fix File Permissions", Labels["action_fix_file_permissions"])
	assert.Equal(t, "Return to Main Menu", Labels["action_return_to_menu"])
}

func TestAddEnhancedImportMessages_Portuguese(t *testing.T) {
	// Initialize Labels map
	Labels = make(map[string]string)

	// Set current language to Portuguese
	SetCurrentLanguage("pt")

	// Add enhanced import messages
	AddEnhancedImportMessages()

	// Test that Portuguese messages are added
	assert.Equal(t, "Arquivo de senha não encontrado", Labels["password_file_not_found"])
	assert.Equal(t, "Não é possível ler o arquivo de senha", Labels["password_file_unreadable"])
	assert.Equal(t, "Arquivo de senha está vazio", Labels["password_file_empty"])
	assert.Equal(t, "Formato de arquivo de senha inválido", Labels["password_file_invalid"])

	// Test batch import messages in Portuguese
	assert.Equal(t, "Operação de importação em lote falhou", Labels["batch_import_failed"])
	assert.Equal(t, "Validação do trabalho de importação falhou", Labels["import_job_validation_failed"])
	assert.Equal(t, "Varredura do diretório falhou", Labels["directory_scan_failed"])

	// Test error category messages in Portuguese
	assert.Equal(t, "Sistema de Arquivos", Labels["error_category_filesystem"])
	assert.Equal(t, "Validação", Labels["error_category_validation"])
	assert.Equal(t, "Senha", Labels["error_category_password"])
	assert.Equal(t, "Ação do Usuário", Labels["error_category_user_action"])

	// Test recovery hint messages in Portuguese
	assert.Equal(t, "Tente inserir a senha manualmente ou criar um arquivo .pwd", Labels["password_file_recovery_not_found"])
	assert.Equal(t, "Verifique as permissões do arquivo e tente novamente", Labels["password_file_recovery_unreadable"])

	// Test user-friendly error messages in Portuguese
	assert.Equal(t, "Não é possível acessar o arquivo selecionado", Labels["error_user_friendly_file_access"])
	assert.Equal(t, "O formato do arquivo não é suportado", Labels["error_user_friendly_invalid_format"])
	assert.Equal(t, "Uma senha é necessária para esta operação", Labels["error_user_friendly_password_required"])

	// Test action messages in Portuguese
	assert.Equal(t, "Repetir Importações Falhadas", Labels["action_retry_failed_imports"])
	assert.Equal(t, "Repetir com Senhas Manuais", Labels["action_retry_with_manual_passwords"])
	assert.Equal(t, "Selecionar Arquivos Diferentes", Labels["action_select_different_files"])
	assert.Equal(t, "Retornar ao Menu Principal", Labels["action_return_to_menu"])
}

func TestAddEnhancedImportMessages_Spanish(t *testing.T) {
	// Initialize Labels map
	Labels = make(map[string]string)

	// Set current language to Spanish
	SetCurrentLanguage("es")

	// Add enhanced import messages
	AddEnhancedImportMessages()

	// Test that Spanish messages are added
	assert.Equal(t, "Archivo de contraseña no encontrado", Labels["password_file_not_found"])
	assert.Equal(t, "No se puede leer el archivo de contraseña", Labels["password_file_unreadable"])
	assert.Equal(t, "El archivo de contraseña está vacío", Labels["password_file_empty"])
	assert.Equal(t, "Formato de archivo de contraseña inválido", Labels["password_file_invalid"])

	// Test batch import messages in Spanish
	assert.Equal(t, "La operación de importación por lotes falló", Labels["batch_import_failed"])
	assert.Equal(t, "La validación del trabajo de importación falló", Labels["import_job_validation_failed"])
	assert.Equal(t, "El escaneo del directorio falló", Labels["directory_scan_failed"])

	// Test error category messages in Spanish
	assert.Equal(t, "Sistema de Archivos", Labels["error_category_filesystem"])
	assert.Equal(t, "Validación", Labels["error_category_validation"])
	assert.Equal(t, "Contraseña", Labels["error_category_password"])
	assert.Equal(t, "Acción del Usuario", Labels["error_category_user_action"])

	// Test recovery hint messages in Spanish
	assert.Equal(t, "Intente ingresar la contraseña manualmente o crear un archivo .pwd", Labels["password_file_recovery_not_found"])
	assert.Equal(t, "Verifique los permisos del archivo e intente nuevamente", Labels["password_file_recovery_unreadable"])

	// Test user-friendly error messages in Spanish
	assert.Equal(t, "No se puede acceder al archivo seleccionado", Labels["error_user_friendly_file_access"])
	assert.Equal(t, "El formato del archivo no es compatible", Labels["error_user_friendly_invalid_format"])
	assert.Equal(t, "Se requiere una contraseña para esta operación", Labels["error_user_friendly_password_required"])

	// Test action messages in Spanish
	assert.Equal(t, "Reintentar Importaciones Fallidas", Labels["action_retry_failed_imports"])
	assert.Equal(t, "Reintentar con Contraseñas Manuales", Labels["action_retry_with_manual_passwords"])
	assert.Equal(t, "Seleccionar Archivos Diferentes", Labels["action_select_different_files"])
	assert.Equal(t, "Regresar al Menú Principal", Labels["action_return_to_menu"])
}

func TestGetEnhancedImportErrorMessage(t *testing.T) {
	// Initialize Labels map
	Labels = make(map[string]string)
	Labels["password_file_not_found"] = "Password file not found"
	Labels["batch_import_failed"] = "Batch import operation failed"

	// Test existing keys
	assert.Equal(t, "Password file not found", GetEnhancedImportErrorMessage("password_file_not_found"))
	assert.Equal(t, "Batch import operation failed", GetEnhancedImportErrorMessage("batch_import_failed"))

	// Test non-existing key (should return the key itself)
	assert.Equal(t, "non_existing_key", GetEnhancedImportErrorMessage("non_existing_key"))
}

func TestFormatErrorWithRecoveryHint(t *testing.T) {
	// Initialize Labels map
	Labels = make(map[string]string)
	Labels["password_file_not_found"] = "Password file not found"
	Labels["password_file_recovery_not_found"] = "Try entering the password manually"
	Labels["batch_import_failed"] = "Batch import operation failed"

	// Test with both error and recovery messages available
	result := FormatErrorWithRecoveryHint("password_file_not_found", "password_file_recovery_not_found")
	expected := "Password file not found\nTry entering the password manually"
	assert.Equal(t, expected, result)

	// Test with recovery message not available (should return just error message)
	result = FormatErrorWithRecoveryHint("batch_import_failed", "non_existing_recovery")
	assert.Equal(t, "Batch import operation failed", result)

	// Test with error message not available
	result = FormatErrorWithRecoveryHint("non_existing_error", "password_file_recovery_not_found")
	expected = "non_existing_error\nTry entering the password manually"
	assert.Equal(t, expected, result)
}

func TestEnhancedImportMessages_Coverage(t *testing.T) {
	// Initialize Labels map
	Labels = make(map[string]string)

	// Set current language to English
	SetCurrentLanguage("en")

	// Add enhanced import messages
	AddEnhancedImportMessages()

	// Test that all expected message categories are present
	expectedCategories := []string{
		// Password file errors
		"password_file_not_found",
		"password_file_unreadable",
		"password_file_empty",
		"password_file_invalid",
		"password_file_oversized",
		"password_file_corrupted",

		// Batch import errors
		"batch_import_failed",
		"import_job_validation_failed",
		"directory_scan_failed",
		"password_input_timeout",
		"password_input_cancelled",
		"password_input_skipped",
		"max_password_attempts_exceeded",

		// Recovery and aggregation errors
		"partial_import_failure",
		"import_interrupted",
		"cleanup_failed",

		// Error categories
		"error_category_filesystem",
		"error_category_validation",
		"error_category_password",
		"error_category_user_action",
		"error_category_system",
		"error_category_network",
		"error_category_unknown",

		// Recovery hints
		"password_file_recovery_not_found",
		"password_file_recovery_unreadable",
		"password_file_recovery_empty",
		"password_file_recovery_invalid",
		"batch_import_recovery_failed",
		"directory_scan_recovery_failed",
		"password_input_recovery_timeout",
		"password_attempts_recovery_exceeded",
		"generic_error_recovery",

		// Retry descriptions
		"retry_description_password_file_not_found",
		"retry_description_password_input_timeout",
		"retry_description_file_not_found",
		"retry_description_directory_scan_failed",
		"retry_description_incorrect_password",
		"retry_description_password_file_unreadable",
		"retry_description_password_file_empty",
		"retry_description_generic",

		// User-friendly messages
		"error_user_friendly_file_access",
		"error_user_friendly_invalid_format",
		"error_user_friendly_password_required",
		"error_user_friendly_operation_failed",
		"error_user_friendly_user_cancelled",
		"error_user_friendly_timeout",
		"error_user_friendly_permission_denied",
		"error_user_friendly_generic",

		// Progress and status messages
		"import_status_scanning_directory",
		"import_status_validating_files",
		"import_status_processing_batch",
		"import_status_waiting_password",
		"import_status_completing",
		"import_status_cleaning_up",

		// Summary messages
		"import_summary_all_successful",
		"import_summary_partial_success",
		"import_summary_all_failed",
		"import_summary_user_cancelled",
		"import_summary_recoverable_errors",
		"import_summary_no_recoverable_errors",

		// Action messages
		"action_retry_failed_imports",
		"action_retry_with_manual_passwords",
		"action_select_different_files",
		"action_fix_file_permissions",
		"action_return_to_menu",
		"action_view_error_details",
		"action_export_error_report",
	}

	// Verify all expected messages are present
	for _, key := range expectedCategories {
		assert.Contains(t, Labels, key, "Missing localization key: %s", key)
		assert.NotEmpty(t, Labels[key], "Empty localization value for key: %s", key)
	}

	// Verify we have a reasonable number of messages
	assert.GreaterOrEqual(t, len(Labels), len(expectedCategories), "Should have at least %d localization messages", len(expectedCategories))
}

func TestEnhancedImportMessages_LanguageSwitching(t *testing.T) {
	// Test switching between languages

	// Start with English
	Labels = make(map[string]string)
	SetCurrentLanguage("en")
	AddEnhancedImportMessages()
	englishMessage := Labels["password_file_not_found"]
	assert.Equal(t, "Password file not found", englishMessage)

	// Switch to Portuguese
	Labels = make(map[string]string)
	SetCurrentLanguage("pt")
	AddEnhancedImportMessages()
	portugueseMessage := Labels["password_file_not_found"]
	assert.Equal(t, "Arquivo de senha não encontrado", portugueseMessage)

	// Switch to Spanish
	Labels = make(map[string]string)
	SetCurrentLanguage("es")
	AddEnhancedImportMessages()
	spanishMessage := Labels["password_file_not_found"]
	assert.Equal(t, "Archivo de contraseña no encontrado", spanishMessage)

	// Verify messages are different
	assert.NotEqual(t, englishMessage, portugueseMessage)
	assert.NotEqual(t, englishMessage, spanishMessage)
	assert.NotEqual(t, portugueseMessage, spanishMessage)
}

func TestEnhancedImportMessages_FallbackToEnglish(t *testing.T) {
	// Test that unsupported languages fall back to English
	Labels = make(map[string]string)
	SetCurrentLanguage("fr") // French is not supported
	AddEnhancedImportMessages()

	// Should have English messages as fallback
	assert.Equal(t, "Password file not found", Labels["password_file_not_found"])
	assert.Equal(t, "Batch import operation failed", Labels["batch_import_failed"])
	assert.Equal(t, "File System", Labels["error_category_filesystem"])
}
