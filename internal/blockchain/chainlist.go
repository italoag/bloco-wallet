package blockchain

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// RPCEndpoint represents an RPC endpoint from ChainList API
type RPCEndpoint struct {
	URL          string `json:"url"`
	Tracking     string `json:"tracking"`
	IsOpenSource bool   `json:"isOpenSource"`
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
	Explorers []struct {
		Name string `json:"name"`
		URL  string `json:"url"`
	} `json:"explorers"`
}

// ChainListService handles interaction with ChainList API
type ChainListService struct {
	client      *http.Client
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

// NewChainListService creates a new ChainList service
func NewChainListService() *ChainListService {
	return &ChainListService{
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
		baseURL: "https://chainlist.org",
		chains:  make([]ChainInfo, 0),
	}
}

// GetChainInfo fetches chain information by chain ID
func (s *ChainListService) GetChainInfo(chainID int) (*ChainInfo, error) {
	url := fmt.Sprintf("%s/rpcs.json", s.baseURL)

	resp, err := s.client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch chain list: %w", err)
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			fmt.Printf("Error closing response body: %v\n", err)
		}
	}(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API request failed with status: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var chains []ChainInfo
	if err := json.Unmarshal(body, &chains); err != nil {
		return nil, fmt.Errorf("failed to parse JSON response: %w", err)
	}

	// Find chain by ID
	for _, chain := range chains {
		if chain.ChainID == chainID {
			return &chain, nil
		}
	}

	return nil, fmt.Errorf("chain with ID %d not found", chainID)
}

// ValidateRPCEndpoint checks if an RPC endpoint is accessible
func (s *ChainListService) ValidateRPCEndpoint(rpcURL string) error {
	if rpcURL == "" {
		return fmt.Errorf("RPC URL cannot be empty")
	}

	// Create a simple JSON-RPC request to check if the endpoint is alive
	reqBody := `{"jsonrpc":"2.0","method":"eth_chainId","params":[],"id":1}`

	resp, err := s.client.Post(rpcURL, "application/json",
		strings.NewReader(reqBody))
	if err != nil {
		return fmt.Errorf("RPC endpoint is not accessible: %w", err)
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			fmt.Printf("Error closing response body: %v\n", err)
		}
	}(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("RPC endpoint returned status: %d", resp.StatusCode)
	}

	return nil
}

// GetChainIDFromRPC attempts to get chain ID from RPC endpoint
func (s *ChainListService) GetChainIDFromRPC(rpcURL string) (int, error) {
	reqBody := `{"jsonrpc":"2.0","method":"eth_chainId","params":[],"id":1}`

	resp, err := s.client.Post(rpcURL, "application/json",
		strings.NewReader(reqBody))
	if err != nil {
		return 0, fmt.Errorf("failed to call RPC: %w", err)
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			fmt.Printf("Error closing response body: %v\n", err)
		}
	}(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("RPC call failed with status: %d", resp.StatusCode)
	}

	var result struct {
		Result string `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, fmt.Errorf("failed to decode response: %w", err)
	}

	if result.Error != nil {
		return 0, fmt.Errorf("RPC error: %s", result.Error.Message)
	}

	// Convert hex chain ID to int
	chainID, err := strconv.ParseInt(result.Result, 0, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse chain ID: %w", err)
	}

	return int(chainID), nil
}

// loadChains loads and caches chain data from ChainList API
func (s *ChainListService) loadChains() error {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()

	// Check if cache is still valid (24 hours)
	if time.Now().Before(s.cacheExpiry) && len(s.chains) > 0 {
		// Debug log removed
		return nil
	}

	// Debug log removed
	url := fmt.Sprintf("%s/rpcs.json", s.baseURL)
	resp, err := s.client.Get(url)
	if err != nil {
		// Debug log removed
		return fmt.Errorf("failed to fetch chain list: %w", err)
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			// Debug log removed
			fmt.Printf("Error closing response body: %v\n", err)
		}
	}(resp.Body)

	if resp.StatusCode != http.StatusOK {
		// Debug log removed
		return fmt.Errorf("API request failed with status: %d", resp.StatusCode)
	}

	// Debug log removed
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		// Debug log removed
		return fmt.Errorf("failed to read response body: %w", err)
	}

	// Debug log removed
	var chains []ChainInfo
	if err := json.Unmarshal(body, &chains); err != nil {
		// Debug log removed
		return fmt.Errorf("failed to parse JSON response: %w", err)
	}

	// Debug log removed
	s.chains = chains
	s.cacheExpiry = time.Now().Add(24 * time.Hour)
	return nil
}

// SearchNetworksByName searches for networks by name with fuzzy matching
func (s *ChainListService) SearchNetworksByName(query string) ([]NetworkSuggestion, error) {
	if err := s.loadChains(); err != nil {
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
	// Debug log removed

	if err := s.loadChains(); err != nil {
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
	workingRPC, err := s.findBestRPCEndpoint(targetChain.RPC, chainID)
	if err != nil {
		// Fallback: If we couldn't find a working RPC endpoint, just use the first one in the list
		if len(targetChain.RPC) > 0 {
			fallbackRPC := targetChain.RPC[0].URL
			return targetChain, fallbackRPC, nil
		}

		return nil, "", fmt.Errorf("no working RPC endpoint found: %w", err)
	}

	return targetChain, workingRPC, nil
}

// findBestRPCEndpoint tests all RPC endpoints and returns the fastest working one
func (s *ChainListService) findBestRPCEndpoint(endpoints []RPCEndpoint, expectedChainID int) (string, error) {
	if len(endpoints) == 0 {
		return "", fmt.Errorf("no RPC endpoints available")
	}

	// Channel to collect results
	results := make(chan RPCConnectionResult, len(endpoints))

	// Create a timeout context
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Test all endpoints concurrently
	for i, endpoint := range endpoints {
		go func(idx int, ep RPCEndpoint) {
			result := RPCConnectionResult{URL: ep.URL}
			start := time.Now()

			chainID, err := s.testRPCEndpoint(ep.URL, expectedChainID)
			result.Latency = time.Since(start)

			if err != nil {
				result.Success = false
				result.Error = err
			} else {
				result.Success = true
				result.ChainID = chainID
			}

			select {
			case results <- result:
				// Result sent successfully
			case <-ctx.Done():
				// Context timed out, no need to send result
			}
		}(i, endpoint)
	}

	// Collect results and find the best one
	var bestResult *RPCConnectionResult

	// Use a timeout to avoid waiting forever
	timeout := time.After(10 * time.Second)

	for i := 0; i < len(endpoints); i++ {
		select {
		case result := <-results:
			if result.Success && result.ChainID == expectedChainID {
				// Select the fastest endpoint
				if bestResult == nil || result.Latency < bestResult.Latency {
					bestResult = &result
				}
			}
		case <-timeout:
			i = len(endpoints) // Exit the loop
		}
	}

	if bestResult == nil {
		return "", fmt.Errorf("no working RPC endpoints found for chain ID %d", expectedChainID)
	}

	return bestResult.URL, nil
}

// testRPCEndpoint tests a single RPC endpoint
func (s *ChainListService) testRPCEndpoint(rpcURL string, expectedChainID int) (int, error) {
	if rpcURL == "" || !strings.HasPrefix(rpcURL, "http") {
		return 0, fmt.Errorf("invalid RPC URL")
	}

	// Create a client with shorter timeout for testing
	client := &http.Client{Timeout: 5 * time.Second}

	reqBody := `{"jsonrpc":"2.0","method":"eth_chainId","params":[],"id":1}`

	resp, err := client.Post(rpcURL, "application/json", strings.NewReader(reqBody))
	if err != nil {
		return 0, fmt.Errorf("RPC request failed: %w", err)
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			fmt.Printf("Error closing response body: %v\n", err)
		}
	}(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("RPC returned status: %d", resp.StatusCode)
	}

	var result struct {
		Result string `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, fmt.Errorf("failed to decode response: %w", err)
	}

	if result.Error != nil {
		return 0, fmt.Errorf("RPC error: %s", result.Error.Message)
	}

	// Convert hex chain ID to int
	chainID, err := strconv.ParseInt(result.Result, 0, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse chain ID: %w", err)
	}

	return int(chainID), nil
}
