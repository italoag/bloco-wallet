package localization

// SetLanguage sets the current language. The appDir parameter is retained for
// backward compatibility and ignored: catalogs are embedded.
func SetLanguage(lang string, appDir string) error {
	SetCurrentLanguage(lang)
	return nil
}
