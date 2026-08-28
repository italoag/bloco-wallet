package terminal

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSanitizeStyledBlockPreservesOnlyBoundedSGR(t *testing.T) {
	input := "\x1b[31;40m██\x1b[0m\n" +
		"\x1b]52;c;c2VjcmV0\x07hidden\x1b[2J\x1b[Hsafe"
	output := SanitizeStyledBlock(input, 4, 32)
	if !strings.Contains(output, "\x1b[31;40m██\x1b[0m") {
		t.Fatalf("styled sanitizer removed safe SGR artwork: %q", output)
	}
	for _, forbidden := range []string{"\x1b]", "\x1b[2J", "\x1b[H", "c2VjcmV0"} {
		if strings.Contains(output, forbidden) {
			t.Fatalf("styled sanitizer retained unsafe sequence %q: %q", forbidden, output)
		}
	}
}

func TestSanitizeInlineBlocksTerminalControlSequences(t *testing.T) {
	inputs := []string{
		"safe\x1b[31mred\x1b[0m",
		"link\x1b]8;;https://example.com\x07click\x1b]8;;\x07",
		"copy\x1b]52;c;dG9rZW4=\x07",
		"bell\a carriage\r\nnext",
		"c1\u009b31mred\u009d52;c;dG9rZW4=\u009c",
	}
	for _, input := range inputs {
		output := SanitizeInline(input, 256)
		for _, forbidden := range []string{"\x1b", "\x07", "\r", "\n", "\u009b", "\u009d", "\u009c"} {
			if strings.Contains(output, forbidden) {
				t.Fatalf("sanitized output retained terminal control %q: %q", forbidden, output)
			}
		}
	}
}

func TestSanitizeInlineBoundsAndPreservesPrintableUnicode(t *testing.T) {
	output := SanitizeInline("Carteira ç 密碼 "+strings.Repeat("x", 100), 20)
	if !utf8.ValidString(output) {
		t.Fatal("sanitized output is not valid UTF-8")
	}
	if !strings.Contains(output, "Carteira ç 密碼") {
		t.Fatalf("printable Unicode was removed: %q", output)
	}
	if utf8.RuneCountInString(output) > 20 {
		t.Fatalf("sanitized output exceeded rune budget: %d", utf8.RuneCountInString(output))
	}
}

func TestSanitizersRejectExtremeLimitsAndBoundInputScan(t *testing.T) {
	if SanitizeBlock("value", 0, 10) != "" || SanitizeBlock("value", 5000, 10) != "" {
		t.Fatal("invalid block limits were accepted")
	}
	block := SanitizeBlock(strings.Repeat("x", 1000), 1, 4)
	if utf8.RuneCountInString(block) > 4 {
		t.Fatal("block input budget was exceeded")
	}
	inline := SanitizeInline(strings.Repeat("\x1b", 1000), 8)
	if utf8.RuneCountInString(inline) > 8 || strings.ContainsRune(inline, '\x1b') {
		t.Fatal("inline control-only input bypassed budget")
	}
	if SanitizeInline("value", 1<<21) != "" {
		t.Fatal("extreme inline limit was accepted")
	}
}

func TestSanitizeBlockAllowsOnlyBoundedStructuralNewlines(t *testing.T) {
	output := SanitizeBlock("one\n\x1b]52;c;secret\x07two\nthree", 2, 16)
	if strings.Count(output, "\n") != 1 || strings.ContainsAny(output, "\x1b\a\r") {
		t.Fatalf("unsafe block output: %q", output)
	}
}

func TestSanitizeInlineHandlesInvalidUTF8AndZeroLimit(t *testing.T) {
	if output := SanitizeInline(string([]byte{'a', 0xff, 'b'}), 8); !utf8.ValidString(output) {
		t.Fatal("invalid UTF-8 was preserved")
	}
	if output := SanitizeInline("value", 0); output != "" {
		t.Fatalf("zero limit returned %q", output)
	}
}

func FuzzSanitizeInline(f *testing.F) {
	f.Add("safe")
	f.Add("\x1b]52;c;secret\x07")
	f.Fuzz(func(t *testing.T, input string) {
		output := SanitizeInline(input, 128)
		if !utf8.ValidString(output) || utf8.RuneCountInString(output) > 128 {
			t.Fatal("sanitizer violated output contract")
		}
		for _, forbidden := range []rune{'\x1b', '\a', '\r', '\n', '\u009b', '\u009d', '\u009c'} {
			if strings.ContainsRune(output, forbidden) {
				t.Fatalf("sanitizer retained control U+%04X", forbidden)
			}
		}
	})
}
