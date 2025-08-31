package blockchain

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// NetworkType represents the type of network
type NetworkType string

const (
	// NetworkTypeStandard represents networks that are found in ChainList
	NetworkTypeStandard NetworkType = "standard"
	// NetworkTypeCustom represents networks that are not found in ChainList or manually added
	NetworkTypeCustom NetworkType = "custom"
)

// NetworkClassification contains the classification result for a network
type NetworkClassification struct {
	Type        NetworkType
	IsValidated bool
	ChainInfo   *ChainInfo
	Key         string
	Source      string // "chainlist" or "manual"
}

// ChainListServiceInterface defines the interface for ChainList operations
type ChainListServiceInterface interface {
	GetChainInfo(chainID int) (*ChainInfo, error)
	GetChainInfoWithRetry(chainID int) (*ChainInfo, string, error)
	ValidateRPCEndpoint(rpcURL string) error
	GetChainIDFromRPC(rpcURL string) (int, error)
}

// NetworkClassificationService handles network classification and validation
type NetworkClassificationService struct {
	chainListService ChainListServiceInterface
}

// NewNetworkClassificationService creates a new NetworkClassificationService
func NewNetworkClassificationService(chainListService ChainListServiceInterface) *NetworkClassificationService {
	return &NetworkClassificationService{
		chainListService: chainListService,
	}
}

// ClassifyNetwork determines if a network is standard (from chainlist) or custom
func (ncs *NetworkClassificationService) ClassifyNetwork(chainID int, name string, rpcEndpoint string) (*NetworkClassification, error) {
	// First, try to validate against ChainList
	chainInfo, _, err := ncs.ValidateNetworkAgainstChainList(chainID, rpcEndpoint)
	if err == nil && chainInfo != nil {
		// Network found in ChainList - classify as standard
		key := ncs.GenerateNetworkKey(NetworkTypeStandard, chainInfo.Name, chainID)
		return &NetworkClassification{
			Type:        NetworkTypeStandard,
			IsValidated: true,
			ChainInfo:   chainInfo,
			Key:         key,
			Source:      "chainlist",
		}, nil
	}

	// Network not found in ChainList or validation failed - classify as custom
	source := "manual"
	if err != nil {
		// Distinguish offline/unavailable chainlist scenario
		if errors.Is(err, ErrChainlistUnavailable) || strings.Contains(err.Error(), "chainlist service not available") {
			source = "manual_offline"
		}
	}
	key := ncs.GenerateNetworkKey(NetworkTypeCustom, name, chainID)
	return &NetworkClassification{
		Type:        NetworkTypeCustom,
		IsValidated: false,
		ChainInfo:   nil,
		Key:         key,
		Source:      source,
	}, nil
}

// ValidateNetworkAgainstChainList validates a network against the ChainList API
func (ncs *NetworkClassificationService) ValidateNetworkAgainstChainList(chainID int, rpcEndpoint string) (*ChainInfo, string, error) {
	if ncs.chainListService == nil {
		return nil, "", fmt.Errorf("chainlist service not available")
	}

	// Optimization: avoid expensive RPC probing when possible
	var (
		chainInfo  *ChainInfo
		workingRPC string
		err        error
	)

	if rpcEndpoint != "" {
		// Fast path: we have a user-provided endpoint; just fetch chain metadata and validate the endpoint
		chainInfo, err = ncs.chainListService.GetChainInfo(chainID)
		if err != nil {
			return nil, "", fmt.Errorf("failed to get chain info from chainlist: %w", err)
		}

		// Check if the provided RPC endpoint matches any of the known endpoints for this chain
		isKnownEndpoint := false
		for _, endpoint := range chainInfo.RPC {
			if strings.EqualFold(endpoint.URL, rpcEndpoint) {
				isKnownEndpoint = true
				workingRPC = endpoint.URL
				break
			}
		}

		// If the provided endpoint is not in the known list, test it and ensure it matches the chain ID
		if !isKnownEndpoint {
			if err := ncs.chainListService.ValidateRPCEndpoint(rpcEndpoint); err != nil {
				return nil, "", fmt.Errorf("provided RPC endpoint is not valid: %w", err)
			}
			actualChainID, err := ncs.chainListService.GetChainIDFromRPC(rpcEndpoint)
			if err != nil {
				return nil, "", fmt.Errorf("failed to verify chain ID from RPC: %w", err)
			}
			if actualChainID != chainID {
				return nil, "", fmt.Errorf("RPC endpoint chain ID (%d) does not match expected chain ID (%d)", actualChainID, chainID)
			}
			workingRPC = rpcEndpoint
		}
	} else {
		// No RPC provided: fetch metadata without probing endpoints and pick the first available RPC if any
		chainInfo, err = ncs.chainListService.GetChainInfo(chainID)
		if err != nil {
			return nil, "", fmt.Errorf("failed to get chain info from chainlist: %w", err)
		}
		if len(chainInfo.RPC) > 0 {
			workingRPC = chainInfo.RPC[0].URL
		}
	}

	return chainInfo, workingRPC, nil
}

// GenerateNetworkKey generates a network key with appropriate prefixes based on classification
func (ncs *NetworkClassificationService) GenerateNetworkKey(networkType NetworkType, name string, chainID int) string {
	// Sanitize the name for use in a key
	sanitizedName := ncs.sanitizeNetworkName(name)

	switch networkType {
	case NetworkTypeStandard:
		// For standard networks, use the format: standard_<sanitized_name>_<chain_id>
		return fmt.Sprintf("standard_%s_%d", sanitizedName, chainID)
	case NetworkTypeCustom:
		// For custom networks, use the format: custom_<sanitized_name>_<chain_id>
		return fmt.Sprintf("custom_%s_%d", sanitizedName, chainID)
	default:
		// Fallback to custom format
		return fmt.Sprintf("custom_%s_%d", sanitizedName, chainID)
	}
}

// sanitizeNetworkName sanitizes a network name for use in configuration keys
func (ncs *NetworkClassificationService) sanitizeNetworkName(name string) string {
	// Convert to lowercase
	sanitized := strings.ToLower(name)

	// Replace spaces and special characters with underscores
	reg := regexp.MustCompile(`[^a-z0-9]+`)
	sanitized = reg.ReplaceAllString(sanitized, "_")

	// Remove leading and trailing underscores
	sanitized = strings.Trim(sanitized, "_")

	// Ensure the name is not empty
	if sanitized == "" {
		sanitized = "unknown"
	}

	// Limit length to avoid overly long keys
	if len(sanitized) > 50 {
		sanitized = sanitized[:50]
	}

	return sanitized
}

// IsNetworkStandard checks if a network key indicates a standard network
func (ncs *NetworkClassificationService) IsNetworkStandard(key string) bool {
	return strings.HasPrefix(key, "standard_")
}

// IsNetworkCustom checks if a network key indicates a custom network
func (ncs *NetworkClassificationService) IsNetworkCustom(key string) bool {
	return strings.HasPrefix(key, "custom_")
}

// GetNetworkTypeFromKey extracts the network type from a network key
func (ncs *NetworkClassificationService) GetNetworkTypeFromKey(key string) NetworkType {
	if ncs.IsNetworkStandard(key) {
		return NetworkTypeStandard
	}
	if ncs.IsNetworkCustom(key) {
		return NetworkTypeCustom
	}
	// Default to custom for unknown formats (backward compatibility)
	return NetworkTypeCustom
}

// ClassifyExistingNetwork classifies an existing network configuration
// This is useful for migrating existing configurations that don't have type information
func (ncs *NetworkClassificationService) ClassifyExistingNetwork(key string, chainID int, name string, rpcEndpoint string) (*NetworkClassification, error) {
	// If the key already has a type prefix, use it
	if ncs.IsNetworkStandard(key) || ncs.IsNetworkCustom(key) {
		networkType := ncs.GetNetworkTypeFromKey(key)

		// For standard networks, try to validate against chainlist
		if networkType == NetworkTypeStandard {
			chainInfo, _, err := ncs.ValidateNetworkAgainstChainList(chainID, rpcEndpoint)
			if err == nil && chainInfo != nil {
				return &NetworkClassification{
					Type:        NetworkTypeStandard,
					IsValidated: true,
					ChainInfo:   chainInfo,
					Key:         key,
					Source:      "chainlist",
				}, nil
			}
		}

		// For custom networks or failed standard validation, return as custom
		return &NetworkClassification{
			Type:        NetworkTypeCustom,
			IsValidated: false,
			ChainInfo:   nil,
			Key:         key,
			Source:      "manual",
		}, nil
	}

	// Key doesn't have type prefix - classify the network
	classification, err := ncs.ClassifyNetwork(chainID, name, rpcEndpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to classify existing network: %w", err)
	}

	// Keep the original key if it's a custom network to maintain backward compatibility
	if classification.Type == NetworkTypeCustom {
		classification.Key = key
	}

	return classification, nil
}
