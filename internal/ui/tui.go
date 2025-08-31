package ui

import (
	"blocowallet/internal/constants"
	"blocowallet/internal/wallet"
	"blocowallet/pkg/localization"
	"bytes"
	"fmt"
	"log"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/arsham/figurine/figurine"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/digitallyserviced/tdfgo/tdf"
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

func NewCLIModel(service *wallet.WalletService) *CLIModel {
	model := &CLIModel{
		Service:      service,
		currentView:  constants.SplashView,
		menuItems:    NewMenu(),
		selectedMenu: 0,
		styles:       createStyles(),
	}

	if err := initializeFont(model); err != nil {
		model.err = err
		return model
	}

	return model
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
			err = os.MkdirAll(customFontDir, os.ModePerm)
			if err != nil {
				log.Printf("Não foi possível criar o diretório de fontes personalizado: %v\n", err)
				// Continuar com as fontes embutidas
				customFontDir = ""
			}
		} else {
			log.Printf("Erro ao verificar o diretório de fontes personalizado: %v\n", err)
			customFontDir = ""
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
		log.Println("Erro ao carregar a lista de fontes configuradas:", err)
		// Se houver erro, escolher qualquer fonte disponível
		rand.NewSource(time.Now().UnixNano())
		selectedFontInfo := availableFonts[rand.Intn(len(availableFonts))]
		return loadSelectedFont(model, selectedFontInfo)
	}

	// Se não houver fontes configuradas, escolher qualquer fonte disponível
	if len(configuredFontNames) == 0 {
		log.Println("A lista de fontes configuradas está vazia, selecionando aleatoriamente.")
		rand.NewSource(time.Now().UnixNano())
		selectedFontInfo := availableFonts[rand.Intn(len(availableFonts))]
		return loadSelectedFont(model, selectedFontInfo)
	}

	// Selecionar uma fonte da lista configurada
	selectedName, err := selectRandomFont(configuredFontNames)
	if err != nil {
		log.Println("Erro ao selecionar uma fonte aleatoriamente:", err)
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
		log.Printf("Fonte '%s' não encontrada. Usando uma fonte aleatória como fallback.\n", selectedName)
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
		log.Println("Erro ao carregar a fonte:", err)
		return errors.Wrap(err, 0)
	}

	if len(fontFile.Fonts) == 0 {
		log.Printf("Nenhuma fonte carregada de '%s'\n", fontInfo.File)
		return errors.New("nenhuma fonte carregada")
	}

	// Armazenar a informação da fonte selecionada no modelo
	model.selectedFont = &fontFile.Fonts[0]
	model.fontInfo = fontInfo

	log.Printf("Fonte carregada com sucesso: %s\n", fontInfo.File)
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
		walletCountCmd(m.Service),
	)
}

func splashCmd() tea.Cmd {
	return tea.Tick(constants.SplashDuration, func(t time.Time) tea.Msg {
		return splashMsg{}
	})
}

func (m *CLIModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if msg == nil {
		return m, nil
	}

	// Tratar as teclas de navegação global (esc/backspace) antes de qualquer outro processamento
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.String() {
		case "esc":
			// Se estiver na tela de lista de wallets e tiver um diálogo de exclusão aberto,
			// não faça nada aqui e deixe o handler específico da view tratar
			if m.currentView == constants.ListWalletsView && m.deletingWallet != nil {
				// Não faz nada, deixa o handler específico tratar
			} else if m.currentView != constants.DefaultView && m.currentView != constants.SplashView {
				// Para a maioria das telas, voltar para o menu principal
				if m.currentView == constants.WalletDetailsView {
					// Comportamento específico para tela de detalhes: voltar para lista de wallets
					m.walletDetails = nil
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
		case "q":
			if m.currentView != constants.SplashView {
				return m, tea.Quit
			}
		}
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height

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
		// Apenas retornar o modelo sem fazer nada, pois a atualização já foi feita
		// Isso evita que a tela inteira seja redesenhada
		return m, nil

	case splashMsg:
		// Transitar para o menu principal após a splash screen
		m.currentView = constants.DefaultView
		// Iniciar o comando para buscar a quantidade de wallets
		return m, walletCountCmd(m.Service)
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
	case constants.CreateWalletView:
		return m.updateCreateWalletPassword(msg)
	case constants.ImportMethodSelectionView:
		return m.updateImportMethodSelection(msg)
	case constants.ImportWalletView:
		return m.updateImportWallet(msg)
	case constants.ImportPrivateKeyView:
		return m.updateImportPrivateKey(msg)
	case constants.ImportKeystoreView:
		return m.updateImportKeystore(msg)
	case constants.EnhancedImportView:
		return m.updateEnhancedImport(msg)
	case constants.ImportWalletPasswordView:
		return m.updateImportWalletPassword(msg)
	case constants.ListWalletsView:
		return m.updateListWallets(msg)
	case constants.WalletPasswordView:
		return m.updateWalletPassword(msg)
	case constants.WalletDetailsView:
		return m.updateWalletDetails(msg)
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
	default:
		m.currentView = constants.DefaultView
		return m, nil
	}
}

func (m *CLIModel) View() string {
	if m.err != nil {
		return m.styles.ErrorStyle.Render(fmt.Sprintf(localization.Labels["error_message"], m.err))
	}

	switch m.currentView {
	case constants.SplashView:
		return m.renderSplash()
	case constants.ListWalletsView:
		// Tratamento especial para a visualização de listagem de carteiras
		// para garantir que ela se encaixe corretamente no layout
		return m.renderListWalletsWithLayout()
	default:
		return m.renderMainView()
	}
}

// renderListWalletsWithLayout renderiza a tela de listagem de carteiras com o layout completo
func (m *CLIModel) renderListWalletsWithLayout() string {
	// Renderizar o cabeçalho da mesma forma que renderMainView
	var logoBuffer bytes.Buffer
	err := figurine.Write(&logoBuffer, "bloco", "Test1.flf")
	if err != nil {
		log.Println(errors.Wrap(err, 0))
		logoBuffer.WriteString("bloco")
	}
	renderedLogo := logoBuffer.String()

	walletCount := m.walletCount
	currentTime := time.Now().Format("02-01-2006 15:04:05")

	headerLeft := lipgloss.JoinVertical(
		lipgloss.Left,
		renderedLogo,
		fmt.Sprintf("Wallets: %d", walletCount),
		fmt.Sprintf("Date: %s", currentTime),
		fmt.Sprintf("Version: %s", localization.Labels["version"]),
	)

	menuItems := m.renderMenuItems()
	menuGrid := lipgloss.JoinVertical(lipgloss.Left, menuItems...)

	// Montar header
	headerContent := lipgloss.JoinHorizontal(
		lipgloss.Top,
		headerLeft,
		lipgloss.NewStyle().Width(m.width-lipgloss.Width(headerLeft)-lipgloss.Width(menuGrid)).Render(""),
		menuGrid,
	)

	// Renderizar header com altura fixa
	renderedHeader := m.styles.Header.Render(headerContent)
	headerHeight := lipgloss.Height(renderedHeader)

	// Preparar conteúdo do footer
	renderedFooter := m.renderStatusBar()
	footerHeight := lipgloss.Height(renderedFooter)

	// Calcular altura disponível para o conteúdo
	contentHeight := m.height - headerHeight - footerHeight - 2

	// Ajustar o tamanho da tabela para caber na área de conteúdo
	if contentHeight > 0 {
		// Reservar espaço para título e instruções
		titleAndInstructionsHeight := 4
		tableHeight := contentHeight - titleAndInstructionsHeight

		if tableHeight > 0 && len(m.wallets) > 0 {
			m.walletTable.SetHeight(tableHeight)
		}
	}

	// Obter conteúdo da visualização de carteiras
	content := m.viewListWallets()

	// Renderizar o conteúdo na área apropriada
	renderedContent := m.styles.Content.Height(contentHeight).Render(content)

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
	var menuItems []string
	for i, item := range m.menuItems {
		style := m.styles.MenuItem
		titleStyle := m.styles.MenuTitle
		if i == m.selectedMenu {
			style = m.styles.MenuSelected
			titleStyle = m.styles.SelectedTitle
		}
		menuText := fmt.Sprintf("%s\n%s", titleStyle.Render(item.title), m.styles.MenuDesc.Render(item.description))
		menuItems = append(menuItems, style.Render(menuText))
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
	case constants.CreateWalletView:
		return m.viewCreateWalletPassword()
	case constants.ImportMethodSelectionView:
		return m.viewImportMethodSelection()
	case constants.ImportWalletView:
		return m.viewImportWallet()
	case constants.ImportPrivateKeyView:
		return m.viewImportPrivateKey()
	case constants.ImportKeystoreView:
		return m.viewImportKeystore()
	case constants.EnhancedImportView:
		return m.viewEnhancedImport()
	case constants.ImportWalletPasswordView:
		return m.viewImportWalletPassword()
	case constants.ListWalletsView:
		return m.viewListWallets()
	case constants.WalletPasswordView:
		return m.viewWalletPassword()
	case constants.WalletDetailsView:
		return m.viewWalletDetails()
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
			// Proceed to password input
			m.passwordInput.Focus()
			m.currentView = constants.CreateWalletView
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

func (m *CLIModel) updateCreateWalletPassword(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "enter":
			password := strings.TrimSpace(m.passwordInput.Value())

			// Validar a complexidade da senha
			validationErr, isValid := wallet.ValidatePassword(password)
			if !isValid {
				m.err = errors.Wrap(errors.New(validationErr.GetErrorMessage()), 0)
				log.Println(m.err.(*errors.Error).ErrorStack())
				return m, nil
			}

			name := strings.TrimSpace(m.nameInput.Value())
			walletDetails, err := m.Service.CreateWallet(name, password)
			if err != nil {
				m.err = errors.Wrap(err, 0)
				log.Println(m.err.(*errors.Error).ErrorStack())
				m.currentView = constants.DefaultView
				return m, nil
			}
			m.walletDetails = walletDetails
			// Ensure networks/config are loaded for balances rendering
			if err := m.ensureConfigAndNetworksLoaded(); err != nil {
				// Log error but continue execution - network loading is non-fatal
				log.Printf("Warning: failed to load networks/config: %v", err)
			}
			m.currentView = constants.WalletDetailsView

			// Atualizar a contagem de wallets
			return m, m.refreshWalletsTable()
		case "esc":
			// Go back to name input
			m.nameInput.Focus()
			m.currentView = constants.CreateWalletNameView
			return m, nil
		default:
			var cmd tea.Cmd
			m.passwordInput, cmd = m.passwordInput.Update(msg)
			return m, cmd
		}
	}
	return m, nil
}

func (m *CLIModel) updateImportWallet(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "enter":
			word := strings.TrimSpace(m.textInputs[m.importStage].Value())
			if word == "" {
				m.err = errors.Wrap(errors.New(localization.Labels["all_words_required"]), 0)
				log.Println(m.err.(*errors.Error).ErrorStack())
				return m, nil
			}
			m.importWords[m.importStage] = word
			m.textInputs[m.importStage].Blur()
			m.importStage++
			if m.importStage < len(m.textInputs) {
				m.textInputs[m.importStage].Focus()
			} else {
				m.passwordInput = textinput.New()
				m.passwordInput.Placeholder = localization.Labels["enter_password"]
				m.passwordInput.CharLimit = constants.PasswordCharLimit
				m.passwordInput.Width = constants.PasswordWidth
				m.passwordInput.EchoMode = textinput.EchoPassword
				m.passwordInput.EchoCharacter = '•'
				m.passwordInput.Validate = func(s string) error {
					_, isValid := wallet.ValidatePassword(s)
					if !isValid && s != "" {
						return fmt.Errorf("")
					}
					return nil
				}
				m.passwordInput.Focus()
				m.currentView = constants.ImportWalletPasswordView
			}
		case "esc":
			m.currentView = constants.DefaultView
		default:
			var cmd tea.Cmd
			m.textInputs[m.importStage], cmd = m.textInputs[m.importStage].Update(msg)
			return m, cmd
		}
	}
	return m, nil
}

func (m *CLIModel) updateImportWalletPassword(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "enter":
			password := strings.TrimSpace(m.passwordInput.Value())

			// Validar a complexidade da senha
			validationErr, isValid := wallet.ValidatePassword(password)
			if !isValid {
				m.err = errors.Wrap(errors.New(validationErr.GetErrorMessage()), 0)
				log.Println(m.err.(*errors.Error).ErrorStack())
				return m, nil
			}

			var walletDetails *wallet.WalletDetails
			var err error

			// Use a default name based on the import method
			var name string
			if m.currentView == constants.ImportWalletPasswordView && len(m.privateKeyInput.Value()) > 0 {
				name = "Imported Private Key Wallet"
			} else if m.mnemonic != "" && m.currentView == constants.ImportWalletPasswordView {
				// If mnemonic field contains a path to a keystore file
				name = "Imported Keystore Wallet"
			} else {
				name = "Imported Mnemonic Wallet"
			}

			// Check which import method we're using
			if m.currentView == constants.ImportWalletPasswordView && len(m.privateKeyInput.Value()) > 0 {
				// Import from private key
				privateKey := strings.TrimSpace(m.privateKeyInput.Value())
				walletDetails, err = m.Service.ImportWalletFromPrivateKey(name, privateKey, password)
			} else if m.mnemonic != "" && m.currentView == constants.ImportWalletPasswordView {
				// Import from keystore file
				keystorePath := m.mnemonic // We stored the keystore path in the mnemonic field
				walletDetails, err = m.Service.ImportWalletFromKeystore(name, keystorePath, password)
			} else {
				// Import from mnemonic
				mnemonic := strings.Join(m.importWords, " ")
				walletDetails, err = m.Service.ImportWallet(name, mnemonic, password)
			}

			if err != nil {
				// Check if it's a KeystoreImportError
				if keystoreErr, ok := err.(*wallet.KeystoreImportError); ok {
					// Get localized error message
					localizedMsg := localization.FormatKeystoreErrorWithField(
						keystoreErr.GetLocalizedMessage(),
						keystoreErr.Field,
					)

					// Add recovery suggestion based on error type
					var recoverySuggestion string
					switch keystoreErr.Type {
					case wallet.ErrorFileNotFound:
						recoverySuggestion = localization.Labels["keystore_recovery_file_not_found"]
					case wallet.ErrorInvalidJSON:
						recoverySuggestion = localization.Labels["keystore_recovery_invalid_json"]
					case wallet.ErrorInvalidKeystore:
						recoverySuggestion = localization.Labels["keystore_recovery_invalid_structure"]
					case wallet.ErrorIncorrectPassword:
						recoverySuggestion = localization.Labels["keystore_recovery_incorrect_password"]
						// Stay on password screen for password errors
						m.err = errors.Wrap(fmt.Errorf("%s\n%s", localizedMsg, recoverySuggestion), 0)
						log.Println(m.err.(*errors.Error).ErrorStack())
						return m, nil
					default:
						recoverySuggestion = localization.Labels["keystore_recovery_general"]
					}

					m.err = errors.Wrap(fmt.Errorf("%s\n%s", localizedMsg, recoverySuggestion), 0)
				} else {
					// Detect duplicate wallet conflicts and show context-aware localized message
					if dupErr, ok := err.(*wallet.DuplicateWalletError); ok {
						// Use the conflict type as both the import method context and conflict type when unknown
						formatted := localization.FormatDuplicateImportError(dupErr.Type, dupErr.Type, dupErr.Address)
						m.err = errors.Wrap(errors.New(formatted), 0)
					} else {
						m.err = errors.Wrap(err, 0)
					}
				}

				log.Println(m.err.(*errors.Error).ErrorStack())
				m.currentView = constants.DefaultView
				return m, nil
			}

			m.walletDetails = walletDetails
			m.currentView = constants.WalletDetailsView

			// Atualizar a contagem de wallets
			return m, m.refreshWalletsTable()
		case "esc":
			m.currentView = constants.DefaultView
		default:
			var cmd tea.Cmd
			m.passwordInput, cmd = m.passwordInput.Update(msg)
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
			// Usar o menu de importação para determinar a ação baseada na seleção
			switch m.selectedMenu {
			case 0: // Primeira opção: Importar por frase mnemônica
				// Preparar campos de entrada para as 12 palavras
				m.textInputs = make([]textinput.Model, constants.MnemonicWordCount)
				m.importWords = make([]string, constants.MnemonicWordCount)
				for i := 0; i < constants.MnemonicWordCount; i++ {
					ti := textinput.New()
					ti.Placeholder = fmt.Sprintf("%s %d", localization.Labels["word"], i+1)
					ti.CharLimit = 50
					ti.Width = 30
					if i == 0 {
						ti.Focus()
					}
					m.textInputs[i] = ti
				}
				m.importStage = 0
				m.currentView = constants.ImportWalletView

			case 1: // Segunda opção: Importar por chave privada
				m.privateKeyInput = textinput.New()
				m.privateKeyInput.Placeholder = localization.Labels["enter_private_key"]
				m.privateKeyInput.CharLimit = 66 // 0x + 64 caracteres hexadecimais
				m.privateKeyInput.Width = 66
				m.privateKeyInput.Focus()
				m.currentView = constants.ImportPrivateKeyView

			case 2: // Terceira opção: Importar por arquivo keystore
				cmd := m.initEnhancedImport()
				return m, cmd

			case 3: // Quarta opção: Voltar ao menu principal
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

func (m *CLIModel) updateImportPrivateKey(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "enter":
			privateKey := strings.TrimSpace(m.privateKeyInput.Value())
			if privateKey == "" {
				m.err = errors.Wrap(errors.New(localization.Labels["invalid_private_key"]), 0)
				log.Println(m.err.(*errors.Error).ErrorStack())
				return m, nil
			}

			// Move to password input screen
			m.passwordInput = textinput.New()
			m.passwordInput.Placeholder = localization.Labels["enter_password"]
			m.passwordInput.CharLimit = constants.PasswordCharLimit
			m.passwordInput.Width = constants.PasswordWidth
			m.passwordInput.EchoMode = textinput.EchoPassword
			m.passwordInput.EchoCharacter = '•'
			m.passwordInput.Validate = func(s string) error {
				_, isValid := wallet.ValidatePassword(s)
				if !isValid && s != "" {
					return fmt.Errorf("")
				}
				return nil
			}
			m.passwordInput.Focus()
			m.currentView = constants.ImportWalletPasswordView

		case "esc":
			m.currentView = constants.DefaultView
		default:
			var cmd tea.Cmd
			m.privateKeyInput, cmd = m.privateKeyInput.Update(msg)

			// Update suggestions as the user types
			if msg.Type == tea.KeyRunes || msg.Type == tea.KeyBackspace || msg.Type == tea.KeyDelete {
				// Get current path
				currentPath := m.privateKeyInput.Value()
				if currentPath == "" {
					currentPath = "."
				}

				// Get the directory and partial filename
				dir := filepath.Dir(currentPath)
				if dir == "." && !strings.HasPrefix(currentPath, "./") && !strings.HasPrefix(currentPath, "/") {
					dir = currentPath
				}

				// Read the directory
				files, err := os.ReadDir(dir)
				if err == nil {
					// Find matching files
					var matches []string
					partial := filepath.Base(currentPath)
					for _, file := range files {
						if strings.HasPrefix(file.Name(), partial) {
							fullPath := filepath.Join(dir, file.Name())
							if file.IsDir() {
								fullPath += "/"
							}
							matches = append(matches, fullPath)
						}
					}

					// Set all matches as suggestions
					if len(matches) > 0 {
						m.privateKeyInput.SetSuggestions(matches)
					}
				}
			}

			return m, cmd
		}
	}
	return m, nil
}

func (m *CLIModel) updateImportKeystore(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "enter":
			keystorePath := strings.TrimSpace(m.privateKeyInput.Value())
			if keystorePath == "" {
				// Use specific error type for empty path
				keystoreErr := wallet.NewKeystoreImportError(
					wallet.ErrorFileNotFound,
					"No keystore path provided",
					nil,
				)
				m.err = errors.Wrap(errors.New(localization.FormatKeystoreErrorWithField(
					keystoreErr.GetLocalizedMessage(),
					"",
				)), 0)
				log.Println(m.err.(*errors.Error).ErrorStack())
				return m, nil
			}

			// Check if file exists
			if _, err := os.Stat(keystorePath); os.IsNotExist(err) {
				// Use specific error type for file not found
				keystoreErr := wallet.NewKeystoreImportError(
					wallet.ErrorFileNotFound,
					fmt.Sprintf("Keystore file not found at path: %s", keystorePath),
					err,
				)
				m.err = errors.Wrap(errors.New(localization.FormatKeystoreErrorWithField(
					keystoreErr.GetLocalizedMessage(),
					"",
				)), 0)
				log.Println(m.err.(*errors.Error).ErrorStack())
				return m, nil
			}

			// Check file size to prevent memory exhaustion
			fileInfo, err := os.Stat(keystorePath)
			if err != nil {
				keystoreErr := wallet.NewKeystoreImportError(
					wallet.ErrorFileNotFound,
					fmt.Sprintf("Error accessing keystore file: %s", keystorePath),
					err,
				)
				m.err = errors.Wrap(errors.New(localization.FormatKeystoreErrorWithField(
					keystoreErr.GetLocalizedMessage(),
					"",
				)), 0)
				log.Println(m.err.(*errors.Error).ErrorStack())
				return m, nil
			}

			// Limit file size to 100KB (reasonable for keystores)
			const maxKeystoreSize = 100 * 1024 // 100KB
			if fileInfo.Size() > maxKeystoreSize {
				keystoreErr := wallet.NewKeystoreImportError(
					wallet.ErrorInvalidKeystore,
					fmt.Sprintf("Keystore file too large: %d bytes (max %d bytes)", fileInfo.Size(), maxKeystoreSize),
					nil,
				)
				m.err = errors.Wrap(errors.New(localization.FormatKeystoreErrorWithField(
					keystoreErr.GetLocalizedMessage(),
					"",
				)), 0)
				log.Println(m.err.(*errors.Error).ErrorStack())
				return m, nil
			}

			// Store the keystore path for later use
			m.mnemonic = keystorePath // Reusing mnemonic field to store keystore path

			// Clear the privateKeyInput to avoid confusion in updateImportWalletPassword
			m.privateKeyInput.SetValue("")

			// Move to password input screen
			m.passwordInput = textinput.New()
			m.passwordInput.Placeholder = localization.Labels["enter_password"]
			m.passwordInput.CharLimit = constants.PasswordCharLimit
			m.passwordInput.Width = constants.PasswordWidth
			m.passwordInput.EchoMode = textinput.EchoPassword
			m.passwordInput.EchoCharacter = '•'
			m.passwordInput.Validate = func(s string) error {
				_, isValid := wallet.ValidatePassword(s)
				if !isValid && s != "" {
					return fmt.Errorf("")
				}
				return nil
			}
			m.passwordInput.Focus()
			m.currentView = constants.ImportWalletPasswordView

		case "esc":
			m.currentView = constants.ImportMethodSelectionView
		case "tab":
			// Implement path autocomplete
			currentPath := m.privateKeyInput.Value()
			if currentPath == "" {
				currentPath = "."
			}

			// Get the directory and partial filename
			dir := filepath.Dir(currentPath)
			if dir == "." && !strings.HasPrefix(currentPath, "./") && !strings.HasPrefix(currentPath, "/") {
				dir = currentPath
			}

			// Read the directory
			files, err := os.ReadDir(dir)
			if err != nil {
				return m, nil
			}

			// Find matching files
			var matches []string
			partial := filepath.Base(currentPath)
			for _, file := range files {
				if strings.HasPrefix(file.Name(), partial) {
					fullPath := filepath.Join(dir, file.Name())
					if file.IsDir() {
						fullPath += "/"
					}
					matches = append(matches, fullPath)
				}
			}

			// Set all matches as suggestions
			if len(matches) > 0 {
				m.privateKeyInput.SetSuggestions(matches)

				// If there's exactly one match, use it
				if len(matches) == 1 {
					m.privateKeyInput.SetValue(matches[0])
				}
			}

			// Let the textinput component handle the tab key
			var cmd tea.Cmd
			m.privateKeyInput, cmd = m.privateKeyInput.Update(msg)
			return m, cmd
		default:
			var cmd tea.Cmd
			m.privateKeyInput, cmd = m.privateKeyInput.Update(msg)

			// Update suggestions as the user types
			if msg.Type == tea.KeyRunes || msg.Type == tea.KeyBackspace || msg.Type == tea.KeyDelete {
				// Get current path
				currentPath := m.privateKeyInput.Value()
				if currentPath == "" {
					currentPath = "."
				}

				// Get the directory and partial filename
				dir := filepath.Dir(currentPath)
				if dir == "." && !strings.HasPrefix(currentPath, "./") && !strings.HasPrefix(currentPath, "/") {
					dir = currentPath
				}

				// Read the directory
				files, err := os.ReadDir(dir)
				if err == nil {
					// Find matching files
					var matches []string
					partial := filepath.Base(currentPath)
					for _, file := range files {
						if strings.HasPrefix(file.Name(), partial) {
							fullPath := filepath.Join(dir, file.Name())
							if file.IsDir() {
								fullPath += "/"
							}
							matches = append(matches, fullPath)
						}
					}

					// Set all matches as suggestions
					if len(matches) > 0 {
						m.privateKeyInput.SetSuggestions(matches)
					}
				}
			}

			return m, cmd
		}
	}
	return m, nil
}

func (m *CLIModel) updateListWallets(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Diálogo de confirmação de exclusão
	if m.deletingWallet != nil {
		if keyMsg, ok := msg.(tea.KeyMsg); ok {
			switch keyMsg.String() {
			case "left", "h":
				if m.dialogButtonIndex > 0 {
					m.dialogButtonIndex = 0
				}
				return m, nil
			case "right", "l":
				if m.dialogButtonIndex < 1 {
					m.dialogButtonIndex = 1
				}
				return m, nil
			case "enter":
				walletToDelete := m.deletingWallet
				shouldDelete := m.dialogButtonIndex == 0

				// Limpar a referência do diálogo antes de qualquer outra operação
				m.deletingWallet = nil
				m.dialogButtonIndex = 0

				if shouldDelete {
					// Executar a exclusão
					err := m.Service.DeleteWallet(walletToDelete)
					if err != nil {
						m.err = errors.Wrap(err, 0)
					}

					// Recarregar a lista de wallets
					wallets, err := m.Service.GetAllWallets()
					if err == nil {
						m.wallets = wallets
						m.walletCount = len(wallets)

						// Reconstruir linhas da tabela
						rows := make([]table.Row, len(wallets))
						for i, w := range wallets {
							rows[i] = table.Row{fmt.Sprintf("%d", w.ID), w.Name, w.Address}
						}
						m.walletTable.SetRows(rows)
					}
				}

				// Forçar uma atualização da tela
				return m, m.refreshWalletsTable()
			case "esc":
				// Limpar a referência do diálogo e forçar atualização
				m.deletingWallet = nil
				m.dialogButtonIndex = 0
				// Forçar uma atualização da tela
				return m, m.refreshWalletsTable()
			}
		}
		return m, nil
	}

	// Continuar com o código existente para quando não houver diálogo
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "d", "delete":
			// Only try to access the table if there are wallets
			if len(m.wallets) > 0 {
				selectedRow := m.walletTable.SelectedRow()
				if len(selectedRow) > 4 {
					address := selectedRow[4]
					for i, w := range m.wallets {
						if w.Address == address {
							m.deletingWallet = &m.wallets[i]
							return m, nil
						}
					}
				}
			}
		case "enter":
			// Only try to access the table if there are wallets
			if len(m.wallets) > 0 {
				selectedRow := m.walletTable.SelectedRow()
				if len(selectedRow) > 4 {
					address := selectedRow[4]
					// Buscar wallet pela address
					for _, w := range m.wallets {
						if w.Address == address {
							m.selectedWallet = &w
							m.initWalletPassword()
							return m, nil
						}
					}
				}
			}
		case "esc":
			m.currentView = constants.DefaultView
			return m, nil
		}
	}

	// Atualizar a tabela com a mensagem apenas se houver wallets
	if len(m.wallets) > 0 {
		var cmd tea.Cmd
		m.walletTable, cmd = m.walletTable.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *CLIModel) updateWalletPassword(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "enter":
			password := strings.TrimSpace(m.passwordInput.Value())
			if password == "" {
				m.err = errors.Wrap(errors.New(localization.Labels["password_cannot_be_empty"]), 0)
				log.Println(m.err.(*errors.Error).ErrorStack())
				m.currentView = constants.DefaultView
				return m, nil
			}
			walletDetails, err := m.Service.LoadWallet(m.selectedWallet, password)
			if err != nil {
				m.err = errors.Wrap(err, 0)
				log.Println(m.err.(*errors.Error).ErrorStack())
				m.currentView = constants.DefaultView
				return m, nil
			}
			m.walletDetails = walletDetails
			m.currentView = constants.WalletDetailsView
		case "esc":
			m.currentView = constants.DefaultView
		default:
			var cmd tea.Cmd
			m.passwordInput, cmd = m.passwordInput.Update(msg)
			return m, cmd
		}
	}
	return m, nil
}

func (m *CLIModel) updateWalletDetails(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			m.walletDetails = nil
			m.currentView = constants.ListWalletsView

			// Ensure the wallet list is properly initialized before showing it
			wallets, err := m.Service.GetAllWallets()
			if err == nil {
				m.wallets = wallets
				m.walletCount = len(wallets)

				// Always rebuild the table, even if there are no wallets
				// The rebuildWalletsTable method already has a check for empty wallets
				m.rebuildWalletsTable()
			}

			return m, nil // Return explícito para consumir o evento de teclado
		}
	}
	return m, nil
}

// updateEnhancedImport handles user input in the enhanced import view
func (m *CLIModel) updateEnhancedImport(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.enhancedImportState == nil {
		// Initialize if not already done
		m.initEnhancedImport()
		return m, nil
	}

	// Handle enhanced import specific messages
	switch msg := msg.(type) {
	case ImportBatchCompleteMsg:
		// Import batch completed
		err := m.enhancedImportState.CompleteImport(msg.Results)
		if err != nil {
			m.err = errors.Wrap(err, 0)
			m.currentView = constants.DefaultView
		}
		return m, nil

	case ImportProgressUpdateMsg:
		// Update progress
		m.enhancedImportState.UpdateProgress(msg.Progress)

		// Collect commands to execute
		var cmds []tea.Cmd

		// Always continue listening for more progress updates if import is still in progress
		if m.enhancedImportState != nil && m.enhancedImportState.GetCurrentPhase() == PhaseImporting {
			cmds = append(cmds, m.listenForProgressUpdates())
		}

		// Handle any pending commands from progress update
		if cmd := m.enhancedImportState.GetPendingCommand(); cmd != nil {
			cmds = append(cmds, cmd)
		}

		return m, tea.Batch(cmds...)

	case ContinueListeningMsg:
		// Continue listening for progress updates if import is still in progress
		if m.enhancedImportState != nil && m.enhancedImportState.GetCurrentPhase() == PhaseImporting {
			return m, m.listenForProgressUpdates()
		}
		return m, nil

	case PasswordRequestMsg:
		// Handle password request
		err := m.enhancedImportState.HandlePasswordRequest(msg.Request)
		if err != nil {
			m.err = errors.Wrap(err, 0)
			m.currentView = constants.DefaultView
		}

		// Continue listening for more password requests if import is still in progress
		if m.enhancedImportState != nil &&
			(m.enhancedImportState.GetCurrentPhase() == PhaseImporting ||
				m.enhancedImportState.GetCurrentPhase() == PhasePasswordInput) {
			return m, m.listenForPasswordRequests()
		}
		return m, nil

	case ReturnToFileSelectionMsg:
		// Return to file selection phase
		err := m.enhancedImportState.TransitionToPhase(PhaseFileSelection)
		if err != nil {
			m.err = errors.Wrap(err, 0)
			m.currentView = constants.DefaultView
		}
		return m, nil

	case ReturnToMenuMsg:
		// Return to main menu
		m.enhancedImportState = nil
		m.currentView = constants.DefaultView
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			// Handle escape key based on current phase
			phase := m.enhancedImportState.GetCurrentPhase()
			switch phase {
			case PhaseFileSelection:
				// Return to main menu
				m.enhancedImportState = nil
				m.currentView = constants.DefaultView
				return m, nil
			case PhaseImporting:
				// Cancel import
				err := m.enhancedImportState.CancelImport()
				if err != nil {
					m.err = errors.Wrap(err, 0)
				}
				return m, nil
			case PhasePasswordInput:
				// Cancel password input
				err := m.enhancedImportState.CancelPasswordInput()
				if err != nil {
					m.err = errors.Wrap(err, 0)
				}
				return m, nil
			case PhaseComplete, PhaseCancelled:
				// Return to main menu
				m.enhancedImportState = nil
				m.currentView = constants.DefaultView
				return m, nil
			}
		case "enter":
			// Handle enter key based on current phase
			phase := m.enhancedImportState.GetCurrentPhase()
			switch phase {
			case PhaseFileSelection:
				// Start import if files are selected
				if len(m.enhancedImportState.SelectedFiles) > 0 || m.enhancedImportState.SelectedDir != "" {
					err := m.enhancedImportState.StartImport()
					if err != nil {
						m.err = errors.Wrap(err, 0)
						return m, nil
					}
					// Start the import batch processing and progress listening
					return m, tea.Batch(
						m.enhancedImportState.ProcessImportBatch(),
						m.listenForProgressUpdates(),
						m.listenForPasswordRequests(),
					)
				}
			case PhaseComplete, PhaseCancelled:
				// Return to main menu
				m.enhancedImportState = nil
				m.currentView = constants.DefaultView
				return m, nil
			}
		}
	}

	// Delegate to enhanced import state
	var cmd tea.Cmd
	_, cmd = m.enhancedImportState.Update(msg)
	return m, cmd
}

func (m *CLIModel) updateTableDimensions() {
	if m.currentView != constants.ListWalletsView || len(m.wallets) == 0 {
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
	m.walletTable.SetWidth(m.width - 12)
	if len(m.wallets) > 0 {
		m.walletTable.SetHeight(contentAreaHeight)
	}

	// Calcular larguras das colunas
	idColWidth := 10
	nameColWidth := 20
	typeColWidth := 20
	createdAtColWidth := 20
	// Aumentar a margem para evitar quebra de linha
	addressColWidth := m.width - idColWidth - nameColWidth - typeColWidth - createdAtColWidth - 20

	if addressColWidth < 20 {
		addressColWidth = 20
	}

	// Atualizar colunas - manter consistente com initListWallets e rebuildWalletsTable
	m.walletTable.SetColumns([]table.Column{
		{Title: localization.Labels["id"], Width: idColWidth},
		{Title: "Nome", Width: nameColWidth},
		{Title: localization.Labels["wallet_type"], Width: typeColWidth},
		{Title: localization.Labels["created_at"], Width: createdAtColWidth},
		{Title: localization.Labels["ethereum_address"], Width: addressColWidth},
	})
}

// Funções de inicialização

func (m *CLIModel) initCreateWallet() {
	m.mnemonic, _ = wallet.GenerateMnemonic()

	// Initialize name input first
	m.nameInput = textinput.New()
	m.nameInput.Placeholder = "Digite o nome da wallet"
	m.nameInput.CharLimit = 50
	m.nameInput.Width = constants.PasswordWidth
	m.nameInput.Focus()
	m.currentView = constants.CreateWalletNameView

	// Initialize password input (will be used after name is entered)
	m.passwordInput = textinput.New()
	m.passwordInput.Placeholder = localization.Labels["enter_password"]
	m.passwordInput.CharLimit = constants.PasswordCharLimit
	m.passwordInput.Width = constants.PasswordWidth
	m.passwordInput.EchoMode = textinput.EchoPassword
	m.passwordInput.EchoCharacter = '•'
	m.passwordInput.Validate = func(s string) error {
		_, isValid := wallet.ValidatePassword(s)
		if !isValid && s != "" {
			return fmt.Errorf("")
		}
		return nil
	}
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

// initEnhancedImport initializes the enhanced import view
func (m *CLIModel) initEnhancedImport() tea.Cmd {
	// Create batch import service
	batchService := wallet.NewBatchImportService(m.Service)

	// Initialize enhanced import state
	m.enhancedImportState = NewEnhancedImportState(batchService, m.styles)

	// Set current view
	m.currentView = constants.EnhancedImportView

	// Initialize the enhanced import state (which will initialize the file picker)
	return m.enhancedImportState.Init()
}

func (m *CLIModel) initListWallets() {
	wallets, err := m.Service.GetAllWallets()
	if err != nil {
		m.err = errors.Wrap(fmt.Errorf("%s: %v", localization.Labels["error_loading_wallets"], err), 0)
		log.Println(m.err.(*errors.Error).ErrorStack())
		m.currentView = constants.DefaultView
		return
	}
	m.wallets = wallets

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
			w.Name,
			walletType,
			createdAt,
			w.Address,
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
	if len(m.wallets) > 0 {
		m.walletTable.SetHeight(contentAreaHeight)
	}

	// Atualizar dimensões da tabela
	m.updateTableDimensions()

	m.currentView = constants.ListWalletsView
}

func (m *CLIModel) initWalletPassword() {
	m.passwordInput = textinput.New()
	m.passwordInput.Placeholder = localization.Labels["enter_wallet_password"]
	m.passwordInput.CharLimit = constants.PasswordCharLimit
	m.passwordInput.Width = constants.PasswordWidth
	m.passwordInput.EchoMode = textinput.EchoPassword
	m.passwordInput.EchoCharacter = '•'
	m.passwordInput.Validate = func(s string) error {
		_, isValid := wallet.ValidatePassword(s)
		if !isValid && s != "" {
			return fmt.Errorf("")
		}
		return nil
	}
	m.passwordInput.Focus()
	m.currentView = constants.WalletPasswordView
}

// initLanguageSelection initializes the language selection view
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
type walletsRefreshedMsg struct{}

func (m *CLIModel) refreshWalletsTable() tea.Cmd {
	return func() tea.Msg {
		// Recarregar a lista de wallets do serviço
		wallets, err := m.Service.GetAllWallets()
		if err != nil {
			m.err = errors.Wrap(err, 0)
			return nil
		}

		// Atualizar a lista de wallets no modelo
		m.wallets = wallets

		// Atualizar a contagem de wallets
		m.walletCount = len(wallets)

		// Se houver wallets, reconstruir a tabela completamente
		// para garantir que ela seja inicializada corretamente
		if len(m.wallets) > 0 {
			m.rebuildWalletsTable()
		}

		// Retornar uma mensagem personalizada para indicar que a lista foi atualizada
		return walletsRefreshedMsg{}
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
			w.Name,
			walletType,
			createdAt,
			w.Address,
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

// listenForProgressUpdates creates a command that listens for progress updates
func (m *CLIModel) listenForProgressUpdates() tea.Cmd {
	if m.enhancedImportState == nil {
		return nil
	}

	return func() tea.Msg {
		progressChan := m.enhancedImportState.GetProgressChan()

		select {
		case progress, ok := <-progressChan:
			if !ok {
				// Channel closed, no more progress updates
				return nil
			}
			return ImportProgressUpdateMsg{Progress: progress}
		case <-time.After(1 * time.Second): // Increased timeout to 1 second
			// Timeout - continue listening by returning a special message
			return ContinueListeningMsg{}
		}
	}
}

// listenForPasswordRequests creates a command that listens for password requests
func (m *CLIModel) listenForPasswordRequests() tea.Cmd {
	if m.enhancedImportState == nil {
		return nil
	}

	return func() tea.Msg {
		passwordRequestChan := m.enhancedImportState.GetPasswordRequestChan()

		select {
		case request, ok := <-passwordRequestChan:
			if !ok {
				// Channel closed, no more password requests
				return nil
			}
			return PasswordRequestMsg{Request: request}
		case <-time.After(100 * time.Millisecond):
			// Timeout - return nil to avoid blocking
			return nil
		}
	}
}
