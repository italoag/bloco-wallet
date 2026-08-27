package ui

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"blocowallet/internal/constants"
	"blocowallet/internal/wallet"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	canonicalKeystoreLimit       = 1 << 20
	canonicalBatchMethod         = wallet.ImportMethod("keystore_batch")
	canonicalEncryptedMethod     = wallet.ImportMethod("bloco_encrypted")
	canonicalBatchLimit          = 100
	canonicalBatchDirectoryLimit = 300
	canonicalImportTimeout       = 5 * time.Minute
)

type canonicalImportField struct {
	key      string
	label    string
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
	method        wallet.ImportMethod
	fields        []canonicalImportField
	stage         int
	preview       *wallet.ImportPreview
	data          []byte
	batchItems    []wallet.KeystoreBatchItem
	batchPreviews []canonicalBatchPreview
	resultLines   []string
	operationID   uint64
	busy          bool
	cancelling    bool
	cancel        context.CancelFunc
	err           string
}

type canonicalPreviewResultMsg struct {
	operationID   uint64
	preview       *wallet.ImportPreview
	data          []byte
	batchItems    []wallet.KeystoreBatchItem
	batchPreviews []canonicalBatchPreview
	err           error
}

type canonicalCommitResultMsg struct {
	operationID uint64
	summary     wallet.AccountSummary
	resultLines []string
	err         error
}

func newCanonicalImportState(method wallet.ImportMethod) *canonicalImportState {
	state := &canonicalImportState{method: method}
	state.fields = append(state.fields, newCanonicalField("name", "Account name", false, false, 128))
	switch method {
	case wallet.ImportMethodMnemonic:
		state.fields = append(state.fields,
			newCanonicalField("mnemonic", "BIP39 mnemonic (12/15/18/21/24 words)", false, true, 2048),
			newCanonicalField("language", "Language (blank = auto-detect)", true, false, 32),
			newCanonicalField("passphrase", "Optional BIP39 passphrase", true, true, 1024),
			newCanonicalField("path", "EVM derivation path (blank = m/44'/60'/0'/0/0)", true, false, 255),
		)
	case wallet.ImportMethodPrivateKey:
		state.fields = append(state.fields, newCanonicalField("private_key", "Private key", false, true, 128))
	case wallet.ImportMethodKeystore:
		state.fields = append(state.fields,
			newCanonicalField("keystore_path", "Absolute Keystore V3 path", false, false, 1024),
			newCanonicalField("source_password", "Keystore source password (may be empty)", true, true, constants.PasswordCharLimit),
		)
	case canonicalBatchMethod:
		mode := newCanonicalField("password_mode", "Password mode: common or sidecar", false, false, 16)
		mode.input.SetValue("common")
		state.fields = append(state.fields,
			newCanonicalField("directory", "Directory containing Keystore V3 files", false, false, 1024),
			mode,
			newCanonicalField("source_password", "Shared keystore source password (may be empty)", true, true, constants.PasswordCharLimit),
		)
	case canonicalEncryptedMethod:
		state.fields = append(state.fields,
			newCanonicalField("encrypted_path", "Absolute Bloco encrypted export path", false, false, 1024),
			newCanonicalField("source_password", "Encrypted export password", false, true, constants.PasswordCharLimit),
		)
	}
	state.fields = append(state.fields,
		newCanonicalField("storage_password", "New vault storage password", false, true, constants.PasswordCharLimit),
		newCanonicalField("confirm_password", "Confirm vault storage password", false, true, constants.PasswordCharLimit),
	)
	state.fields[0].input.Focus()
	return state
}

func newCanonicalField(key, label string, optional, secret bool, limit int) canonicalImportField {
	input := textinput.New()
	input.Placeholder = label
	input.CharLimit = limit
	input.Width = 80
	if secret {
		input.EchoMode = textinput.EchoPassword
		input.EchoCharacter = '•'
	}
	return canonicalImportField{key: key, label: label, optional: optional, input: input}
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
		m.currentView = constants.ImportMethodSelectionView
		return m, nil
	}
	switch result := msg.(type) {
	case canonicalPreviewResultMsg:
		if result.operationID != state.operationID {
			clear(result.data)
			clearCanonicalBatchItems(result.batchItems)
			return m, nil
		}
		state.busy = false
		state.cancelling = false
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
		state.batchPreviews = result.batchPreviews
		state.err = ""
		return m, nil
	case canonicalCommitResultMsg:
		if result.operationID != state.operationID {
			return m, nil
		}
		state.busy = false
		state.cancelling = false
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
			state.err = field.label + " is required"
			return m, nil
		}
		if state.stage < len(state.fields)-1 {
			field.input.Blur()
			state.stage++
			state.fields[state.stage].input.Focus()
			state.err = ""
			return m, nil
		}
		return m, m.startCanonicalPreview()
	}
	var command tea.Cmd
	state.fields[state.stage].input, command = state.fields[state.stage].input.Update(msg)
	return m, command
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
	return func() tea.Msg {
		err := prepareCanonicalImportPreview(ctx, m.Vault, snapshot)
		if err != nil {
			clear(snapshot.data)
			clearCanonicalBatchItems(snapshot.batchItems)
		}
		return canonicalPreviewResultMsg{
			operationID:   operationID,
			preview:       snapshot.preview,
			data:          snapshot.data,
			batchItems:    snapshot.batchItems,
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
	cloned.batchPreviews = nil
	cloned.resultLines = nil
	cloned.cancel = nil
	cloned.busy = false
	cloned.cancelling = false
	return &cloned
}

func prepareCanonicalImportPreview(ctx context.Context, vault *wallet.WalletVault, state *canonicalImportState) error {
	if err := ctx.Err(); err != nil {
		return err
	}
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
	case wallet.ImportMethodKeystore:
		state.data, err = readCanonicalKeystore(state.value("keystore_path"))
		if err == nil {
			preview, err = wallet.PreviewKeystoreImportContext(ctx, state.data, []byte(state.value("source_password")))
		}
	case canonicalBatchMethod:
		passwordMode := strings.ToLower(strings.TrimSpace(state.value("password_mode")))
		if passwordMode != "common" && passwordMode != "sidecar" {
			err = fmt.Errorf("password mode must be common or sidecar")
			break
		}
		state.batchItems, err = readCanonicalKeystoreBatch(state.value("directory"), strings.TrimSpace(state.value("name")), []byte(state.value("source_password")), passwordMode == "sidecar")
		if err == nil {
			state.batchPreviews = make([]canonicalBatchPreview, 0, len(state.batchItems))
			for _, item := range state.batchItems {
				digest := sha256.Sum256(item.KeystoreJSON)
				itemPreview := canonicalBatchPreview{name: item.Name, digest: hex.EncodeToString(digest[:])}
				if item.PreflightErr != nil {
					itemPreview.err = item.PreflightErr.Error()
					state.batchPreviews = append(state.batchPreviews, itemPreview)
					continue
				}
				validated, previewErr := wallet.PreviewKeystoreImportContext(ctx, item.KeystoreJSON, item.SourcePassword)
				if previewErr != nil {
					itemPreview.err = previewErr.Error()
				} else {
					itemPreview.address = validated.Address
				}
				state.batchPreviews = append(state.batchPreviews, itemPreview)
			}
			preview = wallet.ImportPreview{SecretType: wallet.SecretTypePrivateKey, SourceFormat: fmt.Sprintf("keystore_v3_batch:%d", len(state.batchItems))}
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
	return func() tea.Msg {
		summary, resultLines, err := executeCanonicalImport(ctx, m.Vault, snapshot)
		clear(snapshot.data)
		clearCanonicalBatchItems(snapshot.batchItems)
		return canonicalCommitResultMsg{operationID: operationID, summary: summary, resultLines: resultLines, err: err}
	}
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
			PreflightErr:   item.PreflightErr,
		}
	}
	cloned.batchPreviews = append([]canonicalBatchPreview(nil), state.batchPreviews...)
	cloned.cancel = nil
	cloned.busy = false
	cloned.cancelling = false
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
	case wallet.ImportMethodKeystore:
		sourcePassword := []byte(state.value("source_password"))
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
		})
		failures := 0
		resultLines := make([]string, 0, len(results)+1)
		for _, result := range results {
			name := "batch"
			if result.Index >= 0 && result.Index < len(state.batchItems) {
				name = state.batchItems[result.Index].Name
			}
			if result.Err != nil {
				failures++
				resultLines = append(resultLines, fmt.Sprintf("%s | ERROR: %v", name, result.Err))
			} else if result.AlreadyImported {
				resultLines = append(resultLines, fmt.Sprintf("%s | already imported", name))
			} else if result.Summary != nil {
				resultLines = append(resultLines, fmt.Sprintf("%s | %s | imported", name, result.Summary.Address))
				if summary.AccountID == "" {
					summary = *result.Summary
				}
			}
		}
		resultLines = append(resultLines, fmt.Sprintf("Summary: %d succeeded, %d failed", len(results)-failures, failures))
		return summary, resultLines, nil
	default:
		err = fmt.Errorf("unsupported import method")
	}
	return summary, nil, err
}

func (m *CLIModel) viewCanonicalImport() string {
	state := m.canonicalImport
	if state == nil {
		return "Import state is unavailable"
	}
	title := lipgloss.NewStyle().Bold(true).Render("Canonical wallet import")
	if state.busy {
		if state.cancelling {
			return title + "\n\nCancelling safely. Waiting for in-flight cryptographic work to stop."
		}
		return title + "\n\nProcessing bounded cryptographic work. Press Esc to cancel."
	}
	if len(state.resultLines) > 0 {
		return title + "\n\n" + strings.Join(state.resultLines, "\n") + "\n\nPress Enter to return to the account list."
	}
	if state.preview != nil && state.method == canonicalBatchMethod {
		var view strings.Builder
		view.WriteString(title + "\n\nAuthenticated batch preview:\n")
		for _, item := range state.batchPreviews {
			status := item.address
			if item.err != "" {
				status = "ERROR: " + item.err
			}
			_, _ = fmt.Fprintf(&view, "%s | %s | sha256:%s\n", item.name, status, item.digest[:16])
		}
		view.WriteString("\nPress Enter to commit these exact bytes or Esc to cancel.")
		return view.String()
	}
	if state.preview != nil {
		preview := state.preview
		return fmt.Sprintf("%s\n\nSource: %s\nAddress: %s\nType: %s\nPath: %s\nLanguage: %s\nPassphrase present: %t\n\nPress Enter to commit or Esc to cancel.",
			title, preview.SourceFormat, preview.Address, preview.SecretType, preview.DerivationPath, preview.BIP39Language, preview.HasBIP39Passphrase)
	}
	field := state.fields[state.stage]
	view := fmt.Sprintf("%s\n\nStep %d/%d\n%s:\n%s\n\nPress Enter to continue or Esc to cancel.",
		title, state.stage+1, len(state.fields), field.label, field.input.View())
	if state.err != "" {
		view += "\n\n" + m.styles.ErrorStyle.Render(state.err)
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
	m.canonicalImport.batchPreviews = nil
	m.canonicalImport.preview = nil
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

func readCanonicalKeystoreAt(directory *os.File, name string, expected os.FileInfo) (data []byte, err error) {
	file, err := openFileAtNoFollow(directory, name)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}()
	openedInfo, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !openedInfo.Mode().IsRegular() || !os.SameFile(expected, openedInfo) || openedInfo.Size() > canonicalKeystoreLimit {
		return nil, fmt.Errorf("batch keystore changed while opening")
	}
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

func readCanonicalKeystoreBatch(directory, namePrefix string, sourcePassword []byte, useSidecars bool) (items []wallet.KeystoreBatchItem, err error) {
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
	items = make([]wallet.KeystoreBatchItem, 0)
	for _, entry := range entries {
		lowerName := strings.ToLower(entry.Name())
		if entry.IsDir() || strings.HasSuffix(lowerName, ".password") || strings.HasSuffix(lowerName, ".pwd") {
			continue
		}
		if len(items) >= canonicalBatchLimit {
			clearCanonicalBatchItems(items)
			return nil, fmt.Errorf("batch exceeds %d files", canonicalBatchLimit)
		}
		name := strings.TrimSpace(namePrefix + " " + strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name())))
		item := wallet.KeystoreBatchItem{Name: name}
		entryInfo, infoErr := entry.Info()
		if infoErr != nil {
			item.PreflightErr = infoErr
			items = append(items, item)
			continue
		}
		if entryInfo.Mode()&os.ModeSymlink != 0 || !entryInfo.Mode().IsRegular() {
			item.PreflightErr = fmt.Errorf("batch entry is not a regular file")
			items = append(items, item)
			continue
		}
		item.KeystoreJSON, item.PreflightErr = readCanonicalKeystoreAt(directoryFile, entry.Name(), entryInfo)
		if item.PreflightErr == nil {
			item.SourcePassword = append([]byte(nil), sourcePassword...)
			if useSidecars {
				clear(item.SourcePassword)
				item.SourcePassword, item.PreflightErr = readCanonicalPasswordFile(directoryFile, entry.Name())
			}
		}
		items = append(items, item)
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("batch directory contains no candidate keystore files")
	}
	return items, nil
}

func readCanonicalPasswordFile(directory *os.File, keystoreName string) ([]byte, error) {
	baseName := strings.TrimSuffix(keystoreName, filepath.Ext(keystoreName))
	candidates := []string{
		keystoreName + ".password",
		keystoreName + ".pwd",
		baseName + ".password",
		baseName + ".pwd",
	}
	for _, candidate := range candidates {
		file, err := openFileAtNoFollow(directory, candidate)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		info, statErr := file.Stat()
		if statErr != nil {
			_ = file.Close()
			return nil, statErr
		}
		if !info.Mode().IsRegular() || info.Size() > 4096 {
			_ = file.Close()
			return nil, fmt.Errorf("password sidecar size is outside policy")
		}
		password, readErr := io.ReadAll(io.LimitReader(file, 4097))
		closeErr := file.Close()
		if readErr != nil {
			return nil, readErr
		}
		if closeErr != nil {
			clear(password)
			return nil, closeErr
		}
		if len(password) > 4096 {
			clear(password)
			return nil, fmt.Errorf("password sidecar size is outside policy")
		}
		return password, nil
	}
	return nil, fmt.Errorf("no password sidecar found for %s", keystoreName)
}

func clearCanonicalBatchItems(items []wallet.KeystoreBatchItem) {
	for index := range items {
		clear(items[index].KeystoreJSON)
		clear(items[index].SourcePassword)
	}
}
