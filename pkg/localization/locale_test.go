package localization

import (
	"blocowallet/pkg/config"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAvailableLanguagesRejectsUnsafeFilenameCodes(t *testing.T) {
	if os.PathSeparator == '\\' {
		t.Skip("control characters are not portable in Windows filenames")
	}
	root := t.TempDir()
	unsafeCode := "xx\x1b]52;c;secret\x07"
	if err := os.WriteFile(filepath.Join(root, "language."+unsafeCode+".toml"), []byte(""), 0600); err != nil {
		t.Fatal(err)
	}
	for _, languageCode := range GetAvailableLanguages(root) {
		if strings.ContainsAny(languageCode, "\x1b\a\r\n") {
			t.Fatalf("unsafe language code was accepted: %q", languageCode)
		}
	}
	if name := GetLanguageName(unsafeCode); strings.ContainsAny(name, "\x1b\a\r\n") {
		t.Fatalf("unsafe language name was returned: %q", name)
	}
}

func TestInitLocalization(t *testing.T) {
	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "locale_test")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer func(path string) {
		err := os.RemoveAll(path)
		if err != nil {
			t.Fatalf("Failed to remove temp directory: %v", err)
		}
	}(tempDir)

	// Create a test config
	cfg := &config.Config{
		AppDir:    tempDir,
		Language:  "en",
		LocaleDir: filepath.Join(tempDir, "locale"),
	}

	// Initialize localization
	err = InitLocalization(cfg)
	if err != nil {
		t.Fatalf("InitLocalization failed: %v", err)
	}

	// Check if the locale directory was created
	if _, err := os.Stat(cfg.LocaleDir); os.IsNotExist(err) {
		t.Errorf("Locale directory was not created")
	}

	// Check if the default language files were created
	languages := []string{"en", "pt", "es"}
	for _, lang := range languages {
		filename := fmt.Sprintf("language.%s.toml", lang)
		filePath := filepath.Join(cfg.LocaleDir, filename)
		if _, err := os.Stat(filePath); os.IsNotExist(err) {
			t.Errorf("Language file %s was not created", filename)
		}
	}
}

func TestT(t *testing.T) {
	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "locale_test")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer func(path string) {
		err := os.RemoveAll(path)
		if err != nil {
			t.Fatalf("Failed to remove temp directory: %v", err)
		}
	}(tempDir)

	// Create a test config
	cfg := &config.Config{
		AppDir:    tempDir,
		Language:  "en",
		LocaleDir: filepath.Join(tempDir, "locale"),
	}

	// Initialize localization
	err = InitLocalization(cfg)
	if err != nil {
		t.Fatalf("InitLocalization failed: %v", err)
	}

	// Test translation without template data
	translation := T("welcome_message", nil)
	if translation == "" || translation == "welcome_message" {
		t.Errorf("Translation failed for welcome_message")
	}

	// Test translation with template data
	data := map[string]interface{}{
		"View": "Wallets",
	}
	translation = T("status_bar_instructions", data)
	if translation == "" || translation == "status_bar_instructions" {
		t.Errorf("Translation with template data failed for status_bar_instructions")
	}
	if translation == "View: {{.View}} | Press 'esc' to return | Press 'q' to quit" {
		t.Errorf("Template was not processed correctly")
	}
}

func TestTP(t *testing.T) {
	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "locale_test")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer func(path string) {
		err := os.RemoveAll(path)
		if err != nil {
			t.Fatalf("Failed to remove temp directory: %v", err)
		}
	}(tempDir)

	// Create a test config
	cfg := &config.Config{
		AppDir:    tempDir,
		Language:  "en",
		LocaleDir: filepath.Join(tempDir, "locale"),
	}

	// Initialize localization
	err = InitLocalization(cfg)
	if err != nil {
		t.Fatalf("InitLocalization failed: %v", err)
	}

	// Criar manualmente um arquivo de mensagem plural para teste
	pluralContent := `
# Plural test messages

[cats]
one = "{{.Name}} has {{.Count}} cat."
other = "{{.Name}} has {{.Count}} cats."

[apples]
one = "One apple"
other = "{{.Count}} apples"
`
	// Salvar o arquivo de mensagens plurais
	err = os.MkdirAll(cfg.LocaleDir, 0755)
	if err != nil {
		t.Fatalf("Failed to create locale directory: %v", err)
	}

	pluralFilePath := filepath.Join(cfg.LocaleDir, "plural_test.en.toml")
	err = os.WriteFile(pluralFilePath, []byte(pluralContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create plural test file: %v", err)
	}

	// Recarregar os arquivos de mensagem para incluir o novo arquivo plural
	err = loadMessageFiles(cfg.LocaleDir)
	if err != nil {
		t.Fatalf("Failed to reload message files: %v", err)
	}

	// Teste com contagem = 1 (singular)
	data := map[string]interface{}{
		"Name": "John",
	}

	translation := TP("cats", 1, data)
	if translation == "" || translation == "cats" {
		t.Errorf("Plural translation failed for cats with count=1, got: %s", translation)
	}

	expectedSingular := "John has 1 cat."
	if translation != expectedSingular {
		t.Errorf("Expected '%s' but got '%s'", expectedSingular, translation)
	}

	// Teste com contagem = 2 (plural)
	translation = TP("cats", 2, data)
	if translation == "" || translation == "cats" {
		t.Errorf("Plural translation failed for cats with count=2, got: %s", translation)
	}

	expectedPlural := "John has 2 cats."
	if translation != expectedPlural {
		t.Errorf("Expected '%s' but got '%s'", expectedPlural, translation)
	}

	// Teste com outro ID de mensagem
	translation = TP("apples", 1, nil)
	if translation != "One apple" {
		t.Errorf("Expected 'One apple' but got '%s'", translation)
	}

	translation = TP("apples", 5, nil)
	if translation != "5 apples" {
		t.Errorf("Expected '5 apples' but got '%s'", translation)
	}
}

func TestChangeLanguage(t *testing.T) {
	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "locale_test")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer func(path string) {
		err := os.RemoveAll(path)
		if err != nil {
			t.Fatalf("Failed to remove temp directory: %v", err)
		}
	}(tempDir)

	// Create a test config
	cfg := &config.Config{
		AppDir:    tempDir,
		Language:  "en",
		LocaleDir: filepath.Join(tempDir, "locale"),
	}

	// Initialize localization
	err = InitLocalization(cfg)
	if err != nil {
		t.Fatalf("InitLocalization failed: %v", err)
	}

	// Get English translation
	enTranslation := T("welcome_message", nil)

	// Change language to Portuguese
	ChangeLanguage("pt")

	// Get Portuguese translation
	ptTranslation := T("welcome_message", nil)

	// Translations should be different
	if enTranslation == ptTranslation {
		t.Errorf("Language change did not affect translations")
	}

	// Change language to Spanish
	ChangeLanguage("es")

	// Get Spanish translation
	esTranslation := T("welcome_message", nil)

	// Translations should be different
	if enTranslation == esTranslation || ptTranslation == esTranslation {
		t.Errorf("Language change did not affect translations")
	}
}
