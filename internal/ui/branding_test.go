package ui

import (
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestRenderHeaderLogoRestoresMultilineASCIIArt(t *testing.T) {
	logo := renderHeaderLogo()
	if strings.TrimSpace(logo) == "bloco" || strings.Count(logo, "\n") < 2 || lipgloss.Width(logo) <= len("bloco") {
		t.Fatalf("application banner is not multiline ASCII art: %q", logo)
	}
	if regexp.MustCompile(`\[[0-9;]+m`).MatchString(strings.ReplaceAll(logo, "\x1b[", "")) {
		t.Fatalf("application banner exposes ANSI parameters as visible text: %q", logo)
	}
	if !strings.Contains(logo, "\x1b[") {
		t.Fatalf("application banner lost its trusted ANSI colors: %q", logo)
	}
}
