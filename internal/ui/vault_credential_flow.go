package ui

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"blocowallet/internal/constants"
	"blocowallet/internal/wallet"
	"blocowallet/pkg/localization"

	tea "github.com/charmbracelet/bubbletea"
)

type vaultCreateResultMsg struct {
	operationID uint64
	resume      bool
	summary     wallet.AccountSummary
	challenge   wallet.BackupChallenge
	err         error
	pending     bool
	op          *wallet.CredentialBackupOperation
}

func (vaultCreateResultMsg) String() string   { return "vaultCreateResultMsg" }
func (vaultCreateResultMsg) GoString() string { return "vaultCreateResultMsg" }
func (vaultCreateResultMsg) MarshalJSON() ([]byte, error) {
	return nil, errors.New("vault create messages are not serializable")
}

type backupConfirmResultMsg struct {
	operationID uint64
	summary     wallet.AccountSummary
	err         error
	pending     bool
	op          *wallet.CredentialBackupOperation
}

func (backupConfirmResultMsg) String() string   { return "backupConfirmResultMsg" }
func (backupConfirmResultMsg) GoString() string { return "backupConfirmResultMsg" }
func (backupConfirmResultMsg) MarshalJSON() ([]byte, error) {
	return nil, errors.New("backup confirm messages are not serializable")
}

type vaultActionResultMsg struct {
	operationID     uint64
	export          bool
	exportCommitted bool
	exportPath      string
	exportWarning   error
	err             error
	pending         bool
	op              *wallet.CredentialBackupOperation
}

func (vaultActionResultMsg) String() string   { return "vaultActionResultMsg" }
func (vaultActionResultMsg) GoString() string { return "vaultActionResultMsg" }
func (vaultActionResultMsg) MarshalJSON() ([]byte, error) {
	return nil, errors.New("vault action messages are not serializable")
}

func (m *CLIModel) beginVaultBusy(owner string) (uint64, context.Context) {
	m.uiOperationID++
	operationID := m.uiOperationID
	ctx, cancel, _ := m.credentialWorkerContext(canonicalImportTimeout)
	m.vaultCancel = cancel
	m.vaultBusy = true
	m.vaultBusyCancelling = false
	m.vaultBusyOwner = owner
	return operationID, ctx
}

func (m *CLIModel) cancelVaultWorker() {
	if m.vaultCancel != nil {
		m.vaultCancel()
	}
	m.vaultBusyCancelling = true
}

func (m *CLIModel) finishVaultBusy() {
	m.vaultBusy = false
	m.vaultBusyCancelling = false
	m.vaultBusyOwner = ""
	if m.vaultCancel != nil {
		m.vaultCancel()
		m.vaultCancel = nil
	}
}

func (m *CLIModel) startVaultCreate() tea.Cmd {
	password := []byte(m.passwordInput.Value())
	name := strings.TrimSpace(m.nameInput.Value())
	wordCount, _ := strconv.Atoi(m.createWordCountInput.Value())
	language := wallet.BIP39Language(strings.ToLower(strings.ReplaceAll(m.createLanguageInput.Value(), "-", "_")))
	passphrase := m.createPassphraseInput.Value()
	path := m.createDerivationPathInput.Value()
	resumeID := m.resumeBackupAccountID
	m.passwordInput.SetValue("")
	m.createPassphraseInput.SetValue("")
	m.createPasswordConfirmationInput.SetValue("")
	m.createPasswordStage = 0
	m.createPasswordError = ""
	operationID, ctx := m.beginVaultBusy("create")
	vault := m.Vault
	op := m.activeCredentialOperation()
	return func() tea.Msg {
		defer clear(password)
		var summary wallet.AccountSummary
		var challenge wallet.BackupChallenge
		var err error
		if resumeID != "" {
			summary, challenge, err = vault.ResumeBackup(ctx, resumeID, password)
		} else {
			summary, challenge, err = vault.Create(ctx, wallet.CreateAccountRequest{
				Name:            name,
				Password:        password,
				WordCount:       wordCount,
				BIP39Language:   language,
				BIP39Passphrase: passphrase,
				DerivationPath:  path,
			})
		}
		var pendingErr *wallet.CredentialBackupPendingError
		pending := errors.As(err, &pendingErr)
		if pending {
			err = nil
		}
		return vaultCreateResultMsg{operationID: operationID, resume: resumeID != "", summary: summary, challenge: challenge, err: err, pending: pending, op: op}
	}
}

func (m *CLIModel) startVaultConfirmBackup() tea.Cmd {
	challenge := m.backupChallenge
	if challenge == nil {
		return nil
	}
	provided := strings.Fields(m.backupConfirmationInput.Value())
	if len(provided) != len(challenge.RequiredWordIndices) {
		m.backupError = localization.Get("mnemonic_mismatch")
		m.backupConfirmationInput.SetValue("")
		return nil
	}
	answers := make(map[int]string, len(provided))
	for position, index := range challenge.RequiredWordIndices {
		answers[index] = provided[position]
	}
	challengeID := challenge.ChallengeID
	m.backupConfirmationInput.SetValue("")
	operationID, ctx := m.beginVaultBusy("confirm")
	vault := m.Vault
	op := m.activeCredentialOperation()
	return func() tea.Msg {
		active, err := vault.ConfirmBackup(ctx, challengeID, answers)
		var pendingErr *wallet.CredentialBackupPendingError
		pending := errors.As(err, &pendingErr)
		if pending {
			err = nil
		}
		if err == nil && op != nil {
			if _, syncErr := op.Sync(ctx); syncErr != nil {
				pending = true
			}
		}
		return backupConfirmResultMsg{operationID: operationID, summary: active, err: err, pending: pending, op: op}
	}
}

func (m *CLIModel) startVaultAction(export bool) tea.Cmd {
	if m.Vault == nil || m.selectedAccount == nil {
		return nil
	}
	currentPassword := []byte(m.currentPasswordInput.Value())
	newPassword := []byte(m.newPasswordInput.Value())
	confirmPassword := []byte(m.confirmPasswordInput.Value())
	destination := m.exportDestinationInput.Value()
	encrypted := m.vaultExportEncrypted
	accountID := m.selectedAccount.AccountID
	useKeePass := m.credentialUseKeePass && m.credentialToggleEligible(*m.selectedAccount)
	m.clearVaultActionInputs()
	operationID, ctx := m.beginVaultBusy("vault")
	vault := m.Vault
	op := m.activeCredentialOperation()
	return func() tea.Msg {
		defer clear(currentPassword)
		defer clear(newPassword)
		defer clear(confirmPassword)
		var exportCommitted bool
		var exportPath string
		var exportWarning error
		core := func(resolved []byte) error {
			if export {
				handle, unlockErr := vault.Unlock(ctx, accountID, resolved)
				if unlockErr != nil {
					return unlockErr
				}
				var exportErr error
				if encrypted {
					exportErr = vault.ExportEncryptedAccount(ctx, wallet.EncryptedAccountExportRequest{
						Handle:             handle,
						Destination:        destination,
						CurrentPassword:    resolved,
						NewPassword:        newPassword,
						ConfirmNewPassword: confirmPassword,
					})
				} else {
					exportErr = vault.ExportKeystoreV3(ctx, wallet.KeystoreV3ExportRequest{
						Handle:          handle,
						Destination:     destination,
						Password:        newPassword,
						ConfirmPassword: confirmPassword,
					})
				}
				if exportErr == nil || wallet.IsExportCommitted(exportErr) {
					exportCommitted = true
					exportPath = destination
					exportWarning = exportErr
					exportErr = nil
				}
				if lockErr := vault.Lock(handle); lockErr != nil {
					if exportCommitted {
						exportWarning = errors.Join(exportWarning, lockErr)
					} else {
						exportErr = errors.Join(exportErr, lockErr)
					}
				}
				return exportErr
			}
			return vault.RotatePassword(ctx, accountID, resolved, newPassword)
		}
		err := runWithAccountCredential(ctx, op, accountID, currentPassword, useKeePass, core)
		pending := false
		if err != nil {
			var pendingErr *wallet.CredentialBackupPendingError
			if errors.As(err, &pendingErr) {
				pending = true
				err = nil
			}
		}
		if exportCommitted && err != nil {
			exportWarning = errors.Join(exportWarning, err)
			err = nil
		}
		if err == nil && op != nil {
			if _, syncErr := op.Sync(ctx); syncErr != nil {
				pending = true
			}
		}
		return vaultActionResultMsg{operationID: operationID, export: export, exportCommitted: exportCommitted, exportPath: exportPath, exportWarning: exportWarning, err: err, pending: pending, op: op}
	}
}

func (m *CLIModel) handleVaultCreateResult(msg vaultCreateResultMsg) (tea.Model, tea.Cmd) {
	if msg.operationID != m.uiOperationID || m.vaultBusyOwner != "create" && m.vaultBusyOwner != "" {
		m.discardCredentialOperation(msg.op)
		if msg.err == nil && (msg.summary.AccountID != "" || msg.challenge.ChallengeID != "") {
			m.suspendCreateResult(msg.challenge, msg.summary)
			return m, m.refreshWalletsTable()
		}
		return m, nil
	}
	wasCancelling := m.vaultBusyCancelling
	m.finishVaultBusy()
	if msg.err != nil {
		m.discardCredentialOperation(msg.op)
		m.createPasswordError = safeError(msg.err)
		m.passwordInput.SetValue("")
		m.createPasswordConfirmationInput.SetValue("")
		m.passwordInput.Focus()
		if m.vaultQuitAfterResult {
			m.vaultQuitAfterResult = false
			return m, tea.Quit
		}
		return m, nil
	}
	if msg.pending {
		m.backupError = localization.Get("keepass_backup_pending_notice")
	}
	m.pendingAccount = &msg.summary
	challenge := msg.challenge
	m.backupChallenge = &challenge
	m.initBackupMaterialInputs()
	m.backupConfirmationInput.SetValue("")
	m.backupConfirmationInput.Focus()
	m.currentView = constants.CreateWalletBackupView
	if wasCancelling || m.vaultQuitAfterResult {
		quitAfter := m.vaultQuitAfterResult
		m.vaultQuitAfterResult = false
		m.suspendPendingVaultBackup()
		if quitAfter {
			return m, tea.Quit
		}
		return m, nil
	}
	return m, nil
}

func (m *CLIModel) suspendCreateResult(challenge wallet.BackupChallenge, summary wallet.AccountSummary) {
	if m.Vault != nil && challenge.ChallengeID != "" {
		_ = m.Vault.SuspendBackup(challenge.ChallengeID)
	}
	for index := range challenge.Words {
		challenge.Words[index] = ""
	}
	if summary.AccountID != "" {
		m.resumeBackupAccountID = summary.AccountID
	}
	m.lastOperationNotice = localization.Get("backup_suspended_resume")
}

func (m *CLIModel) handleBackupConfirmResult(msg backupConfirmResultMsg) (tea.Model, tea.Cmd) {
	if msg.operationID != m.uiOperationID || (m.vaultBusyOwner != "confirm" && m.vaultBusyOwner != "") {
		m.discardCredentialOperation(msg.op)
		if msg.err == nil && msg.summary.AccountID != "" {
			m.lastOperationNotice = localization.Get("keepass_confirmed_notice")
			if msg.pending {
				m.lastOperationNotice = m.lastOperationNotice + " — " + localization.Get("keepass_backup_pending_notice")
			}
			return m, m.refreshWalletsTable()
		}
		return m, nil
	}
	m.finishVaultBusy()
	if msg.err != nil {
		m.backupError = safeError(msg.err)
		m.backupPassphraseInput.SetValue("")
		if m.vaultQuitAfterResult {
			m.vaultQuitAfterResult = false
			return m, tea.Quit
		}
		return m, nil
	}
	m.discardCredentialOperation(msg.op)
	if msg.pending {
		m.lastOperationNotice = localization.Get("keepass_backup_pending_notice")
	}
	if m.backupChallenge != nil {
		for index := range m.backupChallenge.Words {
			m.backupChallenge.Words[index] = ""
		}
	}
	m.backupPassphraseInput.SetValue("")
	m.backupPathInput.SetValue("")
	m.backupLanguageInput.SetValue("")
	m.backupWordAnswers = nil
	m.backupMaterialStage = 0
	m.backupChallenge = nil
	m.pendingAccount = nil
	m.resumeBackupAccountID = ""
	m.selectedAccount = &msg.summary
	m.initWalletDetailsComponents()
	m.backupError = ""
	m.backupConfirmationInput.SetValue("")
	m.nameInput.SetValue("")
	m.currentView = constants.WalletDetailsView
	if m.vaultQuitAfterResult {
		m.vaultQuitAfterResult = false
		return m, tea.Quit
	}
	return m, m.refreshWalletsTable()
}

func vaultActionNotice(msg vaultActionResultMsg) string {
	var notice string
	switch {
	case msg.exportCommitted && (msg.exportWarning != nil || msg.err != nil):
		notice = localization.T("recovery_status_export_warning", map[string]interface{}{"Path": safeInline(msg.exportPath)})
	case msg.exportCommitted:
		notice = localization.T("vault_export_committed", map[string]interface{}{"Path": safeInline(msg.exportPath)})
	case msg.export:
		notice = localization.Get("vault_export_done")
	default:
		notice = localization.Get("vault_password_rotated")
	}
	if msg.pending {
		notice += " — " + localization.Get("keepass_backup_pending_notice")
	}
	return notice
}

func (m *CLIModel) handleVaultActionResult(msg vaultActionResultMsg) (tea.Model, tea.Cmd) {
	if msg.operationID != m.uiOperationID || (m.vaultBusyOwner != "vault" && m.vaultBusyOwner != "") {
		m.discardCredentialOperation(msg.op)
		if msg.err == nil || msg.exportCommitted {
			m.lastOperationNotice = vaultActionNotice(msg)
			return m, m.refreshWalletsTable()
		}
		return m, nil
	}
	m.finishVaultBusy()
	m.discardCredentialOperation(msg.op)
	if msg.err != nil && !msg.exportCommitted {
		m.vaultActionError = safeError(msg.err)
		m.currentPasswordInput.SetValue("")
		m.newPasswordInput.SetValue("")
		m.confirmPasswordInput.SetValue("")
		m.vaultActionStage = 0
		m.vaultActionPreview = false
		m.currentPasswordInput.Focus()
		if m.vaultQuitAfterResult {
			m.vaultQuitAfterResult = false
			return m, tea.Quit
		}
		return m, nil
	}
	m.lastOperationNotice = vaultActionNotice(msg)
	m.credentialUseKeePass = false
	m.currentView = constants.WalletDetailsView
	m.refreshWalletDetailsComponents()
	if m.vaultQuitAfterResult {
		m.vaultQuitAfterResult = false
		return m, tea.Quit
	}
	return m, m.refreshWalletsTable()
}

func (m *CLIModel) suspendPendingVaultBackup() {
	if m.Vault == nil || m.backupChallenge == nil {
		return
	}
	if err := m.Vault.SuspendBackup(m.backupChallenge.ChallengeID); err != nil {
		m.backupError = err.Error()
		return
	}
	if m.pendingAccount != nil {
		m.resumeBackupAccountID = m.pendingAccount.AccountID
	}
	for index := range m.backupChallenge.Words {
		m.backupChallenge.Words[index] = ""
	}
	m.backupChallenge = nil
	m.pendingAccount = nil
	m.backupPassphraseInput.SetValue("")
	m.backupPathInput.SetValue("")
	m.backupLanguageInput.SetValue("")
	m.backupWordAnswers = nil
	m.backupMaterialStage = 0
	m.backupConfirmationInput.SetValue("")
	m.lastOperationNotice = localization.Get("backup_suspended_resume")
	m.currentView = constants.ListWalletsView
}
