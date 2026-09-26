package localization

// GetCurrentLanguage returns the current language code
func GetCurrentLanguage() string {
	st := ensureLocale()
	if st == nil {
		return "en"
	}
	return st.language
}

// SetCurrentLanguage sets the current language code
func SetCurrentLanguage(lang string) {
	cat, err := ensureCatalog()
	if err != nil {
		return
	}
	currentLocale.Store(stateForLanguage(cat, NormalizeLanguage(lang)))
}
