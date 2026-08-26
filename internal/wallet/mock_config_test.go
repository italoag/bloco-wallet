package wallet

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"blocowallet/pkg/config"
)

func TestMain(m *testing.M) {
	root, err := os.MkdirTemp("", "blocowallet-wallet-tests")
	if err != nil {
		os.Exit(1)
	}
	InitCryptoService(&config.Config{
		AppDir:     root,
		WalletsDir: filepath.Join(root, "keystore"),
		Security: config.SecurityConfig{
			Argon2Time:    1,
			Argon2Memory:  64 * 1024,
			Argon2Threads: 4,
			Argon2KeyLen:  32,
			SaltLength:    16,
		},
	})
	code := m.Run()
	_ = os.RemoveAll(root)
	os.Exit(code)
}

// CreateMockConfig creates a mock configuration for testing
func CreateMockConfig(t *testing.T) *config.Config {
	t.Helper()
	root := t.TempDir()
	return &config.Config{
		AppDir:       root,
		Language:     "en",
		WalletsDir:   filepath.Join(root, "keystore"),
		DatabasePath: filepath.Join(root, "wallets.db"),
		LocaleDir:    filepath.Join(root, "locale"),
		Fonts:        []string{"test-font"},
		Database: config.DatabaseConfig{
			Type: "sqlite",
			DSN:  ":memory:",
		},
		Security: config.SecurityConfig{
			Argon2Time:    1,
			Argon2Memory:  64 * 1024, // 64MB
			Argon2Threads: 4,
			Argon2KeyLen:  32,
			SaltLength:    16,
		},
		Networks: map[string]config.Network{
			"ethereum": {
				Name:        "Ethereum",
				RPCEndpoint: "https://mainnet.infura.io/v3/your-api-key",
				ChainID:     1,
				Symbol:      "ETH",
				Explorer:    "https://etherscan.io",
				IsActive:    true,
			},
		},
	}
}

func TestTestsNeverWriteOutsideSandbox(t *testing.T) {
	forbidden := []string{
		"os." + "UserHomeDir()",
		"/tmp/" + "blocowallet-test",
		"os.WriteFile(" + "\"testdata/",
	}
	root := filepath.Clean(filepath.Join("..", ".."))
	err := filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() || !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		content, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		for _, token := range forbidden {
			if strings.Contains(string(content), token) {
				t.Errorf("test file %s contains forbidden host path access", path)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
