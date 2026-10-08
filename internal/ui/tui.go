package ui

import (
	"blocowallet/internal/blockchain"
	"blocowallet/internal/constants"
	"blocowallet/internal/wallet"
	"blocowallet/pkg/config"
	"blocowallet/pkg/localization"
	"blocowallet/pkg/logger"
	"context"
	"fmt"
	"log"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/digitallyserviced/tdfgo/tdf"
	"github.com/ethereum/go-ethereum/common"
	"github.com/go-errors/errors"
)

// determineWalletType determines the wallet type display string based on ImportMethod as primary source
func determineWalletType(w wallet.Wallet) string {
	// Use ImportMethod as primary source of truth
	switch wallet.ImportMethod(w.ImportMethod) {
	case wallet.ImportMethodMnemonic:
		return localization.Get("imported_mnemonic")
	case wallet.ImportMethodPrivateKey:
		return localization.Get("imported_private_key")
	case wallet.ImportMethodKeystore:
		return localization.Get("imported_keystore")
	default:
		// Fallback to old logic for backward compatibility with wallets missing ImportMethod
		if w.Mnemonic == nil {
			return localization.Get("imported_private_key")
		}
		return localization.Get("imported_mnemonic")
	}
}

// Função para construir a lista de fontes disponíveis tanto do diretório customizado quanto das embutidas
func buildFontsList(customFontDir string) []*tdf.FontInfo {
	var fonts []*tdf.FontInfo

	// Primeiro, tenta adicionar fontes do diretório personalizado, se existir
	if customFontDir != "" {
		if _, err := os.Stat(customFontDir); err == nil {
			// Adicionar fontes do diretório personalizado
			files, err := os.ReadDir(customFontDir)
			if err == nil {
				for _, file := range files {
					if !file.IsDir() && strings.HasSuffix(strings.ToLower(file.Name()), ".tdf") {
						fontPath := filepath.Join(customFontDir, file.Name())
						fontInfo := tdf.NewFontInfo(file.Name(), fontPath)
						fontInfo.FontDir = customFontDir
						fonts = append(fonts, fontInfo)
					}
				}
			}
		}
	}

	// Se nenhuma fonte foi encontrada no diretório personalizado ou se ele não existe,
	// usar as fontes embutidas
	if len(fonts) == 0 {
		builtinFonts := tdf.SearchBuiltinFonts("*")
		fonts = append(fonts, builtinFonts...)
	}

	return fonts
}

type splashMsg struct{}
type clockTickMsg time.Time

type balanceFetchMsg struct {
	operationID uint64
	accountID   string
	balances    []blockchain.NetworkBalance
	failures    []error
}

func NewCLIModel(vault *wallet.WalletVault) (*CLIModel, error) {
	if vault == nil {
		return nil, fmt.Errorf("wallet vault is required")
	}
	model := &CLIModel{
		Vault:               vault,
		currentView:         constants.SplashView,
		menuItems:           NewMenu(),
		selectedMenu:        0,
		styles:              createStyles(),
		displayTime:         time.Now(),
		walletConnectEvents: make(chan tea.Msg, 64),
		localeLanguage:      localization.GetCurrentLanguage(),
	}

	if err := initializeFont(model); err != nil {
		return nil, err
	}

	return model, nil
}

func (m *CLIModel) ConfigureVersion(version string) {
	m.version = strings.TrimSpace(version)
}

func (m *CLIModel) displayVersion() string {
	if m.version == "" {
		return "dev"
	}
	return safeShort(m.version)
}

func (m *CLIModel) ConfigureBalanceProvider(provider *blockchain.MultiProvider, cfg *config.Config) {
	m.balanceProvider = provider
	m.balanceConfig = cfg
	m.currentConfig = cfg
	m.balanceConfigLoader = getConfigurationManager().LoadConfiguration
	m.clearBalanceState()
}

func (m *CLIModel) clearBalanceState() {
	if m.balanceCancel != nil {
		m.balanceCancel()
		m.balanceCancel = nil
	}
	m.balanceOperationID++
	m.networkBalances = nil
	m.balanceError = ""
	m.balanceLoading = false
}

func initializeFont(model *CLIModel) error {
	// Load configuration to get the proper app directory
	cfg, err := loadOrCreateConfig()
	if err != nil {
		return errors.Wrap(err, 0)
	}

	// Use the configured app directory instead of hardcoded path
	appDir := cfg.AppDir

	// Definir o diretório de fontes personalizado
	customFontDir := filepath.Join(appDir, "config", "fonts")

	// Verificar se o diretório de fontes personalizado existe
	if _, err := os.Stat(customFontDir); err != nil {
		// Se não existir, tentar criar o diretório
		if os.IsNotExist(err) {
			err = os.MkdirAll(customFontDir, 0700)
			if err != nil {
				if uiLogger != nil {
					uiLogger.Error("Failed to create custom fonts directory", logger.Error(err), logger.String("component", "fonts"), logger.String("dir", customFontDir))
				}
				// Continuar com as fontes embutidas
				customFontDir = ""
			}
		} else {
			if uiLogger != nil {
				uiLogger.Warn("Failed to stat custom fonts directory", logger.Error(err), logger.String("component", "fonts"), logger.String("dir", customFontDir))
			}
			customFontDir = ""
		}
	}

	// Semear o diretório do usuário com as fontes do repositório (arquivos
	// ausentes apenas, para não sobrescrever fontes modificadas pelo usuário)
	if customFontDir != "" {
		if written, err := seedUserFonts(customFontDir); err != nil {
			if uiLogger != nil {
				uiLogger.Warn("Failed to seed user fonts directory", logger.Error(err), logger.String("component", "fonts"), logger.String("dir", customFontDir))
			}
		} else if written > 0 && uiLogger != nil {
			uiLogger.Info("Seeded user fonts directory from embedded fonts", logger.String("component", "fonts"), logger.Int("count", written), logger.String("dir", customFontDir))
		}
	}

	// Construir a lista de fontes disponíveis (personalizadas + embutidas)
	availableFonts := buildFontsList(customFontDir)

	if len(availableFonts) == 0 {
		return errors.New("nenhuma fonte disponível, nem personalizada nem embutida")
	}

	// Carregar nomes das fontes configuradas
	configuredFontNames, err := loadFontsList(appDir)
	if err != nil {
		if uiLogger != nil {
			uiLogger.Error("Failed to load configured fonts list", logger.Error(err), logger.String("component", "fonts"))
		}
		// Se houver erro, escolher qualquer fonte disponível
		rand.NewSource(time.Now().UnixNano())
		selectedFontInfo := availableFonts[rand.Intn(len(availableFonts))]
		return loadSelectedFont(model, selectedFontInfo)
	}

	// Se não houver fontes configuradas, escolher qualquer fonte disponível
	if len(configuredFontNames) == 0 {
		if uiLogger != nil {
			uiLogger.Info("Configured fonts list is empty; selecting randomly", logger.String("component", "fonts"))
		}
		rand.NewSource(time.Now().UnixNano())
		selectedFontInfo := availableFonts[rand.Intn(len(availableFonts))]
		return loadSelectedFont(model, selectedFontInfo)
	}

	// Selecionar uma fonte da lista configurada
	selectedName, err := selectRandomFont(configuredFontNames)
	if err != nil {
		if uiLogger != nil {
			uiLogger.Error("Failed to randomly select a configured font", logger.Error(err), logger.String("component", "fonts"))
		}
		// Selecionar qualquer fonte disponível como fallback
		rand.NewSource(time.Now().UnixNano())
		selectedFontInfo := availableFonts[rand.Intn(len(availableFonts))]
		return loadSelectedFont(model, selectedFontInfo)
	}

	// Procurar a fonte selecionada nas fontes disponíveis
	var selectedFontInfo *tdf.FontInfo
	for _, fontInfo := range availableFonts {
		baseName := strings.TrimSuffix(fontInfo.File, ".tdf")
		if strings.EqualFold(baseName, selectedName) {
			selectedFontInfo = fontInfo
			break
		}
	}

	// Se não encontrada, usar qualquer fonte disponível como fallback
	if selectedFontInfo == nil {
		if uiLogger != nil {
			uiLogger.Warn("Configured font not found; using random fallback", logger.String("component", "fonts"), logger.String("selected_name", selectedName))
		}
		rand.NewSource(time.Now().UnixNano())
		selectedFontInfo = availableFonts[rand.Intn(len(availableFonts))]
	}

	return loadSelectedFont(model, selectedFontInfo)
}

// Função auxiliar para carregar a fonte selecionada
func loadSelectedFont(model *CLIModel, fontInfo *tdf.FontInfo) error {
	// Carregar a fonte selecionada
	fontFile, err := tdf.LoadFont(fontInfo)
	if err != nil {
		if uiLogger != nil {
			uiLogger.Error("Failed to load font", logger.Error(err), logger.String("component", "fonts"), logger.String("file", fontInfo.File))
		}
		return errors.Wrap(err, 0)
	}

	if len(fontFile.Fonts) == 0 {
		if uiLogger != nil {
			uiLogger.Warn("No fonts loaded from file", logger.String("component", "fonts"), logger.String("file", fontInfo.File))
		}
		return errors.New("nenhuma fonte carregada")
	}

	// Armazenar a informação da fonte selecionada no modelo
	model.selectedFont = &fontFile.Fonts[0]
	model.fontInfo = fontInfo

	if uiLogger != nil {
		uiLogger.Info("Font loaded successfully", logger.String("component", "fonts"), logger.String("file", fontInfo.File))
	}
	return nil
}

// loadFontsList returns the list of available fonts from the configuration
func loadFontsList(appDir string) ([]string, error) {
	// The fonts are now loaded from the main configuration
	// This function is kept for compatibility, but it's now a simple wrapper
	// that returns the fonts from the global configuration

	// Get the fonts from the global configuration
	cfg, err := loadOrCreateConfig()
	if err != nil {
		return nil, fmt.Errorf("erro ao carregar a configuração: %v", err)
	}

	return cfg.GetFontsList(), nil
}

func validateStoragePasswordInput(value string) error {
	if value == "" {
		return nil
	}
	password := []byte(value)
	err := wallet.ValidateStoragePassword(password)
	clear(password)
	if err != nil {
		return fmt.Errorf("")
	}
	return nil
}

func selectRandomFont(fonts []string) (string, error) {
	if len(fonts) == 0 {
		return "", fmt.Errorf("lista de fontes está vazia")
	}

	rand.NewSource(time.Now().UnixNano())
	index := rand.Intn(len(fonts))
	return fonts[index], nil
}

func (m *CLIModel) Init() tea.Cmd {
	return tea.Batch(
		splashCmd(),
		clockTickCmd(),
		walletCountCmd(m.Vault),
		waitForWalletConnectEvent(m.walletConnectEvents),
	)
}

func splashCmd() tea.Cmd {
	return tea.Tick(constants.SplashDuration, func(t time.Time) tea.Msg {
		return splashMsg{}
	})
}

func clockTickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return clockTickMsg(t)
	})
}

func (m *CLIModel) Update(msg tea.Msg) (updated tea.Model, command tea.Cmd) {
	defer func() {
		state := m.recovery
		if state == nil {
			return
		}
		if m.currentView == constants.RecoveryView && m.err == nil && m.selectedAccount != nil && m.selectedAccount.AccountID == state.account.AccountID {
			return
		}
		hadMaterial := state.material != nil
		m.clearRecovery()
		if hadMaterial {
			command = tea.Batch(command, tea.ClearScreen)
		}
	}()
	if msg == nil {
		return m, nil
	}
	if lang := localization.GetCurrentLanguage(); lang != m.localeLanguage {
		m.localeLanguage = lang
		m.refreshLocalizedUI()
	}
	switch message := msg.(type) {
	case walletConnectProposalMsg:
		m.walletConnectHandleProposal(message.proposal)
		return m, waitForWalletConnectEvent(m.walletConnectEvents)
	case walletConnectRequestMsg:
		m.walletConnectHandleRequest(message.session, message.params)
		return m, waitForWalletConnectEvent(m.walletConnectEvents)
	case credentialOpenedMsg:
		return m.handleCredentialOpened(message)
	case credentialOpDoneMsg:
		if m.credentialOperation == message.op {
			m.clearCredentialOperation()
		}
		return m, nil
	case vaultCreateResultMsg:
		return m.handleVaultCreateResult(message)
	case backupConfirmResultMsg:
		return m.handleBackupConfirmResult(message)
	case vaultActionResultMsg:
		return m.handleVaultActionResult(message)
	}
	if m.credentialPrompt != nil && (m.credentialPrompt.ownerView != m.currentView || m.credentialPrompt.owner != m.credentialOwner()) {
		m.clearCredentialPrompt()
	}
	if m.credentialPrompt != nil {
		if key, ok := msg.(tea.KeyMsg); ok && key.String() != "ctrl+q" {
			return m.updateCredentialPrompt(msg)
		}
	}
	if preparedMessage, ok := msg.(nativePreparedMsg); ok && preparedMessage.prepared != nil {
		if m.currentView != constants.NativeTransferView || m.nativeTransfer == nil || preparedMessage.generation != m.nativeTransfer.generation || m.nativeTransfer.phase != nativeTransferPreparing {
			return m, nativeCancelPreparedResultCommand(preparedMessage.engine, preparedMessage.prepared)
		}
	}
	if submittedMessage, ok := msg.(nativeSubmittedMsg); ok && submittedMessage.result.Hash != (common.Hash{}) {
		if m.currentView != constants.NativeTransferView || m.nativeTransfer == nil || submittedMessage.generation != m.nativeTransfer.generation || m.nativeTransfer.phase != nativeTransferSubmitting {
			m.transactionNotice = localization.T("transaction_submitted_notice", map[string]interface{}{"Hash": safeShort(submittedMessage.result.Hash.Hex())})
			if m.selectedAccount != nil {
				m.refreshWalletDetailsComponents()
			}
			return m, nil
		}
	}

	// Tratar as teclas de navegação global (esc/backspace) antes de qualquer outro processamento
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.String() {
		case "esc":
			if m.vaultBusy {
				m.cancelVaultWorker()
				return m, nil
			}
			if m.currentView == constants.RecoveryView && m.recovery != nil {
				return m.updateRecovery(msg)
			}
			if m.currentView == constants.CanonicalImportView {
				if m.canonicalImport != nil && m.canonicalImport.busy {
					if m.canonicalImport.cancel != nil {
						m.canonicalImport.cancel()
					}
					m.canonicalImport.cancelling = true
					return m, nil
				}
				m.clearCanonicalImport()
				m.initImportMethodSelection()
				return m, nil
			}
			if m.currentView == constants.RotatePasswordView || m.currentView == constants.ExportAccountView {
				m.clearVaultActionInputs()
				m.currentView = constants.WalletDetailsView
				return m, nil
			}
			if m.currentView == constants.KeePassSettingsView || m.currentView == constants.KeePassAccountView {
				if m.currentView == constants.KeePassSettingsView {
					if m.keepassSettings != nil && (m.keepassSettings.busy || m.keepassSettings.stage != keepassStageMenu) {
						return m.updateKeePassSettings(msg)
					}
					m.clearKeePassSettings()
					m.currentView = constants.ConfigurationView
					return m, nil
				}
				if m.keepassAccount != nil {
					return m.updateKeePassAccount(msg)
				}
				m.currentView = constants.WalletDetailsView
				return m, nil
			}
			if m.currentView == constants.NativeTransferView {
				cancelPrepared := nativeCancelPreparedCommand(m.nativeTransfer)
				m.clearNativeTransfer()
				m.currentView = constants.WalletDetailsView
				return m, cancelPrepared
			}
			if m.currentView == constants.AccountHistoryView {
				m.clearAccountHistory()
				m.currentView = constants.WalletDetailsView
				m.refreshWalletDetailsComponents()
				return m, nil
			}
			if m.currentView == constants.PersonalSignView {
				m.clearPersonalSign()
				m.currentView = constants.WalletDetailsView
				m.refreshWalletDetailsComponents()
				return m, nil
			}
			if m.currentView == constants.EIP712SignView {
				m.clearEIP712Sign()
				m.currentView = constants.WalletDetailsView
				m.refreshWalletDetailsComponents()
				return m, nil
			}
			if m.currentView == constants.AddNetworkView {
				if m.addNetworkComponent.cancelOperations != nil {
					m.addNetworkComponent.cancelOperations()
				}
				m.addNetworkComponent.searchGeneration++
				m.addNetworkComponent.adding = false
				m.editingNetworkKey = ""
				m.currentView = constants.NetworkMenuView
				return m, nil
			}
			if m.currentView == constants.ListWalletsView && m.accountDeletion != nil {
				return m.updateAccountDeletion(msg)
			}
			if m.currentView != constants.DefaultView && m.currentView != constants.SplashView {
				if m.currentView == constants.CreateWalletNameView || m.currentView == constants.CreateWalletOptionsView || m.currentView == constants.CreateWalletBackupView || m.currentView == constants.CreateWalletView {
					if err := m.cancelPendingVaultBackup(); err != nil {
						m.err = errors.Wrap(err, 0)
						return m, nil
					}
					m.passwordInput.SetValue("")
					m.createPassphraseInput.SetValue("")
					m.createPasswordConfirmationInput.SetValue("")
					m.createPasswordStage = 0
					m.createPasswordError = ""
					m.backupConfirmationInput.SetValue("")
					m.backupError = ""
				}
				// Para a maioria das telas, voltar para o menu principal
				if m.currentView == constants.WalletDetailsView {
					// Comportamento específico para tela de detalhes: voltar para lista de wallets
					m.clearBalanceState()
					m.walletDetails = nil
					m.selectedAccount = nil
					m.currentView = constants.ListWalletsView
				} else {
					// Comportamento padrão: voltar ao menu principal
					m.menuItems = NewMenu()
					m.selectedMenu = 0
					m.currentView = constants.DefaultView
				}
				// Sempre retorne imediatamente após processar a tecla de navegação
				return m, nil
			}
		case "ctrl+q":
			cancelPrepared := nativeCancelPreparedCommand(m.nativeTransfer)
			m.clearCredentialOperation()
			if m.keepassSettings != nil {
				if m.keepassSettings.busy {
					if m.keepassSettings.cancel != nil {
						m.keepassSettings.cancel()
					}
					m.keepassSettings.cancelling = true
					m.keepassSettings.quitAfterResult = true
					return m, nil
				}
				m.clearKeePassSettings()
				m.currentView = constants.ConfigurationView
			}
			if m.keepassAccount != nil {
				if m.keepassAccount.busy {
					if m.keepassAccount.cancel != nil {
						m.keepassAccount.cancel()
					}
					m.keepassAccount.quitAfterResult = true
					return m, nil
				}
				m.keepassAccount = nil
			}
			if m.vaultBusy {
				m.cancelVaultWorker()
				m.vaultQuitAfterResult = true
				return m, nil
			}
			m.clearBalanceState()
			m.clearNativeTransfer()
			if m.accountDeletion != nil {
				if m.accountDeletion.busy {
					if m.accountDeletion.cancel != nil {
						m.accountDeletion.cancel()
					}
					m.accountDeletion.cancelling = true
					m.accountDeletion.quitAfterResult = true
					return m, nil
				}
				m.clearAccountDeletion()
			}
			if m.recovery != nil {
				if m.recovery.busy {
					if m.recovery.cancel != nil {
						m.recovery.cancel()
					}
					m.recovery.cancelling = true
					m.recovery.quitAfterResult = true
					return m, nil
				}
				m.clearRecovery()
			}
			if m.currentView == constants.CanonicalImportView && m.canonicalImport != nil && m.canonicalImport.busy {
				if m.canonicalImport.cancel != nil {
					m.canonicalImport.cancel()
				}
				m.canonicalImport.cancelling = true
				m.canonicalImport.quitAfterResult = true
				return m, nil
			}
			if err := m.cancelPendingVaultBackup(); err != nil {
				m.err = errors.Wrap(err, 0)
				return m, nil
			}
			m.passwordInput.SetValue("")
			m.createPassphraseInput.SetValue("")
			m.createPasswordConfirmationInput.SetValue("")
			m.backupConfirmationInput.SetValue("")
			m.clearImportSecrets()
			m.clearCanonicalImport()
			m.clearAccountHistory()
			m.clearPersonalSign()
			m.clearEIP712Sign()
			m.clearVaultActionInputs()
			if cancelPrepared != nil {
				return m, tea.Sequence(cancelPrepared, tea.Quit)
			}
			return m, tea.Quit
		}
	}

	switch msg := msg.(type) {
	case clockTickMsg:
		m.displayTime = time.Time(msg)
		if now := time.Now(); now.After(m.displayTime) {
			m.displayTime = now
		}
		if m.recovery != nil && m.recovery.stage == recoveryStageRevealed && (m.recovery.expiresAt.IsZero() || !m.displayTime.Before(m.recovery.expiresAt)) {
			m.recoveryPrivacyWipe(localization.Get("recovery_status_expired"))
			return m, tea.Batch(clockTickCmd(), tea.ClearScreen)
		}
		return m, clockTickCmd()
	case balanceFetchMsg:
		if msg.operationID != m.balanceOperationID || m.selectedAccount == nil || msg.accountID != m.selectedAccount.AccountID {
			return m, nil
		}
		m.balanceLoading = false
		if m.balanceCancel != nil {
			m.balanceCancel()
			m.balanceCancel = nil
		}
		m.networkBalances = msg.balances
		if len(msg.failures) > 0 {
			messages := make([]string, 0, len(msg.failures))
			for _, failure := range msg.failures {
				messages = append(messages, safeError(failure))
			}
			m.balanceError = strings.Join(messages, "; ")
		} else if len(msg.balances) == 0 {
			m.balanceError = localization.Get("balance_no_networks")
		} else {
			m.balanceError = ""
		}
		m.refreshWalletDetailsComponents()
		return m, nil
	case canonicalPreviewResultMsg:
		if m.currentView == constants.CanonicalImportView && m.canonicalImport != nil {
			return m.updateCanonicalImport(msg)
		}
		clear(msg.data)
		clearCanonicalBatchItems(msg.batchItems)
		clearCanonicalMnemonicItems(msg.mnemonicItems)
		return m, nil
	case canonicalCommitResultMsg:
		if m.currentView == constants.CanonicalImportView && m.canonicalImport != nil {
			return m.updateCanonicalImport(msg)
		}
		m.discardCredentialOperation(msg.op)
		if msg.err == nil || msg.backupPending {
			notice := localization.Get("canonical_import_committed_notice")
			if msg.backupPending {
				notice += " — " + localization.Get("keepass_backup_pending_notice")
			}
			m.lastOperationNotice = notice
			return m, m.refreshWalletsTable()
		}
		return m, nil
	case canonicalSourcePasswordMsg:
		if m.currentView == constants.CanonicalImportView && m.canonicalImport != nil {
			return m.updateCanonicalImport(msg)
		}
		clear(msg.password)
		return m, nil
	case accountDeletedMsg:
		if m.accountDeletion == nil || msg.operationID != m.accountDeletion.operationID {
			return m, nil
		}
		return m.updateAccountDeletion(msg)
	case recoveryResultMsg:
		if m.currentView != constants.RecoveryView || m.recovery == nil || !m.recovery.busy || msg.operationID != m.recovery.operationID || msg.accountID != m.recovery.account.AccountID {
			if msg.material != nil {
				msg.material.Destroy()
			}
			if msg.export && (msg.err == nil || wallet.IsExportCommitted(msg.err)) {
				m.lastOperationNotice = localization.T("recovery_notice_export_created", map[string]interface{}{"Path": safeInline(msg.destination)})
			}
			return m, nil
		}
		return m.updateRecovery(msg)
	case recoveryExpiredMsg:
		if m.currentView != constants.RecoveryView || m.recovery == nil {
			return m, nil
		}
		return m.updateRecovery(msg)
	case tea.BlurMsg:
		m.clearCredentialOperation()
		if m.keepassSettings != nil {
			if m.keepassSettings.busy && m.keepassSettings.cancel != nil {
				m.keepassSettings.cancel()
				m.keepassSettings.cancelling = true
			}
			m.wipeKeePassSettingsSecrets()
			m.keepassSettings.retryPathInput.SetValue("")
			m.keepassSettings.pathInput.Blur()
			m.keepassSettings.masterInput.Blur()
			m.keepassSettings.confirmInput.Blur()
			m.keepassSettings.consentInput.Blur()
			m.keepassSettings.retryPathInput.Blur()
		}
		if m.keepassAccount != nil {
			if m.keepassAccount.busy && m.keepassAccount.cancel != nil {
				m.keepassAccount.cancel()
			}
			m.keepassAccount.password.SetValue("")
			m.keepassAccount.password.Blur()
		}
		if m.accountDeletion != nil {
			m.accountDeletion.password.SetValue("")
		}
		if m.canonicalImport != nil && m.canonicalImport.busy && m.canonicalImport.cancel != nil {
			m.canonicalImport.cancel()
			m.canonicalImport.cancelling = true
		}
		if m.vaultBusy && m.vaultCancel != nil {
			m.vaultCancel()
			m.vaultBusyCancelling = true
		}
		if m.Vault != nil && m.backupChallenge != nil && !m.vaultBusy {
			m.suspendPendingVaultBackup()
		}
		if m.recovery != nil {
			m.recoveryPrivacyWipe(localization.Get("focus_lost_hidden"))
			return m, tea.ClearScreen
		}
		return m, nil
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		if m.networkListComponent.id != "" {
			m.networkListComponent.SetSize(m.width, m.height)
		}
		if m.addNetworkComponent.id != "" {
			m.addNetworkComponent.SetSize(m.width, m.height)
		}
		if len(m.createOptionList.Items()) > 0 {
			m.createOptionList.SetSize(max(44, min(76, m.width-8)), max(8, min(16, m.height-14)))
		}
		if m.selectedAccount != nil {
			m.refreshWalletDetailsComponents()
		}

		// Atualizar estilos com novas dimensões
		m.styles.Header = m.styles.Header.Width(m.width)
		m.styles.Content = m.styles.Content.Width(m.width)
		m.styles.Footer = m.styles.Footer.Width(m.width)

		// Atualizar dimensões da tabela
		if m.currentView == constants.ListWalletsView {
			m.updateTableDimensions()
		}
		if m.recovery != nil {
			m.recoveryResizeInputs()
			if m.width < 80 || m.height < 24 {
				m.recoveryPrivacyWipe(localization.Get("too_small_hidden"))
				return m, tea.ClearScreen
			}
		}
		return m, nil

	case walletsRefreshedMsg:
		if msg.err != nil {
			m.err = errors.Wrap(msg.err, 0)
			return m, nil
		}
		if m.Vault != nil {
			m.applyAccountList(msg.accounts)
			return m, nil
		}
		m.wallets = msg.wallets
		m.walletCount = len(msg.wallets)
		if len(m.wallets) > 0 {
			m.rebuildWalletsTable()
		}
		return m, nil

	case splashMsg:
		// Transitar para o menu principal após a splash screen
		m.currentView = constants.DefaultView
		// Iniciar o comando para buscar a quantidade de wallets
		return m, walletCountCmd(m.Vault)
	case walletCountMsg:
		if msg.err != nil {
			m.err = msg.err
			log.Println("Erro ao buscar a quantidade de wallets:", msg.err)
		} else {
			m.walletCount = msg.count
		}
		return m, nil
	}

	if m.err != nil {
		if _, ok := msg.(tea.KeyMsg); ok {
			m.err = nil
			m.currentView = constants.DefaultView
		}
		return m, nil
	}

	// Processamento específico para cada tela
	switch m.currentView {
	case constants.SplashView:
		// Nenhuma atualização adicional necessária durante a splash screen
		return m, nil
	case constants.DefaultView:
		return m.updateMenu(msg)
	case constants.CreateWalletNameView:
		return m.updateCreateWalletName(msg)
	case constants.CreateWalletOptionsView:
		return m.updateCreateWalletOptions(msg)
	case constants.CreateWalletBackupView:
		return m.updateCreateWalletBackup(msg)
	case constants.CreateWalletView:
		return m.updateCreateWalletPassword(msg)
	case constants.ImportMethodSelectionView:
		return m.updateImportMethodSelection(msg)
	case constants.CanonicalImportView:
		return m.updateCanonicalImport(msg)
	case constants.ListWalletsView:
		return m.updateListWallets(msg)
	case constants.WalletDetailsView:
		return m.updateWalletDetails(msg)
	case constants.AccountHistoryView:
		return m.updateAccountHistory(msg)
	case constants.PersonalSignView:
		return m.updatePersonalSign(msg)
	case constants.EIP712SignView:
		return m.updateEIP712Sign(msg)
	case constants.ContractCallView:
		return m.updateContractCall(msg)
	case constants.WalletConnectView:
		return m.updateWalletConnect(msg)
	case constants.FIDO2View:
		return m.updateFIDO2(msg)
	case constants.NativeTransferView:
		return m.updateNativeTransfer(msg)
	case constants.RotatePasswordView:
		return m.updateVaultAction(msg, false)
	case constants.ExportAccountView:
		return m.updateVaultAction(msg, true)
	case constants.RecoveryView:
		return m.updateRecovery(msg)
	case constants.ConfigurationView:
		return m.updateConfigMenu(msg)
	case constants.LanguageSelectionView:
		return m.updateLanguageSelection(msg)
	case constants.NetworkMenuView:
		return m.updateNetworkMenu(msg)
	case constants.NetworkListView:
		return m.updateNetworkList(msg)
	case constants.AddNetworkView:
		return m.updateAddNetwork(msg)
	case constants.SafeView:
		return m.updateSafeView(msg)
	case constants.KeePassSettingsView:
		return m.updateKeePassSettings(msg)
	case constants.KeePassAccountView:
		return m.updateKeePassAccount(msg)
	default:
		m.currentView = constants.DefaultView
		return m, nil
	}
}

func (m *CLIModel) View() string {
	if m.err != nil {
		label := safeShort(localization.Get("error_title"))
		return m.styles.ErrorStyle.Render(label + ": " + safeError(m.err))
	}

	if m.credentialPrompt != nil {
		return m.renderMainView()
	}
	switch m.currentView {
	case constants.SplashView:
		return m.renderSplash()
	case constants.ListWalletsView:
		return m.renderListWalletsWithLayout()
	case constants.RecoveryView:
		return m.viewRecovery()
	default:
		return m.renderMainView()
	}
}

// renderListWalletsWithLayout renderiza a tela de listagem de carteiras com o layout completo
func (m *CLIModel) renderListWalletsWithLayout() string {
	if m.width < 80 || m.height < 16 {
		return m.renderTerminalSizeHint(80, 16)
	}
	var headerContent string
	if m.width < 120 || m.height < 32 {
		headerContent = "BLOCO Wallet | " + localization.T("version_label", map[string]interface{}{"Version": m.displayVersion()})
	} else {
		// Renderizar o cabeçalho da mesma forma que renderMainView
		renderedLogo := renderHeaderLogo()

		headerLeft := lipgloss.JoinVertical(
			lipgloss.Left,
			renderedLogo,
			localization.T("version_label", map[string]interface{}{"Version": m.displayVersion()}),
		)

		menuItems := m.renderMenuItems()
		menuGrid := lipgloss.JoinVertical(lipgloss.Left, menuItems...)

		// Montar header
		headerGap := m.width - lipgloss.Width(headerLeft) - lipgloss.Width(menuGrid) - m.styles.Header.GetHorizontalFrameSize()
		headerContent = lipgloss.JoinVertical(lipgloss.Left, headerLeft, menuGrid)
		if headerGap >= 2 {
			headerContent = lipgloss.JoinHorizontal(lipgloss.Top, headerLeft, lipgloss.NewStyle().Width(headerGap).Render(""), menuGrid)
		}
	}

	// Renderizar header com altura fixa
	renderedHeader := m.styles.Header.Render(headerContent)
	headerHeight := lipgloss.Height(renderedHeader)

	// Preparar conteúdo do footer
	renderedFooter := m.renderStatusBar()
	footerHeight := lipgloss.Height(renderedFooter)

	// Calcular altura disponível para o conteúdo
	contentHeight := m.height - headerHeight - footerHeight - 2
	if contentHeight <= 0 {
		return m.renderCompactTerminal()
	}

	// Obter conteúdo da visualização de carteiras
	m.fitMainContent(contentHeight)
	content := m.viewListWallets()

	// Renderizar o conteúdo na área apropriada
	renderedContent := m.styles.Content.Height(contentHeight).MaxHeight(contentHeight).Render(content)

	// Inserir espaço vazio para empurrar o footer para baixo
	remainingHeight := m.height - headerHeight - lipgloss.Height(renderedContent) - footerHeight
	if remainingHeight < 0 {
		remainingHeight = 0
	}
	emptySpace := lipgloss.NewStyle().Height(remainingHeight).Render("")

	// Montar a visualização final
	finalView := lipgloss.JoinVertical(
		lipgloss.Top,
		renderedHeader,
		renderedContent,
		emptySpace,
		renderedFooter,
	)

	return finalView
}

func (m *CLIModel) renderMenuItems() []string {
	menuItems := make([]string, 0, len(m.menuItems))
	for i, item := range m.menuItems {
		if i == m.selectedMenu {
			title := m.styles.SelectedTitle.Render("▸ " + safeInline(item.title))
			desc := m.styles.MenuDesc.Render(safeInline(item.description))
			menuItems = append(menuItems, m.styles.MenuSelected.Render(fmt.Sprintf("%s\n%s", title, desc)))
			continue
		}
		title := m.styles.MenuTitle.Render("  " + safeShort(item.title))
		desc := m.styles.MenuDesc.Render(safeInline(item.description))
		menuItems = append(menuItems, m.styles.MenuItem.Render(fmt.Sprintf("%s\n%s", title, desc)))
	}

	numRows := (len(menuItems) + 1) / 2
	var menuRows []string
	for i := 0; i < numRows; i++ {
		startIndex := i * 2
		endIndex := startIndex + 2
		if endIndex > len(menuItems) {
			endIndex = len(menuItems)
		}
		row := lipgloss.JoinHorizontal(lipgloss.Top, menuItems[startIndex:endIndex]...)
		menuRows = append(menuRows, row)
	}
	return menuRows
}

func (m *CLIModel) getContentView() string {
	if m.credentialPrompt != nil {
		return m.viewCredentialPrompt()
	}
	switch m.currentView {
	case constants.DefaultView:
		return localization.Get("welcome_message")
	case constants.CreateWalletNameView:
		return m.viewCreateWalletName()
	case constants.CreateWalletOptionsView:
		return m.viewCreateWalletOptions()
	case constants.CreateWalletBackupView:
		return m.viewCreateWalletBackup()
	case constants.CreateWalletView:
		return m.viewCreateWalletPassword()
	case constants.ImportMethodSelectionView:
		return m.viewImportMethodSelection()
	case constants.CanonicalImportView:
		return m.viewCanonicalImport()
	case constants.ListWalletsView:
		return m.viewListWallets()
	case constants.WalletDetailsView:
		return m.viewWalletDetails()
	case constants.AccountHistoryView:
		return m.viewAccountHistory()
	case constants.PersonalSignView:
		return m.viewPersonalSign()
	case constants.EIP712SignView:
		return m.viewEIP712Sign()
	case constants.ContractCallView:
		return m.viewContractCall()
	case constants.WalletConnectView:
		return m.viewWalletConnect()
	case constants.FIDO2View:
		return m.viewFIDO2()
	case constants.NativeTransferView:
		return m.viewNativeTransfer()
	case constants.RotatePasswordView:
		return m.viewVaultAction(false)
	case constants.ExportAccountView:
		return m.viewVaultAction(true)
	case constants.ConfigurationView:
		return m.viewConfigMenu()
	case constants.LanguageSelectionView:
		return m.viewLanguageSelection()
	case constants.NetworkMenuView:
		return m.viewNetworkMenu()
	case constants.NetworkListView:
		return m.viewNetworkList()
	case constants.AddNetworkView:
		return m.viewAddNetwork()
	case constants.SafeView:
		return m.viewSafe()
	case constants.KeePassSettingsView:
		return m.viewKeePassSettings()
	case constants.KeePassAccountView:
		return m.viewKeePassAccount()
	default:
		return localization.Get("unknown_state")
	}
}

func (m *CLIModel) updateMenu(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if m.selectedMenu > 0 {
				m.selectedMenu--
			}
		case "down", "j":
			if m.selectedMenu < len(m.menuItems)-1 {
				m.selectedMenu++
			}
		case "left", "h":
			if m.selectedMenu > 1 {
				m.selectedMenu -= 2
			}
		case "right", "l":
			if m.selectedMenu < len(m.menuItems)-2 {
				m.selectedMenu += 2
			}
		case "enter":
			switch m.menuItems[m.selectedMenu].action {
			case "create_wallet":
				m.initCreateWallet()
			case "import_wallet":
				m.initImportWallet()
			case "list_wallets":
				m.initListWallets()
			case "safe":
				m.initSafeView()
			case "configuration":
				m.initConfigMenu()
			case "exit":
				return m, tea.Quit
			}
		case tea.KeyCtrlX.String(), "q":
			return m, tea.Quit
		case "esc":
			// Voltar para o menu principal
			m.menuItems = NewMenu() // Recarregar o menu principal
			m.selectedMenu = 0      // Resetar a seleção
			m.currentView = constants.DefaultView
		}
	}
	return m, nil
}

func (m *CLIModel) updateCreateWalletName(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "enter":
			name := strings.TrimSpace(m.nameInput.Value())
			if name == "" {
				m.err = errors.Wrap(fmt.Errorf("%s", localization.Get("wallet_name_empty_err")), 0)
				if wrappedErr, ok := m.err.(*errors.Error); ok {
					log.Println(wrappedErr.ErrorStack())
				} else {
					log.Println("Error:", m.err)
				}
				m.currentView = constants.DefaultView
				return m, nil
			}
			if m.Vault != nil {
				m.createOptionsStage = 0
				m.configureCreateOptionList(0)
				m.currentView = constants.CreateWalletOptionsView
				return m, nil
			}
			// Proceed to backup confirmation
			m.backupConfirmationInput.Focus()
			m.currentView = constants.CreateWalletBackupView
			return m, nil
		case "esc":
			// Reset the name input field and go back to menu
			m.nameInput = textinput.New()
			m.currentView = constants.DefaultView
		default:
			var cmd tea.Cmd
			m.nameInput, cmd = m.nameInput.Update(msg)
			return m, cmd
		}
	}
	return m, nil
}

func (m *CLIModel) updateCreateWalletOptions(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.createOptionsStage == 2 {
		if keyMessage, ok := msg.(tea.KeyMsg); ok && keyMessage.String() == "enter" {
			m.createPassphraseInput.Blur()
			m.createOptionsStage = 3
			m.createCustomPath = false
			m.configureCreateOptionList(3)
			m.createPasswordError = ""
			return m, nil
		}
		var command tea.Cmd
		m.createPassphraseInput, command = m.createPassphraseInput.Update(msg)
		return m, command
	}
	if m.createOptionsStage == 3 && m.createCustomPath {
		if keyMessage, ok := msg.(tea.KeyMsg); ok && keyMessage.String() == "enter" {
			path, err := wallet.ParseDerivationPath(m.createDerivationPathInput.Value())
			if err != nil {
				m.createPasswordError = err.Error()
				return m, nil
			}
			m.createDerivationPathInput.SetValue(path.String())
			m.createDerivationPathInput.Blur()
			m.passwordInput.Focus()
			m.createPasswordError = ""
			m.currentView = constants.CreateWalletView
			return m, nil
		}
		var command tea.Cmd
		m.createDerivationPathInput, command = m.createDerivationPathInput.Update(msg)
		return m, command
	}
	if keyMessage, ok := msg.(tea.KeyMsg); ok && keyMessage.String() == "enter" {
		selected, ok := m.createOptionList.SelectedItem().(createOptionItem)
		if !ok {
			m.createPasswordError = localization.Get("create_select_required")
			return m, nil
		}
		switch m.createOptionsStage {
		case 0:
			m.createWordCountInput.SetValue(selected.value)
			m.createOptionsStage = 1
			m.configureCreateOptionList(1)
		case 1:
			m.createLanguageInput.SetValue(selected.value)
			m.createOptionsStage = 2
			m.createPassphraseInput.Focus()
		case 3:
			if selected.value == "custom" {
				m.createCustomPath = true
				m.createDerivationPathInput.SetValue("")
				m.createDerivationPathInput.Focus()
				m.createPasswordError = ""
				return m, nil
			}
			m.createDerivationPathInput.SetValue(selected.value)
			m.passwordInput.Focus()
			m.createPasswordError = ""
			m.currentView = constants.CreateWalletView
			return m, nil
		}
		m.createPasswordError = ""
		return m, nil
	}
	var command tea.Cmd
	m.createOptionList, command = m.createOptionList.Update(msg)
	return m, command
}

func (m *CLIModel) updateCreateWalletBackup(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.vaultBusy {
		if key, ok := msg.(tea.KeyMsg); ok && key.String() == "esc" {
			m.cancelVaultWorker()
		}
		return m, nil
	}
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "enter":
			if m.Vault != nil {
				if m.backupChallenge == nil {
					m.err = errors.Wrap(wallet.ErrBackupChallengeNotFound, 0)
					m.currentView = constants.DefaultView
					return m, nil
				}
				needsOp := m.credentialBackupEnabled() && (m.credentialOperation == nil || m.credentialOperation.Context().Err() != nil)
				if needsOp {
					accountID := m.backupChallenge.AccountID
					m.suspendPendingVaultBackup()
					if m.backupChallenge != nil {
						return m, nil
					}
					m.clearCredentialOperation()
					m.initResumeBackup(accountID)
					m.createPasswordError = localization.Get("keepass_confirmation_reauth")
					return m, nil
				}
				return m, m.startVaultConfirmBackup()
			}
			m.err = errors.Wrap(fmt.Errorf("wallet vault is required"), 0)
			m.currentView = constants.DefaultView
			return m, nil
		case "esc":
			if err := m.cancelPendingVaultBackup(); err != nil {
				m.backupError = err.Error()
				return m, nil
			}
			m.backupConfirmationInput.SetValue("")
			m.currentView = constants.DefaultView
			return m, nil
		default:
			var cmd tea.Cmd
			switch m.backupMaterialStage {
			case 0:
				m.backupConfirmationInput, cmd = m.backupConfirmationInput.Update(msg)
			case 1:
				m.backupPathInput, cmd = m.backupPathInput.Update(msg)
			case 2:
				m.backupLanguageInput, cmd = m.backupLanguageInput.Update(msg)
			case 3:
				m.backupPassphraseInput, cmd = m.backupPassphraseInput.Update(msg)
			}
			return m, cmd
		}
	}
	return m, nil
}

func (m *CLIModel) updateCreateWalletPassword(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.vaultBusy {
		if key, ok := msg.(tea.KeyMsg); ok && key.String() == "esc" {
			m.cancelVaultWorker()
		}
		return m, nil
	}
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "enter":
			password := m.passwordInput.Value()
			passwordValidation := []byte(password)
			validationErr := wallet.ValidateStoragePassword(passwordValidation)
			clear(passwordValidation)
			if validationErr != nil {
				m.passwordInput.SetValue("")
				m.err = errors.Wrap(validationErr, 0)
				log.Println(m.err.(*errors.Error).ErrorStack())
				return m, nil
			}
			if m.Vault != nil && m.resumeBackupAccountID != "" {
				needsOp := m.credentialBackupEnabled()
				return m, m.submitWithCredential(needsOp, func() tea.Cmd { return m.startVaultCreate() })
			}
			if m.Vault != nil && m.createPasswordStage == 0 {
				m.passwordInput.Blur()
				m.createPasswordConfirmationInput.Focus()
				m.createPasswordStage = 1
				return m, nil
			}
			if m.Vault != nil && !wallet.SecureCompare(password, m.createPasswordConfirmationInput.Value()) {
				m.createPasswordError = localization.Get("password_confirm_mismatch")
				m.passwordInput.SetValue("")
				m.createPasswordConfirmationInput.SetValue("")
				m.createPasswordConfirmationInput.Blur()
				m.passwordInput.Focus()
				m.createPasswordStage = 0
				return m, nil
			}

			name := strings.TrimSpace(m.nameInput.Value())
			if m.Vault != nil {
				if name == "" {
					m.err = errors.Wrap(errors.New(localization.Get("all_words_required")), 0)
					return m, nil
				}
				needsOp := m.credentialBackupEnabled()
				return m, m.submitWithCredential(needsOp, func() tea.Cmd { return m.startVaultCreate() })
			}
			m.err = errors.Wrap(fmt.Errorf("wallet vault is required"), 0)
			m.currentView = constants.DefaultView
			return m, nil
		case "esc":
			// Go back to name input
			m.nameInput.Focus()
			m.currentView = constants.CreateWalletNameView
			return m, nil
		default:
			var cmd tea.Cmd
			if m.Vault != nil && m.createPasswordStage == 1 {
				m.createPasswordConfirmationInput, cmd = m.createPasswordConfirmationInput.Update(msg)
			} else {
				m.passwordInput, cmd = m.passwordInput.Update(msg)
			}
			return m, cmd
		}
	}
	return m, nil
}

func (m *CLIModel) updateImportMethodSelection(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Criar o menu de importação
	importMenu := NewImportMenu()

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if m.selectedMenu > 0 {
				m.selectedMenu--
			}
		case "down", "j":
			if m.selectedMenu < len(importMenu)-1 {
				m.selectedMenu++
			}
		case "enter":
			switch importMenu[m.selectedMenu].action {
			case "mnemonic":
				m.initCanonicalImport(wallet.ImportMethodMnemonic)
			case "private_key":
				m.initCanonicalImport(wallet.ImportMethodPrivateKey)
			case "keystore":
				m.initCanonicalImport(wallet.ImportMethodKeystore)
			case "keystore_batch":
				m.initCanonicalBatchImport()
			case "mnemonic_batch":
				m.initCanonicalImport(canonicalMnemonicBatchMethod)
			case "private_key_batch":
				m.initCanonicalImport(canonicalPrivateKeyBatchMethod)
			case "bloco_encrypted":
				m.initCanonicalImport(canonicalEncryptedMethod)
			case "watch_only":
				m.initCanonicalImport(wallet.ImportMethodWatchOnly)
			case "back":
				m.clearImportSecrets()
				m.menuItems = NewMenu()
				m.selectedMenu = 0
				m.currentView = constants.DefaultView
			}
		case "esc":
			m.menuItems = NewMenu() // Recarregar o menu principal
			m.selectedMenu = 0      // Resetar a seleção
			m.currentView = constants.DefaultView
		}
	}
	return m, nil
}

func (m *CLIModel) updateConfigMenu(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Criar o menu de configuração
	configMenu := NewConfigMenu()

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if m.selectedMenu > 0 {
				m.selectedMenu--
			}
		case "down", "j":
			if m.selectedMenu < len(configMenu)-1 {
				m.selectedMenu++
			}
		case "enter":
			// Usar o menu de configuração para determinar a ação baseada na seleção
			switch configMenu[m.selectedMenu].action {
			case "networks":
				// Mostrar o submenu de redes
				m.menuItems = NewNetworkMenu()
				m.selectedMenu = 0
				m.currentView = constants.NetworkMenuView
				return m, nil

			case "language":
				// Implementar a lógica para configurar idioma
				m.initLanguageSelection()
				return m, nil

			case "keepass":
				m.initKeePassSettings()
				return m, nil

			case "back":
				m.menuItems = NewMenu() // Recarregar o menu principal
				m.selectedMenu = 0      // Resetar a seleção
				m.currentView = constants.DefaultView
			}
		case "esc":
			m.menuItems = NewMenu() // Recarregar o menu principal
			m.selectedMenu = 0      // Resetar a seleção
			m.currentView = constants.DefaultView
		}
	}
	return m, nil
}

func (m *CLIModel) cancelPendingVaultBackup() error {
	if m.Vault != nil && m.backupChallenge != nil {
		var err error
		if m.resumeBackupAccountID != "" {
			err = m.Vault.SuspendBackup(m.backupChallenge.ChallengeID)
		} else {
			err = m.Vault.CancelBackup(context.Background(), m.backupChallenge.ChallengeID)
		}
		if err != nil {
			return err
		}
		for index := range m.backupChallenge.Words {
			m.backupChallenge.Words[index] = ""
		}
	}
	m.backupChallenge = nil
	m.pendingAccount = nil
	m.backupPassphraseInput.SetValue("")
	m.backupPathInput.SetValue("")
	m.backupLanguageInput.SetValue("")
	m.backupWordAnswers = nil
	m.backupMaterialStage = 0
	m.resumeBackupAccountID = ""
	return nil
}

func (m *CLIModel) clearVaultActionInputs() {
	m.credentialUseKeePass = false

	m.currentPasswordInput.SetValue("")
	m.newPasswordInput.SetValue("")
	m.confirmPasswordInput.SetValue("")
	m.exportDestinationInput.SetValue("")
	m.vaultActionStage = 0
	m.vaultExportEncrypted = false
	m.vaultActionPreview = false
	m.vaultActionError = ""
}

func (m *CLIModel) clearImportSecrets() {
	m.passwordInput.SetValue("")
}

func (m *CLIModel) selectedAccountFromTable() *wallet.AccountSummary {
	cursor := m.walletTable.Cursor()
	if cursor < 0 || cursor >= len(m.accountTableIDs) || cursor >= len(m.walletTable.Rows()) {
		return nil
	}
	accountID := m.accountTableIDs[cursor]
	for index := range m.accounts {
		if m.accounts[index].AccountID == accountID {
			return &m.accounts[index]
		}
	}
	return nil
}

func (m *CLIModel) selectedWalletFromTable() *wallet.Wallet {
	selectedRow := m.walletTable.SelectedRow()
	if len(selectedRow) == 0 {
		return nil
	}
	walletID, err := strconv.Atoi(selectedRow[0])
	if err != nil {
		return nil
	}
	for i := range m.wallets {
		if m.wallets[i].ID == walletID {
			return &m.wallets[i]
		}
	}
	return nil
}

func (m *CLIModel) updateListWallets(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.accountDeletion != nil {
		return m.updateAccountDeletion(msg)
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "d", "delete":
			if m.Vault != nil {
				if selected := m.selectedAccountFromTable(); selected != nil {
					m.initAccountDeletion(*selected)
				}
				return m, nil
			}
			m.err = errors.Wrap(wallet.ErrWalletDeletionDisabled, 0)
			return m, nil
		case "enter":
			if m.Vault != nil {
				if selected := m.selectedAccountFromTable(); selected != nil {
					m.clearBalanceState()
					m.selectedAccount = selected
					m.initWalletDetailsComponents()
					m.currentView = constants.WalletDetailsView
					return m, nil
				}
			}
		case "esc":
			m.currentView = constants.DefaultView
			return m, nil
		}
	}

	// Atualizar a tabela com a mensagem apenas se houver wallets
	if len(m.wallets) > 0 || len(m.accounts) > 0 {
		var cmd tea.Cmd
		m.walletTable, cmd = m.walletTable.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *CLIModel) updateWalletDetails(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.selectedAccount != nil {
		if m.walletDetailsViewport.Width == 0 {
			m.initWalletDetailsComponents()
		} else {
			m.refreshWalletDetailsComponents()
		}
	}
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if key.Matches(msg, m.walletDetailsKeys.ToggleHelp) {
			m.walletDetailsHelp.ShowAll = !m.walletDetailsHelp.ShowAll
			return m, nil
		}
		if key.Matches(msg, m.walletDetailsKeys.Up) || key.Matches(msg, m.walletDetailsKeys.Down) || key.Matches(msg, m.walletDetailsKeys.PageUp) || key.Matches(msg, m.walletDetailsKeys.PageDown) {
			var command tea.Cmd
			m.walletDetailsViewport, command = m.walletDetailsViewport.Update(msg)
			return m, command
		}
		switch {
		case key.Matches(msg, m.walletDetailsKeys.ContractCall):
			if m.transactionEngineFactory != nil && m.transactionAuthorizer != nil && m.selectedAccount != nil && m.selectedAccount.SignerKind.SupportsEOASigning() && m.selectedAccount.Capabilities&wallet.CapabilitySignTransaction != 0 && (m.selectedAccount.State == wallet.AccountStateActive || m.selectedAccount.State == wallet.AccountStateLocked) {
				m.initContractCall()
				return m, nil
			}
		case key.Matches(msg, m.walletDetailsKeys.WalletConnect):
			if m.walletConnectReader != nil && m.selectedAccount != nil {
				m.initWalletConnect()
				return m, nil
			}
		case key.Matches(msg, m.walletDetailsKeys.FIDO2):
			if m.fido2Service != nil && m.selectedAccount != nil {
				m.initFIDO2()
				return m, nil
			}
		case key.Matches(msg, m.walletDetailsKeys.SendNative), key.Matches(msg, m.walletDetailsKeys.SendToken), key.Matches(msg, m.walletDetailsKeys.SendNFT), key.Matches(msg, m.walletDetailsKeys.Send1155), key.Matches(msg, m.walletDetailsKeys.Send1155Batch), key.Matches(msg, m.walletDetailsKeys.ApproveToken):
			if m.transactionEngineFactory != nil && m.transactionAuthorizer != nil && m.selectedAccount != nil && m.selectedAccount.SignerKind.SupportsEOASigning() && m.selectedAccount.Capabilities&wallet.CapabilitySignTransaction != 0 && (m.selectedAccount.State == wallet.AccountStateActive || m.selectedAccount.State == wallet.AccountStateLocked) {
				switch msg.String() {
				case "n":
					m.initNativeTransfer()
				case "t":
					m.initERC20Transfer()
				case "o":
					m.initERC721Transfer()
				case "m":
					m.initERC1155Transfer()
				case "z":
					m.initERC1155BatchTransfer()
				case "a":
					m.initERC20Approve()
				}
				m.currentView = constants.NativeTransferView
				return m, nil
			}
		case key.Matches(msg, m.walletDetailsKeys.History):
			if m.historyReader != nil && m.selectedAccount != nil {
				return m, m.initAccountHistory()
			}
		case key.Matches(msg, m.walletDetailsKeys.SignMessage):
			if m.messageSigningFactory != nil && m.transactionAuthorizer != nil && m.selectedAccount != nil {
				service, err := m.messageSigningFactory(context.Background())
				if err != nil {
					m.err = errors.Wrap(err, 0)
					return m, nil
				}
				m.initPersonalSign(service)
				return m, nil
			}
		case key.Matches(msg, m.walletDetailsKeys.SignTypedData):
			if m.messageSigningFactory != nil && m.transactionAuthorizer != nil && m.selectedAccount != nil {
				service, err := m.messageSigningFactory(context.Background())
				if err != nil {
					m.err = errors.Wrap(err, 0)
					return m, nil
				}
				m.initEIP712Sign(service)
				return m, nil
			}
		case key.Matches(msg, m.walletDetailsKeys.FetchBalances):
			if m.balanceProvider != nil && m.balanceConfig != nil && m.selectedAccount != nil && !m.balanceLoading {
				m.balanceLoading = true
				m.balanceError = ""
				m.balanceOperationID++
				operationID := m.balanceOperationID
				accountID := m.selectedAccount.AccountID
				provider := m.balanceProvider
				cfg := m.balanceConfig
				loader := m.balanceConfigLoader
				address := m.selectedAccount.Address
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				m.balanceCancel = cancel
				return m, func() tea.Msg {
					defer cancel()
					if loader != nil {
						latest, err := loader()
						if err != nil {
							return balanceFetchMsg{operationID: operationID, accountID: accountID, failures: []error{err}}
						}
						cfg = latest
					}
					failures := provider.RefreshProviders(ctx, cfg)
					return balanceFetchMsg{operationID: operationID, accountID: accountID, balances: provider.GetAllBalances(ctx, address), failures: failures}
				}
			}
		case key.Matches(msg, m.walletDetailsKeys.ResumeBackup):
			if m.Vault != nil && m.selectedAccount != nil && m.selectedAccount.State == wallet.AccountStatePendingBackup {
				m.initResumeBackup(m.selectedAccount.AccountID)
				return m, nil
			}
		case key.Matches(msg, m.walletDetailsKeys.Lock):
			if m.Vault != nil && m.selectedAccount != nil {
				if err := m.Vault.LockAccount(context.Background(), m.selectedAccount.AccountID); err != nil {
					m.err = errors.Wrap(err, 0)
					return m, nil
				}
				m.selectedAccount.State = wallet.AccountStateLocked
				m.refreshWalletDetailsComponents()
				return m, m.refreshWalletsTable()
			}
		case key.Matches(msg, m.walletDetailsKeys.Rotate):
			if m.Vault != nil && m.selectedAccount != nil {
				m.initVaultAction(false)
				return m, nil
			}
		case key.Matches(msg, m.walletDetailsKeys.Export):
			if m.Vault != nil && m.selectedAccount != nil {
				m.initVaultAction(true)
				return m, nil
			}
		case key.Matches(msg, m.walletDetailsKeys.EncryptedExport):
			if m.Vault != nil && m.selectedAccount != nil {
				m.initEncryptedExportAction()
				return m, nil
			}
		case key.Matches(msg, m.walletDetailsKeys.KeePass):
			if m.selectedAccount != nil && m.selectedAccount.SignerKind == wallet.SignerKindSoftware && m.selectedAccount.Capabilities&wallet.CapabilityExportSecret != 0 && (m.selectedAccount.State == wallet.AccountStateActive || m.selectedAccount.State == wallet.AccountStateLocked) && m.credentialBackupEnabled() {
				m.initKeePassAccount(*m.selectedAccount)
				return m, m.loadKeePassAccountStatus()
			}
		case key.Matches(msg, m.walletDetailsKeys.Recovery):
			if recoveryAvailable(m.selectedAccount, m.Vault) {
				m.initRecovery()
				return m, nil
			}
		case key.Matches(msg, m.walletDetailsKeys.Back):
			m.clearBalanceState()
			m.walletDetails = nil
			m.selectedAccount = nil
			m.currentView = constants.ListWalletsView
			if m.Vault != nil {
				m.initAccountList()
			}
			return m, nil // Return explícito para consumir o evento de teclado
		}
	}
	return m, nil
}

func (m *CLIModel) updateVaultAction(msg tea.Msg, export bool) (tea.Model, tea.Cmd) {
	if m.Vault == nil || m.selectedAccount == nil {
		m.err = errors.Wrap(fmt.Errorf("vault account is not selected"), 0)
		m.currentView = constants.DefaultView
		return m, nil
	}
	lastStage := 2
	if export {
		lastStage = 3
	}
	if key, ok := msg.(tea.KeyMsg); ok && key.String() == "enter" {
		if m.vaultActionStage < lastStage {
			m.currentPasswordInput.Blur()
			m.newPasswordInput.Blur()
			m.confirmPasswordInput.Blur()
			m.exportDestinationInput.Blur()
			m.vaultActionStage++
			switch m.vaultActionStage {
			case 1:
				m.newPasswordInput.Focus()
			case 2:
				m.confirmPasswordInput.Focus()
			case 3:
				m.exportDestinationInput.Focus()
			}
			return m, nil
		}
		newPassword := m.newPasswordInput.Value()
		if !wallet.SecureCompare(newPassword, m.confirmPasswordInput.Value()) {
			m.vaultActionError = localization.Get("vault_err_new_password_mismatch")
			m.currentPasswordInput.SetValue("")
			m.newPasswordInput.SetValue("")
			m.confirmPasswordInput.SetValue("")
			m.vaultActionStage = 0
			m.currentPasswordInput.Focus()
			return m, nil
		}
		newPasswordValidation := []byte(newPassword)
		validationErr := wallet.ValidateStoragePassword(newPasswordValidation)
		clear(newPasswordValidation)
		if validationErr != nil {
			m.vaultActionError = validationErr.Error()
			m.newPasswordInput.SetValue("")
			m.confirmPasswordInput.SetValue("")
			m.vaultActionStage = 1
			m.newPasswordInput.Focus()
			return m, nil
		}
		if export && !m.vaultActionPreview {
			if !filepath.IsAbs(m.exportDestinationInput.Value()) {
				m.vaultActionError = localization.Get("vault_err_export_abs")
				return m, nil
			}
			m.vaultActionPreview = true
			m.vaultActionError = ""
			return m, nil
		}
		if m.vaultBusy {
			return m, nil
		}
		return m, m.submitWithCredential(m.credentialBackupEnabled(), func() tea.Cmd { return m.startVaultAction(export) })
	}
	if key, ok := msg.(tea.KeyMsg); ok && key.String() == "ctrl+k" && m.vaultActionStage == 0 && m.selectedAccount != nil && m.credentialToggleEligible(*m.selectedAccount) {
		m.credentialUseKeePass = !m.credentialUseKeePass
		if m.credentialUseKeePass {
			m.currentPasswordInput.SetValue("")
		}
		return m, nil
	}
	if m.vaultActionPreview {
		return m, nil
	}
	var command tea.Cmd
	switch m.vaultActionStage {
	case 0:
		m.currentPasswordInput, command = m.currentPasswordInput.Update(msg)
	case 1:
		m.newPasswordInput, command = m.newPasswordInput.Update(msg)
	case 2:
		m.confirmPasswordInput, command = m.confirmPasswordInput.Update(msg)
	case 3:
		m.exportDestinationInput, command = m.exportDestinationInput.Update(msg)
	}
	return m, command
}

func (m *CLIModel) accountTableWidth() int {
	return max(1, m.width-m.styles.Content.GetHorizontalFrameSize())
}

func accountTableLayout(available int, accounts []wallet.AccountSummary) ([]table.Column, []table.Row) {
	nameTitle := localization.Get("name")
	typeTitle := localization.Get("wallet_type")
	createdTitle := localization.Get("created_at")
	addressTitle := localization.Get("ethereum_address")

	addressWidth := min(42, max(1, available-5))
	typeWidth, dateWidth := 0, 0
	visible := 2
	if available >= 116 {
		typeWidth, dateWidth = 24, 16
		visible = 4
	} else if available >= 90 {
		typeWidth = 20
		visible = 3
	}
	if available < 6 {
		addressWidth, typeWidth, dateWidth = 0, 0, 0
		visible = 1
	}
	nameWidth := max(1, available-2*visible-addressWidth-typeWidth-dateWidth)
	if available < 6 {
		nameWidth = max(0, available-2)
	}

	columns := []table.Column{
		{Title: nameTitle, Width: nameWidth},
		{Title: typeTitle, Width: typeWidth},
		{Title: createdTitle, Width: dateWidth},
		{Title: addressTitle, Width: addressWidth},
	}
	rows := make([]table.Row, 0, len(accounts))
	for _, account := range accounts {
		accountType := fmt.Sprintf("%s / %s", safeShort(string(account.SignerKind)), safeShort(string(account.State)))
		createdAt := account.CreatedAt.Format("2006-01-02 15:04")
		rows = append(rows, table.Row{safeShort(account.Name), accountType, createdAt, safeShort(account.Address)})
	}
	return columns, rows
}

func (m *CLIModel) updateTableDimensions() {
	if m.currentView != constants.ListWalletsView || (len(m.wallets) == 0 && len(m.accounts) == 0) {
		return
	}

	// Calcular a altura disponível para a área de conteúdo
	headerHeight := lipgloss.Height(m.styles.Header.Render(""))
	footerHeight := lipgloss.Height(m.styles.Footer.Render(""))

	// Reserva de espaço para o título e instruções dentro da área de conteúdo
	titleAndInstructionsHeight := 6 // Espaço estimado para o título e as instruções

	// Calcular altura final da tabela (altura total - cabeçalho - rodapé - título/instruções - margem)
	contentAreaHeight := m.height - headerHeight - footerHeight - titleAndInstructionsHeight - 2

	// Garantir que a tabela tenha pelo menos uma altura mínima
	if contentAreaHeight < 5 {
		contentAreaHeight = 5
	}

	// Definir largura e altura da tabela
	// Reduzir a largura da tabela para evitar quebra de linha
	m.walletTable.SetWidth(m.accountTableWidth())
	if len(m.wallets) > 0 || len(m.accounts) > 0 {
		m.walletTable.SetHeight(contentAreaHeight)
	}

	if len(m.accounts) > 0 {
		previousID := ""
		if selected := m.selectedAccountFromTable(); selected != nil {
			previousID = selected.AccountID
		}
		previousCursor := m.walletTable.Cursor()
		m.accountTableIDs = make([]string, len(m.accounts))
		for index := range m.accounts {
			m.accountTableIDs[index] = m.accounts[index].AccountID
		}
		columns, rows := accountTableLayout(m.accountTableWidth(), m.accounts)
		m.walletTable.SetColumns(columns)
		m.walletTable.SetRows(rows)
		cursor := min(previousCursor, len(m.accounts)-1)
		if previousID != "" {
			for index := range m.accounts {
				if m.accounts[index].AccountID == previousID {
					cursor = index
					break
				}
			}
		}
		m.walletTable.GotoTop()
		m.walletTable.MoveDown(cursor)
	}
}

// Funções de inicialização

func (m *CLIModel) initCreateWallet() {
	m.clearCredentialPrompt()
	m.backupChallenge = nil
	m.pendingAccount = nil
	m.backupError = ""

	// Initialize name input first
	m.nameInput = textinput.New()
	m.nameInput.Placeholder = localization.Get("wallet_name_placeholder")
	m.nameInput.CharLimit = 50
	m.nameInput.Width = constants.PasswordWidth
	m.nameInput.Focus()
	m.currentView = constants.CreateWalletNameView

	m.createWordCountInput = textinput.New()
	m.createWordCountInput.Placeholder = localization.Get("word_count_placeholder")
	m.createWordCountInput.SetValue("12")
	m.createWordCountInput.CharLimit = 2
	m.createLanguageInput = textinput.New()
	m.createLanguageInput.Placeholder = localization.Get("bip39_language_placeholder")
	m.createLanguageInput.SetValue("english")
	m.createLanguageInput.CharLimit = 32
	m.createPassphraseInput = textinput.New()
	m.createPassphraseInput.Placeholder = localization.Get("passphrase_optional_placeholder")
	m.createPassphraseInput.CharLimit = constants.PasswordCharLimit
	m.createPassphraseInput.EchoMode = textinput.EchoPassword
	m.createPassphraseInput.EchoCharacter = '•'
	m.createDerivationPathInput = textinput.New()
	m.createDerivationPathInput.Placeholder = localization.Get("evm_path_placeholder")
	m.createDerivationPathInput.SetValue("m/44'/60'/0'/0/0")
	m.createDerivationPathInput.CharLimit = 255
	m.createOptionsStage = 0
	m.createCustomPath = false
	m.configureCreateOptionList(0)

	m.backupConfirmationInput = textinput.New()
	m.backupConfirmationInput.Placeholder = localization.Get("confirm_mnemonic")
	m.backupConfirmationInput.CharLimit = 512
	m.backupConfirmationInput.Width = 80
	m.backupConfirmationInput.EchoMode = textinput.EchoPassword
	m.backupConfirmationInput.EchoCharacter = '•'

	// Initialize password input (will be used after name is entered)
	m.passwordInput = textinput.New()
	m.passwordInput.Placeholder = localization.Get("enter_password")
	m.passwordInput.CharLimit = constants.PasswordCharLimit
	m.passwordInput.Width = constants.PasswordWidth
	m.passwordInput.EchoMode = textinput.EchoPassword
	m.passwordInput.EchoCharacter = '•'
	m.passwordInput.Validate = func(s string) error {
		return validateStoragePasswordInput(s)
	}
	m.createPasswordConfirmationInput = textinput.New()
	m.createPasswordConfirmationInput.Placeholder = localization.Get("confirm_storage_placeholder")
	m.createPasswordConfirmationInput.CharLimit = constants.PasswordCharLimit
	m.createPasswordConfirmationInput.Width = constants.PasswordWidth
	m.createPasswordConfirmationInput.EchoMode = textinput.EchoPassword
	m.createPasswordConfirmationInput.EchoCharacter = '•'
	m.createPasswordStage = 0
	m.createPasswordError = ""
}

func (m *CLIModel) initImportMethodSelection() {
	// Usar o menu de importação que inclui a opção de voltar ao menu principal
	m.menuItems = NewImportMenu()
	m.selectedMenu = 0
	m.currentView = constants.ImportMethodSelectionView
}

func (m *CLIModel) initConfigMenu() {
	// Usar o menu de configuração que inclui a opção de voltar ao menu principal
	m.menuItems = NewConfigMenu()
	m.selectedMenu = 0
	m.currentView = constants.ConfigurationView
}

func (m *CLIModel) initImportWallet() {
	// Instead of directly initializing the mnemonic import view,
	// now we show the selection screen first
	m.initImportMethodSelection()
}

func (m *CLIModel) initAccountList() {
	m.menuItems = NewMenu()
	m.selectedMenu = 2
	m.currentView = constants.ListWalletsView
	accounts, err := m.Vault.ListAccounts(context.Background())
	if err != nil {
		m.err = errors.Wrap(err, 0)
		m.currentView = constants.DefaultView
		return
	}
	m.applyAccountList(accounts)
}

func (m *CLIModel) applyAccountList(accounts []wallet.AccountSummary) {
	previousID := ""
	if selected := m.selectedAccountFromTable(); selected != nil {
		previousID = selected.AccountID
	}
	previousCursor := m.walletTable.Cursor()
	m.accounts = accounts
	m.wallets = nil
	m.walletCount = len(accounts)
	m.accountTableIDs = make([]string, len(accounts))
	for index := range accounts {
		m.accountTableIDs[index] = accounts[index].AccountID
	}
	columns, rows := accountTableLayout(m.accountTableWidth(), accounts)
	m.walletTable = table.New(
		table.WithColumns(columns),
		table.WithRows(rows),
		table.WithFocused(true),
	)
	m.walletTable.SetWidth(m.accountTableWidth())
	styles := table.DefaultStyles()
	styles.Header = styles.Header.BorderStyle(lipgloss.NormalBorder()).BorderForeground(lipgloss.Color("240")).BorderBottom(true).Bold(true)
	styles.Selected = styles.Selected.Foreground(lipgloss.Color("229")).Background(lipgloss.Color("57")).Bold(false)
	styles.Cell = styles.Cell.Align(lipgloss.Left)
	m.walletTable.SetStyles(styles)
	contentHeight := m.height - lipgloss.Height(m.styles.Header.Render("")) - lipgloss.Height(m.styles.Footer.Render("")) - 2
	if contentHeight < 0 {
		contentHeight = 0
	}
	if len(accounts) > 0 {
		m.walletTable.SetHeight(contentHeight)
		cursor := min(previousCursor, len(accounts)-1)
		if previousID != "" {
			for index := range accounts {
				if accounts[index].AccountID == previousID {
					cursor = index
					break
				}
			}
		}
		m.walletTable.GotoTop()
		m.walletTable.MoveDown(cursor)
	}
	m.updateTableDimensions()
}

func (m *CLIModel) initListWallets() {
	if m.Vault != nil {
		m.initAccountList()
		return
	}
	m.err = errors.Wrap(fmt.Errorf("wallet vault is required"), 0)
	m.currentView = constants.DefaultView
}

func (m *CLIModel) initBackupMaterialInputs() {
	m.backupPathInput = textinput.New()
	m.backupPathInput.Placeholder = localization.Get("backup_reenter_path")
	m.backupPathInput.CharLimit = 255
	m.backupPathInput.Width = 80
	m.backupLanguageInput = textinput.New()
	m.backupLanguageInput.Placeholder = localization.Get("backup_reenter_language")
	m.backupLanguageInput.CharLimit = 32
	m.backupPassphraseInput = textinput.New()
	m.backupPassphraseInput.Placeholder = localization.Get("backup_reenter_passphrase")
	m.backupPassphraseInput.CharLimit = constants.PasswordCharLimit
	m.backupPassphraseInput.EchoMode = textinput.EchoPassword
	m.backupPassphraseInput.EchoCharacter = '•'
	m.backupMaterialStage = 0
	m.backupWordAnswers = nil
}

func (m *CLIModel) initResumeBackup(accountID string) {
	m.clearCredentialPrompt()
	m.resumeBackupAccountID = accountID
	m.passwordInput = textinput.New()
	m.passwordInput.Placeholder = localization.Get("enter_password")
	m.passwordInput.CharLimit = constants.PasswordCharLimit
	m.passwordInput.Width = constants.PasswordWidth
	m.passwordInput.EchoMode = textinput.EchoPassword
	m.passwordInput.EchoCharacter = '•'
	m.passwordInput.Focus()
	m.createPasswordConfirmationInput = textinput.New()
	m.createPasswordConfirmationInput.Placeholder = localization.Get("confirm_storage_placeholder")
	m.createPasswordConfirmationInput.CharLimit = constants.PasswordCharLimit
	m.createPasswordConfirmationInput.Width = constants.PasswordWidth
	m.createPasswordConfirmationInput.EchoMode = textinput.EchoPassword
	m.createPasswordConfirmationInput.EchoCharacter = '•'
	m.createPasswordStage = 0
	m.createPasswordError = ""
	m.currentView = constants.CreateWalletView
}

func (m *CLIModel) initEncryptedExportAction() {
	m.initVaultAction(true)
	m.vaultExportEncrypted = true
}

func (m *CLIModel) initVaultAction(export bool) {
	m.clearCredentialPrompt()
	m.clearVaultActionInputs()
	m.lastOperationNotice = ""
	m.currentPasswordInput = textinput.New()
	m.currentPasswordInput.Placeholder = localization.Get("current_storage_placeholder")
	m.currentPasswordInput.CharLimit = constants.PasswordCharLimit
	m.currentPasswordInput.Width = constants.PasswordWidth
	m.currentPasswordInput.EchoMode = textinput.EchoPassword
	m.currentPasswordInput.EchoCharacter = '•'
	m.currentPasswordInput.Focus()
	m.newPasswordInput = textinput.New()
	m.newPasswordInput.Placeholder = localization.Get("new_password_placeholder")
	m.newPasswordInput.CharLimit = constants.PasswordCharLimit
	m.newPasswordInput.Width = constants.PasswordWidth
	m.newPasswordInput.EchoMode = textinput.EchoPassword
	m.newPasswordInput.EchoCharacter = '•'
	m.confirmPasswordInput = textinput.New()
	m.confirmPasswordInput.Placeholder = localization.Get("confirm_storage_placeholder")
	m.confirmPasswordInput.CharLimit = constants.PasswordCharLimit
	m.confirmPasswordInput.Width = constants.PasswordWidth
	m.confirmPasswordInput.EchoMode = textinput.EchoPassword
	m.confirmPasswordInput.EchoCharacter = '•'
	m.exportDestinationInput = textinput.New()
	m.exportDestinationInput.Placeholder = localization.Get("export_destination_placeholder")
	m.exportDestinationInput.CharLimit = 1024
	m.exportDestinationInput.Width = 80
	if export {
		m.currentView = constants.ExportAccountView
	} else {
		m.currentView = constants.RotatePasswordView
	}
}

func (m *CLIModel) initLanguageSelection() {
	// Use the existing configuration if available
	if m.currentConfig == nil {
		// Load or create the configuration
		cfg, err := loadOrCreateConfig()
		if err != nil {
			m.err = errors.Wrap(err, 0)
			m.currentView = constants.DefaultView
			return
		}

		// Store the current configuration
		m.currentConfig = cfg
	}

	// Set the menu items to the language menu items
	m.menuItems = NewLanguageMenu(m.currentConfig)

	// Reset the selected menu item
	m.selectedMenu = 0

	// Set the current view to language selection
	m.currentView = constants.LanguageSelectionView
}

// updateLanguageSelection handles user input in the language selection view
func (m *CLIModel) updateLanguageSelection(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if m.selectedMenu > 0 {
				m.selectedMenu--
			}
		case "down", "j":
			if m.selectedMenu < len(m.menuItems)-1 {
				m.selectedMenu++
			}
		case "left", "h":
			if m.selectedMenu > 1 {
				m.selectedMenu -= 2
			}
		case "right", "l":
			if m.selectedMenu < len(m.menuItems)-2 {
				m.selectedMenu += 2
			}
		case "enter":
			item := m.menuItems[m.selectedMenu]
			if item.action != "select_language" {
				m.menuItems = NewConfigMenu()
				m.selectedMenu = 0
				m.currentView = constants.ConfigurationView
				return m, nil
			}

			selectedLang := item.value

			// Update the configuration
			if m.currentConfig != nil && selectedLang != localization.NormalizeLanguage(m.currentConfig.Language) {
				// Atualizar o idioma no arquivo de configuração
				err := updateLanguageInConfig(selectedLang)
				if err != nil {
					m.err = errors.Wrap(err, 0)
					return m, nil
				}

				// Reload the configuration
				cm := getConfigurationManager()
				newCfg, err := cm.ReloadConfiguration()
				if err != nil {
					m.err = errors.Wrap(err, 0)
					return m, nil
				}

				// Update the current configuration
				m.currentConfig = newCfg

				// Reinitialize localization with the new language
				err = localization.InitLocalization(newCfg)
				if err != nil {
					m.err = errors.Wrap(err, 0)
					return m, nil
				}
				m.localeLanguage = localization.GetCurrentLanguage()
				m.refreshLocalizedUI()
			}
			// Return to the config menu
			m.menuItems = NewConfigMenu()
			m.selectedMenu = 0
			m.currentView = constants.ConfigurationView
		case "esc":
			// Return to the config menu
			m.menuItems = NewConfigMenu()
			m.selectedMenu = 0
			m.currentView = constants.ConfigurationView
		}
	}
	return m, nil
}

// updateNetworkMenu handles user input in the network menu view
func (m *CLIModel) updateNetworkMenu(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if m.selectedMenu > 0 {
				m.selectedMenu--
			}
		case "down", "j":
			if m.selectedMenu < len(m.menuItems)-1 {
				m.selectedMenu++
			}
		case "enter":
			switch m.menuItems[m.selectedMenu].action {
			case "add_network":
				m.initAddNetwork()
				return m, nil
			case "network_list":
				m.initNetworkList()
				return m, nil
			case "back":
				m.menuItems = NewConfigMenu()
				m.selectedMenu = 0
				m.currentView = constants.ConfigurationView
				return m, nil
			}
		case "esc":
			// Return to the config menu
			m.menuItems = NewConfigMenu()
			m.selectedMenu = 0
			m.currentView = constants.ConfigurationView
		}
	}
	return m, nil
}

// walletsRefreshedMsg é uma mensagem personalizada para indicar que a lista de wallets foi atualizada
type walletsRefreshedMsg struct {
	wallets  []wallet.Wallet
	accounts []wallet.AccountSummary
	err      error
}

func (m *CLIModel) refreshWalletsTable() tea.Cmd {
	return func() tea.Msg {
		if m.Vault != nil {
			accounts, err := m.Vault.ListAccounts(context.Background())
			return walletsRefreshedMsg{accounts: accounts, err: err}
		}
		return walletsRefreshedMsg{err: fmt.Errorf("wallet vault is required")}
	}
}

func (m *CLIModel) rebuildWalletsTable() {
	// Only create a table if there are wallets
	if len(m.wallets) == 0 {
		return
	}

	// Inicialize as colunas com larguras adequadas
	idColWidth := 10
	nameColWidth := 20
	typeColWidth := 20
	createdAtColWidth := 20
	addressColWidth := m.width - idColWidth - nameColWidth - typeColWidth - createdAtColWidth - 20 // Subtrai 20 para padding e margens

	if addressColWidth < 20 {
		addressColWidth = 20
	}

	columns := []table.Column{
		{Title: localization.Get("id"), Width: idColWidth},
		{Title: localization.Get("name"), Width: nameColWidth},
		{Title: localization.Get("wallet_type"), Width: typeColWidth},
		{Title: localization.Get("created_at"), Width: createdAtColWidth},
		{Title: localization.Get("ethereum_address"), Width: addressColWidth},
	}

	var rows []table.Row
	for _, w := range m.wallets {
		// Determine wallet type using ImportMethod as primary source
		walletType := determineWalletType(w)

		// Format created at date
		createdAt := w.CreatedAt.Format("2006-01-02 15:04")

		rows = append(rows, table.Row{
			fmt.Sprintf("%d", w.ID),
			safeShort(w.Name),
			safeShort(walletType),
			createdAt,
			safeShort(w.Address),
		})
	}

	m.walletTable = table.New(
		table.WithColumns(columns),
		table.WithRows(rows),
		table.WithFocused(true),
	)

	// Definir largura explicitamente para evitar quebra de linha
	m.walletTable.SetWidth(m.width - 12)

	// Ajustar os estilos da tabela
	s := table.DefaultStyles()
	s.Header = s.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("240")).
		BorderBottom(true).
		Bold(true)
	s.Selected = s.Selected.
		Foreground(lipgloss.Color("229")).
		Background(lipgloss.Color("57")).
		Bold(false)
	s.Cell = s.Cell.Align(lipgloss.Left)
	m.walletTable.SetStyles(s)

	// Definir altura da tabela para usar totalmente o espaço disponível
	contentAreaHeight := m.height - lipgloss.Height(m.styles.Header.Render("")) - lipgloss.Height(m.styles.Footer.Render("")) - 2
	if contentAreaHeight < 0 {
		contentAreaHeight = 0
	}
	m.walletTable.SetHeight(contentAreaHeight)

	// Atualizar dimensões da tabela
	m.updateTableDimensions()
}
