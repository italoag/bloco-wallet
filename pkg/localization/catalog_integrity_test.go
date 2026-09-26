package localization

import (
	"sort"
	"strings"
	"testing"
	"text/template/parse"

	"github.com/nicksnyder/go-i18n/v2/i18n"
)

func templateVars(t *testing.T, text string) map[string]bool {
	t.Helper()
	vars := map[string]bool{}
	tree, err := parse.Parse("msg", text, "{{", "}}", map[string]any{})
	if err != nil {
		t.Fatalf("template parse failed for %q: %v", text, err)
	}
	var walk func(n parse.Node)
	walk = func(n parse.Node) {
		switch node := n.(type) {
		case *parse.ListNode:
			for _, c := range node.Nodes {
				walk(c)
			}
		case *parse.ActionNode:
			walk(node.Pipe)
		case *parse.IfNode:
			walk(node.Pipe)
			walk(node.List)
			if node.ElseList != nil {
				walk(node.ElseList)
			}
		case *parse.RangeNode:
			walk(node.Pipe)
			walk(node.List)
			if node.ElseList != nil {
				walk(node.ElseList)
			}
		case *parse.WithNode:
			walk(node.Pipe)
			walk(node.List)
			if node.ElseList != nil {
				walk(node.ElseList)
			}
		case *parse.PipeNode:
			for _, cmd := range node.Cmds {
				walk(cmd)
			}
		case *parse.CommandNode:
			for _, arg := range node.Args {
				walk(arg)
			}
		case *parse.FieldNode:
			if len(node.Ident) > 0 {
				vars[strings.Join(node.Ident, ".")] = true
			}
		case *parse.VariableNode:
			if len(node.Ident) > 0 {
				vars[strings.Join(node.Ident, ".")] = true
			}
		}
	}
	for _, t2 := range tree {
		if t2.Root != nil {
			walk(t2.Root)
		}
	}
	return vars
}

func TestEmbeddedCatalogIntegrity(t *testing.T) {
	cat, err := ensureCatalog()
	if err != nil {
		t.Fatalf("embedded catalog failed to load: %v", err)
	}
	if len(cat.languages) < 2 {
		t.Fatalf("expected multiple bundled languages, got %v", cat.languages)
	}

	reference := cat.static["en"]
	keys := make([]string, 0, len(reference))
	for k := range reference {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, lang := range cat.languages {
		msgs := cat.static[lang]
		if msgs["language_name"] == "" {
			t.Fatalf("locale %s missing language_name", lang)
		}
		for _, key := range keys {
			if msgs[key] == "" {
				t.Fatalf("locale %s missing/empty key %q", lang, key)
			}
		}
		if len(msgs) != len(reference) {
			t.Fatalf("locale %s has %d keys, en has %d", lang, len(msgs), len(reference))
		}
		for key, text := range msgs {
			if strings.Contains(text, `\n`) {
				t.Fatalf("locale %s key %q decodes to a literal backslash-n (source needs a single \\n escape)", lang, key)
			}
			if strings.Contains(text, `\"`) {
				t.Fatalf("locale %s key %q decodes to a literal backslash-quote (source needs a single \" escape)", lang, key)
			}
		}
	}

	// Template variables must match across languages so interpolations stay
	// consistent no matter which locale renders the message. Every provided
	// plural form is validated against the matching English form (or the
	// English "other" when English lacks that CLDR form).
	formOf := func(msg *i18n.Message, name string) string {
		switch name {
		case "zero":
			return msg.Zero
		case "one":
			return msg.One
		case "two":
			return msg.Two
		case "few":
			return msg.Few
		case "many":
			return msg.Many
		}
		return msg.Other
	}
	formNames := []string{"zero", "one", "two", "few", "many", "other"}
	for _, key := range keys {
		enMsg := cat.messages["en"][key]
		for _, lang := range cat.languages {
			if lang == "en" {
				continue
			}
			msg := cat.messages[lang][key]
			for _, form := range formNames {
				text := formOf(msg, form)
				if text == "" {
					continue
				}
				vars := templateVars(t, text)
				enText := formOf(enMsg, form)
				if enText == "" {
					enText = enMsg.Other
				}
				enVars := templateVars(t, enText)
				for v := range enVars {
					if !vars[v] {
						t.Fatalf("locale %s key %q form %q missing template var %q", lang, key, form, v)
					}
				}
				for v := range vars {
					if !enVars[v] {
						t.Fatalf("locale %s key %q form %q adds unexpected template var %q", lang, key, form, v)
					}
				}
			}
		}
	}
}
