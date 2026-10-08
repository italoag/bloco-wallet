package localization

import (
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"text/template"

	"blocowallet/internal/terminal"
	"blocowallet/pkg/config"

	"github.com/BurntSushi/toml"
	"github.com/nicksnyder/go-i18n/v2/i18n"
	"golang.org/x/text/language"
)

//go:embed locales/language.*.toml
var embeddedLocales embed.FS

type catalog struct {
	bundle     *i18n.Bundle
	languages  []string
	localizers map[string]*i18n.Localizer
	messages   map[string]map[string]*i18n.Message
	static     map[string]map[string]string
}

type localeState struct {
	language    string
	localizer   *i18n.Localizer
	static      map[string]string
	enLocalizer *i18n.Localizer
	enStatic    map[string]string
}

var (
	catalogOnce        sync.Once
	embeddedCatalog    *catalog
	embeddedCatalogErr error
	currentLocale      atomic.Pointer[localeState]
)

// loadCatalog parses every bundled language.<code>.toml catalog from source and
// builds an immutable catalog. TOML decoding rejects duplicate message IDs;
// empty translations and syntactically invalid templates are rejected here.
func loadCatalog(source fs.FS) (*catalog, error) {
	entries, err := fs.ReadDir(source, "locales")
	if err != nil {
		return nil, fmt.Errorf("failed to read embedded locales: %w", err)
	}

	bundle := i18n.NewBundle(language.English)
	bundle.RegisterUnmarshalFunc("toml", toml.Unmarshal)

	cat := &catalog{
		bundle:     bundle,
		localizers: map[string]*i18n.Localizer{},
		messages:   map[string]map[string]*i18n.Message{},
		static:     map[string]map[string]string{},
	}

	seenTags := map[string]string{}
	allowedForms := map[string]bool{
		"zero": true, "one": true, "two": true, "few": true, "many": true, "other": true,
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasPrefix(name, "language.") || !strings.HasSuffix(name, ".toml") {
			continue
		}
		code := strings.TrimSuffix(strings.TrimPrefix(name, "language."), ".toml")
		tag, err := language.Parse(code)
		if err != nil {
			return nil, fmt.Errorf("invalid locale file name %s: %w", name, err)
		}
		canonical := tag.String()
		if prev, dup := seenTags[canonical]; dup {
			return nil, fmt.Errorf("locale files %s and %s resolve to the same language tag %q", prev, name, canonical)
		}
		seenTags[canonical] = name

		raw, err := fs.ReadFile(source, "locales/"+name)
		if err != nil {
			return nil, fmt.Errorf("failed to read locale file %s: %w", name, err)
		}
		var decoded map[string]map[string]string
		if _, err := toml.Decode(string(raw), &decoded); err != nil {
			return nil, fmt.Errorf("failed to parse locale file %s: %w", name, err)
		}
		if len(decoded) == 0 {
			return nil, fmt.Errorf("locale file %s has no messages", name)
		}

		lang := canonical
		msgs := make([]*i18n.Message, 0, len(decoded))
		rawTexts := make(map[string]string, len(decoded))
		msgByID := make(map[string]*i18n.Message, len(decoded))
		for id, forms := range decoded {
			if strings.TrimSpace(id) == "" {
				return nil, fmt.Errorf("locale %s contains an empty message ID", lang)
			}
			for form := range forms {
				if !allowedForms[form] {
					return nil, fmt.Errorf("locale %s message %q has unknown plural form %q", lang, id, form)
				}
			}
			text, ok := forms["other"]
			if !ok || strings.TrimSpace(text) == "" {
				return nil, fmt.Errorf("locale %s message %q is missing a non-empty \"other\" form", lang, id)
			}
			msg := &i18n.Message{ID: id, Other: text}
			if v, ok := forms["zero"]; ok {
				msg.Zero = v
			}
			if v, ok := forms["one"]; ok {
				msg.One = v
			}
			if v, ok := forms["two"]; ok {
				msg.Two = v
			}
			if v, ok := forms["few"]; ok {
				msg.Few = v
			}
			if v, ok := forms["many"]; ok {
				msg.Many = v
			}
			for _, form := range forms {
				if strings.TrimSpace(form) == "" {
					return nil, fmt.Errorf("locale %s message %q has an empty plural form", lang, id)
				}
				if _, err := template.New(id).Parse(form); err != nil {
					return nil, fmt.Errorf("locale %s message %q has an invalid template: %w", lang, id, err)
				}
			}
			msgs = append(msgs, msg)
			msgByID[id] = msg
			rawTexts[id] = terminal.SanitizeBlock(text, 64, 8192)
		}
		if err := bundle.AddMessages(tag, msgs...); err != nil {
			return nil, fmt.Errorf("failed to register locale %s: %w", lang, err)
		}
		cat.languages = append(cat.languages, lang)
		cat.localizers[lang] = i18n.NewLocalizer(bundle, lang)
		cat.messages[lang] = msgByID
		cat.static[lang] = rawTexts
	}

	sort.Strings(cat.languages)
	if len(cat.languages) == 0 || cat.localizers["en"] == nil {
		return nil, fmt.Errorf("embedded catalogs must include language.en.toml")
	}
	return cat, nil
}

func ensureCatalog() (*catalog, error) {
	catalogOnce.Do(func() {
		embeddedCatalog, embeddedCatalogErr = loadCatalog(embeddedLocales)
	})
	return embeddedCatalog, embeddedCatalogErr
}

// stateForLanguage resolves a normalized language code into a locale snapshot,
// falling back to English for unknown languages.
func stateForLanguage(cat *catalog, code string) *localeState {
	st := &localeState{
		enLocalizer: cat.localizers["en"],
		enStatic:    cat.static["en"],
	}
	if st.enLocalizer == nil {
		return nil
	}
	if loc := cat.localizers[code]; loc != nil {
		st.language = code
		st.localizer = loc
		st.static = cat.static[code]
	} else {
		st.language = "en"
		st.localizer = st.enLocalizer
		st.static = st.enStatic
	}
	return st
}

// ensureLocale lazily publishes the default English snapshot without
// overwriting a concurrently selected language.
func ensureLocale() *localeState {
	if st := currentLocale.Load(); st != nil {
		return st
	}
	cat, err := ensureCatalog()
	if err != nil {
		return nil
	}
	st := stateForLanguage(cat, "en")
	currentLocale.CompareAndSwap(nil, st)
	return currentLocale.Load()
}

// InitLocalization initializes the embedded catalog and selects the configured language.
func InitLocalization(cfg *config.Config) error {
	if cfg == nil {
		return fmt.Errorf("localization: nil config")
	}
	cat, err := ensureCatalog()
	if err != nil {
		return err
	}
	currentLocale.Store(stateForLanguage(cat, NormalizeLanguage(cfg.Language)))
	return nil
}

// NormalizeLanguage resolves a language marker (underscore/hyphen/case
// variations, regional tags) to an official catalog code, falling back to "en".
func NormalizeLanguage(code string) string {
	tag, err := language.Parse(strings.ReplaceAll(strings.TrimSpace(code), "_", "-"))
	if err != nil {
		return "en"
	}
	cat, err := ensureCatalog()
	if err != nil {
		return "en"
	}
	if cat.localizers[tag.String()] != nil {
		return tag.String()
	}
	if base, _ := tag.Base(); cat.localizers[base.String()] != nil {
		return base.String()
	}
	return "en"
}

func safeMessageID(key string) string {
	return terminal.SanitizeInline(key, 128)
}

// getWithState resolves a raw message text inside a single locale snapshot,
// falling back to that snapshot's own English catalog.
func getWithState(st *localeState, key string) string {
	if value, ok := st.static[key]; ok {
		return value
	}
	if value, ok := st.enStatic[key]; ok {
		return value
	}
	return safeMessageID(key)
}

// Get retorna a mensagem localizada para a chave especificada
func Get(key string) string {
	st := ensureLocale()
	if st == nil {
		return safeMessageID(key)
	}
	return getWithState(st, key)
}

// mergedStatic merges the English catalog under the chosen locale's raw texts
// into an independent map.
func (cat *catalog) mergedStatic(lang string) map[string]string {
	src := cat.static[lang]
	if src == nil {
		src = cat.static["en"]
	}
	out := make(map[string]string, len(cat.static["en"]))
	for k, v := range cat.static["en"] {
		out[k] = v
	}
	for k, v := range src {
		out[k] = v
	}
	return out
}

// Messages returns an independent copy of the resolved raw message texts for a
// locale (English fallback merged under the chosen locale), for
// compatibility/read-only inspection.
func Messages(lang string) map[string]string {
	cat, err := ensureCatalog()
	if err != nil {
		return map[string]string{}
	}
	return cat.mergedStatic(NormalizeLanguage(lang))
}

// GetAvailableLanguages returns the official bundled language codes.
func GetAvailableLanguages(_ string) []string {
	cat, err := ensureCatalog()
	if err != nil {
		return []string{"en"}
	}
	out := make([]string, len(cat.languages))
	copy(out, cat.languages)
	return out
}

// GetLanguageName returns the self-declared display name of a language, or a
// sanitized bounded code when the language is not bundled.
func GetLanguageName(code string) string {
	cat, err := ensureCatalog()
	if err != nil {
		return safeMessageID(code)
	}
	tag, parseErr := language.Parse(strings.ReplaceAll(strings.TrimSpace(code), "_", "-"))
	if parseErr != nil {
		return safeMessageID(code)
	}
	lookup := ""
	if cat.localizers[tag.String()] != nil {
		lookup = tag.String()
	} else if base, _ := tag.Base(); cat.localizers[base.String()] != nil {
		lookup = base.String()
	}
	if name, ok := cat.static[lookup]["language_name"]; lookup != "" && ok && name != "" {
		return name
	}
	return safeMessageID(code)
}
