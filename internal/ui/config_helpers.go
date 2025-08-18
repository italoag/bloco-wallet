package ui

import (
	"blocowallet/internal/blockchain"
	"blocowallet/pkg/config"
	"fmt"
)

// Global configuration manager instance
var globalConfigManager *config.ConfigurationManager

// Global network manager instance
var globalNetworkManager *NetworkManager

// getConfigurationManager returns the global configuration manager instance
func getConfigurationManager() *config.ConfigurationManager {
	if globalConfigManager == nil {
		globalConfigManager = config.NewConfigurationManager()
	}
	return globalConfigManager
}

// loadOrCreateConfig loads the configuration using the ConfigurationManager
func loadOrCreateConfig() (*config.Config, error) {
	cm := getConfigurationManager()
	cfg, err := cm.LoadConfiguration()
	if err != nil {
		return nil, fmt.Errorf("failed to load configuration: %w", err)
	}
	return cfg, nil
}

// updateLanguageInConfig updates the language in the configuration file
func updateLanguageInConfig(language string) error {
	cm := getConfigurationManager()

	// Load current configuration
	cfg, err := cm.LoadConfiguration()
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	// Update the language
	cfg.Language = language

	// Save the updated configuration
	if err := cm.SaveConfiguration(cfg); err != nil {
		return fmt.Errorf("failed to save configuration: %w", err)
	}

	return nil
}

// getNetworkManager returns the global network manager instance
func getNetworkManager() *NetworkManager {
	if globalNetworkManager == nil {
		configManager := getConfigurationManager()
		chainListService := blockchain.NewChainListService()
		globalNetworkManager = NewNetworkManager(configManager, chainListService)
	}
	return globalNetworkManager
}

// addNetworkWithClassification adds a network using the NetworkManager with automatic classification
func addNetworkWithClassification(network config.Network) error {
	nm := getNetworkManager()
	return nm.AddNetwork(network)
}

// removeNetworkWithManager removes a network using the NetworkManager
func removeNetworkWithManager(key string) error {
	nm := getNetworkManager()
	return nm.RemoveNetwork(key)
}

// loadNetworksWithManager loads networks using the NetworkManager
func loadNetworksWithManager() (map[string]config.Network, error) {
	nm := getNetworkManager()
	return nm.LoadNetworks()
}

// getNetworkWithManager gets a specific network using the NetworkManager
func getNetworkWithManager(key string) (*config.Network, error) {
	nm := getNetworkManager()
	return nm.GetNetwork(key)
}

// listNetworksWithClassification lists networks with their classification information
func listNetworksWithClassification() (map[string]NetworkInfo, error) {
	nm := getNetworkManager()
	return nm.ListNetworks()
}

// migrateExistingNetworks migrates existing networks to the new classification system
func migrateExistingNetworks() error {
	nm := getNetworkManager()
	return nm.MigrateExistingNetworks()
}
