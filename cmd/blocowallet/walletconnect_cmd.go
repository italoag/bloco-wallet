package main

import (
	"context"
	"log"
	"os"
	"time"

	"blocowallet/internal/storage"
	"blocowallet/internal/ui"
	"blocowallet/internal/walletconnect"
)

// configureWalletConnect wires the WalletConnect session store. The relay
// connects only when BLOCO_WALLET_WC_RELAY_URL is set; without a relay the
// TUI can still review and revoke persisted sessions.
func configureWalletConnect(app *ui.CLIModel, repo *storage.GORMRepository) {
	relayURL := os.Getenv("BLOCO_WALLET_WC_RELAY_URL")
	service, err := walletconnect.NewService(nil, repo, walletconnect.Options{})
	if err != nil {
		log.Printf("Failed to initialize WalletConnect: %v", err)
		return
	}
	if relayURL != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		relay, relayErr := walletconnect.NewRelayClient(ctx, relayURL)
		cancel()
		if relayErr != nil {
			log.Printf("WalletConnect relay unavailable: %v", relayErr)
		} else {
			service.AttachRelay(relay)
			log.Printf("WalletConnect relay connected")
		}
	}
	app.ConfigureWalletConnect(service, service)
}
