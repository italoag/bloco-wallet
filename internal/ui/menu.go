package ui

import (
	"blocowallet/internal/constants"
	"blocowallet/internal/evm"
	"blocowallet/pkg/config"
	"blocowallet/pkg/localization"

	"github.com/charmbracelet/bubbles/key"
)

// menuItem representa uma única opção no menu
type menuItem struct {
	title       string
	description string
	action      string
	value       string
}

// Title retorna o título do menuItem
func (i menuItem) Title() string {
	return i.title
}

// Description retorna a descrição do menuItem
func (i menuItem) Description() string {
	return i.description
}

// FilterValue retorna o valor de filtro do menuItem
func (i menuItem) FilterValue() string {
	return i.title
}

// NewMenu cria e retorna uma lista de itens do menu
func NewMenu() []menuItem {
	return []menuItem{
		{title: localization.Get("create_new_wallet"), description: localization.Get("create_new_wallet_desc"), action: "create_wallet"},
		{title: localization.Get("import_wallet"), description: localization.Get("import_wallet_desc"), action: "import_wallet"},
		{title: localization.Get("list_wallets"), description: localization.Get("list_wallets_desc"), action: "list_wallets"},
		{title: localization.Get("safe_menu_title"), description: localization.Get("safe_menu_description"), action: "safe"},
		{title: localization.Get("configuration"), description: localization.Get("configuration_desc"), action: "configuration"},
		{title: localization.Get("exit"), description: localization.Get("exit_desc"), action: "exit"},
	}
}

// NewImportMenu cria e retorna uma lista de itens do menu de importação
func NewImportMenu() []menuItem {
	return []menuItem{
		{title: localization.Get("import_mnemonic"), description: localization.Get("import_mnemonic_desc"), action: "mnemonic"},
		{title: localization.Get("import_private_key"), description: localization.Get("import_private_key_desc"), action: "private_key"},
		{title: localization.Get("import_keystore"), description: localization.Get("import_keystore_desc"), action: "keystore"},
		{title: localization.Get("import_batch_keystore"), description: localization.Get("import_batch_keystore_desc"), action: "keystore_batch"},
		{title: localization.Get("import_encrypted_backup"), description: localization.Get("import_encrypted_backup_desc"), action: "bloco_encrypted"},
		{title: localization.Get("import_watch_only"), description: localization.Get("import_watch_only_desc"), action: "watch_only"},
		{title: localization.Get("import_batch_mnemonic"), description: localization.Get("import_batch_mnemonic_desc"), action: "mnemonic_batch"},
		{title: localization.Get("back_to_menu"), description: localization.Get("back_to_menu_desc"), action: "back"},
	}
}

// NewConfigMenu cria e retorna uma lista de itens do menu de configuração
func NewConfigMenu() []menuItem {
	return []menuItem{
		{title: localization.Get("networks"), description: localization.Get("networks_desc"), action: "networks"},
		{title: localization.Get("language"), description: localization.Get("language_desc"), action: "language"},
		{title: localization.Get("back_to_menu"), description: localization.Get("back_to_menu_desc"), action: "back"},
	}
}

// NewNetworkMenu cria e retorna uma lista de itens do menu de redes
func NewNetworkMenu() []menuItem {
	return []menuItem{
		{title: localization.Get("add_network"), description: localization.Get("add_network_desc"), action: "add_network"},
		{title: localization.Get("network_list"), description: localization.Get("network_list_desc"), action: "network_list"},
		{title: localization.Get("back_to_menu"), description: localization.Get("back_to_menu_desc"), action: "back"},
	}
}

// NewLanguageMenu cria e retorna uma lista de itens do menu de idiomas
func NewLanguageMenu(cfg *config.Config) []menuItem {
	languages := localization.GetAvailableLanguages(cfg.LocaleDir)
	current := localization.NormalizeLanguage(cfg.Language)

	menuItems := make([]menuItem, 0, len(languages)+1)
	for _, lang := range languages {
		langName := localization.GetLanguageName(lang)
		title := langName
		if lang == current {
			title = langName + " ✓ " + localization.Get("current")
		}
		menuItems = append(menuItems, menuItem{
			title:       title,
			description: localization.T("language_code_label", map[string]interface{}{"Code": lang}),
			action:      "select_language",
			value:       lang,
		})
	}

	menuItems = append(menuItems, menuItem{
		title:       localization.Get("back_to_menu"),
		description: localization.Get("back_to_menu_desc"),
		action:      "back",
	})

	return menuItems
}

// sameMenuActions reports whether two item lists share the same ordered
// stable action IDs.
func sameMenuActions(a, b []menuItem) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].action != b[i].action || a[i].value != b[i].value {
			return false
		}
	}
	return true
}

// refreshMenuItems rebuilds the installed menu with fresh localized titles and
// descriptions, preserving each item's stable action/value and the current
// selection index.
func (m *CLIModel) refreshMenuItems() {
	if len(m.menuItems) == 0 {
		return
	}
	candidates := [][]menuItem{NewMenu(), NewConfigMenu(), NewNetworkMenu(), NewImportMenu()}
	if m.currentConfig != nil {
		candidates = append(candidates, NewLanguageMenu(m.currentConfig))
	}
	for _, template := range candidates {
		if sameMenuActions(template, m.menuItems) {
			m.menuItems = template
			return
		}
	}
}

// refreshLocalizedUI reapplies localized presentation on already-built
// components after a language change. It only touches labels, placeholders,
// table column titles and help descriptions — never input values, cursor
// positions, selections, secrets, or internal ID mappings.
func (m *CLIModel) refreshLocalizedUI() {
	m.refreshMenuItems()
	m.refreshCanonicalPlaceholders()
	m.refreshWalletDetailsHelp()
	m.refreshWalletTableColumns()
	m.addNetworkComponent.refreshLocalizedLabels()
	m.networkListComponent.refreshLocalizedColumns()
	m.networkListComponent.refreshLocalizedRows()
	m.networkListComponent.refreshKeyHelp()
	m.refreshFormPlaceholders()
	m.refreshAuxiliaryKeyHelp()
}

// refreshCanonicalPlaceholders reapplies localized placeholders on the active
// canonical import fields without touching entered values or focus.
func (m *CLIModel) refreshCanonicalPlaceholders() {
	if m.canonicalImport == nil {
		return
	}
	for index := range m.canonicalImport.fields {
		field := &m.canonicalImport.fields[index]
		field.input.Placeholder = localization.Get(field.labelKey)
	}
}

// refreshWalletTableColumns reapplies localized column titles preserving the
// existing widths and rows.
func (m *CLIModel) refreshWalletTableColumns() {
	columns := m.walletTable.Columns()
	var keys []string
	switch len(columns) {
	case 4: // canonical account table
		keys = []string{"name", "wallet_type", "created_at", "ethereum_address"}
	case 5: // legacy wallet table
		keys = []string{"id", "name", "wallet_type", "created_at", "ethereum_address"}
	default:
		return
	}
	for i, key := range keys {
		columns[i].Title = localization.Get(key)
	}
	m.walletTable.SetColumns(columns)
}

// refreshFormPlaceholders reapplies localized placeholders on active form
// inputs (recovery, account deletion, create/vault flows) without touching
// their values, focus, or stage.
func (m *CLIModel) refreshFormPlaceholders() {
	if state := m.recovery; state != nil {
		state.password.Placeholder = localization.Get("recovery_placeholder_password")
		state.destination.Placeholder = localization.Get("recovery_placeholder_destination")
		if state.selected >= 0 && state.selected < len(state.actions) {
			if state.actions[state.selected].export {
				state.confirmation.Placeholder = localization.Get("recovery_placeholder_export")
			} else {
				state.confirmation.Placeholder = localization.Get("recovery_placeholder_reveal")
			}
		}
	}
	if state := m.accountDeletion; state != nil {
		state.confirmation.Placeholder = localization.Get("delete_confirm_placeholder")
		state.password.Placeholder = localization.Get("vault_storage_password_placeholder")
	}
	if state := m.personalSign; state != nil {
		state.message.Placeholder = localization.Get("sign_message_placeholder")
		state.password.Placeholder = localization.Get("sign_storage_password_placeholder")
	}
	if state := m.eip712Sign; state != nil {
		state.typedData.Placeholder = localization.Get("eip712_placeholder")
		state.password.Placeholder = localization.Get("sign_storage_password_placeholder")
	}
	if state := m.nativeTransfer; state != nil {
		state.contractInput.Placeholder = localization.Get("tx_contract_placeholder")
		state.recipientInput.Placeholder = localization.Get("tx_recipient_placeholder")
		if state.operation == evm.OperationERC721SafeTransfer {
			state.amountInput.Placeholder = localization.Get("tx_token_id_placeholder")
		} else {
			state.amountInput.Placeholder = localization.Get("tx_amount_placeholder")
		}
		state.passwordInput.Placeholder = localization.Get("sign_storage_password_placeholder")
		state.confirmationInput.Placeholder = localization.Get("call_approve_placeholder")
	}
	if state := m.fido2; state != nil {
		state.response.Placeholder = localization.Get("fido2_response_placeholder")
	}
	if state := m.contractCall; state != nil {
		if input, ok := state.inputs["contract"]; ok {
			input.Placeholder = localization.Get("call_addr_placeholder")
		}
		if input, ok := state.inputs["abi"]; ok {
			input.Placeholder = localization.Get("call_abi_placeholder")
		}
		if input, ok := state.inputs["method"]; ok {
			input.Placeholder = localization.Get("call_method_placeholder")
		}
		if input, ok := state.inputs["args"]; ok {
			input.Placeholder = localization.Get("call_args_placeholder")
		}
		if input, ok := state.inputs["value"]; ok {
			input.Placeholder = localization.Get("call_value_placeholder")
		}
		if input, ok := state.inputs["confirm"]; ok {
			input.Placeholder = localization.Get("call_approve_placeholder")
		}
		if input, ok := state.inputs["password"]; ok {
			input.Placeholder = localization.Get("sign_storage_password_placeholder")
		}
	}
	if state := m.safeView; state != nil {
		state.nameInput.Placeholder = localization.Get("safe_name_placeholder")
		state.ownersInput.Placeholder = localization.Get("safe_owners_placeholder")
		state.thresholdInput.Placeholder = localization.Get("safe_threshold_placeholder")
		state.importNameInput.Placeholder = localization.Get("safe_name_placeholder")
		state.importAddrInput.Placeholder = localization.Get("safe_import_addr_placeholder")
		state.proposeToInput.Placeholder = localization.Get("safe_to_placeholder")
		state.proposeValueInput.Placeholder = localization.Get("safe_value_placeholder")
		state.proposeDataInput.Placeholder = localization.Get("safe_data_placeholder")
	}
	m.nameInput.Placeholder = localization.Get("wallet_name_placeholder")
	m.createWordCountInput.Placeholder = localization.Get("word_count_placeholder")
	m.createLanguageInput.Placeholder = localization.Get("bip39_language_placeholder")
	m.createPassphraseInput.Placeholder = localization.Get("passphrase_optional_placeholder")
	m.createDerivationPathInput.Placeholder = localization.Get("evm_path_placeholder")
	m.createPasswordConfirmationInput.Placeholder = localization.Get("confirm_storage_placeholder")
	m.currentPasswordInput.Placeholder = localization.Get("current_storage_placeholder")
	m.newPasswordInput.Placeholder = localization.Get("new_password_placeholder")
	m.confirmPasswordInput.Placeholder = localization.Get("confirm_storage_placeholder")
	m.exportDestinationInput.Placeholder = localization.Get("export_destination_placeholder")
	m.backupPathInput.Placeholder = localization.Get("backup_reenter_path")
	m.backupLanguageInput.Placeholder = localization.Get("backup_reenter_language")
	m.backupPassphraseInput.Placeholder = localization.Get("backup_reenter_passphrase")
	if m.passwordInput.Placeholder != "" {
		if m.currentView == constants.WalletPasswordView {
			m.passwordInput.Placeholder = localization.Get("enter_wallet_password")
		} else {
			m.passwordInput.Placeholder = localization.Get("enter_password")
		}
	}
	m.refreshCreateOptionList()
}

// refreshCreateOptionList rebuilds the creation-flow selector items with fresh
// localized titles, preserving the current selection index and item values.
func (m *CLIModel) refreshCreateOptionList() {
	if len(m.createOptionList.Items()) == 0 {
		return
	}
	index := m.createOptionList.Index()
	m.configureCreateOptionList(m.createOptionsStage)
	m.createOptionList.Select(index)
}

// refreshAuxiliaryKeyHelp reapplies localized help descriptions on the keymaps
// of active secondary flows (history, signing, WalletConnect, FIDO2) without
// changing binding keys or enabled state.
func (m *CLIModel) refreshAuxiliaryKeyHelp() {
	if state := m.accountHistory; state != nil {
		refreshKeyDesc(&state.keys.Up, "hist_scroll_up")
		refreshKeyDesc(&state.keys.Down, "hist_scroll_down")
		refreshKeyDesc(&state.keys.PageUp, "hist_page_up")
		refreshKeyDesc(&state.keys.PageDown, "hist_page_down")
		refreshKeyDesc(&state.keys.Next, "hist_next")
		refreshKeyDesc(&state.keys.Previous, "hist_previous")
		refreshKeyDesc(&state.keys.Refresh, "hist_refresh")
		refreshKeyDesc(&state.keys.ToggleHelp, "hist_more_help")
		refreshKeyDesc(&state.keys.Back, "hist_back")
	}
	if state := m.personalSign; state != nil {
		refreshKeyDesc(&state.keys.Message, "sign_edit_message")
		refreshKeyDesc(&state.keys.Prev, "sign_prev_step")
		refreshKeyDesc(&state.keys.Next, "sign_next_step")
		refreshKeyDesc(&state.keys.Approve, "sign_approve")
		refreshKeyDesc(&state.keys.Back, "hist_back")
	}
	if state := m.eip712Sign; state != nil {
		refreshKeyDesc(&state.keys.Up, "eip712_prev_network")
		refreshKeyDesc(&state.keys.Down, "eip712_next_network")
		refreshKeyDesc(&state.keys.Prev, "sign_prev_step")
		refreshKeyDesc(&state.keys.Next, "sign_next_step")
		refreshKeyDesc(&state.keys.Approve, "sign_approve")
		refreshKeyDesc(&state.keys.Back, "hist_back")
	}
	if state := m.contractCall; state != nil {
		refreshKeyDesc(&state.keys.Up, "eip712_prev_network")
		refreshKeyDesc(&state.keys.Down, "eip712_next_network")
		refreshKeyDesc(&state.keys.Prev, "sign_prev_step")
		refreshKeyDesc(&state.keys.Next, "sign_next_step")
		refreshKeyDesc(&state.keys.Approve, "call_approve_send")
		refreshKeyDesc(&state.keys.Back, "hist_back")
	}
	if state := m.walletConnect; state != nil {
		refreshKeyDesc(&state.keys.Up, "wc_previous")
		refreshKeyDesc(&state.keys.Down, "wc_next")
		refreshKeyDesc(&state.keys.Select, "wc_select")
		refreshKeyDesc(&state.keys.Revoke, "wc_revoke")
		refreshKeyDesc(&state.keys.Approve, "wc_approve")
		refreshKeyDesc(&state.keys.Reject, "wc_reject")
		refreshKeyDesc(&state.keys.Back, "hist_back")
	}
	if state := m.fido2; state != nil {
		refreshKeyDesc(&state.keys.Up, "fido2_previous")
		refreshKeyDesc(&state.keys.Down, "fido2_next")
		refreshKeyDesc(&state.keys.Register, "fido2_register")
		refreshKeyDesc(&state.keys.Auth, "fido2_authenticate")
		refreshKeyDesc(&state.keys.Submit, "fido2_submit")
		refreshKeyDesc(&state.keys.Back, "hist_back")
	}
}

// refreshKeyDesc replaces a binding's help description in place, preserving
// its keys and enabled state.
func refreshKeyDesc(binding *key.Binding, labelKey string) {
	help := binding.Help()
	binding.SetHelp(help.Key, localization.Get(labelKey))
}
