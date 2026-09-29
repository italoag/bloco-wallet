package blockchain

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type networkBoundaryViolation struct {
	path    string
	pattern string
}

var forbiddenNetworkTransportPatterns = []string{
	"http.Client{",
	"http.DefaultClient",
	"http.Get(",
	"http.Post(",
	"http.NewRequest(",
	"http.NewRequestWithContext(",
	"ethclient.Dial",
	"rpc.Dial",
	"websocket.Dial",
}

func goModulePath(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == "module" {
			return fields[1], nil
		}
	}
	return "", fmt.Errorf("module directive missing from %s", path)
}

func declaredSubmodulePaths(root string) (map[string]struct{}, error) {
	paths := make(map[string]struct{})
	data, err := os.ReadFile(filepath.Join(root, ".gitmodules"))
	if os.IsNotExist(err) {
		return paths, nil
	}
	if err != nil {
		return nil, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "path") {
			continue
		}
		fields := strings.SplitN(line, "=", 2)
		if len(fields) != 2 {
			return nil, fmt.Errorf("invalid submodule path in %s", filepath.Join(root, ".gitmodules"))
		}
		relative := strings.TrimSpace(fields[1])
		path := filepath.Clean(filepath.Join(root, relative))
		if !pathWithinRoot(root, path) {
			return nil, fmt.Errorf("submodule path escapes repository: %s", relative)
		}
		paths[path] = struct{}{}
	}
	return paths, nil
}

func pathWithinRoot(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)))
}

func approvedGatewayPath(root string) string {
	return filepath.Clean(filepath.Join(root, "internal", "blockchain", "rpc_gateway.go"))
}

func scanFirstPartyNetworkTransports(root string) ([]networkBoundaryViolation, error) {
	root = filepath.Clean(root)
	rootModule, err := goModulePath(filepath.Join(root, "go.mod"))
	if err != nil {
		return nil, err
	}
	submodules, err := declaredSubmodulePaths(root)
	if err != nil {
		return nil, err
	}
	violations := make([]networkBoundaryViolation, 0)
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			base := filepath.Base(path)
			if base == ".git" || base == "build" || base == "dist" || base == "graphify-out" {
				return filepath.SkipDir
			}
			if path != root {
				if _, declared := submodules[path]; declared {
					module, moduleErr := goModulePath(filepath.Join(path, "go.mod"))
					if moduleErr != nil && !os.IsNotExist(moduleErr) {
						return moduleErr
					}
					if moduleErr == nil && module != rootModule {
						return filepath.SkipDir
					}
				}
			}
			return nil
		}
		if filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") || path == approvedGatewayPath(root) {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		text := string(data)
		for _, pattern := range forbiddenNetworkTransportPatterns {
			if strings.Contains(text, pattern) {
				violations = append(violations, networkBoundaryViolation{path: path, pattern: pattern})
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return violations, nil
}

func TestRPCGatewayOwnsAllOutboundTransports(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", ".."))
	violations, err := scanFirstPartyNetworkTransports(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, violation := range violations {
		t.Errorf("%s creates outbound transport outside RPC gateway: %s", violation.path, violation.pattern)
	}
}

func TestNetworkBoundaryScannerClassifiesModuleOwnership(t *testing.T) {
	root := t.TempDir()
	write := func(relative, content string) {
		t.Helper()
		path := filepath.Join(root, relative)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module blocowallet\n\ngo 1.26.7\n")
	write(".gitmodules", "[submodule \"third_party/external\"]\n\tpath = third_party/external\n")
	write("internal/owned.go", "package owned\nvar _ = http.NewRequest(\"GET\", \"/\", nil)\n")
	write("vendor/owned.go", "package owned\nvar _ = http.NewRequest(\"GET\", \"/\", nil)\n")
	write("third_party/local.go", "package local\nvar _ = http.NewRequest(\"GET\", \"/\", nil)\n")
	write("internal/other/rpc_gateway.go", "package other\nvar _ = http.NewRequest(\"GET\", \"/\", nil)\n")
	write("internal/blockchain/rpc_gateway.go", "package blockchain\nvar _ = http.NewRequest(\"GET\", \"/\", nil)\n")
	write("third_party/external/go.mod", "module example.com/external\n\ngo 1.26.7\n")
	write("third_party/external/helper.go", "package external\nvar _ = http.NewRequest(\"GET\", \"/\", nil)\n")

	violations, err := scanFirstPartyNetworkTransports(root)
	if err != nil {
		t.Fatal(err)
	}
	seen := make(map[string]bool, len(violations))
	for _, violation := range violations {
		seen[filepath.ToSlash(filepath.Join(".", strings.TrimPrefix(violation.path, root+string(filepath.Separator))))] = true
	}
	for _, relative := range []string{"internal/owned.go", "vendor/owned.go", "third_party/local.go", "internal/other/rpc_gateway.go"} {
		if !seen[filepath.ToSlash(relative)] {
			t.Errorf("expected first-party violation for %s", relative)
		}
	}
	if len(seen) != 4 {
		t.Fatalf("expected exactly four first-party violations, got %v", seen)
	}
}
