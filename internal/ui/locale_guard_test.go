package ui

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"blocowallet/pkg/localization"
)

// localeSinkViolation describes a production literal that is either a catalog
// ID missing from the embedded English catalog or a hard-coded user-facing
// string passed to a user-visible sink.
type localeSinkViolation struct {
	pos     string
	detail  string
	literal string
}

var (
	// catalogIDArgs maps localization helper names to the argument indexes that
	// carry catalog message IDs. Helpers whose arguments are enums (import
	// method, stage) are handled separately.
	catalogIDArgs = map[string][]int{
		"Get":                             {0},
		"T":                               {0},
		"TP":                              {0},
		"GetKeystoreErrorMessage":         {0},
		"FormatKeystoreErrorWithField":    {0},
		"GetPasswordFileErrorMessage":     {0},
		"FormatPasswordFileErrorWithFile": {0},
		"GetWalletImportMessage":          {0},
		"GetEnhancedImportErrorMessage":   {0},
		"FormatErrorWithRecoveryHint":     {0, 1},
		"WrapError":                       {0},
	}
	// enumArgs maps localization helpers to allowed literal values for
	// non-ID enum arguments (import methods, conflict types, stages).
	importMethodEnum = map[string]bool{"mnemonic": true, "private_key": true, "keystore": true, "watch_only": true, "bloco_encrypted": true, "keystore_batch": true, "mnemonic_batch": true}
	// printfVerb strips % formatting directives before prose detection.
	printfVerb = regexp.MustCompile(`%[+0#\- *.0-9]*[a-zA-Z%]`)
	// letterSeq matches prose-like letter sequences.
	letterSeq = regexp.MustCompile(`[\p{L}]{2,}`)
	// allowedSinkLiterals are brand/protocol names and layout tokens the
	// localization policy keeps untranslated by design.
	allowedSinkLiterals = map[string]bool{
		// Brand / protocol names kept untranslated by policy.
		"WalletConnect v2": true,
		"BLOCO":            true,
		"Ethereum":         true,
		"MetaMask":         true,
		"Ledger":           true,
		"Trezor":           true,
		// Internal identifiers and technical row formats, never displayed as
		// prose (component IDs, source-format tags, hidden column titles,
		// debug dumps, protocol field tokens).
		"add-network-%d":        true,
		"keystore_v3_batch:%d":  true,
		"bip39_batch:%d":        true,
		"private_key_batch:%d":  true,
		"custom_%s":             true,
		"Key":                   true, // hidden network-key column (Width: 0)
		"%s | %s | sha256:%s\n": true,
		"Phase: %s, Files: %d, Dir: %s, Jobs: %d, Results: %d, Popup: %t, Pending: %t, Complete: %t, Cancelled: %t": true,
		"%s  to=%s  value=%s  %d/%d  %s": true,
	}
	// nonProseLiteral matches strings with no letters: separators, icons,
	// whitespace, numeric layout, punctuation, key tokens.
	nonProseLiteral = regexp.MustCompile(`^[^\p{L}]*$`)
)

func stringLiteral(expr ast.Expr) (string, bool) {
	b, ok := expr.(*ast.BasicLit)
	if !ok || b.Kind != token.STRING {
		return "", false
	}
	value, err := strconv.Unquote(b.Value)
	if err != nil {
		return "", false
	}
	return value, true
}

// collectLiterals flattens a string expression into literal leaves,
// descending through string concatenation.
func collectLiterals(expr ast.Expr) []string {
	switch e := expr.(type) {
	case *ast.BasicLit:
		if v, ok := stringLiteral(e); ok {
			return []string{v}
		}
	case *ast.BinaryExpr:
		if e.Op == token.ADD {
			return append(collectLiterals(e.X), collectLiterals(e.Y)...)
		}
	}
	return nil
}

// isHumanText reports whether a literal looks like user-facing prose once
// printf verbs are stripped. Symbols, digits, punctuation and key tokens pass.
func isHumanText(literal string) bool {
	if allowedSinkLiterals[literal] {
		return false
	}
	stripped := printfVerb.ReplaceAllString(literal, " ")
	return letterSeq.MatchString(stripped)
}

// auditUISources parses every non-test Go file in internal/ui and returns
// catalog-ID violations and hard-coded text violations.
func auditUISources(t *testing.T) []localeSinkViolation {
	t.Helper()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob ui sources: %v", err)
	}
	messages := localization.Messages("en")
	templateKeys := map[string]bool{}
	for id, text := range messages {
		if strings.Contains(text, "{{") {
			templateKeys[id] = true
		}
	}
	var violations []localeSinkViolation
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		parsed, err := parser.ParseFile(fset, file, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", file, err)
		}
		violations = append(violations, auditFile(fset, parsed, messages, templateKeys)...)
	}
	return violations
}

func auditFile(fset *token.FileSet, file *ast.File, messages map[string]string, templateKeys map[string]bool) []localeSinkViolation {
	var violations []localeSinkViolation
	pos := func(node ast.Node) string { return fset.Position(node.Pos()).String() }
	record := func(node ast.Node, detail, value string) {
		violations = append(violations, localeSinkViolation{pos: pos(node), detail: detail, literal: value})
	}
	// templateKeys may be nil in fixture tests; treat any key containing
	// template markers via messages only when provided.
	needsTemplate := func(id string) bool {
		if templateKeys != nil {
			return templateKeys[id]
		}
		return false
	}
	checkIDArg := func(node ast.Node, expr ast.Expr, isGet bool) {
		value, ok := stringLiteral(expr)
		if !ok || value == "" {
			return
		}
		if _, exists := messages[value]; !exists {
			record(node, "unknown catalog ID", value)
			return
		}
		if isGet && needsTemplate(value) {
			record(node, "Get on template key requiring variables (use T/TP)", value)
		}
	}
	checkHumanSink := func(node ast.Node, expr ast.Expr) {
		for _, value := range collectLiterals(expr) {
			if value == "" || nonProseLiteral.MatchString(value) {
				continue
			}
			if isHumanText(value) {
				record(node, "hard-coded user-facing text", value)
			}
		}
	}
	callName := func(n *ast.CallExpr) (pkg string, name string) {
		switch fun := n.Fun.(type) {
		case *ast.SelectorExpr:
			if ident, ok := fun.X.(*ast.Ident); ok {
				return ident.Name, fun.Sel.Name
			}
			return "", fun.Sel.Name
		case *ast.Ident:
			return "", fun.Name
		}
		return "", ""
	}
	ast.Inspect(file, func(node ast.Node) bool {
		switch n := node.(type) {
		case *ast.CallExpr:
			pkg, name := callName(n)
			if pkg == "localization" {
				if indexes, ok := catalogIDArgs[name]; ok {
					for _, i := range indexes {
						if i < len(n.Args) {
							checkIDArg(n, n.Args[i], name == "Get")
						}
					}
				}
				if (name == "T" || name == "TP") && len(n.Args) > 1 {
					// Secrets, rendered widgets and raw payloads must never
					// pass through the translator's sanitizer.
					var blocked bool
					ast.Inspect(n.Args[len(n.Args)-1], func(inner ast.Node) bool {
						switch e := inner.(type) {
						case *ast.CallExpr:
							if sel, ok := e.Fun.(*ast.SelectorExpr); ok && (sel.Sel.Name == "View" || sel.Sel.Name == "Bytes") {
								blocked = true
								record(n, "widget/secret bytes passed to translator", sel.Sel.Name+"()")
							}
						case *ast.SelectorExpr:
							if e.Sel.Name == "Rendered" {
								blocked = true
								record(n, "rendered payload passed to translator", "Rendered")
							}
						}
						return !blocked
					})
				}
				switch name {
				case "FormatDuplicateImportError", "GetNoMnemonicAvailableMessage":
					// arg0 is an import-method enum, not a catalog ID
					if len(n.Args) > 0 {
						if v, ok := stringLiteral(n.Args[0]); ok && v != "" && !importMethodEnum[v] {
							record(n, "unknown import-method enum", v)
						}
					}
				case "GetKeystoreImportStageMessage":
					// arg0 is a stage suffix: keystore_import_stage_<stage>
					if len(n.Args) > 0 {
						if v, ok := stringLiteral(n.Args[0]); ok && v != "" {
							id := "keystore_import_stage_" + v
							if _, exists := messages[id]; !exists {
								record(n, "unknown catalog ID", id)
							}
						}
					}
				}
			}
			if name == "newCanonicalField" && len(n.Args) > 1 {
				checkIDArg(n, n.Args[1], false)
			}
			if name == "WithHelp" && len(n.Args) > 1 {
				checkHumanSink(n, n.Args[1])
			}
			switch name {
			case "WriteString", "Render":
				if len(n.Args) == 1 {
					checkHumanSink(n, n.Args[0])
				}
			case "Sprintf":
				if len(n.Args) > 0 {
					checkHumanSink(n, n.Args[0])
				}
			case "Fprintf":
				if len(n.Args) > 1 {
					checkHumanSink(n, n.Args[1])
				}
			}
		case *ast.KeyValueExpr:
			ident, ok := n.Key.(*ast.Ident)
			if !ok {
				return true
			}
			switch ident.Name {
			case "labelKey":
				checkIDArg(n, n.Value, false)
			case "title", "description", "label", "Title", "Description":
				checkHumanSink(n, n.Value)
			}
		case *ast.AssignStmt:
			for i, lhs := range n.Lhs {
				sel, ok := lhs.(*ast.SelectorExpr)
				if !ok || i >= len(n.Rhs) {
					continue
				}
				switch sel.Sel.Name {
				case "Placeholder", "Title":
					checkHumanSink(n, n.Rhs[i])
				case "labelKey", "progressStage":
					checkIDArg(n, n.Rhs[i], false)
				}
			}
		}
		return true
	})
	return violations
}

// TestUICatalogIDsExist ensures every literal catalog ID referenced from
// production internal/ui code exists in the embedded English catalog.
func TestUICatalogIDsExist(t *testing.T) {
	violations := auditUISources(t)
	for _, v := range violations {
		if v.detail != "hard-coded user-facing text" {
			t.Errorf("%s: %s %q", v.pos, v.detail, v.literal)
		}
	}
}

// TestNoHardcodedUIText ensures production internal/ui files do not pass
// user-facing prose literals to text sinks; all such text must come from
// the embedded catalogs.
func TestNoHardcodedUIText(t *testing.T) {
	violations := auditUISources(t)
	for _, v := range violations {
		if v.detail == "hard-coded user-facing text" {
			t.Errorf("%s: %s %q", v.pos, v.detail, v.literal)
		}
	}
}

// TestLocaleGuardDetectsViolations proves the audit catches a hard-coded
// placeholder, a hard-coded help description, a missing catalog ID, a raw
// string literal, a one-word placeholder, a bare constructor call, a second
// helper ID, a human fmt.Sprintf format, and a Get on a template key.
func TestLocaleGuardDetectsViolations(t *testing.T) {
	const src = `package ui

import (
	"blocowallet/pkg/localization"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	"fmt"
)

func example() {
	var ti textinput.Model
	ti.Placeholder = "Type your password here"
	ti.Placeholder = "Password"
	ti.Placeholder = ` + "`" + `raw string prompt` + "`" + `
	var b key.Binding
	b = key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "do the thing"))
	_ = localization.Get("definitely_missing_catalog_id")
	_ = newCanonicalField(canonicalFieldPassword, "another_missing_id", false)
	_ = localization.FormatErrorWithRecoveryHint("confirm", "third_missing_id")
	_ = fmt.Sprintf("Found %d files", 3)
	_ = localization.FormatDuplicateImportError("bogus_method", "mnemonic", "0x")
	_ = localization.T("confirm", map[string]interface{}{"Input": ti.View()})
	_ = b
}
`
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, "fixture.go", src, 0)
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	violations := auditFile(fset, parsed, map[string]string{"confirm": "Confirm", "field_required": "{{.Field}} required"}, map[string]bool{"field_required": true})
	found := map[string]bool{}
	for _, v := range violations {
		switch v.literal {
		case "Type your password here":
			found["multiword placeholder"] = true
		case "Password":
			found["one-word placeholder"] = true
		case "raw string prompt":
			found["raw literal"] = true
		case "do the thing":
			found["help"] = true
		case "definitely_missing_catalog_id":
			found["missing id"] = true
		case "another_missing_id":
			found["bare constructor id"] = true
		case "third_missing_id":
			found["second helper arg id"] = true
		case "Found %d files":
			found["sprintf format"] = true
		case "bogus_method":
			found["enum arg"] = true
		case "View()":
			found["widget in TemplateData"] = true
		}
	}
	for want := range map[string]bool{
		"multiword placeholder": true, "one-word placeholder": true, "raw literal": true,
		"help": true, "missing id": true, "bare constructor id": true,
		"second helper arg id": true, "sprintf format": true, "enum arg": true,
		"widget in TemplateData": true,
	} {
		if !found[want] {
			t.Errorf("guard missed %s; violations: %#v", want, violations)
		}
	}

	// Get on a template-requiring key must be flagged.
	const src2 = `package ui
import "blocowallet/pkg/localization"
var _ = localization.Get("field_required")
`
	parsed2, err := parser.ParseFile(fset, "fixture2.go", src2, 0)
	if err != nil {
		t.Fatalf("parse fixture2: %v", err)
	}
	var sawTemplateGet bool
	for _, v := range auditFile(fset, parsed2, map[string]string{"field_required": "{{.Field}} required"}, map[string]bool{"field_required": true}) {
		if strings.Contains(v.detail, "template key") {
			sawTemplateGet = true
		}
	}
	if !sawTemplateGet {
		t.Fatalf("guard missed Get on template key")
	}

	// Technical literals must pass: hex/layout/keybinding/brand.
	const src3 = `package ui
import "fmt"
import "github.com/charmbracelet/bubbles/key"
func ok() {
	var b key.Binding
	b = key.NewBinding(key.WithKeys("x"), key.WithHelp("0x", "0x%x"))
	_ = fmt.Sprintf("%s\n%s", "a", "b")
	_ = fmt.Sprintf("0x%x", 1)
	_ = b
}
`
	parsed3, err := parser.ParseFile(fset, "fixture3.go", src3, 0)
	if err != nil {
		t.Fatalf("parse fixture3: %v", err)
	}
	for _, v := range auditFile(fset, parsed3, nil, nil) {
		t.Errorf("technical literal false positive: %#v", v)
	}
}
