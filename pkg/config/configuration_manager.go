package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
)

// ConfigurationManagerInterface defines the interface for configuration management
type ConfigurationManagerInterface interface {
	LoadConfiguration() (*Config, error)
	SaveConfiguration(cfg *Config) error
	GetConfigPath() string
	GetAppDirectory() string
}

// ConfigurationManager manages configuration loading and saving using Viper
type ConfigurationManager struct {
	viper      *viper.Viper
	configPath string
	appDir     string
}

// NewConfigurationManager creates a new ConfigurationManager instance
func NewConfigurationManager() *ConfigurationManager {
	return &ConfigurationManager{
		viper: viper.New(),
	}
}

// LoadConfiguration loads the configuration using Viper with proper directory resolution
func (cm *ConfigurationManager) LoadConfiguration() (*Config, error) {
	// Determine the application directory
	appDir, err := cm.resolveAppDirectory()
	if err != nil {
		return nil, fmt.Errorf("failed to resolve application directory: %w", err)
	}

	cm.appDir = appDir
	cm.configPath = filepath.Join(appDir, "config.toml")

	// Configure Viper
	cm.viper.SetConfigName("config")
	cm.viper.SetConfigType("toml")
	cm.viper.AddConfigPath(appDir)

	// Set up environment variables support
	cm.viper.SetEnvPrefix("BLOCO_WALLET")
	cm.viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	cm.viper.AutomaticEnv()

	// Check if config file exists, create default if not
	if err := cm.ensureConfigFile(); err != nil {
		return nil, fmt.Errorf("failed to ensure config file: %w", err)
	}

	// Read the config file
	if err := cm.viper.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	// Build and return the Config struct
	return cm.buildConfigStruct()
}

// SaveConfiguration saves the configuration maintaining Viper compatibility
func (cm *ConfigurationManager) SaveConfiguration(cfg *Config) error {
	if cm.configPath == "" {
		return fmt.Errorf("configuration not loaded, cannot save")
	}

	// Update Viper with the new configuration values
	cm.updateViperFromConfig(cfg)

	// Write the configuration to file
	if err := cm.viper.WriteConfigAs(cm.configPath); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
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
	if err := os.MkdirAll(cm.appDir, os.ModePerm); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	// Check if config file exists
	if _, err := os.Stat(cm.configPath); os.IsNotExist(err) {
		// Read default config
		defaultConfigData, err := defaultConfig.ReadFile("default_config.toml")
		if err != nil {
			return fmt.Errorf("failed to read default config: %w", err)
		}

		// Write default config to file
		if err := os.WriteFile(cm.configPath, defaultConfigData, 0644); err != nil {
			return fmt.Errorf("failed to write default config: %w", err)
		}
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
			Type: cm.viper.GetString("database.type"),
			DSN:  cm.viper.GetString("database.dsn"),
		},
		Security: SecurityConfig{
			Argon2Time:    cm.viper.GetUint32("security.argon2_time"),
			Argon2Memory:  cm.viper.GetUint32("security.argon2_memory"),
			Argon2Threads: uint8(cm.viper.GetUint("security.argon2_threads")),
			Argon2KeyLen:  cm.viper.GetUint32("security.argon2_key_len"),
			SaltLength:    cm.viper.GetUint32("security.salt_length"),
		},
		Networks: make(map[string]Network),
	}

	// Load networks from config
	networksMap := cm.viper.GetStringMap("networks")
	for key := range networksMap {
		networkKey := "networks." + key
		network := Network{
			Name:        cm.viper.GetString(networkKey + ".name"),
			RPCEndpoint: cm.viper.GetString(networkKey + ".rpc_endpoint"),
			ChainID:     cm.viper.GetInt64(networkKey + ".chain_id"),
			Symbol:      cm.viper.GetString(networkKey + ".symbol"),
			Explorer:    cm.viper.GetString(networkKey + ".explorer"),
			IsActive:    cm.viper.GetBool(networkKey + ".is_active"),
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
	if legacy := os.Getenv("BLOCO_WALLET_DATABASE_DSN"); legacy != "" {
		cfg.Database.DSN = legacy
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

	return cfg, nil
}

// updateViperFromConfig updates Viper configuration from a Config struct
func (cm *ConfigurationManager) updateViperFromConfig(cfg *Config) {
	// App settings
	cm.viper.Set("app.app_dir", cfg.AppDir)
	cm.viper.Set("app.language", cfg.Language)
	cm.viper.Set("app.wallets_dir", cfg.WalletsDir)
	cm.viper.Set("app.database_path", cfg.DatabasePath)
	cm.viper.Set("app.locale_dir", cfg.LocaleDir)

	// Fonts
	cm.viper.Set("fonts.available", cfg.Fonts)

	// Database
	cm.viper.Set("database.type", cfg.Database.Type)
	cm.viper.Set("database.dsn", cfg.Database.DSN)

	// Security
	cm.viper.Set("security.argon2_time", cfg.Security.Argon2Time)
	cm.viper.Set("security.argon2_memory", cfg.Security.Argon2Memory)
	cm.viper.Set("security.argon2_threads", cfg.Security.Argon2Threads)
	cm.viper.Set("security.argon2_key_len", cfg.Security.Argon2KeyLen)
	cm.viper.Set("security.salt_length", cfg.Security.SaltLength)

	// Networks - completely replace the networks section
	// First, clear all existing network keys
	networksMap := cm.viper.GetStringMap("networks")
	for key := range networksMap {
		// Delete all sub-keys for this network
		cm.viper.Set("networks."+key+".name", nil)
		cm.viper.Set("networks."+key+".rpc_endpoint", nil)
		cm.viper.Set("networks."+key+".chain_id", nil)
		cm.viper.Set("networks."+key+".symbol", nil)
		cm.viper.Set("networks."+key+".explorer", nil)
		cm.viper.Set("networks."+key+".is_active", nil)
	}

	// Clear the entire networks section
	cm.viper.Set("networks", map[string]interface{}{})

	// Set new networks
	for key, network := range cfg.Networks {
		cm.viper.Set("networks."+key+".name", network.Name)
		cm.viper.Set("networks."+key+".rpc_endpoint", network.RPCEndpoint)
		cm.viper.Set("networks."+key+".chain_id", network.ChainID)
		cm.viper.Set("networks."+key+".symbol", network.Symbol)
		cm.viper.Set("networks."+key+".explorer", network.Explorer)
		cm.viper.Set("networks."+key+".is_active", network.IsActive)
	}
}

// ReloadConfiguration reloads the configuration from file
func (cm *ConfigurationManager) ReloadConfiguration() (*Config, error) {
	if cm.configPath == "" {
		return nil, fmt.Errorf("configuration not initialized")
	}

	if err := cm.viper.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("failed to reload config file: %w", err)
	}

	return cm.buildConfigStruct()
}
