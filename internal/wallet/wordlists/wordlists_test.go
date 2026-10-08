package wordlists

import (
	"fmt"
	"strings"
	"testing"
)

func TestAllOfficialListsLoad(t *testing.T) {
	names := []string{
		"english",
		"chinese_simplified",
		"chinese_traditional",
		"czech",
		"french",
		"italian",
		"japanese",
		"korean",
		"portuguese",
		"spanish",
	}
	for _, name := range names {
		words, err := Load(name)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(words) != WordsPerList {
			t.Fatalf("%s: got %d words", name, len(words))
		}
		seen := make(map[string]int, len(words))
		for i, word := range words {
			if previous, exists := seen[word]; exists {
				t.Fatalf("%s: duplicate word %q at %d and %d", name, word, previous, i)
			}
			seen[word] = i
		}
	}
}

func TestEnglishListAnchors(t *testing.T) {
	words, err := Load("english")
	if err != nil {
		t.Fatal(err)
	}
	if words[0] != "abandon" || words[WordsPerList-1] != "zoo" {
		t.Fatalf("unexpected english anchors: first=%q last=%q", words[0], words[WordsPerList-1])
	}
}

func TestLoadRejectsInvalidNames(t *testing.T) {
	for _, name := range []string{"", "../english", "english.txt", "missing"} {
		if _, err := Load(name); err == nil {
			t.Fatalf("Load(%q) succeeded", name)
		}
	}
}

func TestParseWordListToleratesCRLF(t *testing.T) {
	// Windows checkouts with core.autocrlf convert LF to CRLF; embedded
	// wordlists must still parse instead of panicking at package init.
	var crlf strings.Builder
	for i := 0; i < WordsPerList; i++ {
		fmt.Fprintf(&crlf, "word%04d\r\n", i)
	}
	words, err := parseWordList([]byte(crlf.String()), "crlf")
	if err != nil {
		t.Fatal(err)
	}
	if len(words) != WordsPerList || words[0] != "word0000" || words[WordsPerList-1] != "word2047" {
		t.Fatalf("unexpected words: len=%d first=%q last=%q", len(words), words[0], words[len(words)-1])
	}
}
