package ui

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"blocowallet/internal/constants"
	"blocowallet/internal/wallet"
	"blocowallet/pkg/localization"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type accountDeletionState struct {
	account      wallet.AccountSummary
	confirmation textinput.Model
	password     textinput.Model
	stage        int
	busy         bool
	cancelling   bool
	operationID  uint64
	cancel       context.CancelFunc
	err          string
}

type accountDeletedMsg struct {
	operationID uint64
	accountID   string
	err         error
}

func (m *CLIModel) initAccountDeletion(summary wallet.AccountSummary) {
	confirmation := textinput.New()
	confirmation.Placeholder = localization.Get("delete_confirm_placeholder")
	confirmation.CharLimit = 128
	confirmation.Width = 80
	confirmation.Focus()
	password := textinput.New()
	password.Placeholder = localization.Get("vault_storage_password_placeholder")
	password.CharLimit = constants.PasswordCharLimit
	password.Width = 80
	password.EchoMode = textinput.EchoPassword
	password.EchoCharacter = '•'
	m.accountDeletion = &accountDeletionState{account: summary, confirmation: confirmation, password: password}
	m.lastOperationNotice = ""
}

func (m *CLIModel) clearAccountDeletion() {
	if m.accountDeletion == nil {
		return
	}
	if m.accountDeletion.cancel != nil {
		m.accountDeletion.cancel()
	}
	m.accountDeletion.confirmation.SetValue("")
	m.accountDeletion.password.SetValue("")
	m.accountDeletion = nil
}

func (m *CLIModel) updateAccountDeletion(msg tea.Msg) (tea.Model, tea.Cmd) {
	state := m.accountDeletion
	if state == nil {
		return m, nil
	}
	if result, ok := msg.(accountDeletedMsg); ok {
		if !state.busy || result.operationID != state.operationID || result.accountID != state.account.AccountID {
			return m, nil
		}
		state.busy = false
		state.cancelling = false
		if state.cancel != nil {
			state.cancel()
		}
		state.cancel = nil
		if result.err != nil {
			state.password.SetValue("")
			state.err = accountDeletionErrorText(result.err)
			return m, nil
		}
		deletedID := result.accountID
		m.clearAccountDeletion()
		if m.selectedAccount != nil && m.selectedAccount.AccountID == deletedID {
			m.selectedAccount = nil
		}
		m.clearBalanceState()
		m.lastOperationNotice = localization.Get("delete_account_deleted")
		m.currentView = constants.ListWalletsView
		return m, m.refreshWalletsTable()
	}
	if state.busy {
		if key, ok := msg.(tea.KeyMsg); ok && key.String() == "esc" {
			if state.cancel != nil {
				state.cancel()
			}
			state.cancelling = true
		}
		return m, nil
	}
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "esc":
			m.clearAccountDeletion()
			return m, nil
		case "enter":
			if state.stage == 0 {
				if state.confirmation.Value() != state.account.AccountID {
					state.err = localization.Get("delete_err_mismatch")
					return m, nil
				}
				state.err = ""
				if state.account.SignerKind == wallet.SignerKindSoftware {
					state.stage = 1
					state.confirmation.Blur()
					state.password.Focus()
					return m, nil
				}
			}
			return m, m.startAccountDeletion()
		}
	}
	var cmd tea.Cmd
	if state.stage == 0 {
		state.confirmation, cmd = state.confirmation.Update(msg)
	} else {
		state.password, cmd = state.password.Update(msg)
	}
	return m, cmd
}

func accountDeletionErrorText(err error) string {
	switch {
	case errors.Is(err, wallet.ErrAccountDeleteConfirmation):
		return localization.Get("delete_err_confirmation")
	case errors.Is(err, wallet.ErrAccountDeletionPending):
		return localization.Get("delete_err_pending")
	case errors.Is(err, wallet.ErrAccountDeletionUnsupported):
		return localization.Get("delete_err_unsupported")
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return localization.Get("delete_err_cancelled")
	default:
		return localization.Get("delete_err_failed")
	}
}

func (m *CLIModel) startAccountDeletion() tea.Cmd {
	state := m.accountDeletion
	m.accountDeletionID++
	state.operationID = m.accountDeletionID
	operationID := state.operationID
	ctx, cancel := context.WithTimeout(context.Background(), canonicalImportTimeout)
	state.cancel = cancel
	state.busy = true
	state.cancelling = false
	state.err = ""
	vault := m.Vault
	accountID := state.account.AccountID
	typedID := state.confirmation.Value()
	var password []byte
	if state.account.SignerKind == wallet.SignerKindSoftware {
		password = []byte(state.password.Value())
		state.password.SetValue("")
	}
	return func() tea.Msg {
		defer cancel()
		defer clear(password)
		err := vault.DeleteAccount(ctx, wallet.DeleteAccountRequest{
			AccountID:        accountID,
			ConfirmAccountID: typedID,
			Password:         password,
		})
		return accountDeletedMsg{operationID: operationID, accountID: accountID, err: err}
	}
}

func (m *CLIModel) viewAccountDeletion() string {
	state := m.accountDeletion
	if state == nil {
		return ""
	}
	title := lipgloss.NewStyle().Bold(true).Render(localization.Get("delete_title"))
	var view strings.Builder
	view.WriteString(title + "\n\n")
	_, _ = view.WriteString(localization.T("delete_account_lines", map[string]interface{}{"Name": safeShort(state.account.Name), "Address": safeShort(state.account.Address), "ID": safeInline(state.account.AccountID)}) + "\n\n")
	view.WriteString(localization.Get("delete_body") + "\n\n")
	if state.busy {
		if state.cancelling {
			view.WriteString(localization.Get("recovery_cancelling"))
		} else {
			view.WriteString(localization.Get("delete_working"))
		}
		return view.String()
	}
	if state.stage == 0 {
		_, _ = fmt.Fprintf(&view, "%s\n%s\n\n", localization.Get("delete_confirm_id_label"), state.confirmation.View())
	} else {
		_, _ = fmt.Fprintf(&view, "%s\n%s\n\n", localization.Get("vault_storage_password_label"), state.password.View())
	}
	if state.err != "" {
		view.WriteString("\n\n" + m.styles.ErrorStyle.Render(safeInline(state.err)))
	}
	return view.String()
}
