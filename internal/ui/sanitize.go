package ui

import "blocowallet/internal/terminal"

func safeInline(value string) string {
	return terminal.SanitizeInline(value, terminal.DefaultInlineLimit)
}

func safeShort(value string) string {
	return terminal.SanitizeInline(value, 128)
}

func safeError(err error) string {
	if err == nil {
		return "Unknown error"
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
