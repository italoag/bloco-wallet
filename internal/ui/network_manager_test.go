package ui

import (
	"blocowallet/internal/blockchain"
	"blocowallet/pkg/config"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// MockConfigurationManager is a mock implementation of ConfigurationManagerInterface for testing
type MockConfigurationManager struct {
	mock.Mock
	config *config.Config
}

func (m *MockConfigurationManager) LoadConfiguration() (*config.Config, error) {
	args := m.Called()
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*config.Config), args.Error(1)
}

func (m *MockConfigurationManager) SaveConfiguration(cfg *config.Config) error {
	args := m.Called(cfg)
	m.config = cfg // Store the saved config for verification
	return args.Error(0)
}

func (m *MockConfigurationManager) GetConfigPath() string {
	args := m.Called()
	return args.String(0)
}

func (m *MockConfigurationManager) GetAppDirectory() string {
	args := m.Called()
	return args.String(0)
}

// MockChainListService is a mock implementation of ChainListServiceInterface for testing
type MockChainListService struct {
	mock.Mock
}

func (m *MockChainListService) GetChainInfo(chainID int) (*blockchain.ChainInfo, error) {
	args := m.Called(chainID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*blockchain.ChainInfo), args.Error(1)
}

func (m *MockChainListService) GetChainInfoWithRetry(chainID int) (*blockchain.ChainInfo, string, error) {
	args := m.Called(chainID)
	if args.Get(0) == nil {
		return nil, args.String(1), args.Error(2)
	}
	return args.Get(0).(*blockchain.ChainInfo), args.String(1), args.Error(2)
}

func (m *MockChainListService) ValidateRPCEndpoint(rpcURL string) error {
	args := m.Called(rpcURL)
	return args.Error(0)
}

func (m *MockChainListService) GetChainIDFromRPC(rpcURL string) (int, error) {
	args := m.Called(rpcURL)
	return args.Int(0), args.Error(1)
}

func TestNewNetworkManager(t *testing.T) {
	mockConfigManager := &MockConfigurationManager{}
	mockChainListService := &MockChainListService{}

	nm := NewNetworkManager(mockConfigManager, mockChainListService)

	assert.NotNil(t, nm)
	assert.Equal(t, mockConfigManager, nm.configManager)
	assert.Equal(t, mockChainListService, nm.chainListService)
	assert.NotNil(t, nm.classificationService)
}

func TestNetworkManager_LoadNetworks(t *testing.T) {
	mockConfigManager := &MockConfigurationManager{}
	mockChainListService := &MockChainListService{}

	// Setup mock expectations with networks
	expectedNetworks := map[string]config.Network{
		"standard_ethereum_mainnet_1": {
			Name:        "Ethereum Mainnet",
			RPCEndpoint: "https://eth.llamarpc.com",
			ChainID:     1,
			Symbol:      "ETH",
			IsActive:    true,
		},
		"custom_my_network_12345": {
			Name:        "My Network",
			RPCEndpoint: "https://my-rpc.example.com",
			ChainID:     12345,
			Symbol:      "MN",
			IsActive:    true,
		},
	}
	cfg := &config.Config{
		Networks: expectedNetworks,
	}
	mockConfigManager.On("LoadConfiguration").Return(cfg, nil)

	nm := NewNetworkManager(mockConfigManager, mockChainListService)

	networks, err := nm.LoadNetworks()

	assert.NoError(t, err)
	assert.Equal(t, expectedNetworks, networks)
	mockConfigManager.AssertExpectations(t)
}

func TestNetworkManager_GetNetwork(t *testing.T) {
	mockConfigManager := &MockConfigurationManager{}
	mockChainListService := &MockChainListService{}

	// Setup mock expectations with networks
	expectedNetwork := config.Network{
		Name:        "Ethereum Mainnet",
		RPCEndpoint: "https://eth.llamarpc.com",
		ChainID:     1,
		Symbol:      "ETH",
		IsActive:    true,
	}
	cfg := &config.Config{
		Networks: map[string]config.Network{
			"standard_ethereum_mainnet_1": expectedNetwork,
		},
	}
	mockConfigManager.On("LoadConfiguration").Return(cfg, nil)

	nm := NewNetworkManager(mockConfigManager, mockChainListService)

	network, err := nm.GetNetwork("standard_ethereum_mainnet_1")

	assert.NoError(t, err)
	assert.NotNil(t, network)
	assert.Equal(t, expectedNetwork.Name, network.Name)
	assert.Equal(t, expectedNetwork.ChainID, network.ChainID)
	mockConfigManager.AssertExpectations(t)
}

func TestNetworkManagerPersistsStandardCredentialReferenceEndToEnd(t *testing.T) {
	t.Setenv("BLOCO_WALLET_APP_APP_DIR", t.TempDir())
	t.Setenv("BLOCO_TEST_STANDARD_RPC", "https://rpc.example.com/v3/super-secret-token")
	manager := config.NewConfigurationManager()
	_, err := manager.LoadConfiguration()
	require.NoError(t, err)
	chainList := &MockChainListService{}
	chainInfo := &blockchain.ChainInfo{
		ChainID: 1,
		Name:    "Ethereum",
		NativeCurrency: struct {
			Name     string `json:"name"`
			Symbol   string `json:"symbol"`
			Decimals int    `json:"decimals"`
		}{Name: "Ether", Symbol: "ETH", Decimals: 18},
		RPC: []blockchain.RPCEndpoint{{URL: "https://rpc.example.com/v3/super-secret-token", Tracking: "none"}},
	}
	chainList.On("GetChainInfo", 1).Return(chainInfo, nil)
	chainList.On("ValidateRPCEndpoint", "https://rpc.example.com/v3/super-secret-token").Return(nil)
	chainList.On("GetChainIDFromRPC", "https://rpc.example.com/v3/super-secret-token").Return(1, nil)
	networkManager := NewNetworkManager(manager, chainList)
	err = networkManager.AddNetwork(config.Network{Name: "Ethereum", ChainID: 1, Symbol: "ETH", RPCEndpointRef: "env:BLOCO_TEST_STANDARD_RPC", IsActive: true})
	require.NoError(t, err)
	reloaded, err := manager.ReloadConfiguration()
	require.NoError(t, err)
	saved := reloaded.Networks["standard_ethereum_1"]
	assert.Empty(t, saved.RPCEndpoint)
	assert.Equal(t, "env:BLOCO_TEST_STANDARD_RPC", saved.RPCEndpointRef)
	data, err := os.ReadFile(manager.GetConfigPath())
	require.NoError(t, err)
	assert.NotContains(t, string(data), "super-secret-token")
}

func TestNetworkManagerPersistsCustomCredentialReferenceEndToEnd(t *testing.T) {
	t.Setenv("BLOCO_WALLET_APP_APP_DIR", t.TempDir())
	t.Setenv("BLOCO_TEST_CUSTOM_RPC", "https://custom.example.com/v2/super-secret-token")
	manager := config.NewConfigurationManager()
	_, err := manager.LoadConfiguration()
	require.NoError(t, err)
	chainList := &MockChainListService{}
	chainList.On("GetChainInfo", 123).Return(nil, assert.AnError)
	chainList.On("ValidateRPCEndpoint", "https://custom.example.com/v2/super-secret-token").Return(nil)
	chainList.On("GetChainIDFromRPC", "https://custom.example.com/v2/super-secret-token").Return(123, nil)
	networkManager := NewNetworkManager(manager, chainList)
	err = networkManager.AddNetwork(config.Network{Name: "Custom", ChainID: 123, Symbol: "CUS", NativeDecimals: 6, NativeDecimalsSet: true, RPCEndpointRef: "env:BLOCO_TEST_CUSTOM_RPC", IsActive: true})
	require.NoError(t, err)
	reloaded, err := manager.ReloadConfiguration()
	require.NoError(t, err)
	saved := reloaded.Networks["custom_custom_123"]
	assert.Empty(t, saved.RPCEndpoint)
	assert.Equal(t, "env:BLOCO_TEST_CUSTOM_RPC", saved.RPCEndpointRef)
	assert.Equal(t, 6, saved.NativeDecimals)
	data, err := os.ReadFile(manager.GetConfigPath())
	require.NoError(t, err)
	assert.NotContains(t, string(data), "super-secret-token")
}

func TestNetworkManager_AddNetwork_Custom(t *testing.T) {
	mockConfigManager := &MockConfigurationManager{}
	mockChainListService := &MockChainListService{}

	// Setup mock expectations
	cfg := &config.Config{
		Networks: make(map[string]config.Network),
	}
	mockConfigManager.On("LoadConfiguration").Return(cfg, nil)
	mockConfigManager.On("SaveConfiguration", mock.AnythingOfType("*config.Config")).Return(nil)

	// Mock chainlist response - network not found (will be classified as custom)
	mockChainListService.On("GetChainInfo", 12345).Return(nil, assert.AnError)
	mockChainListService.On("ValidateRPCEndpoint", "https://test-rpc.example.com").Return(nil)
	mockChainListService.On("GetChainIDFromRPC", "https://test-rpc.example.com").Return(12345, nil)

	nm := NewNetworkManager(mockConfigManager, mockChainListService)

	network := config.Network{
		Name:        "Test Network",
		RPCEndpoint: "https://test-rpc.example.com",
		ChainID:     12345,
		Symbol:      "TEST",
		IsActive:    true,
	}

	err := nm.AddNetwork(network)

	assert.NoError(t, err)
	mockConfigManager.AssertExpectations(t)
	mockChainListService.AssertExpectations(t)

	// Verify the network was saved with the correct custom key
	savedConfig := mockConfigManager.config
	assert.NotNil(t, savedConfig)
	assert.Contains(t, savedConfig.Networks, "custom_test_network_12345")
}

func TestNetworkManager_UpdateNetworkRekeysAndPreservesReference(t *testing.T) {
	configManager := &MockConfigurationManager{}
	chainList := &MockChainListService{}
	cfg := &config.Config{Networks: map[string]config.Network{
		"custom_old_123": {Name: "Old", ChainID: 123, Symbol: "CUS", NativeDecimals: 6, NativeDecimalsSet: true, RPCEndpointRef: "env:BLOCO_TEST_EDIT_RPC", IsActive: false},
	}}
	t.Setenv("BLOCO_TEST_EDIT_RPC", "https://rpc.example.com/v2/credential")
	configManager.On("LoadConfiguration").Return(cfg, nil)
	configManager.On("SaveConfiguration", mock.AnythingOfType("*config.Config")).Return(nil)
	chainList.On("GetChainInfo", 123).Return(nil, assert.AnError)
	chainList.On("ValidateRPCEndpoint", "https://rpc.example.com/v2/credential").Return(nil)
	chainList.On("GetChainIDFromRPC", "https://rpc.example.com/v2/credential").Return(123, nil)
	manager := NewNetworkManager(configManager, chainList)
	err := manager.UpdateNetwork("custom_old_123", config.Network{Name: "Updated", ChainID: 123, Symbol: "CUS", NativeDecimals: 6, NativeDecimalsSet: true, RPCEndpointRef: "env:BLOCO_TEST_EDIT_RPC", IsActive: false})
	require.NoError(t, err)
	assert.NotContains(t, configManager.config.Networks, "custom_old_123")
	updated, exists := configManager.config.Networks["custom_updated_123"]
	assert.True(t, exists)
	assert.Equal(t, "env:BLOCO_TEST_EDIT_RPC", updated.RPCEndpointRef)
	assert.False(t, updated.IsActive)
}

func TestNetworkManager_RemoveNetwork(t *testing.T) {
	mockConfigManager := &MockConfigurationManager{}
	mockChainListService := &MockChainListService{}

	// Setup mock expectations with existing network
	cfg := &config.Config{
		Networks: map[string]config.Network{
			"custom_my_network_12345": {
				Name:        "My Network",
				RPCEndpoint: "https://my-rpc.example.com",
				ChainID:     12345,
				Symbol:      "MN",
				IsActive:    true,
			},
		},
	}
	mockConfigManager.On("LoadConfiguration").Return(cfg, nil)
	mockConfigManager.On("SaveConfiguration", mock.AnythingOfType("*config.Config")).Return(nil)

	nm := NewNetworkManager(mockConfigManager, mockChainListService)

	err := nm.RemoveNetwork("custom_my_network_12345")

	assert.NoError(t, err)
	mockConfigManager.AssertExpectations(t)

	// Verify the network was removed
	savedConfig := mockConfigManager.config
	assert.NotNil(t, savedConfig)
	assert.NotContains(t, savedConfig.Networks, "custom_my_network_12345")
}

// Integration test using real ConfigurationManager
func TestNetworkManager_Integration(t *testing.T) {
	// Create a temporary directory for the test
	tempDir, err := os.MkdirTemp("", "network_manager_test")
	assert.NoError(t, err)
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Logf("Failed to remove temp directory: %v", err)
		}
	}()

	// Set environment variable to use temp directory
	if err := os.Setenv("BLOCO_WALLET_APP_APP_DIR", tempDir); err != nil {
		t.Fatalf("Failed to set environment variable: %v", err)
	}
	defer func() {
		if err := os.Unsetenv("BLOCO_WALLET_APP_APP_DIR"); err != nil {
			t.Logf("Failed to unset environment variable: %v", err)
		}
	}()

	// Create real ConfigurationManager
	configManager := config.NewConfigurationManager()

	// Create mock ChainListService
	mockChainListService := &MockChainListService{}

	// Mock chainlist response for custom network
	mockChainListService.On("GetChainInfo", 12345).Return(nil, assert.AnError)
	mockChainListService.On("ValidateRPCEndpoint", "https://test-rpc.example.com").Return(nil)
	mockChainListService.On("GetChainIDFromRPC", "https://test-rpc.example.com").Return(12345, nil)

	nm := NewNetworkManager(configManager, mockChainListService)

	// Test adding a network
	network := config.Network{
		Name:        "Test Network",
		RPCEndpoint: "https://test-rpc.example.com",
		ChainID:     12345,
		Symbol:      "TEST",
		IsActive:    true,
	}

	err = nm.AddNetwork(network)
	assert.NoError(t, err)

	// Test loading networks
	networks, err := nm.LoadNetworks()
	assert.NoError(t, err)
	assert.Contains(t, networks, "custom_test_network_12345")

	// Test getting specific network
	retrievedNetwork, err := nm.GetNetwork("custom_test_network_12345")
	assert.NoError(t, err)
	assert.Equal(t, "Test Network", retrievedNetwork.Name)

	// Test removing network
	err = nm.RemoveNetwork("custom_test_network_12345")
	assert.NoError(t, err)

	// Verify removal
	networks, err = nm.LoadNetworks()
	assert.NoError(t, err)
	assert.NotContains(t, networks, "custom_test_network_12345")

	mockChainListService.AssertExpectations(t)
}

func TestNetworkManager_ValidateNetwork_Basic(t *testing.T) {
	mockConfigManager := &MockConfigurationManager{}
	mockChainListService := &MockChainListService{}

	// Setup mock expectations for validation
	mockChainListService.On("ValidateRPCEndpoint", "https://eth.llamarpc.com").Return(nil)
	mockChainListService.On("GetChainIDFromRPC", "https://eth.llamarpc.com").Return(1, nil)

	nm := NewNetworkManager(mockConfigManager, mockChainListService)

	network := config.Network{
		Name:        "Ethereum Mainnet",
		RPCEndpoint: "https://eth.llamarpc.com",
		ChainID:     1,
		Symbol:      "ETH",
		IsActive:    true,
	}

	err := nm.ValidateNetwork(network)

	assert.NoError(t, err)
	mockChainListService.AssertExpectations(t)
}

func TestNetworkManager_ValidateNetwork_EmptyName(t *testing.T) {
	mockConfigManager := &MockConfigurationManager{}
	mockChainListService := &MockChainListService{}

	nm := NewNetworkManager(mockConfigManager, mockChainListService)

	network := config.Network{
		Name:        "", // Empty name
		RPCEndpoint: "https://eth.llamarpc.com",
		ChainID:     1,
		Symbol:      "ETH",
		IsActive:    true,
	}

	err := nm.ValidateNetwork(network)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "name cannot be empty")
}
