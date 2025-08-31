package ui

import (
	"blocowallet/internal/wallet"

	tea "github.com/charmbracelet/bubbletea"
)

// Define uma mensagem que contém a contagem de wallets
type walletCountMsg struct {
	count int
	err   error
}

// Comando para buscar wallets e retornar a contagem
func walletCountCmd(service *wallet.WalletService) tea.Cmd {
	return func() tea.Msg {
		wallets, err := service.GetAllWallets()
		if err != nil {
			return walletCountMsg{err: err}
		}
		return walletCountMsg{count: len(wallets)}
	}
}
