package blockchain

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockChainListService is a mock implementation of ChainListServiceInterface for testing
type MockChainListService struct {
	mock.Mock
}

func (m *MockChainListService) GetChainInfoWithRetry(chainID int) (*ChainInfo, string, error) {
	args := m.Called(chainID)
	if args.Get(0) == nil {
		return nil, args.String(1), args.Error(2)
	}
	return args.Get(0).(*ChainInfo), args.String(1), args.Error(2)
}

func (m *MockChainListService) ValidateRPCEndpoint(rpcURL string) error {
	args := m.Called(rpcURL)
	return args.Error(0)
}

func (m *MockChainListService) GetChainIDFromRPC(rpcURL string) (int, error) {
	args := m.Called(rpcURL)
	return args.Int(0), args.Error(1)
}

func TestNewNetworkClassificationService(t *testing.T) {
	chainListService := &MockChainListService{}
	ncs := NewNetworkClassificationService(chainListService)

	assert.NotNil(t, ncs)
	assert.Equal(t, chainListService, ncs.chainListService)
}

func TestNetworkClassificationService_ClassifyNetwork_Standard(t *testing.T) {
	mockChainList := &MockChainListService{}
	ncs := NewNetworkClassificationService(mockChainList)

	// Mock chain info for Ethereum mainnet
	chainInfo := &ChainInfo{
		ChainID: 1,
		Name:    "Ethereum Mainnet",
		NativeCurrency: struct {
			Name     string `json:"name"`
			Symbol   string `json:"symbol"`
			Decimals int    `json:"decimals"`
		}{
			Name:     "Ether",
			Symbol:   "ETH",
			Decimals: 18,
		},
		RPC: []RPCEndpoint{
			{URL: "https://eth.drpc.org", IsOpenSource: true},
		},
	}

	mockChainList.On("GetChainInfoWithRetry", 1).Return(chainInfo, "https://eth.drpc.org", nil)

	classification, err := ncs.ClassifyNetwork(1, "Ethereum Mainnet", "https://eth.drpc.org")

	assert.NoError(t, err)
	assert.NotNil(t, classification)
	assert.Equal(t, NetworkTypeStandard, classification.Type)
	assert.True(t, classification.IsValidated)
	assert.Equal(t, chainInfo, classification.ChainInfo)
	assert.Equal(t, "standard_ethereum_mainnet_1", classification.Key)
	assert.Equal(t, "chainlist", classification.Source)

	mockChainList.AssertExpectations(t)
}

func TestNetworkClassificationService_ClassifyNetwork_Custom(t *testing.T) {
	mockChainList := &MockChainListService{}
	ncs := NewNetworkClassificationService(mockChainList)

	// Mock chainlist returning error (network not found)
	mockChainList.On("GetChainInfoWithRetry", 999999).Return(nil, "", fmt.Errorf("chain not found"))

	classification, err := ncs.ClassifyNetwork(999999, "My Custom Network", "https://custom.rpc.com")

	assert.NoError(t, err)
	assert.NotNil(t, classification)
	assert.Equal(t, NetworkTypeCustom, classification.Type)
	assert.False(t, classification.IsValidated)
	assert.Nil(t, classification.ChainInfo)
	assert.Equal(t, "custom_my_custom_network_999999", classification.Key)
	assert.Equal(t, "manual", classification.Source)

	mockChainList.AssertExpectations(t)
}

func TestNetworkClassificationService_ValidateNetworkAgainstChainList_Success(t *testing.T) {
	mockChainList := &MockChainListService{}
	ncs := NewNetworkClassificationService(mockChainList)

	chainInfo := &ChainInfo{
		ChainID: 1,
		Name:    "Ethereum Mainnet",
		RPC: []RPCEndpoint{
			{URL: "https://eth.drpc.org", IsOpenSource: true},
		},
	}

	mockChainList.On("GetChainInfoWithRetry", 1).Return(chainInfo, "https://eth.drpc.org", nil)

	resultChainInfo, workingRPC, err := ncs.ValidateNetworkAgainstChainList(1, "https://eth.drpc.org")

	assert.NoError(t, err)
	assert.Equal(t, chainInfo, resultChainInfo)
	assert.Equal(t, "https://eth.drpc.org", workingRPC)

	mockChainList.AssertExpectations(t)
}

func TestNetworkClassificationService_ValidateNetworkAgainstChainList_CustomRPC(t *testing.T) {
	mockChainList := &MockChainListService{}
	ncs := NewNetworkClassificationService(mockChainList)

	chainInfo := &ChainInfo{
		ChainID: 1,
		Name:    "Ethereum Mainnet",
		RPC: []RPCEndpoint{
			{URL: "https://eth.drpc.org", IsOpenSource: true},
		},
	}

	customRPC := "https://custom.ethereum.rpc"

	mockChainList.On("GetChainInfoWithRetry", 1).Return(chainInfo, "https://eth.drpc.org", nil)
	mockChainList.On("ValidateRPCEndpoint", customRPC).Return(nil)
	mockChainList.On("GetChainIDFromRPC", customRPC).Return(1, nil)

	resultChainInfo, workingRPC, err := ncs.ValidateNetworkAgainstChainList(1, customRPC)

	assert.NoError(t, err)
	assert.Equal(t, chainInfo, resultChainInfo)
	assert.Equal(t, customRPC, workingRPC)

	mockChainList.AssertExpectations(t)
}

func TestNetworkClassificationService_ValidateNetworkAgainstChainList_InvalidChainID(t *testing.T) {
	mockChainList := &MockChainListService{}
	ncs := NewNetworkClassificationService(mockChainList)

	chainInfo := &ChainInfo{
		ChainID: 1,
		Name:    "Ethereum Mainnet",
		RPC: []RPCEndpoint{
			{URL: "https://eth.drpc.org", IsOpenSource: true},
		},
	}

	customRPC := "https://polygon.rpc"

	mockChainList.On("GetChainInfoWithRetry", 1).Return(chainInfo, "https://eth.drpc.org", nil)
	mockChainList.On("ValidateRPCEndpoint", customRPC).Return(nil)
	mockChainList.On("GetChainIDFromRPC", customRPC).Return(137, nil) // Polygon chain ID

	_, _, err := ncs.ValidateNetworkAgainstChainList(1, customRPC)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "RPC endpoint chain ID (137) does not match expected chain ID (1)")

	mockChainList.AssertExpectations(t)
}

func TestNetworkClassificationService_GenerateNetworkKey(t *testing.T) {
	ncs := NewNetworkClassificationService(nil)

	tests := []struct {
		networkType NetworkType
		name        string
		chainID     int
		expected    string
	}{
		{NetworkTypeStandard, "Ethereum Mainnet", 1, "standard_ethereum_mainnet_1"},
		{NetworkTypeCustom, "My Custom Network", 999, "custom_my_custom_network_999"},
		{NetworkTypeStandard, "Polygon PoS", 137, "standard_polygon_pos_137"},
		{NetworkTypeCustom, "Test Network with Special Chars: @#$%^&*()", 12345, "custom_test_network_with_special_chars_12345"},
		{NetworkTypeCustom, "", 1, "custom_unknown_1"}, // Empty name
	}

	for _, test := range tests {
		t.Run(fmt.Sprintf("%s_%s_%d", test.networkType, test.name, test.chainID), func(t *testing.T) {
			result := ncs.GenerateNetworkKey(test.networkType, test.name, test.chainID)
			assert.Equal(t, test.expected, result)
		})
	}
}

func TestNetworkClassificationService_SanitizeNetworkName(t *testing.T) {
	ncs := NewNetworkClassificationService(nil)

	tests := []struct {
		input    string
		expected string
	}{
		{"Ethereum Mainnet", "ethereum_mainnet"},
		{"Polygon PoS", "polygon_pos"},
		{"Test Network with Special Chars: @#$%^&*()", "test_network_with_special_chars"},
		{"  Leading and Trailing Spaces  ", "leading_and_trailing_spaces"},
		{"Multiple___Underscores", "multiple_underscores"},
		{"", "unknown"},
		{"A very long network name that exceeds the fifty character limit and should be truncated", "a_very_long_network_name_that_exceeds_the_fifty_ch"},
	}

	for _, test := range tests {
		t.Run(test.input, func(t *testing.T) {
			result := ncs.sanitizeNetworkName(test.input)
			assert.Equal(t, test.expected, result)
		})
	}
}

func TestNetworkClassificationService_IsNetworkStandard(t *testing.T) {
	ncs := NewNetworkClassificationService(nil)

	tests := []struct {
		key      string
		expected bool
	}{
		{"standard_ethereum_mainnet_1", true},
		{"custom_my_network_999", false},
		{"ethereum_mainnet_1", false}, // Legacy format
		{"standard_", true},           // Edge case
	}

	for _, test := range tests {
		t.Run(test.key, func(t *testing.T) {
			result := ncs.IsNetworkStandard(test.key)
			assert.Equal(t, test.expected, result)
		})
	}
}

func TestNetworkClassificationService_IsNetworkCustom(t *testing.T) {
	ncs := NewNetworkClassificationService(nil)

	tests := []struct {
		key      string
		expected bool
	}{
		{"custom_my_network_999", true},
		{"standard_ethereum_mainnet_1", false},
		{"ethereum_mainnet_1", false}, // Legacy format
		{"custom_", true},             // Edge case
	}

	for _, test := range tests {
		t.Run(test.key, func(t *testing.T) {
			result := ncs.IsNetworkCustom(test.key)
			assert.Equal(t, test.expected, result)
		})
	}
}

func TestNetworkClassificationService_GetNetworkTypeFromKey(t *testing.T) {
	ncs := NewNetworkClassificationService(nil)

	tests := []struct {
		key      string
		expected NetworkType
	}{
		{"standard_ethereum_mainnet_1", NetworkTypeStandard},
		{"custom_my_network_999", NetworkTypeCustom},
		{"ethereum_mainnet_1", NetworkTypeCustom}, // Legacy format defaults to custom
		{"unknown_format", NetworkTypeCustom},     // Unknown format defaults to custom
	}

	for _, test := range tests {
		t.Run(test.key, func(t *testing.T) {
			result := ncs.GetNetworkTypeFromKey(test.key)
			assert.Equal(t, test.expected, result)
		})
	}
}

func TestNetworkClassificationService_ClassifyExistingNetwork_WithTypePrefix(t *testing.T) {
	mockChainList := &MockChainListService{}
	ncs := NewNetworkClassificationService(mockChainList)

	// Test standard network with validation
	chainInfo := &ChainInfo{
		ChainID: 1,
		Name:    "Ethereum Mainnet",
		RPC: []RPCEndpoint{
			{URL: "https://eth.drpc.org", IsOpenSource: true},
		},
	}

	mockChainList.On("GetChainInfoWithRetry", 1).Return(chainInfo, "https://eth.drpc.org", nil)

	classification, err := ncs.ClassifyExistingNetwork("standard_ethereum_mainnet_1", 1, "Ethereum Mainnet", "https://eth.drpc.org")

	assert.NoError(t, err)
	assert.Equal(t, NetworkTypeStandard, classification.Type)
	assert.True(t, classification.IsValidated)
	assert.Equal(t, "standard_ethereum_mainnet_1", classification.Key)

	mockChainList.AssertExpectations(t)
}

func TestNetworkClassificationService_ClassifyExistingNetwork_LegacyFormat(t *testing.T) {
	mockChainList := &MockChainListService{}
	ncs := NewNetworkClassificationService(mockChainList)

	// Mock chainlist returning error (network not found)
	mockChainList.On("GetChainInfoWithRetry", 999).Return(nil, "", fmt.Errorf("chain not found"))

	classification, err := ncs.ClassifyExistingNetwork("legacy_network_999", 999, "Legacy Network", "https://legacy.rpc")

	assert.NoError(t, err)
	assert.Equal(t, NetworkTypeCustom, classification.Type)
	assert.False(t, classification.IsValidated)
	assert.Equal(t, "legacy_network_999", classification.Key) // Preserves original key for backward compatibility

	mockChainList.AssertExpectations(t)
}

func TestNetworkClassificationService_ValidateNetworkAgainstChainList_NoService(t *testing.T) {
	ncs := NewNetworkClassificationService(nil)

	_, _, err := ncs.ValidateNetworkAgainstChainList(1, "https://eth.drpc.org")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "chainlist service not available")
}
