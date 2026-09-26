package ui

import (
	"encoding/hex"

	"blocowallet/internal/terminal"
	"blocowallet/pkg/localization"
)

func safeInline(value string) string {
	return terminal.SanitizeInline(value, terminal.DefaultInlineLimit)
}

func safeShort(value string) string {
	return terminal.SanitizeInline(value, 128)
}

func safeError(err error) string {
	if err == nil {
		return localization.Get("unknown_error")
	}
	return safeInline(err.Error())
}

func safeLines(values []string) []string {
	sanitized := make([]string, len(values))
	for index, value := range values {
		sanitized[index] = safeInline(value)
	}
	return sanitized
}

// renderCalldataLine renders the localized calldata label followed by the raw
// hex payload — payloads never pass through the translator's sanitizer.
func renderCalldataLine(data []byte) string {
	return localization.Get("tx_calldata_line") + " 0x" + hex.EncodeToString(data)
}
