package localization

import "fmt"

// AddWalletImportMessages adds wallet import-related messages to the Labels map
func AddWalletImportMessages() {
	// English messages
	english := map[string]string{
		"duplicate_mnemonic":               "A wallet with this mnemonic phrase already exists",
		"duplicate_private_key":            "A wallet with this private key already exists",
		"no_mnemonic_available":            "Mnemonic not available (imported via private key)",
		"no_mnemonic_keystore":             "Mnemonic not available - imported from keystore file",
		"guidance_duplicate_mnemonic":      "You already imported this mnemonic. If you meant a different wallet, check the phrase and try again.",
		"guidance_duplicate_private_key":   "This private key is already imported. Open the existing wallet entry instead of importing again.",
		"method_label":                     "Method",
		"method_mnemonic":                  "Mnemonic",
		"method_private_key":               "Private Key",
		"method_keystore":                  "Keystore File",
		"imported_keystore":                "Keystore (Private Key)",
		"keystore_import_stage_validating": "Validating keystore file...",
		"keystore_import_stage_parsing":    "Parsing keystore structure...",
		"keystore_import_stage_decrypting": "Decrypting private key...",
		"keystore_import_stage_saving":     "Saving wallet...",
	}

	// Portuguese messages
	portuguese := map[string]string{
		"duplicate_mnemonic":               "Uma carteira com esta frase mnemônica já existe",
		"duplicate_private_key":            "Uma carteira com esta chave privada já existe",
		"no_mnemonic_available":            "Mnemônica não disponível (importada via chave privada)",
		"no_mnemonic_keystore":             "Mnemônica não disponível - importada de arquivo keystore",
		"guidance_duplicate_mnemonic":      "Você já importou esta mnemônica. Se pretendia outra carteira, verifique a frase e tente novamente.",
		"guidance_duplicate_private_key":   "Esta chave privada já foi importada. Abra a carteira existente em vez de importar novamente.",
		"method_label":                     "Método",
		"method_mnemonic":                  "Mnemônica",
		"method_private_key":               "Chave Privada",
		"method_keystore":                  "Arquivo Keystore",
		"imported_keystore":                "Keystore (Chave Privada)",
		"keystore_import_stage_validating": "Validando arquivo keystore...",
		"keystore_import_stage_parsing":    "Analisando estrutura do keystore...",
		"keystore_import_stage_decrypting": "Descriptografando chave privada...",
		"keystore_import_stage_saving":     "Salvando carteira...",
	}

	// Spanish messages (optional for consistency)
	spanish := map[string]string{
		"duplicate_mnemonic":               "Ya existe una cartera con esta frase mnemónica",
		"duplicate_private_key":            "Ya existe una cartera con esta clave privada",
		"no_mnemonic_available":            "Mnemónica no disponible (importada mediante clave privada)",
		"no_mnemonic_keystore":             "Mnemónica no disponible - importada desde archivo keystore",
		"guidance_duplicate_mnemonic":      "Ya importaste esta mnemónica. Si buscabas otra cartera, revisa la frase e inténtalo nuevamente.",
		"guidance_duplicate_private_key":   "Esta clave privada ya está importada. Abre la cartera existente en lugar de volver a importarla.",
		"method_label":                     "Método",
		"method_mnemonic":                  "Mnemónico",
		"method_private_key":               "Clave Privada",
		"method_keystore":                  "Archivo Keystore",
		"imported_keystore":                "Keystore (Clave Privada)",
		"keystore_import_stage_validating": "Validando archivo keystore...",
		"keystore_import_stage_parsing":    "Analizando estructura del keystore...",
		"keystore_import_stage_decrypting": "Descifrando clave privada...",
		"keystore_import_stage_saving":     "Guardando cartera...",
	}

	// Ensure the Labels map is initialized
	if Labels == nil {
		Labels = make(map[string]string)
	}

	// Always add English defaults first
	for k, v := range english {
		Labels[k] = v
	}

	// Override with the current language when applicable
	switch GetCurrentLanguage() {
	case "pt":
		for k, v := range portuguese {
			Labels[k] = v
		}
	case "es":
		for k, v := range spanish {
			Labels[k] = v
		}
	}
}

// GetWalletImportMessage returns a localized wallet-import message by key
func GetWalletImportMessage(key string) string {
	if Labels == nil {
		return key
	}
	if v, ok := Labels[key]; ok {
		return v
	}
	return key
}

// getMethodName resolves a localized import method display name
func getMethodName(importMethod string) string {
	key := ""
	switch importMethod {
	case "mnemonic":
		key = "method_mnemonic"
	case "private_key":
		key = "method_private_key"
	case "keystore":
		key = "method_keystore"
	default:
		return importMethod
	}
	if v, ok := Labels[key]; ok && v != "" {
		return v
	}
	return importMethod
}

// FormatDuplicateImportError builds a context-aware duplicate error message
// conflictType should be one of: "mnemonic", "private_key"
func FormatDuplicateImportError(importMethod, conflictType, address string) string {
	baseKey := ""
	guidanceKey := ""
	switch conflictType {
	case "mnemonic":
		baseKey = "duplicate_mnemonic"
		guidanceKey = "guidance_duplicate_mnemonic"
	case "private_key":
		baseKey = "duplicate_private_key"
		guidanceKey = "guidance_duplicate_private_key"
	default:
		baseKey = "unknown_error"
	}

	msg := GetWalletImportMessage(baseKey)

	// Add method context
	methodLabel := GetWalletImportMessage("method_label")
	methodName := getMethodName(importMethod)
	if methodLabel == "method_label" || methodLabel == "" {
		methodLabel = "Method"
	}
	msg = fmt.Sprintf("%s [%s: %s]", msg, methodLabel, methodName)

	// Add address context when provided
	if address != "" {
		msg = fmt.Sprintf("%s (%s)", msg, address)
	}

	// Add guidance if available
	if hint := GetWalletImportMessage(guidanceKey); hint != guidanceKey {
		msg = msg + " - " + hint
	}

	return msg
}

// GetNoMnemonicAvailableMessage returns the localized no-mnemonic-available message
func GetNoMnemonicAvailableMessage(importMethod string) string {
	// Return specific message based on import method
	switch importMethod {
	case "keystore":
		return GetWalletImportMessage("no_mnemonic_keystore")
	case "private_key":
		return GetWalletImportMessage("no_mnemonic_available")
	default:
		return GetWalletImportMessage("no_mnemonic_available")
	}
}

// GetKeystoreImportStageMessage returns the localized message for keystore import stages
func GetKeystoreImportStageMessage(stage string) string {
	key := "keystore_import_stage_" + stage
	return GetWalletImportMessage(key)
}
