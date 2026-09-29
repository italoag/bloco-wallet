package ui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"blocowallet/internal/constants"
	"blocowallet/internal/evm"
	"blocowallet/internal/wallet"
	"blocowallet/pkg/config"
	"blocowallet/pkg/localization"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const gatesMaster = "test master password 1234"

func addGateNetwork(model *CLIModel) {
	if model.currentConfig == nil {
		model.currentConfig = &config.Config{}
	}
	if model.currentConfig.Networks == nil {
		model.currentConfig.Networks = map[string]config.Network{}
	}
	model.currentConfig.Networks["gate-net"] = config.Network{
		Name: "GateNet", ChainID: 1, IsActive: true,
		NativeDecimals: 18, NativeDecimalsSet: true,
		RPCEndpoint: "http://127.0.0.1:1",
	}
}

type gateEngineStub struct {
	broadcasts int
}

func (engine *gateEngineStub) PrepareNative(context.Context, evm.PrepareNativeRequest) (*evm.PreparedNativeTransfer, error) {
	return &evm.PreparedNativeTransfer{}, nil
}
func (engine *gateEngineStub) PrepareERC20Transfer(context.Context, evm.PrepareERC20TransferRequest) (*evm.PreparedNativeTransfer, error) {
	return &evm.PreparedNativeTransfer{}, nil
}
func (engine *gateEngineStub) PrepareERC20Approve(context.Context, evm.PrepareERC20ApproveRequest) (*evm.PreparedNativeTransfer, error) {
	return &evm.PreparedNativeTransfer{}, nil
}
func (engine *gateEngineStub) PrepareERC721SafeTransfer(context.Context, evm.PrepareERC721SafeTransferRequest) (*evm.PreparedNativeTransfer, error) {
	return &evm.PreparedNativeTransfer{}, nil
}
func (engine *gateEngineStub) PrepareERC1155SafeTransfer(context.Context, evm.PrepareERC1155SafeTransferRequest) (*evm.PreparedNativeTransfer, error) {
	return &evm.PreparedNativeTransfer{}, nil
}
func (engine *gateEngineStub) PrepareERC1155BatchTransfer(context.Context, evm.PrepareERC1155BatchTransferRequest) (*evm.PreparedNativeTransfer, error) {
	return &evm.PreparedNativeTransfer{}, nil
}
func (engine *gateEngineStub) PrepareContractCall(context.Context, evm.PrepareContractCallRequest) (*evm.PreparedNativeTransfer, error) {
	return &evm.PreparedNativeTransfer{}, nil
}
func (engine *gateEngineStub) ApproveSignAndBroadcast(context.Context, wallet.CapabilityHandle, *evm.PreparedNativeTransfer, evm.ApprovalRequest) (evm.ExecutionResult, error) {
	engine.broadcasts++
	return evm.ExecutionResult{TransactionID: "0xgate"}, nil
}
func (engine *gateEngineStub) TrackTransaction(context.Context, string, uint64, time.Time) (evm.TrackingResult, error) {
	return evm.TrackingResult{State: evm.TransactionConfirmed}, nil
}
func (engine *gateEngineStub) CancelPrepared(context.Context, *evm.PreparedNativeTransfer, string) error {
	return nil
}
func (engine *gateEngineStub) Rebroadcast(context.Context, string) (evm.ExecutionResult, error) {
	return evm.ExecutionResult{TransactionID: "0xgate"}, nil
}

// blurringAuthorizer cancels the UI operation mid-authorization to prove the
// worker context is derived from the credential operation context.
type blurringAuthorizer struct {
	model     *CLIModel
	ctx       context.Context
	accountID string
	password  []byte
	calls     int
}

func (spy *blurringAuthorizer) Authorize(ctx context.Context, accountID string, password []byte, operation wallet.TransactionAuthorizationOperation) error {
	spy.calls++
	spy.ctx = ctx
	spy.accountID = accountID
	spy.password = append([]byte(nil), password...)
	_, _ = spy.model.Update(tea.BlurMsg{})
	<-ctx.Done()
	return ctx.Err()
}

func (spy *blurringAuthorizer) HasActiveSession(context.Context, string) bool { return false }

type spySafeService struct {
	passwords   [][]byte
	accountIDs  []string
	deployCalls int
	signCalls   int
	execCalls   int
}

func (spy *spySafeService) ListNetworks(context.Context) ([]SafeNetwork, error) {
	return []SafeNetwork{{ChainID: 1, Name: "gate"}}, nil
}
func (spy *spySafeService) ListSafeAccounts(context.Context) ([]SafeAccountSummary, error) {
	return nil, nil
}
func (spy *spySafeService) SummarizeSafe(context.Context, string, uint64) (*SafeSummary, error) {
	return &SafeSummary{}, nil
}
func (spy *spySafeService) ListProposals(context.Context, string, uint64, int) ([]SafeProposalSummary, error) {
	return nil, nil
}
func (spy *spySafeService) ListOwnerAccounts(context.Context) ([]wallet.Account, error) {
	return nil, nil
}
func (spy *spySafeService) GetProposalOwners(context.Context, string) ([]common.Address, error) {
	return nil, nil
}
func (spy *spySafeService) ImportSafe(context.Context, string, string, uint64) error { return nil }
func (spy *spySafeService) PrepareDeploy(context.Context, uint64, string, []string, uint64) (*SafeDeploymentSummary, error) {
	return &SafeDeploymentSummary{}, nil
}
func (spy *spySafeService) BroadcastDeploy(_ context.Context, _ *SafeDeploymentSummary, accountID string, password []byte) (string, error) {
	spy.deployCalls++
	spy.accountIDs = append(spy.accountIDs, accountID)
	spy.passwords = append(spy.passwords, append([]byte(nil), password...))
	return "0xdeploy", nil
}
func (spy *spySafeService) Propose(context.Context, string, uint64, string, *big.Int, []byte) (string, error) {
	return "p1", nil
}
func (spy *spySafeService) Sign(_ context.Context, _ string, ownerAccountID string, _ uint64, password []byte) error {
	spy.signCalls++
	spy.accountIDs = append(spy.accountIDs, ownerAccountID)
	spy.passwords = append(spy.passwords, append([]byte(nil), password...))
	return nil
}
func (spy *spySafeService) Execute(_ context.Context, _ string, gasPayerAccountID string, _ uint64, password []byte) (string, error) {
	spy.execCalls++
	spy.accountIDs = append(spy.accountIDs, gasPayerAccountID)
	spy.passwords = append(spy.passwords, append([]byte(nil), password...))
	return "0xexec", nil
}

func cmdResultMsgs(t *testing.T, cmd tea.Cmd) []tea.Msg {
	t.Helper()
	var out []tea.Msg
	var walk func(c tea.Cmd, depth int)
	walk = func(c tea.Cmd, depth int) {
		if c == nil {
			return
		}
		if depth > 64 {
			t.Fatal("cmdResultMsgs exceeded maximum depth")
		}
		msg := c()
		if msg == nil {
			return
		}
		if rv := reflect.ValueOf(msg); rv.IsValid() && rv.Kind() == reflect.Slice && rv.Type().Elem().Kind() == reflect.Func {
			for i := 0; i < rv.Len(); i++ {
				if inner, ok := rv.Index(i).Interface().(tea.Cmd); ok {
					walk(inner, depth+1)
				}
			}
			return
		}
		out = append(out, msg)
	}
	walk(cmd, 0)
	return out
}

func feedCmdResult(model *CLIModel, cmd tea.Cmd, depth int) {
	feedMsgsNoFollowUp(model, cmd, depth)
}

func feedMsgsNoFollowUp(model *CLIModel, cmd tea.Cmd, depth int) {
	if cmd == nil {
		return
	}
	if depth > 1024 {
		panic("feedMsgsNoFollowUp exceeded maximum depth")
	}
	msg := cmd()
	if msg == nil {
		return
	}
	if rv := reflect.ValueOf(msg); rv.IsValid() && rv.Kind() == reflect.Slice && rv.Type().Elem().Kind() == reflect.Func {
		for i := 0; i < rv.Len(); i++ {
			if inner, ok := rv.Index(i).Interface().(tea.Cmd); ok {
				feedMsgsNoFollowUp(model, inner, depth+1)
			}
		}
		return
	}
	_, _ = model.Update(msg)
}

func unlockRecoveryWithoutTick(t *testing.T, model *CLIModel, master string) {
	t.Helper()
	require.NotNil(t, model.credentialPrompt)
	typeRunesViaUpdate(model, master)
	_, openCmd := model.Update(keyMsg("enter"))
	require.NotNil(t, openCmd)
	opened, ok := openCmd().(credentialOpenedMsg)
	require.True(t, ok)
	require.NoError(t, opened.err)
	_, workerCmd := model.handleCredentialOpened(opened)
	feedMsgsNoFollowUp(model, workerCmd, 0)
	require.Nil(t, model.credentialPrompt)
}

func gateSoftwareOwner(summary wallet.AccountSummary) wallet.Account {
	return wallet.Account{
		AccountID:    summary.AccountID,
		Address:      summary.Address,
		SignerKind:   wallet.SignerKindSoftware,
		Capabilities: wallet.CapabilitySignTransaction | wallet.CapabilityExportSecret,
	}
}

func TestKeePassUIPromptRendersOverRecoveryView(t *testing.T) {
	model, _, _, _ := newCredentialUITestModel(t, true)
	summary := createActiveAccount(t, model, "overlay-recovery", "overlay storage pw 2")
	model.selectedAccount = &summary
	model.initRecovery()
	require.NotNil(t, model.recovery)
	pressKey(model, "enter")
	require.NotNil(t, model.recovery)
	pressKey(model, "ctrl+k")
	require.True(t, model.credentialUseKeePass)
	pressKey(model, "enter")
	model.recovery.confirmation.SetValue("REVEAL")
	pressKey(model, "enter")
	require.NotNil(t, model.credentialPrompt)
	model.width = 100
	model.height = 30
	view := model.View()
	assert.Contains(t, view, localization.Get("keepass_master_title"))
	assert.NotContains(t, view, "overlay storage pw 2")
}

func TestKeePassUIPromptDispatchesBlurQuitAndClock(t *testing.T) {
	model, _, _, _ := newCredentialUITestModel(t, true)
	model.requestCredentialOperation(nil)
	require.NotNil(t, model.credentialPrompt)

	_, cmd := model.Update(clockTickMsg(time.Now()))
	assert.NotNil(t, cmd, "clock tick must be rescheduled while the prompt is open")
	assert.NotNil(t, model.credentialPrompt)

	_, _ = model.Update(tea.WindowSizeMsg{Width: 96, Height: 32})
	assert.Equal(t, 96, model.width)
	assert.NotNil(t, model.credentialPrompt)

	_, _ = model.Update(tea.BlurMsg{})
	assert.Nil(t, model.credentialPrompt)
	assert.Nil(t, model.credentialOperation)

	model.requestCredentialOperation(nil)
	require.NotNil(t, model.credentialPrompt)
	_, quit := model.Update(keyMsg("ctrl+q"))
	assert.Nil(t, model.credentialPrompt)
	require.NotNil(t, quit, "ctrl+q must still reach the quit path while the prompt is open")
	assert.IsType(t, tea.QuitMsg{}, quit())
}

func TestKeePassUICancelPropagatesToWorker(t *testing.T) {
	model, _, _, _ := newCredentialUITestModel(t, true)
	summary := createActiveAccount(t, model, "cancelled-signer", "cancel storage pw 7")
	spy := &blurringAuthorizer{model: model}
	model.transactionAuthorizer = spy
	model.selectedAccount = &summary
	signer := common.HexToAddress(summary.Address)
	model.ConfigureMessageSigningFactory(func(context.Context) (MessageSigningService, error) {
		return &personalSignServiceStub{signer: signer}, nil
	})
	service, err := model.messageSigningFactory(context.Background())
	require.NoError(t, err)
	model.initPersonalSign(service)

	model.personalSign.message.SetValue("hello")
	pressKey(model, "n")
	pressKey(model, "a")
	require.Equal(t, personalSignPassword, model.personalSign.phase)
	pressKey(model, "ctrl+k")
	pressKey(model, "enter")
	require.NotNil(t, model.credentialPrompt)
	typeRunesViaUpdate(model, gatesMaster)
	driveCmds(model, pressKey(model, "enter"))

	require.Equal(t, 1, spy.calls)
	assert.Equal(t, summary.AccountID, spy.accountID)
	require.NotNil(t, spy.ctx)
	assert.Error(t, spy.ctx.Err(), "blur during authorization must cancel the worker context")
	assert.Nil(t, model.credentialOperation)

	model.initPersonalSign(service)
	model.personalSign.message.SetValue("hello again")
	pressKey(model, "n")
	pressKey(model, "a")
	require.Equal(t, personalSignPassword, model.personalSign.phase)
	pressKey(model, "ctrl+k")
	pressKey(model, "enter")
	require.NotNil(t, model.credentialPrompt)
	pressKey(model, "esc")
	assert.Nil(t, model.credentialPrompt)
	assert.Equal(t, 1, spy.calls, "cancelled prompt must not run the submission")

	model.initKeePassSettings()
	settingsState := model.keepassSettings
	_, listCmd := model.runKeePassMenuAction("pending")
	require.NotNil(t, listCmd)
	require.True(t, settingsState.busy)
	settingsCancel := settingsState.cancel
	require.NotNil(t, settingsCancel)
	pressKey(model, "esc")
	assert.True(t, settingsState.busy, "Esc cancels the context but the view must keep waiting for the result")
	assert.True(t, settingsState.cancelling)
	feedMsgsNoFollowUp(model, listCmd, 0)
	assert.False(t, settingsState.busy)
	assert.Nil(t, settingsState.cancel)

	model.initKeePassAccount(summary)
	statusCmd := model.loadKeePassAccountStatus()
	require.NotNil(t, statusCmd)
	require.True(t, model.keepassAccount.busy)
	accountCancel := model.keepassAccount.cancel
	cancelCalled := false
	model.keepassAccount.cancel = func() {
		cancelCalled = true
		accountCancel()
	}
	pressKey(model, "esc")
	assert.True(t, cancelCalled, "Esc on a busy KeePass account view must cancel the in-flight worker")
	assert.True(t, model.keepassAccount.busy)
	feedMsgsNoFollowUp(model, statusCmd, 0)
	assert.False(t, model.keepassAccount.busy)
}

func TestKeePassUICreateConfirmBackup(t *testing.T) {
	model, vault, _, _ := newCredentialUITestModel(t, true)
	model.initCreateWallet()
	model.nameInput.SetValue("gate-created")
	pressKey(model, "enter")
	require.Equal(t, constants.CreateWalletOptionsView, model.currentView)
	pressKey(model, "enter")
	pressKey(model, "enter")
	pressKey(model, "enter")
	pressKey(model, "enter")
	require.Equal(t, constants.CreateWalletView, model.currentView)
	model.passwordInput.SetValue("create storage pw 9")
	pressKey(model, "enter")
	require.Equal(t, 1, model.createPasswordStage)
	model.createPasswordConfirmationInput.SetValue("create storage pw 9")
	pressKey(model, "enter")
	require.NotNil(t, model.credentialPrompt)
	unlockMasterViaUpdate(t, model, gatesMaster)

	require.Equal(t, constants.CreateWalletBackupView, model.currentView)
	require.NotNil(t, model.backupChallenge)
	challenge := model.backupChallenge
	answers := make([]string, 0, len(challenge.RequiredWordIndices))
	for _, index := range challenge.RequiredWordIndices {
		answers = append(answers, challenge.Words[index])
	}
	model.backupConfirmationInput.SetValue(strings.Join(answers, " "))
	driveCmds(model, pressKey(model, "enter"))

	assert.Equal(t, constants.WalletDetailsView, model.currentView)
	require.NotNil(t, model.selectedAccount)
	assert.Equal(t, "gate-created", model.selectedAccount.Name)
	assert.Nil(t, model.backupChallenge)
	pending, err := model.credentialService.Pending(context.Background())
	require.NoError(t, err)
	assert.Empty(t, pending)

	accounts, err := vault.ListAccounts(context.Background())
	require.NoError(t, err)
	found := false
	for _, account := range accounts {
		if account.Name == "gate-created" {
			found = true
			assert.Equal(t, wallet.AccountStateActive, account.State)
		}
	}
	assert.True(t, found)
}

func TestKeePassUIBlurSuspendsAndResumesCreation(t *testing.T) {
	model, vault, _, _ := newCredentialUITestModel(t, true)
	model.initCreateWallet()
	model.nameInput.SetValue("suspended-create")
	pressKey(model, "enter")
	pressKey(model, "enter")
	pressKey(model, "enter")
	pressKey(model, "enter")
	pressKey(model, "enter")
	require.Equal(t, constants.CreateWalletView, model.currentView)
	model.passwordInput.SetValue("first storage pw 1")
	pressKey(model, "enter")
	model.createPasswordConfirmationInput.SetValue("first storage pw 1")
	pressKey(model, "enter")
	require.NotNil(t, model.credentialPrompt)

	typeRunesViaUpdate(model, gatesMaster)
	_, openCmd := model.Update(keyMsg("enter"))
	require.NotNil(t, openCmd)
	opened, ok := openCmd().(credentialOpenedMsg)
	require.True(t, ok)
	require.NoError(t, opened.err)
	_, workerCmd := model.handleCredentialOpened(opened)
	require.NotNil(t, workerCmd)
	require.True(t, model.vaultBusy)

	resultMsg, ok := workerCmd().(vaultCreateResultMsg)
	require.True(t, ok)
	_, _ = model.Update(tea.BlurMsg{})
	_, _ = model.Update(resultMsg)

	assert.Equal(t, constants.ListWalletsView, model.currentView)
	assert.Nil(t, model.backupChallenge)
	assert.NotEmpty(t, model.resumeBackupAccountID)
	accountID := model.resumeBackupAccountID

	model.initResumeBackup(accountID)
	model.passwordInput.SetValue("first storage pw 1")
	pressKey(model, "enter")
	require.NotNil(t, model.credentialPrompt)
	unlockMasterViaUpdate(t, model, gatesMaster)
	require.Equal(t, constants.CreateWalletBackupView, model.currentView)
	require.NotNil(t, model.backupChallenge)
	answers := make([]string, 0, len(model.backupChallenge.RequiredWordIndices))
	for _, index := range model.backupChallenge.RequiredWordIndices {
		answers = append(answers, model.backupChallenge.Words[index])
	}
	model.backupConfirmationInput.SetValue(strings.Join(answers, " "))
	driveCmds(model, pressKey(model, "enter"))

	require.NotNil(t, model.selectedAccount)
	assert.Equal(t, accountID, model.selectedAccount.AccountID)
	assert.Equal(t, wallet.AccountStateActive, model.selectedAccount.State)
	pending, err := model.credentialService.Pending(context.Background())
	require.NoError(t, err)
	assert.Empty(t, pending)

	accounts, err := vault.ListAccounts(context.Background())
	require.NoError(t, err)
	found := false
	for _, account := range accounts {
		if account.AccountID == accountID {
			found = true
		}
	}
	assert.True(t, found)
}

func TestKeePassUIRotateAndExport(t *testing.T) {
	model, _, _, _ := newCredentialUITestModel(t, true)
	summary := createActiveAccount(t, model, "rotate-export", "old storage pw 9")
	model.selectedAccount = &summary

	model.initVaultAction(false)
	require.Equal(t, constants.RotatePasswordView, model.currentView)
	model.currentPasswordInput.SetValue("old storage pw 9")
	pressKey(model, "enter")
	model.newPasswordInput.SetValue("new storage pw 10")
	pressKey(model, "enter")
	model.confirmPasswordInput.SetValue("new storage pw 10")
	pressKey(model, "enter")
	require.NotNil(t, model.credentialPrompt)
	unlockMasterViaUpdate(t, model, gatesMaster)
	assert.Equal(t, constants.WalletDetailsView, model.currentView)
	assert.Contains(t, model.lastOperationNotice, localization.Get("vault_password_rotated"))

	destination := filepath.Join(t.TempDir(), "exported.json")
	model.initVaultAction(true)
	require.Equal(t, constants.ExportAccountView, model.currentView)
	pressKey(model, "ctrl+k")
	require.True(t, model.credentialUseKeePass)
	pressKey(model, "enter")
	model.newPasswordInput.SetValue("export password 1")
	pressKey(model, "enter")
	model.confirmPasswordInput.SetValue("export password 1")
	pressKey(model, "enter")
	model.exportDestinationInput.SetValue(destination)
	pressKey(model, "enter")
	require.True(t, model.vaultActionPreview)
	pressKey(model, "enter")
	require.NotNil(t, model.credentialPrompt)
	unlockMasterViaUpdate(t, model, gatesMaster)

	assert.Equal(t, constants.WalletDetailsView, model.currentView)
	assert.FileExists(t, destination)
	pending, err := model.credentialService.Pending(context.Background())
	require.NoError(t, err)
	assert.Empty(t, pending)
}

func TestKeePassUIPartialResultsPreserved(t *testing.T) {
	model, _, _, _ := newCredentialUITestModel(t, true)
	exportPath := filepath.Join(t.TempDir(), "partial.json")

	operationID, _ := model.beginVaultBusy("vault")
	_, _ = model.Update(vaultActionResultMsg{
		operationID:     operationID,
		export:          true,
		exportCommitted: true,
		exportPath:      exportPath,
		pending:         true,
	})
	assert.Contains(t, model.lastOperationNotice, filepath.Base(exportPath))
	assert.Contains(t, model.lastOperationNotice, localization.Get("keepass_backup_pending_notice"))

	model.lastOperationNotice = ""
	_, _ = model.Update(vaultActionResultMsg{
		operationID:     model.uiOperationID + 99,
		export:          true,
		exportCommitted: true,
		exportPath:      exportPath,
	})
	assert.Contains(t, model.lastOperationNotice, filepath.Base(exportPath), "stale committed export must still surface its notice")

	model.lastOperationNotice = ""
	_, _ = model.Update(vaultCreateResultMsg{
		operationID: model.uiOperationID + 77,
		summary:     wallet.AccountSummary{AccountID: "stale-account", Name: "stale"},
		challenge:   wallet.BackupChallenge{ChallengeID: "stale-challenge"},
	})
	assert.Equal(t, "stale-account", model.resumeBackupAccountID)
	assert.Contains(t, model.lastOperationNotice, localization.Get("backup_suspended_resume"))
	assert.Nil(t, model.backupChallenge)
}

func TestKeePassUIImportMethods(t *testing.T) {
	model, vault, _, _ := newCredentialUITestModel(t, true)

	model.initCanonicalImport(wallet.ImportMethodMnemonic)
	state := model.canonicalImport
	require.NotNil(t, state)
	state.fields[0].input.SetValue("Mnemonic acct")
	pressKey(model, "enter")
	state.fields[1].input.SetValue("test test test test test test test test test test test junk")
	pressKey(model, "enter")
	for state.stage < len(state.fields) && state.fields[state.stage].optional {
		pressKey(model, "enter")
	}
	require.Equal(t, "storage_password", state.fields[state.stage].key)
	state.fields[state.stage].input.SetValue("mnemonic storage pw 1")
	pressKey(model, "enter")
	state.fields[state.stage].input.SetValue("mnemonic storage pw 1")
	pressKey(model, "enter")
	require.NotNil(t, model.credentialPrompt)
	unlockMasterViaUpdate(t, model, gatesMaster)
	driveCmds(model, pressKey(model, "enter"))
	require.Nil(t, model.canonicalImport)

	model.initCanonicalImport(wallet.ImportMethodPrivateKey)
	state = model.canonicalImport
	require.NotNil(t, state)
	state.fields[0].input.SetValue("PK acct")
	pressKey(model, "enter")
	state.fields[1].input.SetValue("0x4646464646464646464646464646464646464646464646464646464646464646")
	pressKey(model, "enter")
	require.Equal(t, "storage_password", state.fields[state.stage].key)
	state.fields[state.stage].input.SetValue("pk storage pw 2")
	pressKey(model, "enter")
	state.fields[state.stage].input.SetValue("pk storage pw 2")
	pressKey(model, "enter")
	require.NotNil(t, model.credentialPrompt)
	unlockMasterViaUpdate(t, model, gatesMaster)
	driveCmds(model, pressKey(model, "enter"))
	require.Nil(t, model.canonicalImport)

	accounts, err := vault.ListAccounts(context.Background())
	require.NoError(t, err)
	names := map[string]bool{}
	for _, account := range accounts {
		names[account.Name] = true
	}
	assert.True(t, names["Mnemonic acct"])
	assert.True(t, names["PK acct"])
	pending, err := model.credentialService.Pending(context.Background())
	require.NoError(t, err)
	assert.Empty(t, pending)
}

func TestKeePassUIMnemonicBatch(t *testing.T) {
	model, vault, _, _ := newCredentialUITestModel(t, true)
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "one.mnemonic"), []byte("test test test test test test test test test test test junk\n"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "two.seedphrase"), []byte("legal winner thank year wave sausage worth useful legal winner thank yellow\n"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "three.mnemonic"), []byte("not a seed\n"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "ignored.txt"), []byte("test test test test test test test test test test test junk"), 0600))

	model.initCanonicalImport(canonicalMnemonicBatchMethod)
	state := model.canonicalImport
	require.NotNil(t, state)
	state.fields[0].input.SetValue(root)
	driveCmds(model, pressKey(model, "enter"))
	require.Equal(t, "storage_password", state.fields[state.stage].key)
	state.fields[state.stage].input.SetValue("mn batch storage pw")
	pressKey(model, "enter")
	state.fields[state.stage].input.SetValue("mn batch storage pw")
	pressKey(model, "enter")
	require.NotNil(t, model.credentialPrompt)
	unlockMasterViaUpdate(t, model, gatesMaster)
	require.NotEmpty(t, state.batchPreviews)
	driveCmds(model, pressKey(model, "enter"))

	accounts, err := vault.ListAccounts(context.Background())
	require.NoError(t, err)
	assert.Len(t, accounts, 2)
	pending, err := model.credentialService.Pending(context.Background())
	require.NoError(t, err)
	assert.Empty(t, pending)
	assert.NotContains(t, strings.Join(state.resultLines, "\n"), "mn batch storage pw")
	assert.NotContains(t, strings.Join(state.resultLines, "\n"), "legal winner thank")
}

func TestKeePassUIBatchSourceOptOutAndErrors(t *testing.T) {
	model, vault, store, binding := newCredentialUITestModel(t, true)

	t.Run("wrong kdbx password does not fall back to empty", func(t *testing.T) {
		root := t.TempDir()
		ciphertext := writeTestKeystore(t, filepath.Join(root, "enc.json"), "real source pw")
		seedKeePassFileCredential(t, store, binding, gatesMaster, "keystore_v3", ciphertext, "wrong stored pw")

		model.initCanonicalImport(canonicalBatchMethod)
		state := model.canonicalImport
		require.True(t, state.lookupMissingSidecars)
		state.fields[0].input.SetValue(root)
		driveCmds(model, pressKey(model, "enter"))
		state.fields[state.stage].input.SetValue("batch storage x")
		pressKey(model, "enter")
		state.fields[state.stage].input.SetValue("batch storage x")
		pressKey(model, "enter")
		require.NotNil(t, model.credentialPrompt)
		unlockMasterViaUpdate(t, model, gatesMaster)
		require.Len(t, state.batchPreviews, 1)
		assert.NotEmpty(t, state.batchPreviews[0].err, "wrong stored password must surface an error, not an empty-password fallback")
		driveCmds(model, pressKey(model, "enter"))
		model.clearCanonicalImport()
		assert.Nil(t, model.credentialOperation)
	})

	t.Run("opt out keeps keystore without password failing", func(t *testing.T) {
		root := t.TempDir()
		writeTestKeystore(t, filepath.Join(root, "enc.json"), "another source pw")

		model.initCanonicalImport(canonicalBatchMethod)
		state := model.canonicalImport
		require.True(t, state.lookupMissingSidecars)
		pressKey(model, "ctrl+k")
		assert.False(t, state.lookupMissingSidecars)
		state.fields[0].input.SetValue(root)
		driveCmds(model, pressKey(model, "enter"))
		state.fields[state.stage].input.SetValue("batch storage y")
		pressKey(model, "enter")
		state.fields[state.stage].input.SetValue("batch storage y")
		pressKey(model, "enter")
		require.NotNil(t, model.credentialPrompt)
		unlockMasterViaUpdate(t, model, gatesMaster)
		require.Len(t, state.batchPreviews, 1)
		assert.NotEmpty(t, state.batchPreviews[0].err)
		driveCmds(model, pressKey(model, "enter"))
		model.clearCanonicalImport()
		assert.Nil(t, model.credentialOperation)
	})

	t.Run("empty pwd file keeps priority over wrong kdbx entry", func(t *testing.T) {
		root := t.TempDir()
		ciphertext := writeTestKeystore(t, filepath.Join(root, "empty.json"), "")
		require.NoError(t, os.WriteFile(filepath.Join(root, "empty.pwd"), []byte(""), 0600))
		seedKeePassFileCredential(t, store, binding, gatesMaster, "keystore_v3", ciphertext, "should be ignored")

		model.initCanonicalImport(canonicalBatchMethod)
		state := model.canonicalImport
		state.fields[0].input.SetValue(root)
		driveCmds(model, pressKey(model, "enter"))
		state.fields[state.stage].input.SetValue("batch storage z")
		pressKey(model, "enter")
		state.fields[state.stage].input.SetValue("batch storage z")
		pressKey(model, "enter")
		require.NotNil(t, model.credentialPrompt)
		unlockMasterViaUpdate(t, model, gatesMaster)
		require.Len(t, state.batchPreviews, 1)
		assert.Empty(t, state.batchPreviews[0].err, "empty sidecar file must win over the conflicting KDBX entry")
		driveCmds(model, pressKey(model, "enter"))
		model.clearCanonicalImport()
		assert.Nil(t, model.credentialOperation)

		accounts, err := vault.ListAccounts(context.Background())
		require.NoError(t, err)
		assert.NotEmpty(t, accounts)
		pending, err := model.credentialService.Pending(context.Background())
		require.NoError(t, err)
		assert.Empty(t, pending)
	})
}

func TestKeePassUIPendingRetries(t *testing.T) {
	model, vault, _, _ := newCredentialUITestModel(t, true)
	ctx := context.Background()
	service := model.credentialService

	op, err := service.Begin(ctx, []byte(gatesMaster))
	require.NoError(t, err)
	keystorePath := filepath.Join(t.TempDir(), "pending.json")
	writeTestKeystore(t, keystorePath, "pending file pw")
	data, err := os.ReadFile(keystorePath)
	require.NoError(t, err)
	summary, err := vault.ImportKeystore(op.Context(), wallet.KeystoreImportRequest{
		Name: "pending-acct", KeystoreJSON: data, SourcePassword: []byte("pending file pw"),
		StoragePassword: []byte("pending storage pw"), ConfirmStoragePassword: []byte("pending storage pw"), SourcePath: keystorePath,
	})
	require.NoError(t, err)
	op.Close()

	rows, err := service.Pending(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, rows)

	model.initKeePassSettings()
	state := model.keepassSettings
	for i, action := range model.keepassMenuActions() {
		if action == "pending" {
			state.menuIndex = i
		}
	}
	driveCmds(model, pressKey(model, "enter"))
	require.Equal(t, keepassStagePendingList, state.stage)
	require.NotEmpty(t, state.pending)

	accountIndex, fileIndex := -1, -1
	for i, row := range state.pending {
		if row.State == wallet.CredentialBackupStateDeletePending {
			continue
		}
		if row.ItemID == "account" && accountIndex < 0 {
			accountIndex = i
		}
		if row.ItemID != "account" && fileIndex < 0 {
			fileIndex = i
		}
	}
	require.GreaterOrEqual(t, accountIndex, 0)
	require.GreaterOrEqual(t, fileIndex, 0)

	state.pendingIndex = fileIndex
	pressKey(model, "enter")
	require.Equal(t, keepassStageRetryPath, state.stage)
	movedPath := filepath.Join(t.TempDir(), "moved.json")
	require.NoError(t, os.WriteFile(movedPath, data, 0600))
	state.retryPathInput.SetValue(movedPath)
	pressKey(model, "enter")
	require.Equal(t, keepassStageRetryPassword, state.stage)
	state.masterInput.SetValue("pending file pw")
	pressKey(model, "enter")
	require.NotNil(t, model.credentialPrompt)
	unlockMasterViaUpdate(t, model, gatesMaster)
	assert.Empty(t, state.errText())
	assert.Equal(t, keepassStageMenu, state.stage)

	driveCmds(model, pressKey(model, "enter"))
	require.Equal(t, keepassStagePendingList, state.stage)
	accountIndex = -1
	for i, row := range state.pending {
		if row.ItemID == "account" && row.State != wallet.CredentialBackupStateDeletePending {
			accountIndex = i
		}
	}
	require.GreaterOrEqual(t, accountIndex, 0)
	state.pendingIndex = accountIndex
	pressKey(model, "enter")
	require.Equal(t, keepassStageRetryPassword, state.stage)
	state.masterInput.SetValue("pending storage pw")
	pressKey(model, "enter")
	require.NotNil(t, model.credentialPrompt)
	unlockMasterViaUpdate(t, model, gatesMaster)
	assert.Empty(t, state.errText())

	pending, err := service.Pending(ctx)
	require.NoError(t, err)
	assert.Empty(t, pending)

	delOp, err := service.Begin(ctx, []byte(gatesMaster))
	require.NoError(t, err)
	require.NoError(t, vault.DeleteAccount(delOp.Context(), wallet.DeleteAccountRequest{
		AccountID:              summary.AccountID,
		ConfirmAccountID:       summary.AccountID,
		Password:               []byte("pending storage pw"),
		RemoveCredentialBackup: true,
		ConfirmBackupAccountID: summary.AccountID,
	}))
	delOp.Close()

	driveCmds(model, pressKey(model, "enter"))
	require.Equal(t, keepassStagePendingList, state.stage)
	require.NotEmpty(t, state.pending)
	deleteIndex := -1
	for i, row := range state.pending {
		if row.State == wallet.CredentialBackupStateDeletePending {
			deleteIndex = i
		}
	}
	require.GreaterOrEqual(t, deleteIndex, 0, "expected a delete_pending row")
	state.pendingIndex = deleteIndex
	pressKey(model, "enter")
	require.Equal(t, keepassStageRetryConfirm, state.stage)
	state.consentInput.SetValue("not-the-id")
	pressKey(model, "enter")
	assert.Equal(t, keepassStageRetryConfirm, state.stage)
	assert.NotEmpty(t, state.errText())
	state.consentInput.SetValue(state.pending[deleteIndex].AccountID)
	pressKey(model, "enter")
	require.NotNil(t, model.credentialPrompt)
	unlockMasterViaUpdate(t, model, gatesMaster)

	pending, err = service.Pending(ctx)
	require.NoError(t, err)
	for _, row := range pending {
		assert.NotEqual(t, wallet.CredentialBackupStateDeletePending, row.State)
	}
}

func TestKeePassUISettingsValidation(t *testing.T) {
	model, _, _, _ := newCredentialUITestModel(t, false)
	model.initKeePassSettings()
	state := model.keepassSettings
	pressKey(model, "enter")
	require.Equal(t, keepassStagePath, state.stage)

	state.pathInput.SetValue("relative/vault.kdbx")
	pressKey(model, "enter")
	assert.Equal(t, keepassStagePath, state.stage)
	assert.NotEmpty(t, state.errText())

	state.pathInput.SetValue(filepath.Join(t.TempDir(), "valid.kdbx"))
	pressKey(model, "enter")
	require.Equal(t, keepassStageMaster, state.stage)
	state.masterInput.SetValue("tiny")
	pressKey(model, "enter")
	assert.Equal(t, keepassStageMaster, state.stage)
	assert.NotEmpty(t, state.errText())
	assert.NotContains(t, state.errText(), "policy", "master validation errors must be localized")

	state.masterInput.SetValue("valid master password 9")
	pressKey(model, "enter")
	require.Equal(t, keepassStageConfirmMaster, state.stage)
	state.confirmInput.SetValue("different master")
	pressKey(model, "enter")
	assert.Equal(t, keepassStageConfirmMaster, state.stage)
	assert.NotEmpty(t, state.errText())
	assert.NotEmpty(t, state.masterInput.Value())

	state.confirmInput.SetValue("valid master password 9")
	pressKey(model, "enter")
	require.Equal(t, keepassStageConsent, state.stage)
	state.consentInput.SetValue("enable")
	pressKey(model, "enter")
	assert.Equal(t, keepassStageConsent, state.stage)
	assert.NotEmpty(t, state.errText())
}

func TestKeePassUISettingsSaveFailure(t *testing.T) {
	t.Run("failed save keeps old policy and retry links existing file", func(t *testing.T) {
		model, _, _, _ := newCredentialUITestModel(t, false)
		model.loadConfigFn = func() (*config.Config, error) { return model.currentConfig, nil }
		failErr := errors.New("disk full")
		model.saveConfigFn = func(*config.Config) error { return failErr }

		model.initKeePassSettings()
		state := model.keepassSettings
		pressKey(model, "enter")
		path := filepath.Join(t.TempDir(), "savefail.kdbx")
		state.pathInput.SetValue(path)
		pressKey(model, "enter")
		state.masterInput.SetValue("master password 77")
		pressKey(model, "enter")
		state.confirmInput.SetValue("master password 77")
		pressKey(model, "enter")
		state.consentInput.SetValue("ENABLE")
		driveCmds(model, pressKey(model, "enter"))

		_, statErr := os.Stat(path)
		assert.NoError(t, statErr, "created KDBX file must be preserved after save failure")
		assert.Equal(t, keepassStageMaster, state.stage, "failed save must return focus to the master stage")
		assert.Equal(t, "link", state.mode, "existing committed file must switch the wizard to link mode")
		assert.False(t, model.credentialBackupEnabled(), "failed save must not enable the runtime policy")
		assert.False(t, model.currentConfig.KeePass.Enabled)
		assert.NotEmpty(t, state.errText())

		model.saveConfigFn = func(cfg *config.Config) error {
			model.currentConfig = cfg
			model.balanceConfig = cfg
			return nil
		}
		typeRunesViaUpdate(model, "master password 77")
		pressKey(model, "enter")
		require.Equal(t, keepassStageConsent, state.stage, "link mode goes from master straight to consent")
		typeRunesViaUpdate(model, "ENABLE")
		driveCmds(model, pressKey(model, "enter"))
		assert.True(t, model.credentialBackupEnabled())
		assert.True(t, model.currentConfig.KeePass.Enabled)
	})

	t.Run("cancel before save skips persistence", func(t *testing.T) {
		model, _, _, _ := newCredentialUITestModel(t, false)
		saveCalls := 0
		model.loadConfigFn = func() (*config.Config, error) {
			model.keepassSettings.cancel()
			return model.currentConfig, nil
		}
		model.saveConfigFn = func(*config.Config) error {
			saveCalls++
			return nil
		}

		model.initKeePassSettings()
		state := model.keepassSettings
		pressKey(model, "enter")
		state.pathInput.SetValue(filepath.Join(t.TempDir(), "cancellink.kdbx"))
		pressKey(model, "enter")
		state.masterInput.SetValue("master password 77")
		pressKey(model, "enter")
		state.confirmInput.SetValue("master password 77")
		pressKey(model, "enter")
		state.consentInput.SetValue("ENABLE")
		driveCmds(model, pressKey(model, "enter"))

		assert.Equal(t, 0, saveCalls, "cancelled context must skip the config save")
		assert.False(t, model.credentialBackupEnabled())
		assert.False(t, model.currentConfig.KeePass.Enabled)
	})

	t.Run("cancel inside committed save still applies persisted policy", func(t *testing.T) {
		model, _, _, _ := newCredentialUITestModel(t, false)
		model.loadConfigFn = func() (*config.Config, error) { return model.currentConfig, nil }
		model.saveConfigFn = func(cfg *config.Config) error {
			model.currentConfig = cfg
			model.balanceConfig = cfg
			model.keepassSettings.cancel()
			return nil
		}

		model.initKeePassSettings()
		state := model.keepassSettings
		pressKey(model, "enter")
		state.pathInput.SetValue(filepath.Join(t.TempDir(), "committed.kdbx"))
		pressKey(model, "enter")
		state.masterInput.SetValue("master password 77")
		pressKey(model, "enter")
		state.confirmInput.SetValue("master password 77")
		pressKey(model, "enter")
		state.consentInput.SetValue("ENABLE")
		driveCmds(model, pressKey(model, "enter"))

		assert.True(t, model.currentConfig.KeePass.Enabled)
		assert.True(t, model.credentialBackupEnabled(), "cancel arriving after a committed save must still apply the persisted policy")
	})
}

func TestKeePassUISettingsAutocomplete(t *testing.T) {
	model, _, _, _ := newCredentialUITestModel(t, false)
	dir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(dir, "vaults"), 0700))
	model.initKeePassSettings()
	state := model.keepassSettings
	pressKey(model, "enter")
	require.Equal(t, keepassStagePath, state.stage)
	state.pathInput.SetValue(filepath.Join(dir, "v"))
	_, cmd := model.Update(keyMsg("a"))
	driveCmds(model, cmd)
	suggestions := state.pathInput.AvailableSuggestions()
	assert.NotEmpty(t, suggestions, "path input must offer directory autocomplete")
	assert.True(t, state.pathInput.ShowSuggestions)
}

func TestKeePassUINativeTransferResolvesBackupPassword(t *testing.T) {
	model, _, _, _ := newCredentialUITestModel(t, true)
	summary := createActiveAccount(t, model, "native-signer", "native storage pw 5")
	model.selectedAccount = &summary
	addGateNetwork(model)
	spy := &spyTransactionAuthorizer{}
	model.transactionAuthorizer = spy
	engine := &gateEngineStub{}
	model.ConfigureTransactionEngineFactory(func(context.Context, config.Network) (TransactionEngine, error) {
		return engine, nil
	})

	model.initNativeTransfer()
	require.NotNil(t, model.nativeTransfer)
	model.currentView = constants.NativeTransferView
	state := model.nativeTransfer
	require.Equal(t, nativeTransferSelectNetwork, state.phase)
	driveCmds(model, pressKey(model, "enter"))
	require.Equal(t, nativeTransferEnterRecipient, state.phase)
	state.recipientInput.SetValue("0x000000000000000000000000000000000000dEaD")
	pressKey(model, "enter")
	require.Equal(t, nativeTransferEnterAmount, state.phase)
	state.amountInput.SetValue("0.001")
	driveCmds(model, pressKey(model, "enter"))
	require.Equal(t, nativeTransferPreview, state.phase)
	pressKey(model, "enter")
	require.Equal(t, nativeTransferPassword, state.phase)
	pressKey(model, "ctrl+k")
	pressKey(model, "enter")
	require.NotNil(t, model.credentialPrompt)
	unlockMasterViaUpdate(t, model, gatesMaster)

	require.Equal(t, 1, spy.calls)
	assert.Equal(t, summary.AccountID, spy.accountID)
	assert.Equal(t, []byte("native storage pw 5"), spy.password)
	assert.Equal(t, 1, engine.broadcasts)
	assert.Nil(t, model.credentialOperation)
}

func TestKeePassUIEIP712ResolvesBackupPassword(t *testing.T) {
	model, _, _, _ := newCredentialUITestModel(t, true)
	summary := createActiveAccount(t, model, "eip712-signer", "eip712 storage pw 6")
	model.selectedAccount = &summary
	addGateNetwork(model)
	spy := &spyTransactionAuthorizer{}
	model.transactionAuthorizer = spy
	service := &personalSignServiceStub{signer: common.HexToAddress(summary.Address)}

	model.initEIP712Sign(service)
	state := model.eip712Sign
	require.NotNil(t, state)
	require.Equal(t, eip712SignSelectNetwork, state.phase)
	pressKey(model, "n")
	require.Equal(t, eip712SignEntry, state.phase)
	state.typedData.SetValue(eip712SignUITestFixture)
	pressKey(model, "n")
	require.Equal(t, eip712SignPreview, state.phase)
	pressKey(model, "a")
	require.Equal(t, eip712SignPassword, state.phase)
	pressKey(model, "ctrl+k")
	pressKey(model, "enter")
	require.NotNil(t, model.credentialPrompt)
	unlockMasterViaUpdate(t, model, gatesMaster)

	require.Equal(t, 1, spy.calls)
	assert.Equal(t, summary.AccountID, spy.accountID)
	assert.Equal(t, []byte("eip712 storage pw 6"), spy.password)
	assert.Equal(t, eip712SignComplete, state.phase)
	assert.Nil(t, model.credentialOperation)
}

func TestKeePassUIContractCallResolvesBackupPassword(t *testing.T) {
	model, _, _, _ := newCredentialUITestModel(t, true)
	summary := createActiveAccount(t, model, "contract-caller", "contract storage pw 7")
	model.selectedAccount = &summary
	addGateNetwork(model)
	spy := &spyTransactionAuthorizer{}
	model.transactionAuthorizer = spy
	engine := &gateEngineStub{}
	model.ConfigureTransactionEngineFactory(func(context.Context, config.Network) (TransactionEngine, error) {
		return engine, nil
	})

	model.initContractCall()
	state := model.contractCall
	require.NotNil(t, state)
	pressKey(model, "n")
	require.Equal(t, contractCallContract, state.phase)
	state.inputs["contract"].SetValue("0x000000000000000000000000000000000000dEaD")
	pressKey(model, "enter")
	state.inputs["abi"].SetValue(`[{"type":"function","name":"ping","inputs":[],"outputs":[]}]`)
	pressKey(model, "enter")
	state.inputs["method"].SetValue("ping")
	pressKey(model, "enter")
	state.inputs["args"].SetValue("[]")
	pressKey(model, "enter")
	state.inputs["value"].SetValue("0")
	driveCmds(model, pressKey(model, "enter"))
	require.Equal(t, contractCallPreview, state.phase)
	pressKey(model, "a")
	require.Equal(t, contractCallPassword, state.phase)
	pressKey(model, "ctrl+k")
	pressKey(model, "enter")
	require.NotNil(t, model.credentialPrompt)
	unlockMasterViaUpdate(t, model, gatesMaster)

	require.Equal(t, 1, spy.calls)
	assert.Equal(t, summary.AccountID, spy.accountID)
	assert.Equal(t, []byte("contract storage pw 7"), spy.password)
	assert.Equal(t, 1, engine.broadcasts)
}

func TestKeePassUIRecoveryRequiresConfirmation(t *testing.T) {
	model, _, _, _ := newCredentialUITestModel(t, true)
	summary := createActiveAccount(t, model, "recovery-guard", "recovery storage pw 8")
	model.selectedAccount = &summary

	model.initRecovery()
	state := model.recovery
	require.NotNil(t, state)
	pressKey(model, "enter")
	require.Equal(t, recoveryStagePassword, state.stage)
	pressKey(model, "ctrl+k")
	pressKey(model, "enter")
	require.Equal(t, recoveryStageConfirmation, state.stage)
	state.confirmation.SetValue("WRONG")
	pressKey(model, "enter")
	assert.Equal(t, recoveryStageConfirmation, state.stage)
	assert.NotEmpty(t, state.err)
	assert.Nil(t, state.material, "reveal must not run without the typed confirmation")

	state.confirmation.SetValue("REVEAL")
	pressKey(model, "enter")
	require.NotNil(t, model.credentialPrompt)
	unlockRecoveryWithoutTick(t, model, gatesMaster)
	require.Equal(t, recoveryStageRevealed, state.stage)
	require.NotNil(t, state.material)
	assert.Nil(t, model.credentialOperation)
	material := state.material

	model.clearRecovery()
	model.initRecovery()
	pressKey(model, "enter")
	pressKey(model, "ctrl+k")
	pressKey(model, "enter")
	model.recovery.confirmation.SetValue("REVEAL")
	pressKey(model, "enter")
	require.NotNil(t, model.credentialPrompt, "a second reveal must prompt for the master password again")
	unlockRecoveryWithoutTick(t, model, gatesMaster)
	require.Equal(t, recoveryStageRevealed, model.recovery.stage)
	model.recoveryPrivacyWipe("done")
	material.Destroy()
}

func TestKeePassUISafeCredentials(t *testing.T) {
	model, _, _, _ := newCredentialUITestModel(t, true)
	summary := createActiveAccount(t, model, "safe-owner", "safe storage pw 9")
	spy := &spySafeService{}
	model.ConfigureSafeService(spy)
	owner := gateSoftwareOwner(summary)
	proposal := &SafeProposalSummary{ProposalID: "p1", To: common.HexToAddress("0xdead"), Value: big.NewInt(1)}

	model.safeView = &safeViewState{
		phase:    safeViewSigning,
		owners:   []wallet.Account{owner},
		proposal: proposal,
		chainID:  1,
	}
	model.currentView = constants.SafeView
	pressKey(model, "ctrl+k")
	pressKey(model, "enter")
	require.NotNil(t, model.credentialPrompt)
	unlockMasterViaUpdate(t, model, gatesMaster)
	require.Equal(t, 1, spy.signCalls)
	assert.Equal(t, summary.AccountID, spy.accountIDs[0])
	assert.Equal(t, []byte("safe storage pw 9"), spy.passwords[0])
	assert.Nil(t, model.credentialOperation)

	model.credentialUseKeePass = false
	model.safeView = &safeViewState{
		phase:     safeViewExecute,
		gasPayers: []wallet.Account{owner},
		proposal:  proposal,
		chainID:   1,
	}
	pressKey(model, "ctrl+k")
	pressKey(model, "enter")
	require.NotNil(t, model.credentialPrompt)
	unlockMasterViaUpdate(t, model, gatesMaster)
	require.Equal(t, 1, spy.execCalls)
	assert.Equal(t, []byte("safe storage pw 9"), spy.passwords[1])

	model.credentialUseKeePass = false
	model.safeView = &safeViewState{
		phase:          safeViewDeployRun,
		deployment:     &SafeDeploymentSummary{},
		deployAccounts: []wallet.Account{owner},
		chainID:        1,
	}
	pressKey(model, "ctrl+k")
	pressKey(model, "enter")
	require.NotNil(t, model.credentialPrompt)
	unlockMasterViaUpdate(t, model, gatesMaster)
	require.Equal(t, 1, spy.deployCalls)
	assert.Equal(t, []byte("safe storage pw 9"), spy.passwords[2])
	assert.Nil(t, model.credentialOperation)
}

func TestKeePassUILayoutAndLocale(t *testing.T) {
	model, _, _, _ := newCredentialUITestModel(t, true)
	previous := localization.GetCurrentLanguage()
	t.Cleanup(func() { localization.SetCurrentLanguage(previous) })

	rows := make([]wallet.CredentialBackupState, 0, 24)
	for i := 0; i < 24; i++ {
		rows = append(rows, wallet.CredentialBackupState{
			AccountID:    fmt.Sprintf("account-%02d", i),
			ItemID:       fmt.Sprintf("artifact-%02d", i),
			State:        wallet.CredentialBackupStatePending,
			ArtifactName: "\x07" + strings.Repeat("verylongartifactname", 6),
			ArtifactPath: filepath.Join(t.TempDir(), strings.Repeat("segment-", 20)+"file.json"),
		})
	}
	model.initKeePassSettings()
	state := model.keepassSettings
	state.stage = keepassStagePendingList
	state.pending = rows

	for _, size := range [][2]int{{100, 24}, {120, 32}} {
		for _, lang := range []string{"en", "pt", "es"} {
			localization.SetCurrentLanguage(lang)
			model.refreshLocalizedUI()
			_, _ = model.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
			for i := 0; i < 23; i++ {
				pressKey(model, "down")
			}
			view := model.View()
			require.Contains(t, view, "account-23", "%s %dx%d last selected row must be reachable and visible", lang, size[0], size[1])
			assert.Contains(t, view, "Esc")
			assert.Contains(t, view, "Ctrl+Q")
			assert.Contains(t, view, localization.Get("keepass_state_pending"))
			assert.NotContains(t, view, "\x07", "raw control bytes from pending metadata must be sanitized")
			assert.LessOrEqual(t, lipgloss.Height(view), model.height, "view must respect the terminal height budget")
			for _, line := range strings.Split(view, "\n") {
				assert.LessOrEqual(t, ansi.StringWidth(line), model.width, "rendered line exceeds terminal width")
			}
			for i := 0; i < 23; i++ {
				pressKey(model, "up")
			}
			assert.Contains(t, model.View(), "account-00", "scrolling back up must reveal the first row")
		}
	}

	localization.SetCurrentLanguage("en")
	model.refreshLocalizedUI()
	_, _ = model.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	compact := model.View()
	assert.NotEmpty(t, compact)
	assert.NotContains(t, compact, "account-23", "unsupported sizes must render the compact message, not the list")

	_, _ = model.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	state.stage = keepassStageMaster
	state.masterInput.SetValue("kept-master")
	state.consentInput.SetValue("partial-consent")
	for _, lang := range []string{"pt", "es", "en"} {
		localization.SetCurrentLanguage(lang)
		model.refreshLocalizedUI()
		assert.Equal(t, "kept-master", state.masterInput.Value(), "language switch must preserve the master input value")
		assert.Equal(t, "partial-consent", state.consentInput.Value())
	}
	state.setErrKey("keepass_master_weak", nil)
	for _, lang := range []string{"en", "pt", "es"} {
		localization.SetCurrentLanguage(lang)
		model.refreshLocalizedUI()
		assert.Contains(t, model.View(), localization.Get("keepass_master_weak"), "weak-master error must render in %s", lang)
	}
	localization.SetCurrentLanguage("en")
}

func TestKeePassUINoSecretMessages(t *testing.T) {
	secret := "sup3r-secret-master"
	msgs := []fmt.Stringer{
		vaultCreateResultMsg{challenge: wallet.BackupChallenge{Words: []string{secret}}, err: errors.New(secret)},
		backupConfirmResultMsg{err: errors.New(secret)},
		vaultActionResultMsg{exportPath: secret, err: errors.New(secret)},
		recoveryResultMsg{destination: secret, err: errors.New(secret)},
	}
	for _, msg := range msgs {
		assert.NotContains(t, fmt.Sprintf("%v", msg), secret)
		assert.NotContains(t, fmt.Sprintf("%+v", msg), secret)
		assert.NotContains(t, fmt.Sprintf("%#v", msg), secret)
		data, err := json.Marshal(msg)
		assert.Error(t, err, "credential result messages must refuse JSON serialization")
		assert.Nil(t, data)
	}

	model, _, _, _ := newCredentialUITestModel(t, true)
	summary := createActiveAccount(t, model, "nosecret", "nosecret storage pw")
	model.selectedAccount = &summary
	model.initCreateWallet()
	model.passwordInput.SetValue(secret)
	assert.NotContains(t, model.View(), secret, "masked master/password input must never render the typed secret")

	material, err := model.Vault.RevealRecoverySecret(context.Background(), wallet.RecoverySecretRequest{
		AccountID:        summary.AccountID,
		ConfirmAccountID: summary.AccountID,
		Password:         []byte("nosecret storage pw"),
		Kind:             wallet.RecoveryPrivateKey,
	})
	require.NoError(t, err)
	require.NotEmpty(t, material.Bytes())
	reveal := recoveryResultMsg{material: material, destination: secret}
	assert.NotContains(t, fmt.Sprintf("%v", reveal), fmt.Sprintf("%x", material.Bytes()))
	assert.NotContains(t, fmt.Sprintf("%#v", reveal), fmt.Sprintf("%x", material.Bytes()))
	_, err = json.Marshal(reveal)
	assert.Error(t, err)
	material.Destroy()
}

func TestKeePassUIStaleResults(t *testing.T) {
	model, _, _, _ := newCredentialUITestModel(t, true)
	summary := createActiveAccount(t, model, "stale-owner", "stale storage pw 1")
	model.selectedAccount = &summary

	model.initKeePassSettings()
	_, pendingCmd := model.runKeePassMenuAction("pending")
	require.NotNil(t, pendingCmd)
	stale := pendingCmd().(keepassPendingMsg)
	model.initKeePassSettings()
	freshState := model.keepassSettings
	_, freshCmd := model.runKeePassMenuAction("pending")
	require.NotNil(t, freshCmd)
	freshMsg := freshCmd().(keepassPendingMsg)
	_, _ = model.Update(stale)
	assert.True(t, freshState.busy, "stale pending result must not clear the fresh request")
	assert.Empty(t, freshState.pending, "stale pending result must not populate the list")
	_, _ = model.Update(freshMsg)
	assert.False(t, freshState.busy)

	model.initKeePassAccount(summary)
	oldStatusCmd := model.loadKeePassAccountStatus()
	require.NotNil(t, oldStatusCmd)
	staleStatus := oldStatusCmd().(keepassStatusMsg)
	model.initKeePassAccount(summary)
	freshStatusCmd := model.loadKeePassAccountStatus()
	require.NotNil(t, freshStatusCmd)
	freshStatus := freshStatusCmd().(keepassStatusMsg)
	_, _ = model.Update(staleStatus)
	assert.True(t, model.keepassAccount.busy, "stale status for the same account must not clear the fresh request")
	assert.Empty(t, model.keepassAccount.rows, "stale status must not overwrite the fresh screen")
	_, _ = model.Update(freshStatus)
	assert.False(t, model.keepassAccount.busy)

	accountA := createActiveAccount(t, model, "stale-signer-a", "stale storage pw 1")
	accountB := createActiveAccount(t, model, "stale-signer-b", "stale storage pw 1")
	spy := &spyTransactionAuthorizer{}
	model.transactionAuthorizer = spy
	signer := common.HexToAddress(accountA.Address)
	model.ConfigureMessageSigningFactory(func(context.Context) (MessageSigningService, error) {
		return &personalSignServiceStub{signer: signer}, nil
	})
	service, err := model.messageSigningFactory(context.Background())
	require.NoError(t, err)

	model.selectedAccount = &accountA
	model.initPersonalSign(service)
	oldForm := model.personalSign
	oldForm.message.SetValue("for account a")
	pressKey(model, "n")
	pressKey(model, "a")
	pressKey(model, "ctrl+k")
	pressKey(model, "enter")
	require.NotNil(t, model.credentialPrompt)
	typeRunesViaUpdate(model, gatesMaster)
	_, openCmd := model.Update(keyMsg("enter"))
	require.NotNil(t, openCmd)
	opened := openCmd().(credentialOpenedMsg)
	require.NoError(t, opened.err)
	oldOp := opened.operation

	model.selectedAccount = &accountB
	model.initPersonalSign(service)
	model.personalSign.message.SetValue("for account b")
	_, _ = model.Update(opened)

	assert.Equal(t, 0, spy.calls, "stale credential result must never run the old prepared submission")
	require.NotNil(t, oldOp)
	assert.Error(t, oldOp.Context().Err(), "stale operation must be cancelled and closed")
	assert.Equal(t, "for account b", model.personalSign.message.Value())
}
