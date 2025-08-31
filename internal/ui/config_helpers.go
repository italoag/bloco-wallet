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

// addNetworkWithClassificationInfo adds a network and returns classification information
func addNetworkWithClassificationInfo(network config.Network) (*NetworkClassificationInfo, error) {
	nm := getNetworkManager()

	// Get classification information before adding
	classification, err := nm.classificationService.ClassifyNetwork(int(network.ChainID), network.Name, network.RPCEndpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to classify network: %w", err)
	}

	// Add the network
	err = nm.AddNetwork(network)
	if err != nil {
		return nil, err
	}

	// Return classification info
	return &NetworkClassificationInfo{
		Type:        classification.Type,
		IsValidated: classification.IsValidated,
		Source:      classification.Source,
		Key:         classification.Key,
		ChainInfo:   classification.ChainInfo,
	}, nil
}

// NetworkClassificationInfo contains information about network classification
type NetworkClassificationInfo struct {
	Type        blockchain.NetworkType `json:"type"`
	IsValidated bool                   `json:"is_validated"`
	Source      string                 `json:"source"`
	Key         string                 `json:"key"`
	ChainInfo   *blockchain.ChainInfo  `json:"chain_info,omitempty"`
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
