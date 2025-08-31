package ui

import (
	"blocowallet/internal/constants"
	"blocowallet/internal/wallet"
	"blocowallet/pkg/config"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/digitallyserviced/tdfgo/tdf"
)

type CLIModel struct {
	Service           *wallet.WalletService
	currentView       string
	menuItems         []menuItem
	selectedMenu      int
	importWords       []string
	importStage       int
	textInputs        []textinput.Model
	wallets           []wallet.Wallet
	walletCount       int
	selectedWallet    *wallet.Wallet
	deletingWallet    *wallet.Wallet
	err               error
	nameInput         textinput.Model
	passwordInput     textinput.Model
	privateKeyInput   textinput.Model
	mnemonic          string
	walletTable       table.Model
	width             int
	height            int
	walletDetails     *wallet.WalletDetails
	styles            Styles
	fontsList         []string         // Lista de nomes de fontes carregadas do arquivo externo
	selectedFont      *tdf.TheDrawFont // Fonte selecionada aleatoriamente
	fontInfo          *tdf.FontInfo    // Informação da fonte selecionada
	dialogButtonIndex int              // 0 = Confirmar, 1 = Cancelar
	currentConfig     *config.Config   // Configuração atual da aplicação

	// Network components
	networkListComponent NetworkListComponent // Componente de lista de redes
	addNetworkComponent  AddNetworkComponent  // Componente de adição de rede
	editingNetworkKey    string               // Chave da rede sendo editada

	// Enhanced import state
	enhancedImportState *EnhancedImportState
}

// GetEnhancedImportState returns the enhanced import state
func (m *CLIModel) GetEnhancedImportState() *EnhancedImportState {
	return m.enhancedImportState
}

// SetCurrentView sets the current view
func (m *CLIModel) SetCurrentView(view string) {
	m.currentView = view
}

// GetContentView returns the content view for the current view
func (m *CLIModel) GetContentView() string {
	switch m.currentView {
	case constants.EnhancedImportView:
		return m.viewEnhancedImport()
	default:
		return "Unknown view"
	}
}
