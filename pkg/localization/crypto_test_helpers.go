package localization

// InitCryptoMessagesForTesting initializes crypto messages for unit tests
// without depending on language files or Viper. This function also cleans any
// previous global state to avoid contamination between tests (e.g., previous language
// and residual keys in the Labels map).
func InitCryptoMessagesForTesting() {
	// Force the default test language to English for predictability
	SetCurrentLanguage("en")

	// Reinitialize global map to avoid key leakage between tests
	Labels = make(map[string]string)

	// Add all crypto/base messages in English for use in tests
	for key, value := range DefaultCryptoMessages() {
		Labels[key] = value
	}
}

// GetForTesting returns the message directly from the Labels map for testing
// This function is a simplified version of the Get function for use in tests
func GetForTesting(key string) string {
	if Labels == nil {
		return key
	}

	if value, ok := Labels[key]; ok {
		return value
	}
	return key
}
