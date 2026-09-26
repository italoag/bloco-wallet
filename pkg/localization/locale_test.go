package localization

import (
	"context"
	"errors"
	"fmt"

	"blocowallet/pkg/config"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/nicksnyder/go-i18n/v2/i18n"
)

func useLang(t *testing.T, lang string) {
	t.Helper()
	previous := GetCurrentLanguage()
	SetCurrentLanguage(lang)
	t.Cleanup(func() { SetCurrentLanguage(previous) })
}

func TestAvailableLanguagesRejectsUnsafeFilenameCodes(t *testing.T) {
	unsafeCode := "xx\x1b]52;c;secret\x07"
	for _, languageCode := range GetAvailableLanguages(t.TempDir()) {
		if strings.ContainsAny(languageCode, "\x1b\a\r\n") {
			t.Fatalf("unsafe language code was accepted: %q", languageCode)
		}
	}
	if name := GetLanguageName(unsafeCode); strings.ContainsAny(name, "\x1b\a\r\n") {
		t.Fatalf("unsafe language name was returned: %q", name)
	}
}

func TestInitLocalizationUsesEmbeddedCatalogsOnly(t *testing.T) {
	tempDir := t.TempDir()
	cfg := &config.Config{
		AppDir:    tempDir,
		Language:  "en",
		LocaleDir: filepath.Join(tempDir, "locale", "does", "not", "exist"),
	}

	if err := InitLocalization(cfg); err != nil {
		t.Fatalf("InitLocalization failed: %v", err)
	}
	if _, err := os.Stat(cfg.LocaleDir); !os.IsNotExist(err) {
		t.Fatalf("InitLocalization must not create the locale dir, stat err=%v", err)
	}

	// A conflicting on-disk catalog must be ignored entirely.
	conflicting := []byte("[welcome_message]\nother = \"EVIL ON DISK\"\n")
	if err := os.WriteFile(filepath.Join(tempDir, "language.en.toml"), conflicting, 0600); err != nil {
		t.Fatal(err)
	}
	if err := InitLocalization(&config.Config{Language: "en", LocaleDir: tempDir}); err != nil {
		t.Fatal(err)
	}
	if got := Get("welcome_message"); strings.Contains(got, "EVIL") || got == "" {
		t.Fatalf("disk catalog leaked into localization: %q", got)
	}
	raw, _ := os.ReadFile(filepath.Join(tempDir, "language.en.toml"))
	if string(raw) != string(conflicting) {
		t.Fatal("InitLocalization modified a user file")
	}
}

func TestInitLocalizationNilConfig(t *testing.T) {
	previous := GetCurrentLanguage()
	if err := InitLocalization(nil); err == nil {
		t.Fatal("expected error for nil config")
	}
	if GetCurrentLanguage() != previous {
		t.Fatal("nil config changed the current locale")
	}
}

func TestGetBeforeInitReturnsEnglish(t *testing.T) {
	previous := currentLocale.Swap(nil)
	t.Cleanup(func() { currentLocale.Store(previous) })
	got := Get("wallet_details_title")
	if got != "Wallet Details" {
		t.Fatalf("Get before Init returned %q", got)
	}
}

func TestT(t *testing.T) {
	useLang(t, "en")

	translation := T("welcome_message", nil)
	if translation == "" || translation == "welcome_message" {
		t.Errorf("Translation failed for welcome_message")
	}
	if !strings.Contains(translation, "\n\n") {
		t.Errorf("multi-line welcome_message lost its paragraph break: %q", translation)
	}

	translation = T("status_bar_instructions", map[string]interface{}{"View": "Wallets"})
	if !strings.Contains(translation, "Wallets") || strings.Contains(translation, "{{.View}}") {
		t.Errorf("Template was not processed correctly: %q", translation)
	}
}

func TestTDoesNotReparseInjectedTemplates(t *testing.T) {
	useLang(t, "en")
	got := T("status_bar_instructions", map[string]interface{}{"View": "{{.Other}}"})
	if !strings.Contains(got, "{{.Other}}") {
		t.Fatalf("injected template text was re-parsed or mangled: %q", got)
	}
	got = T("status_bar_instructions", map[string]interface{}{"View": "\x1b[31mOwned\x1b[0m"})
	if strings.Contains(got, "\x1b") {
		t.Fatalf("ANSI controls leaked through T: %q", got)
	}
	if got := T("unknown_key_xyz", nil); got != "unknown_key_xyz" {
		t.Fatalf("unknown key should return bounded key, got %q", got)
	}
}

func TestTMissingParamsFailsClosed(t *testing.T) {
	useLang(t, "en")
	for _, got := range []string{
		T("error", nil),
		T("error", map[string]interface{}{}),
		T("error", map[string]interface{}{"Other": "x"}),
		T("status_bar_instructions", nil),
	} {
		if strings.Contains(got, "<no value>") {
			t.Fatalf("missing param leaked '<no value>': %q", got)
		}
		if got != "error" && got != "status_bar_instructions" {
			t.Fatalf("expected bounded key on missing params, got %q", got)
		}
	}
}

func TestTDecodesRealNewlines(t *testing.T) {
	data := map[string]interface{}{"Name": "acct", "Address": "0xabc", "ID": "1"}
	for _, lang := range []string{"en", "pt", "es"} {
		useLang(t, lang)
		out := T("delete_account_lines", data)
		if strings.Contains(out, `\n`) {
			t.Fatalf("%s: literal backslash-n in output: %q", lang, out)
		}
		if lines := strings.Count(out, "\n"); lines != 2 {
			t.Fatalf("%s: expected 3 physical lines, got %d: %q", lang, lines+1, out)
		}
		useLang(t, lang)
		preview := T("call_preview_body", map[string]interface{}{
			"Contract": "0x1", "Method": "transfer", "Source": "abi",
			"Hash": "0x0", "Value": "0", "Output": "ok", "Calldata": "deadbeef",
		})
		if strings.Contains(preview, `\n`) || !strings.Contains(preview, "\n") {
			t.Fatalf("%s: call_preview_body not multi-line: %q", lang, preview)
		}
		useLang(t, lang)
		tx := T("tx_native_line", map[string]interface{}{
			"To": "0x2", "Wei": "1", "Amount": "0", "Symbol": "ETH",
		})
		if strings.Contains(tx, `\n`) || !strings.Contains(tx, "\n") {
			t.Fatalf("%s: tx_native_line not multi-line: %q", lang, tx)
		}
	}
}

func TestTP(t *testing.T) {
	useLang(t, "en")

	data := map[string]interface{}{"Unused": "x"}
	if got := TP("wallet_count", 1, data); got != "1 wallet" {
		t.Fatalf("TP singular = %q", got)
	}
	if got := TP("wallet_count", 2, data); got != "2 wallets" {
		t.Fatalf("TP plural = %q", got)
	}
	if got := TP("wallet_count", 0, data); got != "0 wallets" {
		t.Fatalf("TP zero = %q", got)
	}
	if _, mutated := data["Count"]; mutated {
		t.Fatal("TP mutated the caller's data map")
	}
	if got := TP("wallet_count", 3, nil); got != "3 wallets" {
		t.Fatalf("TP with nil data = %q", got)
	}

	useLang(t, "pt")
	if got := TP("wallet_count", 1, nil); got != "1 carteira" {
		t.Fatalf("TP pt singular = %q", got)
	}
	useLang(t, "es")
	if got := TP("wallet_count", 5, nil); got != "5 carteras" {
		t.Fatalf("TP es plural = %q", got)
	}
}

func TestChangeLanguageCoherence(t *testing.T) {
	useLang(t, "en")

	enTranslation := T("welcome_message", nil)

	ChangeLanguage("pt")
	if GetCurrentLanguage() != "pt" {
		t.Fatalf("ChangeLanguage did not update current language: %q", GetCurrentLanguage())
	}
	ptTranslation := T("welcome_message", nil)
	if enTranslation == ptTranslation {
		t.Errorf("Language change did not affect translations")
	}
	if got := GetWalletImportMessage("duplicate_mnemonic"); got != "Uma carteira com esta frase mnemônica já existe" {
		t.Fatalf("ChangeLanguage served stale English after switching to pt: %q", got)
	}

	ChangeLanguage("es")
	esTranslation := T("welcome_message", nil)
	if enTranslation == esTranslation || ptTranslation == esTranslation {
		t.Errorf("Language change did not affect translations")
	}
}

func TestRegionalAliasesAndUnknownFallback(t *testing.T) {
	previous := GetCurrentLanguage()
	defer SetCurrentLanguage(previous)

	for _, tc := range []struct{ in, want string }{
		{"en-US", "en"}, {"PT_br", "pt"}, {"pt-BR", "pt"}, {"es-ES", "es"},
		{"fr", "en"}, {"bogus!!", "en"}, {"", "en"},
	} {
		if got := NormalizeLanguage(tc.in); got != tc.want {
			t.Errorf("NormalizeLanguage(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}

	SetCurrentLanguage("pt-BR")
	if GetCurrentLanguage() != "pt" || Get("wallets") != "Carteiras" {
		t.Fatalf("pt-BR did not resolve to Portuguese")
	}
	SetCurrentLanguage("fr")
	if GetCurrentLanguage() != "en" || Get("wallets") != "Wallets" {
		t.Fatalf("unknown language did not fall back to English")
	}
}

func TestMessagesReturnsIndependentCopy(t *testing.T) {
	msgs := Messages("en")
	if msgs["wallet_details_title"] != "Wallet Details" {
		t.Fatalf("Messages(en) missing keys: %q", msgs["wallet_details_title"])
	}
	msgs["wallet_details_title"] = "MUTATED"
	useLang(t, "en")
	if Get("wallet_details_title") != "Wallet Details" {
		t.Fatal("mutating Messages copy changed the catalog")
	}
}

func TestGetAvailableLanguagesCopy(t *testing.T) {
	langs := GetAvailableLanguages("")
	want := []string{"en", "es", "pt"}
	if len(langs) != len(want) {
		t.Fatalf("GetAvailableLanguages = %v", langs)
	}
	for i := range want {
		if langs[i] != want[i] {
			t.Fatalf("GetAvailableLanguages = %v", langs)
		}
	}
	langs[0] = "zz"
	if GetAvailableLanguages("")[0] != "en" {
		t.Fatal("mutating the returned slice changed the catalog")
	}
}

func TestGetLanguageNameSelfDeclared(t *testing.T) {
	for _, tc := range []struct{ code, want string }{
		{"en", "English"}, {"pt", "Português"}, {"es", "Español"},
	} {
		if got := GetLanguageName(tc.code); got != tc.want {
			t.Errorf("GetLanguageName(%q) = %q, want %q", tc.code, got, tc.want)
		}
	}
	if name := GetLanguageName("fr"); name == "English" {
		t.Fatalf("unknown language code must not claim English, got %q", name)
	}
	if got := GetLanguageName("xx\x1by"); strings.ContainsAny(got, "\x1b") {
		t.Fatalf("unsafe name returned: %q", got)
	}
}

func TestConcurrentLocaleAccess(t *testing.T) {
	previous := GetCurrentLanguage()
	defer SetCurrentLanguage(previous)

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			langs := []string{"en", "pt", "es"}
			validWelcome := map[string]bool{
				"Welcome to the BLOCO Wallet!\n\nSelect an option from the menu.":                    true,
				"Bem-vindo ao Gerenciador de Carteiras BLOCO!\n\nSelecione uma opção do menu.":       true,
				"¡Bienvenido al Administrador de Carteras BLOCO!\n\nSeleccione una opción del menú.": true,
			}
			validPlurals := map[string]bool{
				"2 wallets": true, "2 carteiras": true, "2 carteras": true,
			}
			sharedData := map[string]interface{}{"Marker": "readonly"}
			for j := 0; j < 50; j++ {
				lang := langs[(i+j)%len(langs)]
				ChangeLanguage(lang)
				got := T("welcome_message", nil)
				if !validWelcome[got] {
					t.Errorf("incoherent or missing translation: %q", got)
					return
				}
				if tp := TP("wallet_count", 2, sharedData); !validPlurals[tp] {
					t.Errorf("incoherent plural: %q", tp)
					return
				}
				if _, mutated := sharedData["Count"]; mutated {
					t.Error("TP mutated the shared caller map")
					return
				}
				_ = Get("wallets")
			}
		}(i)
	}
	wg.Wait()
}

// loadCatalog error paths and per-language fallback are exercised against an
// in-memory FS, never runtime user files.
func TestLoadCatalogRejectsInvalidSources(t *testing.T) {
	if _, err := loadCatalog(fstest.MapFS{}); err == nil {
		t.Fatal("expected error for missing locales dir")
	}
	for name, fsys := range map[string]fstest.MapFS{
		"dup":         {"locales/language.en.toml": {Data: []byte("[a]\nother=\"1\"\n[a]\nother=\"2\"\n")}},
		"empty":       {"locales/language.en.toml": {Data: []byte("[a]\nother=\"\"\n")}},
		"badtemplate": {"locales/language.en.toml": {Data: []byte("[a]\nother=\"{{.X\"\n")}},
		"badname":     {"locales/language.@@@.toml": {Data: []byte("[a]\nother=\"x\"\n")}},
		"badfield":    {"locales/language.en.toml": {Data: []byte("[a]\nother=\"x\"\nplural=\"y\"\n")}},
		"emptyid":     {"locales/language.en.toml": {Data: []byte("[ ]\nother=\"x\"\n")}},
		"dupalias": {
			"locales/language.en-us.toml": {Data: []byte("[a]\nother=\"x\"\n")},
			"locales/language.en-US.toml": {Data: []byte("[a]\nother=\"x\"\n")},
		},
	} {
		if _, err := loadCatalog(fsys); err == nil {
			t.Fatalf("loadCatalog accepted invalid source %q", name)
		}
	}
}

func TestLoadCatalogFallbackPerLanguage(t *testing.T) {
	fsys := fstest.MapFS{
		"locales/language.en.toml": {Data: []byte("[language_name]\nother=\"English\"\n[only_en]\nother=\"Only English\"\n[shared]\nother=\"Shared\"\n")},
		"locales/language.pt.toml": {Data: []byte("[language_name]\nother=\"Português\"\n[shared]\nother=\"Partilhado\"\n")},
	}
	cat, err := loadCatalog(fsys)
	if err != nil {
		t.Fatal(err)
	}
	st := stateForLanguage(cat, "pt")
	if got := getWithState(st, "only_en"); got != "Only English" {
		t.Fatalf("getWithState lost English fallback: %q", got)
	}
	if got := getWithState(st, "shared"); got != "Partilhado" {
		t.Fatalf("expected pt static, got %q", got)
	}
	merged := cat.mergedStatic("pt")
	if merged["only_en"] != "Only English" || merged["shared"] != "Partilhado" {
		t.Fatalf("merged static wrong: %v", merged)
	}
	merged["only_en"] = "mutated"
	if cat.mergedStatic("pt")["only_en"] == "mutated" {
		t.Fatal("mergedStatic returned shared mutable state")
	}
	// missing in pt: go-i18n returns en text with MessageNotFoundErr; keep it.
	msg, ok := translateWithState(st, &i18n.LocalizeConfig{MessageID: "only_en"})
	if !ok || msg != "Only English" {
		t.Fatalf("per-language fallback dropped valid message: %q ok=%v", msg, ok)
	}
	msg, ok = translateWithState(st, &i18n.LocalizeConfig{MessageID: "shared"})
	if !ok || msg != "Partilhado" {
		t.Fatalf("expected pt message, got %q", msg)
	}
}

func TestWrapErrorPreservesCause(t *testing.T) {
	if got := WrapError("net_err_load_config", nil); got != nil {
		t.Fatalf("nil cause must return nil, got %v", got)
	}
	for _, lang := range []string{"en", "pt", "es"} {
		useLang(t, lang)
		cause := context.Canceled
		err := WrapError("net_err_load_config", cause)
		if err == nil {
			t.Fatalf("%s: expected wrapped error", lang)
		}
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("%s: errors.Is lost the cause: %v", lang, err)
		}
		nested := fmt.Errorf("outer: %w", &os.PathError{Op: "open", Path: "/x", Err: errors.New("denied")})
		wrapped := WrapError("net_err_load_config", nested)
		var pathErr *os.PathError
		if !errors.As(wrapped, &pathErr) {
			t.Fatalf("%s: errors.As lost nested cause: %v", lang, wrapped)
		}
		if wrapped.Error() == "" || strings.Contains(wrapped.Error(), "{{") {
			t.Fatalf("%s: localized message not rendered: %q", lang, wrapped.Error())
		}
	}
	useLang(t, "pt")
	err := WrapError("net_err_load_config", context.Canceled)
	if !strings.HasPrefix(err.Error(), "Falha") {
		t.Fatalf("expected pt prefix, got %q", err.Error())
	}
}
