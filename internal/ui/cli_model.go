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
	Service                         *wallet.WalletService
	Vault                           *wallet.WalletVault
	currentView                     string
	menuItems                       []menuItem
	selectedMenu                    int
	importWords                     []string
	importStage                     int
	textInputs                      []textinput.Model
	wallets                         []wallet.Wallet
	accounts                        []wallet.AccountSummary
	walletCount                     int
	selectedWallet                  *wallet.Wallet
	selectedAccount                 *wallet.AccountSummary
	deletingWallet                  *wallet.Wallet
	err                             error
	nameInput                       textinput.Model
	createWordCountInput            textinput.Model
	createLanguageInput             textinput.Model
	createPassphraseInput           textinput.Model
	createDerivationPathInput       textinput.Model
	createOptionsStage              int
	passwordInput                   textinput.Model
	createPasswordConfirmationInput textinput.Model
	createPasswordStage             int
	createPasswordError             string
	backupConfirmationInput         textinput.Model
	backupPathInput                 textinput.Model
	backupLanguageInput             textinput.Model
	backupPassphraseInput           textinput.Model
	backupMaterialStage             int
	backupWordAnswers               map[int]string
	backupError                     string
	backupChallenge                 *wallet.BackupChallenge
	pendingAccount                  *wallet.AccountSummary
	resumeBackupAccountID           string
	privateKeyInput                 textinput.Model
	currentPasswordInput            textinput.Model
	newPasswordInput                textinput.Model
	confirmPasswordInput            textinput.Model
	exportDestinationInput          textinput.Model
	vaultActionStage                int
	vaultExportEncrypted            bool
	vaultActionPreview              bool
	vaultActionError                string
	lastOperationNotice             string
	canonicalImport                 *canonicalImportState
	canonicalOperationID            uint64
	pendingImportMethod             wallet.ImportMethod
	keystorePath                    string
	mnemonic                        string
	walletTable                     table.Model
	width                           int
	height                          int
	walletDetails                   *wallet.WalletDetails
	styles                          Styles
	// fontsList         []string         // Lista de nomes de fontes carregadas do arquivo externo - currently unused
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
