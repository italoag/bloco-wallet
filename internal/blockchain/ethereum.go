package blockchain

import (
	"context"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

// Ethereum implements wallet.BalanceProvider for Ethereum blockchain
type Ethereum struct {
	gateway  *RPCGateway
	session  *ValidatedRPCSession
	timeout  time.Duration
	symbol   string
	decimals int
}

// NewEthereum creates a new Ethereum balance provider
func NewEthereum(ctx context.Context, gateway *RPCGateway, rpcURL string, expectedChainID int64, timeout time.Duration, symbol string, decimals int) (*Ethereum, error) {
	if gateway == nil {
		return nil, fmt.Errorf("RPC gateway is required")
	}
	session, err := gateway.ValidateChain(ctx, rpcURL, expectedChainID)
	if err != nil {
		return nil, fmt.Errorf("failed to validate Ethereum provider: %w", err)
	}
	return &Ethereum{
		gateway:  gateway,
		session:  session,
		timeout:  timeout,
		symbol:   symbol,
		decimals: decimals,
	}, nil
}

// GetBalance gets the ETH balance for an address
func (e *Ethereum) GetBalance(ctx context.Context, address string) (*big.Int, error) {
	ctx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()
	if !common.IsHexAddress(address) {
		return nil, fmt.Errorf("invalid Ethereum address")
	}
	var encoded string
	if err := e.gateway.Call(ctx, e.session, "eth_getBalance", []any{common.HexToAddress(address).Hex(), "latest"}, &encoded); err != nil {
		return nil, fmt.Errorf("failed to get balance: %w", err)
	}
	balance, err := parseRPCQuantity(encoded, 256)
	if err != nil {
		return nil, fmt.Errorf("RPC returned an invalid balance")
	}
	return balance, nil
}

// Close closes the Ethereum client connection
func (e *Ethereum) Close() {}

// GetNetworkSymbol returns the symbol of the network (ETH, MATIC, etc)
func (e *Ethereum) GetNetworkSymbol() string {
	return e.symbol
}

// GetNetworkDecimals returns the number of decimals for the network's native currency
func (e *Ethereum) GetNetworkDecimals() int {
	return e.decimals
}
