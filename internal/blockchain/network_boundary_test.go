package blockchain

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRPCGatewayOwnsAllOutboundTransports(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", ".."))
	forbidden := []string{
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
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			base := filepath.Base(path)
			if base == ".git" || base == "build" || base == "dist" || base == "graphify-out" {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") || filepath.Base(path) == "rpc_gateway.go" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, pattern := range forbidden {
			if strings.Contains(string(data), pattern) {
				t.Errorf("%s creates outbound transport outside RPC gateway: %s", path, pattern)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
