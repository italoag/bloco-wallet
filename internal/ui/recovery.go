package ui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"blocowallet/internal/constants"
	"blocowallet/internal/wallet"
	"blocowallet/pkg/localization"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

const recoveryRevealDuration = 60 * time.Second

const (
	recoveryStageMenu = iota
	recoveryStageDestination
	recoveryStagePassword
	recoveryStageConfirmation
	recoveryStageRevealed
)

type recoveryAction struct {
	kind     wallet.RecoverySecretKind
	export   bool
	labelKey string
	hint     string
}

type recoveryState struct {
	account         wallet.AccountSummary
	actions         []recoveryAction
	selected        int
	stage           int
	password        textinput.Model
	confirmation    textinput.Model
	destination     textinput.Model
	material        *wallet.RecoveryMaterial
	expiresAt       time.Time
	operationID     uint64
	cancel          context.CancelFunc
	busy            bool
	cancelling      bool
	exitAfterResult bool
	quitAfterResult bool
	scrollOffset    int
	err             string
	status          string
}

type recoveryResultMsg struct {
	operationID uint64
	accountID   string
	material    *wallet.RecoveryMaterial
	destination string
	err         error
	export      bool
}

func (recoveryResultMsg) String() string {
	return "[redacted recovery result]"
}

func (recoveryResultMsg) GoString() string {
	return "[redacted recovery result]"
}

type recoveryExpiredMsg struct {
	operationID uint64
}

func recoveryAvailable(account *wallet.AccountSummary, vault *wallet.WalletVault) bool {
	if vault == nil || account == nil {
		return false
	}
	if account.SignerKind != wallet.SignerKindSoftware || account.Capabilities&wallet.CapabilityExportSecret == 0 {
		return false
	}
	return account.State == wallet.AccountStateActive || account.State == wallet.AccountStateLocked
}

func recoveryActionsFor(summary wallet.AccountSummary) []recoveryAction {
	actions := make([]recoveryAction, 0, 6)
	if summary.SecretType == wallet.SecretTypeMnemonic {
		actions = append(actions,
			recoveryAction{kind: wallet.RecoveryMnemonic, labelKey: "recovery_show_mnemonic"},
			recoveryAction{kind: wallet.RecoveryMnemonic, export: true, labelKey: "recovery_export_mnemonic", hint: ".mnemonic"},
		)
		if summary.HasBIP39Passphrase {
			actions = append(actions,
				recoveryAction{kind: wallet.RecoveryPassphrase, labelKey: "recovery_show_passphrase"},
				recoveryAction{kind: wallet.RecoveryPassphrase, export: true, labelKey: "recovery_export_passphrase", hint: ".passphrase"},
			)
		}
	}
	actions = append(actions,
		recoveryAction{kind: wallet.RecoveryPrivateKey, labelKey: "recovery_show_key"},
		recoveryAction{kind: wallet.RecoveryPrivateKey, export: true, labelKey: "recovery_export_key", hint: ".key"},
	)
	return actions
}

func recoveryInputWidth(width int) int {
	if width < 40 {
		return 20
	}
	return min(76, width-8)
}

func (m *CLIModel) recoveryResizeInputs() {
	if m.recovery == nil {
		return
	}
	width := recoveryInputWidth(m.width)
	m.recovery.password.Width = width
	m.recovery.confirmation.Width = width
	m.recovery.destination.Width = width
}

func (m *CLIModel) initRecovery() {
	if !recoveryAvailable(m.selectedAccount, m.Vault) {
		return
	}
	if m.recovery != nil {
		m.clearRecovery()
	}
	summary := *m.selectedAccount
	password := textinput.New()
	password.Placeholder = localization.Get("recovery_placeholder_password")
	password.CharLimit = constants.PasswordCharLimit
	password.EchoMode = textinput.EchoPassword
	password.EchoCharacter = '•'
	confirmation := textinput.New()
	confirmation.CharLimit = 16
	destination := textinput.New()
	destination.Placeholder = localization.Get("recovery_placeholder_destination")
	destination.CharLimit = 1024
	m.recovery = &recoveryState{
		account:      summary,
		actions:      recoveryActionsFor(summary),
		password:     password,
		confirmation: confirmation,
		destination:  destination,
	}
	m.recoveryResizeInputs()
	m.lastOperationNotice = ""
	m.currentView = constants.RecoveryView
}

func (m *CLIModel) recoveryWipeOwned() {
	state := m.recovery
	if state == nil {
		return
	}
	if state.material != nil {
		state.material.Destroy()
		state.material = nil
	}
	state.expiresAt = time.Time{}
	state.scrollOffset = 0
	state.password.SetValue("")
	state.confirmation.SetValue("")
	state.password.Blur()
	state.confirmation.Blur()
	state.destination.Blur()
	state.err = ""
}

func (m *CLIModel) recoveryPrivacyWipe(status string) {
	state := m.recovery
	if state == nil {
		return
	}
	m.recoveryWipeOwned()
	if state.busy {
		if state.cancel != nil {
			state.cancel()
		}
		state.cancelling = true
		return
	}
	state.stage = recoveryStageMenu
	state.status = status
}

func (m *CLIModel) clearRecovery() {
	if m.recovery == nil {
		return
	}
	m.recoveryWipeOwned()
	if m.recovery.cancel != nil {
		m.recovery.cancel()
		m.recovery.cancel = nil
	}
	m.recovery = nil
}

func (m *CLIModel) updateRecovery(msg tea.Msg) (tea.Model, tea.Cmd) {
	state := m.recovery
	if state == nil {
		return m, nil
	}
	switch result := msg.(type) {
	case recoveryResultMsg:
		if !state.busy || result.operationID != state.operationID || result.accountID != state.account.AccountID {
			if result.material != nil {
				result.material.Destroy()
			}
			if result.export && (result.err == nil || wallet.IsExportCommitted(result.err)) {
				m.lastOperationNotice = localization.T("recovery_notice_export_created", map[string]interface{}{"Path": safeInline(result.destination)})
			}
			return m, nil
		}
		state.busy = false
		wasCancelling := state.cancelling
		state.cancelling = false
		if state.cancel != nil {
			state.cancel()
		}
		state.cancel = nil
		state.err = ""
		var notice string
		state.scrollOffset = 0
		if result.export {
			if result.material != nil {
				result.material.Destroy()
			}
			switch {
			case result.err == nil:
				state.status = localization.T("recovery_status_export_completed", map[string]interface{}{"Path": safeInline(result.destination)})
				notice = state.status
				state.stage = recoveryStageMenu
			case wallet.IsExportCommitted(result.err):
				state.status = localization.T("recovery_status_export_warning", map[string]interface{}{"Path": safeInline(result.destination)})
				notice = state.status
				state.stage = recoveryStageMenu
			case wasCancelling:
				state.status = localization.Get("recovery_status_export_cancelled")
				state.stage = recoveryStageMenu
			default:
				state.status = ""
				state.err = recoveryErrorText(result.err)
				if errors.Is(result.err, os.ErrExist) {
					state.stage = recoveryStageDestination
					state.destination.Focus()
				} else {
					state.stage = recoveryStagePassword
					state.password.Focus()
				}
			}
		} else {
			if result.material != nil {
				action := state.actions[state.selected]
				authorized := !wasCancelling && result.err == nil &&
					m.selectedAccount != nil && m.selectedAccount.AccountID == state.account.AccountID &&
					result.material.AccountID == state.account.AccountID &&
					result.material.Address == state.account.Address &&
					result.material.Kind == action.kind
				if !authorized {
					result.material.Destroy()
					state.stage = recoveryStageMenu
					if wasCancelling {
						state.status = localization.Get("recovery_status_reveal_cancelled")
					} else {
						state.status = localization.Get("recovery_status_reveal_discarded")
					}
				} else {
					state.material = result.material
					now := time.Now()
					m.displayTime = now
					state.expiresAt = now.Add(recoveryRevealDuration)
					state.stage = recoveryStageRevealed
					state.scrollOffset = 0
					state.status = ""
					operationID := result.operationID
					if state.quitAfterResult || state.exitAfterResult {
						break
					}
					return m, tea.Tick(recoveryRevealDuration, func(time.Time) tea.Msg {
						return recoveryExpiredMsg{operationID: operationID}
					})
				}
			}
			if result.err != nil {
				if result.material != nil {
					result.material.Destroy()
				}
				if wasCancelling {
					state.status = localization.Get("recovery_err_cancelled")
					state.stage = recoveryStageMenu
				} else {
					state.status = ""
					state.err = recoveryErrorText(result.err)
					state.stage = recoveryStagePassword
					state.password.Focus()
				}
			} else if result.material == nil {
				state.status = ""
				state.err = recoveryErrorText(wallet.ErrRecoveryUnavailable)
				state.stage = recoveryStageMenu
			}
		}
		if state.quitAfterResult {
			if notice != "" {
				m.lastOperationNotice = notice
			}
			m.clearRecovery()
			return m, tea.Sequence(tea.ClearScreen, tea.Quit)
		}
		if state.exitAfterResult {
			if notice != "" {
				m.lastOperationNotice = notice
			}
			m.clearRecovery()
			m.currentView = constants.WalletDetailsView
			m.refreshWalletDetailsComponents()
			return m, tea.ClearScreen
		}
		return m, nil
	case recoveryExpiredMsg:
		if result.operationID != state.operationID || state.stage != recoveryStageRevealed {
			return m, nil
		}
		m.recoveryPrivacyWipe(localization.Get("recovery_status_expired"))
		return m, tea.ClearScreen
	}
	m.displayTime = time.Now()
	if state.stage == recoveryStageRevealed && (state.expiresAt.IsZero() || !m.displayTime.Before(state.expiresAt)) {
		m.recoveryPrivacyWipe(localization.Get("recovery_status_expired"))
		return m, tea.ClearScreen
	}
	if state.busy {
		if key, ok := msg.(tea.KeyMsg); ok && key.String() == "esc" {
			if state.cancel != nil {
				state.cancel()
			}
			state.cancelling = true
			state.exitAfterResult = true
		}
		return m, nil
	}
	if key, ok := msg.(tea.KeyMsg); ok {
		if key.String() == "esc" {
			m.clearRecovery()
			m.currentView = constants.WalletDetailsView
			m.refreshWalletDetailsComponents()
			return m, tea.ClearScreen
		}
		layout := m.recoveryPageLayout()
		maxOffset := max(0, len(layout.lines)-layout.bodyHeight)
		switch key.String() {
		case "pgdown":
			state.scrollOffset = min(layout.offset+layout.bodyHeight, maxOffset)
			return m, nil
		case "pgup":
			state.scrollOffset = max(0, layout.offset-layout.bodyHeight)
			return m, nil
		}
		switch state.stage {
		case recoveryStageMenu:
			switch key.String() {
			case "up", "k":
				if state.selected > 0 {
					state.selected--
				}
				state.scrollOffset = 0
				return m, nil
			case "down", "j":
				if state.selected < len(state.actions)-1 {
					state.selected++
				}
				state.scrollOffset = 0
				return m, nil
			case "enter":
				return m.advanceRecovery()
			}
		case recoveryStageRevealed:
			switch key.String() {
			case "down":
				state.scrollOffset = min(layout.offset+1, maxOffset)
			case "up":
				state.scrollOffset = max(0, layout.offset-1)
			case "enter":
				return m, nil
			}
			return m, nil
		default:
			if key.String() == "enter" {
				return m.advanceRecovery()
			}
		}
	}
	var cmd tea.Cmd
	switch state.stage {
	case recoveryStageDestination:
		state.destination, cmd = state.destination.Update(msg)
	case recoveryStagePassword:
		state.password, cmd = state.password.Update(msg)
	case recoveryStageConfirmation:
		state.confirmation, cmd = state.confirmation.Update(msg)
	}
	return m, cmd
}

func (m *CLIModel) advanceRecovery() (tea.Model, tea.Cmd) {
	state := m.recovery
	state.scrollOffset = 0
	action := state.actions[state.selected]
	switch state.stage {
	case recoveryStageMenu:
		state.err = ""
		state.status = ""
		if action.export {
			state.stage = recoveryStageDestination
			state.destination.Focus()
			return m, nil
		}
		state.stage = recoveryStagePassword
		state.password.Focus()
		return m, nil
	case recoveryStageDestination:
		destination := canonicalExpandHome(strings.TrimSpace(state.destination.Value()))
		if !filepath.IsAbs(destination) {
			state.err = localization.Get("recovery_err_destination_abs")
			return m, nil
		}
		state.destination.SetValue(destination)
		state.err = ""
		state.stage = recoveryStagePassword
		state.destination.Blur()
		state.password.Focus()
		return m, nil
	case recoveryStagePassword:
		if state.password.Value() == "" {
			state.err = localization.Get("recovery_err_password_required")
			return m, nil
		}
		state.err = ""
		state.stage = recoveryStageConfirmation
		state.password.Blur()
		if action.export {
			state.confirmation.Placeholder = localization.Get("recovery_placeholder_export")
		} else {
			state.confirmation.Placeholder = localization.Get("recovery_placeholder_reveal")
		}
		state.confirmation.Focus()
		return m, nil
	case recoveryStageConfirmation:
		expected := "REVEAL"
		if action.export {
			expected = "EXPORT"
		}
		if state.confirmation.Value() != expected {
			state.err = localization.T("recovery_err_type_exact", map[string]interface{}{"Token": expected})
			return m, nil
		}
		return m, m.startRecovery(action)
	}
	return m, nil
}

func (m *CLIModel) startRecovery(action recoveryAction) tea.Cmd {
	state := m.recovery
	m.recoveryOperationID++
	state.operationID = m.recoveryOperationID
	operationID := state.operationID
	ctx, cancel := context.WithTimeout(context.Background(), canonicalImportTimeout)
	state.cancel = cancel
	state.busy = true
	state.cancelling = false
	state.exitAfterResult = false
	state.quitAfterResult = false
	state.err = ""
	vault := m.Vault
	accountID := state.account.AccountID
	password := []byte(state.password.Value())
	state.password.SetValue("")
	state.confirmation.SetValue("")
	destination := ""
	if action.export {
		destination = state.destination.Value()
	}
	kind := action.kind
	return func() tea.Msg {
		defer cancel()
		defer clear(password)
		request := wallet.RecoverySecretRequest{
			AccountID:        accountID,
			ConfirmAccountID: accountID,
			Password:         password,
			Kind:             kind,
		}
		if destination != "" {
			err := vault.ExportRecoverySecret(ctx, wallet.RecoveryExportRequest{
				RecoverySecretRequest: request,
				Destination:           destination,
			})
			return recoveryResultMsg{operationID: operationID, accountID: accountID, destination: destination, err: err, export: true}
		}
		material, err := vault.RevealRecoverySecret(ctx, request)
		if err != nil || ctx.Err() != nil {
			if material != nil {
				material.Destroy()
				material = nil
			}
			if err == nil {
				err = ctx.Err()
			}
		}
		return recoveryResultMsg{operationID: operationID, accountID: accountID, material: material, err: err}
	}
}

func recoveryErrorText(err error) string {
	switch {
	case errors.Is(err, wallet.ErrRecoveryAuthentication):
		return localization.Get("recovery_err_auth")
	case errors.Is(err, wallet.ErrRecoveryConfirmation):
		return localization.Get("recovery_err_confirm")
	case errors.Is(err, wallet.ErrRecoveryUnavailable):
		return localization.Get("recovery_err_unavailable")
	case errors.Is(err, wallet.ErrCapabilityDenied):
		return localization.Get("recovery_err_denied")
	case errors.Is(err, wallet.ErrCapabilityExpired):
		return localization.Get("recovery_err_state_changed")
	case errors.Is(err, wallet.ErrVaultClosed):
		return localization.Get("recovery_err_vault_closed")
	case errors.Is(err, wallet.ErrAccountNotFound):
		return localization.Get("recovery_err_account_gone")
	case errors.Is(err, os.ErrExist):
		return localization.Get("recovery_err_dest_exists")
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return localization.Get("recovery_err_cancelled")
	default:
		return localization.Get("recovery_err_failed")
	}
}

func recoveryBoolLabel(value bool) string {
	if value {
		return localization.Get("yes")
	}
	return localization.Get("no")
}

func recoveryWrap(value string, width int) []string {
	if width < 1 {
		width = 1
	}
	wrapped := ansi.Hardwrap(value, width, true)
	return strings.Split(wrapped, "\n")
}

func recoveryKindLabel(kind wallet.RecoverySecretKind) string {
	switch kind {
	case wallet.RecoveryMnemonic:
		return localization.Get("recovery_kind_mnemonic")
	case wallet.RecoveryPrivateKey:
		return localization.Get("recovery_kind_private_key")
	case wallet.RecoveryPassphrase:
		return localization.Get("recovery_kind_passphrase")
	default:
		return localization.Get("recovery_kind_secret")
	}
}

func (m *CLIModel) recoverySecretBody() []string {
	state := m.recovery
	if state == nil || state.material == nil {
		return nil
	}
	width := max(1, m.width)
	var body []string
	switch state.material.Kind {
	case wallet.RecoveryMnemonic:
		words := strings.Fields(string(state.material.Bytes()))
		cellWidth := max(14, (width-4)/3)
		for index := 0; index < len(words); index += 3 {
			var row strings.Builder
			for column := 0; column < 3 && index+column < len(words); column++ {
				cell := fmt.Sprintf("%d. %s", index+column+1, words[index+column])
				if column > 0 {
					row.WriteString("  ")
				}
				if lipgloss.Width(cell) > cellWidth {
					if row.Len() > 0 {
						body = append(body, row.String())
						row.Reset()
					}
					body = append(body, recoveryWrap(cell, width)...)
					continue
				}
				row.WriteString(lipgloss.NewStyle().Width(cellWidth).Render(cell))
			}
			if row.Len() > 0 {
				body = append(body, row.String())
			}
		}
		if state.material.HasBIP39Passphrase {
			body = append(body, "", localization.Get("recovery_passphrase_also_required"))
		}
	case wallet.RecoveryPrivateKey:
		body = append(body, string(state.material.Bytes()), "", localization.Get("recovery_key_controls_only"))
	case wallet.RecoveryPassphrase:
		body = append(body, recoveryWrap(localization.Get("recovery_passphrase_label")+" "+strconv.Quote(string(state.material.Bytes())), width)...)
		body = append(body, "", localization.Get("recovery_passphrase_store_exact"))
	}
	return body
}

type recoveryPageLayout struct {
	header     string
	lines      []string
	footer     string
	bodyHeight int
	offset     int
}

func (m *CLIModel) recoveryPageLayout() recoveryPageLayout {
	state := m.recovery
	layout := recoveryPageLayout{bodyHeight: 1}
	if state == nil {
		return layout
	}
	width := max(1, m.width)
	warning := lipgloss.NewStyle().Foreground(lipgloss.Color("#FF0000"))
	wrap := func(value string) []string {
		return recoveryWrap(value, width)
	}
	warn := func(value string) []string {
		lines := wrap(value)
		for index := range lines {
			lines[index] = warning.Render(lines[index])
		}
		return lines
	}
	var head strings.Builder
	head.WriteString(lipgloss.NewStyle().Bold(true).Render(localization.Get("recovery_title")) + "\n")
	for _, line := range wrap(localization.T("recovery_account_line", map[string]interface{}{"Name": safeInline(state.account.Name), "Address": safeInline(state.account.Address), "ID": safeInline(state.account.AccountID)})) {
		head.WriteString(line + "\n")
	}
	layout.header = head.String()

	action := recoveryAction{}
	if state.selected >= 0 && state.selected < len(state.actions) {
		action = state.actions[state.selected]
	}
	var lines []string
	var footer strings.Builder
	footer.WriteString("\n")
	switch state.stage {
	case recoveryStageMenu:
		for index, item := range state.actions {
			cursor := "  "
			if index == state.selected {
				cursor = "> "
			}
			lines = append(lines, wrap(cursor+localization.Get(item.labelKey))...)
		}
		if state.account.SecretType != wallet.SecretTypeMnemonic {
			lines = append(lines, "")
			lines = append(lines, wrap(localization.Get("recovery_no_phrase"))...)
		}
		if state.status != "" {
			lines = append(lines, "")
			lines = append(lines, wrap(safeInline(state.status))...)
		}
		lines = append(lines, "")
		lines = append(lines, wrap(localization.Get("recovery_requires_confirm"))...)
	case recoveryStageDestination:
		lines = append(lines, warn(localization.T("recovery_destination_warning", map[string]interface{}{"Hint": action.hint}))...)
		footer.WriteString(state.destination.View() + "\n")
	case recoveryStagePassword:
		lines = append(lines, wrap(localization.Get("recovery_placeholder_password")+":")...)
		footer.WriteString(state.password.View() + "\n")
	case recoveryStageConfirmation:
		lines = append(lines, wrap(localization.T("recovery_confirm_action", map[string]interface{}{"Action": localization.Get(action.labelKey)}))...)
		lines = append(lines, "")
		lines = append(lines, wrap(localization.T("recovery_kind_line", map[string]interface{}{"Kind": recoveryKindLabel(action.kind), "Path": safeInline(state.account.DerivationPath), "Lang": safeInline(state.account.BIP39Language), "Passphrase": recoveryBoolLabel(state.account.HasBIP39Passphrase)}))...)
		if action.export {
			lines = append(lines, wrap(localization.T("recovery_destination_label", map[string]interface{}{"Path": safeInline(state.destination.Value())}))...)
			lines = append(lines, warn(localization.Get("recovery_warn_file_unencrypted"))...)
		} else {
			lines = append(lines, warn(localization.Get("recovery_warn_screen_plaintext"))...)
		}
		if action.kind == wallet.RecoveryMnemonic && state.account.HasBIP39Passphrase {
			lines = append(lines, wrap(localization.Get("recovery_passphrase_account_note"))...)
		}
		if action.kind == wallet.RecoveryPrivateKey {
			lines = append(lines, wrap(localization.Get("recovery_key_controls_only_note"))...)
		}
		footer.WriteString(state.confirmation.View() + "\n")
	case recoveryStageRevealed:
		expired := m.displayTime.IsZero() || state.expiresAt.IsZero() || !m.displayTime.Before(state.expiresAt)
		if state.material != nil && !expired {
			lines = append(lines, warn(localization.Get("recovery_warn_unencrypted_secret"))...)
			lines = append(lines, wrap(localization.T("recovery_kind_line_reveal", map[string]interface{}{"Kind": recoveryKindLabel(state.material.Kind), "Path": safeInline(state.material.DerivationPath), "Lang": safeInline(string(state.material.BIP39Language))}))...)
			lines = append(lines, "")
			for _, line := range m.recoverySecretBody() {
				lines = append(lines, wrap(line)...)
			}
		} else {
			lines = append(lines, wrap(localization.Get("recovery_secret_hidden"))...)
		}
	}
	if state.err != "" {
		lines = append(warn(localization.Get("error_title")+": "+safeInline(state.err)), lines...)
	}
	if state.busy {
		if state.cancelling {
			lines = append(lines, wrap(localization.Get("recovery_cancelling"))...)
		} else {
			lines = append(lines, wrap(localization.Get("recovery_working"))...)
		}
	}
	footer.WriteString(m.renderStatusBar())
	layout.footer = footer.String()
	layout.lines = lines
	layout.bodyHeight = max(1, m.height-lipgloss.Height(layout.header)-lipgloss.Height(layout.footer))
	layout.offset = min(max(state.scrollOffset, 0), max(0, len(lines)-layout.bodyHeight))
	return layout
}

func (m *CLIModel) viewRecovery() string {
	if m.width < 80 || m.height < 24 {
		return m.renderTerminalSizeHint(80, 24)
	}
	if m.recovery == nil {
		return ""
	}
	layout := m.recoveryPageLayout()
	var view strings.Builder
	view.WriteString(layout.header)
	end := min(len(layout.lines), layout.offset+layout.bodyHeight)
	for _, line := range layout.lines[layout.offset:end] {
		view.WriteString(line + "\n")
	}
	for shown := end - layout.offset; shown < layout.bodyHeight; shown++ {
		view.WriteString("\n")
	}
	view.WriteString(layout.footer)
	return view.String()
}
