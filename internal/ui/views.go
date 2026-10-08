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
		return localization.Get("amount_unavailable")
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

	var view strings.Builder
	view.WriteString(
		lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#00FF00")).Render(localization.Get("create_wallet_title")) + "\n\n" +
			localization.Get("create_wallet_name_prompt") + "\n\n" +
			m.nameInput.View() + "\n\n" +
			localization.Get("press_enter"),
	)
	return view.String()
}

func (m *CLIModel) viewCreateWalletOptions() string {
	title := lipgloss.NewStyle().Bold(true).Render(localization.Get("create_options_title"))
	progress := localization.T("create_step_progress", map[string]interface{}{"Step": m.createOptionsStage + 1})
	var content string
	switch {
	case m.createOptionsStage == 2:
		content = localization.Get("create_passphrase_body") + "\n\n" + m.createPassphraseInput.View() + "\n\n" + localization.Get("create_passphrase_hint")
	case m.createOptionsStage == 3 && m.createCustomPath:
		content = localization.Get("create_custom_path_body") + "\n\n" + m.createDerivationPathInput.View() + "\n\n" + localization.Get("create_custom_path_hint")
	default:
		content = m.createOptionList.View()
	}
	view := title + "\n\n" + progress + "\n\n" + content + "\n\n" + localization.Get("create_enter_continue")
	if m.createPasswordError != "" {
		view += "\n\n" + m.styles.ErrorStyle.Render(m.createPasswordError)
	}
	return view
}

func (m *CLIModel) viewCreateWalletBackup() string {

	var backupWordValues []string
	confirmationLabel := localization.Get("confirm_mnemonic")
	confirmationInput := m.backupConfirmationInput.View()
	materialNotice := ""
	if m.Vault != nil && m.backupChallenge != nil {
		backupWordValues = append([]string(nil), m.backupChallenge.Words...)
		indices := make([]string, 0, len(m.backupChallenge.RequiredWordIndices))
		for _, index := range m.backupChallenge.RequiredWordIndices {
			indices = append(indices, fmt.Sprintf("#%d", index+1))
		}
		confirmationLabel = localization.T("backup_confirm_words", map[string]interface{}{"Indices": strings.Join(indices, ", ")})
		materialNotice = localization.T("backup_metadata", map[string]interface{}{"Path": m.backupChallenge.DerivationPath, "Language": m.backupChallenge.BIP39Language})
		if m.backupChallenge.RequiresMaterialConfirmation {
			materialNotice += localization.Get("backup_passphrase_note")
		}
		materialNotice += "\n"
	}

	var view strings.Builder
	view.WriteString(
		lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#00FF00")).Render(localization.Get("mnemonic_phrase")) + "\n\n" +
			renderMnemonicCards(backupWordValues, m.width-8) + "\n\n" +
			materialNotice + confirmationLabel + "\n\n" +
			confirmationInput + "\n\n",
	)
	if m.backupError != "" {
		view.WriteString(m.styles.ErrorStyle.Render(m.backupError) + "\n\n")
	}
	view.WriteString(localization.Get("press_enter"))
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
		builder.WriteString(localization.Get("pwd_rule_required"))
	} else if validationErr != nil {
		builder.WriteString(m.styles.RedCross.Render("✗"))
		builder.WriteString(localization.Get("pwd_rule_length"))
	} else {
		builder.WriteString(m.styles.GreenCheck.Render("✓"))
		builder.WriteString(localization.Get("pwd_rule_length"))
	}

	// Check for lowercase letter
	if password == "" || validationErr != nil {
		builder.WriteString(m.styles.RedCross.Render("✗"))
		builder.WriteString(localization.Get("pwd_rule_utf8"))
	} else {
		builder.WriteString(m.styles.GreenCheck.Render("✓"))
		builder.WriteString(localization.Get("pwd_rule_utf8"))
	}

	// Check for uppercase letter
	if password == "" || validationErr != nil {
		builder.WriteString(m.styles.RedCross.Render("✗"))
		builder.WriteString(localization.Get("pwd_rule_printable"))
	} else {
		builder.WriteString(m.styles.GreenCheck.Render("✓"))
		builder.WriteString(localization.Get("pwd_rule_printable"))
	}

	// Check for digit or special character
	if password == "" || validationErr != nil {
		builder.WriteString(m.styles.RedCross.Render("✗"))
		builder.WriteString(localization.Get("pwd_rule_whitespace"))
	} else {
		builder.WriteString(m.styles.GreenCheck.Render("✓"))
		builder.WriteString(localization.Get("pwd_rule_whitespace"))
	}

	return builder.String()
}

// viewCreateWalletPassword renderiza a visualização de criação de wallet
func (m *CLIModel) viewCreateWalletPassword() string {

	var view strings.Builder
	view.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#00FF00")).Render(localization.Get("enter_password")) + "\n\n")
	view.WriteString(m.passwordInput.View() + "\n\n")
	view.WriteString(m.renderPasswordValidation(m.passwordInput.Value()) + "\n\n")
	if m.Vault != nil {
		view.WriteString(localization.Get("confirm_storage_password_label") + "\n" + m.createPasswordConfirmationInput.View() + "\n\n")
	}
	if m.createPasswordError != "" {
		view.WriteString(m.styles.ErrorStyle.Render(m.createPasswordError) + "\n\n")
	}
	view.WriteString(localization.Get("press_enter"))
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

	projectInfo := fmt.Sprintf("%s %s", "BLOCO Wallet", m.displayVersion())

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

func (m *CLIModel) renderCompactTerminal() string {
	return m.renderTerminalSizeHint(100, 24)
}

func (m *CLIModel) renderTerminalSizeHint(minWidth, minHeight int) string {
	width := max(1, m.width)
	height := max(1, m.height)
	message := lipgloss.NewStyle().Bold(true).Align(lipgloss.Center).MaxWidth(max(1, width-2)).MaxHeight(max(1, height-2)).Render(localization.T("terminal_too_small", map[string]interface{}{"Width": minWidth, "Height": minHeight}))
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, message)
}

func (m *CLIModel) fitMainContent(contentHeight int) {
	if contentHeight < 1 {
		return
	}
	switch m.currentView {
	case constants.ListWalletsView:
		if (len(m.wallets) > 0 || len(m.accounts) > 0) && m.accountDeletion == nil {
			overhead := lipgloss.Height(m.viewListWallets()) - lipgloss.Height(m.walletTable.View())
			if overhead < 0 {
				overhead = 0
			}
			target := max(3, contentHeight-m.styles.Content.GetVerticalFrameSize()-overhead)
			if target != lipgloss.Height(m.walletTable.View()) {
				cursor := m.walletTable.Cursor()
				m.walletTable.SetHeight(target)
				m.walletTable.GotoTop()
				m.walletTable.MoveDown(cursor)
			}
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
		m.walletDetailsViewport.Height = max(1, contentHeight-m.styles.Content.GetVerticalFrameSize())
	case constants.KeePassSettingsView:
		if m.keepassSettings == nil {
			return
		}
		m.keepassSettings.contentHeight = max(1, contentHeight-m.styles.Content.GetVerticalFrameSize())
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
		localization.T("version_label", map[string]interface{}{"Version": m.displayVersion()}),
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
	if m.currentView == constants.WalletDetailsView && m.selectedAccount != nil && m.walletDetailsHelp.Width != 0 {
		m.updateWalletDetailsKeyAvailability()
	}
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

func (m *CLIModel) viewImportMethodSelection() string {

	// Em vez de renderizar o menu de importação novamente, exibir apenas uma mensagem informativa
	// já que o menu já é exibido na área padrão de menu
	return localization.Get("welcome_message")
}

// viewConfigMenu renderiza a visualização de configuração
func (m *CLIModel) viewConfigMenu() string {

	// Em vez de renderizar o menu de configuração novamente, exibir apenas uma mensagem informativa
	// já que o menu já é exibido na área padrão de menu
	return localization.Get("welcome_message")
}

func (m *CLIModel) viewListWallets() string {

	if m.accountDeletion != nil {
		return m.viewAccountDeletion()
	}

	var view strings.Builder

	// Adicionar título à visualização
	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#7D56F4")).
		MarginBottom(1).
		Render(localization.Get("list_wallets_title"))

	view.WriteString(title + "\n")

	// Verificar se há wallets para exibir
	if len(m.wallets) == 0 && len(m.accounts) == 0 {
		// Exibir mensagem quando não há wallets
		message := localization.Get("no_wallets_message")
		noWalletsMsg := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#5C5C5C")).
			Render(message)

		view.WriteString(noWalletsMsg)
	} else {
		// Adicionar a visualização da tabela
		tableView := m.walletTable.View()
		view.WriteString(tableView)
	}

	if m.lastOperationNotice != "" {
		view.WriteString("\n" + lipgloss.NewStyle().
			Foreground(lipgloss.Color("#5C5C5C")).
			Render(safeInline(m.lastOperationNotice)))
	}

	return view.String()
}

func (m *CLIModel) viewVaultAction(export bool) string {
	title := localization.Get("vault_rotate_title")
	if export {
		title = localization.Get("vault_export_keystore_title")
		if m.vaultExportEncrypted {
			title = localization.Get("vault_export_encrypted_title")
		}
	}
	if export && m.vaultActionPreview && m.selectedAccount != nil {
		format := localization.Get("vault_export_format_keystore")
		if m.vaultExportEncrypted {
			format = localization.Get("vault_export_format_encrypted")
		}
		return lipgloss.NewStyle().Bold(true).Render(localization.Get("vault_export_confirm_title")) + "\n\n" +
			localization.T("vault_export_confirm_body", map[string]interface{}{
				"Name": safeShort(m.selectedAccount.Name), "Address": safeShort(m.selectedAccount.Address),
				"Format": safeShort(format), "Destination": safeInline(m.exportDestinationInput.Value()),
			})
	}
	var view strings.Builder
	view.WriteString(lipgloss.NewStyle().Bold(true).Render(title) + "\n\n")
	view.WriteString(localization.Get("vault_current_password") + "\n" + m.currentPasswordInput.View() + "\n")
	if m.selectedAccount != nil {
		if label := m.credentialMethodLabel(m.credentialToggleEligible(*m.selectedAccount)); label != "" {
			view.WriteString(label + "\n")
		}
	}
	view.WriteString("\n")
	view.WriteString(localization.Get("vault_new_password") + "\n" + m.newPasswordInput.View() + "\n\n")
	view.WriteString(localization.Get("vault_confirm_password") + "\n" + m.confirmPasswordInput.View() + "\n\n")
	if export {
		view.WriteString(localization.Get("vault_destination") + "\n" + m.exportDestinationInput.View() + "\n\n")
	}
	if m.vaultActionError != "" {
		view.WriteString(m.styles.ErrorStyle.Render(m.vaultActionError) + "\n\n")
	}
	return view.String()
}

// viewWalletDetails renderiza a visualização de detalhes da wallet
func (m *CLIModel) viewWalletDetails() string {
	if m.selectedAccount != nil {
		return m.walletDetailsViewport.View()
	}

	if m.walletDetails != nil {
		var view strings.Builder

		// Resolve import method display name
		methodLabel := localization.Get("method_label")
		methodName := ""
		switch m.walletDetails.ImportMethod {
		case wallet.ImportMethodMnemonic:
			methodName = localization.Get("method_mnemonic")
		case wallet.ImportMethodPrivateKey:
			methodName = localization.Get("method_private_key")
		case wallet.ImportMethodKeystore:
			methodName = localization.Get("method_keystore")
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
			lipgloss.NewStyle().Bold(true).Render(localization.Get("wallet_details_title")+"\n\n") +
				fmt.Sprintf("%-*s %s\n", 20, localization.Get("ethereum_address"), safeShort(m.walletDetails.Wallet.Address)) +
				fmt.Sprintf("%-*s %s\n", 20, localization.Get("private_key"), localization.GetWalletImportMessage("sensitive_data_hidden")) +
				fmt.Sprintf("%-*s %s\n", 20, methodLabel+":", methodName) +
				fmt.Sprintf("%-*s %s\n\n", 20, localization.Get("mnemonic_phrase_label"), mnemonicText),
		)

		return view.String()
	}
	return localization.Get("select_wallet_prompt")
}

// viewLanguageSelection renderiza a visualização de seleção de idioma
func (m *CLIModel) viewLanguageSelection() string {

	// Em vez de renderizar o menu de idiomas novamente, exibir apenas uma mensagem informativa
	// já que o menu já é exibido na área padrão de menu
	return localization.Get("welcome_message")
}

// viewNetworkMenu renderiza a visualização do menu de redes
func (m *CLIModel) viewNetworkMenu() string {

	// Em vez de renderizar o menu de redes novamente, exibir apenas uma mensagem informativa
	// já que o menu já é exibido na área padrão de menu
	return localization.Get("welcome_message")
}
