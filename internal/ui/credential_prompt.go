package ui

import (
	"context"
	"errors"
	"strings"
	"time"

	"blocowallet/internal/constants"
	"blocowallet/internal/keepass"
	"blocowallet/internal/wallet"
	"blocowallet/pkg/localization"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type credentialPromptState struct {
	input         textinput.Model
	ownerView     string
	owner         any
	generation    uint64
	busy          bool
	err           string
	pendingCancel context.CancelFunc
	onReady       func(*wallet.CredentialBackupOperation) tea.Cmd
	onCancel      func()
}

type credentialOpenedMsg struct {
	generation uint64
	ownerView  string
	operation  *wallet.CredentialBackupOperation
	cancel     context.CancelFunc
	err        error
}

func (credentialOpenedMsg) String() string   { return "credentialOpenedMsg" }
func (credentialOpenedMsg) GoString() string { return "credentialOpenedMsg" }
func (credentialOpenedMsg) MarshalJSON() ([]byte, error) {
	return nil, errors.New("credential operation messages are not serializable")
}

func (m *CLIModel) ConfigureCredentialBackups(service *wallet.CredentialBackupService, store *keepass.Store) {
	m.credentialService = service
	m.credentialStore = store
}

func (m *CLIModel) credentialBackupEnabled() bool {
	if m.credentialService == nil {
		return false
	}
	return m.credentialService.Policy().Enabled
}

func (m *CLIModel) requestCredentialOperation(onReady func(*wallet.CredentialBackupOperation) tea.Cmd) tea.Cmd {
	return m.requestCredentialOperationCancel(onReady, nil)
}

func (m *CLIModel) requestCredentialOperationCancel(onReady func(*wallet.CredentialBackupOperation) tea.Cmd, onCancel func()) tea.Cmd {
	if !m.credentialBackupEnabled() {
		if onReady == nil {
			return nil
		}
		return onReady(nil)
	}
	if m.credentialOperation != nil && m.credentialOperation.Context().Err() == nil {
		if onReady == nil {
			return nil
		}
		return onReady(m.credentialOperation)
	}
	m.clearCredentialOperation()
	if m.credentialPrompt != nil {
		m.clearCredentialPrompt()
	}
	input := textinput.New()
	input.Placeholder = localization.Get("keepass_master_placeholder")
	input.EchoMode = textinput.EchoPassword
	input.EchoCharacter = '•'
	input.CharLimit = constants.PasswordCharLimit
	input.Width = constants.PasswordWidth
	input.Focus()
	m.credentialGeneration++
	m.credentialPrompt = &credentialPromptState{
		input:      input,
		ownerView:  m.currentView,
		owner:      m.credentialOwner(),
		generation: m.credentialGeneration,
		onReady:    onReady,
		onCancel:   onCancel,
	}
	return nil
}

func (m *CLIModel) credentialOwner() any {
	switch m.currentView {
	case constants.PersonalSignView:
		return m.personalSign
	case constants.EIP712SignView:
		return m.eip712Sign
	case constants.NativeTransferView:
		return m.nativeTransfer
	case constants.ContractCallView:
		return m.contractCall
	case constants.RecoveryView:
		return m.recovery
	case constants.CanonicalImportView:
		return m.canonicalImport
	case constants.KeePassSettingsView:
		return m.keepassSettings
	case constants.KeePassAccountView:
		return m.keepassAccount
	case constants.SafeView:
		return m.safeView
	case constants.ListWalletsView:
		if m.accountDeletion != nil {
			return m.accountDeletion
		}
		return m.uiOperationID
	default:
		return m.uiOperationID
	}
}

func (m *CLIModel) clearCredentialPrompt() {
	prompt := m.credentialPrompt
	if prompt == nil {
		return
	}
	prompt.input.SetValue("")
	prompt.input.Blur()
	if prompt.onCancel != nil {
		prompt.onCancel()
		prompt.onCancel = nil
	}
	if prompt.pendingCancel != nil {
		prompt.pendingCancel()
		prompt.pendingCancel = nil
	}
	m.credentialGeneration++
	m.credentialPrompt = nil
}

func (m *CLIModel) clearCredentialOperation() {
	m.clearCredentialPrompt()
	if m.credentialOperation != nil {
		m.credentialOperation.Close()
		m.credentialOperation = nil
	}
	if m.credentialCancel != nil {
		m.credentialCancel()
		m.credentialCancel = nil
	}
}

func (m *CLIModel) handleCredentialOpened(result credentialOpenedMsg) (tea.Model, tea.Cmd) {
	prompt := m.credentialPrompt
	if prompt == nil || result.generation != prompt.generation || result.ownerView != m.currentView || prompt.owner != m.credentialOwner() {
		if result.operation != nil {
			result.operation.Close()
		}
		if result.cancel != nil {
			result.cancel()
		}
		if prompt != nil && result.generation == prompt.generation {
			m.clearCredentialPrompt()
		}
		return m, nil
	}
	prompt.busy = false
	prompt.pendingCancel = nil
	if result.err != nil {
		prompt.err = credentialMasterErrorText(result.err)
		prompt.input.SetValue("")
		prompt.input.Focus()
		return m, nil
	}
	onReady := prompt.onReady
	m.credentialPrompt = nil
	m.credentialOperation = result.operation
	m.credentialCancel = result.cancel
	if onReady == nil {
		return m, nil
	}
	return m, onReady(result.operation)
}

func (m *CLIModel) updateCredentialPrompt(msg tea.Msg) (tea.Model, tea.Cmd) {
	prompt := m.credentialPrompt
	if prompt == nil {
		return m, nil
	}
	if prompt.busy {
		if key, ok := msg.(tea.KeyMsg); ok && key.String() == "esc" {
			m.clearCredentialPrompt()
		}
		return m, nil
	}
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch key.String() {
	case "esc":
		m.clearCredentialPrompt()
		return m, nil
	case "enter":
		master := []byte(prompt.input.Value())
		if len(master) == 0 {
			prompt.err = localization.Get("keepass_master_required")
			return m, nil
		}
		prompt.input.SetValue("")
		prompt.busy = true
		prompt.err = ""
		ctx, cancel := context.WithCancel(context.Background())
		prompt.pendingCancel = cancel
		service := m.credentialService
		generation := prompt.generation
		ownerView := prompt.ownerView
		return m, func() tea.Msg {
			defer clear(master)
			op, err := service.Begin(ctx, master)
			if err != nil {
				cancel()
				return credentialOpenedMsg{generation: generation, ownerView: ownerView, err: err}
			}
			return credentialOpenedMsg{generation: generation, ownerView: ownerView, operation: op, cancel: cancel}
		}
	}
	var command tea.Cmd
	prompt.input, command = prompt.input.Update(msg)
	return m, command
}

func (m *CLIModel) viewCredentialPrompt() string {
	prompt := m.credentialPrompt
	if prompt == nil {
		return ""
	}
	title := lipgloss.NewStyle().Bold(true).Render(localization.Get("keepass_master_title"))
	var view strings.Builder
	view.WriteString(title + "\n\n")
	view.WriteString(localization.Get("keepass_master_body") + "\n\n")
	if prompt.busy {
		view.WriteString(localization.Get("canonical_processing") + "\n")
	} else {
		view.WriteString(prompt.input.View() + "\n")
	}
	if prompt.err != "" {
		view.WriteString("\n" + m.styles.ErrorStyle.Render(safeInline(prompt.err)))
	}
	return view.String()
}

func credentialMasterErrorText(err error) string {
	switch {
	case errors.Is(err, keepass.ErrAuthentication):
		return localization.Get("keepass_err_auth")
	case errors.Is(err, keepass.ErrBusy):
		return localization.Get("keepass_err_busy")
	case errors.Is(err, keepass.ErrNotFound):
		return localization.Get("keepass_err_missing")
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return localization.Get("keepass_err_cancelled")
	default:
		return localization.Get("keepass_err_failed")
	}
}

type credentialOpDoneMsg struct {
	op *wallet.CredentialBackupOperation
}

func (credentialOpDoneMsg) String() string   { return "credentialOpDoneMsg" }
func (credentialOpDoneMsg) GoString() string { return "credentialOpDoneMsg" }

func (m *CLIModel) credentialToggleEligible(account wallet.AccountSummary) bool {
	return m.credentialBackupEnabled() && account.SignerKind == wallet.SignerKindSoftware
}

func (m *CLIModel) credentialEligibleSigner(signerKind wallet.SignerKind) bool {
	return m.credentialBackupEnabled() && signerKind == wallet.SignerKindSoftware
}

func (m *CLIModel) discardCredentialOperation(op *wallet.CredentialBackupOperation) {
	if op == nil {
		return
	}
	if m.credentialOperation == op {
		m.clearCredentialOperation()
		return
	}
	op.Close()
}

func (m *CLIModel) activeCredentialOperation() *wallet.CredentialBackupOperation {
	op := m.credentialOperation
	if op != nil && op.Context().Err() != nil {
		m.clearCredentialOperation()
		return nil
	}
	return op
}

func (m *CLIModel) credentialWorkerContext(timeout time.Duration) (context.Context, context.CancelFunc, *wallet.CredentialBackupOperation) {
	op := m.activeCredentialOperation()
	parent := context.Background()
	if op != nil {
		parent = op.Context()
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	return ctx, cancel, op
}

func (m *CLIModel) submitWithCredential(useKeePass bool, submit func() tea.Cmd) tea.Cmd {
	return m.submitWithCredentialCancel(useKeePass, submit, nil)
}

func (m *CLIModel) submitWithCredentialCancel(useKeePass bool, submit func() tea.Cmd, onCancel func()) tea.Cmd {
	if useKeePass && m.credentialBackupEnabled() && (m.credentialOperation == nil || m.credentialOperation.Context().Err() != nil) {
		return m.requestCredentialOperationCancel(func(*wallet.CredentialBackupOperation) tea.Cmd {
			return submit()
		}, onCancel)
	}
	return submit()
}

func (m *CLIModel) credentialMethodLabel(eligible bool) string {
	if !eligible {
		return ""
	}
	if m.credentialUseKeePass {
		return localization.Get("keepass_method_keepass")
	}
	return localization.Get("keepass_method_manual")
}


func runWithAccountCredential(ctx context.Context, op *wallet.CredentialBackupOperation, accountID string, manual []byte, useKeePass bool, fn func([]byte) error) error {
	if useKeePass {
		if op == nil {
			return wallet.ErrCredentialBackupRequired
		}
		return op.WithAccountPassword(ctx, accountID, fn)
	}
	return fn(manual)
}
