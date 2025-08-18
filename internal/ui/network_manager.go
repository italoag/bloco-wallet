package ui

import (
	"blocowallet/internal/blockchain"
	"blocowallet/pkg/config"
	"fmt"
)

// ConfigurationManagerInterface defines the interface for configuration management
type ConfigurationManagerInterface interface {
	LoadConfiguration() (*config.Config, error)
	SaveConfiguration(cfg *config.Config) error
	GetConfigPath() string
	GetAppDirectory() string
}

// NetworkManager manages network operations with automatic classification
type NetworkManager struct {
	configManager         ConfigurationManagerInterface
	classificationService *blockchain.NetworkClassificationService
	chainListService      blockchain.ChainListServiceInterface
}

// NewNetworkManager creates a new NetworkManager instance
func NewNetworkManager(configManager ConfigurationManagerInterface, chainListService blockchain.ChainListServiceInterface) *NetworkManager {
	classificationService := blockchain.NewNetworkClassificationService(chainListService)

	return &NetworkManager{
		configManager:         configManager,
		classificationService: classificationService,
		chainListService:      chainListService,
	}
}

// AddNetwork adds a new network with automatic classification
func (nm *NetworkManager) AddNetwork(network config.Network) error {
	// Load current configuration
	cfg, err := nm.configManager.LoadConfiguration()
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	// Initialize Networks map if it's nil
	if cfg.Networks == nil {
		cfg.Networks = make(map[string]config.Network)
	}

	// Classify the network
	classification, err := nm.classificationService.ClassifyNetwork(int(network.ChainID), network.Name, network.RPCEndpoint)
	if err != nil {
		return fmt.Errorf("failed to classify network: %w", err)
	}

	// If the network is standard and we have chain info, enhance the network data
	if classification.Type == blockchain.NetworkTypeStandard && classification.ChainInfo != nil {
		// Use chainlist data to fill in missing information
		if network.Symbol == "" {
			network.Symbol = classification.ChainInfo.NativeCurrency.Symbol
		}
		if network.Explorer == "" && len(classification.ChainInfo.Explorers) > 0 {
			network.Explorer = classification.ChainInfo.Explorers[0].URL
		}
		// Use the working RPC endpoint if available
		if classification.ChainInfo != nil {
			_, workingRPC, err := nm.chainListService.GetChainInfoWithRetry(int(network.ChainID))
			if err == nil && workingRPC != "" && network.RPCEndpoint == "" {
				network.RPCEndpoint = workingRPC
			}
		}
	}

	// Check if network already exists
	if _, exists := cfg.Networks[classification.Key]; exists {
		return fmt.Errorf("network with key '%s' already exists", classification.Key)
	}

	// Add the network to the configuration
	cfg.Networks[classification.Key] = network

	// Save the configuration
	if err := nm.configManager.SaveConfiguration(cfg); err != nil {
		return fmt.Errorf("failed to save configuration: %w", err)
	}

	return nil
}

// UpdateNetwork updates an existing network
func (nm *NetworkManager) UpdateNetwork(key string, network config.Network) error {
	// Load current configuration
	cfg, err := nm.configManager.LoadConfiguration()
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	// Initialize Networks map if it's nil
	if cfg.Networks == nil {
		cfg.Networks = make(map[string]config.Network)
	}

	// Check if network exists
	if _, exists := cfg.Networks[key]; !exists {
		return fmt.Errorf("network with key '%s' not found", key)
	}

	// Classify the updated network to determine if the key should change
	classification, err := nm.classificationService.ClassifyNetwork(int(network.ChainID), network.Name, network.RPCEndpoint)
	if err != nil {
		return fmt.Errorf("failed to classify updated network: %w", err)
	}

	// If the classification results in a different key, we need to handle the migration
	if classification.Key != key {
		// Remove the old entry
		delete(cfg.Networks, key)

		// Check if the new key already exists
		if _, exists := cfg.Networks[classification.Key]; exists {
			return fmt.Errorf("cannot update network: a network with key '%s' already exists", classification.Key)
		}

		// Add with the new key
		cfg.Networks[classification.Key] = network
	} else {
		// Update in place
		cfg.Networks[key] = network
	}

	// Save the configuration
	if err := nm.configManager.SaveConfiguration(cfg); err != nil {
		return fmt.Errorf("failed to save configuration: %w", err)
	}

	return nil
}

// RemoveNetwork removes a network from the configuration
func (nm *NetworkManager) RemoveNetwork(key string) error {
	// Load current configuration
	cfg, err := nm.configManager.LoadConfiguration()
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	// Initialize Networks map if it's nil
	if cfg.Networks == nil {
		cfg.Networks = make(map[string]config.Network)
	}

	// Check if network exists
	if _, exists := cfg.Networks[key]; !exists {
		return fmt.Errorf("network with key '%s' not found", key)
	}

	// Remove the network
	delete(cfg.Networks, key)

	// Save the configuration
	if err := nm.configManager.SaveConfiguration(cfg); err != nil {
		return fmt.Errorf("failed to save configuration: %w", err)
	}

	return nil
}

// LoadNetworks loads all networks from the Viper configuration
func (nm *NetworkManager) LoadNetworks() (map[string]config.Network, error) {
	// Load configuration using ConfigurationManager
	cfg, err := nm.configManager.LoadConfiguration()
	if err != nil {
		return nil, fmt.Errorf("failed to load configuration: %w", err)
	}

	// Initialize Networks map if it's nil
	if cfg.Networks == nil {
		cfg.Networks = make(map[string]config.Network)
	}

	return cfg.Networks, nil
}

// GetNetwork retrieves a specific network by key
func (nm *NetworkManager) GetNetwork(key string) (*config.Network, error) {
	networks, err := nm.LoadNetworks()
	if err != nil {
		return nil, fmt.Errorf("failed to load networks: %w", err)
	}

	network, exists := networks[key]
	if !exists {
		return nil, fmt.Errorf("network with key '%s' not found", key)
	}

	return &network, nil
}

// ListNetworks returns all networks with their classification information
func (nm *NetworkManager) ListNetworks() (map[string]NetworkInfo, error) {
	networks, err := nm.LoadNetworks()
	if err != nil {
		return nil, fmt.Errorf("failed to load networks: %w", err)
	}

	result := make(map[string]NetworkInfo)

	for key, network := range networks {
		// Classify existing network to get type information
		classification, err := nm.classificationService.ClassifyExistingNetwork(key, int(network.ChainID), network.Name, network.RPCEndpoint)
		if err != nil {
			// If classification fails, treat as custom
			classification = &blockchain.NetworkClassification{
				Type:        blockchain.NetworkTypeCustom,
				IsValidated: false,
				ChainInfo:   nil,
				Key:         key,
				Source:      "manual",
			}
		}

		result[key] = NetworkInfo{
			Network:     network,
			Type:        classification.Type,
			IsValidated: classification.IsValidated,
			Source:      classification.Source,
			ChainInfo:   classification.ChainInfo,
		}
	}

	return result, nil
}

// NetworkInfo contains network information with classification details
type NetworkInfo struct {
	Network     config.Network         `json:"network"`
	Type        blockchain.NetworkType `json:"type"`
	IsValidated bool                   `json:"is_validated"`
	Source      string                 `json:"source"`
	ChainInfo   *blockchain.ChainInfo  `json:"chain_info,omitempty"`
}

// MigrateExistingNetworks migrates existing networks to the new classification system
func (nm *NetworkManager) MigrateExistingNetworks() error {
	// Load current configuration
	cfg, err := nm.configManager.LoadConfiguration()
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	// Initialize Networks map if it's nil
	if cfg.Networks == nil {
		cfg.Networks = make(map[string]config.Network)
		return nil // Nothing to migrate
	}

	migrationNeeded := false
	newNetworks := make(map[string]config.Network)

	for key, network := range cfg.Networks {
		// Check if the network already has proper classification
		if nm.classificationService.IsNetworkStandard(key) || nm.classificationService.IsNetworkCustom(key) {
			// Already properly classified, keep as is
			newNetworks[key] = network
			continue
		}

		// Network needs migration - classify it
		classification, err := nm.classificationService.ClassifyNetwork(int(network.ChainID), network.Name, network.RPCEndpoint)
		if err != nil {
			// If classification fails, treat as custom with original key
			newKey := fmt.Sprintf("custom_%s", key)
			newNetworks[newKey] = network
			migrationNeeded = true
			continue
		}

		// Use the new classified key
		newNetworks[classification.Key] = network
		migrationNeeded = true
	}

	// Only save if migration was needed
	if migrationNeeded {
		cfg.Networks = newNetworks
		if err := nm.configManager.SaveConfiguration(cfg); err != nil {
			return fmt.Errorf("failed to save migrated configuration: %w", err)
		}
	}

	return nil
}

// ValidateNetwork validates a network configuration
func (nm *NetworkManager) ValidateNetwork(network config.Network) error {
	if network.Name == "" {
		return fmt.Errorf("network name cannot be empty")
	}

	if network.ChainID <= 0 {
		return fmt.Errorf("chain ID must be positive")
	}

	if network.RPCEndpoint == "" {
		return fmt.Errorf("RPC endpoint cannot be empty")
	}

	if network.Symbol == "" {
		return fmt.Errorf("symbol cannot be empty")
	}

	// Validate RPC endpoint accessibility
	if err := nm.chainListService.ValidateRPCEndpoint(network.RPCEndpoint); err != nil {
		return fmt.Errorf("RPC endpoint validation failed: %w", err)
	}

	// Verify chain ID matches the RPC endpoint
	actualChainID, err := nm.chainListService.GetChainIDFromRPC(network.RPCEndpoint)
	if err != nil {
		return fmt.Errorf("failed to verify chain ID from RPC: %w", err)
	}

	if int64(actualChainID) != network.ChainID {
		return fmt.Errorf("RPC endpoint chain ID (%d) does not match expected chain ID (%d)", actualChainID, network.ChainID)
	}

	return nil
}
