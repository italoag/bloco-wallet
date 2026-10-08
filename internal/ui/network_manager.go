package ui

import (
	"context"
	"errors"

	"blocowallet/internal/blockchain"
	"blocowallet/pkg/config"
	"blocowallet/pkg/localization"
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

func saveNetworkConfiguration(manager ConfigurationManagerInterface, cfg *config.Config) error {
	err := manager.SaveConfiguration(cfg)
	if config.IsConfigCommitted(err) {
		return nil
	}
	return err
}

// AddNetwork adds a new network with automatic classification
func (nm *NetworkManager) AddNetwork(network config.Network) error {
	_, err := nm.AddNetworkWithClassification(network)
	return err
}

func (nm *NetworkManager) AddNetworkWithClassification(network config.Network) (*blockchain.NetworkClassification, error) {
	return nm.AddNetworkWithClassificationContext(context.Background(), network)
}

func (nm *NetworkManager) AddNetworkWithClassificationContext(ctx context.Context, network config.Network) (*blockchain.NetworkClassification, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Load current configuration
	cfg, err := nm.configManager.LoadConfiguration()
	if err != nil {
		return nil, localization.WrapError("net_err_load_config", err)
	}

	// Initialize Networks map if it's nil
	if cfg.Networks == nil {
		cfg.Networks = make(map[string]config.Network)
	}

	resolvedEndpoint, err := network.ResolveRPCEndpoint(config.EnvironmentCredentialProvider{})
	if err != nil {
		return nil, localization.WrapError("net_err_resolve_rpc", err)
	}
	// Classify the network
	classification, err := nm.classificationService.ClassifyNetworkContext(ctx, int(network.ChainID), network.Name, resolvedEndpoint)
	if err != nil {
		return nil, localization.WrapError("net_err_classify", err)
	}

	// If the network is standard and we have chain info, enhance the network data
	if classification.Type == blockchain.NetworkTypeStandard && classification.ChainInfo != nil {
		// Use chainlist data to fill in missing information
		network.Symbol = classification.ChainInfo.NativeCurrency.Symbol
		network.NativeDecimals = classification.ChainInfo.NativeCurrency.Decimals
		network.NativeDecimalsSet = true
		if network.Explorer == "" && len(classification.ChainInfo.Explorers) > 0 {
			network.Explorer = classification.ChainInfo.Explorers[0].URL
		}
	}

	network.RegistryListed = classification.Type == blockchain.NetworkTypeStandard
	network.IdentityValidated = classification.IsValidated
	if classification.ChainInfo != nil {
		for _, endpoint := range classification.ChainInfo.RPC {
			if endpoint.URL == resolvedEndpoint {
				network.Tracking = endpoint.Tracking
				break
			}
		}
	}

	// Check if network already exists
	if _, exists := cfg.Networks[classification.Key]; exists {
		return nil, errors.New(localization.T("net_err_already_exists", map[string]interface{}{"Key": classification.Key}))
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Add the network to the configuration
	cfg.Networks[classification.Key] = network

	// Save the configuration
	if err := saveNetworkConfiguration(nm.configManager, cfg); err != nil {
		return nil, localization.WrapError("net_err_save_config", err)
	}

	return classification, nil
}

// UpdateNetwork updates an existing network
func (nm *NetworkManager) UpdateNetwork(key string, network config.Network) error {
	return nm.UpdateNetworkContext(context.Background(), key, network)
}

func (nm *NetworkManager) UpdateNetworkContext(ctx context.Context, key string, network config.Network) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	// Load current configuration
	cfg, err := nm.configManager.LoadConfiguration()
	if err != nil {
		return localization.WrapError("net_err_load_config", err)
	}

	// Initialize Networks map if it's nil
	if cfg.Networks == nil {
		cfg.Networks = make(map[string]config.Network)
	}

	// Check if network exists
	if _, exists := cfg.Networks[key]; !exists {
		return errors.New(localization.T("net_err_key_not_found", map[string]interface{}{"Key": key}))
	}

	resolvedEndpoint, err := network.ResolveRPCEndpoint(config.EnvironmentCredentialProvider{})
	if err != nil {
		return localization.WrapError("net_err_resolve_rpc", err)
	}
	// Classify the updated network to determine if the key should change
	classification, err := nm.classificationService.ClassifyNetworkContext(ctx, int(network.ChainID), network.Name, resolvedEndpoint)
	if err != nil {
		return localization.WrapError("net_err_classify_updated", err)
	}

	network.RegistryListed = classification.Type == blockchain.NetworkTypeStandard
	network.IdentityValidated = classification.IsValidated
	if classification.ChainInfo != nil {
		network.Symbol = classification.ChainInfo.NativeCurrency.Symbol
		network.NativeDecimals = classification.ChainInfo.NativeCurrency.Decimals
		network.NativeDecimalsSet = true
	}
	network.Tracking = ""
	if classification.ChainInfo != nil {
		for _, endpoint := range classification.ChainInfo.RPC {
			if endpoint.URL == resolvedEndpoint {
				network.Tracking = endpoint.Tracking
				break
			}
		}
	}

	if err := ctx.Err(); err != nil {
		return err
	}
	// If the classification results in a different key, we need to handle the migration
	if classification.Key != key {
		// Remove the old entry
		delete(cfg.Networks, key)

		// Check if the new key already exists
		if _, exists := cfg.Networks[classification.Key]; exists {
			return errors.New(localization.T("net_err_update_exists", map[string]interface{}{"Key": classification.Key}))
		}

		// Add with the new key
		cfg.Networks[classification.Key] = network
	} else {
		// Update in place
		cfg.Networks[key] = network
	}

	// Save the configuration
	if err := saveNetworkConfiguration(nm.configManager, cfg); err != nil {
		return localization.WrapError("net_err_save_config", err)
	}

	return nil
}

// RemoveNetwork removes a network from the configuration
func (nm *NetworkManager) RemoveNetwork(key string) error {
	// Load current configuration
	cfg, err := nm.configManager.LoadConfiguration()
	if err != nil {
		return localization.WrapError("net_err_load_config", err)
	}

	// Initialize Networks map if it's nil
	if cfg.Networks == nil {
		cfg.Networks = make(map[string]config.Network)
	}

	// Check if network exists
	if _, exists := cfg.Networks[key]; !exists {
		return errors.New(localization.T("net_err_key_not_found", map[string]interface{}{"Key": key}))
	}

	// Remove the network
	delete(cfg.Networks, key)

	// Save the configuration
	if err := saveNetworkConfiguration(nm.configManager, cfg); err != nil {
		return localization.WrapError("net_err_save_config", err)
	}

	return nil
}

// LoadNetworks loads all networks from the Viper configuration
func (nm *NetworkManager) LoadNetworks() (map[string]config.Network, error) {
	// Load configuration using ConfigurationManager
	cfg, err := nm.configManager.LoadConfiguration()
	if err != nil {
		return nil, localization.WrapError("net_err_load_config", err)
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
		return nil, localization.WrapError("net_err_load_networks", err)
	}

	network, exists := networks[key]
	if !exists {
		return nil, errors.New(localization.T("net_err_key_not_found", map[string]interface{}{"Key": key}))
	}

	return &network, nil
}

// ListNetworks returns all networks with their classification information
func (nm *NetworkManager) ListNetworks() (map[string]NetworkInfo, error) {
	networks, err := nm.LoadNetworks()
	if err != nil {
		return nil, localization.WrapError("net_err_load_networks", err)
	}

	result := make(map[string]NetworkInfo)

	for key, network := range networks {
		nType := blockchain.NetworkTypeCustom
		source := "manual"
		if network.RegistryListed {
			nType = blockchain.NetworkTypeStandard
			source = "stored_registry_claim"
		}
		result[key] = NetworkInfo{
			Network:             network,
			Type:                nType,
			IsValidated:         false,
			PreviouslyValidated: network.IdentityValidated,
			// Presentation-independent codes/raw values; resolved at render.
			CurrentHealth:    "unchecked",
			PrivacyTracking:  network.Tracking,
			QuorumConfidence: "single_provider",
			Source:           source,
			ChainInfo:        nil,
		}
	}

	return result, nil
}

// NetworkInfo contains network information with classification details
type NetworkInfo struct {
	Network             config.Network         `json:"network"`
	Type                blockchain.NetworkType `json:"type"`
	IsValidated         bool                   `json:"is_validated"`
	PreviouslyValidated bool                   `json:"previously_validated"`
	CurrentHealth       string                 `json:"current_health"`
	PrivacyTracking     string                 `json:"privacy_tracking"`
	QuorumConfidence    string                 `json:"quorum_confidence"`
	Source              string                 `json:"source"`
	ChainInfo           *blockchain.ChainInfo  `json:"chain_info,omitempty"`
}

// MigrateExistingNetworks migrates existing networks to the new classification system
func (nm *NetworkManager) MigrateExistingNetworks() error {
	// Load current configuration
	cfg, err := nm.configManager.LoadConfiguration()
	if err != nil {
		return localization.WrapError("net_err_load_config", err)
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
		if err := saveNetworkConfiguration(nm.configManager, cfg); err != nil {
			return localization.WrapError("net_err_save_migrated", err)
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

	resolvedEndpoint, err := network.ResolveRPCEndpoint(config.EnvironmentCredentialProvider{})
	if err != nil {
		return err
	}

	if network.Symbol == "" {
		return fmt.Errorf("symbol cannot be empty")
	}

	// Validate RPC endpoint accessibility
	if err := nm.chainListService.ValidateRPCEndpoint(resolvedEndpoint); err != nil {
		return fmt.Errorf("RPC endpoint validation failed: %w", err)
	}

	// Verify chain ID matches the RPC endpoint
	actualChainID, err := nm.chainListService.GetChainIDFromRPC(resolvedEndpoint)
	if err != nil {
		return fmt.Errorf("failed to verify chain ID from RPC: %w", err)
	}

	if int64(actualChainID) != network.ChainID {
		return fmt.Errorf("RPC endpoint chain ID (%d) does not match expected chain ID (%d)", actualChainID, network.ChainID)
	}

	return nil
}
