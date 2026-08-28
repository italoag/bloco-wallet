package config

import (
	"bytes"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"os"
	"strconv"
	"strings"

	"blocowallet/internal/terminal"

	"github.com/BurntSushi/toml"
)

type persistentAppConfig struct {
	AppDir       string `toml:"app_dir"`
	Language     string `toml:"language"`
	WalletsDir   string `toml:"wallets_dir"`
	DatabasePath string `toml:"database_path"`
	LocaleDir    string `toml:"locale_dir"`
}

type persistentDatabaseConfig struct {
	Type   string `toml:"type"`
	DSN    string `toml:"dsn,omitempty"`
	DSNRef string `toml:"dsn_ref,omitempty"`
}

type persistentSecurityConfig struct {
	Argon2Time                   uint32 `toml:"argon2_time"`
	Argon2Memory                 uint32 `toml:"argon2_memory"`
	Argon2Threads                uint8  `toml:"argon2_threads"`
	Argon2KeyLen                 uint32 `toml:"argon2_key_len"`
	SaltLength                   uint32 `toml:"salt_length"`
	TransactionAuthorizationMode string `toml:"transaction_authorization_mode"`
}

type persistentFontsConfig struct {
	Available []string `toml:"available"`
}

type persistentNetworkPolicyConfig struct {
	AllowedLocalTargets []string `toml:"allowed_local_targets"`
}

type persistentNetworkConfig struct {
	Name               string `toml:"name"`
	RPCEndpoint        string `toml:"rpc_endpoint,omitempty"`
	RPCEndpointRef     string `toml:"rpc_endpoint_ref,omitempty"`
	ChainID            int64  `toml:"chain_id"`
	Symbol             string `toml:"symbol"`
	NativeDecimals     int    `toml:"native_decimals"`
	NativeDecimalsSet  bool   `toml:"native_decimals_set"`
	ConfirmationTarget uint64 `toml:"confirmation_target,omitempty"`
	Explorer           string `toml:"explorer,omitempty"`
	IsActive           bool   `toml:"is_active"`
	RegistryListed     bool   `toml:"registry_listed"`
	IdentityValidated  bool   `toml:"identity_validated"`
	Tracking           string `toml:"tracking,omitempty"`
}

type persistentConfig struct {
	App           persistentAppConfig                `toml:"app"`
	Database      persistentDatabaseConfig           `toml:"database"`
	Security      persistentSecurityConfig           `toml:"security"`
	NetworkPolicy persistentNetworkPolicyConfig      `toml:"network_policy"`
	Fonts         persistentFontsConfig              `toml:"fonts"`
	Networks      map[string]persistentNetworkConfig `toml:"networks"`
}

func validateAllowedLocalTargets(targets []string) error {
	if len(targets) > 16 {
		return fmt.Errorf("local RPC target policy exceeds entry budget")
	}
	seen := make(map[string]struct{}, len(targets))
	for _, target := range targets {
		if target != strings.ToLower(strings.TrimSpace(target)) || len(target) > 255 || strings.ContainsAny(target, "\x00\r\n\x1b") {
			return fmt.Errorf("local RPC target is outside policy")
		}
		host, port, err := net.SplitHostPort(target)
		portNumber, portErr := strconv.ParseUint(port, 10, 16)
		if err != nil || host == "" || portErr != nil || portNumber == 0 {
			return fmt.Errorf("local RPC target must be an exact host and port")
		}
		if address, err := netip.ParseAddr(host); err == nil && !address.IsLoopback() && !address.IsPrivate() {
			return fmt.Errorf("local RPC target address is not local")
		}
		if _, exists := seen[target]; exists {
			return fmt.Errorf("local RPC target is duplicated")
		}
		seen[target] = struct{}{}
	}
	return nil
}

func endpointPathMayContainCredential(escapedPath string) bool {
	segments := strings.Split(strings.Trim(escapedPath, "/"), "/")
	for index, segment := range segments {
		lower := strings.ToLower(segment)
		if lower == "" {
			continue
		}
		if index > 0 {
			previous := strings.ToLower(segments[index-1])
			if previous == "v1" || previous == "v2" || previous == "v3" || strings.Contains(previous, "key") || strings.Contains(previous, "token") || strings.Contains(previous, "project") {
				return true
			}
		}
		if strings.Contains(lower, "apikey") || strings.Contains(lower, "api-key") || strings.Contains(lower, "token") || strings.Contains(lower, "secret") || len(segment) >= 24 {
			return true
		}
	}
	return false
}

func validatePublicEndpoint(rawURL string) error {
	parsed, err := url.ParseRequestURI(rawURL)
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" {
		return fmt.Errorf("RPC endpoint must be an absolute HTTP(S) URL")
	}
	hostnameLabels := strings.Split(parsed.Hostname(), ".")
	firstLabel := strings.ToLower(hostnameLabels[0])
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || endpointPathMayContainCredential(parsed.EscapedPath()) || len(firstLabel) >= 24 || strings.Contains(firstLabel, "key") || strings.Contains(firstLabel, "token") || strings.Contains(firstLabel, "secret") {
		return fmt.Errorf("RPC endpoint credentials require a credential reference")
	}
	if strings.ContainsAny(rawURL, "\r\n\x00\x1b") {
		return fmt.Errorf("RPC endpoint contains unsafe characters")
	}
	return nil
}

func validateExplorerURL(rawURL string) error {
	if rawURL == "" {
		return nil
	}
	parsed, err := url.ParseRequestURI(rawURL)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" || len(rawURL) > 4096 || strings.ContainsAny(rawURL, "\x00\r\n\x1b") {
		return fmt.Errorf("explorer URL is outside policy")
	}
	return nil
}

func isEnvironmentCredentialValue(value string) bool {
	if value == "" {
		return false
	}
	for _, entry := range os.Environ() {
		_, environmentValue, found := strings.Cut(entry, "=")
		if found && environmentValue == value {
			return true
		}
	}
	return false
}

func marshalPersistentConfig(cfg *Config) ([]byte, error) {
	if cfg == nil {
		return nil, fmt.Errorf("configuration is required")
	}
	if len(cfg.Networks) > 256 {
		return nil, fmt.Errorf("network configuration exceeds entry budget")
	}
	if err := validateAllowedLocalTargets(cfg.NetworkPolicy.AllowedLocalTargets); err != nil {
		return nil, err
	}
	if cfg.Database.DSN != "" && cfg.Database.DSNRef != "" {
		return nil, fmt.Errorf("database DSN and credential reference are mutually exclusive")
	}
	if cfg.Database.DSN != "" && (isEnvironmentCredentialValue(cfg.Database.DSN) || !strings.EqualFold(cfg.Database.Type, "sqlite") || strings.ContainsAny(cfg.Database.DSN, "?#\r\n\x00\x1b")) {
		return nil, fmt.Errorf("database DSN requires a credential reference")
	}
	if cfg.Database.DSNRef != "" {
		if err := ValidateCredentialReference(cfg.Database.DSNRef); err != nil {
			return nil, err
		}
	}
	authorizationMode := cfg.Security.TransactionAuthorizationMode
	if authorizationMode == "" {
		authorizationMode = "password_per_transaction"
	}
	if authorizationMode != "password_per_transaction" && authorizationMode != "temporary_session" {
		return nil, fmt.Errorf("transaction authorization mode is outside policy")
	}
	persisted := persistentConfig{
		App: persistentAppConfig{
			AppDir:       cfg.AppDir,
			Language:     terminal.SanitizeInline(cfg.Language, 16),
			WalletsDir:   cfg.WalletsDir,
			DatabasePath: cfg.DatabasePath,
			LocaleDir:    cfg.LocaleDir,
		},
		Database: persistentDatabaseConfig{Type: cfg.Database.Type, DSN: cfg.Database.DSN, DSNRef: cfg.Database.DSNRef},
		Security: persistentSecurityConfig{
			Argon2Time:                   cfg.Security.Argon2Time,
			Argon2Memory:                 cfg.Security.Argon2Memory,
			Argon2Threads:                cfg.Security.Argon2Threads,
			Argon2KeyLen:                 cfg.Security.Argon2KeyLen,
			SaltLength:                   cfg.Security.SaltLength,
			TransactionAuthorizationMode: authorizationMode,
		},
		NetworkPolicy: persistentNetworkPolicyConfig{AllowedLocalTargets: append([]string(nil), cfg.NetworkPolicy.AllowedLocalTargets...)},
		Fonts:         persistentFontsConfig{Available: append([]string(nil), cfg.Fonts...)},
		Networks:      make(map[string]persistentNetworkConfig, len(cfg.Networks)),
	}
	for key, network := range cfg.Networks {
		if key == "" || terminal.SanitizeInline(key, 128) != key {
			return nil, fmt.Errorf("network key is outside policy")
		}
		tracking := strings.ToLower(strings.TrimSpace(network.Tracking))
		if tracking != "" && tracking != "none" && tracking != "limited" && tracking != "yes" && tracking != "unknown" {
			return nil, fmt.Errorf("network %q tracking metadata is outside policy", terminal.SanitizeInline(key, 64))
		}
		if network.ChainID <= 0 || network.NativeDecimals < 0 || network.NativeDecimals > 36 || network.ConfirmationTarget > 10_000 || network.Name == "" || terminal.SanitizeInline(network.Name, 128) != network.Name || terminal.SanitizeInline(network.Symbol, 16) != network.Symbol {
			return nil, fmt.Errorf("network %q metadata is outside policy", terminal.SanitizeInline(key, 64))
		}
		if network.RPCEndpoint != "" && network.RPCEndpointRef != "" {
			return nil, fmt.Errorf("network %q has both RPC endpoint and credential reference", terminal.SanitizeInline(key, 64))
		}
		if network.RPCEndpoint != "" && isEnvironmentCredentialValue(network.RPCEndpoint) {
			return nil, fmt.Errorf("network %q environment endpoint requires a credential reference", terminal.SanitizeInline(key, 64))
		}
		if network.RPCEndpoint != "" {
			if err := validatePublicEndpoint(network.RPCEndpoint); err != nil {
				return nil, fmt.Errorf("network %q: %w", terminal.SanitizeInline(key, 64), err)
			}
		}
		if network.RPCEndpointRef != "" {
			if err := ValidateCredentialReference(network.RPCEndpointRef); err != nil {
				return nil, fmt.Errorf("network %q: %w", terminal.SanitizeInline(key, 64), err)
			}
		}
		if err := validateExplorerURL(network.Explorer); err != nil {
			return nil, fmt.Errorf("network %q: %w", terminal.SanitizeInline(key, 64), err)
		}
		persisted.Networks[key] = persistentNetworkConfig{
			Name:               network.Name,
			RPCEndpoint:        network.RPCEndpoint,
			RPCEndpointRef:     network.RPCEndpointRef,
			ChainID:            network.ChainID,
			Symbol:             network.Symbol,
			NativeDecimals:     network.NativeDecimals,
			NativeDecimalsSet:  network.NativeDecimalsSet,
			ConfirmationTarget: network.ConfirmationTarget,
			Explorer:           network.Explorer,
			IsActive:           network.IsActive,
			RegistryListed:     network.RegistryListed,
			IdentityValidated:  network.IdentityValidated,
			Tracking:           tracking,
		}
	}
	var output bytes.Buffer
	if err := toml.NewEncoder(&output).Encode(persisted); err != nil {
		return nil, err
	}
	if output.Len() == 0 {
		return nil, fmt.Errorf("encoded configuration is empty")
	}
	return output.Bytes(), nil
}
