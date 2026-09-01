package ui

import (
	"fmt"
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
	ToggleHelp      key.Binding
	Back            key.Binding
}

func newWalletDetailsKeyMap() WalletDetailsKeyMap {
	return WalletDetailsKeyMap{
		Up:              key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "scroll up")),
		Down:            key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "scroll down")),
		PageUp:          key.NewBinding(key.WithKeys("pgup", "u"), key.WithHelp("pgup/u", "page up")),
		PageDown:        key.NewBinding(key.WithKeys("pgdown", "d"), key.WithHelp("pgdn/d", "page down")),
		Lock:            key.NewBinding(key.WithKeys("l"), key.WithHelp("l", "Lock")),
		Rotate:          key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "Rotate password")),
		Export:          key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "Export Keystore")),
		EncryptedExport: key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "Encrypted backup")),
		FetchBalances:   key.NewBinding(key.WithKeys("f"), key.WithHelp("f", "Fetch balances")),
		History:         key.NewBinding(key.WithKeys("h"), key.WithHelp("h", "Local history")),
		SignMessage:     key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "Sign message")),
		SignTypedData:   key.NewBinding(key.WithKeys("v"), key.WithHelp("v", "Sign typed data")),
		SendNative:      key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "Send native")),
		SendToken:       key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "Send ERC-20")),
		SendNFT:         key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "Send NFT")),
		Send1155:        key.NewBinding(key.WithKeys("m"), key.WithHelp("m", "Send 1155")),
		Send1155Batch:   key.NewBinding(key.WithKeys("z"), key.WithHelp("z", "Send 1155 batch")),
		ContractCall:    key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "Contract call")),
		WalletConnect:   key.NewBinding(key.WithKeys("w"), key.WithHelp("w", "WC sessions")),
		FIDO2:           key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "Security keys")),
		ApproveToken:    key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "Approve ERC-20")),
		ResumeBackup:    key.NewBinding(key.WithKeys("b"), key.WithHelp("b", "Resume backup")),
		ToggleHelp:      key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "More help")),
		Back:            key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
	}
}

func (keyMap WalletDetailsKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{keyMap.Lock, keyMap.FetchBalances, keyMap.History, keyMap.SignMessage, keyMap.SignTypedData, keyMap.SendNative, keyMap.SendToken, keyMap.SendNFT, keyMap.Send1155, keyMap.Send1155Batch, keyMap.ContractCall, keyMap.WalletConnect, keyMap.FIDO2, keyMap.ResumeBackup, keyMap.Back}
}

func (keyMap WalletDetailsKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{keyMap.Up, keyMap.Down, keyMap.PageUp, keyMap.PageDown},
		{keyMap.Lock, keyMap.Rotate, keyMap.Export, keyMap.EncryptedExport},
		{keyMap.FetchBalances, keyMap.History, keyMap.SignMessage, keyMap.SignTypedData, keyMap.SendNative, keyMap.SendToken, keyMap.SendNFT, keyMap.Send1155, keyMap.Send1155Batch, keyMap.ContractCall, keyMap.WalletConnect, keyMap.FIDO2, keyMap.ApproveToken},
		{keyMap.ResumeBackup, keyMap.ToggleHelp, keyMap.Back},
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
		return localization.Labels["select_wallet_prompt"]
	}
	cardWidth := max(36, min(78, model.walletDetailsViewport.Width-4))
	card := lipgloss.NewStyle().Width(cardWidth).Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("#7D56F4")).Padding(0, 1)
	identity := strings.Join([]string{
		"Name: " + safeShort(account.Name),
		"Address: " + safeShort(account.Address),
		"Account ID: " + safeShort(account.AccountID),
		"Signer: " + safeShort(string(account.SignerKind)),
		"State: " + safeShort(string(account.State)),
	}, "\n")
	securityLines := []string{
		"Derivation path: " + safeShort(account.DerivationPath),
		"BIP39 language: " + safeShort(account.BIP39Language),
		fmt.Sprintf("BIP39 passphrase configured: %t", account.HasBIP39Passphrase),
		"Related account: " + safeShort(account.RelatedAccountID),
	}
	if account.SignerKind == wallet.SignerKindWatchOnly {
		securityLines = []string{"Custody: Watch-only", "No signing secrets are stored for this account.", "Related account: " + safeShort(account.RelatedAccountID)}
	}
	security := strings.Join(securityLines, "\n")
	sections := []string{
		lipgloss.NewStyle().Bold(true).Render(localization.Labels["wallet_details_title"]),
		card.Render(lipgloss.NewStyle().Bold(true).Render("Identity") + "\n" + identity),
		card.Render(lipgloss.NewStyle().Bold(true).Render("Security and derivation") + "\n" + security),
	}
	if model.balanceLoading {
		sections = append(sections, card.Render("Balances\nLoading after explicit approval..."))
	} else if len(model.networkBalances) > 0 || model.balanceError != "" {
		lines := []string{lipgloss.NewStyle().Bold(true).Render("Balances")}
		for _, balance := range model.networkBalances {
			if balance.Error != nil {
				lines = append(lines, safeShort(balance.NetworkName)+": unavailable ("+safeError(balance.Error)+")")
			} else {
				lines = append(lines, safeShort(balance.NetworkName)+": "+formatNativeAmount(balance.Amount, balance.Decimals)+" "+safeShort(balance.Symbol))
			}
		}
		if model.balanceError != "" {
			lines = append(lines, "Provider warning: "+safeInline(model.balanceError))
		}
		sections = append(sections, card.Render(strings.Join(lines, "\n")))
	}
	notices := make([]string, 0, 3)
	if model.balanceProvider != nil {
		notices = append(notices, "Privacy: fetching balances sends this public address to each active provider.")
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
