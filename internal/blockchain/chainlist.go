package blockchain

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"

	"blocowallet/internal/terminal"
)

// ErrChainlistUnavailable is returned when ChainList API cannot be reached or responds with an error
var ErrChainlistUnavailable = errors.New("chainlist unavailable")

// RPCEndpoint represents an RPC endpoint from ChainList API
type RPCEndpoint struct {
	URL          string `json:"url"`
	Tracking     string `json:"tracking"`
	IsOpenSource bool   `json:"isOpenSource"`
}

type Explorer struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

func (explorer *Explorer) UnmarshalJSON(data []byte) error {
	if explorer == nil || len(data) == 0 || len(data) > 8192 {
		return fmt.Errorf("invalid ChainList explorer")
	}
	trimmed := strings.TrimSpace(string(data))
	if strings.HasPrefix(trimmed, "\"") {
		var endpoint string
		if err := json.Unmarshal(data, &endpoint); err != nil {
			return err
		}
		explorer.Name = ""
		explorer.URL = endpoint
		return nil
	}
	type explorerAlias Explorer
	var decoded explorerAlias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*explorer = Explorer(decoded)
	return nil
}

// ChainInfo represents chain information from ChainList API
type ChainInfo struct {
	ChainID        int    `json:"chainId"`
	Name           string `json:"name"`
	NativeCurrency struct {
		Name     string `json:"name"`
		Symbol   string `json:"symbol"`
		Decimals int    `json:"decimals"`
	} `json:"nativeCurrency"`
	RPC       []RPCEndpoint `json:"rpc"`
	Explorers []Explorer    `json:"explorers"`
}

func validateChainCatalog(chains []ChainInfo) error {
	if len(chains) > 4096 {
		return fmt.Errorf("ChainList catalog exceeds chain budget")
	}
	chainIDs := make(map[int]struct{}, len(chains))
	for _, chain := range chains {
		if chain.ChainID <= 0 || chain.NativeCurrency.Decimals < 0 || chain.NativeCurrency.Decimals > 36 || len(chain.RPC) > 256 || len(chain.Explorers) > 16 {
			return fmt.Errorf("ChainList entry exceeds shape policy")
		}
		if _, exists := chainIDs[chain.ChainID]; exists {
			return fmt.Errorf("ChainList contains a duplicate chain ID")
		}
		chainIDs[chain.ChainID] = struct{}{}
		endpoints := make(map[string]struct{}, len(chain.RPC))
		for _, endpoint := range chain.RPC {
			tracking := strings.ToLower(strings.TrimSpace(endpoint.Tracking))
			if len(endpoint.URL) == 0 || len(endpoint.URL) > 4096 || (tracking != "" && tracking != "none" && tracking != "limited" && tracking != "yes" && tracking != "unspecified") {
				return fmt.Errorf("ChainList RPC metadata exceeds policy")
			}
			if _, exists := endpoints[endpoint.URL]; exists {
				return fmt.Errorf("ChainList contains a duplicate RPC endpoint")
			}
			endpoints[endpoint.URL] = struct{}{}
		}
		for _, explorer := range chain.Explorers {
			parsedExplorer, err := url.ParseRequestURI(explorer.URL)
			if len(explorer.URL) == 0 || len(explorer.URL) > 4096 || strings.ContainsAny(explorer.URL, "\x00\r\n\x1b") || err != nil || parsedExplorer.Scheme != "https" || parsedExplorer.Host == "" || parsedExplorer.User != nil || parsedExplorer.Fragment != "" {
				return fmt.Errorf("ChainList explorer metadata exceeds policy")
			}
		}
	}
	return nil
}

func sanitizeChainInfo(chain *ChainInfo) {
	chain.Name = terminal.SanitizeInline(chain.Name, 128)
	chain.NativeCurrency.Name = terminal.SanitizeInline(chain.NativeCurrency.Name, 64)
	chain.NativeCurrency.Symbol = terminal.SanitizeInline(chain.NativeCurrency.Symbol, 16)
	for index := range chain.RPC {
		chain.RPC[index].Tracking = strings.ToLower(strings.TrimSpace(chain.RPC[index].Tracking))
		if chain.RPC[index].Tracking == "" || chain.RPC[index].Tracking == "unspecified" {
			chain.RPC[index].Tracking = "unknown"
		}
	}
	for index := range chain.Explorers {
		chain.Explorers[index].Name = terminal.SanitizeInline(chain.Explorers[index].Name, 64)
	}
}

// ChainListService handles interaction with ChainList API
type ChainListService struct {
	gateway     *RPCGateway
	baseURL     string
	chains      []ChainInfo
	cacheMu     sync.RWMutex
	cacheExpiry time.Time
}

// NetworkSuggestion represents a search suggestion for a network
type NetworkSuggestion struct {
	ChainID int    `json:"chainId"`
	Name    string `json:"name"`
	Symbol  string `json:"symbol"`
}

// RPCConnectionResult represents the result of testing an RPC connection
type RPCConnectionResult struct {
	URL     string
	Success bool
	Error   error
	ChainID int
	Latency time.Duration
}

func NewChainListServiceWithGateway(gateway *RPCGateway, baseURL string) *ChainListService {
	return &ChainListService{
		gateway: gateway,
		baseURL: strings.TrimRight(baseURL, "/"),
		chains:  make([]ChainInfo, 0),
	}
}

// GetChainInfo fetches chain information by chain ID
func (s *ChainListService) GetChainInfo(chainID int) (*ChainInfo, error) {
	return s.GetChainInfoContext(context.Background(), chainID)
}

func (s *ChainListService) GetChainInfoContext(ctx context.Context, chainID int) (*ChainInfo, error) {
	if err := s.loadChains(ctx); err != nil {
		return nil, err
	}
	s.cacheMu.RLock()
	defer s.cacheMu.RUnlock()
	for index := range s.chains {
		if s.chains[index].ChainID == chainID {
			chain := s.chains[index]
			return &chain, nil
		}
	}
	return nil, fmt.Errorf("chain with ID %d not found", chainID)
}

// ValidateRPCEndpoint checks if an RPC endpoint is accessible
func (s *ChainListService) ValidateRPCEndpoint(rpcURL string) error {
	return s.ValidateRPCEndpointContext(context.Background(), rpcURL)
}

func (s *ChainListService) ValidateRPCEndpointContext(ctx context.Context, rpcURL string) error {
	if rpcURL == "" {
		return NewNetworkOperationError("validate", "RPC URL cannot be empty", nil)
	}
	if _, err := s.gateway.ChainID(ctx, rpcURL); err != nil {
		return NewNetworkOperationError("validate", "RPC endpoint validation failed", err)
	}
	return nil
}

// GetChainIDFromRPC attempts to get chain ID from RPC endpoint
func (s *ChainListService) GetChainIDFromRPC(rpcURL string) (int, error) {
	return s.GetChainIDFromRPCContext(context.Background(), rpcURL)
}

func (s *ChainListService) GetChainIDFromRPCContext(ctx context.Context, rpcURL string) (int, error) {
	chainID, err := s.gateway.ChainID(ctx, rpcURL)
	if err != nil {
		return 0, NewNetworkOperationError("validate", "failed to validate RPC chain identity", err)
	}
	return int(chainID), nil
}

// loadChains loads and caches chain data from ChainList API with simple retry/backoff
func (s *ChainListService) loadChains(ctx context.Context) error {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()

	// Check if cache is still valid (24 hours)
	if time.Now().Before(s.cacheExpiry) && len(s.chains) > 0 {
		return nil
	}

	registryURL := fmt.Sprintf("%s/rpcs.json", s.baseURL)

	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		bodyBytes, err := s.gateway.Fetch(ctx, registryURL)
		if err != nil {
			lastErr = err
			if attempt < 2 && isTransientNetworkError(err) {
				if err := waitForRetry(ctx, time.Duration(300*(1<<attempt))*time.Millisecond); err != nil {
					return err
				}
				continue
			}
			return NewNetworkOperationError("search", "failed to fetch chain list", fmt.Errorf("%w: %w", ErrChainlistUnavailable, err))
		}

		var chains []ChainInfo
		if err := json.Unmarshal(bodyBytes, &chains); err != nil {
			return NewNetworkOperationError("search", "failed to parse ChainList response", fmt.Errorf("%w: %w", ErrChainlistUnavailable, err))
		}
		if err := validateChainCatalog(chains); err != nil {
			return NewNetworkOperationError("search", "invalid ChainList response", err)
		}
		for index := range chains {
			sanitizeChainInfo(&chains[index])
		}

		// Success: cache and return
		s.chains = chains
		s.cacheExpiry = time.Now().Add(24 * time.Hour)
		return nil
	}

	// Exhausted attempts
	return NewNetworkOperationError("search", "unable to fetch network data from ChainList", lastErr)
}

func waitForRetry(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// isTransientNetworkError determines whether an error is likely transient
func isTransientNetworkError(err error) bool {
	if err == nil {
		return false
	}
	var ne net.Error
	if errors.As(err, &ne) {
		if ne.Timeout() {
			return true
		}
		// Temporary() is deprecated in Go 1.20+, but many implementations still provide it
		type temporary interface{ Temporary() bool }
		if t, ok := any(ne).(temporary); ok && t.Temporary() {
			return true
		}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	// Fallback heuristic by message substring
	s := strings.ToLower(err.Error())
	if strings.Contains(s, "timeout") || strings.Contains(s, "temporar") || strings.Contains(s, "connection reset") || strings.Contains(s, "connection refused") || strings.Contains(s, "econnreset") {
		return true
	}
	return false
}

// SearchNetworksByName searches for networks by name with fuzzy matching
func (s *ChainListService) SearchNetworksByName(query string) ([]NetworkSuggestion, error) {
	return s.SearchNetworksByNameContext(context.Background(), query)
}

func (s *ChainListService) SearchNetworksByNameContext(ctx context.Context, query string) ([]NetworkSuggestion, error) {
	if err := s.loadChains(ctx); err != nil {
		return nil, err
	}

	s.cacheMu.RLock()
	defer s.cacheMu.RUnlock()

	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return []NetworkSuggestion{}, nil
	}

	var suggestions []NetworkSuggestion
	for _, chain := range s.chains {
		name := strings.ToLower(chain.Name)

		// Exact match gets priority
		if name == query {
			suggestions = append([]NetworkSuggestion{{
				ChainID: chain.ChainID,
				Name:    chain.Name,
				Symbol:  chain.NativeCurrency.Symbol,
			}}, suggestions...)
			continue
		}

		// Contains match
		if strings.Contains(name, query) {
			suggestions = append(suggestions, NetworkSuggestion{
				ChainID: chain.ChainID,
				Name:    chain.Name,
				Symbol:  chain.NativeCurrency.Symbol,
			})
		}
	}

	// Limit results to avoid overwhelming the UI
	if len(suggestions) > 10 {
		suggestions = suggestions[:10]
	}

	return suggestions, nil
}

// GetChainInfoWithRetry gets chain info and tests RPC endpoints with retry logic
func (s *ChainListService) GetChainInfoWithRetry(chainID int) (*ChainInfo, string, error) {
	return s.GetChainInfoWithRetryContext(context.Background(), chainID)
}

func (s *ChainListService) GetChainInfoWithRetryContext(ctx context.Context, chainID int) (*ChainInfo, string, error) {
	// Debug log removed

	if err := s.loadChains(ctx); err != nil {
		// Debug log removed
		return nil, "", err
	}

	s.cacheMu.RLock()
	chains := s.chains
	s.cacheMu.RUnlock()

	// Debug log removed

	// Find chain by ID
	var targetChain *ChainInfo
	for _, chain := range chains {
		if chain.ChainID == chainID {
			targetChain = &chain
			break
		}
	}

	if targetChain == nil {
		return nil, "", fmt.Errorf("chain with ID %d not found", chainID)
	}

	// Test RPC endpoints and find the best one
	workingRPC, err := s.findBestRPCEndpoint(ctx, targetChain.RPC, chainID)
	if err != nil {
		return nil, "", fmt.Errorf("no working RPC endpoint found: %w", err)
	}

	return targetChain, workingRPC, nil
}

// findBestRPCEndpoint tests all RPC endpoints and returns the fastest working one
func (s *ChainListService) findBestRPCEndpoint(ctx context.Context, endpoints []RPCEndpoint, expectedChainID int) (string, error) {
	if len(endpoints) == 0 {
		return "", fmt.Errorf("no RPC endpoints available")
	}
	if len(endpoints) > 256 {
		return "", fmt.Errorf("RPC endpoint catalog exceeds policy")
	}
	ordered := make([]RPCEndpoint, 0, min(32, len(endpoints)))
	for _, endpoint := range endpoints {
		if strings.EqualFold(endpoint.Tracking, "none") {
			ordered = append(ordered, endpoint)
			if len(ordered) == 32 {
				break
			}
		}
	}
	if len(ordered) == 0 {
		return "", fmt.Errorf("no privacy-preserving RPC endpoints are available")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	jobs := make(chan RPCEndpoint)
	results := make(chan RPCConnectionResult, len(ordered))
	workers := min(8, len(ordered))
	var waitGroup sync.WaitGroup
	waitGroup.Add(workers)
	for range workers {
		go func() {
			defer waitGroup.Done()
			for endpoint := range jobs {
				start := time.Now()
				chainID, err := s.testRPCEndpoint(ctx, endpoint.URL, expectedChainID)
				result := RPCConnectionResult{URL: endpoint.URL, ChainID: chainID, Latency: time.Since(start), Success: err == nil, Error: err}
				select {
				case results <- result:
				case <-ctx.Done():
					return
				}
			}
		}()
	}
	go func() {
		defer close(jobs)
		for _, endpoint := range ordered {
			select {
			case jobs <- endpoint:
			case <-ctx.Done():
				return
			}
		}
	}()
	go func() {
		waitGroup.Wait()
		close(results)
	}()
	var bestResult *RPCConnectionResult
	for result := range results {
		if result.Success && (bestResult == nil || result.Latency < bestResult.Latency) {
			candidate := result
			bestResult = &candidate
		}
	}
	if bestResult == nil {
		return "", fmt.Errorf("no working RPC endpoints found for chain ID %d", expectedChainID)
	}
	return bestResult.URL, nil
}

// testRPCEndpoint tests a single RPC endpoint
func (s *ChainListService) testRPCEndpoint(ctx context.Context, rpcURL string, expectedChainID int) (int, error) {
	session, err := s.gateway.ValidateChain(ctx, rpcURL, int64(expectedChainID))
	if err != nil {
		return 0, err
	}
	return int(session.ChainID()), nil
}
