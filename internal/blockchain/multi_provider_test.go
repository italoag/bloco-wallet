package blockchain

import (
	"context"
	"errors"
	"math/big"
	"testing"

	"blocowallet/pkg/config"
)

type testBalanceProvider struct {
	amount *big.Int
	err    error
	calls  int
}

func (provider *testBalanceProvider) GetBalance(context.Context, string) (*big.Int, error) {
	provider.calls++
	if provider.err != nil {
		return nil, provider.err
	}
	return new(big.Int).Set(provider.amount), nil
}

func (*testBalanceProvider) GetNetworkSymbol() string { return "ETH" }
func (*testBalanceProvider) GetNetworkDecimals() int  { return 18 }

func TestMultiProviderNeverConvertsFailuresToSyntheticBalances(t *testing.T) {
	provider := &testBalanceProvider{err: errors.New("provider unavailable")}
	multi := NewMultiProvider(NewRPCGateway(RPCGatewayOptions{}), config.EnvironmentCredentialProvider{})
	multi.AddProvider("network", provider, config.Network{Name: "Network", IsActive: true})
	results := multi.GetAllBalances(context.Background(), "0x0000000000000000000000000000000000000001")
	if len(results) != 1 || results[0].Amount != nil || results[0].Error == nil {
		t.Fatalf("provider failure became synthetic data: %+v", results)
	}
}

func TestCancelledProviderRefreshCannotReplaceExistingState(t *testing.T) {
	provider := &testBalanceProvider{amount: big.NewInt(7)}
	multi := NewMultiProvider(NewRPCGateway(RPCGatewayOptions{}), config.EnvironmentCredentialProvider{})
	multi.AddProvider("network", provider, config.Network{Name: "Network", NativeDecimals: 18, NativeDecimalsSet: true, IsActive: true})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if failures := multi.RefreshProviders(ctx, &config.Config{Networks: map[string]config.Network{}}); len(failures) == 0 {
		t.Fatal("cancelled refresh was reported as success")
	}
	results := multi.GetAllBalances(context.Background(), "0x0000000000000000000000000000000000000001")
	if len(results) != 1 || results[0].Amount == nil || results[0].Amount.Int64() != 7 {
		t.Fatalf("cancelled refresh replaced existing provider state: %+v", results)
	}
}

func TestMultiProviderCachesSuccessfulBalancesForBoundedTTL(t *testing.T) {
	provider := &testBalanceProvider{amount: big.NewInt(42)}
	multi := NewMultiProvider(NewRPCGateway(RPCGatewayOptions{}), config.EnvironmentCredentialProvider{})
	multi.AddProvider("network", provider, config.Network{Name: "Network", IsActive: true})
	for range 2 {
		results := multi.GetAllBalances(context.Background(), "0x0000000000000000000000000000000000000001")
		if len(results) != 1 || results[0].Amount == nil || results[0].Amount.Int64() != 42 {
			t.Fatalf("unexpected balance result: %+v", results)
		}
	}
	if provider.calls != 1 {
		t.Fatalf("balance cache did not prevent duplicate request: %d", provider.calls)
	}
}
