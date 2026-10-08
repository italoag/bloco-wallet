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
		return localization.Labels["imported_mnemonic"]
	case wallet.ImportMethodPrivateKey:
		return localization.Labels["imported_private_key"]
	case wallet.ImportMethodKeystore:
		return localization.Labels["imported_keystore"]
	default:
		// Fallback to old logic for backward compatibility with wallets missing ImportMethod
		if w.Mnemonic == nil {
			return localization.Labels["imported_private_key"]
		}
		return localization.Labels["imported_mnemonic"]
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

func (m *CLIModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if msg == nil {
		return m, nil
	}
	switch message := msg.(type) {
	case walletConnectProposalMsg:
		m.walletConnectHandleProposal(message.proposal)
		return m, waitForWalletConnectEvent(m.walletConnectEvents)
	case walletConnectRequestMsg:
		m.walletConnectHandleRequest(message.session, message.params)
		return m, waitForWalletConnectEvent(m.walletConnectEvents)
	}
	if preparedMessage, ok := msg.(nativePreparedMsg); ok && preparedMessage.prepared != nil {
		if m.currentView != constants.NativeTransferView || m.nativeTransfer == nil || preparedMessage.generation != m.nativeTransfer.generation || m.nativeTransfer.phase != nativeTransferPreparing {
			return m, nativeCancelPreparedResultCommand(preparedMessage.engine, preparedMessage.prepared)
		}
	}
	if submittedMessage, ok := msg.(nativeSubmittedMsg); ok && submittedMessage.result.Hash != (common.Hash{}) {
		if m.currentView != constants.NativeTransferView || m.nativeTransfer == nil || submittedMessage.generation != m.nativeTransfer.generation || m.nativeTransfer.phase != nativeTransferSubmitting {
			m.transactionNotice = "Transaction submitted or outcome pending: " + safeShort(submittedMessage.result.Hash.Hex())
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
			m.clearBalanceState()
			m.clearNativeTransfer()
			if m.currentView == constants.CanonicalImportView && m.canonicalImport != nil && m.canonicalImport.busy {
				if m.canonicalImport.cancel != nil {
					m.canonicalImport.cancel()
				}
				m.canonicalImport.cancelling = true
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
			m.balanceError = "No active networks with validated providers"
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
		return m, nil
	case canonicalCommitResultMsg:
		if m.currentView == constants.CanonicalImportView && m.canonicalImport != nil {
			return m.updateCanonicalImport(msg)
		}
		return m, nil
	case canonicalSourcePasswordMsg:
		if m.currentView == constants.CanonicalImportView && m.canonicalImport != nil {
			return m.updateCanonicalImport(msg)
		}
		clear(msg.password)
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
	default:
		m.currentView = constants.DefaultView
		return m, nil
	}
}

func (m *CLIModel) View() string {
	if m.err != nil {
		label := safeShort(localization.Labels["error_title"])
		if label == "" {
			label = "Error"
		}
		return m.styles.ErrorStyle.Render(label + ": " + safeError(m.err))
	}

	switch m.currentView {
	case constants.SplashView:
		return m.renderSplash()
	case constants.ListWalletsView:
		return m.renderListWalletsWithLayout()
	default:
		return m.renderMainView()
	}
}

// renderListWalletsWithLayout renderiza a tela de listagem de carteiras com o layout completo
func (m *CLIModel) renderListWalletsWithLayout() string {
	if m.width < 100 || m.height < 24 {
		return m.renderCompactTerminal()
	}
	// Renderizar o cabeçalho da mesma forma que renderMainView
	renderedLogo := renderHeaderLogo()

	headerLeft := lipgloss.JoinVertical(
		lipgloss.Left,
		renderedLogo,
		fmt.Sprintf("Version: %s", m.displayVersion()),
	)

	menuItems := m.renderMenuItems()
	menuGrid := lipgloss.JoinVertical(lipgloss.Left, menuItems...)

	// Montar header
	headerGap := m.width - lipgloss.Width(headerLeft) - lipgloss.Width(menuGrid) - m.styles.Header.GetHorizontalFrameSize()
	headerContent := lipgloss.JoinVertical(lipgloss.Left, headerLeft, menuGrid)
	if headerGap >= 2 {
		headerContent = lipgloss.JoinHorizontal(lipgloss.Top, headerLeft, lipgloss.NewStyle().Width(headerGap).Render(""), menuGrid)
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
	switch m.currentView {
	case constants.DefaultView:
		return localization.Labels["welcome_message"]
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
	default:
		return localization.Labels["unknown_state"]
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
			switch m.menuItems[m.selectedMenu].title {
			case localization.Labels["create_new_wallet"]:
				m.initCreateWallet()
			case localization.Labels["import_wallet"]:
				m.initImportWallet()
			case localization.Labels["list_wallets"]:
				m.initListWallets()
			case "Safe multisig":
				m.initSafeView()
			case localization.Labels["configuration"]:
				m.initConfigMenu()
			case tea.KeyCtrlX.String(), "q", localization.Labels["exit"]:
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
				m.err = errors.Wrap(fmt.Errorf("o nome da wallet não pode estar vazio"), 0)
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
			m.createPasswordError = "Select an option before continuing"
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
				provided := strings.Fields(m.backupConfirmationInput.Value())
				if len(provided) != len(m.backupChallenge.RequiredWordIndices) {
					m.backupError = localization.Labels["mnemonic_mismatch"]
					m.backupConfirmationInput.SetValue("")
					return m, nil
				}
				m.backupWordAnswers = make(map[int]string, len(provided))
				for position, index := range m.backupChallenge.RequiredWordIndices {
					m.backupWordAnswers[index] = provided[position]
				}
				active, err := m.Vault.ConfirmBackup(context.Background(), m.backupChallenge.ChallengeID, m.backupWordAnswers)
				if err != nil {
					m.backupError = err.Error()
					m.backupPassphraseInput.SetValue("")
					return m, nil
				}
				for index := range m.backupChallenge.Words {
					m.backupChallenge.Words[index] = ""
				}
				m.backupPassphraseInput.SetValue("")
				m.backupPathInput.SetValue("")
				m.backupLanguageInput.SetValue("")
				m.backupWordAnswers = nil
				m.backupMaterialStage = 0
				m.backupChallenge = nil
				m.pendingAccount = nil
				m.resumeBackupAccountID = ""
				m.selectedAccount = &active
				m.initWalletDetailsComponents()
				m.backupError = ""
				m.backupConfirmationInput.SetValue("")
				m.nameInput.SetValue("")
				m.currentView = constants.WalletDetailsView
				return m, m.refreshWalletsTable()
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
				passwordBytes := []byte(password)
				summary, challenge, err := m.Vault.ResumeBackup(context.Background(), m.resumeBackupAccountID, passwordBytes)
				clear(passwordBytes)
				m.passwordInput.SetValue("")
				if err != nil {
					m.createPasswordError = err.Error()
					return m, nil
				}
				m.pendingAccount = &summary
				m.backupChallenge = &challenge
				m.initBackupMaterialInputs()
				m.backupConfirmationInput.SetValue("")
				m.backupConfirmationInput.Focus()
				m.currentView = constants.CreateWalletBackupView
				return m, nil
			}
			if m.Vault != nil && m.createPasswordStage == 0 {
				m.passwordInput.Blur()
				m.createPasswordConfirmationInput.Focus()
				m.createPasswordStage = 1
				return m, nil
			}
			if m.Vault != nil && !wallet.SecureCompare(password, m.createPasswordConfirmationInput.Value()) {
				m.createPasswordError = "Storage password confirmation does not match"
				m.passwordInput.SetValue("")
				m.createPasswordConfirmationInput.SetValue("")
				m.createPasswordConfirmationInput.Blur()
				m.passwordInput.Focus()
				m.createPasswordStage = 0
				return m, nil
			}

			if m.Vault == nil {
				m.err = errors.Wrap(fmt.Errorf("wallet vault is required"), 0)
				m.currentView = constants.DefaultView
				return m, nil
			}
			name := strings.TrimSpace(m.nameInput.Value())
			passwordBytes := []byte(password)
			wordCount, _ := strconv.Atoi(m.createWordCountInput.Value())
			summary, challenge, err := m.Vault.Create(context.Background(), wallet.CreateAccountRequest{
				Name:            name,
				Password:        passwordBytes,
				WordCount:       wordCount,
				BIP39Language:   wallet.BIP39Language(strings.ToLower(strings.ReplaceAll(m.createLanguageInput.Value(), "-", "_"))),
				BIP39Passphrase: m.createPassphraseInput.Value(),
				DerivationPath:  m.createDerivationPathInput.Value(),
			})
			clear(passwordBytes)
			m.createPassphraseInput.SetValue("")
			m.passwordInput.SetValue("")
			m.createPasswordConfirmationInput.SetValue("")
			m.createPasswordStage = 0
			m.createPasswordError = ""
			if err != nil {
				m.err = errors.Wrap(err, 0)
				m.currentView = constants.DefaultView
				return m, nil
			}
			m.pendingAccount = &summary
			m.backupChallenge = &challenge
			m.initBackupMaterialInputs()
			m.backupConfirmationInput.SetValue("")
			m.backupConfirmationInput.Focus()
			m.currentView = constants.CreateWalletBackupView
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
			switch m.selectedMenu {
			case 0:
				m.initCanonicalImport(wallet.ImportMethodMnemonic)
			case 1:
				m.initCanonicalImport(wallet.ImportMethodPrivateKey)
			case 2:
				m.initCanonicalImport(wallet.ImportMethodKeystore)
			case 3:
				m.initCanonicalBatchImport()
			case 4:
				m.initCanonicalImport(canonicalEncryptedMethod)
			case 5:
				m.initCanonicalImport(wallet.ImportMethodWatchOnly)
			case 6:
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
			switch m.selectedMenu {
			case 0: // Primeira opção: Redes
				// Mostrar o submenu de redes
				m.menuItems = NewNetworkMenu()
				m.selectedMenu = 0
				m.currentView = constants.NetworkMenuView
				return m, nil

			case 1: // Segunda opção: Idioma
				// Implementar a lógica para configurar idioma
				m.initLanguageSelection()
				return m, nil

			case 2: // Terceira opção: Voltar ao menu principal
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
	selectedRow := m.walletTable.SelectedRow()
	if len(selectedRow) == 0 {
		return nil
	}
	for index := range m.accounts {
		if m.accounts[index].AccountID == selectedRow[0] {
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
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "d", "delete":
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
			m.vaultActionError = "New password confirmation does not match"
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
				m.vaultActionError = "Export destination must be absolute"
				return m, nil
			}
			m.vaultActionPreview = true
			m.vaultActionError = ""
			return m, nil
		}
		currentPasswordBytes := []byte(m.currentPasswordInput.Value())
		newPasswordBytes := []byte(newPassword)
		confirmPasswordBytes := []byte(m.confirmPasswordInput.Value())
		defer clear(currentPasswordBytes)
		defer clear(newPasswordBytes)
		defer clear(confirmPasswordBytes)
		var err error
		if export {
			var handle wallet.CapabilityHandle
			handle, err = m.Vault.Unlock(context.Background(), m.selectedAccount.AccountID, currentPasswordBytes)
			if err == nil {
				if m.vaultExportEncrypted {
					err = m.Vault.ExportEncryptedAccount(context.Background(), wallet.EncryptedAccountExportRequest{
						Handle:             handle,
						Destination:        m.exportDestinationInput.Value(),
						CurrentPassword:    currentPasswordBytes,
						NewPassword:        newPasswordBytes,
						ConfirmNewPassword: confirmPasswordBytes,
					})
				} else {
					err = m.Vault.ExportKeystoreV3(context.Background(), wallet.KeystoreV3ExportRequest{
						Handle:          handle,
						Destination:     m.exportDestinationInput.Value(),
						Password:        newPasswordBytes,
						ConfirmPassword: confirmPasswordBytes,
					})
				}
				if wallet.IsExportCommitted(err) {
					m.lastOperationNotice = err.Error()
					err = nil
				} else if err == nil {
					m.lastOperationNotice = "Export completed successfully"
				}
				lockErr := m.Vault.Lock(handle)
				if err == nil {
					err = lockErr
				}
			}
		} else {
			err = m.Vault.RotatePassword(context.Background(), m.selectedAccount.AccountID, currentPasswordBytes, newPasswordBytes)
		}
		if err != nil {
			m.vaultActionError = err.Error()
			m.currentPasswordInput.SetValue("")
			m.newPasswordInput.SetValue("")
			m.confirmPasswordInput.SetValue("")
			m.vaultActionStage = 0
			m.currentPasswordInput.Focus()
			return m, nil
		}
		m.clearVaultActionInputs()
		m.currentView = constants.WalletDetailsView
		m.refreshWalletDetailsComponents()
		return m, m.refreshWalletsTable()
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

func accountTableLayout(width int, accounts []wallet.AccountSummary) ([]table.Column, []table.Row) {
	available := max(20, width-8)
	columns := []table.Column{
		{Title: localization.Labels["id"], Width: 36},
		{Title: "Nome", Width: 20},
		{Title: localization.Labels["wallet_type"], Width: 24},
		{Title: localization.Labels["created_at"], Width: 16},
		{Title: localization.Labels["ethereum_address"], Width: 42},
	}
	compactDate := false
	if available < 100 {
		columns = []table.Column{
			{Title: localization.Labels["id"], Width: 1},
			{Title: "Nome", Width: 8},
			{Title: localization.Labels["wallet_type"], Width: 10},
			{Title: localization.Labels["ethereum_address"], Width: 42},
		}
	} else if available < 130 {
		columns = []table.Column{
			{Title: localization.Labels["id"], Width: 8},
			{Title: "Nome", Width: 14},
			{Title: localization.Labels["wallet_type"], Width: 16},
			{Title: localization.Labels["ethereum_address"], Width: 42},
		}
	} else if available < 170 {
		columns = []table.Column{
			{Title: localization.Labels["id"], Width: 12},
			{Title: "Nome", Width: 16},
			{Title: localization.Labels["wallet_type"], Width: 20},
			{Title: localization.Labels["created_at"], Width: 10},
			{Title: localization.Labels["ethereum_address"], Width: 42},
		}
		compactDate = true
	}
	rows := make([]table.Row, 0, len(accounts))
	for _, account := range accounts {
		accountType := fmt.Sprintf("%s / %s", safeShort(string(account.SignerKind)), safeShort(string(account.State)))
		if len(columns) == 4 {
			rows = append(rows, table.Row{safeShort(account.AccountID), safeShort(account.Name), accountType, safeShort(account.Address)})
			continue
		}
		createdAt := account.CreatedAt.Format("2006-01-02 15:04")
		if compactDate {
			createdAt = account.CreatedAt.Format("2006-01-02")
		}
		rows = append(rows, table.Row{safeShort(account.AccountID), safeShort(account.Name), accountType, createdAt, safeShort(account.Address)})
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
	m.walletTable.SetWidth(max(20, m.width-8))
	if len(m.wallets) > 0 || len(m.accounts) > 0 {
		m.walletTable.SetHeight(contentAreaHeight)
	}

	if len(m.accounts) > 0 {
		columns, rows := accountTableLayout(m.width, m.accounts)
		m.walletTable.SetColumns(columns)
		m.walletTable.SetRows(rows)
	}
}

// Funções de inicialização

func (m *CLIModel) initCreateWallet() {
	m.backupChallenge = nil
	m.pendingAccount = nil
	m.backupError = ""

	// Initialize name input first
	m.nameInput = textinput.New()
	m.nameInput.Placeholder = "Digite o nome da wallet"
	m.nameInput.CharLimit = 50
	m.nameInput.Width = constants.PasswordWidth
	m.nameInput.Focus()
	m.currentView = constants.CreateWalletNameView

	m.createWordCountInput = textinput.New()
	m.createWordCountInput.Placeholder = "Word count"
	m.createWordCountInput.SetValue("12")
	m.createWordCountInput.CharLimit = 2
	m.createLanguageInput = textinput.New()
	m.createLanguageInput.Placeholder = "BIP39 language"
	m.createLanguageInput.SetValue("english")
	m.createLanguageInput.CharLimit = 32
	m.createPassphraseInput = textinput.New()
	m.createPassphraseInput.Placeholder = "Optional BIP39 passphrase"
	m.createPassphraseInput.CharLimit = constants.PasswordCharLimit
	m.createPassphraseInput.EchoMode = textinput.EchoPassword
	m.createPassphraseInput.EchoCharacter = '•'
	m.createDerivationPathInput = textinput.New()
	m.createDerivationPathInput.Placeholder = "EVM derivation path"
	m.createDerivationPathInput.SetValue("m/44'/60'/0'/0/0")
	m.createDerivationPathInput.CharLimit = 255
	m.createOptionsStage = 0
	m.createCustomPath = false
	m.configureCreateOptionList(0)

	m.backupConfirmationInput = textinput.New()
	m.backupConfirmationInput.Placeholder = localization.Labels["confirm_mnemonic"]
	m.backupConfirmationInput.CharLimit = 512
	m.backupConfirmationInput.Width = 80
	m.backupConfirmationInput.EchoMode = textinput.EchoPassword
	m.backupConfirmationInput.EchoCharacter = '•'

	// Initialize password input (will be used after name is entered)
	m.passwordInput = textinput.New()
	m.passwordInput.Placeholder = localization.Labels["enter_password"]
	m.passwordInput.CharLimit = constants.PasswordCharLimit
	m.passwordInput.Width = constants.PasswordWidth
	m.passwordInput.EchoMode = textinput.EchoPassword
	m.passwordInput.EchoCharacter = '•'
	m.passwordInput.Validate = func(s string) error {
		return validateStoragePasswordInput(s)
	}
	m.createPasswordConfirmationInput = textinput.New()
	m.createPasswordConfirmationInput.Placeholder = "Confirm storage password"
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
	accounts, err := m.Vault.ListAccounts(context.Background())
	if err != nil {
		m.err = errors.Wrap(err, 0)
		m.currentView = constants.DefaultView
		return
	}
	m.applyAccountList(accounts)
	m.currentView = constants.ListWalletsView
}

func (m *CLIModel) applyAccountList(accounts []wallet.AccountSummary) {
	m.accounts = accounts
	m.wallets = nil
	m.walletCount = len(accounts)
	columns, rows := accountTableLayout(m.width, accounts)
	m.walletTable = table.New(
		table.WithColumns(columns),
		table.WithRows(rows),
		table.WithFocused(true),
	)
	m.walletTable.SetWidth(max(20, m.width-8))
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
	m.backupPathInput.Placeholder = "Re-enter the derivation path"
	m.backupPathInput.CharLimit = 255
	m.backupPathInput.Width = 80
	m.backupLanguageInput = textinput.New()
	m.backupLanguageInput.Placeholder = "Re-enter the BIP39 language"
	m.backupLanguageInput.CharLimit = 32
	m.backupPassphraseInput = textinput.New()
	m.backupPassphraseInput.Placeholder = "Re-enter the BIP39 passphrase"
	m.backupPassphraseInput.CharLimit = constants.PasswordCharLimit
	m.backupPassphraseInput.EchoMode = textinput.EchoPassword
	m.backupPassphraseInput.EchoCharacter = '•'
	m.backupMaterialStage = 0
	m.backupWordAnswers = nil
}

func (m *CLIModel) initResumeBackup(accountID string) {
	m.resumeBackupAccountID = accountID
	m.passwordInput = textinput.New()
	m.passwordInput.Placeholder = localization.Labels["enter_password"]
	m.passwordInput.CharLimit = constants.PasswordCharLimit
	m.passwordInput.Width = constants.PasswordWidth
	m.passwordInput.EchoMode = textinput.EchoPassword
	m.passwordInput.EchoCharacter = '•'
	m.passwordInput.Focus()
	m.createPasswordConfirmationInput = textinput.New()
	m.createPasswordConfirmationInput.Placeholder = "Confirm storage password"
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
	m.clearVaultActionInputs()
	m.lastOperationNotice = ""
	m.currentPasswordInput = textinput.New()
	m.currentPasswordInput.Placeholder = "Current storage password"
	m.currentPasswordInput.CharLimit = constants.PasswordCharLimit
	m.currentPasswordInput.Width = constants.PasswordWidth
	m.currentPasswordInput.EchoMode = textinput.EchoPassword
	m.currentPasswordInput.EchoCharacter = '•'
	m.currentPasswordInput.Focus()
	m.newPasswordInput = textinput.New()
	m.newPasswordInput.Placeholder = "New password"
	m.newPasswordInput.CharLimit = constants.PasswordCharLimit
	m.newPasswordInput.Width = constants.PasswordWidth
	m.newPasswordInput.EchoMode = textinput.EchoPassword
	m.newPasswordInput.EchoCharacter = '•'
	m.confirmPasswordInput = textinput.New()
	m.confirmPasswordInput.Placeholder = "Confirm new password"
	m.confirmPasswordInput.CharLimit = constants.PasswordCharLimit
	m.confirmPasswordInput.Width = constants.PasswordWidth
	m.confirmPasswordInput.EchoMode = textinput.EchoPassword
	m.confirmPasswordInput.EchoCharacter = '•'
	m.exportDestinationInput = textinput.New()
	m.exportDestinationInput.Placeholder = "Absolute export destination"
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
			// If the last item (Back) is selected, return to the config menu
			if m.selectedMenu == len(m.menuItems)-1 {
				m.menuItems = NewConfigMenu()
				m.selectedMenu = 0
				m.currentView = constants.ConfigurationView
				return m, nil
			}

			// Otherwise, change the language
			// Extract the language code from the description (format: "language: XX")
			descParts := strings.Split(m.menuItems[m.selectedMenu].description, ": ")
			if len(descParts) < 2 {
				m.err = errors.Wrap(fmt.Errorf("invalid language selection format"), 0)
				return m, nil
			}

			selectedLang := strings.TrimSpace(descParts[1])

			// Update the configuration
			if m.currentConfig != nil && selectedLang != m.currentConfig.Language {
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

				// Return to the config menu
				m.menuItems = NewConfigMenu()
				m.selectedMenu = 0
				m.currentView = constants.ConfigurationView

				// Reinitialize localization with the new language
				err = localization.InitLocalization(newCfg)
				if err != nil {
					m.err = errors.Wrap(err, 0)
					return m, nil
				}

				// Return to the config menu
				m.menuItems = NewConfigMenu()
				m.selectedMenu = 0
				m.currentView = constants.ConfigurationView
			} else {
				// If no change or error, just return to the config menu
				m.menuItems = NewConfigMenu()
				m.selectedMenu = 0
				m.currentView = constants.ConfigurationView
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
			// If the last item (Back) is selected, return to the config menu
			if m.selectedMenu == len(m.menuItems)-1 {
				m.menuItems = NewConfigMenu()
				m.selectedMenu = 0
				m.currentView = constants.ConfigurationView
				return m, nil
			}

			// Otherwise, handle the selected option
			switch m.selectedMenu {
			case 0: // Add Network
				m.initAddNetwork()
				return m, nil
			case 1: // Network List
				m.initNetworkList()
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
		{Title: localization.Labels["id"], Width: idColWidth},
		{Title: "Nome", Width: nameColWidth},
		{Title: localization.Labels["wallet_type"], Width: typeColWidth},
		{Title: localization.Labels["created_at"], Width: createdAtColWidth},
		{Title: localization.Labels["ethereum_address"], Width: addressColWidth},
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
