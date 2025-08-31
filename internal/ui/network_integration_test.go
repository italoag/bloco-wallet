package ui

import (
	"blocowallet/pkg/config"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNetworkConfigurationIntegration(t *testing.T) {
	// Create a temporary directory for the test
	tempDir, err := os.MkdirTemp("", "blocowallet-integration-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Logf("Failed to remove temp directory: %v", err)
		}
	}()

	// Set environment variable to use temp directory
	originalEnv := os.Getenv("BLOCO_WALLET_APP_APP_DIR")
	defer func() {
		if originalEnv != "" {
			if err := os.Setenv("BLOCO_WALLET_APP_APP_DIR", originalEnv); err != nil {
				t.Logf("Failed to restore environment variable: %v", err)
			}
		} else {
			if err := os.Unsetenv("BLOCO_WALLET_APP_APP_DIR"); err != nil {
				t.Logf("Failed to unset environment variable: %v", err)
			}
		}
	}()
	if err := os.Setenv("BLOCO_WALLET_APP_APP_DIR", tempDir); err != nil {
		t.Fatalf("Failed to set environment variable: %v", err)
	}

	// Reset the global configuration manager to pick up the new environment variable
	globalConfigManager = nil

	// Load configuration using ConfigurationManager
	cfg, err := loadOrCreateConfig()
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	// Create a model with the test config
	model := &CLIModel{
		currentConfig: cfg,
	}

	// Simulate adding a network
	network1 := config.Network{
		Name:        "Ethereum Mainnet",
		RPCEndpoint: "https://eth.drpc.org",
		ChainID:     1,
		Symbol:      "ETH",
		Explorer:    "",
		IsActive:    true,
	}
	key1 := "custom_ethereum_mainnet_1"
	model.currentConfig.Networks[key1] = network1

	// Save the config
	err = model.saveConfigToFile()
	assert.NoError(t, err)

	// Reload the config to verify it was saved correctly
	reloadedCfg, err := config.LoadConfig(tempDir)
	assert.NoError(t, err)
	assert.Contains(t, reloadedCfg.Networks, key1)
	assert.Equal(t, network1.Name, reloadedCfg.Networks[key1].Name)
	assert.Equal(t, network1.RPCEndpoint, reloadedCfg.Networks[key1].RPCEndpoint)
	assert.Equal(t, network1.ChainID, reloadedCfg.Networks[key1].ChainID)
	assert.Equal(t, network1.Symbol, reloadedCfg.Networks[key1].Symbol)
	assert.Equal(t, network1.IsActive, reloadedCfg.Networks[key1].IsActive)

	// Simulate adding a second network
	network2 := config.Network{
		Name:        "Amoy",
		RPCEndpoint: "https://polygon-amoy.drpc.org",
		ChainID:     80002,
		Symbol:      "POL",
		Explorer:    "",
		IsActive:    true,
	}
	key2 := "custom_amoy_80002"
	model.currentConfig.Networks[key2] = network2

	// Save the config again
	err = model.saveConfigToFile()
	assert.NoError(t, err)

	// Reload the config to verify both networks were saved correctly
	reloadedCfg, err = config.LoadConfig(tempDir)
	assert.NoError(t, err)
	assert.Contains(t, reloadedCfg.Networks, key1)
	assert.Contains(t, reloadedCfg.Networks, key2)
	assert.Equal(t, network1.Name, reloadedCfg.Networks[key1].Name)
	assert.Equal(t, network2.Name, reloadedCfg.Networks[key2].Name)

	// Simulate editing a network
	network1Modified := network1
	network1Modified.RPCEndpoint = "https://ethereum.publicnode.com"
	model.currentConfig.Networks[key1] = network1Modified

	// Save the config again
	err = model.saveConfigToFile()
	assert.NoError(t, err)

	// Reload the config to verify the network was updated correctly
	reloadedCfg, err = config.LoadConfig(tempDir)
	assert.NoError(t, err)
	assert.Contains(t, reloadedCfg.Networks, key1)
	assert.Equal(t, network1Modified.RPCEndpoint, reloadedCfg.Networks[key1].RPCEndpoint)

	// Simulate removing a network
	delete(model.currentConfig.Networks, key1)

	// Save the config again
	err = model.saveConfigToFile()
	assert.NoError(t, err)

	// Reload the config to verify the network was removed
	// Nota: O Viper pode manter valores antigos no mapa interno mesmo após a remoção
	// Vamos verificar o conteúdo do arquivo diretamente
	configPath := filepath.Join(tempDir, "config.toml")
	content, err := os.ReadFile(configPath)
	assert.NoError(t, err)

	// Converter para string para facilitar a verificação
	configContent := string(content)

	// Verificar se a seção da rede removida não existe no arquivo
	assert.NotContains(t, configContent, "[networks."+key1+"]", "A seção da rede removida não deveria existir no arquivo")

	// Verificar se a seção da outra rede ainda existe no arquivo
	assert.Contains(t, configContent, "[networks."+key2+"]", "A seção da outra rede deveria existir no arquivo")
	assert.Contains(t, reloadedCfg.Networks, key2)

	// Test with special characters in network name
	network3 := config.Network{
		Name:        "Test Network with Special Chars: @#$%^&*()",
		RPCEndpoint: "https://test.example.com",
		ChainID:     999,
		Symbol:      "TEST",
		Explorer:    "",
		IsActive:    true,
	}
	key3 := sanitizeNetworkKey("custom_test_network_with_special_chars_999")
	model.currentConfig.Networks[key3] = network3

	// Save the config again
	err = model.saveConfigToFile()
	assert.NoError(t, err)

	// Reload the config to verify the network with special characters was saved correctly
	reloadedCfg, err = config.LoadConfig(tempDir)
	assert.NoError(t, err)
	assert.Contains(t, reloadedCfg.Networks, key3)
	assert.Equal(t, network3.Name, reloadedCfg.Networks[key3].Name)
}
