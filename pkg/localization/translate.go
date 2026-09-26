package localization

import (
	"errors"

	"blocowallet/internal/terminal"

	"github.com/nicksnyder/go-i18n/v2/i18n"
	i18nTemplate "github.com/nicksnyder/go-i18n/v2/i18n/template"
)

// strictTemplateParser fails template execution when a required variable is
// absent so localized output never emits "<no value>" or partial renders.
var strictTemplateParser i18nTemplate.Parser = &i18nTemplate.TextParser{Option: "missingkey=error"}

// sanitizeTemplateOutput strips terminal control sequences from a rendered
// template while preserving line structure.
func sanitizeTemplateOutput(value string) string {
	return terminal.SanitizeBlock(value, 64, 8192)
}

// templateConfig builds a LocalizeConfig bound to the strict parser.
func templateConfig(messageID string, data map[string]interface{}) *i18n.LocalizeConfig {
	return &i18n.LocalizeConfig{
		MessageID:      messageID,
		TemplateData:   data,
		TemplateParser: strictTemplateParser,
	}
}

// translateWithState renders cfg inside a single locale snapshot, retrying with
// the snapshot's own English localizer as an explicit fallback. A valid message
// returned alongside *i18n.MessageNotFoundErr is preserved.
func translateWithState(st *localeState, cfg *i18n.LocalizeConfig) (string, bool) {
	cfg.TemplateParser = strictTemplateParser
	msg, err := st.localizer.Localize(cfg)
	if err == nil {
		return sanitizeTemplateOutput(msg), true
	}
	var notFound *i18n.MessageNotFoundErr
	if !errors.As(err, &notFound) {
		return "", false
	}
	if msg != "" {
		// go-i18n returns the fallback-language message together with
		// MessageNotFoundErr; keep it.
		return sanitizeTemplateOutput(msg), true
	}
	if enMsg, enErr := st.enLocalizer.Localize(cfg); enErr == nil {
		return sanitizeTemplateOutput(enMsg), true
	}
	return "", false
}

// T returns the translated message for the specified ID
// data is a map of template variables that will be substituted in the message
func T(messageID string, data map[string]interface{}) string {
	st := ensureLocale()
	if st == nil {
		return safeMessageID(messageID)
	}
	msg, ok := translateWithState(st, &i18n.LocalizeConfig{
		MessageID:    messageID,
		TemplateData: data,
	})
	if !ok {
		return safeMessageID(messageID)
	}
	return msg
}

// TP returns the plural-translated message for the specified ID
// count is the number that determines which plural form to use
// data is a map of template variables that will be substituted in the message
func TP(messageID string, count interface{}, data map[string]interface{}) string {
	st := ensureLocale()
	if st == nil {
		return safeMessageID(messageID)
	}

	cloned := make(map[string]interface{}, len(data)+1)
	for k, v := range data {
		cloned[k] = v
	}
	cloned["Count"] = count

	msg, ok := translateWithState(st, &i18n.LocalizeConfig{
		MessageID:    messageID,
		PluralCount:  count,
		TemplateData: cloned,
	})
	if !ok {
		return safeMessageID(messageID)
	}
	return msg
}

// ChangeLanguage changes the active language
func ChangeLanguage(lang string) {
	SetCurrentLanguage(lang)
}
