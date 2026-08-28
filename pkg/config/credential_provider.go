package config

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

var environmentCredentialName = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,127}$`)

type CredentialProvider interface {
	Resolve(reference string) (string, error)
}

type EnvironmentCredentialProvider struct{}

func ValidateCredentialReference(reference string) error {
	provider, name, found := strings.Cut(reference, ":")
	if !found || provider != "env" || !environmentCredentialName.MatchString(name) {
		return fmt.Errorf("invalid credential reference")
	}
	return nil
}

func (database DatabaseConfig) ResolveDSN(provider CredentialProvider) (string, error) {
	if database.DSN != "" && database.DSNRef != "" {
		return "", fmt.Errorf("database DSN and credential reference are mutually exclusive")
	}
	if database.DSNRef != "" {
		if provider == nil {
			return "", fmt.Errorf("credential provider is required")
		}
		return provider.Resolve(database.DSNRef)
	}
	return database.DSN, nil
}

func (network Network) ResolveRPCEndpoint(provider CredentialProvider) (string, error) {
	if network.RPCEndpoint != "" && network.RPCEndpointRef != "" {
		return "", fmt.Errorf("RPC endpoint and credential reference are mutually exclusive")
	}
	if network.RPCEndpointRef != "" {
		if provider == nil {
			return "", fmt.Errorf("credential provider is required")
		}
		return provider.Resolve(network.RPCEndpointRef)
	}
	if network.RPCEndpoint == "" {
		return "", fmt.Errorf("RPC endpoint is unavailable")
	}
	return network.RPCEndpoint, nil
}

func (EnvironmentCredentialProvider) Resolve(reference string) (string, error) {
	if err := ValidateCredentialReference(reference); err != nil {
		return "", err
	}
	_, name, _ := strings.Cut(reference, ":")
	value, exists := os.LookupEnv(name)
	if !exists || value == "" {
		return "", fmt.Errorf("credential reference is unavailable")
	}
	return value, nil
}
