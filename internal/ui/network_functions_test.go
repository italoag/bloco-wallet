package ui

import (
	"blocowallet/pkg/config"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
)

func TestSaveConfigToFile(t *testing.T) {
	// Create a temporary directory for the test
	tempDir, err := os.MkdirTemp("", "blocowallet-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
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

	// Reset the global configuration manager to pick up the new environment variable
	globalConfigManager = nil

	// Load configuration using ConfigurationManager
	cfg, err := loadOrCreateConfig()
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	// Add some test networks
	cfg.Networks["custom_ethereum_1"] = config.Network{
		Name:        "Ethereum",
		RPCEndpoint: "https://eth.example.com",
		ChainID:     1,
		Symbol:      "ETH",
		Explorer:    "https://etherscan.io",
		IsActive:    true,
	}

	cfg.Networks["custom_polygon_137"] = config.Network{
		Name:        "Polygon",
		RPCEndpoint: "https://polygon.example.com",
		ChainID:     137,
		Symbol:      "MATIC",
		Explorer:    "https://polygonscan.com",
		IsActive:    true,
	}

	// Create a model with the test config
	model := &CLIModel{
		currentConfig: cfg,
	}

	// Save the config
	err = model.saveConfigToFile()
	assert.NoError(t, err)

	// Check if the file was created
	configPath := filepath.Join(tempDir, "config.toml")
	_, err = os.Stat(configPath)
	assert.NoError(t, err)

	// Read the config back with Viper
	v := viper.New()
	v.SetConfigFile(configPath)
	err = v.ReadInConfig()
	assert.NoError(t, err)

	// Check if the networks were saved correctly
	assert.Equal(t, "Ethereum", v.GetString("networks.custom_ethereum_1.name"))
	assert.Equal(t, int64(1), v.GetInt64("networks.custom_ethereum_1.chain_id"))
	assert.Equal(t, "ETH", v.GetString("networks.custom_ethereum_1.symbol"))
	assert.Equal(t, "https://eth.example.com", v.GetString("networks.custom_ethereum_1.rpc_endpoint"))
	assert.Equal(t, true, v.GetBool("networks.custom_ethereum_1.is_active"))

	assert.Equal(t, "Polygon", v.GetString("networks.custom_polygon_137.name"))
	assert.Equal(t, int64(137), v.GetInt64("networks.custom_polygon_137.chain_id"))
	assert.Equal(t, "MATIC", v.GetString("networks.custom_polygon_137.symbol"))
	assert.Equal(t, "https://polygon.example.com", v.GetString("networks.custom_polygon_137.rpc_endpoint"))
	assert.Equal(t, true, v.GetBool("networks.custom_polygon_137.is_active"))

	// Add a third network and save again to test updating
	cfg.Networks["custom_amoy_80002"] = config.Network{
		Name:        "Amoy",
		RPCEndpoint: "https://polygon-amoy.drpc.org",
		ChainID:     80002,
		Symbol:      "POL",
		Explorer:    "",
		IsActive:    true,
	}

	// Save the config again
	err = model.saveConfigToFile()
	assert.NoError(t, err)

	// Read the config back again
	v = viper.New()
	v.SetConfigFile(configPath)
	err = v.ReadInConfig()
	assert.NoError(t, err)

	// Check if all three networks are there
	assert.Equal(t, "Ethereum", v.GetString("networks.custom_ethereum_1.name"))
	assert.Equal(t, "Polygon", v.GetString("networks.custom_polygon_137.name"))
	assert.Equal(t, "Amoy", v.GetString("networks.custom_amoy_80002.name"))
	assert.Equal(t, int64(80002), v.GetInt64("networks.custom_amoy_80002.chain_id"))
	assert.Equal(t, "POL", v.GetString("networks.custom_amoy_80002.symbol"))
	assert.Equal(t, "https://polygon-amoy.drpc.org", v.GetString("networks.custom_amoy_80002.rpc_endpoint"))
	assert.Equal(t, true, v.GetBool("networks.custom_amoy_80002.is_active"))

	// Test removing a network
	delete(cfg.Networks, "custom_polygon_137")
	err = model.saveConfigToFile()
	assert.NoError(t, err)

	// Read the config back again
	v = viper.New()
	v.SetConfigFile(configPath)
	err = v.ReadInConfig()
	assert.NoError(t, err)

	// Check if the remaining networks are still there
	assert.Equal(t, "Ethereum", v.GetString("networks.custom_ethereum_1.name"))
	assert.Equal(t, "Amoy", v.GetString("networks.custom_amoy_80002.name"))

	// O Viper pode manter valores antigos no mapa interno mesmo após a remoção
	// O importante é que a rede não apareça quando carregamos a configuração novamente
	reloadedCfg, err := config.LoadConfig(tempDir)
	assert.NoError(t, err)
	_, exists := reloadedCfg.Networks["custom_polygon_137"]
	assert.False(t, exists, "A rede removida não deveria existir após recarregar a configuração")
}

func TestSanitizeNetworkKey(t *testing.T) {
	testCases := []struct {
		input    string
		expected string
	}{
		{"normal_key", "normal_key"},
		{"key with spaces", "key_with_spaces"},
		{"key-with-hyphens", "key_with_hyphens"},
		{"key.with.dots", "key_with_dots"},
		{"key@with#special$chars", "key_with_special_chars"},
		{"123_numeric_start", "123_numeric_start"},
		{"", ""},
		{"UPPERCASE", "UPPERCASE"},
		{"MixedCase", "MixedCase"},
	}

	for _, tc := range testCases {
		result := sanitizeNetworkKey(tc.input)
		assert.Equal(t, tc.expected, result)
	}
}
