package ui

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"blocowallet/internal/constants"
	"blocowallet/internal/keepass"
	"blocowallet/internal/wallet"
	"blocowallet/pkg/config"
	"blocowallet/pkg/localization"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

const (
	keepassStageMenu = iota
	keepassStagePath
	keepassStageMaster
	keepassStageConfirmMaster
	keepassStageConsent
	keepassStageDisableConfirm
	keepassStagePendingList
	keepassStageRetryPath
	keepassStageRetryPassword
	keepassStageRetryConfirm
)

const keepassPendingWindow = 8

type keepassSettingsState struct {
	stage             int
	mode              string
	menuIndex         int
	pathInput         textinput.Model
	masterInput       textinput.Model
	confirmInput      textinput.Model
	consentInput      textinput.Model
	retryPathInput    textinput.Model
	busy              bool
	cancelling        bool
	quitAfterResult   bool
	cancel            context.CancelFunc
	err               string
	errKey            string
	errParams         map[string]interface{}
	notice            string
	noticeKey         string
	noticeParams      map[string]interface{}
	pending           []wallet.CredentialBackupState
	pendingIndex      int
	pendingScroll     int
	retryRow          *wallet.CredentialBackupState
	retryConfirmID    string
	retryUseKeePass   bool
	retryPath         string
	generation        uint64
	suggestKey        string
	suggestValue      string
	suggestGeneration uint64
	contentHeight     int
}

func (state *keepassSettingsState) setErrKey(key string, params map[string]interface{}) {
	state.err = ""
	state.errKey = key
	state.errParams = params
}

func (state *keepassSettingsState) setErr(err error) {
	state.errKey = ""
	state.errParams = nil
	state.err = safeError(err)
}

func (state *keepassSettingsState) errText() string {
	if state.errKey != "" {
		return localization.T(state.errKey, state.errParams)
	}
	return state.err
}

func (state *keepassSettingsState) noticeText() string {
	if state.noticeKey != "" {
		return localization.T(state.noticeKey, state.noticeParams)
	}
	return state.notice
}

type keepassActionMsg struct {
	generation      uint64
	err             error
	noticeKey       string
	noticeParams    map[string]interface{}
	fileCommitted   bool
	configCommitted bool
	closeOp         bool
	path            string
}

type keepassPendingMsg struct {
	generation uint64
	rows       []wallet.CredentialBackupState
	err        error
}

type keepassStatusMsg struct {
	accountID  string
	rows       []wallet.CredentialBackupState
	report     *wallet.CredentialBackupReport
	err        error
	generation uint64
}

type keepassPathSuggestionsMsg struct {
	generation  uint64
	value       string
	suggestions []string
}

func newCredentialInput(placeholder string, echo bool) textinput.Model {
	input := textinput.New()
	input.Placeholder = placeholder
	if echo {
		input.EchoMode = textinput.EchoPassword
		input.EchoCharacter = '•'
	}
	input.CharLimit = 1024
	input.Width = constants.PasswordWidth
	return input
}

func (m *CLIModel) initKeePassSettings() {
	state := &keepassSettingsState{
		stage: keepassStageMenu,
	}
	state.pathInput = newCredentialInput(localization.Get("keepass_path_placeholder"), false)
	state.pathInput.ShowSuggestions = true
	state.masterInput = newCredentialInput(localization.Get("keepass_master_placeholder"), true)
	state.confirmInput = newCredentialInput(localization.Get("keepass_confirm_placeholder"), true)
	state.consentInput = newCredentialInput("", false)
	state.retryPathInput = newCredentialInput(localization.Get("keepass_pending_path_label"), false)
	state.retryPathInput.ShowSuggestions = true
	m.uiOperationID++
	state.generation = m.uiOperationID
	m.keepassSettings = state
	m.currentView = constants.KeePassSettingsView
}

func (m *CLIModel) clearKeePassSettings() {
	state := m.keepassSettings
	if state == nil {
		return
	}
	if state.cancel != nil {
		state.cancel()
	}
	state.masterInput.SetValue("")
	state.confirmInput.SetValue("")
	state.consentInput.SetValue("")
	state.retryPathInput.SetValue("")
	m.keepassSettings = nil
}

func (m *CLIModel) wipeKeePassSettingsSecrets() {
	state := m.keepassSettings
	if state == nil {
		return
	}
	state.masterInput.SetValue("")
	state.confirmInput.SetValue("")
	state.consentInput.SetValue("")
}

func (m *CLIModel) keepassConfigLoad() (*config.Config, error) {
	if m.loadConfigFn != nil {
		return m.loadConfigFn()
	}
	return getConfigurationManager().LoadConfiguration()
}

func (m *CLIModel) keepassMenuActions() []string {
	policy := wallet.CredentialBackupPolicy{}
	if m.credentialService != nil {
		policy = m.credentialService.Policy()
	}
	actions := []string{"create", "link", "test", "pending"}
	if policy.Enabled {
		actions = append(actions, "disable")
	}
	return append(actions, "back")
}

func (m *CLIModel) updateKeePassSettings(msg tea.Msg) (tea.Model, tea.Cmd) {
	state := m.keepassSettings
	if state == nil {
		m.currentView = constants.ConfigurationView
		return m, nil
	}
	switch message := msg.(type) {
	case keepassActionMsg:
		if message.generation != state.generation {
			return m, nil
		}
		state.busy = false
		state.cancelling = false
		if state.cancel != nil {
			state.cancel()
			state.cancel = nil
		}
		if message.closeOp {
			m.clearCredentialOperation()
		}
		if state.quitAfterResult {
			return m, tea.Quit
		}
		if message.err != nil {
			errKey := keepassActionErrorKey(message.err)
			if message.configCommitted {
				state.setErrKey("keepass_config_committed_error", map[string]interface{}{"Reason": localization.T(errKey, nil)})
			} else if message.fileCommitted {
				state.setErrKey("keepass_created_save_failed", map[string]interface{}{"Path": safeInline(message.path), "Reason": localization.T(errKey, nil)})
			} else {
				state.setErrKey(errKey, nil)
			}
			if state.mode == "create" || state.mode == "link" {
				if message.fileCommitted {
					state.mode = "link"
				}
				state.stage = keepassStageMaster
				state.masterInput.SetValue("")
				state.confirmInput.SetValue("")
				state.consentInput.SetValue("")
				state.masterInput.Focus()
			}
			return m, nil
		}
		m.refreshCredentialConfig()
		state.noticeKey = message.noticeKey
		state.noticeParams = message.noticeParams
		state.notice = ""
		state.stage = keepassStageMenu
		state.mode = ""
		return m, nil
	case keepassPendingMsg:
		if message.generation != state.generation {
			return m, nil
		}
		state.busy = false
		if state.cancel != nil {
			state.cancel()
			state.cancel = nil
		}
		if state.quitAfterResult {
			return m, tea.Quit
		}
		if message.err != nil {
			state.setErr(message.err)
			return m, nil
		}
		state.pending = message.rows
		state.pendingIndex = 0
		state.pendingScroll = 0
		state.stage = keepassStagePendingList
		return m, nil
	case keepassPathSuggestionsMsg:
		if message.generation != state.suggestGeneration {
			return m, nil
		}
		if state.stage == keepassStagePath && state.pathInput.Value() == message.value {
			state.pathInput.SetSuggestions(message.suggestions)
		}
		if state.stage == keepassStageRetryPath && state.retryPathInput.Value() == message.value {
			state.retryPathInput.SetSuggestions(message.suggestions)
		}
		return m, nil
	case tea.KeyMsg:
		return m.updateKeePassSettingsKey(message)
	}
	return m, nil
}

func (m *CLIModel) updateKeePassSettingsKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	state := m.keepassSettings
	if state.busy {
		if key.String() == "esc" && state.cancel != nil {
			state.cancel()
			state.cancelling = true
		}
		return m, nil
	}
	state.err = ""
	state.errKey = ""
	state.errParams = nil
	state.notice = ""
	state.noticeKey = ""
	state.noticeParams = nil
	switch state.stage {
	case keepassStageMenu:
		actions := m.keepassMenuActions()
		switch key.String() {
		case "up", "k":
			if state.menuIndex > 0 {
				state.menuIndex--
			}
		case "down", "j":
			if state.menuIndex < len(actions)-1 {
				state.menuIndex++
			}
		case "enter":
			return m.runKeePassMenuAction(actions[state.menuIndex])
		case "esc":
			m.clearKeePassSettings()
			m.currentView = constants.ConfigurationView
		}
		return m, nil
	case keepassStagePath:
		return m.updateKeePassPathStage(key)
	case keepassStageMaster, keepassStageConfirmMaster, keepassStageConsent:
		return m.updateKeePassWizardSecret(key)
	case keepassStageDisableConfirm:
		switch key.String() {
		case "esc":
			state.stage = keepassStageMenu
			return m, nil
		case "enter":
			consent := strings.TrimSpace(state.consentInput.Value())
			if consent != "DISABLE" {
				state.setErrKey("keepass_consent_required", nil)
				return m, nil
			}
			state.consentInput.SetValue("")
			state.consentInput.Blur()
			return m.disableKeePass(state)
		}
		var command tea.Cmd
		state.consentInput, command = state.consentInput.Update(key)
		return m, command
	case keepassStagePendingList:
		switch key.String() {
		case "esc":
			state.stage = keepassStageMenu
			return m, nil
		case "up", "k":
			if state.pendingIndex > 0 {
				state.pendingIndex--
				if state.pendingIndex < state.pendingScroll {
					state.pendingScroll = state.pendingIndex
				}
			}
		case "down", "j":
			if state.pendingIndex < len(state.pending)-1 {
				state.pendingIndex++
				if state.pendingIndex >= state.pendingScroll+m.keepassPendingLimit() {
					state.pendingScroll = state.pendingIndex - m.keepassPendingLimit() + 1
				}
			}
		case "enter":
			if len(state.pending) == 0 {
				return m, nil
			}
			row := state.pending[state.pendingIndex]
			rowCopy := row
			state.retryRow = &rowCopy
			return m.beginPendingRetry()
		}
		return m, nil
	case keepassStageRetryPath, keepassStageRetryPassword, keepassStageRetryConfirm:
		return m.updatePendingRetryKey(key)
	}
	return m, nil
}

func (m *CLIModel) updateKeePassPathStage(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	state := m.keepassSettings
	switch key.String() {
	case "esc":
		state.stage = keepassStageMenu
		state.pathInput.Blur()
		return m, nil
	case "enter":
		path := canonicalExpandHome(strings.TrimSpace(state.pathInput.Value()))
		if !filepath.IsAbs(path) || !strings.EqualFold(filepath.Ext(path), ".kdbx") {
			state.setErrKey("keepass_path_required", nil)
			return m, nil
		}
		state.pathInput.SetValue(path)
		state.pathInput.Blur()
		state.masterInput.SetValue("")
		state.masterInput.Focus()
		state.stage = keepassStageMaster
		return m, nil
	}
	var command tea.Cmd
	state.pathInput, command = state.pathInput.Update(key)
	return m, tea.Batch(command, keepassSuggestCmd(state, &state.pathInput, ".kdbx"))
}

func keepassSuggestCmd(state *keepassSettingsState, input *textinput.Model, ext string) tea.Cmd {
	value := input.Value()
	key := "path"
	if input == &state.retryPathInput {
		key = "retryPath"
		ext = ".json"
	}
	if state.suggestKey == key && state.suggestValue == value {
		return nil
	}
	state.suggestKey = key
	state.suggestValue = value
	if value == "" {
		return nil
	}
	state.suggestGeneration++
	generation := state.suggestGeneration
	return func() tea.Msg {
		suggestions, err := pathSuggestions(value, false, ext)
		if err != nil {
			return keepassPathSuggestionsMsg{generation: generation, value: value}
		}
		return keepassPathSuggestionsMsg{generation: generation, value: value, suggestions: suggestions}
	}
}

func (m *CLIModel) updateKeePassWizardSecret(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	state := m.keepassSettings
	stages := []int{keepassStageMaster}
	if state.mode == "create" {
		stages = append(stages, keepassStageConfirmMaster)
	}
	stages = append(stages, keepassStageConsent)
	index := 0
	for i, stage := range stages {
		if stage == state.stage {
			index = i
		}
	}
	inputFor := func(stage int) *textinput.Model {
		switch stage {
		case keepassStageMaster:
			return &state.masterInput
		case keepassStageConfirmMaster:
			return &state.confirmInput
		default:
			return &state.consentInput
		}
	}
	switch key.String() {
	case "esc":
		state.stage = keepassStageMenu
		state.masterInput.SetValue("")
		state.confirmInput.SetValue("")
		state.consentInput.SetValue("")
		return m, nil
	case "enter":
		current := inputFor(state.stage)
		switch state.stage {
		case keepassStageMaster:
			if state.mode == "create" {
				master := []byte(state.masterInput.Value())
				validationErr := keepass.ValidateMasterPassword(master)
				clear(master)
				if validationErr != nil {
					state.setErrKey("keepass_master_weak", nil)
					return m, nil
				}
			} else if state.masterInput.Value() == "" {
				state.setErrKey("keepass_master_required", nil)
				return m, nil
			}
		case keepassStageConfirmMaster:
			if !wallet.SecureCompare(state.masterInput.Value(), state.confirmInput.Value()) {
				state.setErrKey("passwords_do_not_match", nil)
				state.confirmInput.SetValue("")
				return m, nil
			}
		case keepassStageConsent:
			if strings.TrimSpace(state.consentInput.Value()) != "ENABLE" {
				state.setErrKey("keepass_consent_required", nil)
				return m, nil
			}
			return m.submitKeePassWizard()
		}
		current.Blur()
		next := inputFor(stages[index+1])
		next.SetValue("")
		next.Focus()
		state.stage = stages[index+1]
		return m, nil
	}
	var command tea.Cmd
	*inputFor(state.stage), command = inputFor(state.stage).Update(key)
	return m, command
}

func (m *CLIModel) submitKeePassWizard() (tea.Model, tea.Cmd) {
	state := m.keepassSettings
	path := canonicalExpandHome(strings.TrimSpace(state.pathInput.Value()))
	master := []byte(state.masterInput.Value())
	mode := state.mode
	state.masterInput.SetValue("")
	state.confirmInput.SetValue("")
	state.consentInput.SetValue("")
	state.busy = true
	m.uiOperationID++
	state.generation = m.uiOperationID
	generation := state.generation
	service := m.credentialService
	store := m.credentialStore
	vault := m.Vault
	loadFn := m.loadConfigFn
	saveFn := m.saveConfigFn
	if loadFn == nil {
		loadFn = getConfigurationManager().LoadConfiguration
	}
	if saveFn == nil {
		saveFn = getConfigurationManager().SaveConfiguration
	}
	ctx, cancel := context.WithTimeout(context.Background(), canonicalImportTimeout)
	state.cancel = cancel
	return m, func() tea.Msg {
		defer cancel()
		defer clear(master)
		if store == nil || service == nil || vault == nil {
			return keepassActionMsg{generation: generation, err: wallet.ErrCredentialBackupUnavailable}
		}
		var binding keepass.Binding
		fileCommitted := false
		if mode == "create" {
			vaultID, err := vault.CredentialVaultID(ctx)
			if err != nil {
				return keepassActionMsg{generation: generation, err: err}
			}
			created, err := store.Create(ctx, path, vaultID, master)
			if err != nil {
				msg := keepassActionMsg{generation: generation, err: err, path: path}
				var committed *keepass.CommittedWarning
				if errors.As(err, &committed) {
					msg.fileCommitted = true
				}
				return msg
			}
			binding = created
			fileCommitted = true
		} else {
			targetID, err := store.Inspect(ctx, path, master)
			if err != nil {
				return keepassActionMsg{generation: generation, err: err}
			}
			vaultID, err := vault.CredentialVaultID(ctx)
			if err != nil {
				return keepassActionMsg{generation: generation, err: err}
			}
			binding = keepass.Binding{Path: path, TargetID: targetID, VaultID: vaultID}
		}
		if err := ctx.Err(); err != nil {
			return keepassActionMsg{generation: generation, err: err, fileCommitted: fileCommitted, path: binding.Path}
		}
		cfg, err := loadFn()
		if err != nil {
			return keepassActionMsg{generation: generation, err: err, fileCommitted: fileCommitted, path: binding.Path}
		}
		if err := ctx.Err(); err != nil {
			return keepassActionMsg{generation: generation, err: err, fileCommitted: fileCommitted, path: binding.Path}
		}
		updated := *cfg
		cfg = &updated
		cfg.KeePass = config.KeePassConfig{Enabled: true, Path: binding.Path, TargetID: binding.TargetID, VaultID: binding.VaultID}
		committedWarning := false
		if saveErr := saveFn(cfg); saveErr != nil {
			if !config.IsConfigCommitted(saveErr) {
				return keepassActionMsg{generation: generation, err: saveErr, fileCommitted: fileCommitted, path: binding.Path}
			}
			committedWarning = true
		}
		applyCtx, applyCancel := context.WithTimeout(context.WithoutCancel(ctx), canonicalImportTimeout)
		defer applyCancel()
		if err := service.Configure(applyCtx, wallet.CredentialBackupPolicy{Enabled: true, Binding: binding}); err != nil {
			return keepassActionMsg{generation: generation, err: err, fileCommitted: fileCommitted, configCommitted: true, path: binding.Path}
		}
		msg := keepassActionMsg{generation: generation, noticeKey: "keepass_enabled_notice"}
		if committedWarning {
			msg.noticeKey = "keepass_config_warning"
		}
		return msg
	}
}

func (m *CLIModel) disableKeePass(state *keepassSettingsState) (tea.Model, tea.Cmd) {
	state.busy = true
	m.uiOperationID++
	state.generation = m.uiOperationID
	generation := state.generation
	service := m.credentialService
	binding := wallet.CredentialBackupPolicy{}.Binding
	if service != nil {
		binding = service.Policy().Binding
	}
	loadFn := m.loadConfigFn
	saveFn := m.saveConfigFn
	if loadFn == nil {
		loadFn = getConfigurationManager().LoadConfiguration
	}
	if saveFn == nil {
		saveFn = getConfigurationManager().SaveConfiguration
	}
	ctx, cancel := context.WithTimeout(context.Background(), canonicalImportTimeout)
	state.cancel = cancel
	return m, func() tea.Msg {
		defer cancel()
		cfg, err := loadFn()
		if err != nil {
			return keepassActionMsg{generation: generation, err: err}
		}
		if err := ctx.Err(); err != nil {
			return keepassActionMsg{generation: generation, err: err}
		}
		updated := *cfg
		cfg = &updated
		cfg.KeePass = config.KeePassConfig{Enabled: false, Path: binding.Path, TargetID: binding.TargetID, VaultID: binding.VaultID}
		committedWarning := false
		if saveErr := saveFn(cfg); saveErr != nil {
			if !config.IsConfigCommitted(saveErr) {
				return keepassActionMsg{generation: generation, err: saveErr}
			}
			committedWarning = true
		}
		if service != nil {
			applyCtx, applyCancel := context.WithTimeout(context.WithoutCancel(ctx), canonicalImportTimeout)
			defer applyCancel()
			if err := service.Configure(applyCtx, wallet.CredentialBackupPolicy{Enabled: false, Binding: binding}); err != nil {
				return keepassActionMsg{generation: generation, err: err, configCommitted: true}
			}
		}
		msg := keepassActionMsg{generation: generation, noticeKey: "keepass_disabled_notice", closeOp: true}
		if committedWarning {
			msg.noticeKey = "keepass_config_warning"
		}
		return msg
	}
}

func (m *CLIModel) runKeePassMenuAction(action string) (tea.Model, tea.Cmd) {
	state := m.keepassSettings
	switch action {
	case "create", "link":
		state.mode = action
		state.stage = keepassStagePath
		defaultPath := ""
		if cfg := m.balanceConfig; cfg != nil && cfg.AppDir != "" {
			defaultPath = filepath.Join(cfg.AppDir, "bloco-wallet.kdbx")
		}
		if action == "link" {
			defaultPath = ""
		}
		state.pathInput.SetValue(defaultPath)
		state.pathInput.Focus()
		return m, nil
	case "test":
		if !m.credentialBackupEnabled() {
			state.setErrKey("keepass_err_unavailable", nil)
			return m, nil
		}
		m.uiOperationID++
		state.generation = m.uiOperationID
		generation := state.generation
		return m, m.requestCredentialOperation(func(op *wallet.CredentialBackupOperation) tea.Cmd {
			state.busy = true
			return tea.Sequence(func() tea.Msg {
				defer op.Close()
				return keepassActionMsg{generation: generation, noticeKey: "keepass_test_ok", closeOp: true}
			}, func() tea.Msg {
				return credentialOpDoneMsg{op: op}
			})
		})
	case "pending":
		state.busy = true
		m.uiOperationID++
		state.generation = m.uiOperationID
		generation := state.generation
		service := m.credentialService
		ctx, cancel := context.WithTimeout(context.Background(), canonicalImportTimeout)
		state.cancel = cancel
		return m, func() tea.Msg {
			defer cancel()
			if service == nil {
				return keepassPendingMsg{generation: generation, err: wallet.ErrCredentialBackupUnavailable}
			}
			rows, err := service.Pending(ctx)
			return keepassPendingMsg{generation: generation, rows: rows, err: err}
		}
	case "disable":
		state.stage = keepassStageDisableConfirm
		state.consentInput.SetValue("")
		state.consentInput.Focus()
		return m, nil
	case "back":
		m.clearKeePassSettings()
		m.currentView = constants.ConfigurationView
		return m, nil
	}
	return m, nil
}

func (state *keepassSettingsState) retryAllowsKeePass() bool {
	return state.retryRow != nil
}

func (m *CLIModel) beginPendingRetry() (tea.Model, tea.Cmd) {
	state := m.keepassSettings
	row := state.retryRow
	if row == nil {
		return m, nil
	}
	state.retryConfirmID = ""
	state.retryUseKeePass = false
	state.retryPath = row.ArtifactPath
	if row.State == wallet.CredentialBackupStateDeletePending {
		state.consentInput.SetValue("")
		state.consentInput.Focus()
		state.stage = keepassStageRetryConfirm
		return m, nil
	}
	if row.ItemID != "account" {
		state.retryPathInput.SetValue(row.ArtifactPath)
		state.retryPathInput.Focus()
		state.stage = keepassStageRetryPath
		return m, nil
	}
	state.masterInput.SetValue("")
	state.masterInput.Focus()
	state.stage = keepassStageRetryPassword
	return m, nil
}

func (m *CLIModel) updatePendingRetryKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	state := m.keepassSettings
	switch key.String() {
	case "esc":
		state.stage = keepassStagePendingList
		state.masterInput.SetValue("")
		state.consentInput.SetValue("")
		state.retryPathInput.SetValue("")
		state.retryUseKeePass = false
		return m, nil
	case "ctrl+k":
		if state.stage == keepassStageRetryPassword && state.retryRow != nil {
			state.retryUseKeePass = !state.retryUseKeePass
			if state.retryUseKeePass {
				state.masterInput.SetValue("")
			}
		}
		return m, nil
	case "enter":
		switch state.stage {
		case keepassStageRetryPath:
			path := canonicalExpandHome(strings.TrimSpace(state.retryPathInput.Value()))
			if !filepath.IsAbs(path) {
				state.setErrKey("keepass_path_required", nil)
				return m, nil
			}
			state.retryPath = path
			state.retryPathInput.Blur()
			state.masterInput.SetValue("")
			state.masterInput.Focus()
			state.stage = keepassStageRetryPassword
			return m, nil
		case keepassStageRetryConfirm:
			typed := strings.TrimSpace(state.consentInput.Value())
			state.consentInput.SetValue("")
			if state.retryRow == nil || typed != state.retryRow.AccountID {
				state.setErrKey("keepass_consent_required", nil)
				return m, nil
			}
			state.retryConfirmID = typed
			return m.runPendingRetry()
		default:
			password := []byte(state.masterInput.Value())
			state.masterInput.SetValue("")
			if len(password) == 0 && !state.retryUseKeePass && state.retryRow != nil && state.retryRow.ItemID == "account" {
				clear(password)
				state.setErrKey("keepass_password_required", nil)
				return m, nil
			}
			return m.runPendingRetryWithPassword(password)
		}
	}
	var command tea.Cmd
	switch state.stage {
	case keepassStageRetryPath:
		state.retryPathInput, command = state.retryPathInput.Update(key)
		return m, tea.Batch(command, keepassSuggestCmd(state, &state.retryPathInput, ".json"))
	case keepassStageRetryConfirm:
		state.consentInput, command = state.consentInput.Update(key)
	default:
		state.masterInput, command = state.masterInput.Update(key)
	}
	return m, command
}

func (m *CLIModel) runPendingRetry() (tea.Model, tea.Cmd) {
	return m.runPendingRetryWithPassword(nil)
}

func (m *CLIModel) runPendingRetryWithPassword(password []byte) (tea.Model, tea.Cmd) {
	state := m.keepassSettings
	row := state.retryRow
	if row == nil {
		return m, nil
	}
	rowCopy := *row
	confirmID := state.retryConfirmID
	useKeePass := state.retryUseKeePass
	retryPath := state.retryPath
	m.uiOperationID++
	state.generation = m.uiOperationID
	generation := state.generation
	return m, m.requestCredentialOperationCancel(func(op *wallet.CredentialBackupOperation) tea.Cmd {
		state.busy = true
		ctx, cancel := context.WithTimeout(op.Context(), canonicalImportTimeout)
		state.cancel = cancel
		return func() tea.Msg {
			defer cancel()
			defer op.Close()
			defer func() {
				if password != nil {
					clear(password)
				}
			}()
			var err error
			switch {
			case rowCopy.State == wallet.CredentialBackupStateDeletePending:
				err = op.RetryDeletion(ctx, rowCopy.AccountID, confirmID)
			case rowCopy.ItemID == "account":
				if useKeePass {
					err = op.WithAccountPassword(ctx, rowCopy.AccountID, func(stored []byte) error {
						return op.BackupAccount(ctx, rowCopy.AccountID, stored)
					})
				} else {
					err = op.BackupAccount(ctx, rowCopy.AccountID, password)
				}
			default:
				ciphertext, readErr := readCanonicalKeystore(retryPath)
				if readErr != nil {
					return keepassActionMsg{generation: generation, err: readErr, closeOp: true}
				}
				defer clear(ciphertext)
				digest := fmt.Sprintf("%x", sha256.Sum256(ciphertext))
				if rowCopy.ArtifactDigest != "" && digest != rowCopy.ArtifactDigest {
					return keepassActionMsg{generation: generation, err: errKeePassDigestMismatch, closeOp: true}
				}
				key := wallet.CredentialBackupKey{TargetID: rowCopy.TargetID, VaultID: rowCopy.VaultID, AccountID: rowCopy.AccountID, ItemID: rowCopy.ItemID}
				if useKeePass {
					err = op.WithFilePassword(ctx, rowCopy.ArtifactKind, ciphertext, func(stored []byte) error {
						return op.RetryArtifact(ctx, key, ciphertext, stored, retryPath)
					})
				} else {
					err = op.RetryArtifact(ctx, key, ciphertext, password, retryPath)
				}
			}
			if err != nil {
				return keepassActionMsg{generation: generation, err: err, closeOp: true}
			}
			report, syncErr := op.Sync(ctx)
			if syncErr != nil {
				return keepassActionMsg{generation: generation, err: syncErr, closeOp: true}
			}
			return keepassActionMsg{generation: generation, noticeKey: "keepass_retry_done", noticeParams: map[string]interface{}{"Saved": report.AccountsSaved + report.FilesSaved, "Deleted": report.Deleted}, closeOp: true}
		}
	}, func() {
		if password != nil {
			clear(password)
		}
	})
}

var errKeePassDigestMismatch = errors.New("credential artifact digest mismatch")

func keepassActionErrorKey(err error) string {
	switch {
	case keepass.IsCommitted(err):
		return "keepass_err_durability"
	case errors.Is(err, errKeePassDigestMismatch):
		return "keepass_err_digest"
	case errors.Is(err, keepass.ErrAuthentication):
		return "keepass_err_auth"
	case errors.Is(err, keepass.ErrBusy):
		return "keepass_err_busy"
	case errors.Is(err, keepass.ErrNotFound):
		return "keepass_err_missing"
	case errors.Is(err, keepass.ErrConflict), errors.Is(err, config.ErrConfigConflict):
		return "keepass_err_conflict"
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return "keepass_err_cancelled"
	default:
		return "keepass_err_failed"
	}
}

func (m *CLIModel) refreshCredentialConfig() {
	cfg, err := m.keepassConfigLoad()
	if err != nil {
		return
	}
	m.currentConfig = cfg
	if m.balanceConfig != nil {
		m.balanceConfig = cfg
	}
}

func keepassStateLabel(state string) string {
	switch state {
	case wallet.CredentialBackupStateSynced:
		return localization.Get("keepass_state_synced")
	case wallet.CredentialBackupStatePending:
		return localization.Get("keepass_state_pending")
	case wallet.CredentialBackupStatePrepared:
		return localization.Get("keepass_state_prepared")
	case wallet.CredentialBackupStateDeletePending:
		return localization.Get("keepass_state_delete_pending")
	default:
		return localization.Get("keepass_state_not_registered")
	}
}

func keepassItemLabel(row wallet.CredentialBackupState) string {
	if row.ItemID == "account" {
		return localization.Get("keepass_item_account")
	}
	name := row.ArtifactName
	if name == "" {
		name = row.ItemID
	}
	return safeInline(name)
}

func (m *CLIModel) viewKeePassSettings() string {
	state := m.keepassSettings
	if state == nil {
		return ""
	}
	var view strings.Builder
	view.WriteString(lipgloss.NewStyle().Bold(true).Render(localization.Get("menu_keepass")) + "\n")
	policy := wallet.CredentialBackupPolicy{}
	if m.credentialService != nil {
		policy = m.credentialService.Policy()
	}
	statusLabel := localization.Get("keepass_status_disabled")
	if policy.Enabled {
		statusLabel = localization.Get("keepass_status_enabled")
	}
	pathWidth := max(1, m.width-m.styles.Content.GetHorizontalFrameSize()-ansi.StringWidth(localization.T("keepass_status_line", map[string]interface{}{"Status": statusLabel, "Path": ""}))-2)
	view.WriteString(localization.T("keepass_status_line", map[string]interface{}{"Status": statusLabel, "Path": ansi.Truncate(safeInline(policy.Binding.Path), pathWidth, "")}) + "\n")
	if text := state.errText(); text != "" {
		view.WriteString(m.styles.ErrorStyle.Render(safeInline(text)) + "\n")
	}
	if text := state.noticeText(); text != "" {
		view.WriteString(safeInline(text) + "\n")
	}
	switch state.stage {
	case keepassStageMenu:
		actions := m.keepassMenuActions()
		for i, action := range actions {
			label := localization.Get("keepass_action_" + action)
			if i == state.menuIndex {
				view.WriteString(m.styles.SelectedTitle.Render("▸ "+label) + "\n")
			} else {
				view.WriteString(m.styles.MenuTitle.Render("  "+label) + "\n")
			}
		}
	case keepassStagePath:
		view.WriteString(localization.Get("keepass_path_label") + "\n")
		view.WriteString(state.pathInput.View() + "\n")
		view.WriteString("\n" + m.styles.MenuDesc.Render(localization.Get("keepass_recovery_warning")) + "\n")
	case keepassStageMaster:
		view.WriteString(localization.Get("keepass_master_label") + "\n")
		view.WriteString(state.masterInput.View() + "\n")
	case keepassStageConfirmMaster:
		view.WriteString(localization.Get("keepass_confirm_label") + "\n")
		view.WriteString(state.confirmInput.View() + "\n")
	case keepassStageConsent:
		view.WriteString(localization.Get("keepass_consent_label") + "\n")
		view.WriteString(state.consentInput.View() + "\n")
		view.WriteString("\n" + m.styles.MenuDesc.Render(localization.Get("keepass_recovery_warning")) + "\n")
	case keepassStageDisableConfirm:
		view.WriteString(localization.Get("keepass_disable_confirm") + "\n")
		view.WriteString(state.consentInput.View() + "\n")
	case keepassStagePendingList:
		if len(state.pending) == 0 {
			view.WriteString(localization.Get("keepass_pending_empty") + "\n")
		}
		limit := m.keepassPendingLimit()
		if state.pendingIndex < state.pendingScroll {
			state.pendingScroll = state.pendingIndex
		}
		if state.pendingIndex >= state.pendingScroll+limit {
			state.pendingScroll = state.pendingIndex - limit + 1
		}
		if state.pendingScroll < 0 {
			state.pendingScroll = 0
		}
		maxWidth := max(1, m.width-m.styles.Content.GetHorizontalFrameSize()-2)
		end := state.pendingScroll + limit
		if end > len(state.pending) {
			end = len(state.pending)
		}
		for i := state.pendingScroll; i < end; i++ {
			row := state.pending[i]
			line := fmt.Sprintf("%s %s %s", keepassStateLabel(row.State), safeShort(row.AccountID), keepassItemLabel(row))
			line = ansi.Truncate(line, max(1, maxWidth-2), "")
			if i == state.pendingIndex {
				view.WriteString(m.styles.SelectedTitle.Render("▸ "+line) + "\n")
			} else {
				view.WriteString(m.styles.MenuTitle.Render("  "+line) + "\n")
			}
		}
		if len(state.pending) > limit {
			view.WriteString(m.styles.MenuDesc.Render(localization.T("keepass_pending_more", map[string]interface{}{"Count": len(state.pending)})) + "\n")
		}
		view.WriteString("\n" + m.styles.MenuDesc.Render(localization.Get("keepass_old_backups_warning")) + "\n")
	case keepassStageRetryPath:
		view.WriteString(localization.Get("keepass_pending_path_label") + "\n")
		view.WriteString(state.retryPathInput.View() + "\n")
	case keepassStageRetryPassword:
		label := localization.Get("keepass_wallet_password_label")
		if state.retryRow != nil && state.retryRow.ItemID != "account" {
			label = localization.Get("keepass_source_password_label")
		}
		view.WriteString(label + "\n")
		view.WriteString(state.masterInput.View() + "\n")
		if state.retryUseKeePass {
			view.WriteString(m.styles.MenuDesc.Render(localization.Get("keepass_method_keepass")) + "\n")
		}
	case keepassStageRetryConfirm:
		view.WriteString(localization.Get("keepass_retry_confirm_label") + "\n")
		view.WriteString(state.consentInput.View() + "\n")
		view.WriteString("\n" + m.styles.MenuDesc.Render(localization.Get("keepass_delete_entries_warning")) + "\n")
	}
	if state.busy {
		if state.cancelling {
			view.WriteString("\n" + localization.Get("canonical_cancelling") + "\n")
		} else {
			view.WriteString("\n" + localization.Get("canonical_processing") + "\n")
		}
	}
	return view.String()
}

func (m *CLIModel) keepassPendingLimit() int {
	state := m.keepassSettings
	limit := m.height - 16
	if state != nil && state.contentHeight > 0 {
		overhead := 6
		if state.errText() != "" || state.noticeText() != "" {
			overhead += 2
		}
		if state.busy {
			overhead += 2
		}
		limit = state.contentHeight - overhead
	}
	if limit < 1 {
		limit = 1
	}
	if limit > keepassPendingWindow {
		limit = keepassPendingWindow
	}
	return limit
}

const (
	keepassAccountStageMenu = iota
	keepassAccountStagePassword
)

type keepassAccountState struct {
	account         wallet.AccountSummary
	menuIndex       int
	stage           int
	hasBackup       bool
	password        textinput.Model
	rows            []wallet.CredentialBackupState
	report          *wallet.CredentialBackupReport
	busy            bool
	quitAfterResult bool
	cancel          context.CancelFunc
	generation      uint64
	err             string
	errKey          string
	errParams       map[string]interface{}
	notice          string
	noticeKey       string
	noticeParams    map[string]interface{}
}

func (state *keepassAccountState) setErrKey(key string, params map[string]interface{}) {
	state.err = ""
	state.errKey = key
	state.errParams = params
}

func (state *keepassAccountState) errText() string {
	if state.errKey != "" {
		return localization.T(state.errKey, state.errParams)
	}
	return state.err
}

func (state *keepassAccountState) noticeText() string {
	if state.noticeKey != "" {
		return localization.T(state.noticeKey, state.noticeParams)
	}
	return state.notice
}

func (m *CLIModel) initKeePassAccount(account wallet.AccountSummary) {
	if account.SignerKind != wallet.SignerKindSoftware || account.Capabilities&wallet.CapabilityExportSecret == 0 {
		return
	}
	if !m.credentialBackupEnabled() {
		return
	}
	password := newCredentialInput(localization.Get("enter_wallet_password"), true)
	m.uiOperationID++
	m.keepassAccount = &keepassAccountState{account: account, password: password, generation: m.uiOperationID}
	m.currentView = constants.KeePassAccountView
}

func (m *CLIModel) loadKeePassAccountStatus() tea.Cmd {
	state := m.keepassAccount
	service := m.credentialService
	if state == nil || service == nil {
		return nil
	}
	m.uiOperationID++
	state.generation = m.uiOperationID
	generation := state.generation
	state.busy = true
	accountID := state.account.AccountID
	ctx, cancel := context.WithCancel(context.Background())
	state.cancel = cancel
	return func() tea.Msg {
		defer cancel()
		rows, err := service.Status(ctx, accountID)
		return keepassStatusMsg{accountID: accountID, rows: rows, err: err, generation: generation}
	}
}

func (m *CLIModel) updateKeePassAccount(msg tea.Msg) (tea.Model, tea.Cmd) {
	state := m.keepassAccount
	if state == nil {
		m.currentView = constants.WalletDetailsView
		return m, nil
	}
	switch message := msg.(type) {
	case keepassStatusMsg:
		if message.generation != state.generation || message.accountID != state.account.AccountID {
			return m, nil
		}
		state.busy = false
		if state.cancel != nil {
			state.cancel()
			state.cancel = nil
		}
		if state.quitAfterResult {
			return m, tea.Quit
		}
		if message.report != nil {
			state.report = message.report
		}
		if message.err != nil {
			state.setErrKey(keepassActionErrorKey(message.err), nil)
			return m, nil
		}
		state.rows = message.rows
		state.hasBackup = false
		for _, row := range message.rows {
			if row.ItemID == "account" && row.State == wallet.CredentialBackupStateSynced {
				state.hasBackup = true
			}
		}
		if message.report != nil {
			state.noticeKey = "keepass_retry_done"
			state.noticeParams = map[string]interface{}{"Saved": message.report.AccountsSaved + message.report.FilesSaved, "Deleted": message.report.Deleted}
			state.notice = ""
			state.stage = keepassAccountStageMenu
			state.password.SetValue("")
			m.credentialUseKeePass = false
		}
		return m, nil
	case keepassActionMsg:
		if message.generation != state.generation {
			return m, nil
		}
		state.busy = false
		if state.cancel != nil {
			state.cancel()
			state.cancel = nil
		}
		if state.quitAfterResult {
			return m, tea.Quit
		}
		if message.err != nil {
			state.setErrKey(keepassActionErrorKey(message.err), nil)
			return m, nil
		}
		state.noticeKey = message.noticeKey
		state.noticeParams = message.noticeParams
		state.notice = ""
		return m, nil
	case tea.KeyMsg:
		if state.busy {
			if message.String() == "esc" && state.cancel != nil {
				state.cancel()
			}
			return m, nil
		}
		if state.stage == keepassAccountStagePassword {
			return m.updateKeePassAccountPassword(message)
		}
		switch key := message.String(); key {
		case "esc":
			m.keepassAccount = nil
			m.currentView = constants.WalletDetailsView
			return m, nil
		case "up", "k":
			if state.menuIndex > 0 {
				state.menuIndex--
			}
		case "down", "j":
			if state.menuIndex < 2 {
				state.menuIndex++
			}
		case "enter":
			state.err = ""
			state.errKey = ""
			state.errParams = nil
			state.notice = ""
			state.noticeKey = ""
			state.noticeParams = nil
			switch state.menuIndex {
			case 0:
				if !m.credentialBackupEnabled() {
					state.setErrKey("keepass_err_unavailable", nil)
					return m, nil
				}
				state.password.SetValue("")
				state.password.Focus()
				state.stage = keepassAccountStagePassword
				m.credentialUseKeePass = false
				return m, nil
			case 1:
				return m, m.loadKeePassAccountStatus()
			case 2:
				m.keepassAccount = nil
				m.currentView = constants.WalletDetailsView
				return m, nil
			}
		}
	}
	return m, nil
}

func (m *CLIModel) updateKeePassAccountPassword(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	state := m.keepassAccount
	switch key.String() {
	case "esc":
		state.password.SetValue("")
		state.password.Blur()
		state.stage = keepassAccountStageMenu
		m.credentialUseKeePass = false
		return m, nil
	case "ctrl+k":
		if state.hasBackup {
			m.credentialUseKeePass = !m.credentialUseKeePass
			if m.credentialUseKeePass {
				state.password.SetValue("")
			}
		}
		return m, nil
	case "enter":
		useKeePass := m.credentialUseKeePass && state.hasBackup
		password := []byte(state.password.Value())
		state.password.SetValue("")
		if len(password) == 0 && !useKeePass {
			state.setErrKey("keepass_password_required", nil)
			return m, nil
		}
		accountID := state.account.AccountID
		service := m.credentialService
		m.uiOperationID++
		state.generation = m.uiOperationID
		generation := state.generation
		return m, m.requestCredentialOperationCancel(func(op *wallet.CredentialBackupOperation) tea.Cmd {
			state.busy = true
			ctx, cancel := context.WithCancel(op.Context())
			state.cancel = cancel
			return tea.Sequence(func() tea.Msg {
				defer cancel()
				defer clear(password)
				defer op.Close()
				var err error
				if useKeePass {
					err = op.WithAccountPassword(ctx, accountID, func(stored []byte) error {
						return op.BackupAccount(ctx, accountID, stored)
					})
				} else {
					err = op.BackupAccount(ctx, accountID, password)
				}
				if err != nil {
					return keepassActionMsg{err: err, generation: generation}
				}
				report, syncErr := op.Sync(ctx)
				var rows []wallet.CredentialBackupState
				if syncErr == nil && service != nil {
					statusCtx, statusCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
					rows, syncErr = service.Status(statusCtx, accountID)
					statusCancel()
				}
				return keepassStatusMsg{accountID: accountID, rows: rows, report: &report, err: syncErr, generation: generation}
			}, func() tea.Msg {
				return credentialOpDoneMsg{op: op}
			})
		}, func() {
			clear(password)
		})
	}
	var command tea.Cmd
	state.password, command = state.password.Update(key)
	return m, command
}

func (m *CLIModel) viewKeePassAccount() string {
	state := m.keepassAccount
	if state == nil {
		return ""
	}
	var view strings.Builder
	view.WriteString(lipgloss.NewStyle().Bold(true).Render(localization.Get("keepass_account_title")) + "\n\n")
	if state.stage == keepassAccountStagePassword {
		view.WriteString(localization.Get("keepass_wallet_password_label") + "\n")
		view.WriteString(state.password.View() + "\n")
		view.WriteString(m.credentialMethodLabel(state.hasBackup) + "\n")
	} else {
		actions := []string{"keepass_save_backup", "keepass_view_status", "back_to_menu"}
		for i, key := range actions {
			label := localization.Get(key)
			if i == state.menuIndex {
				view.WriteString(m.styles.SelectedTitle.Render("▸ "+label) + "\n")
			} else {
				view.WriteString(m.styles.MenuTitle.Render("  "+label) + "\n")
			}
		}
	}
	for _, row := range state.rows {
		_, _ = fmt.Fprintf(&view, "  %s %s\n", keepassStateLabel(row.State), keepassItemLabel(row))
	}
	if state.report != nil && len(state.report.BackupPaths) > 0 {
		view.WriteString("\n" + safeInline(strings.Join(state.report.BackupPaths, ", ")) + "\n")
	}
	if state.busy {
		view.WriteString("\n" + localization.Get("canonical_processing") + "\n")
	}
	if text := state.errText(); text != "" {
		view.WriteString("\n" + m.styles.ErrorStyle.Render(safeInline(text)) + "\n")
	}
	if text := state.noticeText(); text != "" {
		view.WriteString("\n" + safeInline(text) + "\n")
	}
	return view.String()
}
