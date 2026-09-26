package ui

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"blocowallet/internal/constants"
	"blocowallet/internal/wallet"
	"blocowallet/pkg/localization"

	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	canonicalKeystoreLimit       = 1 << 20
	canonicalBatchMethod         = wallet.ImportMethod("keystore_batch")
	canonicalMnemonicBatchMethod = wallet.ImportMethod("mnemonic_batch")
	canonicalEncryptedMethod     = wallet.ImportMethod("bloco_encrypted")
	canonicalBatchLimit          = 100
	canonicalBatchDirectoryLimit = 300
	canonicalImportTimeout       = 5 * time.Minute
)

type canonicalImportField struct {
	key      string
	labelKey string
	optional bool
	input    textinput.Model
}

type canonicalBatchPreview struct {
	name    string
	address string
	digest  string
	err     string
}

type canonicalImportState struct {
	method                 wallet.ImportMethod
	fields                 []canonicalImportField
	stage                  int
	preview                *wallet.ImportPreview
	data                   []byte
	batchItems             []wallet.KeystoreBatchItem
	mnemonicItems          []wallet.MnemonicBatchItem
	batchPreviews          []canonicalBatchPreview
	resultLines            []string
	operationID            uint64
	busy                   bool
	cancelling             bool
	cancel                 context.CancelFunc
	err                    string
	sourcePassword         []byte
	sourcePasswordFromFile bool
	logDirectory           string
	events                 chan tea.Msg
	batchProgress          wallet.KeystoreBatchProgress
	progressStage          string
	reportProgress         func(wallet.KeystoreBatchProgress)
	suggestKey             string
	suggestValue           string
	suggestGeneration      uint64
}

type canonicalPreviewResultMsg struct {
	operationID   uint64
	preview       *wallet.ImportPreview
	data          []byte
	batchItems    []wallet.KeystoreBatchItem
	mnemonicItems []wallet.MnemonicBatchItem
	batchPreviews []canonicalBatchPreview
	err           error
}

type canonicalCommitResultMsg struct {
	operationID uint64
	summary     wallet.AccountSummary
	resultLines []string
	err         error
}

type canonicalImportProgressMsg struct {
	operationID uint64
	stage       string
	progress    wallet.KeystoreBatchProgress
}

type canonicalImportChannelClosedMsg struct{}

type canonicalSourcePasswordMsg struct {
	operationID uint64
	password    []byte
	found       bool
	err         error
}

type canonicalPathSuggestionsMsg struct {
	generation  uint64
	fieldKey    string
	value       string
	suggestions []string
	err         error
}

var (
	errCanonicalBatchSourceRead    = errors.New("source file or password sidecar could not be read")
	errCanonicalBatchValidation    = errors.New("keystore validation or decryption failed")
	errCanonicalMnemonicValidation = errors.New("mnemonic validation failed")
	errCanonicalImportCancelled    = errors.New("import cancelled")
)

func (state *canonicalImportState) isBatch() bool {
	return state.method == canonicalBatchMethod || state.method == canonicalMnemonicBatchMethod
}

func newCanonicalImportState(method wallet.ImportMethod) *canonicalImportState {
	state := &canonicalImportState{method: method}
	if !state.isBatch() {
		state.fields = append(state.fields, newCanonicalField("name", "canonical_label_name", false, false, 128))
	}
	switch method {
	case wallet.ImportMethodMnemonic:
		state.fields = append(state.fields,
			newCanonicalField("mnemonic", "canonical_label_mnemonic", false, true, 2048),
			newCanonicalField("language", "canonical_label_language", true, false, 32),
			newCanonicalField("passphrase", "canonical_label_passphrase", true, true, 1024),
			newCanonicalField("path", "canonical_label_path", true, false, 255),
		)
	case wallet.ImportMethodPrivateKey:
		state.fields = append(state.fields, newCanonicalField("private_key", "canonical_label_private_key", false, true, 128))
	case wallet.ImportMethodKeystore:
		state.fields = append(state.fields,
			newCanonicalField("keystore_path", "canonical_label_keystore_path", false, false, 1024),
			newCanonicalField("source_password", "canonical_label_source_password_keystore", true, true, constants.PasswordCharLimit),
		)
	case canonicalBatchMethod:
		state.fields = append(state.fields,
			newCanonicalField("directory", "canonical_label_directory_keystore", false, false, 1024),
		)
	case canonicalMnemonicBatchMethod:
		state.fields = append(state.fields,
			newCanonicalField("directory", "canonical_label_directory_mnemonic", false, false, 1024),
		)
	case canonicalEncryptedMethod:
		state.fields = append(state.fields,
			newCanonicalField("encrypted_path", "canonical_label_encrypted_path", false, false, 1024),
			newCanonicalField("source_password", "canonical_label_source_password_encrypted", false, true, constants.PasswordCharLimit),
		)
	case wallet.ImportMethodWatchOnly:
		state.fields = append(state.fields, newCanonicalField("address", "canonical_label_address", false, false, 42))
	}
	if method != wallet.ImportMethodWatchOnly {
		state.fields = append(state.fields,
			newCanonicalField("storage_password", "canonical_label_storage_password", false, true, constants.PasswordCharLimit),
			newCanonicalField("confirm_password", "canonical_label_confirm_password", false, true, constants.PasswordCharLimit),
		)
	}
	for index := range state.fields {
		if state.fields[index].key == "keystore_path" || state.fields[index].key == "directory" {
			state.fields[index].input.ShowSuggestions = true
		}
	}
	state.fields[0].input.Focus()
	return state
}

func newCanonicalField(key, labelKey string, optional, secret bool, limit int) canonicalImportField {
	input := textinput.New()
	input.Placeholder = localization.Get(labelKey)
	input.CharLimit = limit
	input.Width = 80
	if secret {
		input.EchoMode = textinput.EchoPassword
		input.EchoCharacter = '•'
	}
	return canonicalImportField{key: key, labelKey: labelKey, optional: optional, input: input}
}

func (m *CLIModel) initCanonicalImport(method wallet.ImportMethod) {
	m.canonicalImport = newCanonicalImportState(method)
	m.currentView = constants.CanonicalImportView
}

func (m *CLIModel) initCanonicalBatchImport() {
	m.initCanonicalImport(canonicalBatchMethod)
}

func (m *CLIModel) updateCanonicalImport(msg tea.Msg) (tea.Model, tea.Cmd) {
	state := m.canonicalImport
	if state == nil || m.Vault == nil {
		switch result := msg.(type) {
		case canonicalSourcePasswordMsg:
			clear(result.password)
		case canonicalPreviewResultMsg:
			clear(result.data)
			clearCanonicalBatchItems(result.batchItems)
			clearCanonicalMnemonicItems(result.mnemonicItems)
		}
		m.currentView = constants.ImportMethodSelectionView
		return m, nil
	}
	switch result := msg.(type) {
	case canonicalPreviewResultMsg:
		if result.operationID != state.operationID {
			clear(result.data)
			clearCanonicalBatchItems(result.batchItems)
			clearCanonicalMnemonicItems(result.mnemonicItems)
			return m, nil
		}
		state.busy = false
		state.cancelling = false
		state.events = nil
		if state.cancel != nil {
			state.cancel()
		}
		state.cancel = nil
		if result.err != nil {
			state.err = result.err.Error()
			return m, nil
		}
		state.preview = result.preview
		state.data = result.data
		state.batchItems = result.batchItems
		state.mnemonicItems = result.mnemonicItems
		state.batchPreviews = result.batchPreviews
		state.err = ""
		return m, nil
	case canonicalCommitResultMsg:
		if result.operationID != state.operationID {
			return m, nil
		}
		state.busy = false
		state.cancelling = false
		state.events = nil
		if state.cancel != nil {
			state.cancel()
		}
		state.cancel = nil
		if result.err != nil {
			state.err = result.err.Error()
			return m, nil
		}
		if len(result.resultLines) > 0 {
			state.resultLines = result.resultLines
			m.clearCanonicalImportSecrets(false)
			if result.summary.AccountID != "" {
				m.selectedAccount = &result.summary
			}
			return m, m.refreshWalletsTable()
		}
		m.clearCanonicalImport()
		m.selectedAccount = &result.summary
		m.currentView = constants.WalletDetailsView
		return m, m.refreshWalletsTable()
	case canonicalImportProgressMsg:
		if result.operationID != state.operationID || state.events == nil {
			return m, nil
		}
		state.batchProgress = result.progress
		state.progressStage = result.stage
		return m, waitCanonicalImportEvent(state.events)
	case canonicalImportChannelClosedMsg:
		return m, nil
	case canonicalSourcePasswordMsg:
		if result.operationID != state.operationID {
			clear(result.password)
			return m, nil
		}
		wasCancelling := state.cancelling
		state.busy = false
		state.cancelling = false
		if state.cancel != nil {
			state.cancel()
		}
		state.cancel = nil
		if wasCancelling {
			clear(result.password)
			clear(state.sourcePassword)
			state.sourcePassword = nil
			state.sourcePasswordFromFile = false
			state.err = errCanonicalImportCancelled.Error()
			return m, nil
		}
		if result.err != nil {
			clear(result.password)
			state.err = localization.Get("canonical_sidecar_read_error")
			return m, nil
		}
		clear(state.sourcePassword)
		if result.found {
			state.sourcePassword = result.password
			state.sourcePasswordFromFile = true
		} else {
			clear(result.password)
			state.sourcePassword = nil
			state.sourcePasswordFromFile = false
		}
		return m, m.advanceCanonicalField()
	case canonicalPathSuggestionsMsg:
		if result.generation != state.suggestGeneration || state.stage >= len(state.fields) {
			return m, nil
		}
		field := &state.fields[state.stage]
		if field.key != result.fieldKey || field.input.Value() != result.value {
			return m, nil
		}
		field.input.SetSuggestions(result.suggestions)
		return m, nil
	}
	if state.busy {
		return m, nil
	}
	if key, ok := msg.(tea.KeyMsg); ok && key.String() == "enter" {
		if len(state.resultLines) > 0 {
			m.clearCanonicalImport()
			m.initAccountList()
			return m, nil
		}
		if state.preview != nil {
			return m, m.startCanonicalCommit()
		}
		field := &state.fields[state.stage]
		if !field.optional && field.input.Value() == "" {
			state.err = localization.T("field_required", map[string]interface{}{"Field": localization.Get(field.labelKey)})
			return m, nil
		}
		if state.stage < len(state.fields)-1 {
			if field.key == "keystore_path" && state.method == wallet.ImportMethodKeystore {
				expanded := canonicalExpandHome(field.input.Value())
				if !filepath.IsAbs(expanded) || !strings.EqualFold(filepath.Ext(expanded), ".json") {
					state.err = localization.Get("canonical_invalid_keystore_path")
					return m, nil
				}
				field.input.SetValue(expanded)
				return m, m.startCanonicalSourcePasswordLookup()
			}
			if field.key == "directory" {
				field.input.SetValue(canonicalExpandHome(field.input.Value()))
			}
			return m, m.advanceCanonicalField()
		}
		return m, m.startCanonicalPreview()
	}
	if state.preview != nil {
		return m, nil
	}
	var command tea.Cmd
	state.fields[state.stage].input, command = state.fields[state.stage].input.Update(msg)
	if suggest := state.canonicalSuggestCmd(); suggest != nil {
		return m, tea.Batch(command, suggest)
	}
	return m, command
}

func (m *CLIModel) advanceCanonicalField() tea.Cmd {
	state := m.canonicalImport
	state.fields[state.stage].input.Blur()
	state.stage++
	if state.sourcePasswordFromFile && state.stage < len(state.fields) && state.fields[state.stage].key == "source_password" {
		state.stage++
	}
	state.fields[state.stage].input.Focus()
	state.err = ""
	return state.canonicalSuggestCmd()
}

func (m *CLIModel) startCanonicalSourcePasswordLookup() tea.Cmd {
	state := m.canonicalImport
	m.canonicalOperationID++
	state.operationID = m.canonicalOperationID
	operationID := state.operationID
	ctx, cancel := context.WithTimeout(context.Background(), canonicalImportTimeout)
	state.cancel = cancel
	state.busy = true
	state.cancelling = false
	state.err = ""
	path := state.value("keystore_path")
	return func() tea.Msg {
		defer cancel()
		if err := ctx.Err(); err != nil {
			return canonicalSourcePasswordMsg{operationID: operationID, err: err}
		}
		directory, err := openPathNoFollow(filepath.Dir(path), true)
		if err != nil {
			return canonicalSourcePasswordMsg{operationID: operationID, err: err}
		}
		defer func() { _ = directory.Close() }()
		password, found, err := readCanonicalPasswordFile(directory, filepath.Base(path))
		if ctxErr := ctx.Err(); ctxErr != nil {
			clear(password)
			return canonicalSourcePasswordMsg{operationID: operationID, err: ctxErr}
		}
		return canonicalSourcePasswordMsg{operationID: operationID, password: password, found: found, err: err}
	}
}

func waitCanonicalImportEvent(events <-chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-events
		if !ok {
			return canonicalImportChannelClosedMsg{}
		}
		return msg
	}
}

func (state *canonicalImportState) canonicalSuggestCmd() tea.Cmd {
	if state.stage >= len(state.fields) {
		return nil
	}
	field := &state.fields[state.stage]
	if field.key != "keystore_path" && field.key != "directory" {
		return nil
	}
	value := field.input.Value()
	if state.suggestKey == field.key && state.suggestValue == value {
		return nil
	}
	if field.key == "keystore_path" && state.suggestKey == "keystore_path" && state.suggestValue != value {
		clear(state.sourcePassword)
		state.sourcePassword = nil
		state.sourcePasswordFromFile = false
	}
	state.suggestKey = field.key
	state.suggestValue = value
	state.suggestGeneration++
	if value == "" {
		return nil
	}
	generation := state.suggestGeneration
	fieldKey := field.key
	directoriesOnly := field.key == "directory"
	return func() tea.Msg {
		suggestions, err := canonicalPathSuggestions(value, directoriesOnly)
		return canonicalPathSuggestionsMsg{generation: generation, fieldKey: fieldKey, value: value, suggestions: suggestions, err: err}
	}
}

func (m *CLIModel) startCanonicalPreview() tea.Cmd {
	state := m.canonicalImport
	m.canonicalOperationID++
	state.operationID = m.canonicalOperationID
	operationID := state.operationID
	ctx, cancel := context.WithTimeout(context.Background(), canonicalImportTimeout)
	state.cancel = cancel
	state.busy = true
	state.cancelling = false
	state.err = ""
	snapshot := cloneCanonicalImportState(state)
	if state.isBatch() {
		events := make(chan tea.Msg, canonicalBatchLimit+3)
		state.events = events
		state.batchProgress = wallet.KeystoreBatchProgress{}
		state.progressStage = "canonical_stage_validating"
		snapshot.reportProgress = func(report wallet.KeystoreBatchProgress) {
			events <- canonicalImportProgressMsg{operationID: operationID, stage: "canonical_stage_validating", progress: report}
		}
		worker := func() tea.Msg {
			defer close(events)
			defer cancel()
			defer clear(snapshot.sourcePassword)
			err := prepareCanonicalImportPreview(ctx, m.Vault, snapshot)
			if err != nil {
				clear(snapshot.data)
				clearCanonicalBatchItems(snapshot.batchItems)
				clearCanonicalMnemonicItems(snapshot.mnemonicItems)
			}
			events <- canonicalPreviewResultMsg{
				operationID:   operationID,
				preview:       snapshot.preview,
				data:          snapshot.data,
				batchItems:    snapshot.batchItems,
				mnemonicItems: snapshot.mnemonicItems,
				batchPreviews: snapshot.batchPreviews,
				err:           err,
			}
			return nil
		}
		return tea.Batch(worker, waitCanonicalImportEvent(events))
	}
	return func() tea.Msg {
		defer cancel()
		defer clear(snapshot.sourcePassword)
		err := prepareCanonicalImportPreview(ctx, m.Vault, snapshot)
		if err != nil {
			clear(snapshot.data)
			clearCanonicalBatchItems(snapshot.batchItems)
			clearCanonicalMnemonicItems(snapshot.mnemonicItems)
		}
		return canonicalPreviewResultMsg{
			operationID:   operationID,
			preview:       snapshot.preview,
			data:          snapshot.data,
			batchItems:    snapshot.batchItems,
			mnemonicItems: snapshot.mnemonicItems,
			batchPreviews: snapshot.batchPreviews,
			err:           err,
		}
	}
}

func cloneCanonicalImportState(state *canonicalImportState) *canonicalImportState {
	cloned := *state
	cloned.fields = append([]canonicalImportField(nil), state.fields...)
	cloned.preview = nil
	cloned.data = nil
	cloned.batchItems = nil
	cloned.mnemonicItems = nil
	cloned.batchPreviews = nil
	cloned.resultLines = nil
	cloned.cancel = nil
	cloned.busy = false
	cloned.cancelling = false
	cloned.events = nil
	cloned.sourcePassword = append([]byte(nil), state.sourcePassword...)
	return &cloned
}

func prepareCanonicalImportPreview(ctx context.Context, vault *wallet.WalletVault, state *canonicalImportState) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if state.method != wallet.ImportMethodWatchOnly {
		storagePassword := state.value("storage_password")
		confirmation := state.value("confirm_password")
		if !wallet.SecureCompare(storagePassword, confirmation) {
			return wallet.ErrStoragePasswordConfirmation
		}
		storagePasswordBytes := []byte(storagePassword)
		defer clear(storagePasswordBytes)
		if err := wallet.ValidateStoragePassword(storagePasswordBytes); err != nil {
			return err
		}
	}
	var preview wallet.ImportPreview
	var err error
	switch state.method {
	case wallet.ImportMethodMnemonic:
		preview, err = wallet.PreviewMnemonicImport(wallet.MnemonicImportRequest{
			Mnemonic:        state.value("mnemonic"),
			BIP39Passphrase: state.value("passphrase"),
			BIP39Language:   wallet.BIP39Language(strings.ToLower(strings.ReplaceAll(state.value("language"), "-", "_"))),
			DerivationPath:  state.value("path"),
		})
	case wallet.ImportMethodPrivateKey:
		preview, err = wallet.PreviewPrivateKeyImport(wallet.PrivateKeyImportRequest{PrivateKey: state.value("private_key")})
	case wallet.ImportMethodWatchOnly:
		preview, err = wallet.PreviewWatchOnlyImport(wallet.WatchOnlyImportRequest{Address: state.value("address")})
	case wallet.ImportMethodKeystore:
		state.data, err = readCanonicalKeystore(state.value("keystore_path"))
		if err == nil {
			sourcePassword := canonicalSourcePassword(state)
			preview, err = wallet.PreviewKeystoreImportContext(ctx, state.data, sourcePassword)
			clear(sourcePassword)
		}
	case canonicalBatchMethod:
		state.batchItems, err = readCanonicalKeystoreBatch(state.value("directory"))
		if err == nil {
			err = assignCanonicalBatchNames(ctx, vault, state)
		}
		if err == nil {
			if state.reportProgress != nil {
				state.reportProgress(wallet.KeystoreBatchProgress{Total: len(state.batchItems)})
			}
			batchProgress := wallet.KeystoreBatchProgress{Total: len(state.batchItems)}
			state.batchPreviews = make([]canonicalBatchPreview, 0, len(state.batchItems))
			for index := range state.batchItems {
				item := &state.batchItems[index]
				digest := sha256.Sum256(item.KeystoreJSON)
				itemPreview := canonicalBatchPreview{name: item.Name, digest: hex.EncodeToString(digest[:])}
				if item.PreflightErr != nil {
					itemPreview.err = canonicalBatchFailureReason(item.PreflightErr)
					state.batchPreviews = append(state.batchPreviews, itemPreview)
					batchProgress.Completed++
					batchProgress.Failed++
					if state.reportProgress != nil {
						state.reportProgress(batchProgress)
					}
					continue
				}
				validated, previewErr := wallet.PreviewKeystoreImportContext(ctx, item.KeystoreJSON, item.SourcePassword)
				if previewErr != nil {
					item.PreflightErr = fmt.Errorf("%w", errCanonicalBatchValidation)
					itemPreview.err = canonicalBatchFailureReason(item.PreflightErr)
				} else {
					itemPreview.address = validated.Address
				}
				state.batchPreviews = append(state.batchPreviews, itemPreview)
				batchProgress.Completed++
				if item.PreflightErr != nil {
					batchProgress.Failed++
				}
				if state.reportProgress != nil {
					state.reportProgress(batchProgress)
				}
			}
			preview = wallet.ImportPreview{SecretType: wallet.SecretTypePrivateKey, SourceFormat: fmt.Sprintf("keystore_v3_batch:%d", len(state.batchItems))}
		}
	case canonicalMnemonicBatchMethod:
		state.mnemonicItems, err = readCanonicalMnemonicBatch(state.value("directory"))
		if err == nil {
			err = assignCanonicalBatchNames(ctx, vault, state)
		}
		if err == nil {
			if state.reportProgress != nil {
				state.reportProgress(wallet.KeystoreBatchProgress{Total: len(state.mnemonicItems)})
			}
			batchProgress := wallet.KeystoreBatchProgress{Total: len(state.mnemonicItems)}
			state.batchPreviews = make([]canonicalBatchPreview, 0, len(state.mnemonicItems))
			for index := range state.mnemonicItems {
				item := &state.mnemonicItems[index]
				digest := sha256.Sum256(item.Mnemonic)
				itemPreview := canonicalBatchPreview{name: item.Name, digest: hex.EncodeToString(digest[:])}
				if item.PreflightErr != nil {
					itemPreview.err = canonicalBatchFailureReason(item.PreflightErr)
					state.batchPreviews = append(state.batchPreviews, itemPreview)
					batchProgress.Completed++
					batchProgress.Failed++
					if state.reportProgress != nil {
						state.reportProgress(batchProgress)
					}
					continue
				}
				validated, previewErr := wallet.PreviewMnemonicFileImport(item.Mnemonic)
				if previewErr != nil {
					item.PreflightErr = fmt.Errorf("%w", errCanonicalMnemonicValidation)
					itemPreview.err = canonicalBatchFailureReason(item.PreflightErr)
				} else {
					itemPreview.address = validated.Address
				}
				state.batchPreviews = append(state.batchPreviews, itemPreview)
				batchProgress.Completed++
				if item.PreflightErr != nil {
					batchProgress.Failed++
				}
				if state.reportProgress != nil {
					state.reportProgress(batchProgress)
				}
				if ctxErr := ctx.Err(); ctxErr != nil {
					err = ctxErr
					break
				}
			}
			if err == nil {
				preview = wallet.ImportPreview{SecretType: wallet.SecretTypeMnemonic, DerivationPath: "m/44'/60'/0'/0/0", SourceFormat: fmt.Sprintf("bip39_batch:%d", len(state.mnemonicItems))}
			}
		}
	case canonicalEncryptedMethod:
		state.data, err = readCanonicalKeystore(state.value("encrypted_path"))
		if err == nil {
			preview, err = vault.PreviewEncryptedAccountImport(ctx, state.data, []byte(state.value("source_password")))
		}
	default:
		err = fmt.Errorf("unsupported import method")
	}
	if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		clear(state.data)
		state.data = nil
		clearCanonicalBatchItems(state.batchItems)
		state.batchItems = nil
		clearCanonicalMnemonicItems(state.mnemonicItems)
		state.mnemonicItems = nil
		return err
	}
	state.preview = &preview
	state.err = ""
	return nil
}

func (m *CLIModel) startCanonicalCommit() tea.Cmd {
	state := m.canonicalImport
	m.canonicalOperationID++
	state.operationID = m.canonicalOperationID
	operationID := state.operationID
	ctx, cancel := context.WithTimeout(context.Background(), canonicalImportTimeout)
	state.cancel = cancel
	state.busy = true
	state.cancelling = false
	state.err = ""
	snapshot := cloneCanonicalCommitState(state)
	if state.isBatch() {
		snapshot.logDirectory = m.canonicalFailureLogDirectory()
		events := make(chan tea.Msg, canonicalBatchLimit+3)
		state.events = events
		state.batchProgress = wallet.KeystoreBatchProgress{}
		state.progressStage = "canonical_stage_importing"
		snapshot.reportProgress = func(report wallet.KeystoreBatchProgress) {
			events <- canonicalImportProgressMsg{operationID: operationID, stage: "canonical_stage_importing", progress: report}
		}
		worker := func() tea.Msg {
			defer close(events)
			defer cancel()
			defer clear(snapshot.sourcePassword)
			summary, resultLines, err := executeCanonicalImport(ctx, m.Vault, snapshot)
			clear(snapshot.data)
			clearCanonicalBatchItems(snapshot.batchItems)
			clearCanonicalMnemonicItems(snapshot.mnemonicItems)
			events <- canonicalCommitResultMsg{operationID: operationID, summary: summary, resultLines: resultLines, err: err}
			return nil
		}
		return tea.Batch(worker, waitCanonicalImportEvent(events))
	}
	return func() tea.Msg {
		defer cancel()
		defer clear(snapshot.sourcePassword)
		summary, resultLines, err := executeCanonicalImport(ctx, m.Vault, snapshot)
		clear(snapshot.data)
		clearCanonicalBatchItems(snapshot.batchItems)
		clearCanonicalMnemonicItems(snapshot.mnemonicItems)
		return canonicalCommitResultMsg{operationID: operationID, summary: summary, resultLines: resultLines, err: err}
	}
}

func (m *CLIModel) canonicalFailureLogDirectory() string {
	if m.balanceConfig != nil && m.balanceConfig.AppDir != "" {
		return m.balanceConfig.AppDir
	}
	cfg, err := loadOrCreateConfig()
	if err == nil && cfg != nil {
		return cfg.AppDir
	}
	return ""
}

func cloneCanonicalCommitState(state *canonicalImportState) *canonicalImportState {
	cloned := *state
	cloned.fields = append([]canonicalImportField(nil), state.fields...)
	cloned.data = append([]byte(nil), state.data...)
	cloned.batchItems = make([]wallet.KeystoreBatchItem, len(state.batchItems))
	for index, item := range state.batchItems {
		cloned.batchItems[index] = wallet.KeystoreBatchItem{
			Name:           item.Name,
			KeystoreJSON:   append([]byte(nil), item.KeystoreJSON...),
			SourcePassword: append([]byte(nil), item.SourcePassword...),
			SourcePath:     item.SourcePath,
			PreflightErr:   item.PreflightErr,
		}
	}
	cloned.mnemonicItems = make([]wallet.MnemonicBatchItem, len(state.mnemonicItems))
	for index, item := range state.mnemonicItems {
		cloned.mnemonicItems[index] = wallet.MnemonicBatchItem{
			Name:         item.Name,
			SourcePath:   item.SourcePath,
			Mnemonic:     append([]byte(nil), item.Mnemonic...),
			PreflightErr: item.PreflightErr,
		}
	}
	cloned.batchPreviews = append([]canonicalBatchPreview(nil), state.batchPreviews...)
	cloned.cancel = nil
	cloned.busy = false
	cloned.cancelling = false
	cloned.events = nil
	cloned.sourcePassword = append([]byte(nil), state.sourcePassword...)
	return &cloned
}

func executeCanonicalImport(ctx context.Context, vault *wallet.WalletVault, state *canonicalImportState) (wallet.AccountSummary, []string, error) {
	storagePassword := []byte(state.value("storage_password"))
	confirmation := []byte(state.value("confirm_password"))
	defer clear(storagePassword)
	defer clear(confirmation)
	var summary wallet.AccountSummary
	var err error
	switch state.method {
	case wallet.ImportMethodMnemonic:
		summary, err = vault.ImportMnemonic(ctx, wallet.MnemonicImportRequest{
			Name:                   strings.TrimSpace(state.value("name")),
			Mnemonic:               state.value("mnemonic"),
			BIP39Passphrase:        state.value("passphrase"),
			BIP39Language:          wallet.BIP39Language(strings.ToLower(strings.ReplaceAll(state.value("language"), "-", "_"))),
			DerivationPath:         state.value("path"),
			StoragePassword:        storagePassword,
			ConfirmStoragePassword: confirmation,
		})
	case wallet.ImportMethodPrivateKey:
		summary, err = vault.ImportPrivateKey(ctx, wallet.PrivateKeyImportRequest{
			Name:                   strings.TrimSpace(state.value("name")),
			PrivateKey:             state.value("private_key"),
			StoragePassword:        storagePassword,
			ConfirmStoragePassword: confirmation,
		})
	case wallet.ImportMethodWatchOnly:
		address := state.value("address")
		if state.preview != nil && state.preview.SignerKind == wallet.SignerKindWatchOnly && state.preview.Address != "" {
			address = state.preview.Address
		}
		summary, err = vault.ImportWatchOnly(ctx, wallet.WatchOnlyImportRequest{
			Name:    strings.TrimSpace(state.value("name")),
			Address: address,
		})
	case wallet.ImportMethodKeystore:
		sourcePassword := canonicalSourcePassword(state)
		summary, err = vault.ImportKeystore(ctx, wallet.KeystoreImportRequest{
			Name:                   strings.TrimSpace(state.value("name")),
			KeystoreJSON:           state.data,
			SourcePassword:         sourcePassword,
			StoragePassword:        storagePassword,
			ConfirmStoragePassword: confirmation,
		})
		clear(sourcePassword)
	case canonicalEncryptedMethod:
		sourcePassword := []byte(state.value("source_password"))
		summary, err = vault.ImportEncryptedAccount(ctx, wallet.EncryptedAccountImportRequest{
			Name:                   strings.TrimSpace(state.value("name")),
			ExportJSON:             state.data,
			ExportPassword:         sourcePassword,
			StoragePassword:        storagePassword,
			ConfirmStoragePassword: confirmation,
		})
		clear(sourcePassword)
	case canonicalBatchMethod:
		for index, item := range state.batchItems {
			digest := sha256.Sum256(item.KeystoreJSON)
			if index >= len(state.batchPreviews) || hex.EncodeToString(digest[:]) != state.batchPreviews[index].digest {
				return wallet.AccountSummary{}, nil, fmt.Errorf("batch item changed after preview")
			}
		}
		results := vault.ImportKeystoreBatch(ctx, wallet.KeystoreBatchImportRequest{
			Items:                  state.batchItems,
			StoragePassword:        storagePassword,
			ConfirmStoragePassword: confirmation,
			MaxConcurrency:         2,
			OnProgress:             state.reportProgress,
		})
		commitSummary, resultLines := canonicalBatchResultLines(state.logDirectory, "keystore-import-failures-*.log", canonicalBatchFileRefsFromKeystores(state.batchItems), results)
		return commitSummary, resultLines, nil
	case canonicalMnemonicBatchMethod:
		for index, item := range state.mnemonicItems {
			digest := sha256.Sum256(item.Mnemonic)
			if index >= len(state.batchPreviews) || hex.EncodeToString(digest[:]) != state.batchPreviews[index].digest {
				return wallet.AccountSummary{}, nil, fmt.Errorf("batch item changed after preview")
			}
		}
		results := vault.ImportMnemonicBatch(ctx, wallet.MnemonicBatchImportRequest{
			Items:                  state.mnemonicItems,
			StoragePassword:        storagePassword,
			ConfirmStoragePassword: confirmation,
			MaxConcurrency:         2,
			OnProgress:             state.reportProgress,
		})
		commitSummary, resultLines := canonicalBatchResultLines(state.logDirectory, "mnemonic-import-failures-*.log", canonicalBatchFileRefsFromMnemonics(state.mnemonicItems), results)
		return commitSummary, resultLines, nil
	default:
		err = fmt.Errorf("unsupported import method")
	}
	return summary, nil, err
}

func (m *CLIModel) viewCanonicalImport() string {
	state := m.canonicalImport
	if state == nil {
		return localization.Get("canonical_state_unavailable")
	}
	title := lipgloss.NewStyle().Bold(true).Render(localization.Get("canonical_import_title"))
	if state.busy {
		if state.cancelling {
			return title + "\n\n" + localization.Get("canonical_cancelling")
		}
		if state.isBatch() && state.batchProgress.Total > 0 {
			fraction := float64(state.batchProgress.Completed) / float64(state.batchProgress.Total)
			bar := progress.New(progress.WithDefaultGradient(), progress.WithWidth(40)).ViewAs(fraction)
			stageLabel := state.progressStage
			if stageLabel == "" {
				stageLabel = "canonical_stage_processing"
			}
			return fmt.Sprintf("%s\n\n%s\n%s\n%s",
				title, localization.Get(stageLabel), bar,
				localization.T("canonical_batch_progress", map[string]interface{}{
					"Found":    state.batchProgress.Total,
					"Done":     state.batchProgress.Completed,
					"Total":    state.batchProgress.Total,
					"Imported": state.batchProgress.Imported,
					"Already":  state.batchProgress.AlreadyImported,
					"Failed":   state.batchProgress.Failed,
				}))
		}
		return title + "\n\n" + localization.Get("canonical_processing")
	}
	if len(state.resultLines) > 0 {
		return title + "\n\n" + strings.Join(safeLines(state.resultLines), "\n")
	}
	if state.preview != nil && state.isBatch() {
		var view strings.Builder
		view.WriteString(title + "\n\n" + localization.Get("canonical_batch_preview_title") + "\n")
		if state.method == canonicalMnemonicBatchMethod {
			view.WriteString(localization.Get("detail_derivation_path") + ": m/44'/60'/0'/0/0\n")
		}
		for _, item := range state.batchPreviews {
			status := item.address
			if item.err != "" {
				status = localization.T("canonical_preview_error", map[string]interface{}{"Err": item.err})
			}
			if state.method == canonicalMnemonicBatchMethod {
				_, _ = fmt.Fprintf(&view, "%s | %s\n", safeShort(item.name), safeInline(status))
			} else {
				_, _ = fmt.Fprintf(&view, "%s | %s | sha256:%s\n", safeShort(item.name), safeInline(status), safeShort(item.digest[:16]))
			}
		}
		return view.String()
	}
	if state.preview != nil {
		preview := state.preview
		secretType := string(preview.SecretType)
		if secretType == "" {
			secretType = localization.Get("canonical_secret_none")
		}
		passphrasePresent := localization.Get("no")
		if preview.HasBIP39Passphrase {
			passphrasePresent = localization.Get("yes")
		}
		return fmt.Sprintf("%s\n\n%s",
			title, localization.T("canonical_preview_summary", map[string]interface{}{
				"Source":     safeShort(preview.SourceFormat),
				"Address":    safeShort(preview.Address),
				"Signer":     safeShort(string(preview.SignerKind)),
				"Secret":     safeShort(secretType),
				"Path":       safeShort(preview.DerivationPath),
				"Language":   safeShort(string(preview.BIP39Language)),
				"Passphrase": passphrasePresent,
			}))
	}
	field := state.fields[state.stage]
	view := fmt.Sprintf("%s\n\n%s\n%s:\n%s",
		title,
		localization.T("canonical_step", map[string]interface{}{"Step": state.stage + 1, "Total": len(state.fields)}),
		localization.Get(field.labelKey), field.input.View())
	if state.err != "" {
		view += "\n\n" + m.styles.ErrorStyle.Render(safeInline(state.err))
	}
	return view
}

func (m *CLIModel) clearCanonicalImportSecrets(clearResults bool) {
	if m.canonicalImport == nil {
		return
	}
	for index := range m.canonicalImport.fields {
		m.canonicalImport.fields[index].input.SetValue("")
	}
	clear(m.canonicalImport.data)
	m.canonicalImport.data = nil
	clearCanonicalBatchItems(m.canonicalImport.batchItems)
	m.canonicalImport.batchItems = nil
	clearCanonicalMnemonicItems(m.canonicalImport.mnemonicItems)
	m.canonicalImport.mnemonicItems = nil
	m.canonicalImport.batchPreviews = nil
	m.canonicalImport.preview = nil
	clear(m.canonicalImport.sourcePassword)
	m.canonicalImport.sourcePassword = nil
	m.canonicalImport.sourcePasswordFromFile = false
	m.canonicalImport.events = nil
	m.canonicalImport.batchProgress = wallet.KeystoreBatchProgress{}
	m.canonicalImport.progressStage = ""
	if clearResults {
		m.canonicalImport.resultLines = nil
	}
}

func (m *CLIModel) clearCanonicalImport() {
	if m.canonicalImport == nil {
		return
	}
	if m.canonicalImport.cancel != nil {
		m.canonicalImport.cancel()
	}
	m.clearCanonicalImportSecrets(true)
	m.canonicalImport = nil
}

func (state *canonicalImportState) value(key string) string {
	for _, field := range state.fields {
		if field.key == key {
			return field.input.Value()
		}
	}
	return ""
}

func readCanonicalKeystore(path string) (data []byte, err error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Size() > canonicalKeystoreLimit {
		return nil, fmt.Errorf("keystore path must be a regular file no larger than 1 MiB")
	}
	file, err := openPathNoFollow(path, false)
	if err != nil {
		return nil, err
	}
	openedInfo, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	if !openedInfo.Mode().IsRegular() || !os.SameFile(info, openedInfo) {
		_ = file.Close()
		return nil, fmt.Errorf("keystore changed while opening")
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}()
	data, err = io.ReadAll(io.LimitReader(file, canonicalKeystoreLimit+1))
	if err != nil {
		return nil, err
	}
	if len(data) > canonicalKeystoreLimit {
		clear(data)
		return nil, fmt.Errorf("keystore exceeds 1 MiB")
	}
	return data, nil
}

func readCanonicalFileAt(directory *os.File, name string, expected os.FileInfo, limit int64) (data []byte, err error) {
	file, err := openFileAtNoFollow(directory, name)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil {
			if err == nil {
				err = closeErr
			}
			clear(data)
			data = nil
		}
	}()
	openedInfo, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !openedInfo.Mode().IsRegular() || !os.SameFile(expected, openedInfo) || openedInfo.Size() > limit {
		return nil, fmt.Errorf("batch file changed while opening")
	}
	data, err = io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		clear(data)
		return nil, err
	}
	if len(data) > int(limit) {
		clear(data)
		return nil, fmt.Errorf("batch file exceeds %d bytes", limit)
	}
	return data, nil
}

func readCanonicalKeystoreAt(directory *os.File, name string, expected os.FileInfo) (data []byte, err error) {
	return readCanonicalFileAt(directory, name, expected, canonicalKeystoreLimit)
}

func readCanonicalKeystoreBatch(directory string) (items []wallet.KeystoreBatchItem, err error) {
	if !filepath.IsAbs(directory) {
		return nil, fmt.Errorf("batch directory must be absolute")
	}
	info, err := os.Lstat(directory)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return nil, fmt.Errorf("batch path must be a regular directory")
	}
	directoryFile, err := openPathNoFollow(directory, true)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := directoryFile.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}()
	openedInfo, err := directoryFile.Stat()
	if err != nil {
		return nil, err
	}
	if !openedInfo.IsDir() || !os.SameFile(info, openedInfo) {
		return nil, fmt.Errorf("batch directory changed while opening")
	}
	entries, err := directoryFile.ReadDir(canonicalBatchDirectoryLimit + 1)
	if err != nil && err != io.EOF {
		return nil, err
	}
	if len(entries) > canonicalBatchDirectoryLimit {
		return nil, fmt.Errorf("batch exceeds %d directory entries", canonicalBatchDirectoryLimit)
	}
	byName := make(map[string]os.DirEntry, len(entries))
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".json") {
			continue
		}
		names = append(names, entry.Name())
		byName[entry.Name()] = entry
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("batch directory contains no JSON keystore files")
	}
	if len(names) > canonicalBatchLimit {
		return nil, fmt.Errorf("batch exceeds %d files", canonicalBatchLimit)
	}
	sort.Strings(names)
	items = make([]wallet.KeystoreBatchItem, 0, len(names))
	for _, fileName := range names {
		item := wallet.KeystoreBatchItem{
			Name:       strings.TrimSuffix(fileName, filepath.Ext(fileName)),
			SourcePath: filepath.Join(directory, fileName),
		}
		entryInfo, infoErr := byName[fileName].Info()
		if infoErr != nil {
			item.PreflightErr = fmt.Errorf("%w: %v", errCanonicalBatchSourceRead, infoErr)
			items = append(items, item)
			continue
		}
		if entryInfo.Mode()&os.ModeSymlink != 0 || !entryInfo.Mode().IsRegular() {
			item.PreflightErr = fmt.Errorf("%w", errCanonicalBatchSourceRead)
			items = append(items, item)
			continue
		}
		data, readErr := readCanonicalKeystoreAt(directoryFile, fileName, entryInfo)
		if readErr != nil {
			item.PreflightErr = fmt.Errorf("%w: %v", errCanonicalBatchSourceRead, readErr)
			items = append(items, item)
			continue
		}
		item.KeystoreJSON = data
		password, found, passwordErr := readCanonicalPasswordFile(directoryFile, fileName)
		if passwordErr != nil {
			item.PreflightErr = fmt.Errorf("%w: %v", errCanonicalBatchSourceRead, passwordErr)
		} else if found {
			item.SourcePassword = password
		}
		items = append(items, item)
	}
	return items, nil
}

func readCanonicalPasswordFile(directory *os.File, keystoreName string) (password []byte, found bool, err error) {
	candidate := strings.TrimSuffix(keystoreName, filepath.Ext(keystoreName)) + ".pwd"
	file, err := openFileAtNoFollow(directory, candidate)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, true, err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, true, err
	}
	if !info.Mode().IsRegular() || info.Size() > 4096 {
		_ = file.Close()
		return nil, true, fmt.Errorf("password sidecar must be a regular file no larger than 4096 bytes")
	}
	data, err := io.ReadAll(io.LimitReader(file, 4097))
	closeErr := file.Close()
	if err != nil {
		clear(data)
		return nil, true, err
	}
	if closeErr != nil {
		clear(data)
		return nil, true, closeErr
	}
	if len(data) > 4096 {
		clear(data)
		return nil, true, fmt.Errorf("password sidecar must be a regular file no larger than 4096 bytes")
	}
	if bytes.HasSuffix(data, []byte("\r\n")) {
		data[len(data)-1] = 0
		data[len(data)-2] = 0
		data = data[:len(data)-2]
	} else if bytes.HasSuffix(data, []byte("\n")) {
		data[len(data)-1] = 0
		data = data[:len(data)-1]
	}
	return data, true, nil
}

func canonicalSourcePassword(state *canonicalImportState) []byte {
	if state.method == wallet.ImportMethodKeystore && state.sourcePasswordFromFile {
		return append([]byte(nil), state.sourcePassword...)
	}
	return []byte(state.value("source_password"))
}

func canonicalBatchFailureReason(err error) string {
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return localization.Get("canonical_err_cancelled")
	case errors.Is(err, errCanonicalBatchSourceRead):
		return localization.Get("canonical_err_source_read")
	case errors.Is(err, errCanonicalBatchValidation):
		return localization.Get("canonical_err_keystore_validation")
	case errors.Is(err, errCanonicalMnemonicValidation):
		return localization.Get("canonical_err_mnemonic_validation")
	case errors.Is(err, wallet.ErrAccountDeleted):
		return wallet.ErrAccountDeleted.Error()
	default:
		return localization.Get("canonical_err_import_failed")
	}
}

func clearCanonicalBatchItems(items []wallet.KeystoreBatchItem) {
	for index := range items {
		clear(items[index].KeystoreJSON)
		clear(items[index].SourcePassword)
	}
}
