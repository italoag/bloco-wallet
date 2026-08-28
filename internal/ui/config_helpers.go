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

var globalRPCGateway *blockchain.RPCGateway
var globalChainListService *blockchain.ChainListService

func ConfigureRPCGateway(gateway *blockchain.RPCGateway) {
	globalRPCGateway = gateway
	globalChainListService = nil
	globalNetworkManager = nil
}

func getRPCGateway() *blockchain.RPCGateway {
	if globalRPCGateway == nil {
		globalRPCGateway = blockchain.NewRPCGateway(blockchain.RPCGatewayOptions{})
	}
	return globalRPCGateway
}

func getChainListService() *blockchain.ChainListService {
	if globalChainListService == nil {
		globalChainListService = blockchain.NewChainListServiceWithGateway(getRPCGateway(), "https://chainlist.org")
	}
	return globalChainListService
}

func ConfigureConfigurationManager(manager *config.ConfigurationManager) {
	globalConfigManager = manager
	globalNetworkManager = nil
}

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
	if err := cm.SaveConfiguration(cfg); err != nil && !config.IsConfigCommitted(err) {
		return fmt.Errorf("failed to save configuration: %w", err)
	}

	return nil
}

// getNetworkManager returns the global network manager instance
func getNetworkManager() *NetworkManager {
	if globalNetworkManager == nil {
		configManager := getConfigurationManager()
		globalNetworkManager = NewNetworkManager(configManager, getChainListService())
	}
	return globalNetworkManager
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
