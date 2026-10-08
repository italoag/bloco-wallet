// Package wordlists embeds the official BIP-39 wordlists from the bitcoin/bips
// project (https://github.com/bitcoin/bips/tree/master/bip-0039) so mnemonic
// generation and validation do not depend on external modules.
package wordlists

import (
	"embed"
	"fmt"
	"strings"
)

// WordsPerList is the fixed BIP-39 wordlist size.
const WordsPerList = 2048

//go:embed *.txt
var files embed.FS

// Load returns the BIP-39 wordlist identified by fileBase (for example
// "english" for english.txt). The returned slice has exactly WordsPerList
// entries in specification order.
func Load(fileBase string) ([]string, error) {
	if fileBase == "" || strings.ContainsAny(fileBase, "/\\.") {
		return nil, fmt.Errorf("wordlists: invalid wordlist name %q", fileBase)
	}
	data, err := files.ReadFile(fileBase + ".txt")
	if err != nil {
		return nil, fmt.Errorf("wordlists: read %s: %w", fileBase, err)
	}
	return parseWordList(data, fileBase)
}

func parseWordList(data []byte, fileBase string) ([]string, error) {
	// Normalize CRLF so embedded lists stay valid even when the working tree
	// was checked out with autocrlf (Windows CI builds).
	content := strings.ReplaceAll(string(data), "\r\n", "\n")
	content = strings.TrimRight(content, "\n")
	if content == "" {
		return nil, fmt.Errorf("wordlists: %s is empty", fileBase)
	}
	words := strings.Split(content, "\n")
	if len(words) != WordsPerList {
		return nil, fmt.Errorf("wordlists: %s must contain %d words, got %d", fileBase, WordsPerList, len(words))
	}
	for index, word := range words {
		if word == "" || strings.TrimSpace(word) != word {
			return nil, fmt.Errorf("wordlists: %s word %d is malformed", fileBase, index)
		}
	}
	return words, nil
}
