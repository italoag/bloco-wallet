package ui

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
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
	"blocowallet/internal/keepass"
	"blocowallet/internal/wallet"
	"blocowallet/pkg/localization"

	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	canonicalKeystoreLimit         = 1 << 20
	canonicalBatchMethod           = wallet.ImportMethod("keystore_batch")
	canonicalMnemonicBatchMethod   = wallet.ImportMethod("mnemonic_batch")
	canonicalPrivateKeyBatchMethod = wallet.ImportMethod("private_key_batch")
	canonicalEncryptedMethod       = wallet.ImportMethod("bloco_encrypted")
	canonicalBatchLimit            = 100
	canonicalBatchDirectoryLimit   = 300
	canonicalImportTimeout         = 5 * time.Minute
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
	secret  []byte
	err     string
	kdbx    bool
}

type canonicalImportState struct {
	method                 wallet.ImportMethod
	fields                 []canonicalImportField
	stage                  int
	preview                *wallet.ImportPreview
	data                   []byte
	batchItems             []wallet.KeystoreBatchItem
	mnemonicItems          []wallet.MnemonicBatchItem
	privateKeyItems        []wallet.PrivateKeyBatchItem
	batchPreviews          []canonicalBatchPreview
	resultLines            []string
	operationID            uint64
	busy                   bool
	cancelling             bool
	quitAfterResult        bool
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
	lookupMissingSidecars  bool
	kdbxSources            map[string]bool
	kdbxSource             bool
}

type canonicalPreviewResultMsg struct {
	operationID     uint64
	preview         *wallet.ImportPreview
	data            []byte
	batchItems      []wallet.KeystoreBatchItem
	mnemonicItems   []wallet.MnemonicBatchItem
	privateKeyItems []wallet.PrivateKeyBatchItem
	batchPreviews   []canonicalBatchPreview
	kdbxSource      bool
	err             error
}

type canonicalCommitResultMsg struct {
	operationID   uint64
	summary       wallet.AccountSummary
	resultLines   []string
	err           error
	backupPending bool
	backupSaved   int
	backupFailed  int
	op            *wallet.CredentialBackupOperation
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
	errCanonicalBatchSourceRead      = errors.New("source file or password sidecar could not be read")
	errCanonicalBatchValidation      = errors.New("keystore validation or decryption failed")
	errCanonicalMnemonicValidation   = errors.New("mnemonic validation failed")
	errCanonicalPrivateKeyValidation = errors.New("private key validation failed")
	errCanonicalImportCancelled      = errors.New("import cancelled")
)

func (state *canonicalImportState) isBatch() bool {
	return state.method == canonicalBatchMethod || state.method == canonicalMnemonicBatchMethod || state.method == canonicalPrivateKeyBatchMethod
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
	case canonicalPrivateKeyBatchMethod:
		state.fields = append(state.fields,
			newCanonicalField("directory", "canonical_label_directory_private_key", false, false, 1024),
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
	m.credentialUseKeePass = false
	m.canonicalImport = newCanonicalImportState(method)
	m.canonicalImport.lookupMissingSidecars = m.credentialBackupEnabled()
	m.canonicalImport.kdbxSources = make(map[string]bool)
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
			clearCanonicalPrivateKeyItems(result.privateKeyItems)
			clearCanonicalBatchPreviews(result.batchPreviews)
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
			clearCanonicalPrivateKeyItems(result.privateKeyItems)
			clearCanonicalBatchPreviews(result.batchPreviews)
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
			if state.quitAfterResult {
				return m, tea.Quit
			}
			return m, nil
		}
		state.preview = result.preview
		state.data = result.data
		state.batchItems = result.batchItems
		state.mnemonicItems = result.mnemonicItems
		state.privateKeyItems = result.privateKeyItems
		state.batchPreviews = result.batchPreviews
		state.kdbxSource = result.kdbxSource
		state.err = ""
		if state.quitAfterResult {
			return m, tea.Quit
		}
		return m, nil
	case canonicalCommitResultMsg:
		if result.operationID != state.operationID {
			m.discardCredentialOperation(result.op)
			return m, nil
		}
		state.busy = false
		state.cancelling = false
		state.events = nil
		if state.cancel != nil {
			state.cancel()
		}
		state.cancel = nil
		m.discardCredentialOperation(result.op)
		if result.err != nil {
			var pendingErr *wallet.CredentialBackupPendingError
			if !errors.As(result.err, &pendingErr) {
				state.err = result.err.Error()
				if state.quitAfterResult {
					return m, tea.Quit
				}
				return m, nil
			}
			result.backupPending = true
		}
		notice := ""
		if result.backupPending {
			notice = localization.Get("keepass_backup_pending_notice")
		}
		if result.backupSaved > 0 || result.backupPending {
			notice = strings.TrimSpace(notice + " " + localization.T("keepass_commit_summary", map[string]interface{}{"Saved": result.backupSaved, "Pending": result.backupFailed}))
			m.lastOperationNotice = notice
		}
		if len(result.resultLines) > 0 {
			state.resultLines = result.resultLines
			m.clearCanonicalImportSecrets(false)
			if result.summary.AccountID != "" {
				m.selectedAccount = &result.summary
			}
			cmd := m.refreshWalletsTable()
			if state.quitAfterResult {
				return m, tea.Sequence(cmd, tea.Quit)
			}
			return m, cmd
		}
		m.clearCanonicalImport()
		m.selectedAccount = &result.summary
		m.currentView = constants.WalletDetailsView
		if state.quitAfterResult {
			return m, tea.Quit
		}
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
			needsOp := m.credentialBackupEnabled() && state.method != wallet.ImportMethodWatchOnly
			return m, m.submitWithCredential(needsOp, func() tea.Cmd { return m.startCanonicalCommit() })
		}
		field := &state.fields[state.stage]
		keepsSource := field.key == "source_password" && m.credentialUseKeePass && m.credentialBackupEnabled()
		if !field.optional && field.input.Value() == "" && !keepsSource {
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
		needsOp := m.credentialBackupEnabled() && state.method != wallet.ImportMethodWatchOnly
		return m, m.submitWithCredential(needsOp, func() tea.Cmd { return m.startCanonicalPreview() })
	}
	if state.preview != nil {
		return m, nil
	}
	if key, ok := msg.(tea.KeyMsg); ok && key.String() == "ctrl+k" && state.stage < len(state.fields) && m.credentialBackupEnabled() {
		switch state.fields[state.stage].key {
		case "source_password":
			m.credentialUseKeePass = !m.credentialUseKeePass
			if m.credentialUseKeePass {
				state.fields[state.stage].input.SetValue("")
			}
			return m, nil
		case "directory":
			if state.method == canonicalBatchMethod {
				state.lookupMissingSidecars = !state.lookupMissingSidecars
				return m, nil
			}
		}
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
	op := m.activeCredentialOperation()
	parent := context.Background()
	if op != nil {
		parent = op.Context()
	}
	ctx, cancel := context.WithTimeout(parent, canonicalImportTimeout)
	state.cancel = cancel
	state.busy = true
	state.cancelling = false
	state.err = ""
	snapshot := cloneCanonicalImportState(state)
	useKeePass := m.credentialUseKeePass && op != nil
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
			err := prepareCanonicalImportPreview(ctx, m.Vault, snapshot, op, useKeePass)
			if err != nil {
				clear(snapshot.data)
				clearCanonicalBatchItems(snapshot.batchItems)
				clearCanonicalMnemonicItems(snapshot.mnemonicItems)
				clearCanonicalPrivateKeyItems(snapshot.privateKeyItems)
				clearCanonicalBatchPreviews(snapshot.batchPreviews)
			}
			events <- canonicalPreviewResultMsg{
				operationID:     operationID,
				preview:         snapshot.preview,
				data:            snapshot.data,
				batchItems:      snapshot.batchItems,
				mnemonicItems:   snapshot.mnemonicItems,
				privateKeyItems: snapshot.privateKeyItems,
				batchPreviews:   snapshot.batchPreviews,
				kdbxSource:      snapshot.kdbxSource,
				err:             err,
			}
			return nil
		}
		return tea.Batch(worker, waitCanonicalImportEvent(events))
	}
	return func() tea.Msg {
		defer cancel()
		defer clear(snapshot.sourcePassword)
		err := prepareCanonicalImportPreview(ctx, m.Vault, snapshot, op, useKeePass)
		if err != nil {
			clear(snapshot.data)
			clearCanonicalBatchItems(snapshot.batchItems)
			clearCanonicalMnemonicItems(snapshot.mnemonicItems)
			clearCanonicalPrivateKeyItems(snapshot.privateKeyItems)
			clearCanonicalBatchPreviews(snapshot.batchPreviews)
		}
		return canonicalPreviewResultMsg{
			operationID:     operationID,
			preview:         snapshot.preview,
			data:            snapshot.data,
			batchItems:      snapshot.batchItems,
			mnemonicItems:   snapshot.mnemonicItems,
			privateKeyItems: snapshot.privateKeyItems,
			batchPreviews:   snapshot.batchPreviews,
			kdbxSource:      snapshot.kdbxSource,
			err:             err,
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
	cloned.privateKeyItems = nil
	cloned.batchPreviews = nil
	cloned.resultLines = nil
	cloned.cancel = nil
	cloned.busy = false
	cloned.cancelling = false
	cloned.events = nil
	cloned.sourcePassword = append([]byte(nil), state.sourcePassword...)
	cloned.lookupMissingSidecars = state.lookupMissingSidecars
	cloned.kdbxSources = make(map[string]bool)
	cloned.kdbxSource = false
	return &cloned
}

func prepareCanonicalImportPreview(ctx context.Context, vault *wallet.WalletVault, state *canonicalImportState, credential *wallet.CredentialBackupOperation, useKeePass bool) error {
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
			if useKeePass && !state.sourcePasswordFromFile {
				err = credential.WithFilePassword(ctx, "keystore_v3", state.data, func(resolved []byte) error {
					var inner error
					preview, inner = wallet.PreviewKeystoreImportContext(ctx, state.data, resolved)
					return inner
				})
				if err == nil {
					state.kdbxSource = true
				}
			} else {
				sourcePassword := canonicalSourcePassword(state)
				preview, err = wallet.PreviewKeystoreImportContext(ctx, state.data, sourcePassword)
				clear(sourcePassword)
			}
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
				if !item.SourcePasswordFromFile && state.lookupMissingSidecars && credential != nil {
					lookupErr := credential.WithFilePassword(ctx, "keystore_v3", item.KeystoreJSON, func(resolved []byte) error {
						validated, inner := wallet.PreviewKeystoreImportContext(ctx, item.KeystoreJSON, resolved)
						if inner == nil {
							itemPreview.address = validated.Address
						}
						return inner
					})
					switch {
					case lookupErr == nil:
						itemPreview.kdbx = true
						state.kdbxSources[itemPreview.digest] = true
						state.batchPreviews = append(state.batchPreviews, itemPreview)
						batchProgress.Completed++
						if state.reportProgress != nil {
							state.reportProgress(batchProgress)
						}
						continue
					case errors.Is(lookupErr, keepass.ErrNotFound):
					default:
						item.PreflightErr = lookupErr
						itemPreview.err = canonicalBatchFailureReason(lookupErr)
						state.batchPreviews = append(state.batchPreviews, itemPreview)
						batchProgress.Completed++
						batchProgress.Failed++
						if state.reportProgress != nil {
							state.reportProgress(batchProgress)
						}
						continue
					}
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
				itemPreview := canonicalBatchPreview{name: item.Name, secret: append([]byte(nil), item.Mnemonic...)}
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
	case canonicalPrivateKeyBatchMethod:
		state.privateKeyItems, err = readCanonicalPrivateKeyBatch(state.value("directory"))
		if err == nil {
			err = assignCanonicalBatchNames(ctx, vault, state)
		}
		if err == nil {
			if state.reportProgress != nil {
				state.reportProgress(wallet.KeystoreBatchProgress{Total: len(state.privateKeyItems)})
			}
			batchProgress := wallet.KeystoreBatchProgress{Total: len(state.privateKeyItems)}
			state.batchPreviews = make([]canonicalBatchPreview, 0, len(state.privateKeyItems))
			for index := range state.privateKeyItems {
				item := &state.privateKeyItems[index]
				itemPreview := canonicalBatchPreview{name: item.Name, secret: append([]byte(nil), item.PrivateKey...)}
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
				validated, previewErr := wallet.PreviewPrivateKeyFileImport(item.PrivateKey)
				if previewErr != nil {
					item.PreflightErr = fmt.Errorf("%w", errCanonicalPrivateKeyValidation)
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
				preview = wallet.ImportPreview{SecretType: wallet.SecretTypePrivateKey, SourceFormat: fmt.Sprintf("private_key_batch:%d", len(state.privateKeyItems))}
			}
		}
	case canonicalEncryptedMethod:
		state.data, err = readCanonicalKeystore(state.value("encrypted_path"))
		if err == nil {
			if useKeePass {
				err = credential.WithFilePassword(ctx, "bloco_encrypted", state.data, func(resolved []byte) error {
					var inner error
					preview, inner = vault.PreviewEncryptedAccountImport(ctx, state.data, resolved)
					return inner
				})
				if err == nil {
					state.kdbxSource = true
				}
			} else {
				sourcePassword := []byte(state.value("source_password"))
				preview, err = vault.PreviewEncryptedAccountImport(ctx, state.data, sourcePassword)
				clear(sourcePassword)
			}
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
		clearCanonicalPrivateKeyItems(state.privateKeyItems)
		state.privateKeyItems = nil
		clearCanonicalBatchPreviews(state.batchPreviews)
		state.batchPreviews = nil
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
	op := m.activeCredentialOperation()
	parent := context.Background()
	if op != nil {
		parent = op.Context()
	}
	ctx, cancel := context.WithTimeout(parent, canonicalImportTimeout)
	state.cancel = cancel
	state.busy = true
	state.cancelling = false
	state.err = ""
	snapshot := cloneCanonicalCommitState(state)
	useKeePass := m.credentialUseKeePass && m.credentialBackupEnabled()
	syncBackup := func(err error) (saved, failed int, pending bool) {
		if op == nil {
			return 0, 0, false
		}
		if err != nil {
			var pendingErr *wallet.CredentialBackupPendingError
			if !errors.As(err, &pendingErr) {
				return 0, 0, false
			}
			pending = true
		}
		report, syncErr := op.Sync(ctx)
		saved = report.AccountsSaved + report.FilesSaved
		failed = report.Pending
		return saved, failed, pending || syncErr != nil || report.Pending > 0
	}
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
			summary, resultLines, err := executeCanonicalImport(ctx, m.Vault, snapshot, op, useKeePass)
			clear(snapshot.data)
			clearCanonicalBatchItems(snapshot.batchItems)
			clearCanonicalMnemonicItems(snapshot.mnemonicItems)
			clearCanonicalPrivateKeyItems(snapshot.privateKeyItems)
			clearCanonicalBatchPreviews(snapshot.batchPreviews)
			var pendingErr *wallet.CredentialBackupPendingError
			if op != nil && (err == nil || errors.As(err, &pendingErr)) {
				events <- canonicalImportProgressMsg{operationID: operationID, stage: "keepass_stage_syncing"}
			}
			saved, failed, pending := syncBackup(err)
			events <- canonicalCommitResultMsg{operationID: operationID, summary: summary, resultLines: resultLines, err: err, backupPending: pending, backupSaved: saved, backupFailed: failed, op: op}
			return nil
		}
		return tea.Batch(worker, waitCanonicalImportEvent(events))
	}
	return func() tea.Msg {
		defer cancel()
		defer clear(snapshot.sourcePassword)
		summary, resultLines, err := executeCanonicalImport(ctx, m.Vault, snapshot, op, useKeePass)
		clear(snapshot.data)
		clearCanonicalBatchItems(snapshot.batchItems)
		clearCanonicalMnemonicItems(snapshot.mnemonicItems)
		clearCanonicalPrivateKeyItems(snapshot.privateKeyItems)
		clearCanonicalBatchPreviews(snapshot.batchPreviews)
		saved, failed, pending := syncBackup(err)
		return canonicalCommitResultMsg{operationID: operationID, summary: summary, resultLines: resultLines, err: err, backupPending: pending, backupSaved: saved, backupFailed: failed, op: op}
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
			Name:                   item.Name,
			KeystoreJSON:           append([]byte(nil), item.KeystoreJSON...),
			SourcePassword:         append([]byte(nil), item.SourcePassword...),
			SourcePasswordFromFile: item.SourcePasswordFromFile,
			SourcePath:             item.SourcePath,
			PreflightErr:           item.PreflightErr,
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
	cloned.privateKeyItems = make([]wallet.PrivateKeyBatchItem, len(state.privateKeyItems))
	for index, item := range state.privateKeyItems {
		cloned.privateKeyItems[index] = wallet.PrivateKeyBatchItem{
			Name:         item.Name,
			SourcePath:   item.SourcePath,
			PrivateKey:   append([]byte(nil), item.PrivateKey...),
			PreflightErr: item.PreflightErr,
		}
	}
	cloned.batchPreviews = append([]canonicalBatchPreview(nil), state.batchPreviews...)
	cloned.lookupMissingSidecars = state.lookupMissingSidecars
	cloned.kdbxSources = state.kdbxSources
	cloned.cancel = nil
	cloned.busy = false
	cloned.cancelling = false
	cloned.events = nil
	cloned.sourcePassword = append([]byte(nil), state.sourcePassword...)
	return &cloned
}

func executeCanonicalImport(ctx context.Context, vault *wallet.WalletVault, state *canonicalImportState, credential *wallet.CredentialBackupOperation, useKeePass bool) (wallet.AccountSummary, []string, error) {
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
		importKeystore := func(sourcePassword []byte) error {
			var inner error
			summary, inner = vault.ImportKeystore(ctx, wallet.KeystoreImportRequest{
				Name:                   strings.TrimSpace(state.value("name")),
				KeystoreJSON:           state.data,
				SourcePassword:         sourcePassword,
				SourcePath:             state.value("keystore_path"),
				StoragePassword:        storagePassword,
				ConfirmStoragePassword: confirmation,
			})
			return inner
		}
		if useKeePass && credential != nil && !state.sourcePasswordFromFile {
			err = credential.WithFilePassword(ctx, "keystore_v3", state.data, importKeystore)
		} else {
			sourcePassword := canonicalSourcePassword(state)
			err = importKeystore(sourcePassword)
			clear(sourcePassword)
		}
	case canonicalEncryptedMethod:
		importEncrypted := func(sourcePassword []byte) error {
			var inner error
			summary, inner = vault.ImportEncryptedAccount(ctx, wallet.EncryptedAccountImportRequest{
				Name:                   strings.TrimSpace(state.value("name")),
				ExportJSON:             state.data,
				ExportPassword:         sourcePassword,
				SourcePath:             state.value("encrypted_path"),
				StoragePassword:        storagePassword,
				ConfirmStoragePassword: confirmation,
			})
			return inner
		}
		if useKeePass && credential != nil {
			err = credential.WithFilePassword(ctx, "bloco_encrypted", state.data, importEncrypted)
		} else {
			sourcePassword := []byte(state.value("source_password"))
			err = importEncrypted(sourcePassword)
			clear(sourcePassword)
		}
	case canonicalBatchMethod:
		for index, item := range state.batchItems {
			digest := sha256.Sum256(item.KeystoreJSON)
			if index >= len(state.batchPreviews) || hex.EncodeToString(digest[:]) != state.batchPreviews[index].digest {
				return wallet.AccountSummary{}, nil, fmt.Errorf("batch item changed after preview")
			}
		}
		if credential != nil && state.lookupMissingSidecars {
			for index := range state.batchItems {
				item := &state.batchItems[index]
				if item.PreflightErr != nil || item.SourcePasswordFromFile {
					continue
				}
				lookupErr := credential.WithFilePassword(ctx, "keystore_v3", item.KeystoreJSON, func(resolved []byte) error {
					item.SourcePassword = append([]byte(nil), resolved...)
					return nil
				})
				if lookupErr != nil && !errors.Is(lookupErr, keepass.ErrNotFound) {
					item.PreflightErr = lookupErr
				}
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
			if index >= len(state.batchPreviews) || subtle.ConstantTimeCompare(item.Mnemonic, state.batchPreviews[index].secret) != 1 {
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
	case canonicalPrivateKeyBatchMethod:
		for index, item := range state.privateKeyItems {
			if index >= len(state.batchPreviews) || subtle.ConstantTimeCompare(item.PrivateKey, state.batchPreviews[index].secret) != 1 {
				return wallet.AccountSummary{}, nil, fmt.Errorf("batch item changed after preview")
			}
		}
		results := vault.ImportPrivateKeyBatch(ctx, wallet.PrivateKeyBatchImportRequest{
			Items:                  state.privateKeyItems,
			StoragePassword:        storagePassword,
			ConfirmStoragePassword: confirmation,
			MaxConcurrency:         2,
			OnProgress:             state.reportProgress,
		})
		commitSummary, resultLines := canonicalBatchResultLines(state.logDirectory, "private-key-import-failures-*.log", canonicalBatchFileRefsFromPrivateKeys(state.privateKeyItems), results)
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
			if item.kdbx {
				status = status + " " + localization.Get("keepass_source_kdbx")
			}
			if state.method == canonicalMnemonicBatchMethod || state.method == canonicalPrivateKeyBatchMethod {
				_, _ = fmt.Fprintf(&view, "%s | %s\n", safeShort(item.name), safeInline(status))
			} else {
				_, _ = fmt.Fprintf(&view, "%s | %s | sha256:%s\n", safeShort(item.name), safeInline(status), safeShort(item.digest[:min(16, len(item.digest))]))
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
		sourceLine := ""
		if state.kdbxSource {
			sourceLine = "\n" + localization.Get("keepass_source_kdbx")
		}
		return fmt.Sprintf("%s\n\n%s%s",
			title, localization.T("canonical_preview_summary", map[string]interface{}{
				"Source":     safeShort(preview.SourceFormat),
				"Address":    safeShort(preview.Address),
				"Signer":     safeShort(string(preview.SignerKind)),
				"Secret":     safeShort(secretType),
				"Path":       safeShort(preview.DerivationPath),
				"Language":   safeShort(string(preview.BIP39Language)),
				"Passphrase": passphrasePresent,
			}), sourceLine)
	}
	field := state.fields[state.stage]
	view := fmt.Sprintf("%s\n\n%s\n%s:\n%s",
		title,
		localization.T("canonical_step", map[string]interface{}{"Step": state.stage + 1, "Total": len(state.fields)}),
		localization.Get(field.labelKey), field.input.View())
	if field.key == "source_password" && m.credentialBackupEnabled() {
		view += "\n" + m.credentialMethodLabel(true)
	}
	if field.key == "directory" && state.method == canonicalBatchMethod && m.credentialBackupEnabled() {
		label := localization.Get("keepass_status_disabled")
		if state.lookupMissingSidecars {
			label = localization.Get("keepass_status_enabled")
		}
		view += "\n" + localization.T("keepass_lookup_label", map[string]interface{}{"Status": label})
	}
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
	clearCanonicalPrivateKeyItems(m.canonicalImport.privateKeyItems)
	m.canonicalImport.privateKeyItems = nil
	clearCanonicalBatchPreviews(m.canonicalImport.batchPreviews)
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
	m.credentialUseKeePass = false
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
			item.SourcePasswordFromFile = true
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
	case errors.Is(err, errCanonicalPrivateKeyValidation):
		return localization.Get("canonical_err_private_key_validation")
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

// clearCanonicalBatchPreviews wipes the approved secret snapshots stored for
// preview-to-commit binding on mnemonic/private-key batch items.
func clearCanonicalBatchPreviews(previews []canonicalBatchPreview) {
	for index := range previews {
		clear(previews[index].secret)
	}
}
