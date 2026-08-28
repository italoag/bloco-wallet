package ui

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestViewFunctionsDoNotPerformIOOrReadClock(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	forbiddenPackages := map[string]bool{"os": true, "io": true, "net": true, "http": true, "log": true}
	forbiddenTimeCalls := map[string]bool{"Now": true, "Since": true, "Until": true, "Sleep": true, "After": true, "NewTimer": true, "NewTicker": true}
	files := token.NewFileSet()
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".go" || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(files, entry.Name(), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, declaration := range parsed.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Body == nil || (function.Name.Name != "View" && !strings.HasPrefix(function.Name.Name, "view") && !strings.HasPrefix(function.Name.Name, "render")) {
				continue
			}
			ast.Inspect(function.Body, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				selector, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				identifier, ok := selector.X.(*ast.Ident)
				if ok && (forbiddenPackages[identifier.Name] || (identifier.Name == "time" && forbiddenTimeCalls[selector.Sel.Name])) {
					t.Errorf("%s.%s calls %s.%s in a View path", entry.Name(), function.Name.Name, identifier.Name, selector.Sel.Name)
				}
				return true
			})
		}
	}
}
