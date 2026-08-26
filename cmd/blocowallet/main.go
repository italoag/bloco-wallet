package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"

	"blocowallet/internal/storage"
	"blocowallet/internal/ui"
	"blocowallet/internal/wallet"
	"blocowallet/pkg/config"
	"blocowallet/pkg/localization"
	"blocowallet/pkg/logger"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/ethereum/go-ethereum/accounts/keystore"
)

var (
	// Version information - will be injected by the build process
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

func main() {
	// Print version information if requested
	if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "-v") {
		fmt.Printf("bloco-wallet-manager version %s\n", version)
		fmt.Printf("Git commit: %s\n", commit)
		fmt.Printf("Build date: %s\n", date)
		return
	}

	// Disable standard logger output to avoid terminal logs
	log.SetOutput(io.Discard)

	// Initialize configuration first to determine application directories
	configManager := config.NewConfigurationManager()
	cfg, err := configManager.LoadConfiguration()
	if err != nil {
		log.Printf("Failed to load configuration: %v", err)
		os.Exit(1)
	}

	// Initialize file-based logger (no terminal output)
	logDir := filepath.Join(cfg.AppDir, "logs")
	lgr, err := logger.NewFileLogger(logger.LoggingConfig{
		LogDir:      logDir,
		LogLevel:    "info",
		MaxFileSize: 25,
		MaxBackups:  3,
		MaxAge:      14,
	})
	if err != nil {
		// Fall back silently; continue without crashing per requirements
		lgr, _ = logger.NewFileLogger(logger.LoggingConfig{})
	}
	// Provide UI package with file-based logger for debug-only input logs
	ui.SetLogger(lgr)
	wallet.SetLogger(lgr)
	defer func() {
		if lgr != nil {
			_ = lgr.Sync()
		}
	}()

	// Initialize localization
	if err := localization.InitLocalization(cfg); err != nil {
		log.Printf("Failed to initialize localization: %v", err)
		os.Exit(1)
	}

	// Initialize crypto service
	wallet.InitCryptoService(cfg)
	lgr.Info("Crypto service initialized")

	// Create wallet repository
	repo, err := storage.NewWalletRepository(cfg)
	if err != nil {
		log.Printf("Failed to create wallet repository: %v", err)
		os.Exit(1)
	}
	defer func() {
		if err := repo.Close(); err != nil {
			log.Printf("Error closing repository: %v", err)
		}
	}()

	// Create keystore
	keystoreDir := cfg.WalletsDir
	if err := ensurePrivateDirectory(keystoreDir, cfg.AppDir); err != nil {
		log.Printf("Failed to secure keystore directory: %v", err)
		os.Exit(1)
	}

	ks := keystore.NewKeyStore(keystoreDir, keystore.StandardScryptN, keystore.StandardScryptP)

	// Initialize wallet service
	walletService := wallet.NewWalletService(repo, ks, keystoreDir)
	lgr.Info("Wallet service initialized")

	// Initialize and start the TUI application
	app := ui.NewCLIModel(walletService)
	p := tea.NewProgram(app, tea.WithAltScreen())

	lgr.Info("Starting application")
	if _, err := p.Run(); err != nil {
		log.Printf("Application error: %v", err)
		os.Exit(1)
	}
}

func ensurePrivateDirectory(path, appDir string) error {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		if err := os.MkdirAll(path, 0700); err != nil {
			return err
		}
		return os.Chmod(path, 0700)
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("path must be a regular directory")
	}
	if info.Mode().Perm()&0077 == 0 {
		return nil
	}
	root, err := filepath.Abs(appDir)
	if err != nil || appDir == "" {
		return fmt.Errorf("external directory is not private")
	}
	target, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	relative, err := filepath.Rel(root, target)
	if err != nil || filepath.IsAbs(relative) || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return fmt.Errorf("external directory is not private")
	}
	return os.Chmod(target, 0700)
}
