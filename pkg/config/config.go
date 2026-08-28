package config

import (
	"bytes"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/viper"
)

//go:embed default_config.toml
var defaultConfig embed.FS

// Config holds all application configuration
type Config struct {
	AppDir        string
	Language      string
	WalletsDir    string
	DatabasePath  string
	LocaleDir     string
	Fonts         []string
	Database      DatabaseConfig
	Security      SecurityConfig
	NetworkPolicy NetworkPolicyConfig
	Networks      map[string]Network
	revision      [32]byte
	hasRevision   bool
}

// DatabaseConfig holds database-specific configuration
type DatabaseConfig struct {
	Type   string // sqlite, postgres, mysql
	DSN    string // Data Source Name (connection string)
	DSNRef string
}

// SecurityConfig holds security-specific configuration
type SecurityConfig struct {
	Argon2Time                   uint32
	Argon2Memory                 uint32
	Argon2Threads                uint8
	Argon2KeyLen                 uint32
	SaltLength                   uint32
	TransactionAuthorizationMode string
}

type NetworkPolicyConfig struct {
	AllowedLocalTargets []string
}

// Network creates a new Config instance with default values
type Network struct {
	Name               string
	RPCEndpoint        string // RPC endpoint for the network
	RPCEndpointRef     string
	ChainID            int64
	Symbol             string
	NativeDecimals     int
	NativeDecimalsSet  bool
	ConfirmationTarget uint64
	Explorer           string
	IsActive           bool
	RegistryListed     bool
	IdentityValidated  bool
	Tracking           string
}

// LoadConfig loads the configuration from a TOML file using Viper
// It also supports environment variables with the prefix BLOCOWALLET_
func LoadConfig(appDir string) (*Config, error) {
	v := viper.New()

	// Set the default configuration file path
	configPath := filepath.Join(appDir, "config.toml")

	// Configure Viper
	v.SetConfigName("config")
	v.SetConfigType("toml")
	v.AddConfigPath(appDir)

	// Set up environment variables support
	// Prefix per guidelines: BLOCO_WALLET_
	v.SetEnvPrefix("BLOCO_WALLET")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	if err := ensurePrivateConfigDirectory(appDir); err != nil {
		return nil, fmt.Errorf("failed to secure config directory: %w", err)
	}
	unlock, err := lockConfigFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("lock configuration: %w", err)
	}
	defer unlock()
	if err := validatePrivateConfigPath(configPath, true); err != nil {
		return nil, err
	}
	if _, err := os.Lstat(configPath); os.IsNotExist(err) {
		defaultConfigData, err := defaultConfig.ReadFile("default_config.toml")
		if err != nil {
			return nil, fmt.Errorf("failed to read default config: %w", err)
		}
		if err := writeAtomicConfig(configPath, defaultConfigData, false); err != nil && !IsConfigCommitted(err) {
			return nil, fmt.Errorf("failed to write default config: %w", err)
		}
	} else if err != nil {
		return nil, err
	}

	if err := secureConfigPermissions(configPath, false); err != nil {
		return nil, fmt.Errorf("failed to secure config file: %w", err)
	}

	if err := validatePrivateConfigPath(configPath, false); err != nil {
		return nil, err
	}
	data, err := readPrivateConfigFile(configPath)
	if err != nil {
		return nil, err
	}
	// Read the config file
	if err := v.ReadConfig(bytes.NewReader(data)); err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	// Create config struct
	cfg := &Config{
		AppDir:       v.GetString("app.app_dir"),
		Language:     v.GetString("app.language"),
		WalletsDir:   v.GetString("app.wallets_dir"),
		DatabasePath: v.GetString("app.database_path"),
		LocaleDir:    v.GetString("app.locale_dir"),
		Fonts:        v.GetStringSlice("fonts.available"),
		Database: DatabaseConfig{
			Type:   v.GetString("database.type"),
			DSN:    v.GetString("database.dsn"),
			DSNRef: v.GetString("database.dsn_ref"),
		},
		Security: SecurityConfig{
			Argon2Time:                   v.GetUint32("security.argon2_time"),
			Argon2Memory:                 v.GetUint32("security.argon2_memory"),
			Argon2Threads:                uint8(v.GetUint("security.argon2_threads")),
			Argon2KeyLen:                 v.GetUint32("security.argon2_key_len"),
			SaltLength:                   v.GetUint32("security.salt_length"),
			TransactionAuthorizationMode: v.GetString("security.transaction_authorization_mode"),
		},
		NetworkPolicy: NetworkPolicyConfig{AllowedLocalTargets: v.GetStringSlice("network_policy.allowed_local_targets")},
		Networks:      make(map[string]Network),
	}

	// Load networks from config
	networksMap := v.GetStringMap("networks")
	for key := range networksMap {
		networkKey := "networks." + key
		network := Network{
			Name:               v.GetString(networkKey + ".name"),
			RPCEndpoint:        v.GetString(networkKey + ".rpc_endpoint"),
			RPCEndpointRef:     v.GetString(networkKey + ".rpc_endpoint_ref"),
			ChainID:            v.GetInt64(networkKey + ".chain_id"),
			Symbol:             v.GetString(networkKey + ".symbol"),
			NativeDecimals:     v.GetInt(networkKey + ".native_decimals"),
			NativeDecimalsSet:  v.GetBool(networkKey + ".native_decimals_set"),
			ConfirmationTarget: v.GetUint64(networkKey + ".confirmation_target"),
			Explorer:           v.GetString(networkKey + ".explorer"),
			IsActive:           v.GetBool(networkKey + ".is_active"),
			RegistryListed:     v.GetBool(networkKey + ".registry_listed"),
			IdentityValidated:  v.GetBool(networkKey + ".identity_validated"),
			Tracking:           v.GetString(networkKey + ".tracking"),
		}
		cfg.Networks[key] = network
	}

	// Resolve home directory and expand/apply defaults
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get user home directory: %w", err)
	}

	// Keep raw values to detect if fields were intentionally left empty
	rawAppDir := strings.TrimSpace(cfg.AppDir)
	rawWalletsDir := strings.TrimSpace(cfg.WalletsDir)
	rawDatabasePath := strings.TrimSpace(cfg.DatabasePath)
	rawLocaleDir := strings.TrimSpace(cfg.LocaleDir)

	// Resolve AppDir first
	cfg.AppDir = expandPath(rawAppDir, homeDir)

	// Derive defaults relative to AppDir when unspecified; otherwise expand provided paths
	if rawWalletsDir == "" {
		cfg.WalletsDir = filepath.Join(cfg.AppDir, "keystore")
	} else {
		cfg.WalletsDir = expandPath(rawWalletsDir, homeDir)
	}
	if rawDatabasePath == "" {
		cfg.DatabasePath = filepath.Join(cfg.AppDir, "bloco.db")
	} else {
		cfg.DatabasePath = expandPath(rawDatabasePath, homeDir)
	}
	if rawLocaleDir == "" {
		cfg.LocaleDir = filepath.Join(cfg.AppDir, "locale")
	} else {
		cfg.LocaleDir = expandPath(rawLocaleDir, homeDir)
	}

	// Backward-compatibility for legacy env variables with BLOCO_WALLET_ prefix.
	// Preferred env vars are handled by Viper with BLOCOWALLET_ prefix already.
	walletsWasDefault := rawWalletsDir == ""
	dbWasDefault := rawDatabasePath == ""
	localeWasDefault := rawLocaleDir == ""

	if legacy := os.Getenv("BLOCO_WALLET_APP_APP_DIR"); legacy != "" {
		cfg.AppDir = expandPath(legacy, homeDir)
		// If dependent paths were defaulted, re-derive them from the new AppDir
		if walletsWasDefault {
			cfg.WalletsDir = filepath.Join(cfg.AppDir, "keystore")
		}
		if dbWasDefault {
			cfg.DatabasePath = filepath.Join(cfg.AppDir, "bloco.db")
		}
		if localeWasDefault {
			cfg.LocaleDir = filepath.Join(cfg.AppDir, "locale")
		}
	}
	// Support both old KEYSTORE_DIR and a corrected WALLETS_DIR legacy name
	if legacy := os.Getenv("BLOCO_WALLET_APP_KEYSTORE_DIR"); legacy != "" {
		cfg.WalletsDir = expandPath(legacy, homeDir)
	}
	if legacy := os.Getenv("BLOCO_WALLET_APP_WALLETS_DIR"); legacy != "" {
		cfg.WalletsDir = expandPath(legacy, homeDir)
	}
	if legacy := os.Getenv("BLOCO_WALLET_APP_DATABASE_PATH"); legacy != "" {
		cfg.DatabasePath = expandPath(legacy, homeDir)
	}
	if legacy := os.Getenv("BLOCO_WALLET_DATABASE_TYPE"); legacy != "" {
		cfg.Database.Type = legacy
	}
	if _, exists := os.LookupEnv("BLOCO_WALLET_DATABASE_DSN"); exists {
		cfg.Database.DSN = ""
		cfg.Database.DSNRef = "env:BLOCO_WALLET_DATABASE_DSN"
	}

	// Set default values for security if not provided
	if cfg.Security.Argon2Time == 0 {
		cfg.Security.Argon2Time = 1
	}
	if cfg.Security.Argon2Memory == 0 {
		cfg.Security.Argon2Memory = 64 * 1024 // 64MB
	}
	if cfg.Security.Argon2Threads == 0 {
		cfg.Security.Argon2Threads = 4
	}
	if cfg.Security.Argon2KeyLen == 0 {
		cfg.Security.Argon2KeyLen = 32
	}
	if cfg.Security.SaltLength == 0 {
		cfg.Security.SaltLength = 16
	}
	if cfg.Security.TransactionAuthorizationMode == "" {
		cfg.Security.TransactionAuthorizationMode = "password_per_transaction"
	}

	if _, err := marshalPersistentConfig(cfg); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}
	return cfg, nil
}

// GetFontsList returns the list of available fonts
func (c *Config) GetFontsList() []string {
	return c.Fonts
}

func expandPath(path, homeDir string) string {
	// Normalize and handle trivial cases first
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		// If not provided, resolve to OS-specific default for this app
		return resolveBlocoUserDir(homeDir)
	}

	// Expand environment variables like $HOME, %APPDATA%, etc.
	expanded := os.ExpandEnv(trimmed)

	// Handle leading ~ or ~/ for the current user
	if strings.HasPrefix(expanded, "~") {
		// Only support current user ~ expansion; ~user is not supported cross-platform
		if expanded == "~" {
			expanded = homeDir
		} else if strings.HasPrefix(expanded, "~/") || strings.HasPrefix(expanded, "~\\") {
			expanded = filepath.Join(homeDir, strings.TrimPrefix(strings.TrimPrefix(expanded, "~/"), "~\\"))
		}
	}

	// Clean path separators for the current OS
	expanded = filepath.Clean(expanded)

	// If the path clearly points to a conventional home-based app folder for this project,
	// switch to the preferred OS-specific location while preserving intent.
	// We only redirect when the target is one of the known defaults to avoid surprising users
	// who provide explicit custom locations elsewhere.
	if isKnownHomeBlocoPath(expanded, homeDir) {
		return resolveBlocoUserDir(homeDir)
	}

	return expanded
}

// isKnownHomeBlocoPath returns true if the given path equals one of the common
// home-rooted defaults that we want to map to the OS-preferred data dir for this app.
func isKnownHomeBlocoPath(p, homeDir string) bool {
	p = filepath.Clean(p)
	homeDir = filepath.Clean(homeDir)
	// Known candidates in the user's home (both Unix-like and Windows with forward/backslashes)
	candidates := []string{
		filepath.Join(homeDir, ".bloco"),
		filepath.Join(homeDir, ".config", "bloco"),
		filepath.Join(homeDir, ".cache", "bloco"),
		// Historical/legacy option observed in this project
		filepath.Join(homeDir, ".wallets"),
	}
	for _, c := range candidates {
		if p == c {
			return true
		}
	}
	return false
}

// resolveBlocoUserDir determines the appropriate per-OS user data directory for the
// "bloco" application, respecting existing directories in a sensible hierarchy and
// falling back to the recommended default for the OS.
func resolveBlocoUserDir(homeDir string) string {
	osName := runtime.GOOS

	// Helper to check existence
	exists := func(path string) bool {
		if path == "" {
			return false
		}
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			return true
		}
		return false
	}

	switch osName {
	case "darwin":
		// macOS: prefer existing Unix-style locations if present, otherwise use
		// the recommended Application Support path.
		cfg := filepath.Join(homeDir, ".config", "bloco")
		legacy := filepath.Join(homeDir, ".bloco")
		cache := filepath.Join(homeDir, ".cache", "bloco")
		recommended := filepath.Join(homeDir, "Library", "Application Support", "bloco")
		for _, c := range []string{cfg, legacy, cache, recommended} {
			if exists(c) {
				return c
			}
		}
		return recommended

	case "windows":
		// Windows: prefer %APPDATA% (Roaming) if present, otherwise %LOCALAPPDATA%.
		appData := os.Getenv("APPDATA") // e.g., C:\\Users\\Name\\AppData\\Roaming
		localAppData := os.Getenv("LOCALAPPDATA")
		userProfile := os.Getenv("USERPROFILE")
		var candidates []string
		if appData != "" {
			candidates = append(candidates, filepath.Join(appData, "Bloco"))
		}
		if localAppData != "" {
			candidates = append(candidates, filepath.Join(localAppData, "Bloco"))
		}
		if userProfile != "" {
			candidates = append(candidates, filepath.Join(userProfile, ".bloco"))
		}
		// Pick the first existing; otherwise default to APPDATA if available
		for _, c := range candidates {
			if exists(c) {
				return c
			}
		}
		if appData != "" {
			return filepath.Join(appData, "Bloco")
		}
		if localAppData != "" {
			return filepath.Join(localAppData, "Bloco")
		}
		// Fallback to home-based hidden dir
		return filepath.Join(homeDir, ".bloco")

	default:
		// Linux/Unix/BSD: follow XDG Base Directory if available; otherwise ~/.config.
		xdgConfig := os.Getenv("XDG_CONFIG_HOME")
		if xdgConfig == "" {
			xdgConfig = filepath.Join(homeDir, ".config")
		}
		xdgCache := os.Getenv("XDG_CACHE_HOME")
		if xdgCache == "" {
			xdgCache = filepath.Join(homeDir, ".cache")
		}
		cfg := filepath.Join(xdgConfig, "bloco")
		legacy := filepath.Join(homeDir, ".bloco")
		cache := filepath.Join(xdgCache, "bloco")
		for _, c := range []string{cfg, legacy, cache} {
			if exists(c) {
				return c
			}
		}
		// Recommended default
		return cfg
	}
}
