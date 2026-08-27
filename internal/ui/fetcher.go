package ui

import (
	"context"
	"fmt"

	"blocowallet/internal/wallet"

	tea "github.com/charmbracelet/bubbletea"
)

// Define uma mensagem que contém a contagem de wallets
type walletCountMsg struct {
	count int
	err   error
}

// Comando para buscar wallets e retornar a contagem
func walletCountCmd(service *wallet.WalletService, vault *wallet.WalletVault) tea.Cmd {
	return func() tea.Msg {
		if vault != nil {
			accounts, err := vault.ListAccounts(context.Background())
			if err != nil {
				return walletCountMsg{err: err}
			}
			return walletCountMsg{count: len(accounts)}
		}
		if service == nil {
			return walletCountMsg{err: fmt.Errorf("wallet catalog is unavailable")}
		}
		wallets, err := service.GetAllWallets()
		if err != nil {
			return walletCountMsg{err: err}
		}
		return walletCountMsg{count: len(wallets)}
	}
}
