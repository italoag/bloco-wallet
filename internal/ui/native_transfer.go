package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"sort"
	"strings"
	"time"

	"blocowallet/internal/constants"
	"blocowallet/internal/evm"
	"blocowallet/internal/wallet"
	"blocowallet/pkg/config"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/ethereum/go-ethereum/common"
)

type TransactionEngine interface {
	PrepareNative(context.Context, evm.PrepareNativeRequest) (*evm.PreparedNativeTransfer, error)
	PrepareERC20Transfer(context.Context, evm.PrepareERC20TransferRequest) (*evm.PreparedNativeTransfer, error)
	PrepareERC20Approve(context.Context, evm.PrepareERC20ApproveRequest) (*evm.PreparedNativeTransfer, error)
	PrepareERC721SafeTransfer(context.Context, evm.PrepareERC721SafeTransferRequest) (*evm.PreparedNativeTransfer, error)
	PrepareERC1155SafeTransfer(context.Context, evm.PrepareERC1155SafeTransferRequest) (*evm.PreparedNativeTransfer, error)
	PrepareERC1155BatchTransfer(context.Context, evm.PrepareERC1155BatchTransferRequest) (*evm.PreparedNativeTransfer, error)
	PrepareContractCall(context.Context, evm.PrepareContractCallRequest) (*evm.PreparedNativeTransfer, error)
	ApproveSignAndBroadcast(context.Context, wallet.CapabilityHandle, *evm.PreparedNativeTransfer, evm.ApprovalRequest) (evm.ExecutionResult, error)
	TrackTransaction(context.Context, string, uint64, time.Time) (evm.TrackingResult, error)
	CancelPrepared(context.Context, *evm.PreparedNativeTransfer, string) error
	Rebroadcast(context.Context, string) (evm.ExecutionResult, error)
}

type TransactionEngineFactory func(context.Context, config.Network) (TransactionEngine, error)

type TransactionAuthorizer interface {
	Authorize(context.Context, string, []byte, wallet.TransactionAuthorizationOperation) error
	HasActiveSession(context.Context, string) bool
}

type nativeTransferPhase string

const (
	nativeTransferSelectNetwork  nativeTransferPhase = "select_network"
	nativeTransferConnecting     nativeTransferPhase = "connecting"
	nativeTransferEnterContract  nativeTransferPhase = "enter_contract"
	nativeTransferEnterRecipient nativeTransferPhase = "enter_recipient"
	nativeTransferEnterTokenID   nativeTransferPhase = "enter_token_id"
	nativeTransferEnterEffects   nativeTransferPhase = "enter_effects"
	nativeTransferEnterAmount    nativeTransferPhase = "enter_amount"
	nativeTransferPreparing      nativeTransferPhase = "preparing"
	nativeTransferPreview        nativeTransferPhase = "preview"
	nativeTransferReinforced     nativeTransferPhase = "reinforced"
	nativeTransferPassword       nativeTransferPhase = "password"
	nativeTransferSubmitting     nativeTransferPhase = "submitting"
	nativeTransferTracking       nativeTransferPhase = "tracking"
	nativeTransferComplete       nativeTransferPhase = "complete"
)

type nativeNetworkChoice struct {
	key     string
	network config.Network
}

type nativeTransferState struct {
	phase              nativeTransferPhase
	account            wallet.AccountSummary
	operation          evm.Operation
	networks           []nativeNetworkChoice
	selected           int
	generation         uint64
	planGeneration     uint64
	cancel             context.CancelFunc
	engine             TransactionEngine
	contractInput      textinput.Model
	recipientInput     textinput.Model
	amountInput        textinput.Model
	passwordInput      textinput.Model
	confirmationInput  textinput.Model
	contract           common.Address
	recipient          common.Address
	tokenID            *big.Int
	effects            []evm.EffectEntry
	amountText         string
	prepared           *evm.PreparedNativeTransfer
	result             *evm.ExecutionResult
	tracking           *evm.TrackingResult
	confirmationTarget uint64
	err                string
}

type nativeEngineReadyMsg struct {
	generation uint64
	engine     TransactionEngine
	err        error
}

type nativePreparedMsg struct {
	generation uint64
	engine     TransactionEngine
	prepared   *evm.PreparedNativeTransfer
	err        error
}

type nativeSubmittedMsg struct {
	generation uint64
	result     evm.ExecutionResult
	err        error
}

type nativeTrackTickMsg struct {
	generation uint64
}

type nativeTrackedMsg struct {
	generation uint64
	result     evm.TrackingResult
	err        error
}

func (model *CLIModel) ConfigureTransactionEngineFactory(factory TransactionEngineFactory) {
	model.transactionEngineFactory = factory
}

func (model *CLIModel) ConfigureTransactionAuthorizer(authorizer TransactionAuthorizer) {
	model.transactionAuthorizer = authorizer
}

func (model *CLIModel) nextNativeTransferGeneration() uint64 {
	model.nativeTransferGeneration++
	return model.nativeTransferGeneration
}

func (model *CLIModel) initNativeTransfer() {
	model.initTransactionTransfer(evm.OperationNativeTransfer)
}

func (model *CLIModel) initERC20Transfer() {
	model.initTransactionTransfer(evm.OperationERC20Transfer)
}

func (model *CLIModel) initERC20Approve() {
	model.initTransactionTransfer(evm.OperationERC20Approve)
}

func (model *CLIModel) initERC721Transfer() {
	model.initTransactionTransfer(evm.OperationERC721SafeTransfer)
}

func (model *CLIModel) initERC1155Transfer() {
	model.initTransactionTransfer(evm.OperationERC1155SafeTransfer)
}

func (model *CLIModel) initERC1155BatchTransfer() {
	model.initTransactionTransfer(evm.OperationERC1155BatchTransfer)
}

func parseERC1155BatchEffects(value string) ([]evm.EffectEntry, error) {
	var pairs [][]string
	if err := json.Unmarshal([]byte(value), &pairs); err != nil {
		return nil, fmt.Errorf("effects must be JSON like [[id,amount],...]")
	}
	if len(pairs) == 0 || len(pairs) > 64 {
		return nil, fmt.Errorf("effects must contain between 1 and 64 pairs")
	}
	effects := make([]evm.EffectEntry, 0, len(pairs))
	for _, pair := range pairs {
		if len(pair) != 2 {
			return nil, fmt.Errorf("each effect must be [id, amount]")
		}
		tokenID, ok := new(big.Int).SetString(strings.TrimSpace(pair[0]), 10)
		if !ok || tokenID.Sign() < 0 || tokenID.BitLen() > 256 {
			return nil, fmt.Errorf("token ID must be an exact unsigned base-10 integer")
		}
		amount, ok := new(big.Int).SetString(strings.TrimSpace(pair[1]), 10)
		if !ok || amount.Sign() <= 0 || amount.BitLen() > 256 {
			return nil, fmt.Errorf("amount must be an exact positive base-10 integer")
		}
		effects = append(effects, evm.EffectEntry{TokenID: tokenID, Amount: amount})
	}
	return effects, nil
}

func (model *CLIModel) initTransactionTransfer(operation evm.Operation) {
	if model.selectedAccount == nil || model.currentConfig == nil {
		return
	}
	choices := make([]nativeNetworkChoice, 0, len(model.currentConfig.Networks))
	for key, network := range model.currentConfig.Networks {
		if network.IsActive && network.ChainID > 0 && network.NativeDecimalsSet && (network.RPCEndpoint != "" || network.RPCEndpointRef != "") {
			choices = append(choices, nativeNetworkChoice{key: key, network: network})
		}
	}
	sort.Slice(choices, func(left, right int) bool { return choices[left].key < choices[right].key })
	contractInput := textinput.New()
	contractInput.Placeholder = "0x token contract"
	contractInput.CharLimit = 42
	contractInput.Width = 44
	recipientInput := textinput.New()
	recipientInput.Placeholder = "0x recipient address"
	recipientInput.CharLimit = 42
	recipientInput.Width = 44
	amountInput := textinput.New()
	amountInput.Placeholder = "Amount"
	if operation == evm.OperationERC721SafeTransfer {
		amountInput.Placeholder = "Token ID"
	}
	amountInput.CharLimit = 96
	amountInput.Width = 32
	passwordInput := textinput.New()
	passwordInput.Placeholder = "Storage password"
	passwordInput.EchoMode = textinput.EchoPassword
	passwordInput.EchoCharacter = '•'
	passwordInput.CharLimit = constants.PasswordCharLimit
	passwordInput.Width = constants.PasswordWidth
	confirmationInput := textinput.New()
	confirmationInput.Placeholder = "Type APPROVE"
	confirmationInput.CharLimit = 16
	confirmationInput.Width = 20
	state := &nativeTransferState{
		phase: nativeTransferSelectNetwork, account: *model.selectedAccount, operation: operation, networks: choices,
		generation: model.nextNativeTransferGeneration(), planGeneration: 1, confirmationTarget: 12,
		contractInput: contractInput, recipientInput: recipientInput, amountInput: amountInput,
		passwordInput: passwordInput, confirmationInput: confirmationInput,
	}
	if len(choices) == 0 {
		state.err = "No active network with validated native currency metadata is configured"
	}
	model.nativeTransfer = state
}

func (model *CLIModel) clearNativeTransfer() {
	if model.nativeTransfer == nil {
		return
	}
	if model.nativeTransfer.cancel != nil {
		model.nativeTransfer.cancel()
	}
	model.nextNativeTransferGeneration()
	model.nativeTransfer.passwordInput.SetValue("")
	model.nativeTransfer.confirmationInput.SetValue("")
	model.nativeTransfer = nil
}

func (model *CLIModel) updateNativeTransfer(msg tea.Msg) (tea.Model, tea.Cmd) {
	state := model.nativeTransfer
	if state == nil {
		model.currentView = constants.WalletDetailsView
		return model, nil
	}
	switch message := msg.(type) {
	case nativeEngineReadyMsg:
		if message.generation != state.generation || state.phase != nativeTransferConnecting {
			return model, nil
		}
		state.cancel = nil
		if message.err != nil || message.engine == nil {
			state.err = safeError(message.err)
			state.phase = nativeTransferSelectNetwork
			return model, nil
		}
		state.engine = message.engine
		if state.operation == evm.OperationNativeTransfer {
			state.phase = nativeTransferEnterRecipient
			state.recipientInput.Focus()
		} else {
			state.phase = nativeTransferEnterContract
			state.contractInput.Focus()
		}
		return model, nil
	case nativePreparedMsg:
		if message.generation != state.generation || state.phase != nativeTransferPreparing {
			return model, nativeCancelPreparedResultCommand(message.engine, message.prepared)
		}
		state.cancel = nil
		if message.err != nil || message.prepared == nil {
			state.err = safeError(message.err)
			state.phase = nativeTransferEnterAmount
			state.amountInput.Focus()
			return model, nil
		}
		state.prepared = message.prepared
		state.phase = nativeTransferPreview
		return model, nil
	case nativeSubmittedMsg:
		if message.generation != state.generation || state.phase != nativeTransferSubmitting {
			return model, nil
		}
		state.cancel = nil
		if message.err != nil {
			state.err = safeError(message.err)
			if message.result.Hash != (common.Hash{}) && message.result.TransactionID != "" {
				state.result = &message.result
				state.phase = nativeTransferTracking
				return model, nativeTrackCommand(state)
			}
			state.phase = nativeTransferPassword
			state.passwordInput.Focus()
			return model, nil
		}
		state.result = &message.result
		state.phase = nativeTransferTracking
		return model, nativeTrackCommand(state)
	case nativeTrackedMsg:
		if message.generation != state.generation || state.phase != nativeTransferTracking {
			return model, nil
		}
		state.cancel = nil
		if message.err != nil {
			state.err = safeError(message.err)
			generation := state.generation
			return model, tea.Tick(2*time.Second, func(time.Time) tea.Msg { return nativeTrackTickMsg{generation: generation} })
		}
		state.tracking = &message.result
		if message.result.State == evm.TransactionConfirmed || message.result.State == evm.TransactionReverted || message.result.State == evm.TransactionEffectUnverified {
			state.phase = nativeTransferComplete
			return model, nil
		}
		generation := state.generation
		return model, tea.Tick(2*time.Second, func(time.Time) tea.Msg { return nativeTrackTickMsg{generation: generation} })
	case nativeTrackTickMsg:
		if message.generation != state.generation || state.phase != nativeTransferTracking {
			return model, nil
		}
		return model, nativeTrackCommand(state)
	case tea.KeyMsg:
		if message.String() == "esc" {
			cancelPrepared := nativeCancelPreparedCommand(state)
			model.clearNativeTransfer()
			model.currentView = constants.WalletDetailsView
			return model, cancelPrepared
		}
		switch state.phase {
		case nativeTransferSelectNetwork:
			if len(state.networks) == 0 {
				return model, nil
			}
			switch message.String() {
			case "up", "k":
				if state.selected > 0 {
					state.selected--
				}
			case "down", "j":
				if state.selected < len(state.networks)-1 {
					state.selected++
				}
			case "enter":
				state.err = ""
				state.generation = model.nextNativeTransferGeneration()
				generation := state.generation
				choice := state.networks[state.selected]
				state.confirmationTarget = choice.network.ConfirmationTarget
				if state.confirmationTarget == 0 {
					state.confirmationTarget = 12
				}
				ctx, cancel := context.WithCancel(context.Background())
				state.cancel = cancel
				state.phase = nativeTransferConnecting
				factory := model.transactionEngineFactory
				return model, func() tea.Msg {
					engine, err := factory(ctx, choice.network)
					return nativeEngineReadyMsg{generation: generation, engine: engine, err: err}
				}
			}
		case nativeTransferEnterContract:
			if message.String() == "enter" {
				value := strings.TrimSpace(state.contractInput.Value())
				if !common.IsHexAddress(value) || len(value) != 42 || common.HexToAddress(value) == (common.Address{}) {
					state.err = "Token contract must be a non-zero 20-byte EVM address"
					return model, nil
				}
				state.contract = common.HexToAddress(value)
				state.err = ""
				state.contractInput.Blur()
				state.phase = nativeTransferEnterRecipient
				state.recipientInput.Focus()
				return model, nil
			}
			var command tea.Cmd
			state.contractInput, command = state.contractInput.Update(message)
			return model, command
		case nativeTransferEnterRecipient:
			if message.String() == "enter" {
				value := strings.TrimSpace(state.recipientInput.Value())
				if !common.IsHexAddress(value) || len(value) != 42 {
					state.err = "Recipient must be a 20-byte EVM address"
					return model, nil
				}
				state.recipient = common.HexToAddress(value)
				if state.recipient == common.HexToAddress(state.account.Address) {
					state.err = "Recipient must differ from sender"
					return model, nil
				}
				state.err = ""
				state.recipientInput.Blur()
				switch state.operation {
				case evm.OperationERC1155SafeTransfer:
					state.phase = nativeTransferEnterTokenID
				case evm.OperationERC1155BatchTransfer:
					state.phase = nativeTransferEnterEffects
				default:
					state.phase = nativeTransferEnterAmount
				}
				state.amountInput.Focus()
				return model, nil
			}
			var command tea.Cmd
			state.recipientInput, command = state.recipientInput.Update(message)
			return model, command
		case nativeTransferEnterTokenID:
			if message.String() == "enter" {
				tokenID, ok := new(big.Int).SetString(strings.TrimSpace(state.amountInput.Value()), 10)
				if !ok || tokenID.Sign() < 0 || tokenID.BitLen() > 256 {
					state.err = "Token ID must be an exact unsigned base-10 integer"
					return model, nil
				}
				state.tokenID = tokenID
				state.err = ""
				state.amountInput.SetValue("")
				state.phase = nativeTransferEnterAmount
				state.amountInput.Focus()
				return model, nil
			}
			var command tea.Cmd
			state.amountInput, command = state.amountInput.Update(message)
			return model, command
		case nativeTransferEnterEffects:
			if message.String() == "enter" {
				effects, err := parseERC1155BatchEffects(state.amountInput.Value())
				if err != nil {
					state.err = safeError(err)
					return model, nil
				}
				state.effects = effects
				state.err = ""
				state.amountInput.SetValue("")
				state.phase = nativeTransferPreparing
				state.generation = model.nextNativeTransferGeneration()
				generation := state.generation
				engine := state.engine
				account := state.account
				contract := state.contract
				recipient := state.recipient
				planGeneration := state.planGeneration
				choice := state.networks[state.selected]
				ctx, cancel := context.WithCancel(context.Background())
				state.cancel = cancel
				return model, func() tea.Msg {
					operationID, err := evm.NewOperationID()
					if err != nil {
						return nativePreparedMsg{generation: generation, engine: engine, err: err}
					}
					prepared, prepareErr := engine.PrepareERC1155BatchTransfer(ctx, evm.PrepareERC1155BatchTransferRequest{
						OperationID: operationID, PlanGeneration: planGeneration, AccountID: account.AccountID,
						ChainID: uint64(choice.network.ChainID), From: common.HexToAddress(account.Address),
						Contract: contract, To: recipient, Effects: effects,
					})
					if prepareErr == nil && ctx.Err() != nil && prepared != nil {
						_ = engine.CancelPrepared(context.Background(), prepared, "user_cancelled")
						return nativePreparedMsg{generation: generation, engine: engine, err: ctx.Err()}
					}
					return nativePreparedMsg{generation: generation, engine: engine, prepared: prepared, err: prepareErr}
				}
			}
			var command tea.Cmd
			state.amountInput, command = state.amountInput.Update(message)
			return model, command
		case nativeTransferEnterAmount:
			if message.String() == "enter" {
				choice := state.networks[state.selected]
				amountText := state.amountInput.Value()
				var amount *big.Int
				var err error
				if state.operation == evm.OperationNativeTransfer {
					amount, err = evm.ParseUnits(amountText, uint8(choice.network.NativeDecimals))
				} else {
					amount, _ = new(big.Int).SetString(amountText, 10)
					if amount == nil || amount.Sign() < 0 || amount.BitLen() > 256 || (state.operation == evm.OperationERC20Transfer && amount.Sign() == 0) {
						err = fmt.Errorf("token amount must be an exact unsigned base-unit integer")
					}
				}
				if err != nil {
					state.err = safeError(err)
					return model, nil
				}
				operationID, err := evm.NewOperationID()
				if err != nil {
					state.err = "Unable to create transaction operation"
					return model, nil
				}
				state.err = ""
				state.amountText = amountText
				state.amountInput.Blur()
				state.generation = model.nextNativeTransferGeneration()
				generation := state.generation
				engine := state.engine
				account := state.account
				operation := state.operation
				contract := state.contract
				recipient := state.recipient
				planGeneration := state.planGeneration
				ctx, cancel := context.WithCancel(context.Background())
				state.cancel = cancel
				state.phase = nativeTransferPreparing
				return model, func() tea.Msg {
					var prepared *evm.PreparedNativeTransfer
					var prepareErr error
					from := common.HexToAddress(account.Address)
					switch operation {
					case evm.OperationNativeTransfer:
						prepared, prepareErr = engine.PrepareNative(ctx, evm.PrepareNativeRequest{
							OperationID: operationID, PlanGeneration: planGeneration, AccountID: account.AccountID,
							ChainID: uint64(choice.network.ChainID), From: from, To: recipient, Amount: amount,
						})
					case evm.OperationERC20Transfer:
						prepared, prepareErr = engine.PrepareERC20Transfer(ctx, evm.PrepareERC20TransferRequest{
							OperationID: operationID, PlanGeneration: planGeneration, AccountID: account.AccountID,
							ChainID: uint64(choice.network.ChainID), From: from, Contract: contract, To: recipient, Amount: amount,
						})
					case evm.OperationERC20Approve:
						prepared, prepareErr = engine.PrepareERC20Approve(ctx, evm.PrepareERC20ApproveRequest{
							OperationID: operationID, PlanGeneration: planGeneration, AccountID: account.AccountID,
							ChainID: uint64(choice.network.ChainID), From: from, Contract: contract, Spender: recipient, Amount: amount,
						})
					case evm.OperationERC721SafeTransfer:
						prepared, prepareErr = engine.PrepareERC721SafeTransfer(ctx, evm.PrepareERC721SafeTransferRequest{
							OperationID: operationID, PlanGeneration: planGeneration, AccountID: account.AccountID,
							ChainID: uint64(choice.network.ChainID), From: from, Contract: contract, To: recipient, TokenID: amount,
						})
					case evm.OperationERC1155SafeTransfer:
						prepared, prepareErr = engine.PrepareERC1155SafeTransfer(ctx, evm.PrepareERC1155SafeTransferRequest{
							OperationID: operationID, PlanGeneration: planGeneration, AccountID: account.AccountID,
							ChainID: uint64(choice.network.ChainID), From: from, Contract: contract, To: recipient, TokenID: state.tokenID, Amount: amount,
						})
					default:
						prepareErr = fmt.Errorf("unsupported transaction operation")
					}
					if prepareErr == nil && ctx.Err() != nil && prepared != nil {
						_ = engine.CancelPrepared(context.Background(), prepared, "user_cancelled")
						return nativePreparedMsg{generation: generation, engine: engine, err: ctx.Err()}
					}
					return nativePreparedMsg{generation: generation, engine: engine, prepared: prepared, err: prepareErr}
				}
			}
			var command tea.Cmd
			state.amountInput, command = state.amountInput.Update(message)
			return model, command
		case nativeTransferPreview:
			if message.String() == "enter" {
				if state.operation == evm.OperationERC20Approve {
					state.phase = nativeTransferReinforced
					state.confirmationInput.Focus()
				} else {
					state.phase = nativeTransferPassword
					state.passwordInput.Focus()
				}
			}
		case nativeTransferReinforced:
			if message.String() == "enter" {
				if state.confirmationInput.Value() != "APPROVE" {
					state.err = "Type APPROVE exactly to reinforce this spender approval"
					return model, nil
				}
				state.confirmationInput.SetValue("")
				state.confirmationInput.Blur()
				state.err = ""
				state.phase = nativeTransferPassword
				state.passwordInput.Focus()
				return model, nil
			}
			var command tea.Cmd
			state.confirmationInput, command = state.confirmationInput.Update(message)
			return model, command
		case nativeTransferPassword:
			if message.String() == "enter" {
				password := []byte(state.passwordInput.Value())
				authorizer := model.transactionAuthorizer
				accountID := state.account.AccountID
				if authorizer == nil || (len(password) == 0 && !authorizer.HasActiveSession(context.Background(), accountID)) {
					state.err = "Password is required unless a temporary session is active"
					state.passwordInput.Focus()
					return model, nil
				}
				state.passwordInput.SetValue("")
				state.passwordInput.Blur()
				state.generation = model.nextNativeTransferGeneration()
				generation := state.generation
				engine := state.engine
				prepared := state.prepared
				operation := state.operation
				confirmationTarget := state.confirmationTarget
				ctx, cancel := context.WithCancel(context.Background())
				state.cancel = cancel
				state.phase = nativeTransferSubmitting
				return model, func() tea.Msg {
					defer clear(password)
					var result evm.ExecutionResult
					err := authorizer.Authorize(ctx, accountID, password, func(handle wallet.CapabilityHandle, epoch uint64) error {
						riskLevel := evm.RiskNormal
						confirmationLevel := evm.ConfirmationStandard
						if operation == evm.OperationERC20Approve {
							riskLevel = evm.RiskCritical
							confirmationLevel = evm.ConfirmationReinforced
						}
						var operationErr error
						result, operationErr = engine.ApproveSignAndBroadcast(ctx, handle, prepared, evm.ApprovalRequest{
							AuthorizationEpoch: epoch, RiskLevel: riskLevel, ConfirmationLevel: confirmationLevel, ConfirmationTarget: confirmationTarget,
						})
						return operationErr
					})
					return nativeSubmittedMsg{generation: generation, result: result, err: err}
				}
			}
			var command tea.Cmd
			state.passwordInput, command = state.passwordInput.Update(message)
			return model, command
		case nativeTransferTracking:
			if message.String() == "b" && state.result != nil {
				state.generation = model.nextNativeTransferGeneration()
				generation := state.generation
				engine := state.engine
				transactionID := state.result.TransactionID
				ctx, cancel := context.WithCancel(context.Background())
				state.cancel = cancel
				state.phase = nativeTransferSubmitting
				return model, func() tea.Msg {
					result, err := engine.Rebroadcast(ctx, transactionID)
					return nativeSubmittedMsg{generation: generation, result: result, err: err}
				}
			}
		case nativeTransferComplete:
			if message.String() == "enter" {
				model.clearNativeTransfer()
				model.currentView = constants.WalletDetailsView
			}
		}
	}
	return model, nil
}

func nativeCancelPreparedResultCommand(engine TransactionEngine, prepared *evm.PreparedNativeTransfer) tea.Cmd {
	if engine == nil || prepared == nil {
		return nil
	}
	return func() tea.Msg {
		_ = engine.CancelPrepared(context.Background(), prepared, "user_cancelled")
		return nil
	}
}

func nativeCancelPreparedCommand(state *nativeTransferState) tea.Cmd {
	if state == nil || state.engine == nil || state.prepared == nil || state.result != nil || (state.phase != nativeTransferPreview && state.phase != nativeTransferReinforced && state.phase != nativeTransferPassword) {
		return nil
	}
	return nativeCancelPreparedResultCommand(state.engine, state.prepared)
}

func nativeTrackCommand(state *nativeTransferState) tea.Cmd {
	if state == nil || state.engine == nil || state.result == nil {
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	state.cancel = cancel
	generation := state.generation
	engine := state.engine
	transactionID := state.result.TransactionID
	confirmationTarget := state.confirmationTarget
	return func() tea.Msg {
		result, err := engine.TrackTransaction(ctx, transactionID, confirmationTarget, time.Now().UTC())
		return nativeTrackedMsg{generation: generation, result: result, err: err}
	}
}

func (model *CLIModel) viewNativeTransfer() string {
	state := model.nativeTransfer
	if state == nil {
		return "Transaction flow is unavailable"
	}
	var builder strings.Builder
	title := "Send native asset"
	switch state.operation {
	case evm.OperationERC20Transfer:
		title = "Send ERC-20 token"
	case evm.OperationERC20Approve:
		title = "Approve ERC-20 spender"
	case evm.OperationERC721SafeTransfer:
		title = "Send ERC-721 token"
	case evm.OperationERC1155SafeTransfer:
		title = "Send ERC-1155 token"
	case evm.OperationERC1155BatchTransfer:
		title = "Send ERC-1155 batch"
	}
	builder.WriteString(title + "\n\n")
	if state.err != "" {
		builder.WriteString(model.styles.ErrorStyle.Render(safeInline(state.err)))
		builder.WriteString("\n\n")
	}
	switch state.phase {
	case nativeTransferSelectNetwork:
		builder.WriteString("Select network:\n")
		for index, choice := range state.networks {
			prefix := "  "
			if index == state.selected {
				prefix = "> "
			}
			_, _ = fmt.Fprintf(&builder, "%s%s (%d) %s\n", prefix, safeShort(choice.network.Name), choice.network.ChainID, safeShort(choice.network.Symbol))
		}
		builder.WriteString("\nEnter: select • Esc: back")
	case nativeTransferConnecting:
		builder.WriteString("Validating provider and chain identity...")
	case nativeTransferEnterContract:
		builder.WriteString("Token contract:\n" + state.contractInput.View() + "\n\nEnter: continue • Esc: back")
	case nativeTransferEnterRecipient:
		label := "Recipient"
		if state.operation == evm.OperationERC20Approve {
			label = "Spender"
		}
		builder.WriteString(label + ":\n" + state.recipientInput.View() + "\n\nEnter: continue • Esc: back")
	case nativeTransferEnterTokenID:
		builder.WriteString("Token ID (exact base-10 identifier):\n" + state.amountInput.View() + "\n\nEnter: continue • Esc: back")
	case nativeTransferEnterEffects:
		builder.WriteString("Effects as JSON pairs [[id,amount],...] (max 64):\n" + state.amountInput.View() + "\n\nEnter: prepare and simulate • Esc: back")
	case nativeTransferEnterAmount:
		switch state.operation {
		case evm.OperationNativeTransfer:
			choice := state.networks[state.selected]
			builder.WriteString("Amount (" + safeShort(choice.network.Symbol) + "):\n" + state.amountInput.View() + "\n\nEnter: prepare and simulate • Esc: back")
		case evm.OperationERC721SafeTransfer:
			builder.WriteString("Token ID (exact base-10 identifier):\n" + state.amountInput.View() + "\n\nEnter: prepare and simulate • Esc: back")
		case evm.OperationERC1155SafeTransfer:
			builder.WriteString("Amount in exact base units:\n" + state.amountInput.View() + "\n\nEnter: prepare and simulate • Esc: back")
		default:
			builder.WriteString("Token amount in exact base units:\n" + state.amountInput.View() + "\n\nEnter: prepare and simulate • Esc: back")
		}
	case nativeTransferPreparing:
		builder.WriteString("Reserving nonce, estimating fees, and simulating...")
	case nativeTransferPreview:
		plan := state.prepared.Plan()
		transaction := plan.Transaction()
		blockNumber, blockHash := plan.SimulationBlock()
		builder.WriteString("Review exact transaction\n")
		_, _ = fmt.Fprintf(&builder, "Network: %s (chain %s)\n", safeShort(state.networks[state.selected].network.Name), plan.ChainID())
		_, _ = fmt.Fprintf(&builder, "Operation: %s\nFrom: %s\n", safeShort(string(plan.Operation())), safeShort(plan.From().Hex()))
		switch state.operation {
		case evm.OperationNativeTransfer:
			decimals := uint8(state.networks[state.selected].network.NativeDecimals)
			_, _ = fmt.Fprintf(&builder, "To: %s\nAmount: %s wei (%s %s)\n", safeShort(transaction.To().Hex()), plan.Amount(), evm.FormatUnits(plan.Amount(), decimals), safeShort(state.networks[state.selected].network.Symbol))
		case evm.OperationERC721SafeTransfer:
			asset := plan.Asset()
			_, _ = fmt.Fprintf(&builder, "Contract: %s\nRecipient: %s\nToken ID: %s\nCalldata: 0x%x\n", safeShort(asset.Contract.Hex()), safeShort(plan.Counterparty().Hex()), plan.TokenID(), transaction.Data())
		case evm.OperationERC1155SafeTransfer:
			asset := plan.Asset()
			_, _ = fmt.Fprintf(&builder, "Contract: %s\nRecipient: %s\nToken ID: %s\nAmount: %s\nCalldata: 0x%x\n", safeShort(asset.Contract.Hex()), safeShort(plan.Counterparty().Hex()), plan.TokenID(), plan.Amount(), transaction.Data())
		case evm.OperationERC1155BatchTransfer:
			asset := plan.Asset()
			_, _ = fmt.Fprintf(&builder, "Contract: %s\nRecipient: %s\nEffects:\n", safeShort(asset.Contract.Hex()), safeShort(plan.Counterparty().Hex()))
			for _, effect := range plan.Effects() {
				_, _ = fmt.Fprintf(&builder, "  Token ID %s × %s\n", effect.TokenID, effect.Amount)
			}
			_, _ = fmt.Fprintf(&builder, "Calldata: 0x%x\n", transaction.Data())
		default:
			asset := plan.Asset()
			counterpartyLabel := "Recipient"
			if state.operation == evm.OperationERC20Approve {
				counterpartyLabel = "Spender"
			}
			_, _ = fmt.Fprintf(&builder, "Contract: %s\n%s: %s\nToken: %s (%s), decimals %d\nRaw amount: %s\nCalldata: 0x%x\n", safeShort(asset.Contract.Hex()), counterpartyLabel, safeShort(plan.Counterparty().Hex()), safeShort(asset.Name), safeShort(asset.Symbol), asset.Decimals, plan.Amount(), transaction.Data())
		}
		_, _ = fmt.Fprintf(&builder, "Nonce: %d\nGas limit: %d\n", transaction.Nonce(), transaction.Gas())
		if transaction.Type() == 2 {
			_, _ = fmt.Fprintf(&builder, "Max fee per gas: %s wei\nPriority fee per gas: %s wei\n", transaction.GasFeeCap(), transaction.GasTipCap())
		} else {
			_, _ = fmt.Fprintf(&builder, "Gas price: %s wei\n", transaction.GasPrice())
		}
		maximumGasCost := plan.MaximumGasCost()
		maximumDebit := new(big.Int).Add(transaction.Value(), maximumGasCost)
		nativeDecimals := uint8(state.networks[state.selected].network.NativeDecimals)
		nativeSymbol := safeShort(state.networks[state.selected].network.Symbol)
		_, _ = fmt.Fprintf(&builder, "Maximum gas cost: %s wei (%s %s)\nMaximum total debit: %s wei (%s %s)\n", maximumGasCost, evm.FormatUnits(maximumGasCost, nativeDecimals), nativeSymbol, maximumDebit, evm.FormatUnits(maximumDebit, nativeDecimals), nativeSymbol)
		_, _ = fmt.Fprintf(&builder, "Confirmation target: %d blocks\n", state.confirmationTarget)
		_, _ = fmt.Fprintf(&builder, "Simulation block: %d (%s)\nSimulation/policy commitment: %s\nPlan: 0x%x\nDigest: 0x%x\n", blockNumber, safeShort(blockHash.Hex()), safeShort(plan.SimulationResultHash().Hex()), plan.PlanHash(), plan.TransactionDigest())
		for _, finding := range state.prepared.Findings() {
			_, _ = fmt.Fprintf(&builder, "Risk [%s]: %s (%s)\n", safeShort(string(finding.Severity)), safeShort(string(finding.ID)), safeShort(finding.Subject.Hex()))
		}
		builder.WriteString("\nEnter: approve exact structured intent • Esc: cancel")
	case nativeTransferReinforced:
		builder.WriteString("Critical warning: this grants a contract permission to spend token units. Verify the spender independently.\n\nType APPROVE to perform the second confirmation:\n" + state.confirmationInput.View() + "\n\nEnter: continue • Esc: cancel")
	case nativeTransferPassword:
		if state.account.SignerKind == wallet.SignerKindSoftware {
			builder.WriteString("Enter storage password to sign this approved transaction:\n" + state.passwordInput.View() + "\n\nEnter: sign and broadcast • Esc: cancel")
		} else {
			builder.WriteString("Press Enter, then review and confirm the exact transaction on the external signer.\n\nEnter: continue • Esc: cancel")
		}
	case nativeTransferSubmitting:
		builder.WriteString("Signing approved structured intent and broadcasting exact bytes...")
	case nativeTransferTracking:
		_, _ = fmt.Fprintf(&builder, "Transaction: %s\nTracking receipt and confirmations", safeShort(state.result.Hash.Hex()))
		if state.tracking != nil {
			_, _ = fmt.Fprintf(&builder, "\nState: %s\nConfirmations: %d/%d", safeShort(string(state.tracking.State)), state.tracking.Confirmations, state.confirmationTarget)
		}
		builder.WriteString("\n\nB: rebroadcast persisted bytes • Esc: return; tracking resumes after restart")
	case nativeTransferComplete:
		_, _ = fmt.Fprintf(&builder, "Transaction %s\nTransaction hash: %s\n\nEnter or Esc: return", safeShort(string(state.tracking.State)), safeShort(state.result.Hash.Hex()))
	}
	return builder.String()
}
