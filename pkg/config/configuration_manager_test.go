package config

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewConfigurationManager(t *testing.T) {
	cm := NewConfigurationManager()
	assert.NotNil(t, cm)
	assert.NotNil(t, cm.viper)
}

func TestConfigurationManager_LoadConfiguration(t *testing.T) {
	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "bloco_config_test")
	require.NoError(t, err)
	if runtime.GOOS != "windows" {
		require.NoError(t, os.Chmod(tempDir, 0755))
	}
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Logf("Warning: could not remove temp dir: %v", err)
		}
	}()

	// Set environment variable to use temp directory
	originalEnv := os.Getenv("BLOCO_WALLET_APP_APP_DIR")
	defer func() {
		if originalEnv != "" {
			if err := os.Setenv("BLOCO_WALLET_APP_APP_DIR", originalEnv); err != nil {
				t.Logf("Warning: could not restore env var: %v", err)
			}
		} else {
			if err := os.Unsetenv("BLOCO_WALLET_APP_APP_DIR"); err != nil {
				t.Logf("Warning: could not unset env var: %v", err)
			}
		}
	}()
	if err := os.Setenv("BLOCO_WALLET_APP_APP_DIR", tempDir); err != nil {
		t.Fatalf("Failed to set environment variable: %v", err)
	}

	cm := NewConfigurationManager()
	cfg, err := cm.LoadConfiguration()

	assert.NoError(t, err)
	assert.NotNil(t, cfg)
	assert.Equal(t, tempDir, cfg.AppDir)
	assert.Equal(t, filepath.Join(tempDir, "keystore"), cfg.WalletsDir)
	assert.Equal(t, filepath.Join(tempDir, "bloco.db"), cfg.DatabasePath)
	assert.Equal(t, filepath.Join(tempDir, "locale"), cfg.LocaleDir)
	assert.Equal(t, "en", cfg.Language)
	assert.Equal(t, "sqlite", cfg.Database.Type)

	// Verify config file was created
	configPath := filepath.Join(tempDir, "config.toml")
	assert.FileExists(t, configPath)
	if runtime.GOOS != "windows" {
		dirInfo, statErr := os.Stat(tempDir)
		require.NoError(t, statErr)
		assert.Equal(t, os.FileMode(0700), dirInfo.Mode().Perm())
		fileInfo, statErr := os.Stat(configPath)
		require.NoError(t, statErr)
		assert.Equal(t, os.FileMode(0600), fileInfo.Mode().Perm())
	}
}

func TestConfigurationManager_SaveConfiguration(t *testing.T) {
	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "bloco_config_test")
	require.NoError(t, err)
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Logf("Warning: could not remove temp dir: %v", err)
		}
	}()

	// Set environment variable to use temp directory
	originalEnv := os.Getenv("BLOCO_WALLET_APP_APP_DIR")
	defer func() {
		if originalEnv != "" {
			if err := os.Setenv("BLOCO_WALLET_APP_APP_DIR", originalEnv); err != nil {
				t.Logf("Warning: could not restore env var: %v", err)
			}
		} else {
			if err := os.Unsetenv("BLOCO_WALLET_APP_APP_DIR"); err != nil {
				t.Logf("Warning: could not unset env var: %v", err)
			}
		}
	}()
	if err := os.Setenv("BLOCO_WALLET_APP_APP_DIR", tempDir); err != nil {
		t.Fatalf("Failed to set environment variable: %v", err)
	}

	cm := NewConfigurationManager()

	// Load initial configuration
	cfg, err := cm.LoadConfiguration()
	require.NoError(t, err)

	// Add a test network
	testNetwork := Network{
		Name:        "Test Network",
		RPCEndpoint: "https://test.rpc.com",
		ChainID:     12345,
		Symbol:      "TEST",
		Explorer:    "https://test.explorer.com",
		IsActive:    true,
	}
	cfg.Networks["test_network_12345"] = testNetwork

	// Save the configuration
	err = cm.SaveConfiguration(cfg)
	assert.NoError(t, err)

	// Reload and verify the network was saved
	reloadedCfg, err := cm.LoadConfiguration()
	assert.NoError(t, err)
	assert.Contains(t, reloadedCfg.Networks, "test_network_12345")

	savedNetwork := reloadedCfg.Networks["test_network_12345"]
	assert.Equal(t, testNetwork.Name, savedNetwork.Name)
	assert.Equal(t, testNetwork.RPCEndpoint, savedNetwork.RPCEndpoint)
	assert.Equal(t, testNetwork.ChainID, savedNetwork.ChainID)
	assert.Equal(t, testNetwork.Symbol, savedNetwork.Symbol)
	assert.Equal(t, testNetwork.Explorer, savedNetwork.Explorer)
	assert.Equal(t, testNetwork.IsActive, savedNetwork.IsActive)
}

func TestConfigurationManager_GetConfigPath(t *testing.T) {
	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "bloco_config_test")
	require.NoError(t, err)
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Logf("Warning: could not remove temp dir: %v", err)
		}
	}()

	// Set environment variable to use temp directory
	originalEnv := os.Getenv("BLOCO_WALLET_APP_APP_DIR")
	defer func() {
		if originalEnv != "" {
			if err := os.Setenv("BLOCO_WALLET_APP_APP_DIR", originalEnv); err != nil {
				t.Logf("Warning: could not restore env var: %v", err)
			}
		} else {
			if err := os.Unsetenv("BLOCO_WALLET_APP_APP_DIR"); err != nil {
				t.Logf("Warning: could not unset env var: %v", err)
			}
		}
	}()
	if err := os.Setenv("BLOCO_WALLET_APP_APP_DIR", tempDir); err != nil {
		t.Fatalf("Failed to set environment variable: %v", err)
	}

	cm := NewConfigurationManager()

	// Before loading, path should be empty
	assert.Empty(t, cm.GetConfigPath())

	// After loading, path should be set
	_, err = cm.LoadConfiguration()
	require.NoError(t, err)

	expectedPath := filepath.Join(tempDir, "config.toml")
	assert.Equal(t, expectedPath, cm.GetConfigPath())
}

func TestConfigurationManager_GetAppDirectory(t *testing.T) {
	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "bloco_config_test")
	require.NoError(t, err)
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Logf("Warning: could not remove temp dir: %v", err)
		}
	}()

	// Set environment variable to use temp directory
	originalEnv := os.Getenv("BLOCO_WALLET_APP_APP_DIR")
	defer func() {
		if originalEnv != "" {
			if err := os.Setenv("BLOCO_WALLET_APP_APP_DIR", originalEnv); err != nil {
				t.Logf("Warning: could not restore env var: %v", err)
			}
		} else {
			if err := os.Unsetenv("BLOCO_WALLET_APP_APP_DIR"); err != nil {
				t.Logf("Warning: could not unset env var: %v", err)
			}
		}
	}()
	if err := os.Setenv("BLOCO_WALLET_APP_APP_DIR", tempDir); err != nil {
		t.Fatalf("Failed to set environment variable: %v", err)
	}

	cm := NewConfigurationManager()

	// Before loading, app directory should be empty
	assert.Empty(t, cm.GetAppDirectory())

	// After loading, app directory should be set
	_, err = cm.LoadConfiguration()
	require.NoError(t, err)

	assert.Equal(t, tempDir, cm.GetAppDirectory())
}

func TestConfigurationManager_ReloadConfiguration(t *testing.T) {
	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "bloco_config_test")
	require.NoError(t, err)
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Logf("Warning: could not remove temp dir: %v", err)
		}
	}()

	// Set environment variable to use temp directory
	originalEnv := os.Getenv("BLOCO_WALLET_APP_APP_DIR")
	defer func() {
		if originalEnv != "" {
			if err := os.Setenv("BLOCO_WALLET_APP_APP_DIR", originalEnv); err != nil {
				t.Logf("Warning: could not restore env var: %v", err)
			}
		} else {
			if err := os.Unsetenv("BLOCO_WALLET_APP_APP_DIR"); err != nil {
				t.Logf("Warning: could not unset env var: %v", err)
			}
		}
	}()
	if err := os.Setenv("BLOCO_WALLET_APP_APP_DIR", tempDir); err != nil {
		t.Fatalf("Failed to set environment variable: %v", err)
	}

	cm := NewConfigurationManager()

	// Load initial configuration
	cfg, err := cm.LoadConfiguration()
	require.NoError(t, err)

	// Add a network and save
	testNetwork := Network{
		Name:        "Test Network",
		RPCEndpoint: "https://test.rpc.com",
		ChainID:     12345,
		Symbol:      "TEST",
		Explorer:    "https://test.explorer.com",
		IsActive:    true,
	}
	cfg.Networks["test_network_12345"] = testNetwork
	err = cm.SaveConfiguration(cfg)
	require.NoError(t, err)

	// Reload configuration
	reloadedCfg, err := cm.ReloadConfiguration()
	assert.NoError(t, err)
	assert.Contains(t, reloadedCfg.Networks, "test_network_12345")
}

func TestConfigurationManager_EnvironmentVariables(t *testing.T) {
	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "bloco_config_test")
	require.NoError(t, err)
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Logf("Warning: could not remove temp dir: %v", err)
		}
	}()

	// Set environment variables
	originalEnvs := map[string]string{
		"BLOCO_WALLET_APP_APP_DIR":   os.Getenv("BLOCO_WALLET_APP_APP_DIR"),
		"BLOCO_WALLET_APP_LANGUAGE":  os.Getenv("BLOCO_WALLET_APP_LANGUAGE"),
		"BLOCO_WALLET_DATABASE_TYPE": os.Getenv("BLOCO_WALLET_DATABASE_TYPE"),
	}
	defer func() {
		for key, value := range originalEnvs {
			if value != "" {
				if err := os.Setenv(key, value); err != nil {
					t.Logf("Warning: could not restore env var %s: %v", key, err)
				}
			} else {
				if err := os.Unsetenv(key); err != nil {
					t.Logf("Warning: could not unset env var %s: %v", key, err)
				}
			}
		}
	}()

	if err := os.Setenv("BLOCO_WALLET_APP_APP_DIR", tempDir); err != nil {
		t.Fatalf("Failed to set environment variable: %v", err)
	}
	if err := os.Setenv("BLOCO_WALLET_APP_LANGUAGE", "pt"); err != nil {
		t.Logf("Warning: could not set env var: %v", err)
	}
	if err := os.Setenv("BLOCO_WALLET_DATABASE_TYPE", "sqlite"); err != nil {
		t.Logf("Warning: could not set env var: %v", err)
	}

	cm := NewConfigurationManager()
	cfg, err := cm.LoadConfiguration()

	assert.NoError(t, err)
	assert.Equal(t, tempDir, cfg.AppDir)
	assert.Equal(t, "pt", cfg.Language)
	assert.Equal(t, "sqlite", cfg.Database.Type)
}

func TestConfigurationManager_LegacyEnvironmentVariables(t *testing.T) {
	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "bloco_config_test")
	require.NoError(t, err)
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Logf("Warning: could not remove temp dir: %v", err)
		}
	}()

	walletsDir := filepath.Join(tempDir, "legacy_wallets")
	dbPath := filepath.Join(tempDir, "legacy.db")

	// Set legacy environment variables
	originalEnvs := map[string]string{
		"BLOCO_WALLET_APP_APP_DIR":       os.Getenv("BLOCO_WALLET_APP_APP_DIR"),
		"BLOCO_WALLET_APP_KEYSTORE_DIR":  os.Getenv("BLOCO_WALLET_APP_KEYSTORE_DIR"),
		"BLOCO_WALLET_APP_DATABASE_PATH": os.Getenv("BLOCO_WALLET_APP_DATABASE_PATH"),
	}
	defer func() {
		for key, value := range originalEnvs {
			if value != "" {
				if err := os.Setenv(key, value); err != nil {
					t.Logf("Warning: could not restore env var %s: %v", key, err)
				}
			} else {
				if err := os.Unsetenv(key); err != nil {
					t.Logf("Warning: could not unset env var %s: %v", key, err)
				}
			}
		}
	}()

	if err := os.Setenv("BLOCO_WALLET_APP_APP_DIR", tempDir); err != nil {
		t.Fatalf("Failed to set environment variable: %v", err)
	}
	if err := os.Setenv("BLOCO_WALLET_APP_KEYSTORE_DIR", walletsDir); err != nil {
		t.Logf("Warning: could not set env var: %v", err)
	}
	if err := os.Setenv("BLOCO_WALLET_APP_DATABASE_PATH", dbPath); err != nil {
		t.Logf("Warning: could not set env var: %v", err)
	}

	cm := NewConfigurationManager()
	cfg, err := cm.LoadConfiguration()

	assert.NoError(t, err)
	assert.Equal(t, tempDir, cfg.AppDir)
	assert.Equal(t, walletsDir, cfg.WalletsDir)
	assert.Equal(t, dbPath, cfg.DatabasePath)
}

func TestConfigurationManagerAtomicSavePreservesPreviousConfig(t *testing.T) {
	t.Setenv("BLOCO_WALLET_APP_APP_DIR", t.TempDir())
	manager := NewConfigurationManager()
	cfg, err := manager.LoadConfiguration()
	require.NoError(t, err)
	before, err := os.ReadFile(manager.GetConfigPath())
	require.NoError(t, err)
	cfg.Networks["invalid"] = Network{
		Name:           "Invalid",
		RPCEndpoint:    "https://rpc.example.com",
		RPCEndpointRef: "env:BLOCO_TEST_RPC_URL",
		ChainID:        1,
	}
	require.Error(t, manager.SaveConfiguration(cfg))
	after, err := os.ReadFile(manager.GetConfigPath())
	require.NoError(t, err)
	assert.Equal(t, before, after)
}

func TestConfigurationManagerRejectsResolvedEnvironmentSecret(t *testing.T) {
	t.Setenv("BLOCO_WALLET_APP_APP_DIR", t.TempDir())
	t.Setenv("BLOCO_TEST_RPC_URL", "https://rpc.example.com/super-secret-token")
	manager := NewConfigurationManager()
	cfg, err := manager.LoadConfiguration()
	require.NoError(t, err)
	before, err := os.ReadFile(manager.GetConfigPath())
	require.NoError(t, err)
	cfg.Networks["leak"] = Network{Name: "Leak", RPCEndpoint: os.Getenv("BLOCO_TEST_RPC_URL"), ChainID: 1}
	require.Error(t, manager.SaveConfiguration(cfg))
	after, err := os.ReadFile(manager.GetConfigPath())
	require.NoError(t, err)
	assert.Equal(t, before, after)
	assert.NotContains(t, string(after), "super-secret-token")
}

func TestConfigurationManagerPersistsCredentialReferenceNotSecret(t *testing.T) {
	t.Setenv("BLOCO_WALLET_APP_APP_DIR", t.TempDir())
	t.Setenv("BLOCO_TEST_RPC_URL", "https://rpc.example.com/super-secret-token")
	manager := NewConfigurationManager()
	cfg, err := manager.LoadConfiguration()
	require.NoError(t, err)
	cfg.Networks["referenced"] = Network{
		Name:           "Referenced",
		RPCEndpointRef: "env:BLOCO_TEST_RPC_URL",
		ChainID:        1,
		Symbol:         "ETH",
		IsActive:       true,
	}
	require.NoError(t, manager.SaveConfiguration(cfg))
	data, err := os.ReadFile(manager.GetConfigPath())
	require.NoError(t, err)
	assert.Contains(t, string(data), "env:BLOCO_TEST_RPC_URL")
	assert.NotContains(t, string(data), "super-secret-token")
	if runtime.GOOS != "windows" {
		info, err := os.Stat(manager.GetConfigPath())
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0600), info.Mode().Perm())
	}
}

func TestConfigurationManagerBindsRevisionToLoadedSnapshot(t *testing.T) {
	t.Setenv("BLOCO_WALLET_APP_APP_DIR", t.TempDir())
	manager := NewConfigurationManager()
	first, err := manager.LoadConfiguration()
	require.NoError(t, err)
	second, err := manager.LoadConfiguration()
	require.NoError(t, err)
	first.Language = "pt"
	require.NoError(t, manager.SaveConfiguration(first))
	second.Language = "es"
	assert.ErrorIs(t, manager.SaveConfiguration(second), ErrConfigConflict)
}

func TestConfigurationManagerRejectsStaleConcurrentSave(t *testing.T) {
	t.Setenv("BLOCO_WALLET_APP_APP_DIR", t.TempDir())
	first := NewConfigurationManager()
	second := NewConfigurationManager()
	firstConfig, err := first.LoadConfiguration()
	require.NoError(t, err)
	secondConfig, err := second.LoadConfiguration()
	require.NoError(t, err)
	firstConfig.Language = "pt"
	require.NoError(t, first.SaveConfiguration(firstConfig))
	secondConfig.Language = "es"
	err = second.SaveConfiguration(secondConfig)
	assert.ErrorIs(t, err, ErrConfigConflict)
	reloaded, err := first.ReloadConfiguration()
	require.NoError(t, err)
	assert.Equal(t, "pt", reloaded.Language)
}

func TestConfigurationManagerSerializesConcurrentWriters(t *testing.T) {
	t.Setenv("BLOCO_WALLET_APP_APP_DIR", t.TempDir())
	first := NewConfigurationManager()
	second := NewConfigurationManager()
	firstConfig, err := first.LoadConfiguration()
	require.NoError(t, err)
	secondConfig, err := second.LoadConfiguration()
	require.NoError(t, err)
	firstConfig.Language = "pt"
	secondConfig.Language = "es"
	start := make(chan struct{})
	results := make(chan error, 2)
	go func() { <-start; results <- first.SaveConfiguration(firstConfig) }()
	go func() { <-start; results <- second.SaveConfiguration(secondConfig) }()
	close(start)
	firstErr := <-results
	secondErr := <-results
	successes := 0
	conflicts := 0
	for _, result := range []error{firstErr, secondErr} {
		if result == nil {
			successes++
		} else if errors.Is(result, ErrConfigConflict) {
			conflicts++
		}
	}
	assert.Equal(t, 1, successes)
	assert.Equal(t, 1, conflicts)
}

func TestConfigurationManagerRejectsCredentialBearingConfigOnLoad(t *testing.T) {
	root := t.TempDir()
	t.Setenv("BLOCO_WALLET_APP_APP_DIR", root)
	manager := NewConfigurationManager()
	_, err := manager.LoadConfiguration()
	require.NoError(t, err)
	malicious := `[app]
language = "en"
[database]
type = "sqlite"
[networks.leak]
name = "Leak"
rpc_endpoint = "https://rpc.example.com/v3/super-secret"
chain_id = 1
symbol = "ETH"
is_active = true
`
	require.NoError(t, os.WriteFile(manager.GetConfigPath(), []byte(malicious), 0600))
	_, err = manager.ReloadConfiguration()
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "super-secret")
}

func TestConfigurationManagerRejectsSymlinkAppDirectoryBeforeChmod(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink permissions vary on Windows")
	}
	target := t.TempDir()
	require.NoError(t, os.Chmod(target, 0755))
	link := filepath.Join(t.TempDir(), "config-link")
	require.NoError(t, os.Symlink(target, link))
	t.Setenv("BLOCO_WALLET_APP_APP_DIR", link)
	_, err := NewConfigurationManager().LoadConfiguration()
	require.Error(t, err)
	info, err := os.Stat(target)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0755), info.Mode().Perm())
}

func TestConfigurationManagerRejectsSymlinkConfig(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink permissions vary on Windows")
	}
	root := t.TempDir()
	t.Setenv("BLOCO_WALLET_APP_APP_DIR", root)
	manager := NewConfigurationManager()
	_, err := manager.LoadConfiguration()
	require.NoError(t, err)
	victim := filepath.Join(t.TempDir(), "victim.toml")
	require.NoError(t, os.WriteFile(victim, []byte("unchanged"), 0600))
	require.NoError(t, os.Remove(manager.GetConfigPath()))
	require.NoError(t, os.Symlink(victim, manager.GetConfigPath()))
	_, err = manager.LoadConfiguration()
	require.Error(t, err)
	data, err := os.ReadFile(victim)
	require.NoError(t, err)
	assert.Equal(t, "unchanged", string(data))
}

func TestConfigurationManager_SaveConfiguration_WithoutLoad(t *testing.T) {
	cm := NewConfigurationManager()
	cfg := &Config{
		Language: "en",
	}

	err := cm.SaveConfiguration(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "configuration not loaded")
}

func TestConfigurationManager_ReloadConfiguration_WithoutLoad(t *testing.T) {
	cm := NewConfigurationManager()

	_, err := cm.ReloadConfiguration()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "configuration not initialized")
}
