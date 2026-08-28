package ui

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"blocowallet/internal/constants"
	"blocowallet/internal/evm"
	"blocowallet/internal/wallet"
	"blocowallet/pkg/config"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/ethereum/go-ethereum/common"
)

type testTransactionAuthorizer struct{}

func (testTransactionAuthorizer) Authorize(_ context.Context, _ string, _ []byte, operation wallet.TransactionAuthorizationOperation) error {
	return operation(wallet.CapabilityHandle{}, 1)
}
func (testTransactionAuthorizer) HasActiveSession(context.Context, string) bool { return false }

type cancellableUIEngine struct{}

func (cancellableUIEngine) PrepareNative(ctx context.Context, _ evm.PrepareNativeRequest) (*evm.PreparedNativeTransfer, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("prepare failed")
}
func (cancellableUIEngine) PrepareERC20Transfer(context.Context, evm.PrepareERC20TransferRequest) (*evm.PreparedNativeTransfer, error) {
	return nil, fmt.Errorf("prepare failed")
}
func (cancellableUIEngine) PrepareERC20Approve(context.Context, evm.PrepareERC20ApproveRequest) (*evm.PreparedNativeTransfer, error) {
	return nil, fmt.Errorf("prepare failed")
}
func (cancellableUIEngine) PrepareERC721SafeTransfer(context.Context, evm.PrepareERC721SafeTransferRequest) (*evm.PreparedNativeTransfer, error) {
	return nil, fmt.Errorf("prepare failed")
}
func (cancellableUIEngine) PrepareERC1155SafeTransfer(context.Context, evm.PrepareERC1155SafeTransferRequest) (*evm.PreparedNativeTransfer, error) {
	return nil, fmt.Errorf("prepare failed")
}
func (cancellableUIEngine) PrepareERC1155BatchTransfer(context.Context, evm.PrepareERC1155BatchTransferRequest) (*evm.PreparedNativeTransfer, error) {
	return nil, fmt.Errorf("prepare failed")
}
func (cancellableUIEngine) PrepareContractCall(context.Context, evm.PrepareContractCallRequest) (*evm.PreparedNativeTransfer, error) {
	return nil, fmt.Errorf("prepare failed")
}
func (cancellableUIEngine) ApproveSignAndBroadcast(context.Context, wallet.CapabilityHandle, *evm.PreparedNativeTransfer, evm.ApprovalRequest) (evm.ExecutionResult, error) {
	return evm.ExecutionResult{}, fmt.Errorf("submit failed")
}
func (cancellableUIEngine) TrackTransaction(context.Context, string, uint64, time.Time) (evm.TrackingResult, error) {
	return evm.TrackingResult{}, nil
}
func (cancellableUIEngine) CancelPrepared(context.Context, *evm.PreparedNativeTransfer, string) error {
	return nil
}
func (cancellableUIEngine) Rebroadcast(context.Context, string) (evm.ExecutionResult, error) {
	return evm.ExecutionResult{}, fmt.Errorf("rebroadcast failed")
}

func TestERC20ApprovalRequiresDistinctReinforcedConfirmation(t *testing.T) {
	model := eligibleNativeTransferModel()
	model.initERC20Approve()
	model.nativeTransfer.phase = nativeTransferPreview
	_, _ = model.updateNativeTransfer(tea.KeyMsg{Type: tea.KeyEnter})
	if model.nativeTransfer.phase != nativeTransferReinforced {
		t.Fatal("ERC-20 approval skipped reinforced confirmation phase")
	}
	model.nativeTransfer.confirmationInput.SetValue("approve")
	_, _ = model.updateNativeTransfer(tea.KeyMsg{Type: tea.KeyEnter})
	if model.nativeTransfer.phase != nativeTransferReinforced {
		t.Fatal("non-exact reinforced confirmation was accepted")
	}
	model.nativeTransfer.confirmationInput.SetValue("APPROVE")
	_, _ = model.updateNativeTransfer(tea.KeyMsg{Type: tea.KeyEnter})
	if model.nativeTransfer.phase != nativeTransferPassword {
		t.Fatal("reinforced approval did not advance to authorization")
	}
}

func TestNativeTransferOldSubmissionCannotCorruptNewFlow(t *testing.T) {
	model := eligibleNativeTransferModel()
	model.initNativeTransfer()
	oldGeneration := model.nativeTransfer.generation
	model.nativeTransfer.phase = nativeTransferSubmitting
	model.clearNativeTransfer()
	model.initNativeTransfer()
	newGeneration := model.nativeTransfer.generation
	if newGeneration == oldGeneration {
		t.Fatal("native transfer generation was reused")
	}
	_, _ = model.updateNativeTransfer(nativeSubmittedMsg{
		generation: oldGeneration,
		result:     evm.ExecutionResult{TransactionID: "old", Hash: common.HexToHash("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")},
	})
	if model.nativeTransfer.phase != nativeTransferSelectNetwork || model.nativeTransfer.result != nil {
		t.Fatal("stale submission corrupted a new transfer")
	}
}

func TestNativeTransferCancellationIgnoresStalePrepareResult(t *testing.T) {
	model := eligibleNativeTransferModel()
	model.ConfigureTransactionEngineFactory(func(context.Context, config.Network) (TransactionEngine, error) {
		return cancellableUIEngine{}, nil
	})
	_, _ = model.updateWalletDetails(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	_, factoryCommand := model.updateNativeTransfer(tea.KeyMsg{Type: tea.KeyEnter})
	if factoryCommand == nil {
		t.Fatal("network selection did not create an async factory command")
	}
	_, _ = model.updateNativeTransfer(factoryCommand())
	model.nativeTransfer.recipientInput.SetValue("0x2222222222222222222222222222222222222222")
	_, _ = model.updateNativeTransfer(tea.KeyMsg{Type: tea.KeyEnter})
	model.nativeTransfer.amountInput.SetValue("1")
	_, prepareCommand := model.updateNativeTransfer(tea.KeyMsg{Type: tea.KeyEnter})
	if prepareCommand == nil {
		t.Fatal("amount confirmation did not create an async prepare command")
	}
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if model.currentView != constants.WalletDetailsView || model.nativeTransfer != nil {
		t.Fatal("escape did not clear native transfer state")
	}
	_, _ = model.Update(prepareCommand())
	if model.currentView != constants.WalletDetailsView || model.nativeTransfer != nil {
		t.Fatal("stale prepare result restored cancelled transfer")
	}
}

func TestNativeTransferPasswordNeverRendersPlaintext(t *testing.T) {
	model := eligibleNativeTransferModel()
	model.initNativeTransfer()
	model.nativeTransfer.phase = nativeTransferPassword
	model.nativeTransfer.passwordInput.SetValue("super-secret-password")
	view := model.viewNativeTransfer()
	if strings.Contains(view, "super-secret-password") {
		t.Fatal("native transfer view exposed plaintext password")
	}
}

func eligibleNativeTransferModel() *CLIModel {
	return &CLIModel{
		currentView: constants.WalletDetailsView,
		selectedAccount: &wallet.AccountSummary{
			AccountID:    "8b9b0587-388e-4fca-bba4-bf544ebe53ca",
			Address:      "0x1111111111111111111111111111111111111111",
			SignerKind:   wallet.SignerKindSoftware,
			Capabilities: wallet.CapabilitySignTransaction,
			State:        wallet.AccountStateActive,
		},
		currentConfig: &config.Config{Networks: map[string]config.Network{
			"test": {Name: "Test", ChainID: 1, Symbol: "ETH", NativeDecimals: 18, NativeDecimalsSet: true, IsActive: true, RPCEndpoint: "https://rpc.example.com"},
		}},
		styles:                createStyles(),
		transactionAuthorizer: testTransactionAuthorizer{},
	}
}

func TestWalletDetailsExposesERC20Operations(t *testing.T) {
	for key, operation := range map[rune]evm.Operation{'t': evm.OperationERC20Transfer, 'a': evm.OperationERC20Approve} {
		model := eligibleNativeTransferModel()
		model.ConfigureTransactionEngineFactory(func(context.Context, config.Network) (TransactionEngine, error) { return cancellableUIEngine{}, nil })
		_, command := model.updateWalletDetails(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{key}})
		if command != nil || model.nativeTransfer == nil || model.nativeTransfer.operation != operation || model.currentView != constants.NativeTransferView {
			t.Fatalf("key %q did not initialize %s", key, operation)
		}
	}
}

func TestNativeTransferStartsLazilyFromEligibleAccount(t *testing.T) {
	calls := 0
	model := &CLIModel{
		currentView: constants.WalletDetailsView,
		selectedAccount: &wallet.AccountSummary{
			AccountID:    "8b9b0587-388e-4fca-bba4-bf544ebe53ca",
			Address:      "0x1111111111111111111111111111111111111111",
			SignerKind:   wallet.SignerKindSoftware,
			Capabilities: wallet.CapabilitySignTransaction,
			State:        wallet.AccountStateActive,
		},
		currentConfig: &config.Config{Networks: map[string]config.Network{
			"test": {Name: "Test", ChainID: 1, Symbol: "ETH", NativeDecimals: 18, NativeDecimalsSet: true, IsActive: true},
		}},
		styles:                createStyles(),
		transactionAuthorizer: testTransactionAuthorizer{},
	}
	model.ConfigureTransactionEngineFactory(func(context.Context, config.Network) (TransactionEngine, error) {
		calls++
		return nil, nil
	})
	_, command := model.updateWalletDetails(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	if command != nil || calls != 0 || model.currentView != constants.NativeTransferView || model.nativeTransfer == nil {
		t.Fatalf("native transfer was not initialized lazily: view=%s calls=%d command=%v state=%+v", model.currentView, calls, command, model.nativeTransfer)
	}
}
