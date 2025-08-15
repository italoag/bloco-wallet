package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ConfigFileManager manages read and write operations on the configuration file
type ConfigFileManager struct {
	configPath string
}

// NewConfigFileManager creates a new instance of the manager
func NewConfigFileManager(configPath string) *ConfigFileManager {
	return &ConfigFileManager{
		configPath: configPath,
	}
}

// ReadConfig reads the current configuration file
func (cfm *ConfigFileManager) ReadConfig() ([]string, error) {
	// Check if the file exists
	if _, err := os.Stat(cfm.configPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("configuration file not found: %w", err)
	}

	// Read the file content
	content, err := os.ReadFile(cfm.configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read configuration file: %w", err)
	}

	// Split the content into lines
	lines := strings.Split(string(content), "\n")
	return lines, nil
}

// WriteConfig writes the updated content to the configuration file
func (cfm *ConfigFileManager) WriteConfig(lines []string) error {
	// Create a backup before writing
	backupPath, err := cfm.BackupConfig()
	if err != nil {
		return fmt.Errorf("failed to create backup: %w", err)
	}

	// Join the lines into a string
	content := strings.Join(lines, "\n")

	// Write to a temporary file first
	tempPath := cfm.configPath + ".tmp"
	err = os.WriteFile(tempPath, []byte(content), 0644)
	if err != nil {
		// Try to restore the backup in case of error
		_ = cfm.RestoreConfig(backupPath)
		return fmt.Errorf("failed to write temporary file: %w", err)
	}

	// Rename the temporary file to the configuration file
	err = os.Rename(tempPath, cfm.configPath)
	if err != nil {
		// Try to restore the backup in case of error
		_ = cfm.RestoreConfig(backupPath)
		return fmt.Errorf("failed to rename temporary file: %w", err)
	}

	return nil
}

// BackupConfig creates a backup of the configuration file
func (cfm *ConfigFileManager) BackupConfig() (string, error) {
	// Check if the file exists
	if _, err := os.Stat(cfm.configPath); os.IsNotExist(err) {
		return "", fmt.Errorf("configuration file not found: %w", err)
	}

	// Create backup file name with timestamp
	timestamp := time.Now().Format("20060102_150405")
	backupPath := fmt.Sprintf("%s.%s.bak", cfm.configPath, timestamp)

	// Copy the file to the backup
	content, err := os.ReadFile(cfm.configPath)
	if err != nil {
		return "", fmt.Errorf("failed to read file for backup: %w", err)
	}

	err = os.WriteFile(backupPath, content, 0644)
	if err != nil {
		return "", fmt.Errorf("failed to write backup file: %w", err)
	}

	return backupPath, nil
}

// RestoreConfig restores the configuration file from a backup
func (cfm *ConfigFileManager) RestoreConfig(backupPath string) error {
	// Check if the backup exists
	if _, err := os.Stat(backupPath); os.IsNotExist(err) {
		return fmt.Errorf("backup file not found: %w", err)
	}

	// Read the backup content
	content, err := os.ReadFile(backupPath)
	if err != nil {
		return fmt.Errorf("failed to read backup file: %w", err)
	}

	// Write the backup content to the configuration file
	err = os.WriteFile(cfm.configPath, content, 0644)
	if err != nil {
		return fmt.Errorf("failed to restore backup: %w", err)
	}

	return nil
}

// EnsureConfigDir ensures that the configuration directory exists
func (cfm *ConfigFileManager) EnsureConfigDir() error {
	dir := filepath.Dir(cfm.configPath)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		err = os.MkdirAll(dir, os.ModePerm)
		if err != nil {
			return fmt.Errorf("failed to create configuration directory: %w", err)
		}
	}
	return nil
}

// ValidateConfigFile checks if the configuration file is valid
func (cfm *ConfigFileManager) ValidateConfigFile() error {
	// Here we could implement a more complete validation of the TOML file
	// For now, we just check if the file exists and can be read
	_, err := cfm.ReadConfig()
	return err
}
