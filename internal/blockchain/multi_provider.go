package blockchain

import (
	"context"
	"fmt"
	"math/big"
	"sort"
	"strings"
	"sync"
	"time"

	"blocowallet/internal/terminal"
	"blocowallet/pkg/config"
)

const balanceCacheTTL = 30 * time.Second

// MultiProvider manages multiple balance providers for different networks
type MultiProvider struct {
	gateway     *RPCGateway
	credentials config.CredentialProvider
	providers   map[string]Provider
	networks    map[string]config.Network
	cache       map[string]cachedBalance
	mu          sync.RWMutex
	refreshMu   sync.Mutex
}

// Provider represents a blockchain provider with network information
type Provider struct {
	balanceProvider BalanceProvider
	network         config.Network
	endpoint        string
}

// BalanceProvider is interface implemented by Ethereum, Mock, etc.
type BalanceProvider interface {
	GetBalance(ctx context.Context, address string) (*big.Int, error)
	GetNetworkSymbol() string
	GetNetworkDecimals() int
}

type cachedBalance struct {
	amount    *big.Int
	expiresAt time.Time
}

// NewMultiProvider creates a new MultiProvider
func NewMultiProvider(gateway *RPCGateway, credentials config.CredentialProvider) *MultiProvider {
	return &MultiProvider{
		gateway:     gateway,
		credentials: credentials,
		providers:   make(map[string]Provider),
		networks:    make(map[string]config.Network),
		cache:       make(map[string]cachedBalance),
	}
}

// AddProvider adds a balance provider for a specific network
func (mp *MultiProvider) AddProvider(key string, provider BalanceProvider, network config.Network) {
	mp.mu.Lock()
	defer mp.mu.Unlock()
	if existing, exists := mp.providers[key]; exists && existing.balanceProvider != provider {
		if closer, ok := existing.balanceProvider.(interface{ Close() }); ok {
			closer.Close()
		}
	}
	mp.providers[key] = Provider{balanceProvider: provider, network: network, endpoint: network.RPCEndpoint}
	mp.networks[key] = network
	for cacheKey := range mp.cache {
		if strings.HasPrefix(cacheKey, key+":") {
			delete(mp.cache, cacheKey)
		}
	}
}

// RemoveProvider removes a balance provider for a specific network
func (mp *MultiProvider) RemoveProvider(key string) {
	mp.mu.Lock()
	defer mp.mu.Unlock()
	if provider, exists := mp.providers[key]; exists {
		if closer, ok := provider.balanceProvider.(interface{ Close() }); ok {
			closer.Close()
		}
		delete(mp.providers, key)
		delete(mp.networks, key)
	}
	for cacheKey := range mp.cache {
		if len(cacheKey) >= len(key)+1 && cacheKey[:len(key)+1] == key+":" {
			delete(mp.cache, cacheKey)
		}
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
	providers := make(map[string]Provider, len(mp.providers))
	for key, provider := range mp.providers {
		if provider.network.IsActive {
			providers[key] = provider
		}
	}
	mp.mu.RUnlock()
	results := make([]NetworkBalance, 0, len(providers))
	resultChannel := make(chan NetworkBalance, len(providers))
	jobs := make(chan struct {
		key      string
		provider Provider
	})
	workers := min(4, len(providers))
	var waitGroup sync.WaitGroup
	waitGroup.Add(workers)
	for range workers {
		go func() {
			defer waitGroup.Done()
			for job := range jobs {
				resultChannel <- mp.balanceForProvider(ctx, job.key, job.provider, address)
			}
		}()
	}
	go func() {
		defer close(jobs)
		for key, provider := range providers {
			select {
			case jobs <- struct {
				key      string
				provider Provider
			}{key: key, provider: provider}:
			case <-ctx.Done():
				return
			}
		}
	}()
	go func() {
		waitGroup.Wait()
		close(resultChannel)
	}()
	seen := make(map[string]bool, len(providers))
	for result := range resultChannel {
		results = append(results, result)
		seen[result.NetworkKey] = true
	}
	for key, provider := range providers {
		if !seen[key] {
			missingErr := ctx.Err()
			if missingErr == nil {
				missingErr = fmt.Errorf("balance request did not complete")
			}
			results = append(results, NetworkBalance{NetworkKey: key, NetworkName: terminal.SanitizeInline(provider.network.Name, 128), Symbol: terminal.SanitizeInline(provider.network.Symbol, 16), Decimals: provider.network.NativeDecimals, Error: missingErr})
		}
	}
	sort.Slice(results, func(left, right int) bool { return results[left].NetworkKey < results[right].NetworkKey })
	return results
}

func (mp *MultiProvider) balanceForProvider(ctx context.Context, key string, provider Provider, address string) NetworkBalance {
	balance := NetworkBalance{
		NetworkKey:  key,
		NetworkName: terminal.SanitizeInline(provider.network.Name, 128),
		Symbol:      terminal.SanitizeInline(provider.balanceProvider.GetNetworkSymbol(), 16),
		Decimals:    provider.balanceProvider.GetNetworkDecimals(),
	}
	cacheKey := key + ":" + address
	mp.mu.RLock()
	cached, exists := mp.cache[cacheKey]
	mp.mu.RUnlock()
	if exists && time.Now().Before(cached.expiresAt) {
		balance.Amount = new(big.Int).Set(cached.amount)
		return balance
	}
	amount, err := provider.balanceProvider.GetBalance(ctx, address)
	if err != nil {
		balance.Error = fmt.Errorf("balance unavailable on %s: %w", balance.NetworkName, err)
		return balance
	}
	if amount == nil || amount.Sign() < 0 || amount.BitLen() > 256 {
		balance.Error = fmt.Errorf("balance unavailable on %s: invalid provider amount", balance.NetworkName)
		return balance
	}
	balance.Amount = new(big.Int).Set(amount)
	mp.mu.Lock()
	now := time.Now()
	for key, entry := range mp.cache {
		if now.After(entry.expiresAt) {
			delete(mp.cache, key)
		}
	}
	if len(mp.cache) >= 256 {
		for key := range mp.cache {
			delete(mp.cache, key)
			break
		}
	}
	mp.cache[cacheKey] = cachedBalance{amount: new(big.Int).Set(amount), expiresAt: now.Add(balanceCacheTTL)}
	mp.mu.Unlock()
	return balance
}

// RefreshProviders updates the provider list based on current network configuration
func (mp *MultiProvider) RefreshProviders(ctx context.Context, cfg *config.Config) []error {
	mp.refreshMu.Lock()
	defer mp.refreshMu.Unlock()
	if cfg == nil {
		return []error{fmt.Errorf("configuration is required")}
	}
	mp.mu.RLock()
	existing := make(map[string]Provider, len(mp.providers))
	for key, provider := range mp.providers {
		existing[key] = provider
	}
	mp.mu.RUnlock()
	providers := make(map[string]Provider)
	networks := make(map[string]config.Network)
	reused := make(map[string]bool)
	failures := make([]error, 0)
	for key, network := range cfg.Networks {
		if !network.IsActive {
			continue
		}
		if !network.NativeDecimalsSet {
			failures = append(failures, fmt.Errorf("network %s: native currency decimals are unavailable", terminal.SanitizeInline(network.Name, 128)))
			continue
		}
		endpoint, err := network.ResolveRPCEndpoint(mp.credentials)
		if err != nil {
			failures = append(failures, fmt.Errorf("network %s: %w", terminal.SanitizeInline(network.Name, 128), err))
			continue
		}
		if current, exists := existing[key]; exists && current.endpoint == endpoint && current.network.ChainID == network.ChainID && current.network.NativeDecimals == network.NativeDecimals && current.network.Symbol == network.Symbol {
			current.network = network
			providers[key] = current
			networks[key] = network
			reused[key] = true
			continue
		}
		provider, err := NewEthereum(ctx, mp.gateway, endpoint, network.ChainID, DefaultTimeout, network.Symbol, network.NativeDecimals)
		if err != nil {
			failures = append(failures, fmt.Errorf("network %s: %w", terminal.SanitizeInline(network.Name, 128), err))
			continue
		}
		providers[key] = Provider{balanceProvider: provider, network: network, endpoint: endpoint}
		networks[key] = network
	}
	if err := ctx.Err(); err != nil {
		for key, provider := range providers {
			if reused[key] {
				continue
			}
			if closer, ok := provider.balanceProvider.(interface{ Close() }); ok {
				closer.Close()
			}
		}
		return append(failures, err)
	}
	mp.mu.Lock()
	if err := ctx.Err(); err != nil {
		mp.mu.Unlock()
		for key, provider := range providers {
			if !reused[key] {
				if closer, ok := provider.balanceProvider.(interface{ Close() }); ok {
					closer.Close()
				}
			}
		}
		return append(failures, err)
	}
	for key, provider := range mp.providers {
		if reused[key] {
			continue
		}
		if closer, ok := provider.balanceProvider.(interface{ Close() }); ok {
			closer.Close()
		}
	}
	for cacheKey := range mp.cache {
		networkKey, _, _ := strings.Cut(cacheKey, ":")
		if !reused[networkKey] {
			delete(mp.cache, cacheKey)
		}
	}
	mp.providers = providers
	mp.networks = networks
	mp.mu.Unlock()
	return failures
}

// DefaultTimeout for blockchain connections
const DefaultTimeout = 30 * time.Second

// Close closes all providers
func (mp *MultiProvider) Close() {
	mp.mu.Lock()
	defer mp.mu.Unlock()
	for key, provider := range mp.providers {
		if closer, ok := provider.balanceProvider.(interface{ Close() }); ok {
			closer.Close()
		}
		delete(mp.providers, key)
	}
	mp.cache = make(map[string]cachedBalance)
}
