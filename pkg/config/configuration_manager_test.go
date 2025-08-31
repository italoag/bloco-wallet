package config

import (
	"os"
	"path/filepath"
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
	defer os.RemoveAll(tempDir)

	// Set environment variable to use temp directory
	originalEnv := os.Getenv("BLOCO_WALLET_APP_APP_DIR")
	defer func() {
		if originalEnv != "" {
			os.Setenv("BLOCO_WALLET_APP_APP_DIR", originalEnv)
		} else {
			os.Unsetenv("BLOCO_WALLET_APP_APP_DIR")
		}
	}()
	os.Setenv("BLOCO_WALLET_APP_APP_DIR", tempDir)

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
}

func TestConfigurationManager_SaveConfiguration(t *testing.T) {
	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "bloco_config_test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Set environment variable to use temp directory
	originalEnv := os.Getenv("BLOCO_WALLET_APP_APP_DIR")
	defer func() {
		if originalEnv != "" {
			os.Setenv("BLOCO_WALLET_APP_APP_DIR", originalEnv)
		} else {
			os.Unsetenv("BLOCO_WALLET_APP_APP_DIR")
		}
	}()
	os.Setenv("BLOCO_WALLET_APP_APP_DIR", tempDir)

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
	defer os.RemoveAll(tempDir)

	// Set environment variable to use temp directory
	originalEnv := os.Getenv("BLOCO_WALLET_APP_APP_DIR")
	defer func() {
		if originalEnv != "" {
			os.Setenv("BLOCO_WALLET_APP_APP_DIR", originalEnv)
		} else {
			os.Unsetenv("BLOCO_WALLET_APP_APP_DIR")
		}
	}()
	os.Setenv("BLOCO_WALLET_APP_APP_DIR", tempDir)

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
	defer os.RemoveAll(tempDir)

	// Set environment variable to use temp directory
	originalEnv := os.Getenv("BLOCO_WALLET_APP_APP_DIR")
	defer func() {
		if originalEnv != "" {
			os.Setenv("BLOCO_WALLET_APP_APP_DIR", originalEnv)
		} else {
			os.Unsetenv("BLOCO_WALLET_APP_APP_DIR")
		}
	}()
	os.Setenv("BLOCO_WALLET_APP_APP_DIR", tempDir)

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
	defer os.RemoveAll(tempDir)

	// Set environment variable to use temp directory
	originalEnv := os.Getenv("BLOCO_WALLET_APP_APP_DIR")
	defer func() {
		if originalEnv != "" {
			os.Setenv("BLOCO_WALLET_APP_APP_DIR", originalEnv)
		} else {
			os.Unsetenv("BLOCO_WALLET_APP_APP_DIR")
		}
	}()
	os.Setenv("BLOCO_WALLET_APP_APP_DIR", tempDir)

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
	defer os.RemoveAll(tempDir)

	// Set environment variables
	originalEnvs := map[string]string{
		"BLOCO_WALLET_APP_APP_DIR":   os.Getenv("BLOCO_WALLET_APP_APP_DIR"),
		"BLOCO_WALLET_APP_LANGUAGE":  os.Getenv("BLOCO_WALLET_APP_LANGUAGE"),
		"BLOCO_WALLET_DATABASE_TYPE": os.Getenv("BLOCO_WALLET_DATABASE_TYPE"),
	}
	defer func() {
		for key, value := range originalEnvs {
			if value != "" {
				os.Setenv(key, value)
			} else {
				os.Unsetenv(key)
			}
		}
	}()

	os.Setenv("BLOCO_WALLET_APP_APP_DIR", tempDir)
	os.Setenv("BLOCO_WALLET_APP_LANGUAGE", "pt")
	os.Setenv("BLOCO_WALLET_DATABASE_TYPE", "sqlite")

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
	defer os.RemoveAll(tempDir)

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
				os.Setenv(key, value)
			} else {
				os.Unsetenv(key)
			}
		}
	}()

	os.Setenv("BLOCO_WALLET_APP_APP_DIR", tempDir)
	os.Setenv("BLOCO_WALLET_APP_KEYSTORE_DIR", walletsDir)
	os.Setenv("BLOCO_WALLET_APP_DATABASE_PATH", dbPath)

	cm := NewConfigurationManager()
	cfg, err := cm.LoadConfiguration()

	assert.NoError(t, err)
	assert.Equal(t, tempDir, cfg.AppDir)
	assert.Equal(t, walletsDir, cfg.WalletsDir)
	assert.Equal(t, dbPath, cfg.DatabasePath)
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
