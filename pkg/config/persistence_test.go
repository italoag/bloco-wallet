package config

import (
	"fmt"
	"testing"
)

func TestPersistentConfigValidatesTransactionPolicies(t *testing.T) {
	configuration := &Config{
		Database: DatabaseConfig{Type: "sqlite"},
		Security: SecurityConfig{TransactionAuthorizationMode: "temporary_session"},
		Networks: map[string]Network{
			"network": {Name: "Network", ChainID: 1, RPCEndpoint: "https://rpc.example.com", Symbol: "ETH", ConfirmationTarget: 12},
		},
	}
	if _, err := marshalPersistentConfig(configuration); err != nil {
		t.Fatal(err)
	}
	configuration.Security.TransactionAuthorizationMode = "unbounded"
	if _, err := marshalPersistentConfig(configuration); err == nil {
		t.Fatal("unknown transaction authorization mode was accepted")
	}
	configuration.Security.TransactionAuthorizationMode = "password_per_transaction"
	network := configuration.Networks["network"]
	network.ConfirmationTarget = 10_001
	configuration.Networks["network"] = network
	if _, err := marshalPersistentConfig(configuration); err == nil {
		t.Fatal("excessive confirmation target was accepted")
	}
}

func TestPersistentConfigRequiresReferencesForCredentialBearingValues(t *testing.T) {
	base := &Config{
		Database:      DatabaseConfig{Type: "sqlite"},
		NetworkPolicy: NetworkPolicyConfig{AllowedLocalTargets: []string{"127.0.0.1:8545"}},
		Networks: map[string]Network{
			"network": {Name: "Network", ChainID: 1, RPCEndpoint: "https://rpc.example.com", Symbol: "ETH"},
		},
	}
	if _, err := marshalPersistentConfig(base); err != nil {
		t.Fatal(err)
	}
	base.NetworkPolicy.AllowedLocalTargets = []string{"93.184.216.34:8545"}
	if _, err := marshalPersistentConfig(base); err == nil {
		t.Fatal("public address was accepted as a local RPC target")
	}
	base.NetworkPolicy.AllowedLocalTargets = []string{"127.0.0.1:8545"}
	if _, err := marshalPersistentConfig(base); err != nil {
		t.Fatal(err)
	}
	base.Networks["network"] = Network{Name: "Network", ChainID: 1, RPCEndpoint: "https://rpc.example.com/eth", Symbol: "ETH"}
	if _, err := marshalPersistentConfig(base); err != nil {
		t.Fatalf("credential-free RPC path was rejected: %v", err)
	}
	base.Networks["network"] = Network{Name: "Network", ChainID: 1, RPCEndpoint: "https://rpc.example.com/v3/token", Symbol: "ETH"}
	if _, err := marshalPersistentConfig(base); err == nil {
		t.Fatal("credential-bearing RPC path was persisted")
	}
	base.Networks["network"] = Network{Name: "Network", ChainID: 1, RPCEndpoint: "https://rpc.example.com/v1/12345678901234567890123456789012", Symbol: "ETH"}
	if _, err := marshalPersistentConfig(base); err == nil {
		t.Fatal("short-version credential path was persisted")
	}
	base.Networks["network"] = Network{Name: "Network", ChainID: 1, RPCEndpoint: "https://12345678901234567890123456789012.rpc.example.com", Symbol: "ETH"}
	if _, err := marshalPersistentConfig(base); err == nil {
		t.Fatal("credential-bearing RPC hostname was persisted")
	}
	base.Networks["network"] = Network{Name: "Network", ChainID: 1, RPCEndpointRef: "env:BLOCO_RPC_URL", Symbol: "ETH"}
	if _, err := marshalPersistentConfig(base); err != nil {
		t.Fatalf("credential reference was rejected: %v", err)
	}
	base.Database = DatabaseConfig{Type: "sqlite", DSN: "file:/vault.db?_key=super-secret"}
	if _, err := marshalPersistentConfig(base); err == nil {
		t.Fatal("credential-bearing SQLite DSN was persisted")
	}
	base.Database = DatabaseConfig{Type: "postgres", DSN: "postgres://user:password@db.example.com/wallet"}
	if _, err := marshalPersistentConfig(base); err == nil {
		t.Fatal("credential-bearing database DSN was persisted")
	}
	base.Database = DatabaseConfig{Type: "postgres", DSNRef: "env:BLOCO_DATABASE_DSN"}
	if _, err := marshalPersistentConfig(base); err != nil {
		t.Fatalf("database credential reference was rejected: %v", err)
	}
}

func TestPersistentConfigEnforcesNetworkBudget(t *testing.T) {
	cfg := &Config{Database: DatabaseConfig{Type: "sqlite"}, Networks: make(map[string]Network)}
	for index := range 257 {
		key := fmt.Sprintf("network_%d", index)
		cfg.Networks[key] = Network{Name: key, ChainID: int64(index + 1), RPCEndpoint: "https://rpc.example.com"}
	}
	if _, err := marshalPersistentConfig(cfg); err == nil {
		t.Fatal("oversized network configuration was persisted")
	}
}
