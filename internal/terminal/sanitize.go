package terminal

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

const DefaultInlineLimit = 512

func SanitizeBlock(value string, maxLines, maxRunesPerLine int) string {
	if maxLines <= 0 || maxRunesPerLine <= 0 {
		return ""
	}
	if maxLines > 4096 || maxRunesPerLine > 1<<20 {
		return ""
	}
	maxInputBytes := maxLines * maxRunesPerLine * utf8.UTFMax * 2
	if len(value) > maxInputBytes {
		value = value[:maxInputBytes]
	}
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", " ")
	lines := strings.Split(value, "\n")
	if len(lines) > maxLines {
		lines = lines[:maxLines]
	}
	for index := range lines {
		lines[index] = SanitizeInline(lines[index], maxRunesPerLine)
	}
	return strings.Join(lines, "\n")
}

func SanitizeStyledBlock(value string, maxLines, maxRunesPerLine int) string {
	if maxLines <= 0 || maxRunesPerLine <= 0 || maxLines > 4096 || maxRunesPerLine > 1<<20 {
		return ""
	}
	maxInputBytes := maxLines * maxRunesPerLine * utf8.UTFMax * 16
	if len(value) > maxInputBytes {
		value = value[:maxInputBytes]
	}
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "")
	lines := strings.Split(value, "\n")
	if len(lines) > maxLines {
		lines = lines[:maxLines]
	}
	for index := range lines {
		lines[index] = sanitizeStyledLine(lines[index], maxRunesPerLine)
	}
	return strings.Join(lines, "\n")
}

func sanitizeStyledLine(value string, maxRunes int) string {
	var output strings.Builder
	output.Grow(min(len(value), maxRunes*4))
	written := 0
	for index := 0; index < len(value) && written < maxRunes; {
		if value[index] == '\x1b' {
			next, sequence, safe := consumeEscapeSequence(value, index)
			if safe {
				output.WriteString(sequence)
			}
			index = next
			continue
		}
		r, size := utf8.DecodeRuneInString(value[index:])
		if r == utf8.RuneError && size == 1 {
			output.WriteRune(utf8.RuneError)
			written++
			index++
			continue
		}
		if r >= '\u0080' && r <= '\u009f' {
			index = consumeC1Sequence(value, index, size, r)
			continue
		}
		index += size
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || r == '\u2028' || r == '\u2029' {
			continue
		}
		if !unicode.IsPrint(r) {
			r = utf8.RuneError
		}
		output.WriteRune(r)
		written++
	}
	result := output.String()
	if strings.Contains(result, "\x1b[") && !strings.HasSuffix(result, "\x1b[0m") {
		result += "\x1b[0m"
	}
	return result
}

func consumeEscapeSequence(value string, start int) (int, string, bool) {
	if start+1 >= len(value) {
		return len(value), "", false
	}
	switch value[start+1] {
	case '[':
		end, final := consumeCSI(value, start+2)
		if final == 'm' && end-start <= 64 && validSGRParameters(value[start+2:end-1]) {
			return end, value[start:end], true
		}
		return end, "", false
	case ']':
		return consumeStringControl(value, start+2, true), "", false
	case 'P', 'X', '^', '_':
		return consumeStringControl(value, start+2, false), "", false
	default:
		return min(start+2, len(value)), "", false
	}
}

func consumeCSI(value string, start int) (int, byte) {
	limit := min(len(value), start+64)
	for index := start; index < limit; index++ {
		if value[index] >= 0x40 && value[index] <= 0x7e {
			return index + 1, value[index]
		}
	}
	return limit, 0
}

func consumeStringControl(value string, start int, allowBell bool) int {
	for index := start; index < len(value); index++ {
		if allowBell && value[index] == '\a' {
			return index + 1
		}
		if value[index] == '\x1b' && index+1 < len(value) && value[index+1] == '\\' {
			return index + 2
		}
		if index+1 < len(value) && value[index] == 0xc2 && value[index+1] == 0x9c {
			return index + 2
		}
	}
	return len(value)
}

func consumeC1Sequence(value string, start, size int, control rune) int {
	switch control {
	case '\u009b':
		end, _ := consumeCSI(value, start+size)
		return end
	case '\u0090', '\u009d', '\u009e', '\u009f':
		return consumeStringControl(value, start+size, control == '\u009d')
	default:
		return start + size
	}
}

func validSGRParameters(value string) bool {
	for _, character := range value {
		if (character < '0' || character > '9') && character != ';' {
			return false
		}
	}
	return true
}

func SanitizeInline(value string, maxRunes int) string {
	if maxRunes <= 0 || maxRunes > 1<<20 || value == "" {
		return ""
	}
	truncated := false
	maxInputBytes := maxRunes * utf8.UTFMax * 2
	if len(value) > maxInputBytes {
		value = value[:maxInputBytes]
		truncated = true
	}
	var output strings.Builder
	output.Grow(min(len(value), maxRunes*2))
	written := 0
	lastSpace := false
	for len(value) > 0 {
		r, size := utf8.DecodeRuneInString(value)
		if size == 0 {
			break
		}
		value = value[size:]
		if written == maxRunes {
			truncated = true
			break
		}
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || r == '\u2028' || r == '\u2029' {
			if !lastSpace {
				output.WriteByte(' ')
				written++
				lastSpace = true
			}
			continue
		}
		if !unicode.IsPrint(r) {
			r = utf8.RuneError
		}
		output.WriteRune(r)
		written++
		lastSpace = unicode.IsSpace(r)
	}
	if (truncated || value != "") && written > 0 {
		runes := []rune(output.String())
		if len(runes) >= maxRunes {
			runes[maxRunes-1] = '…'
			return string(runes[:maxRunes])
		}
	}
	return output.String()
}
