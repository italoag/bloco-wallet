package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

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

	// Initialize logger
	logger, err := logger.NewLogger("info")
	if err != nil {
		log.Fatalf("Failed to initialize logger: %v", err)
	}
	defer func() {
		if err := logger.Sync(); err != nil {
			// Sync errors are usually not critical, log them if needed
			log.Printf("Logger sync error: %v", err)
		}
	}()

	// Initialize configuration
	configManager := config.NewConfigurationManager()
	cfg, err := configManager.LoadConfiguration()
	if err != nil {
		log.Printf("Failed to load configuration: %v", err)
		os.Exit(1)
	}

	// Initialize localization
	if err := localization.InitLocalization(cfg); err != nil {
		log.Printf("Failed to initialize localization: %v", err)
		os.Exit(1)
	}

	// Initialize crypto service
	wallet.InitCryptoService(cfg)
	logger.Info("Crypto service initialized")

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
	keystoreDir := filepath.Join(cfg.WalletsDir, "keystore")
	if err := os.MkdirAll(keystoreDir, 0755); err != nil {
		log.Printf("Failed to create keystore directory: %v", err)
		os.Exit(1)
	}

	ks := keystore.NewKeyStore(keystoreDir, keystore.StandardScryptN, keystore.StandardScryptP)

	// Initialize wallet service
	walletService := wallet.NewWalletService(repo, ks)
	logger.Info("Wallet service initialized")

	// Initialize and start the TUI application
	app := ui.NewCLIModel(walletService)
	p := tea.NewProgram(app, tea.WithAltScreen())

	logger.Info("Starting application")
	if _, err := p.Run(); err != nil {
		log.Printf("Application error: %v", err)
		os.Exit(1)
	}
}
