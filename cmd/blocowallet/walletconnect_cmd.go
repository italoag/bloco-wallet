package main

import (
	"context"
	"log"
	"os"
	"sync"
	"time"

	"blocowallet/internal/blockchain"
	"blocowallet/internal/storage"
	"blocowallet/internal/ui"
	"blocowallet/internal/walletconnect"
)

// configureWalletConnect wires the WalletConnect session store. The relay
// connects only when BLOCO_WALLET_WC_RELAY_URL is set; without a relay the
// TUI can still review and revoke persisted sessions.
func configureWalletConnect(app *ui.CLIModel, repo *storage.GORMRepository, gateway *blockchain.RPCGateway) func() {
	relayURL := os.Getenv("BLOCO_WALLET_WC_RELAY_URL")
	service, err := walletconnect.NewService(nil, repo, walletconnect.Options{})
	if err != nil {
		log.Printf("Failed to initialize WalletConnect: %v", err)
		return func() {}
	}
	service.OnProposal(app.WalletConnectProposalHandler())
	service.OnRequest(app.WalletConnectRequestHandler())
	app.ConfigureWalletConnect(service, service)
	if relayURL == "" {
		return func() {}
	}
	lifecycleContext, cancelLifecycle := context.WithCancel(context.Background())
	var wait sync.WaitGroup
	wait.Add(1)
	go func() {
		defer wait.Done()
		backoff := time.Second
		for lifecycleContext.Err() == nil {
			dialContext, cancelDial := context.WithTimeout(lifecycleContext, 15*time.Second)
			relay, relayErr := walletconnect.NewRelayClient(dialContext, relayURL, gateway)
			cancelDial()
			if relayErr == nil {
				service.AttachRelay(relay)
				for _, topic := range service.Topics() {
					subscribeContext, cancelSubscribe := context.WithTimeout(lifecycleContext, 15*time.Second)
					relayErr = relay.Subscribe(subscribeContext, topic)
					cancelSubscribe()
					if relayErr != nil {
						break
					}
				}
				if relayErr == nil {
					log.Printf("WalletConnect relay connected")
					backoff = time.Second
					service.Run(lifecycleContext)
				}
				if closeErr := relay.Close(); closeErr != nil {
					log.Printf("WalletConnect relay close failed: %v", closeErr)
				}
			} else if lifecycleContext.Err() == nil {
				log.Printf("WalletConnect relay unavailable: %v", relayErr)
			}
			if lifecycleContext.Err() != nil {
				return
			}
			timer := time.NewTimer(backoff)
			select {
			case <-lifecycleContext.Done():
				if !timer.Stop() {
					<-timer.C
				}
				return
			case <-timer.C:
			}
			if backoff < 30*time.Second {
				backoff *= 2
				if backoff > 30*time.Second {
					backoff = 30 * time.Second
				}
			}
		}
	}()
	return func() {
		cancelLifecycle()
		wait.Wait()
	}
}
