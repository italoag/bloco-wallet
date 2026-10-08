package localization

// InitCryptoMessagesForTesting switches the locale to English for unit tests
// without depending on config files or Viper.
func InitCryptoMessagesForTesting() {
	SetCurrentLanguage("en")
}

// GetForTesting returns the localized message for use in tests
func GetForTesting(key string) string {
	return Get(key)
}
