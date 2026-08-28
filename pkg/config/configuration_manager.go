package config

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/spf13/viper"
)

// ConfigurationManagerInterface defines the interface for configuration management
type ConfigurationManagerInterface interface {
	LoadConfiguration() (*Config, error)
	SaveConfiguration(cfg *Config) error
	GetConfigPath() string
	GetAppDirectory() string
}

var ErrConfigConflict = errors.New("configuration changed since it was loaded")

// ConfigurationManager manages configuration loading and saving using Viper
type ConfigurationManager struct {
	viper        *viper.Viper
	configPath   string
	appDir       string
	loadedDigest [sha256.Size]byte
	hasDigest    bool
	mu           sync.Mutex
}

// NewConfigurationManager creates a new ConfigurationManager instance
func NewConfigurationManager() *ConfigurationManager {
	return &ConfigurationManager{
		viper: viper.New(),
	}
}

func (cm *ConfigurationManager) configureViper() {
	cm.viper.SetConfigName("config")
	cm.viper.SetConfigType("toml")
	cm.viper.AddConfigPath(cm.appDir)
	cm.viper.SetEnvPrefix("BLOCO_WALLET")
	cm.viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	cm.viper.AutomaticEnv()
}

// LoadConfiguration loads the configuration using Viper with proper directory resolution
func (cm *ConfigurationManager) LoadConfiguration() (*Config, error) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	// Determine the application directory
	appDir, err := cm.resolveAppDirectory()
	if err != nil {
		return nil, fmt.Errorf("failed to resolve application directory: %w", err)
	}

	cm.appDir = appDir
	cm.configPath = filepath.Join(appDir, "config.toml")

	cm.viper = viper.New()
	cm.configureViper()
	if err := ensurePrivateConfigDirectory(cm.appDir); err != nil {
		return nil, fmt.Errorf("failed to secure config directory: %w", err)
	}
	unlock, err := lockConfigFile(cm.configPath)
	if err != nil {
		return nil, fmt.Errorf("lock configuration: %w", err)
	}
	defer unlock()
	// Check if config file exists, create default if not
	if err := cm.ensureConfigFile(); err != nil {
		return nil, fmt.Errorf("failed to ensure config file: %w", err)
	}
	if err := validatePrivateConfigPath(cm.configPath, false); err != nil {
		return nil, err
	}
	data, err := readPrivateConfigFile(cm.configPath)
	if err != nil {
		return nil, err
	}
	if err := cm.viper.ReadConfig(bytes.NewReader(data)); err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}
	cm.loadedDigest = sha256.Sum256(data)
	cm.hasDigest = true

	// Build and return the Config struct
	cfg, err := cm.buildConfigStruct()
	if err != nil {
		return nil, err
	}
	if _, err := marshalPersistentConfig(cfg); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}
	return cfg, nil
}

// SaveConfiguration saves the configuration maintaining Viper compatibility
func (cm *ConfigurationManager) SaveConfiguration(cfg *Config) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if cm.configPath == "" {
		return fmt.Errorf("configuration not loaded, cannot save")
	}
	unlock, err := lockConfigFile(cm.configPath)
	if err != nil {
		return fmt.Errorf("lock configuration: %w", err)
	}
	defer unlock()
	if err := validatePrivateConfigPath(cm.configPath, false); err != nil {
		return err
	}
	current, err := readPrivateConfigFile(cm.configPath)
	if err != nil {
		return err
	}
	currentHash := sha256.Sum256(current)
	if cfg == nil || !cfg.hasRevision || cfg.revision != currentHash {
		return ErrConfigConflict
	}
	data, err := marshalPersistentConfig(cfg)
	if err != nil {
		return fmt.Errorf("encode configuration: %w", err)
	}
	writeErr := writeAtomicConfig(cm.configPath, data, true)
	if writeErr != nil && !IsConfigCommitted(writeErr) {
		return fmt.Errorf("write configuration: %w", writeErr)
	}
	cm.loadedDigest = sha256.Sum256(data)
	cm.hasDigest = true
	cfg.revision = cm.loadedDigest
	cfg.hasRevision = true
	cm.viper = viper.New()
	cm.configureViper()
	if err := cm.viper.ReadConfig(bytes.NewReader(data)); err != nil {
		return fmt.Errorf("reload configuration: %w", err)
	}
	if writeErr != nil {
		return fmt.Errorf("write configuration: %w", writeErr)
	}
	return nil
}

// GetConfigPath returns the path to the configuration file
func (cm *ConfigurationManager) GetConfigPath() string {
	return cm.configPath
}

// GetAppDirectory returns the resolved application directory
func (cm *ConfigurationManager) GetAppDirectory() string {
	return cm.appDir
}

// resolveAppDirectory determines the appropriate application directory
func (cm *ConfigurationManager) resolveAppDirectory() (string, error) {
	// First check if there's an environment variable override
	if envAppDir := os.Getenv("BLOCO_WALLET_APP_APP_DIR"); envAppDir != "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("failed to get user home directory: %w", err)
		}
		return expandPath(envAppDir, homeDir), nil
	}

	// Check for legacy environment variable
	if legacyAppDir := os.Getenv("BLOCO_WALLET_APP_DIR"); legacyAppDir != "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("failed to get user home directory: %w", err)
		}
		return expandPath(legacyAppDir, homeDir), nil
	}

	// Use the default OS-specific directory resolution
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get user home directory: %w", err)
	}

	return resolveBlocoUserDir(homeDir), nil
}

// ensureConfigFile ensures the config file exists, creating it from default if needed
func (cm *ConfigurationManager) ensureConfigFile() error {
	// Create directory if it doesn't exist
	if err := ensurePrivateConfigDirectory(cm.appDir); err != nil {
		return fmt.Errorf("failed to secure config directory: %w", err)
	}
	if err := validatePrivateConfigPath(cm.configPath, true); err != nil {
		return err
	}
	if _, err := os.Lstat(cm.configPath); os.IsNotExist(err) {
		defaultConfigData, err := defaultConfig.ReadFile("default_config.toml")
		if err != nil {
			return fmt.Errorf("failed to read default config: %w", err)
		}
		if err := writeAtomicConfig(cm.configPath, defaultConfigData, false); err != nil && !IsConfigCommitted(err) {
			return fmt.Errorf("failed to write default config: %w", err)
		}
	} else if err != nil {
		return err
	}
	if err := secureConfigPermissions(cm.configPath, false); err != nil {
		return fmt.Errorf("failed to secure config file: %w", err)
	}

	return nil
}

// buildConfigStruct builds a Config struct from Viper values
func (cm *ConfigurationManager) buildConfigStruct() (*Config, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get user home directory: %w", err)
	}

	cfg := &Config{
		AppDir:       cm.viper.GetString("app.app_dir"),
		Language:     cm.viper.GetString("app.language"),
		WalletsDir:   cm.viper.GetString("app.wallets_dir"),
		DatabasePath: cm.viper.GetString("app.database_path"),
		LocaleDir:    cm.viper.GetString("app.locale_dir"),
		Fonts:        cm.viper.GetStringSlice("fonts.available"),
		Database: DatabaseConfig{
			Type:   cm.viper.GetString("database.type"),
			DSN:    cm.viper.GetString("database.dsn"),
			DSNRef: cm.viper.GetString("database.dsn_ref"),
		},
		Security: SecurityConfig{
			Argon2Time:                   cm.viper.GetUint32("security.argon2_time"),
			Argon2Memory:                 cm.viper.GetUint32("security.argon2_memory"),
			Argon2Threads:                uint8(cm.viper.GetUint("security.argon2_threads")),
			Argon2KeyLen:                 cm.viper.GetUint32("security.argon2_key_len"),
			SaltLength:                   cm.viper.GetUint32("security.salt_length"),
			TransactionAuthorizationMode: cm.viper.GetString("security.transaction_authorization_mode"),
		},
		NetworkPolicy: NetworkPolicyConfig{AllowedLocalTargets: cm.viper.GetStringSlice("network_policy.allowed_local_targets")},
		Networks:      make(map[string]Network),
	}

	// Load networks from config
	networksMap := cm.viper.GetStringMap("networks")
	for key := range networksMap {
		networkKey := "networks." + key
		network := Network{
			Name:               cm.viper.GetString(networkKey + ".name"),
			RPCEndpoint:        cm.viper.GetString(networkKey + ".rpc_endpoint"),
			RPCEndpointRef:     cm.viper.GetString(networkKey + ".rpc_endpoint_ref"),
			ChainID:            cm.viper.GetInt64(networkKey + ".chain_id"),
			Symbol:             cm.viper.GetString(networkKey + ".symbol"),
			NativeDecimals:     cm.viper.GetInt(networkKey + ".native_decimals"),
			NativeDecimalsSet:  cm.viper.GetBool(networkKey + ".native_decimals_set"),
			ConfirmationTarget: cm.viper.GetUint64(networkKey + ".confirmation_target"),
			Explorer:           cm.viper.GetString(networkKey + ".explorer"),
			IsActive:           cm.viper.GetBool(networkKey + ".is_active"),
			RegistryListed:     cm.viper.GetBool(networkKey + ".registry_listed"),
			IdentityValidated:  cm.viper.GetBool(networkKey + ".identity_validated"),
			Tracking:           cm.viper.GetString(networkKey + ".tracking"),
		}
		cfg.Networks[key] = network
	}

	// Resolve paths using the same logic as the original LoadConfig
	cfg.AppDir = cm.appDir // Use the resolved app directory

	// Keep raw values to detect if fields were intentionally left empty
	rawWalletsDir := strings.TrimSpace(cfg.WalletsDir)
	rawDatabasePath := strings.TrimSpace(cfg.DatabasePath)
	rawLocaleDir := strings.TrimSpace(cfg.LocaleDir)

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

	// Handle legacy environment variables - these override the config file values
	walletsWasDefault := rawWalletsDir == ""
	dbWasDefault := rawDatabasePath == ""
	localeWasDefault := rawLocaleDir == ""

	// Process legacy environment variables that override specific paths
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

	// Handle legacy app dir override - this affects dependent paths
	if legacy := os.Getenv("BLOCO_WALLET_APP_APP_DIR"); legacy != "" {
		cfg.AppDir = expandPath(legacy, homeDir)
		// Re-derive dependent paths only if they were using defaults
		if walletsWasDefault && os.Getenv("BLOCO_WALLET_APP_KEYSTORE_DIR") == "" && os.Getenv("BLOCO_WALLET_APP_WALLETS_DIR") == "" {
			cfg.WalletsDir = filepath.Join(cfg.AppDir, "keystore")
		}
		if dbWasDefault && os.Getenv("BLOCO_WALLET_APP_DATABASE_PATH") == "" {
			cfg.DatabasePath = filepath.Join(cfg.AppDir, "bloco.db")
		}
		if localeWasDefault {
			cfg.LocaleDir = filepath.Join(cfg.AppDir, "locale")
		}
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

	cfg.revision = cm.loadedDigest
	cfg.hasRevision = cm.hasDigest
	return cfg, nil
}

// ReloadConfiguration reloads the configuration from file
func (cm *ConfigurationManager) ReloadConfiguration() (*Config, error) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if cm.configPath == "" {
		return nil, fmt.Errorf("configuration not initialized")
	}

	unlock, err := lockConfigFile(cm.configPath)
	if err != nil {
		return nil, fmt.Errorf("lock configuration: %w", err)
	}
	defer unlock()
	if err := validatePrivateConfigPath(cm.configPath, false); err != nil {
		return nil, err
	}
	data, err := readPrivateConfigFile(cm.configPath)
	if err != nil {
		return nil, err
	}
	cm.viper = viper.New()
	cm.configureViper()
	if err := cm.viper.ReadConfig(bytes.NewReader(data)); err != nil {
		return nil, fmt.Errorf("failed to reload config file: %w", err)
	}
	cm.loadedDigest = sha256.Sum256(data)
	cm.hasDigest = true
	cfg, err := cm.buildConfigStruct()
	if err != nil {
		return nil, err
	}
	if _, err := marshalPersistentConfig(cfg); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}
	return cfg, nil
}
