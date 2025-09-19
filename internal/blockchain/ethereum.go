package blockchain

import (
	"context"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
)

// Ethereum implements wallet.BalanceProvider for Ethereum blockchain
type Ethereum struct {
	client   *ethclient.Client
	timeout  time.Duration
	symbol   string
	decimals int
}

// NewEthereum creates a new Ethereum balance provider
func NewEthereum(rpcURL string, timeout time.Duration, symbol string, decimals int) (*Ethereum, error) {
	client, err := ethclient.Dial(rpcURL)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Ethereum node: %w", err)
	}

	return &Ethereum{
		client:   client,
		timeout:  timeout,
		symbol:   symbol,
		decimals: decimals,
	}, nil
}

// GetBalance gets the ETH balance for an address
func (e *Ethereum) GetBalance(ctx context.Context, address string) (*big.Int, error) {
	// Add timeout to context
	ctx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()

	if !common.IsHexAddress(address) {
		return nil, fmt.Errorf("invalid Ethereum address: %s", address)
	}

	addr := common.HexToAddress(address)
	balance, err := e.client.BalanceAt(ctx, addr, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get balance for address %s: %w", address, err)
	}

	return balance, nil
}

// Close closes the Ethereum client connection
func (e *Ethereum) Close() {
	e.client.Close()
}

// GetNetworkSymbol returns the symbol of the network (ETH, MATIC, etc)
func (e *Ethereum) GetNetworkSymbol() string {
	return e.symbol
}

// GetNetworkDecimals returns the number of decimals for the network's native currency
func (e *Ethereum) GetNetworkDecimals() int {
	return e.decimals
}

// Mock implementation for testing/development
type Mock struct {
	symbol   string
	decimals int
}

// NewMock creates a new mock balance provider
func NewMock() *Mock {
	return &Mock{
		symbol:   "ETH",
		decimals: 18,
	}
}

// GetBalance returns a mock balance
func (m *Mock) GetBalance(ctx context.Context, address string) (*big.Int, error) {
	// Return a mock balance of 1.5 ETH (1.5 * 10^18 wei)
	balance := new(big.Int)
	balance.SetString("1500000000000000000", 10)
	return balance, nil
}

// GetNetworkSymbol returns the symbol of the network (ETH, MATIC, etc)
func (m *Mock) GetNetworkSymbol() string {
	return m.symbol
}

// GetNetworkDecimals returns the number of decimals for the network's native currency
func (m *Mock) GetNetworkDecimals() int {
	return m.decimals
}
