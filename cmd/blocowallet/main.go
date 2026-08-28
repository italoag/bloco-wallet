package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"blocowallet/internal/blockchain"
	"blocowallet/internal/evm"
	"blocowallet/internal/storage"
	"blocowallet/internal/ui"
	"blocowallet/internal/wallet"
	"blocowallet/pkg/config"
	"blocowallet/pkg/localization"
	"blocowallet/pkg/logger"

	tea "github.com/charmbracelet/bubbletea"
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
	ui.ConfigureConfigurationManager(configManager)

	if len(os.Args) > 1 && os.Args[1] == "daemon" {
		runDaemon(cfg)
		return
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

	if err := ensurePrivateDirectory(cfg.AppDir, cfg.AppDir); err != nil {
		log.Printf("Failed to secure application directory: %v", err)
		os.Exit(1)
	}

	// Create wallet repository
	repo, err := storage.NewVaultRepository(cfg)
	if err != nil {
		log.Printf("Failed to create wallet repository: %v", err)
		os.Exit(1)
	}
	defer func() {
		if err := repo.Close(); err != nil {
			log.Printf("Error closing repository: %v", err)
		}
	}()

	codec, err := wallet.NewSecretEnvelopeCodec(wallet.ProductionArgon2idPolicy())
	if err != nil {
		log.Printf("Failed to initialize secret envelope: %v", err)
		os.Exit(1)
	}
	identityKey, err := loadOrCreateSourceIdentityKey(cfg.AppDir)
	if err != nil {
		log.Printf("Failed to initialize source identity key: %v", err)
		os.Exit(1)
	}
	vault, err := wallet.NewWalletVault(repo, codec, wallet.VaultOptions{SourceIdentityKey: identityKey})
	clear(identityKey)
	if err != nil {
		log.Printf("Failed to initialize wallet vault: %v", err)
		os.Exit(1)
	}
	defer vault.Close()
	lgr.Info("Wallet vault initialized")
	rpcGateway := blockchain.NewRPCGateway(blockchain.RPCGatewayOptions{AllowedLocalTargets: cfg.NetworkPolicy.AllowedLocalTargets})
	ui.ConfigureRPCGateway(rpcGateway)
	balanceProvider := blockchain.NewMultiProvider(rpcGateway, config.EnvironmentCredentialProvider{})
	defer balanceProvider.Close()
	softwareSigner, err := wallet.NewSoftwareSignerWithApprovalVerifier(vault, repo)
	if err != nil {
		log.Printf("Failed to initialize transaction signer: %v", err)
		os.Exit(1)
	}
	transactionSigner := configureExternalSigners(softwareSigner, repo)
	transactionAuthorizer, err := wallet.NewTransactionAuthorizer(vault, wallet.TransactionAuthorizationMode(cfg.Security.TransactionAuthorizationMode))
	if err != nil {
		log.Printf("Failed to initialize transaction authorizer: %v", err)
		os.Exit(1)
	}
	defer transactionAuthorizer.Close()

	// Initialize and start the TUI application
	app, err := ui.NewCLIModel(vault)
	if err != nil {
		log.Printf("Failed to initialize TUI: %v", err)
		os.Exit(1)
	}
	app.ConfigureBalanceProvider(balanceProvider, cfg)
	app.ConfigureHistoryReader(repo)
	app.ConfigureTransactionAuthorizer(transactionAuthorizer)
	app.ConfigureMessageSigningFactory(func(context.Context) (ui.MessageSigningService, error) {
		return evm.NewMessageSigningService(repo, transactionSigner, evm.MessageSigningOptions{ApprovalTTL: 2 * time.Minute})
	})
	configureWalletConnect(app, repo)
	createEngine := func(ctx context.Context, network config.Network) (*evm.Engine, error) {
		endpoint, err := network.ResolveRPCEndpoint(config.EnvironmentCredentialProvider{})
		if err != nil {
			return nil, err
		}
		session, err := rpcGateway.ValidateChain(ctx, endpoint, network.ChainID)
		if err != nil {
			return nil, err
		}
		rpc, err := blockchain.NewEVMRPC(rpcGateway, session)
		if err != nil {
			return nil, err
		}
		return evm.NewEngine(repo, rpc, transactionSigner, evm.EngineOptions{
			ReservationTTL: 5 * time.Minute,
			ApprovalTTL:    2 * time.Minute,
		})
	}
	app.ConfigureTransactionEngineFactory(func(ctx context.Context, network config.Network) (ui.TransactionEngine, error) {
		return createEngine(ctx, network)
	})
	recovery, err := evm.NewRecoverySupervisor(repo, func(ctx context.Context, chainID uint64) (evm.RecoveryTracker, error) {
		for _, network := range cfg.Networks {
			if network.IsActive && network.ChainID > 0 && uint64(network.ChainID) == chainID {
				return createEngine(ctx, network)
			}
		}
		return nil, fmt.Errorf("no active provider for recoverable chain")
	}, 2*time.Minute)
	if err != nil {
		log.Printf("Failed to initialize transaction recovery: %v", err)
		os.Exit(1)
	}
	recoveryContext, cancelRecovery := context.WithCancel(context.Background())
	recoveryDone := make(chan struct{})
	go func() {
		defer close(recoveryDone)
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			if err := recovery.RecoverOnce(recoveryContext, 100, time.Now().UTC()); err != nil && recoveryContext.Err() == nil {
				log.Printf("Transaction recovery pass failed: %v", err)
			}
			select {
			case <-recoveryContext.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	defer func() {
		cancelRecovery()
		<-recoveryDone
	}()
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
