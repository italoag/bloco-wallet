package config

import (
	"strings"
	"testing"
)

func TestEnvironmentCredentialProviderResolvesReferences(t *testing.T) {
	t.Setenv("BLOCO_TEST_RPC_URL", "https://rpc.example.com/token")
	provider := EnvironmentCredentialProvider{}
	value, err := provider.Resolve("env:BLOCO_TEST_RPC_URL")
	if err != nil {
		t.Fatal(err)
	}
	if value != "https://rpc.example.com/token" {
		t.Fatal("credential provider returned wrong value")
	}
	for _, reference := range []string{"", "file:secret", "env:", "env:bad-name", "env:MISSING_BLOCO_VALUE"} {
		if _, err := provider.Resolve(reference); err == nil {
			t.Fatalf("invalid credential reference %q was accepted", reference)
		}
	}
}

func TestNetworkResolvesCredentialReferenceWithoutPersistingValue(t *testing.T) {
	t.Setenv("BLOCO_TEST_RPC_URL", "https://rpc.example.com/token")
	network := Network{RPCEndpointRef: "env:BLOCO_TEST_RPC_URL"}
	endpoint, err := network.ResolveRPCEndpoint(EnvironmentCredentialProvider{})
	if err != nil || endpoint != "https://rpc.example.com/token" {
		t.Fatalf("failed to resolve referenced endpoint: %v", err)
	}
	if _, err := (Network{}).ResolveRPCEndpoint(EnvironmentCredentialProvider{}); err == nil {
		t.Fatal("empty endpoint was resolved")
	}
	network.RPCEndpoint = "https://public.example.com"
	if _, err := network.ResolveRPCEndpoint(EnvironmentCredentialProvider{}); err == nil {
		t.Fatal("ambiguous endpoint configuration was resolved")
	}
}

func TestCredentialErrorsDoNotExposeResolvedValues(t *testing.T) {
	secret := "super-secret-token"
	t.Setenv("BLOCO_EMPTY_TEST", "")
	_, err := (EnvironmentCredentialProvider{}).Resolve("env:BLOCO_EMPTY_TEST")
	if err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("credential error leaked a secret: %v", err)
	}
}
