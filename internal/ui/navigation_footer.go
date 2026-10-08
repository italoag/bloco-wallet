package ui

import (
	"strings"

	"blocowallet/internal/constants"
	"blocowallet/internal/wallet"
	"blocowallet/pkg/localization"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

type navigationHint struct {
	key   string
	label string
}

func renderNavigationHints(hints []navigationHint) string {
	tokens := make([]string, 0, len(hints))
	for _, hint := range hints {
		if hint.key == "" {
			continue
		}
		tokens = append(tokens, hint.key+" "+hint.label)
	}
	return strings.Join(tokens, " | ")
}

func (m *CLIModel) footerViewName() string {
	// Map view constants to human-readable names
	viewNames := map[string]string{
		constants.DefaultView:               localization.Get("main_menu_title"),
		constants.SplashView:                localization.Get("view_splash"),
		constants.CreateWalletNameView:      localization.Get("create_new_wallet"),
		constants.CreateWalletOptionsView:   localization.Get("create_new_wallet"),
		constants.CreateWalletBackupView:    localization.Get("create_new_wallet"),
		constants.CreateWalletView:          localization.Get("create_new_wallet"),
		constants.ImportMethodSelectionView: localization.Get("import_method_title"),
		constants.CanonicalImportView:       localization.Get("view_import_wallet"),
		constants.ListWalletsView:           localization.Get("list_wallets"),
		constants.WalletDetailsView:         localization.Get("wallet_details_title"),
		constants.RotatePasswordView:        localization.Get("view_change_password"),
		constants.ExportAccountView:         localization.Get("view_export_wallet"),
		constants.RecoveryView:              localization.Get("view_recovery"),
		constants.AccountHistoryView:        localization.Get("view_local_history"),
		constants.PersonalSignView:          localization.Get("view_sign_message"),
		constants.EIP712SignView:            localization.Get("view_sign_typed_data"),
		constants.ContractCallView:          localization.Get("view_contract_call"),
		constants.NativeTransferView:        localization.Get("view_send"),
		constants.WalletConnectView:         "WalletConnect",
		constants.FIDO2View:                 localization.Get("view_security_keys"),
		constants.SafeView:                  localization.Get("safe_menu_title"),
		constants.ConfigurationView:         localization.Get("configuration"),
		constants.LanguageSelectionView:     localization.Get("language"),
		constants.NetworkMenuView:           localization.Get("networks"),
		constants.NetworkListView:           localization.Get("network_list"),
		constants.AddNetworkView:            localization.Get("add_network"),
		constants.KeePassSettingsView:       localization.Get("menu_keepass"),
		constants.KeePassAccountView:        localization.Get("keepass_account_title"),
	}
	if m.credentialPrompt != nil {
		return safeInline(localization.Get("keepass_master_title"))
	}
	// Get the view name from the map, or use the current view constant if not found
	viewName := viewNames[m.currentView]
	if m.currentView == constants.ListWalletsView && m.accountDeletion != nil {
		viewName = localization.Get("view_delete_wallet")
	}
	if viewName == "" {
		viewName = m.currentView
	}
	return safeInline(viewName)
}

func (m *CLIModel) navigationHints() []navigationHint {
	hint := func(key, label string) navigationHint { return navigationHint{key: key, label: label} }
	if m.credentialPrompt != nil {
		if m.credentialPrompt.busy {
			return []navigationHint{hint("Esc", localization.Get("hint_cancel")), hint("Ctrl+Q", localization.Get("hint_quit"))}
		}
		return []navigationHint{hint("Enter", localization.Get("hint_continue")), hint("Esc", localization.Get("hint_cancel"))}
	}
	switch m.currentView {
	case constants.ListWalletsView:
		if m.accountDeletion != nil {
			deletion := m.accountDeletion
			if deletion.busy || deletion.cancelling {
				return []navigationHint{hint("Esc", localization.Get("hint_cancel")), hint("Ctrl+Q", localization.Get("hint_cancel"))}
			}
			enter := localization.Get("hint_continue")
			if deletion.stage != 0 || deletion.account.SignerKind != wallet.SignerKindSoftware {
				enter = localization.Get("hint_delete")
			}
			hints := []navigationHint{hint("Enter", enter), hint("Esc", localization.Get("hint_cancel")), hint("Ctrl+Q", localization.Get("hint_quit"))}
			if deletion.stage == 0 && m.credentialBackupEnabled() && deletion.account.SignerKind == wallet.SignerKindSoftware {
				hints = append(hints[:1], append([]navigationHint{hint("Ctrl+B", localization.Get("hint_keepass_remove"))}, hints[1:]...)...)
			}
			if deletion.stage == 1 && m.credentialToggleEligible(deletion.account) {
				hints = append(hints[:1], append([]navigationHint{hint("Ctrl+K", localization.Get("hint_keepass_toggle"))}, hints[1:]...)...)
			}
			return hints
		}
		var hints []navigationHint
		// Se houver espaço, adicionar instruções na parte inferior
		if len(m.walletTable.Rows()) > 0 {
			// Só mostra instruções de rolagem se houver mais itens que o espaço disponível
			hints = append(hints, hint("Up/Down", localization.Get("hint_navigate")))
			if m.selectedAccountFromTable() != nil || m.selectedWalletFromTable() != nil {
				hints = append(hints, hint("Enter", localization.Get("hint_open")))
			}
			if m.Vault != nil && m.selectedAccountFromTable() != nil {
				hints = append(hints, hint("d/Delete", localization.Get("hint_delete")))
			}
		}
		return append(hints, hint("Esc", localization.Get("hint_back")), hint("Ctrl+Q", localization.Get("hint_quit")))
	case constants.CanonicalImportView:
		state := m.canonicalImport
		if state == nil {
			return []navigationHint{hint("Esc", localization.Get("hint_back")), hint("Ctrl+Q", localization.Get("hint_quit"))}
		}
		if state.busy {
			return []navigationHint{hint("Esc", localization.Get("hint_cancel")), hint("Ctrl+Q", localization.Get("hint_cancel"))}
		}
		if len(state.resultLines) > 0 {
			return []navigationHint{hint("Enter", localization.Get("hint_wallets")), hint("Esc", localization.Get("hint_back")), hint("Ctrl+Q", localization.Get("hint_quit"))}
		}
		if state.preview != nil {
			return []navigationHint{hint("Enter", localization.Get("hint_import")), hint("Esc", localization.Get("hint_cancel")), hint("Ctrl+Q", localization.Get("hint_quit"))}
		}
		enter := localization.Get("hint_continue")
		if state.stage == len(state.fields)-1 {
			enter = localization.Get("hint_preview")
		}
		hints := []navigationHint{hint("Enter", enter)}
		if state.stage < len(state.fields) && state.fields[state.stage].key == "source_password" && m.credentialBackupEnabled() {
			hints = append(hints, hint("Ctrl+K", localization.Get("hint_keepass_toggle")))
		}
		if state.stage < len(state.fields) && (state.fields[state.stage].key == "keystore_path" || state.fields[state.stage].key == "directory") {
			hints = append(hints, hint("Tab", localization.Get("hint_complete")), hint("Up/Down", localization.Get("hint_suggestions")))
		}
		return append(hints, hint("Esc", localization.Get("hint_cancel")), hint("Ctrl+Q", localization.Get("hint_quit")))
	case constants.WalletDetailsView:
		if m.selectedAccount == nil {
			return []navigationHint{hint("Esc", localization.Get("hint_back")), hint("Ctrl+Q", localization.Get("hint_quit"))}
		}
		var bindings []key.Binding
		if m.walletDetailsHelp.ShowAll {
			for _, group := range m.walletDetailsKeys.FullHelp() {
				bindings = append(bindings, group...)
			}
		} else {
			keys := m.walletDetailsKeys
			bindings = []key.Binding{keys.Recovery, keys.Export, keys.EncryptedExport, keys.Lock, keys.KeePass, keys.FetchBalances, keys.History, keys.ToggleHelp, keys.Back}
		}
		var hints []navigationHint
		seen := make(map[string]bool)
		for _, binding := range bindings {
			if !binding.Enabled() {
				continue
			}
			help := binding.Help()
			if help.Key == "" || seen[help.Key] {
				continue
			}
			seen[help.Key] = true
			hints = append(hints, hint(help.Key, help.Desc))
		}
		return append(hints, hint("Ctrl+Q", localization.Get("hint_quit")))
	case constants.RecoveryView:
		state := m.recovery
		if state == nil {
			return []navigationHint{hint("Esc", localization.Get("hint_back")), hint("Ctrl+Q", localization.Get("hint_quit"))}
		}
		if state.busy {
			return []navigationHint{hint("Esc", localization.Get("hint_cancel")), hint("Ctrl+Q", localization.Get("hint_cancel_quit"))}
		}
		switch state.stage {
		case recoveryStageMenu:
			return []navigationHint{hint("Up/Down", localization.Get("hint_select")), hint("Enter", localization.Get("hint_continue")), hint("PgUp/PgDown", localization.Get("hint_scroll")), hint("Esc", localization.Get("hint_back")), hint("Ctrl+Q", localization.Get("hint_quit"))}
		case recoveryStageDestination, recoveryStagePassword:
			hints := []navigationHint{hint("Enter", localization.Get("hint_continue"))}
			if state.stage == recoveryStagePassword && m.credentialToggleEligible(state.account) {
				hints = append(hints, hint("Ctrl+K", localization.Get("hint_keepass_toggle")))
			}
			return append(hints, hint("PgUp/PgDown", localization.Get("hint_scroll")), hint("Esc", localization.Get("hint_cancel")), hint("Ctrl+Q", localization.Get("hint_quit")))
		case recoveryStageConfirmation:
			enter := localization.Get("hint_reveal")
			if state.selected >= 0 && state.selected < len(state.actions) && state.actions[state.selected].export {
				enter = localization.Get("hint_export")
			}
			return []navigationHint{hint("Enter", enter), hint("PgUp/PgDown", localization.Get("hint_scroll")), hint("Esc", localization.Get("hint_cancel")), hint("Ctrl+Q", localization.Get("hint_quit"))}
		case recoveryStageRevealed:
			return []navigationHint{hint("PgUp/PgDown", localization.Get("hint_scroll")), hint("Esc", localization.Get("hint_hide")), hint("Ctrl+Q", localization.Get("hint_quit"))}
		}
		return []navigationHint{hint("Esc", localization.Get("hint_back")), hint("Ctrl+Q", localization.Get("hint_quit"))}
	case constants.RotatePasswordView:
		enter := localization.Get("hint_continue")
		if m.vaultActionStage >= 2 {
			enter = localization.Get("hint_change_password")
		}
		hints := []navigationHint{hint("Enter", enter)}
		if m.vaultActionStage == 0 && m.selectedAccount != nil && m.credentialToggleEligible(*m.selectedAccount) {
			hints = append(hints, hint("Ctrl+K", localization.Get("hint_keepass_toggle")))
		}
		return append(hints, hint("Esc", localization.Get("hint_cancel")), hint("Ctrl+Q", localization.Get("hint_quit")))
	case constants.ExportAccountView:
		enter := localization.Get("hint_continue")
		if m.vaultActionPreview {
			enter = localization.Get("hint_export")
		}
		hints := []navigationHint{hint("Enter", enter)}
		if m.vaultActionStage == 0 && m.selectedAccount != nil && m.credentialToggleEligible(*m.selectedAccount) {
			hints = append(hints, hint("Ctrl+K", localization.Get("hint_keepass_toggle")))
		}
		return append(hints, hint("Esc", localization.Get("hint_cancel")), hint("Ctrl+Q", localization.Get("hint_quit")))
	case constants.KeePassSettingsView:
		state := m.keepassSettings
		if state == nil {
			return []navigationHint{hint("Esc", localization.Get("hint_back")), hint("Ctrl+Q", localization.Get("hint_quit"))}
		}
		if state.busy {
			return []navigationHint{hint("Esc", localization.Get("hint_cancel")), hint("Ctrl+Q", localization.Get("hint_quit"))}
		}
		switch state.stage {
		case keepassStageMenu:
			return []navigationHint{hint("Up/Down", localization.Get("hint_select")), hint("Enter", localization.Get("hint_open")), hint("Esc", localization.Get("hint_back")), hint("Ctrl+Q", localization.Get("hint_quit"))}
		case keepassStagePath:
			return []navigationHint{hint("Enter", localization.Get("hint_continue")), hint("Tab", localization.Get("hint_complete")), hint("Up/Down", localization.Get("hint_suggestions")), hint("Esc", localization.Get("hint_back")), hint("Ctrl+Q", localization.Get("hint_quit"))}
		case keepassStagePendingList:
			return []navigationHint{hint("Up/Down", localization.Get("hint_select")), hint("Enter", localization.Get("hint_open")), hint("Esc", localization.Get("hint_back")), hint("Ctrl+Q", localization.Get("hint_quit"))}
		case keepassStageRetryPath:
			return []navigationHint{hint("Enter", localization.Get("hint_continue")), hint("Tab", localization.Get("hint_complete")), hint("Up/Down", localization.Get("hint_suggestions")), hint("Esc", localization.Get("hint_back")), hint("Ctrl+Q", localization.Get("hint_quit"))}
		case keepassStageRetryPassword:
			hints := []navigationHint{hint("Enter", localization.Get("hint_continue"))}
			if state.retryAllowsKeePass() {
				hints = append(hints, hint("Ctrl+K", localization.Get("hint_keepass_toggle")))
			}
			return append(hints, hint("Esc", localization.Get("hint_back")), hint("Ctrl+Q", localization.Get("hint_quit")))
		default:
			return []navigationHint{hint("Enter", localization.Get("hint_continue")), hint("Esc", localization.Get("hint_back")), hint("Ctrl+Q", localization.Get("hint_quit"))}
		}
	case constants.KeePassAccountView:
		state := m.keepassAccount
		if state == nil {
			return []navigationHint{hint("Esc", localization.Get("hint_back")), hint("Ctrl+Q", localization.Get("hint_quit"))}
		}
		if state.busy {
			return []navigationHint{hint("Esc", localization.Get("hint_cancel")), hint("Ctrl+Q", localization.Get("hint_quit"))}
		}
		if state.stage == keepassAccountStagePassword {
			hints := []navigationHint{hint("Enter", localization.Get("hint_continue"))}
			if state.hasBackup {
				hints = append(hints, hint("Ctrl+K", localization.Get("hint_keepass_toggle")))
			}
			return append(hints, hint("Esc", localization.Get("hint_back")), hint("Ctrl+Q", localization.Get("hint_quit")))
		}
		return []navigationHint{hint("Up/Down", localization.Get("hint_select")), hint("Enter", localization.Get("hint_open")), hint("Esc", localization.Get("hint_back")), hint("Ctrl+Q", localization.Get("hint_quit"))}
	case constants.NativeTransferView:
		if m.nativeTransfer != nil && m.nativeTransfer.phase == nativeTransferPassword && m.credentialToggleEligible(m.nativeTransfer.account) {
			return []navigationHint{hint("Enter", localization.Get("hint_confirm")), hint("Ctrl+K", localization.Get("hint_keepass_toggle")), hint("Esc", localization.Get("hint_cancel")), hint("Ctrl+Q", localization.Get("hint_quit"))}
		}
		return []navigationHint{hint("Enter", localization.Get("hint_confirm")), hint("Esc", localization.Get("hint_cancel")), hint("Ctrl+Q", localization.Get("hint_quit"))}
	case constants.PersonalSignView:
		if m.personalSign != nil && m.personalSign.phase == personalSignPassword && m.credentialToggleEligible(m.personalSign.account) {
			return []navigationHint{hint("Enter", localization.Get("hint_confirm")), hint("Ctrl+K", localization.Get("hint_keepass_toggle")), hint("Esc", localization.Get("hint_cancel")), hint("Ctrl+Q", localization.Get("hint_quit"))}
		}
		return []navigationHint{hint("Esc", localization.Get("hint_back")), hint("Ctrl+Q", localization.Get("hint_quit"))}
	case constants.EIP712SignView:
		if m.eip712Sign != nil && m.eip712Sign.phase == eip712SignPassword && m.credentialToggleEligible(m.eip712Sign.account) {
			return []navigationHint{hint("Enter", localization.Get("hint_confirm")), hint("Ctrl+K", localization.Get("hint_keepass_toggle")), hint("Esc", localization.Get("hint_cancel")), hint("Ctrl+Q", localization.Get("hint_quit"))}
		}
		return []navigationHint{hint("Esc", localization.Get("hint_back")), hint("Ctrl+Q", localization.Get("hint_quit"))}
	case constants.ContractCallView:
		if m.contractCall != nil && m.contractCall.phase == contractCallPassword && m.credentialToggleEligible(m.contractCall.account) {
			return []navigationHint{hint("Enter", localization.Get("hint_confirm")), hint("Ctrl+K", localization.Get("hint_keepass_toggle")), hint("Esc", localization.Get("hint_cancel")), hint("Ctrl+Q", localization.Get("hint_quit"))}
		}
		return []navigationHint{hint("Esc", localization.Get("hint_back")), hint("Ctrl+Q", localization.Get("hint_quit"))}
	case constants.DefaultView:
		return []navigationHint{hint("Up/Down", localization.Get("hint_select")), hint("Enter", localization.Get("hint_open")), hint("Ctrl+Q", localization.Get("hint_quit"))}
	case constants.ConfigurationView, constants.ImportMethodSelectionView, constants.LanguageSelectionView, constants.NetworkMenuView, constants.NetworkListView:
		return []navigationHint{hint("Up/Down", localization.Get("hint_select")), hint("Enter", localization.Get("hint_open")), hint("Esc", localization.Get("hint_back")), hint("Ctrl+Q", localization.Get("hint_quit"))}
	default:
		return []navigationHint{hint("Esc", localization.Get("hint_back")), hint("Ctrl+Q", localization.Get("hint_quit"))}
	}
}

func packNavigationHints(hints []navigationHint, width int) []string {
	inner := max(1, width)
	var lines []string
	var current strings.Builder
	for _, hint := range hints {
		if hint.key == "" {
			continue
		}
		token := hint.key + " " + hint.label
		for _, piece := range strings.Split(ansi.Hardwrap(token, inner, true), "\n") {
			if current.Len() == 0 {
				current.WriteString(piece)
				continue
			}
			if lipgloss.Width(current.String())+3+lipgloss.Width(piece) <= inner {
				current.WriteString(" | " + piece)
				continue
			}
			lines = append(lines, current.String())
			current.Reset()
			current.WriteString(piece)
		}
	}
	if current.Len() > 0 {
		lines = append(lines, current.String())
	}
	return lines
}

func (m *CLIModel) renderStatusBar() string {
	width := max(1, m.width)
	// Left part: Number of wallets
	leftStyle := m.styles.StatusBarLeft // Used assignment for copying.
	left := leftStyle.
		SetString(localization.T("footer_wallets_count", map[string]interface{}{"Count": m.walletCount})).
		String()

	// Right part: Current date and time
	currentTime := formatDisplayTime(m.displayTime)
	rightStyle := m.styles.StatusBarRight // Used assignment for copying.
	right := rightStyle.
		SetString(localization.T("footer_date", map[string]interface{}{"Date": currentTime})).
		String()

	// Center part: Current view and shortcut keys
	centerStyle := m.styles.StatusBarCenter // Used assignment for copying.
	viewName := m.footerViewName()
	hints := m.navigationHints()
	centerText := localization.Get("footer_view_prefix") + viewName
	if joined := renderNavigationHints(hints); joined != "" {
		centerText += " | " + joined
	}
	if lipgloss.Width(left)+lipgloss.Width(right)+lipgloss.Width(centerText)+centerStyle.GetHorizontalFrameSize() <= width {
		centerWidth := width - lipgloss.Width(left) - lipgloss.Width(right)
		center := centerStyle.
			SetString(centerText).
			Width(centerWidth).
			Align(lipgloss.Center).
			String()
		// Join all parts
		return lipgloss.JoinHorizontal(lipgloss.Top, left, center, right)
	}

	metadataText := localization.Get("footer_view_prefix") + viewName
	leftWidth := lipgloss.Width(left)
	rightWidth := lipgloss.Width(right)
	showRight := true
	showLeft := true
	available := width - leftWidth - rightWidth - centerStyle.GetHorizontalFrameSize()
	if available < 12 {
		showRight = false
		available += rightWidth
	}
	if available < 12 {
		showLeft = false
		available += leftWidth
	}
	metadataText = ansi.Truncate(metadataText, max(1, available), "")
	metadataCenter := centerStyle.
		SetString(metadataText).
		Width(width - boolToInt(showLeft, leftWidth) - boolToInt(showRight, rightWidth)).
		Align(lipgloss.Center).
		String()
	var metadataParts []string
	if showLeft {
		metadataParts = append(metadataParts, left)
	}
	metadataParts = append(metadataParts, metadataCenter)
	if showRight {
		metadataParts = append(metadataParts, right)
	}
	metadata := lipgloss.JoinHorizontal(lipgloss.Top, metadataParts...)

	hintLines := packNavigationHints(hints, width-centerStyle.GetHorizontalFrameSize())
	if len(hintLines) == 0 {
		return metadata
	}
	var actions strings.Builder
	for index, line := range hintLines {
		if index > 0 {
			actions.WriteString("\n")
		}
		actions.WriteString(centerStyle.Width(width).Render(line))
	}
	return metadata + "\n" + actions.String()
}

func boolToInt(flag bool, value int) int {
	if flag {
		return value
	}
	return 0
}
