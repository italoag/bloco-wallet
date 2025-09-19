package blockchain

import (
	"context"
	"fmt"
	"math/big"
	"sync"
	"time"

	"blocowallet/pkg/config"
)

// MultiProvider manages multiple balance providers for different networks
type MultiProvider struct {
	providers map[string]Provider
	networks  map[string]config.Network
	mu        sync.RWMutex
}

// Provider represents a blockchain provider with network information
type Provider struct {
	balanceProvider BalanceProvider
	network         config.Network
}

// BalanceProvider is interface implemented by Ethereum, Mock, etc.
type BalanceProvider interface {
	GetBalance(ctx context.Context, address string) (*big.Int, error)
	GetNetworkSymbol() string
	GetNetworkDecimals() int
}

// NewMultiProvider creates a new MultiProvider
func NewMultiProvider() *MultiProvider {
	return &MultiProvider{
		providers: make(map[string]Provider),
		networks:  make(map[string]config.Network),
	}
}

// AddProvider adds a balance provider for a specific network
func (mp *MultiProvider) AddProvider(key string, provider BalanceProvider, network config.Network) {
	mp.mu.Lock()
	defer mp.mu.Unlock()

	mp.providers[key] = Provider{
		balanceProvider: provider,
		network:         network,
	}
	mp.networks[key] = network
}

// RemoveProvider removes a balance provider for a specific network
func (mp *MultiProvider) RemoveProvider(key string) {
	mp.mu.Lock()
	defer mp.mu.Unlock()

	if provider, exists := mp.providers[key]; exists {
		// If the provider implements Close method, call it
		if closer, ok := provider.balanceProvider.(interface{ Close() }); ok {
			closer.Close()
		}
		delete(mp.providers, key)
		delete(mp.networks, key)
	}
}

// NetworkBalance holds the balance information for a specific network
type NetworkBalance struct {
	NetworkKey  string
	NetworkName string
	Symbol      string
	Decimals    int
	Amount      *big.Int
	Error       error
}

// GetAllBalances gets the balance for a wallet address on all active networks
func (mp *MultiProvider) GetAllBalances(ctx context.Context, address string) []NetworkBalance {
	mp.mu.RLock()
	defer mp.mu.RUnlock()

	results := make([]NetworkBalance, 0, len(mp.providers))

	// For each provider, get the balance
	for key, provider := range mp.providers {
		if !provider.network.IsActive {
			continue
		}

		balance := NetworkBalance{
			NetworkKey:  key,
			NetworkName: provider.network.Name,
			Symbol:      provider.balanceProvider.GetNetworkSymbol(),
			Decimals:    provider.balanceProvider.GetNetworkDecimals(),
		}

		amount, err := provider.balanceProvider.GetBalance(ctx, address)
		if err != nil {
			balance.Error = fmt.Errorf("failed to get balance on %s: %w", provider.network.Name, err)
		} else {
			balance.Amount = amount
		}

		results = append(results, balance)
	}

	return results
}

// RefreshProviders updates the provider list based on current network configuration
func (mp *MultiProvider) RefreshProviders(cfg *config.Config) {
	mp.mu.Lock()
	defer mp.mu.Unlock()

	// Track which networks we still need
	stillNeeded := make(map[string]bool)

	// First, handle default networks
	for key, network := range cfg.Networks {
		stillNeeded[key] = true

		// Skip inactive networks
		if !network.IsActive {
			continue
		}

		// If we already have this provider, continue
		if _, exists := mp.providers[key]; exists {
			continue
		}

		// Create a new provider
		if network.RPCEndpoint != "" {
			provider, err := NewEthereum(
				network.RPCEndpoint,
				DefaultTimeout,
				network.Symbol,
				18, // Most EVM chains use 18 decimals
			)
			if err != nil {
				// If we can't connect, use a mock provider
				mockProvider := NewMock()
				mp.providers[key] = Provider{
					balanceProvider: mockProvider,
					network:         network,
				}
			} else {
				mp.providers[key] = Provider{
					balanceProvider: provider,
					network:         network,
				}
			}
			mp.networks[key] = network
		}
	}

	// Remove providers for networks that no longer exist or are inactive
	for key, provider := range mp.providers {
		if needed, exists := stillNeeded[key]; !exists || !needed {
			// Remove provider if network no longer exists
			if closer, ok := provider.balanceProvider.(interface{ Close() }); ok {
				closer.Close()
			}
			delete(mp.providers, key)
			delete(mp.networks, key)
		} else {
			// Update network status in cached networks
			if network, exists := cfg.Networks[key]; exists {
				mp.networks[key] = network
			} else if network, exists := cfg.Networks[key]; exists {
				mp.networks[key] = network
			}
		}
	}
}

// DefaultTimeout for blockchain connections
const DefaultTimeout = 30 * time.Second

// Close closes all providers
func (mp *MultiProvider) Close() {
	mp.mu.Lock()
	defer mp.mu.Unlock()

	for key, provider := range mp.providers {
		// If the provider implements Close method, call it
		if closer, ok := provider.balanceProvider.(interface{ Close() }); ok {
			closer.Close()
		}
		delete(mp.providers, key)
	}
}
