package wordlists

import "testing"

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
