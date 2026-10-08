package ui

import (
	"strings"

	"blocowallet/internal/wallet"
	"blocowallet/pkg/localization"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/lipgloss"
)

type WalletDetailsKeyMap struct {
	Up              key.Binding
	Down            key.Binding
	PageUp          key.Binding
	PageDown        key.Binding
	Lock            key.Binding
	Rotate          key.Binding
	Export          key.Binding
	EncryptedExport key.Binding
	Recovery        key.Binding
	FetchBalances   key.Binding
	History         key.Binding
	SignMessage     key.Binding
	SignTypedData   key.Binding
	SendNative      key.Binding
	SendToken       key.Binding
	SendNFT         key.Binding
	Send1155        key.Binding
	Send1155Batch   key.Binding
	ContractCall    key.Binding
	WalletConnect   key.Binding
	FIDO2           key.Binding
	ApproveToken    key.Binding
	ResumeBackup    key.Binding
	KeePass         key.Binding
	ToggleHelp      key.Binding
	Back            key.Binding
}

func newWalletDetailsKeyMap() WalletDetailsKeyMap {
	return WalletDetailsKeyMap{
		Up:              key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", localization.Get("help_scroll_up"))),
		Down:            key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", localization.Get("help_scroll_down"))),
		PageUp:          key.NewBinding(key.WithKeys("pgup", "u"), key.WithHelp("pgup/u", localization.Get("help_page_up"))),
		PageDown:        key.NewBinding(key.WithKeys("pgdown", "d"), key.WithHelp("pgdn/d", localization.Get("help_page_down"))),
		Lock:            key.NewBinding(key.WithKeys("l"), key.WithHelp("l", localization.Get("help_lock"))),
		Rotate:          key.NewBinding(key.WithKeys("r"), key.WithHelp("r", localization.Get("help_rotate"))),
		Export:          key.NewBinding(key.WithKeys("e"), key.WithHelp("e", localization.Get("help_export_keystore"))),
		EncryptedExport: key.NewBinding(key.WithKeys("x"), key.WithHelp("x", localization.Get("help_encrypted_backup"))),
		Recovery:        key.NewBinding(key.WithKeys("R"), key.WithHelp("R", localization.Get("help_recovery"))),
		FetchBalances:   key.NewBinding(key.WithKeys("f"), key.WithHelp("f", localization.Get("help_fetch_balances"))),
		History:         key.NewBinding(key.WithKeys("h"), key.WithHelp("h", localization.Get("help_history"))),
		SignMessage:     key.NewBinding(key.WithKeys("s"), key.WithHelp("s", localization.Get("help_sign_message"))),
		SignTypedData:   key.NewBinding(key.WithKeys("v"), key.WithHelp("v", localization.Get("help_sign_typed_data"))),
		SendNative:      key.NewBinding(key.WithKeys("n"), key.WithHelp("n", localization.Get("help_send_native"))),
		SendToken:       key.NewBinding(key.WithKeys("t"), key.WithHelp("t", localization.Get("help_send_erc20"))),
		SendNFT:         key.NewBinding(key.WithKeys("o"), key.WithHelp("o", localization.Get("help_send_nft"))),
		Send1155:        key.NewBinding(key.WithKeys("m"), key.WithHelp("m", localization.Get("help_send_1155"))),
		Send1155Batch:   key.NewBinding(key.WithKeys("z"), key.WithHelp("z", localization.Get("help_send_1155_batch"))),
		ContractCall:    key.NewBinding(key.WithKeys("c"), key.WithHelp("c", localization.Get("help_contract_call"))),
		WalletConnect:   key.NewBinding(key.WithKeys("w"), key.WithHelp("w", localization.Get("help_wc_sessions"))),
		FIDO2:           key.NewBinding(key.WithKeys("p"), key.WithHelp("p", localization.Get("help_security_keys"))),
		ApproveToken:    key.NewBinding(key.WithKeys("a"), key.WithHelp("a", localization.Get("help_approve_erc20"))),
		ResumeBackup:    key.NewBinding(key.WithKeys("b"), key.WithHelp("b", localization.Get("help_resume_backup"))),
		KeePass:         key.NewBinding(key.WithKeys("K"), key.WithHelp("K", localization.Get("help_keepass"))),
		ToggleHelp:      key.NewBinding(key.WithKeys("?"), key.WithHelp("?", localization.Get("help_more"))),
		Back:            key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", localization.Get("help_back"))),
	}
}

// refreshWalletDetailsHelp reapplies localized help descriptions on the
// existing bindings, preserving keys and Enabled state.
func (model *CLIModel) refreshWalletDetailsHelp() {
	type entry struct {
		binding *key.Binding
		key     string
	}
	k := &model.walletDetailsKeys
	for _, e := range []entry{
		{&k.Up, "help_scroll_up"},
		{&k.Down, "help_scroll_down"},
		{&k.PageUp, "help_page_up"},
		{&k.PageDown, "help_page_down"},
		{&k.Lock, "help_lock"},
		{&k.Rotate, "help_rotate"},
		{&k.Export, "help_export_keystore"},
		{&k.EncryptedExport, "help_encrypted_backup"},
		{&k.Recovery, "help_recovery"},
		{&k.FetchBalances, "help_fetch_balances"},
		{&k.History, "help_history"},
		{&k.SignMessage, "help_sign_message"},
		{&k.SignTypedData, "help_sign_typed_data"},
		{&k.SendNative, "help_send_native"},
		{&k.SendToken, "help_send_erc20"},
		{&k.SendNFT, "help_send_nft"},
		{&k.Send1155, "help_send_1155"},
		{&k.Send1155Batch, "help_send_1155_batch"},
		{&k.ContractCall, "help_contract_call"},
		{&k.WalletConnect, "help_wc_sessions"},
		{&k.FIDO2, "help_security_keys"},
		{&k.ApproveToken, "help_approve_erc20"},
		{&k.ResumeBackup, "help_resume_backup"},
		{&k.KeePass, "help_keepass"},
		{&k.ToggleHelp, "help_more"},
		{&k.Back, "help_back"},
	} {
		e.binding.SetHelp(e.binding.Help().Key, localization.Get(e.key))
	}
}

func (keyMap WalletDetailsKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{keyMap.Lock, keyMap.Recovery, keyMap.FetchBalances, keyMap.History, keyMap.SignMessage, keyMap.SignTypedData, keyMap.SendNative, keyMap.SendToken, keyMap.SendNFT, keyMap.Send1155, keyMap.Send1155Batch, keyMap.ContractCall, keyMap.WalletConnect, keyMap.FIDO2, keyMap.KeePass, keyMap.ResumeBackup, keyMap.Back}
}

func (keyMap WalletDetailsKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{keyMap.Up, keyMap.Down, keyMap.PageUp, keyMap.PageDown},
		{keyMap.Lock, keyMap.Rotate, keyMap.Export, keyMap.EncryptedExport, keyMap.Recovery},
		{keyMap.FetchBalances, keyMap.History, keyMap.SignMessage, keyMap.SignTypedData, keyMap.SendNative, keyMap.SendToken, keyMap.SendNFT, keyMap.Send1155, keyMap.Send1155Batch, keyMap.ContractCall, keyMap.WalletConnect, keyMap.FIDO2, keyMap.ApproveToken},
		{keyMap.ResumeBackup, keyMap.KeePass, keyMap.ToggleHelp, keyMap.Back},
	}
}

func (model *CLIModel) walletDetailsDimensions() (int, int) {
	width := model.width
	if width <= 0 {
		width = 100
	}
	height := model.height
	if height <= 0 {
		height = 40
	}
	return max(40, width-6), max(8, height-12)
}

func (model *CLIModel) initWalletDetailsComponents() {
	model.walletDetailsKeys = newWalletDetailsKeyMap()
	model.walletDetailsHelp = help.New()
	width, height := model.walletDetailsDimensions()
	model.walletDetailsViewport = viewport.New(width, height)
	model.walletDetailsViewport.Style = lipgloss.NewStyle().Padding(0, 1)
	model.refreshWalletDetailsComponents()
}

func (model *CLIModel) refreshWalletDetailsComponents() {
	if model.walletDetailsHelp.Width == 0 && model.walletDetailsViewport.Width == 0 {
		model.initWalletDetailsComponents()
		return
	}
	width, height := model.walletDetailsDimensions()
	model.walletDetailsViewport.Width = width
	model.walletDetailsViewport.Height = height
	model.walletDetailsHelp.Width = width
	model.updateWalletDetailsKeyAvailability()
	model.walletDetailsViewport.SetContent(model.walletDetailsContent())
}

func (model *CLIModel) updateWalletDetailsKeyAvailability() {
	account := model.selectedAccount
	hasAccount := account != nil
	hasVault := model.Vault != nil && hasAccount
	pendingBackup := hasAccount && account.State == wallet.AccountStatePendingBackup
	custodial := hasVault && account.SignerKind == wallet.SignerKindSoftware
	canExport := custodial && account.Capabilities&wallet.CapabilityExportSecret != 0 && !pendingBackup
	canTransact := hasAccount && model.transactionEngineFactory != nil && model.transactionAuthorizer != nil && account.SignerKind.SupportsEOASigning() && account.Capabilities&wallet.CapabilitySignTransaction != 0 && (account.State == wallet.AccountStateActive || account.State == wallet.AccountStateLocked)
	model.walletDetailsKeys.Lock.SetEnabled(custodial && !pendingBackup)
	model.walletDetailsKeys.Rotate.SetEnabled(custodial && !pendingBackup)
	model.walletDetailsKeys.Export.SetEnabled(canExport)
	model.walletDetailsKeys.EncryptedExport.SetEnabled(canExport)
	model.walletDetailsKeys.Recovery.SetEnabled(canExport && (account.State == wallet.AccountStateActive || account.State == wallet.AccountStateLocked))
	model.walletDetailsKeys.KeePass.SetEnabled(canExport && (account.State == wallet.AccountStateActive || account.State == wallet.AccountStateLocked) && model.credentialBackupEnabled())
	model.walletDetailsKeys.FetchBalances.SetEnabled(hasAccount && model.balanceProvider != nil && !pendingBackup)
	model.walletDetailsKeys.History.SetEnabled(hasAccount && model.historyReader != nil && !pendingBackup)
	model.walletDetailsKeys.SignMessage.SetEnabled(hasAccount && model.messageSigningFactory != nil && model.transactionAuthorizer != nil && account.SignerKind.SupportsEOASigning() && account.Capabilities&wallet.CapabilitySignMessage != 0 && !pendingBackup)
	model.walletDetailsKeys.SignTypedData.SetEnabled(hasAccount && model.messageSigningFactory != nil && model.transactionAuthorizer != nil && account.SignerKind.SupportsEOASigning() && account.Capabilities&wallet.CapabilitySignMessage != 0 && !pendingBackup)
	model.walletDetailsKeys.SendNative.SetEnabled(canTransact)
	model.walletDetailsKeys.SendToken.SetEnabled(canTransact)
	model.walletDetailsKeys.SendNFT.SetEnabled(canTransact)
	model.walletDetailsKeys.Send1155.SetEnabled(canTransact)
	model.walletDetailsKeys.Send1155Batch.SetEnabled(canTransact)
	model.walletDetailsKeys.ContractCall.SetEnabled(canTransact)
	model.walletDetailsKeys.WalletConnect.SetEnabled(hasAccount && model.walletConnectReader != nil && !pendingBackup)
	model.walletDetailsKeys.FIDO2.SetEnabled(hasAccount && model.fido2Service != nil && !pendingBackup)
	model.walletDetailsKeys.ApproveToken.SetEnabled(canTransact)
	model.walletDetailsKeys.ResumeBackup.SetEnabled(custodial && pendingBackup)
}

func (model *CLIModel) walletDetailsContent() string {
	account := model.selectedAccount
	if account == nil {
		return localization.Get("select_wallet_prompt")
	}
	cardWidth := max(36, min(78, model.walletDetailsViewport.Width-4))
	card := lipgloss.NewStyle().Width(cardWidth).Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("#7D56F4")).Padding(0, 1)
	identity := strings.Join([]string{
		localization.Get("detail_name") + ": " + safeShort(account.Name),
		localization.Get("detail_address") + ": " + safeShort(account.Address),
		localization.Get("detail_account_id") + ": " + safeShort(account.AccountID),
		localization.Get("detail_signer") + ": " + safeShort(string(account.SignerKind)),
		localization.Get("detail_state") + ": " + safeShort(string(account.State)),
	}, "\n")
	passphraseState := localization.Get("no")
	if account.HasBIP39Passphrase {
		passphraseState = localization.Get("yes")
	}
	securityLines := []string{
		localization.Get("detail_derivation_path") + ": " + safeShort(account.DerivationPath),
		localization.Get("detail_bip39_language") + ": " + safeShort(account.BIP39Language),
		localization.Get("detail_passphrase_configured") + ": " + passphraseState,
		localization.Get("detail_related_account") + ": " + safeShort(account.RelatedAccountID),
	}
	if account.SignerKind == wallet.SignerKindWatchOnly {
		securityLines = []string{localization.Get("detail_custody_watch_only"), localization.Get("detail_watch_only_note"), localization.Get("detail_related_account") + ": " + safeShort(account.RelatedAccountID)}
	}
	security := strings.Join(securityLines, "\n")
	sections := []string{
		lipgloss.NewStyle().Bold(true).Render(localization.Get("wallet_details_title")),
		card.Render(lipgloss.NewStyle().Bold(true).Render(localization.Get("wallet_details_identity")) + "\n" + identity),
		card.Render(lipgloss.NewStyle().Bold(true).Render(localization.Get("wallet_details_security")) + "\n" + security),
	}
	if model.balanceLoading {
		sections = append(sections, card.Render(localization.Get("wallet_details_balances")+"\n"+localization.Get("wallet_details_loading_balances")))
	} else if len(model.networkBalances) > 0 || model.balanceError != "" {
		lines := []string{lipgloss.NewStyle().Bold(true).Render(localization.Get("wallet_details_balances"))}
		for _, balance := range model.networkBalances {
			if balance.Error != nil {
				lines = append(lines, safeShort(balance.NetworkName)+": "+localization.Get("wallet_details_unavailable")+" ("+safeError(balance.Error)+")")
			} else {
				lines = append(lines, safeShort(balance.NetworkName)+": "+formatNativeAmount(balance.Amount, balance.Decimals)+" "+safeShort(balance.Symbol))
			}
		}
		if model.balanceError != "" {
			lines = append(lines, localization.Get("wallet_details_provider_warning")+": "+safeInline(model.balanceError))
		}
		sections = append(sections, card.Render(strings.Join(lines, "\n")))
	}
	notices := make([]string, 0, 3)
	if model.balanceProvider != nil {
		notices = append(notices, localization.Get("wallet_details_privacy_notice"))
	}
	if model.lastOperationNotice != "" {
		notices = append(notices, safeInline(model.lastOperationNotice))
	}
	if model.transactionNotice != "" {
		notices = append(notices, safeInline(model.transactionNotice))
	}
	if len(notices) > 0 {
		sections = append(sections, card.BorderForeground(lipgloss.Color("#E5C07B")).Render(strings.Join(notices, "\n")))
	}
	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}
