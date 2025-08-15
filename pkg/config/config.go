package config

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
)

//go:embed default_config.toml
var defaultConfig embed.FS

// Config holds all application configuration
type Config struct {
	AppDir       string
	Language     string
	WalletsDir   string
	DatabasePath string
	LocaleDir    string
	Fonts        []string
	Database     DatabaseConfig
	Security     SecurityConfig
	Networks     map[string]Network
}

// DatabaseConfig holds database-specific configuration
type DatabaseConfig struct {
	Type string // sqlite, postgres, mysql
	DSN  string // Data Source Name (connection string)
}

// SecurityConfig holds security-specific configuration
type SecurityConfig struct {
	Argon2Time    uint32
	Argon2Memory  uint32
	Argon2Threads uint8
	Argon2KeyLen  uint32
	SaltLength    uint32
}

// Network creates a new Config instance with default values
type Network struct {
	Name        string
	RPCEndpoint string // RPC endpoint for the network
	ChainID     int64
	Symbol      string
	Explorer    string
	IsActive    bool
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
	v.SetEnvPrefix("BLOCO_WALLET")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	// Check if a config file exists
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		// Create a config directory if it doesn't exist
		if err := os.MkdirAll(appDir, os.ModePerm); err != nil {
			return nil, fmt.Errorf("failed to create config directory: %w", err)
		}

		// Read default config
		defaultConfigData, err := defaultConfig.ReadFile("default_config.toml")
		if err != nil {
			return nil, fmt.Errorf("failed to read default config: %w", err)
		}

		// Write default config to file
		if err := os.WriteFile(configPath, defaultConfigData, 0644); err != nil {
			return nil, fmt.Errorf("failed to write default config: %w", err)
		}
	}

	// Read the config file
	if err := v.ReadInConfig(); err != nil {
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
			Type: v.GetString("database.type"),
			DSN:  v.GetString("database.dsn"),
		},
		Security: SecurityConfig{
			Argon2Time:    v.GetUint32("security.argon2_time"),
			Argon2Memory:  v.GetUint32("security.argon2_memory"),
			Argon2Threads: uint8(v.GetUint("security.argon2_threads")),
			Argon2KeyLen:  v.GetUint32("security.argon2_key_len"),
			SaltLength:    v.GetUint32("security.salt_length"),
		},
		Networks: make(map[string]Network),
	}

	// Load networks from config
	networksMap := v.GetStringMap("networks")
	for key := range networksMap {
		networkKey := "networks." + key
		network := Network{
			Name:        v.GetString(networkKey + ".name"),
			RPCEndpoint: v.GetString(networkKey + ".rpc_endpoint"),
			ChainID:     v.GetInt64(networkKey + ".chain_id"),
			Symbol:      v.GetString(networkKey + ".symbol"),
			Explorer:    v.GetString(networkKey + ".explorer"),
			IsActive:    v.GetBool(networkKey + ".is_active"),
		}
		cfg.Networks[key] = network
	}

	// Expand paths with ~ to home directory
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get user home directory: %w", err)
	}

	cfg.AppDir = expandPath(cfg.AppDir, homeDir)
	cfg.WalletsDir = expandPath(cfg.WalletsDir, homeDir)
	cfg.DatabasePath = expandPath(cfg.DatabasePath, homeDir)
	cfg.LocaleDir = expandPath(cfg.LocaleDir, homeDir)

	// Override with environment variables if they exist
	if envAppDir := os.Getenv("BLOCO_WALLET_APP_APP_DIR"); envAppDir != "" {
		cfg.AppDir = expandPath(envAppDir, homeDir)
	}

	if envWalletsDir := os.Getenv("BLOCO_WALLET_APP_KEYSTORE_DIR"); envWalletsDir != "" {
		cfg.WalletsDir = expandPath(envWalletsDir, homeDir)
	}

	if envDatabasePath := os.Getenv("BLOCO_WALLET_APP_DATABASE_PATH"); envDatabasePath != "" {
		cfg.DatabasePath = expandPath(envDatabasePath, homeDir)
	}

	if envDatabaseType := os.Getenv("BLOCO_WALLET_DATABASE_TYPE"); envDatabaseType != "" {
		cfg.Database.Type = envDatabaseType
	}

	if envDatabaseDSN := os.Getenv("BLOCO_WALLET_DATABASE_DSN"); envDatabaseDSN != "" {
		cfg.Database.DSN = envDatabaseDSN
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

// GetFontsList returns the list of available fonts
func (c *Config) GetFontsList() []string {
	return c.Fonts
}

func expandPath(path, homeDir string) string {
	if len(path) > 1 && path[:2] == "~/" {
		return filepath.Join(homeDir, path[2:])
	}
	return path
}
