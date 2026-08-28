package ui

import (
	"blocowallet/internal/constants"
	"blocowallet/internal/terminal"
	"blocowallet/internal/wallet"
	"blocowallet/pkg/localization"
	"bytes"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/arsham/figurine/figurine"
	"github.com/charmbracelet/lipgloss"
	"github.com/digitallyserviced/tdfgo/tdf"
)

func renderHeaderLogo() string {
	var buffer bytes.Buffer
	if err := figurine.Write(&buffer, "bloco", "Test1.flf"); err != nil {
		return "bloco"
	}
	logo := terminal.SanitizeStyledBlock(strings.TrimSpace(buffer.String()), 12, 120)
	if logo == "" {
		return "bloco"
	}
	return logo
}

func formatDisplayTime(value time.Time) string {
	if value.IsZero() {
		return "--"
	}
	return value.Format("02-01-2006 15:04:05")
}

func formatNativeAmount(amount *big.Int, decimals int) string {
	if amount == nil {
		return "unavailable"
	}
	if decimals <= 0 {
		return amount.String()
	}
	digits := amount.String()
	for len(digits) <= decimals {
		digits = "0" + digits
	}
	integer := digits[:len(digits)-decimals]
	fraction := strings.TrimRight(digits[len(digits)-decimals:], "0")
	if fraction == "" {
		return integer
	}
	return integer + "." + fraction
}

// viewCreateWalletName renderiza a visualização de entrada do nome da wallet
func (m *CLIModel) viewCreateWalletName() string {
	if localization.Labels == nil {
		return "Localization labels not initialized."
	}

	var view strings.Builder
	view.WriteString(
		lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#00FF00")).Render("Criar Nova Wallet") + "\n\n" +
			"Digite o nome para sua nova wallet:" + "\n\n" +
			m.nameInput.View() + "\n\n" +
			localization.Labels["press_enter"],
	)
	return view.String()
}

func (m *CLIModel) viewCreateWalletOptions() string {
	title := lipgloss.NewStyle().Bold(true).Render("Configure BIP39 account")
	progress := fmt.Sprintf("Step %d/4", m.createOptionsStage+1)
	var content string
	switch {
	case m.createOptionsStage == 2:
		content = "Optional BIP39 passphrase\n\n" + m.createPassphraseInput.View() + "\n\nLeave blank for no passphrase. This value cannot be recovered from the mnemonic."
	case m.createOptionsStage == 3 && m.createCustomPath:
		content = "Custom EVM derivation path\n\n" + m.createDerivationPathInput.View() + "\n\nThe path must remain inside the EVM BIP44 namespace m/44'/60'."
	default:
		content = m.createOptionList.View()
	}
	view := title + "\n\n" + progress + "\n\n" + content + "\n\nPress Enter to continue or Esc to cancel."
	if m.createPasswordError != "" {
		view += "\n\n" + m.styles.ErrorStyle.Render(m.createPasswordError)
	}
	return view
}

func (m *CLIModel) viewCreateWalletBackup() string {
	if localization.Labels == nil {
		return "Localization labels not initialized."
	}

	backupWordValues := strings.Fields(m.mnemonic)
	confirmationLabel := localization.Labels["confirm_mnemonic"]
	confirmationInput := m.backupConfirmationInput.View()
	materialNotice := ""
	if m.Vault != nil && m.backupChallenge != nil {
		backupWordValues = append([]string(nil), m.backupChallenge.Words...)
		indices := make([]string, 0, len(m.backupChallenge.RequiredWordIndices))
		for _, index := range m.backupChallenge.RequiredWordIndices {
			indices = append(indices, fmt.Sprintf("#%d", index+1))
		}
		confirmationLabel = fmt.Sprintf("Confirm only the requested words in order (%s):", strings.Join(indices, ", "))
		materialNotice = fmt.Sprintf("Backup metadata (no re-entry required):\nPath: %s\nLanguage: %s\n", m.backupChallenge.DerivationPath, m.backupChallenge.BIP39Language)
		if m.backupChallenge.RequiresMaterialConfirmation {
			materialNotice += "Back up any configured BIP39 passphrase separately; it cannot be recovered from these words.\n"
		}
		materialNotice += "\n"
	}

	var view strings.Builder
	view.WriteString(
		lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#00FF00")).Render(localization.Labels["mnemonic_phrase"]) + "\n\n" +
			renderMnemonicCards(backupWordValues, m.width-8) + "\n\n" +
			materialNotice + confirmationLabel + "\n\n" +
			confirmationInput + "\n\n",
	)
	if m.backupError != "" {
		view.WriteString(m.styles.ErrorStyle.Render(m.backupError) + "\n\n")
	}
	view.WriteString(localization.Labels["press_enter"])
	return view.String()
}

// renderPasswordValidation renders the password validation status
func (m *CLIModel) renderPasswordValidation(password string) string {
	passwordBytes := []byte(password)
	validationErr := wallet.ValidateStoragePassword(passwordBytes)
	clear(passwordBytes)

	var builder strings.Builder

	// Check for minimum length
	if password == "" {
		builder.WriteString(m.styles.RedCross.Render("✗"))
		builder.WriteString(" required\n")
	} else if validationErr != nil {
		builder.WriteString(m.styles.RedCross.Render("✗"))
		builder.WriteString(" has 15 characters or more\n")
	} else {
		builder.WriteString(m.styles.GreenCheck.Render("✓"))
		builder.WriteString(" has 15 characters or more\n")
	}

	// Check for lowercase letter
	if password == "" || validationErr != nil {
		builder.WriteString(m.styles.RedCross.Render("✗"))
		builder.WriteString(" is valid UTF-8\n")
	} else {
		builder.WriteString(m.styles.GreenCheck.Render("✓"))
		builder.WriteString(" is valid UTF-8\n")
	}

	// Check for uppercase letter
	if password == "" || validationErr != nil {
		builder.WriteString(m.styles.RedCross.Render("✗"))
		builder.WriteString(" contains only printable characters\n")
	} else {
		builder.WriteString(m.styles.GreenCheck.Render("✓"))
		builder.WriteString(" contains only printable characters\n")
	}

	// Check for digit or special character
	if password == "" || validationErr != nil {
		builder.WriteString(m.styles.RedCross.Render("✗"))
		builder.WriteString(" preserves whitespace exactly")
	} else {
		builder.WriteString(m.styles.GreenCheck.Render("✓"))
		builder.WriteString(" preserves whitespace exactly")
	}

	return builder.String()
}

// viewCreateWalletPassword renderiza a visualização de criação de wallet
func (m *CLIModel) viewCreateWalletPassword() string {
	if localization.Labels == nil {
		return "Localization labels not initialized."
	}

	var view strings.Builder
	view.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#00FF00")).Render(localization.Labels["enter_password"]) + "\n\n")
	view.WriteString(m.passwordInput.View() + "\n\n")
	view.WriteString(m.renderPasswordValidation(m.passwordInput.Value()) + "\n\n")
	if m.Vault != nil {
		view.WriteString("Confirm storage password:\n" + m.createPasswordConfirmationInput.View() + "\n\n")
	}
	if m.createPasswordError != "" {
		view.WriteString(m.styles.ErrorStyle.Render(m.createPasswordError) + "\n\n")
	}
	view.WriteString(localization.Labels["press_enter"])
	return view.String()
}

// renderSplash renderiza a tela de splash screen
func (m *CLIModel) renderSplash() string {
	// Verificar se a fonte selecionada está disponível
	if m.selectedFont == nil {
		return m.styles.ErrorStyle.Render(constants.ErrorFontNotFoundMessage)
	}

	// Inicializar o renderizador de string para a fonte selecionada
	fontString := tdf.NewTheDrawFontStringFont(m.selectedFont)

	// Renderizar o logo "bloco"
	renderedLogo := fontString.RenderString("bloco")
	renderedLogo = terminal.SanitizeStyledBlock(strings.TrimSpace(renderedLogo), 24, 160) // Remove any extra whitespace

	projectInfo := fmt.Sprintf("%s v%s", "BLOCO Wallet", localization.Labels["version"])

	// Center the projectInfo text
	projectInfoStyled := lipgloss.NewStyle().
		Align(lipgloss.Center).
		Render(projectInfo)

	// Create the splash screen content
	splashContent := lipgloss.JoinVertical(
		lipgloss.Center,
		renderedLogo,
		projectInfoStyled,
	)

	// Usar lipgloss.Place para centralizar horizontal e verticalmente
	finalSplash := lipgloss.Place(
		m.width,
		m.height,
		lipgloss.Center,
		lipgloss.Center,
		splashContent,
	)

	return finalSplash
}

func (m *CLIModel) renderStatusBar() string {
	// Left part: Number of wallets
	leftStyle := m.styles.StatusBarLeft // Used assignment for copying.
	left := leftStyle.
		SetString(fmt.Sprintf("Wallets: %d", m.walletCount)).
		String()

	// Right part: Current date and time
	currentTime := formatDisplayTime(m.displayTime)
	rightStyle := m.styles.StatusBarRight // Used assignment for copying.
	right := rightStyle.
		SetString(fmt.Sprintf("Date: %s", currentTime)).
		String()

	// Map view constants to human-readable names
	viewNames := map[string]string{
		constants.DefaultView:               localization.Labels["main_menu_title"],
		constants.SplashView:                "Splash",
		constants.CreateWalletNameView:      localization.Labels["create_new_wallet"],
		constants.CreateWalletBackupView:    localization.Labels["create_new_wallet"],
		constants.CreateWalletView:          localization.Labels["create_new_wallet"],
		constants.ImportWalletView:          localization.Labels["import_wallet"],
		constants.ImportWalletPasswordView:  localization.Labels["import_wallet"],
		constants.ImportMethodSelectionView: localization.Labels["import_method_title"],
		constants.ImportPrivateKeyView:      localization.Labels["import_private_key"],
		constants.ListWalletsView:           localization.Labels["list_wallets"],
		constants.WalletPasswordView:        localization.Labels["enter_wallet_password"],
		constants.WalletDetailsView:         localization.Labels["wallet_details_title"],
		constants.AccountHistoryView:        "Local History",
		constants.PersonalSignView:          "Sign Message",
		constants.EIP712SignView:            "Sign Typed Data",
		constants.ConfigurationView:         localization.Labels["configuration"],
		constants.LanguageSelectionView:     localization.Labels["language"],
		constants.NetworkMenuView:           localization.Labels["networks"],
		constants.NetworkListView:           localization.Labels["network_list"],
		constants.AddNetworkView:            localization.Labels["add_network"],
	}

	// Get the view name from the map, or use the current view constant if not found
	viewName := viewNames[m.currentView]
	if viewName == "" {
		viewName = m.currentView
	}

	// Center part: Current view and shortcut keys
	centerContent := fmt.Sprintf("View: %s | Press 'esc' to return | Press 'q' to quit", viewName)
	centerWidth := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if centerWidth < 12 {
		width := max(1, m.width)
		return lipgloss.NewStyle().Width(width).MaxWidth(width).Align(lipgloss.Center).Render(centerContent)
	}
	centerStyle := m.styles.StatusBarCenter // Used assignment for copying.
	center := centerStyle.
		SetString(centerContent).
		Width(centerWidth).
		Align(lipgloss.Center).
		String()

	// Join all parts
	statusBar := lipgloss.JoinHorizontal(
		lipgloss.Top,
		left,
		center,
		right,
	)

	return statusBar
}

func (m *CLIModel) renderCompactTerminal() string {
	width := max(1, m.width)
	height := max(1, m.height)
	message := lipgloss.NewStyle().Bold(true).Align(lipgloss.Center).MaxWidth(max(1, width-2)).MaxHeight(max(1, height-2)).Render("Terminal too small\nResize to at least 100 × 24")
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, message)
}

func (m *CLIModel) fitMainContent(contentHeight int) {
	if contentHeight < 1 {
		return
	}
	switch m.currentView {
	case constants.ListWalletsView:
		if len(m.wallets) > 0 || len(m.accounts) > 0 {
			m.walletTable.SetHeight(max(1, contentHeight-3))
		}
	case constants.WalletDetailsView:
		if m.selectedAccount == nil {
			return
		}
		if m.walletDetailsViewport.Width == 0 {
			m.initWalletDetailsComponents()
		}
		width, _ := m.walletDetailsDimensions()
		m.walletDetailsViewport.Width = width
		m.walletDetailsHelp.Width = width
		m.updateWalletDetailsKeyAvailability()
		m.walletDetailsViewport.SetContent(m.walletDetailsContent())
		helpHeight := lipgloss.Height(m.walletDetailsHelp.View(m.walletDetailsKeys))
		m.walletDetailsViewport.Height = max(1, contentHeight-helpHeight-1)
	case constants.AccountHistoryView:
		if m.accountHistory == nil {
			return
		}
		width := max(40, m.width-6)
		m.accountHistory.viewport.Width = width
		m.accountHistory.help.Width = width
		m.refreshAccountHistoryContent()
		helpHeight := lipgloss.Height(m.accountHistory.help.View(m.accountHistory.keys))
		m.accountHistory.viewport.Height = max(1, contentHeight-helpHeight-1)
	}
}

func (m *CLIModel) renderMainView() string {
	if m.width < 100 || m.height < 24 {
		return m.renderCompactTerminal()
	}
	renderedLogo := renderHeaderLogo()

	headerLeft := lipgloss.JoinVertical(
		lipgloss.Left,
		renderedLogo,
		fmt.Sprintf("Version: %s", localization.Labels["version"]),
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

	// Calcular altura da área de conteúdo
	contentHeight := m.height - headerHeight - footerHeight - 2 // Subtrai 2 para evitar overflow

	if contentHeight <= 0 {
		return m.renderCompactTerminal()
	}

	// Obter a visualização do conteúdo
	m.fitMainContent(contentHeight)
	content := m.getContentView()

	// Renderizar conteúdo com altura ajustada
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

// viewImportWallet renderiza a visualização de importação de wallet
func (m *CLIModel) viewImportWallet() string {
	if localization.Labels == nil {
		return "Localization labels not initialized."
	}

	var view strings.Builder

	// Renderizando o título com destaque
	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#7D56F4")).
		MarginBottom(1).
		Render(localization.Labels["import_wallet_title"])

	view.WriteString(title + "\n")

	// Pequena descrição do método de importação por mnemônica
	desc := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#AAAAAA")).
		Render(localization.Labels["import_mnemonic_desc"])
	view.WriteString(desc + "\n\n")

	// Estilo para o campo ativo
	activeStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#00FF00")).
		Bold(true)

	// Estilo para campos inativos
	inactiveStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#AAAAAA"))

	// Renderizar cada campo de entrada
	for i, ti := range m.textInputs {
		wordLabel := fmt.Sprintf("%s %d:", localization.Labels["word"], i+1)
		paddedLabel := fmt.Sprintf("%-10s", wordLabel) // Padding para alinhamento

		if i == m.importStage {
			// Campo ativo com destaque
			view.WriteString(activeStyle.Render(paddedLabel) + " " + ti.View() + "\n\n")
		} else {
			// Campos inativos
			view.WriteString(inactiveStyle.Render(paddedLabel) + " " + ti.View() + "\n")
		}
	}

	// Instruções para o usuário
	instructions := lipgloss.NewStyle().
		MarginTop(1).
		Italic(true).
		Render(localization.Labels["press_enter"])

	view.WriteString("\n" + instructions)

	// Adicionar uma borda ao redor de tudo
	content := view.String()
	return lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#7D56F4")).
		Padding(1, 2).
		Render(content)
}

// viewImportWalletPassword renderiza a visualização de senha após importação
func (m *CLIModel) viewImportWalletPassword() string {
	if localization.Labels == nil {
		return "Localization labels not initialized."
	}

	var view strings.Builder
	view.WriteString(
		lipgloss.NewStyle().Bold(true).Render(localization.Labels["enter_password"]+"\n\n") +
			m.passwordInput.View() + "\n\n" +
			m.renderPasswordValidation(m.passwordInput.Value()) + "\n\n" +
			localization.Labels["press_enter"],
	)
	return view.String()
}

// viewImportMethodSelection renderiza a visualização de seleção de methods de importação
func (m *CLIModel) viewImportMethodSelection() string {
	if localization.Labels == nil {
		return "Localization labels not initialized."
	}

	// Em vez de renderizar o menu de importação novamente, exibir apenas uma mensagem informativa
	// já que o menu já é exibido na área padrão de menu
	return localization.Labels["welcome_message"]
}

// viewConfigMenu renderiza a visualização de configuração
func (m *CLIModel) viewConfigMenu() string {
	if localization.Labels == nil {
		return "Localization labels not initialized."
	}

	// Em vez de renderizar o menu de configuração novamente, exibir apenas uma mensagem informativa
	// já que o menu já é exibido na área padrão de menu
	return localization.Labels["welcome_message"]
}

// viewImportPrivateKey renderiza a visualização de importação de chave privada
func (m *CLIModel) viewImportPrivateKey() string {
	// Use MenuTitle style for the header instead of non-existent Title style
	title := m.styles.MenuTitle.Render(localization.Labels["private_key_title"])
	desc := m.styles.MenuDesc.Render(localization.Labels["import_private_key_desc"]) // brief help about method
	input := m.privateKeyInput.View()
	// Use MenuDesc instead of non-existent Instructions style
	instructions := m.styles.MenuDesc.Render(localization.Labels["press_enter"])

	return lipgloss.JoinVertical(
		lipgloss.Left,
		title,
		"",
		desc,
		"",
		input,
		"",
		instructions,
	)
}

// viewImportKeystore renderiza a visualização de importação de arquivo keystore
func (m *CLIModel) viewImportKeystore() string {
	// Use MenuTitle style for the header
	title := m.styles.MenuTitle.Render(localization.Labels["keystore_title"])
	input := m.privateKeyInput.View()

	// Instructions for the user
	instructions := m.styles.MenuDesc.Render(localization.Labels["press_enter"] + " | Tab to show/cycle through suggestions")

	return lipgloss.JoinVertical(
		lipgloss.Left,
		title,
		"",
		input,
		"",
		instructions,
	)
}

// viewListWallets renderiza a visualização de listagem de wallets
func (m *CLIModel) viewListWallets() string {
	if localization.Labels == nil {
		return "Localization labels not initialized."
	}

	// Se não há diálogo de exclusão, retornar apenas a tabela
	if m.deletingWallet == nil {
		var view strings.Builder

		// Adicionar título à visualização
		title := lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#7D56F4")).
			MarginBottom(1).
			Render(localization.Labels["list_wallets_title"])

		view.WriteString(title + "\n")

		// Verificar se há wallets para exibir
		if len(m.wallets) == 0 && len(m.accounts) == 0 {
			// Exibir mensagem quando não há wallets
			message := "No wallets found. Create a new wallet to get started."
			if val, ok := localization.Labels["no_wallets_message"]; ok {
				message = val
			}
			noWalletsMsg := lipgloss.NewStyle().
				Foreground(lipgloss.Color("#5C5C5C")).
				Render(message)

			view.WriteString(noWalletsMsg)
		} else {
			// Adicionar a visualização da tabela
			tableView := m.walletTable.View()
			view.WriteString(tableView)

			// Se houver espaço, adicionar instruções na parte inferior
			if m.walletTable.Height() < len(m.wallets)+len(m.accounts) {
				// Só mostra instruções de rolagem se houver mais itens que o espaço disponível
				instructions := "\n" + lipgloss.NewStyle().
					Foreground(lipgloss.Color("#5C5C5C")).
					Render(localization.Labels["list_wallets_instructions"])

				view.WriteString(instructions)
			}
		}

		return view.String()
	}

	// Se há um diálogo de confirmação de exclusão, renderizar o diálogo
	return m.renderDeleteConfirmationDialog()
}

// renderDeleteConfirmationDialog renderiza o diálogo de confirmação de exclusão
func (m *CLIModel) renderDeleteConfirmationDialog() string {
	// Primeiro, renderizar a tabela de wallets
	var tableView string
	if len(m.wallets) > 0 {
		tableView = m.walletTable.View()
	} else {
		// Se não há wallets, criar uma área vazia para o diálogo
		tableView = strings.Repeat("\n", 10)
	}

	// Caixa de diálogo centralizada com botões estilizados e seleção
	question := localization.Labels["confirm_delete_wallet"]
	address := fmt.Sprintf("%s: %s", localization.Labels["ethereum_address"], safeShort(m.deletingWallet.Address))

	// Botões com seleção (garante espaçamento entre os textos)
	var confirmBtn, cancelBtn string
	if m.dialogButtonIndex == 0 {
		confirmBtn = m.styles.DialogButtonActive.Render(localization.Labels["confirm"])
		cancelBtn = m.styles.DialogButton.Render(localization.Labels["cancel"])
	} else {
		confirmBtn = m.styles.DialogButton.Render(localization.Labels["confirm"])
		cancelBtn = m.styles.DialogButtonActive.Render(localization.Labels["cancel"])
	}

	buttons := lipgloss.JoinHorizontal(lipgloss.Center, confirmBtn, "   ", cancelBtn)
	content := lipgloss.JoinVertical(lipgloss.Center, question, address, "", buttons)
	dialog := m.styles.Dialog.Render(content)

	// Calcular a posição do diálogo para centralizá-lo na área da tabela
	tableWidth := lipgloss.Width(tableView)
	tableHeight := lipgloss.Height(tableView)
	dialogWidth := lipgloss.Width(dialog)
	dialogHeight := lipgloss.Height(dialog)

	// Calcular posições para centralizar o diálogo na tabela
	leftPadding := (tableWidth - dialogWidth) / 2
	if leftPadding < 0 {
		leftPadding = 0
	}

	// Dividir a tabela em linhas
	tableLines := strings.Split(tableView, "\n")

	// Calcular a linha inicial para o diálogo
	startLine := (tableHeight - dialogHeight) / 2
	if startLine < 0 {
		startLine = 0
	}

	// Dividir o diálogo em linhas
	dialogLines := strings.Split(dialog, "\n")

	// Inserir o diálogo nas linhas da tabela
	for i := 0; i < dialogHeight && i+startLine < len(tableLines); i++ {
		// Garantir que a linha da tabela é longa o suficiente
		for len(tableLines[i+startLine]) < leftPadding {
			tableLines[i+startLine] += " "
		}

		// Inserir a linha do diálogo na posição correta
		if leftPadding < len(tableLines[i+startLine]) {
			prefix := tableLines[i+startLine][:leftPadding]
			suffix := ""
			if leftPadding+dialogWidth < len(tableLines[i+startLine]) {
				suffix = tableLines[i+startLine][leftPadding+dialogWidth:]
			}
			tableLines[i+startLine] = prefix + dialogLines[i] + suffix
		} else {
			padding := strings.Repeat(" ", leftPadding-len(tableLines[i+startLine]))
			tableLines[i+startLine] += padding + dialogLines[i]
		}
	}

	// Reconstruir a visualização da tabela com o diálogo
	return strings.Join(tableLines, "\n")
}

func (m *CLIModel) viewVaultAction(export bool) string {
	title := "Rotate storage password"
	if export {
		title = "Export Keystore V3"
		if m.vaultExportEncrypted {
			title = "Export Bloco encrypted backup"
		}
	}
	if export && m.vaultActionPreview && m.selectedAccount != nil {
		format := "Keystore V3 (derived private key only)"
		if m.vaultExportEncrypted {
			format = "Bloco encrypted backup (canonical secret)"
		}
		return lipgloss.NewStyle().Bold(true).Render("Confirm secret export") + "\n\n" +
			fmt.Sprintf("Account: %s\nAddress: %s\nFormat: %s\nDestination: %s\n\nThis operation writes an exportable secret artifact.\nPress Enter again to export or Esc to cancel.",
				safeShort(m.selectedAccount.Name), safeShort(m.selectedAccount.Address), safeShort(format), safeInline(m.exportDestinationInput.Value()))
	}
	var view strings.Builder
	view.WriteString(lipgloss.NewStyle().Bold(true).Render(title) + "\n\n")
	view.WriteString("Current password:\n" + m.currentPasswordInput.View() + "\n\n")
	view.WriteString("New password:\n" + m.newPasswordInput.View() + "\n\n")
	view.WriteString("Confirm new password:\n" + m.confirmPasswordInput.View() + "\n\n")
	if export {
		view.WriteString("Destination:\n" + m.exportDestinationInput.View() + "\n\n")
	}
	if m.vaultActionError != "" {
		view.WriteString(m.styles.ErrorStyle.Render(m.vaultActionError) + "\n\n")
	}
	view.WriteString("Press Enter to advance. Press Esc to cancel.")
	return view.String()
}

// viewWalletPassword renderiza a visualização de entrada de senha para wallet selecionada
func (m *CLIModel) viewWalletPassword() string {
	if localization.Labels == nil {
		return "Localization labels not initialized."
	}

	var view strings.Builder
	view.WriteString(
		lipgloss.NewStyle().Bold(true).Render(localization.Labels["enter_wallet_password"]+"\n\n") +
			m.passwordInput.View() + "\n\n" +
			m.renderPasswordValidation(m.passwordInput.Value()) + "\n\n" +
			localization.Labels["press_enter"],
	)
	return view.String()
}

// viewWalletDetails renderiza a visualização de detalhes da wallet
func (m *CLIModel) viewWalletDetails() string {
	if localization.Labels == nil {
		return "Localization labels not initialized."
	}
	if m.selectedAccount != nil {
		return m.walletDetailsViewport.View() + "\n" + m.walletDetailsHelp.View(m.walletDetailsKeys)
	}

	if m.walletDetails != nil {
		var view strings.Builder

		// Resolve import method display name
		methodLabel := localization.Labels["method_label"]
		methodName := ""
		switch m.walletDetails.ImportMethod {
		case wallet.ImportMethodMnemonic:
			methodName = localization.Labels["method_mnemonic"]
		case wallet.ImportMethodPrivateKey:
			methodName = localization.Labels["method_private_key"]
		case wallet.ImportMethodKeystore:
			// Show "Keystore File" for keystore imports
			if localization.Labels["method_keystore"] != "" {
				methodName = localization.Labels["method_keystore"]
			} else {
				methodName = "Keystore File" // Fallback
			}
		default:
			methodName = safeShort(string(m.walletDetails.ImportMethod))
		}

		// Determine mnemonic text based on import method
		mnemonicText := ""
		if m.walletDetails.HasMnemonic && m.walletDetails.Mnemonic != nil && *m.walletDetails.Mnemonic != "" {
			mnemonicText = localization.GetWalletImportMessage("sensitive_data_hidden")
		} else {
			// Use specific message based on import method
			switch m.walletDetails.ImportMethod {
			case wallet.ImportMethodKeystore:
				mnemonicText = localization.GetWalletImportMessage("no_mnemonic_keystore")
			case wallet.ImportMethodPrivateKey:
				mnemonicText = localization.GetWalletImportMessage("no_mnemonic_available")
			default:
				mnemonicText = localization.GetWalletImportMessage("no_mnemonic_available")
			}
		}

		view.WriteString(
			lipgloss.NewStyle().Bold(true).Render(localization.Labels["wallet_details_title"]+"\n\n") +
				fmt.Sprintf("%-*s %s\n", 20, localization.Labels["ethereum_address"], safeShort(m.walletDetails.Wallet.Address)) +
				fmt.Sprintf("%-*s %s\n", 20, localization.Labels["private_key"], localization.GetWalletImportMessage("sensitive_data_hidden")) +
				fmt.Sprintf("%-*s %s\n", 20, methodLabel+":", methodName) +
				fmt.Sprintf("%-*s %s\n\n", 20, localization.Labels["mnemonic_phrase_label"], mnemonicText),
		)

		view.WriteString("\n" + localization.Labels["press_esc"])
		return view.String()
	}
	return localization.Labels["select_wallet_prompt"]
}

// viewLanguageSelection renderiza a visualização de seleção de idioma
func (m *CLIModel) viewLanguageSelection() string {
	if localization.Labels == nil {
		return "Localization labels not initialized."
	}

	// Em vez de renderizar o menu de idiomas novamente, exibir apenas uma mensagem informativa
	// já que o menu já é exibido na área padrão de menu
	return localization.Labels["welcome_message"]
}

// viewNetworkMenu renderiza a visualização do menu de redes
func (m *CLIModel) viewNetworkMenu() string {
	if localization.Labels == nil {
		return "Localization labels not initialized."
	}

	// Em vez de renderizar o menu de redes novamente, exibir apenas uma mensagem informativa
	// já que o menu já é exibido na área padrão de menu
	return localization.Labels["welcome_message"]
}

// viewEnhancedImport renderiza a visualização de importação aprimorada
func (m *CLIModel) viewEnhancedImport() string {
	if m.enhancedImportState == nil {
		return "Enhanced import not initialized"
	}

	return m.enhancedImportState.View()
}
