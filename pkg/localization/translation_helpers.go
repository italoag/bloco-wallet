package localization

// snapshot returns one coherent locale snapshot, or nil when the catalog is
// unavailable.
func snapshot() *localeState {
	return ensureLocale()
}

// GetKeystoreErrorMessage returns a localized error message for a keystore error key
func GetKeystoreErrorMessage(key string) string {
	return Get(key)
}

// FormatKeystoreErrorWithField formats a keystore error message with a field name
func FormatKeystoreErrorWithField(key string, field string) string {
	st := snapshot()
	if st == nil {
		return safeMessageID(key)
	}
	message := getWithState(st, key)
	if field == "" {
		return message
	}
	if out, ok := translateWithState(st, templateConfig("message_with_field", map[string]interface{}{"Message": message, "Field": field})); ok {
		return out
	}
	return message
}

// GetPasswordFileErrorMessage returns a localized error message for a password file error key
func GetPasswordFileErrorMessage(key string) string {
	return Get(key)
}

// FormatPasswordFileErrorWithFile formats a password file error message with a file name
func FormatPasswordFileErrorWithFile(key string, file string) string {
	st := snapshot()
	if st == nil {
		return safeMessageID(key)
	}
	message := getWithState(st, key)
	if file == "" {
		return message
	}
	if out, ok := translateWithState(st, templateConfig("message_with_file", map[string]interface{}{"Message": message, "File": file})); ok {
		return out
	}
	return message
}

// GetWalletImportMessage returns a localized wallet-import message by key
func GetWalletImportMessage(key string) string {
	return Get(key)
}

// getMethodName resolves a localized import method display name
func getMethodName(st *localeState, importMethod string) string {
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
	if v := getWithState(st, key); v != key && v != "" {
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

	st := snapshot()
	if st == nil {
		return baseKey
	}
	data := map[string]interface{}{
		"Message":     getWithState(st, baseKey),
		"MethodLabel": getWithState(st, "method_label"),
		"Method":      getMethodName(st, importMethod),
		"Address":     address,
		"Guidance":    "",
	}
	if guidanceKey != "" {
		if hint := getWithState(st, guidanceKey); hint != guidanceKey {
			data["Guidance"] = hint
		}
	}
	if out, ok := translateWithState(st, templateConfig("duplicate_import_detail", data)); ok {
		return out
	}
	return data["Message"].(string)
}

// GetNoMnemonicAvailableMessage returns the localized no-mnemonic-available message
func GetNoMnemonicAvailableMessage(importMethod string) string {
	// Return specific message based on import method
	switch importMethod {
	case "keystore":
		return Get("no_mnemonic_keystore")
	default:
		return Get("no_mnemonic_available")
	}
}

// GetKeystoreImportStageMessage returns the localized message for keystore import stages
func GetKeystoreImportStageMessage(stage string) string {
	return Get("keystore_import_stage_" + stage)
}

// GetEnhancedImportErrorMessage returns a localized error message for enhanced import errors
func GetEnhancedImportErrorMessage(key string) string {
	return Get(key)
}

// translatedError preserves an error's causal chain while presenting a
// localized message.
type translatedError struct {
	message string
	cause   error
}

func (err *translatedError) Error() string { return err.message }
func (err *translatedError) Unwrap() error { return err.cause }

// WrapError returns an error whose message is the localized messageID with the
// cause's text interpolated, while preserving errors.Is/As on the cause.
func WrapError(messageID string, cause error) error {
	if cause == nil {
		return nil
	}
	return &translatedError{
		message: T(messageID, map[string]interface{}{"Err": cause.Error()}),
		cause:   cause,
	}
}

// FormatErrorWithRecoveryHint formats an error message with recovery hint
func FormatErrorWithRecoveryHint(errorKey string, recoveryKey string) string {
	st := snapshot()
	if st == nil {
		return errorKey
	}
	errorMsg := getWithState(st, errorKey)
	recoveryMsg := getWithState(st, recoveryKey)
	if recoveryMsg != recoveryKey {
		return errorMsg + "\n" + recoveryMsg
	}
	return errorMsg
}
