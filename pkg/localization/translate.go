package localization

import (
	"fmt"

	"github.com/nicksnyder/go-i18n/v2/i18n"
)

// T returns the translated message for the specified ID
// data is a map of template variables that will be substituted in the message
func T(messageID string, data map[string]interface{}) string {
	if localizer == nil {
		return messageID
	}

	msg, err := localizer.Localize(&i18n.LocalizeConfig{
		MessageID:    messageID,
		TemplateData: data,
	})

	if err != nil {
		return messageID
	}

	return msg
}

// TP returns the plural-translated message for the specified ID
// count is the number that determines which plural form to use
// data is a map of template variables that will be substituted in the message
func TP(messageID string, count interface{}, data map[string]interface{}) string {
	if localizer == nil {
		return messageID
	}

	// If data is nil, initialize it
	if data == nil {
		data = make(map[string]interface{})
	}

	// Add the count to the template data
	data["Count"] = count

	msg, err := localizer.Localize(&i18n.LocalizeConfig{
		MessageID:    messageID,
		PluralCount:  count,
		TemplateData: data,
	})

	if err != nil {
		return fmt.Sprintf("%s (error: %v)", messageID, err)
	}

	return msg
}

// ChangeLanguage changes the localizer language
func ChangeLanguage(lang string) {
	if bundle == nil {
		return
	}

	localizer = i18n.NewLocalizer(bundle, lang)

	// Update the global Labels map to reflect the new language
	err := populateLabelsMap()
	if err != nil {
		return
	}
}
