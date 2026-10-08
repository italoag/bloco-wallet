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
	account         wallet.AccountSummary
	confirmation    textinput.Model
	password        textinput.Model
	backupConfirm   textinput.Model
	removeBackup    bool
	stage           int
	busy            bool
	cancelling      bool
	quitAfterResult bool
	operationID     uint64
	cancel          context.CancelFunc
	err             string
}

type accountDeletedMsg struct {
	operationID uint64
	accountID   string
	err         error
	op          *wallet.CredentialBackupOperation
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
	backupConfirm := textinput.New()
	backupConfirm.Placeholder = localization.Get("delete_confirm_placeholder")
	backupConfirm.CharLimit = 128
	backupConfirm.Width = 80
	m.accountDeletion = &accountDeletionState{account: summary, confirmation: confirmation, password: password, backupConfirm: backupConfirm}
	m.credentialUseKeePass = false
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
	m.accountDeletion.backupConfirm.SetValue("")
	m.credentialUseKeePass = false
	m.accountDeletion = nil
}

func (m *CLIModel) updateAccountDeletion(msg tea.Msg) (tea.Model, tea.Cmd) {
	state := m.accountDeletion
	if state == nil {
		return m, nil
	}
	if result, ok := msg.(accountDeletedMsg); ok {
		if !state.busy || result.operationID != state.operationID || result.accountID != state.account.AccountID {
			m.discardCredentialOperation(result.op)
			return m, nil
		}
		state.busy = false
		state.cancelling = false
		if state.cancel != nil {
			state.cancel()
		}
		state.cancel = nil
		m.discardCredentialOperation(result.op)
		quit := state.quitAfterResult
		notice := localization.Get("delete_account_deleted")
		if result.err != nil {
			var pendingErr *wallet.CredentialBackupPendingError
			if !errors.As(result.err, &pendingErr) {
				state.password.SetValue("")
				state.err = accountDeletionErrorText(result.err)
				return m, nil
			}
			notice = notice + " — " + localization.Get("keepass_backup_pending_notice")
		}
		deletedID := result.accountID
		m.clearAccountDeletion()
		if m.selectedAccount != nil && m.selectedAccount.AccountID == deletedID {
			m.selectedAccount = nil
		}
		m.clearBalanceState()
		m.lastOperationNotice = notice
		m.currentView = constants.ListWalletsView
		cmd := m.refreshWalletsTable()
		if quit {
			return m, tea.Sequence(cmd, tea.Quit)
		}
		return m, cmd
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
		case "ctrl+b":
			if state.stage == 0 && m.credentialBackupEnabled() && state.account.SignerKind == wallet.SignerKindSoftware {
				state.removeBackup = !state.removeBackup
			}
			return m, nil
		case "ctrl+k":
			if state.stage == 1 && m.credentialToggleEligible(state.account) {
				m.credentialUseKeePass = !m.credentialUseKeePass
				if m.credentialUseKeePass {
					state.password.SetValue("")
				}
			}
			return m, nil
		case "enter":
			switch state.stage {
			case 0:
				if state.confirmation.Value() != state.account.AccountID {
					state.err = localization.Get("delete_err_mismatch")
					return m, nil
				}
				state.err = ""
				if state.removeBackup {
					state.stage = 2
					state.confirmation.Blur()
					state.backupConfirm.Focus()
					return m, nil
				}
				if state.account.SignerKind == wallet.SignerKindSoftware {
					state.stage = 1
					state.confirmation.Blur()
					state.password.Focus()
					return m, nil
				}
			case 2:
				if state.backupConfirm.Value() != state.account.AccountID {
					state.err = localization.Get("delete_err_mismatch")
					return m, nil
				}
				state.err = ""
				state.stage = 1
				state.backupConfirm.Blur()
				state.password.Focus()
				return m, nil
			}
			useKeePass := m.credentialUseKeePass && m.credentialToggleEligible(state.account)
			return m, m.submitWithCredential(useKeePass || state.removeBackup, func() tea.Cmd { return m.startAccountDeletion() })
		}
	}
	var cmd tea.Cmd
	switch state.stage {
	case 0:
		state.confirmation, cmd = state.confirmation.Update(msg)
	case 2:
		state.backupConfirm, cmd = state.backupConfirm.Update(msg)
	default:
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
	useKeePass := m.credentialUseKeePass && m.credentialToggleEligible(state.account)
	parent := context.Background()
	var op *wallet.CredentialBackupOperation
	if (useKeePass || state.removeBackup) && m.credentialOperation != nil && m.credentialOperation.Context().Err() == nil {
		op = m.credentialOperation
		parent = op.Context()
	}
	ctx, cancel := context.WithTimeout(parent, canonicalImportTimeout)
	state.cancel = cancel
	state.busy = true
	state.cancelling = false
	state.quitAfterResult = false
	state.err = ""
	vault := m.Vault
	accountID := state.account.AccountID
	typedID := state.confirmation.Value()
	removeBackup := state.removeBackup
	backupConfirmID := state.backupConfirm.Value()
	var password []byte
	if state.account.SignerKind == wallet.SignerKindSoftware {
		password = []byte(state.password.Value())
		state.password.SetValue("")
	}
	state.backupConfirm.SetValue("")
	return func() tea.Msg {
		defer cancel()
		defer clear(password)
		var err error
		if useKeePass {
			err = runWithAccountCredential(ctx, op, accountID, nil, true, func(resolved []byte) error {
				return vault.DeleteAccount(ctx, wallet.DeleteAccountRequest{
					AccountID:              accountID,
					ConfirmAccountID:       typedID,
					Password:               resolved,
					RemoveCredentialBackup: removeBackup,
					ConfirmBackupAccountID: backupConfirmID,
				})
			})
		} else {
			err = vault.DeleteAccount(ctx, wallet.DeleteAccountRequest{
				AccountID:              accountID,
				ConfirmAccountID:       typedID,
				Password:               password,
				RemoveCredentialBackup: removeBackup,
				ConfirmBackupAccountID: backupConfirmID,
			})
		}
		if err == nil && removeBackup && op != nil {
			if _, syncErr := op.Sync(ctx); syncErr != nil {
				err = &wallet.CredentialBackupPendingError{Cause: syncErr}
			}
		}
		return accountDeletedMsg{operationID: operationID, accountID: accountID, err: err, op: op}
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
	switch state.stage {
	case 0:
		_, _ = fmt.Fprintf(&view, "%s\n%s\n", localization.Get("delete_confirm_id_label"), state.confirmation.View())
		if m.credentialBackupEnabled() && state.account.SignerKind == wallet.SignerKindSoftware {
			label := localization.Get("keepass_status_disabled")
			if state.removeBackup {
				label = localization.Get("keepass_status_enabled")
			}
			_, _ = fmt.Fprintf(&view, "%s\n", localization.T("keepass_remove_toggle", map[string]interface{}{"Status": label}))
		}
		view.WriteString("\n")
	case 2:
		_, _ = fmt.Fprintf(&view, "%s\n%s\n\n", localization.Get("keepass_retry_confirm_label"), state.backupConfirm.View())
	default:
		_, _ = fmt.Fprintf(&view, "%s\n%s\n%s\n\n", localization.Get("vault_storage_password_label"), state.password.View(), m.credentialMethodLabel(m.credentialToggleEligible(state.account)))
	}
	if state.err != "" {
		view.WriteString("\n\n" + m.styles.ErrorStyle.Render(safeInline(state.err)))
	}
	return view.String()
}
