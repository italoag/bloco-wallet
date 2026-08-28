package ui

import (
	"context"
	"fmt"
	"math/big"
	"sort"
	"strings"
	"time"

	"blocowallet/internal/constants"
	"blocowallet/internal/evm"
	"blocowallet/internal/wallet"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/ethereum/go-ethereum/common"
)

type contractCallPhase string

const (
	contractCallSelectNetwork contractCallPhase = "select_network"
	contractCallContract      contractCallPhase = "enter_contract"
	contractCallABI           contractCallPhase = "enter_abi"
	contractCallMethod        contractCallPhase = "enter_method"
	contractCallArgs          contractCallPhase = "enter_args"
	contractCallValue         contractCallPhase = "enter_value"
	contractCallPreview       contractCallPhase = "preview"
	contractCallReinforced    contractCallPhase = "reinforced"
	contractCallPassword      contractCallPhase = "password"
	contractCallSubmitting    contractCallPhase = "submitting"
	contractCallTracking      contractCallPhase = "tracking"
	contractCallComplete      contractCallPhase = "complete"
)

type contractCallKeyMap struct {
	Up      key.Binding
	Down    key.Binding
	Prev    key.Binding
	Next    key.Binding
	Approve key.Binding
	Back    key.Binding
}

func newContractCallKeyMap() contractCallKeyMap {
	return contractCallKeyMap{
		Up:      key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "previous network")),
		Down:    key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "next network")),
		Prev:    key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "previous step")),
		Next:    key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "next step")),
		Approve: key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "approve and send")),
		Back:    key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
	}
}

func (keys contractCallKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{keys.Prev, keys.Next, keys.Approve, keys.Back}
}

func (keys contractCallKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{keys.Up, keys.Down}, {keys.Prev, keys.Next, keys.Approve, keys.Back}}
}

type contractCallState struct {
	phase      contractCallPhase
	account    wallet.AccountSummary
	networks   []nativeNetworkChoice
	selected   int
	contract   common.Address
	abiJSON    string
	method     string
	args       string
	value      *big.Int
	prepared   *evm.PreparedNativeTransfer
	result     *evm.ExecutionResult
	tracking   *evm.TrackingResult
	generation uint64
	planGen    uint64
	cancel     context.CancelFunc
	err        string
	inputs     map[string]*textinput.Model
	order      []string
	keys       contractCallKeyMap
	help       help.Model
}

type contractCallPreparedMsg struct {
	generation uint64
	prepared   *evm.PreparedNativeTransfer
	err        error
}

type contractCallSubmittedMsg struct {
	generation uint64
	result     evm.ExecutionResult
	err        error
}

type contractCallTrackTickMsg struct {
	generation uint64
}

func (model *CLIModel) initContractCall() {
	if model.selectedAccount == nil || model.currentConfig == nil {
		return
	}
	choices := make([]nativeNetworkChoice, 0, len(model.currentConfig.Networks))
	for key, network := range model.currentConfig.Networks {
		if network.IsActive && network.ChainID > 0 {
			choices = append(choices, nativeNetworkChoice{key: key, network: network})
		}
	}
	sort.Slice(choices, func(left, right int) bool { return choices[left].key < choices[right].key })
	state := &contractCallState{
		phase:    contractCallSelectNetwork,
		account:  *model.selectedAccount,
		networks: choices,
		planGen:  1,
		inputs:   make(map[string]*textinput.Model),
		keys:     newContractCallKeyMap(),
		help:     help.New(),
	}
	state.order = []string{"contract", "abi", "method", "args", "value"}
	for _, field := range state.order {
		input := textinput.New()
		switch field {
		case "contract":
			input.Placeholder = "0x contract address"
			input.CharLimit = 42
			input.Width = 44
		case "abi":
			input.Placeholder = "ABI JSON array"
			input.CharLimit = evm.MaxCallABIBytes
			input.Width = 110
		case "method":
			input.Placeholder = "method name (e.g. deposit)"
			input.CharLimit = 128
			input.Width = 40
		case "args":
			input.Placeholder = "arguments JSON array (e.g. [\"0x…\",\"7\"])"
			input.CharLimit = evm.MaxCallArgsJSON
			input.Width = 110
		case "value":
			input.Placeholder = "Native value in base units (0 allowed)"
			input.CharLimit = 96
			input.Width = 32
		}
		state.inputs[field] = &input
	}
	state.help.Width = max(40, model.width-6)
	if len(choices) == 0 {
		state.err = "No active network with chain ID is configured"
	}
	model.contractCall = state
	model.currentView = constants.ContractCallView
}

func (model *CLIModel) updateContractCall(message tea.Msg) (tea.Model, tea.Cmd) {
	state := model.contractCall
	if state == nil {
		model.currentView = constants.WalletDetailsView
		return model, nil
	}
	switch message := message.(type) {
	case contractCallPreparedMsg:
		if message.generation != state.generation || state.phase != contractCallPreview {
			return model, nativeCancelPreparedResultCommand(model.contractCallEngine(), message.prepared)
		}
		state.cancel = nil
		if message.err != nil || message.prepared == nil {
			state.err = safeError(message.err)
			state.phase = contractCallValue
			return model, nil
		}
		state.prepared = message.prepared
		return model, nil
	case contractCallSubmittedMsg:
		if message.generation != state.generation || state.phase != contractCallSubmitting {
			return model, nil
		}
		state.cancel = nil
		if message.err != nil {
			state.err = safeError(message.err)
			state.phase = contractCallPassword
			return model, nil
		}
		state.result = &message.result
		state.phase = contractCallTracking
		return model, model.contractCallTrackCommand(state)
	case contractCallTrackTickMsg:
		if message.generation != state.generation || state.phase != contractCallTracking {
			return model, nil
		}
		return model, model.contractCallTrackCommand(state)
	case tea.KeyMsg:
		if key.Matches(message, state.keys.Back) {
			model.clearContractCall()
			model.currentView = constants.WalletDetailsView
			model.refreshWalletDetailsComponents()
			return model, nil
		}
		if state.phase == contractCallSubmitting || state.phase == contractCallComplete {
			return model, nil
		}
		switch state.phase {
		case contractCallSelectNetwork:
			if len(state.networks) == 0 {
				return model, nil
			}
			if key.Matches(message, state.keys.Down) {
				state.selected = (state.selected + 1) % len(state.networks)
				return model, nil
			}
			if key.Matches(message, state.keys.Up) {
				state.selected = (state.selected + len(state.networks) - 1) % len(state.networks)
				return model, nil
			}
			if key.Matches(message, state.keys.Next) {
				state.phase = contractCallContract
				state.inputs["contract"].Focus()
				return model, nil
			}
		case contractCallContract, contractCallABI, contractCallMethod, contractCallArgs, contractCallValue:
			field := contractCallFieldForPhase(state.phase)
			if key.Matches(message, state.keys.Prev) {
				state.inputs[field].Blur()
				state.phase = contractCallPreviousPhase(state.phase)
				state.inputs[contractCallFieldForPhase(state.phase)].Focus()
				return model, nil
			}
			if message.String() == "enter" {
				if err := model.commitContractCallField(state, field); err != nil {
					state.err = safeError(err)
					return model, nil
				}
				state.err = ""
				state.inputs[field].Blur()
				if state.phase == contractCallValue {
					return model, model.startContractCallPrepare()
				}
				state.phase = contractCallNextPhase(state.phase)
				state.inputs[contractCallFieldForPhase(state.phase)].Focus()
				return model, nil
			}
			updated, command := state.inputs[field].Update(message)
			state.inputs[field] = &updated
			return model, command
		case contractCallPreview:
			if key.Matches(message, state.keys.Prev) {
				state.phase = contractCallValue
				state.inputs["value"].Focus()
				return model, nil
			}
			if key.Matches(message, state.keys.Next) || key.Matches(message, state.keys.Approve) {
				if state.prepared != nil && len(state.prepared.Findings()) > 0 {
					for _, finding := range state.prepared.Findings() {
						if finding.Severity == evm.RiskSeverityCritical {
							state.phase = contractCallReinforced
							confirm := textinput.New()
							confirm.Placeholder = "Type APPROVE"
							confirm.CharLimit = 16
							confirm.Width = 20
							state.inputs["confirm"] = &confirm
							state.inputs["confirm"].Focus()
							return model, nil
						}
					}
				}
				state.phase = contractCallPassword
				password := textinput.New()
				password.Placeholder = "Storage password"
				password.EchoMode = textinput.EchoPassword
				password.EchoCharacter = '•'
				password.CharLimit = constants.PasswordCharLimit
				password.Width = constants.PasswordWidth
				state.inputs["password"] = &password
				state.inputs["password"].Focus()
				return model, nil
			}
		case contractCallReinforced:
			if message.String() == "enter" {
				if state.inputs["confirm"].Value() != "APPROVE" {
					state.err = "Type APPROVE exactly to reinforce this contract call"
					return model, nil
				}
				state.inputs["confirm"].SetValue("")
				state.phase = contractCallPassword
				password := textinput.New()
				password.Placeholder = "Storage password"
				password.EchoMode = textinput.EchoPassword
				password.EchoCharacter = '•'
				password.CharLimit = constants.PasswordCharLimit
				password.Width = constants.PasswordWidth
				state.inputs["password"] = &password
				state.inputs["password"].Focus()
				return model, nil
			}
			updated, command := state.inputs["confirm"].Update(message)
			state.inputs["confirm"] = &updated
			return model, command
		case contractCallPassword:
			if message.String() == "enter" {
				password := []byte(state.inputs["password"].Value())
				authorizer := model.transactionAuthorizer
				accountID := state.account.AccountID
				if authorizer == nil || (len(password) == 0 && !authorizer.HasActiveSession(context.Background(), accountID)) {
					state.err = "Password is required unless a temporary session is active"
					state.inputs["password"].Focus()
					return model, nil
				}
				state.inputs["password"].SetValue("")
				state.err = ""
				state.generation = model.nextContractCallGeneration()
				generation := state.generation
				engine := model.contractCallEngine()
				prepared := state.prepared
				confirmationTarget := uint64(state.networks[state.selected].network.ConfirmationTarget)
				if confirmationTarget == 0 {
					confirmationTarget = 12
				}
				ctx, cancel := context.WithCancel(context.Background())
				state.cancel = cancel
				state.phase = contractCallSubmitting
				return model, func() tea.Msg {
					defer clear(password)
					var result evm.ExecutionResult
					err := authorizer.Authorize(ctx, accountID, password, func(handle wallet.CapabilityHandle, epoch uint64) error {
						riskLevel := evm.RiskNormal
						confirmationLevel := evm.ConfirmationStandard
						for _, finding := range prepared.Findings() {
							if finding.Severity == evm.RiskSeverityCritical {
								riskLevel = evm.RiskCritical
								confirmationLevel = evm.ConfirmationReinforced
							}
						}
						var operationErr error
						result, operationErr = engine.ApproveSignAndBroadcast(ctx, handle, prepared, evm.ApprovalRequest{
							AuthorizationEpoch: epoch, RiskLevel: riskLevel, ConfirmationLevel: confirmationLevel, ConfirmationTarget: confirmationTarget,
						})
						return operationErr
					})
					return contractCallSubmittedMsg{generation: generation, result: result, err: err}
				}
			}
			updated, command := state.inputs["password"].Update(message)
			state.inputs["password"] = &updated
			return model, command
		case contractCallTracking:
			if message.String() == "b" && state.result != nil {
				state.generation = model.nextContractCallGeneration()
				generation := state.generation
				engine := model.contractCallEngine()
				transactionID := state.result.TransactionID
				ctx, cancel := context.WithCancel(context.Background())
				state.cancel = cancel
				state.phase = contractCallSubmitting
				return model, func() tea.Msg {
					result, err := engine.Rebroadcast(ctx, transactionID)
					return contractCallSubmittedMsg{generation: generation, result: result, err: err}
				}
			}
		case contractCallComplete:
			if message.String() == "enter" {
				model.clearContractCall()
				model.currentView = constants.WalletDetailsView
				model.refreshWalletDetailsComponents()
				return model, nil
			}
		}
	}
	return model, nil
}

func contractCallFieldForPhase(phase contractCallPhase) string {
	switch phase {
	case contractCallContract:
		return "contract"
	case contractCallABI:
		return "abi"
	case contractCallMethod:
		return "method"
	case contractCallArgs:
		return "args"
	case contractCallValue:
		return "value"
	default:
		return ""
	}
}

func contractCallNextPhase(phase contractCallPhase) contractCallPhase {
	switch phase {
	case contractCallContract:
		return contractCallABI
	case contractCallABI:
		return contractCallMethod
	case contractCallMethod:
		return contractCallArgs
	case contractCallArgs:
		return contractCallValue
	default:
		return phase
	}
}

func contractCallPreviousPhase(phase contractCallPhase) contractCallPhase {
	switch phase {
	case contractCallABI:
		return contractCallContract
	case contractCallMethod:
		return contractCallABI
	case contractCallArgs:
		return contractCallMethod
	case contractCallValue:
		return contractCallArgs
	default:
		return phase
	}
}

func (model *CLIModel) commitContractCallField(state *contractCallState, field string) error {
	value := state.inputs[field].Value()
	switch field {
	case "contract":
		value = strings.TrimSpace(value)
		if !common.IsHexAddress(value) || len(value) != 42 || common.HexToAddress(value) == (common.Address{}) {
			return fmt.Errorf("contract must be a non-zero 20-byte EVM address")
		}
		state.contract = common.HexToAddress(value)
	case "abi":
		state.abiJSON = value
	case "method":
		state.method = strings.TrimSpace(value)
		if state.method == "" {
			return fmt.Errorf("method name is required")
		}
	case "args":
		state.args = value
	case "value":
		value = strings.TrimSpace(value)
		if value == "" {
			value = "0"
		}
		amount, ok := new(big.Int).SetString(value, 10)
		if !ok || amount.Sign() < 0 || amount.BitLen() > 256 {
			return fmt.Errorf("value must be an exact unsigned base-10 integer")
		}
		state.value = amount
	}
	return nil
}

func (model *CLIModel) startContractCallPrepare() tea.Cmd {
	state := model.contractCall
	if state == nil || model.transactionEngineFactory == nil {
		return nil
	}
	state.phase = contractCallPreview
	state.generation = model.nextContractCallGeneration()
	generation := state.generation
	choice := state.networks[state.selected]
	account := state.account
	contract := state.contract
	abiJSON := state.abiJSON
	method := state.method
	args := state.args
	value := new(big.Int).Set(state.value)
	planGen := state.planGen
	ctx, cancel := context.WithCancel(context.Background())
	state.cancel = cancel
	return func() tea.Msg {
		engine, err := model.transactionEngineFactory(ctx, choice.network)
		if err != nil {
			return contractCallPreparedMsg{generation: generation, err: err}
		}
		operationID, err := evm.NewOperationID()
		if err != nil {
			return contractCallPreparedMsg{generation: generation, err: err}
		}
		prepared, prepareErr := engine.PrepareContractCall(ctx, evm.PrepareContractCallRequest{
			OperationID: operationID, PlanGeneration: planGen, AccountID: account.AccountID,
			ChainID: uint64(choice.network.ChainID), From: common.HexToAddress(account.Address),
			Contract: contract, Value: value, ABI: []byte(abiJSON), ABISource: evm.ABISourceLocal,
			Method: method, Args: []byte(args),
		})
		if prepareErr == nil && ctx.Err() != nil && prepared != nil {
			_ = engine.CancelPrepared(context.Background(), prepared, "user_cancelled")
			return contractCallPreparedMsg{generation: generation, err: ctx.Err()}
		}
		model.contractCallEngineValue = engine
		return contractCallPreparedMsg{generation: generation, prepared: prepared, err: prepareErr}
	}
}

func (model *CLIModel) contractCallTrackCommand(state *contractCallState) tea.Cmd {
	if state == nil || state.result == nil || model.contractCallEngineValue == nil {
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	state.cancel = cancel
	generation := state.generation
	engine := model.contractCallEngineValue
	transactionID := state.result.TransactionID
	confirmationTarget := uint64(state.networks[state.selected].network.ConfirmationTarget)
	if confirmationTarget == 0 {
		confirmationTarget = 12
	}
	return func() tea.Msg {
		result, err := engine.TrackTransaction(ctx, transactionID, confirmationTarget, time.Now().UTC())
		if err != nil {
			return contractCallTrackTickMsg{generation: generation}
		}
		if result.State == evm.TransactionConfirmed || result.State == evm.TransactionReverted || result.State == evm.TransactionEffectUnverified {
			state.tracking = &result
			state.phase = contractCallComplete
			return contractCallSubmittedMsg{generation: generation, result: evm.ExecutionResult{TransactionID: transactionID, Hash: state.result.Hash}, err: nil}
		}
		state.tracking = &result
		return contractCallTrackTickMsg{generation: generation}
	}
}

func (model *CLIModel) viewContractCall() string {
	state := model.contractCall
	if state == nil {
		return "Contract call is unavailable."
	}
	title := lipgloss.NewStyle().Bold(true).Render("Contract Call (known ABI)")
	var builder strings.Builder
	builder.WriteString(title + "\n\n")
	if state.err != "" {
		builder.WriteString(model.styles.ErrorStyle.Render(safeInline(state.err)))
		builder.WriteString("\n\n")
	}
	switch state.phase {
	case contractCallSelectNetwork:
		builder.WriteString("Select network:\n")
		for index, choice := range state.networks {
			prefix := "  "
			if index == state.selected {
				prefix = "> "
			}
			_, _ = fmt.Fprintf(&builder, "%s%s (chain %d)\n", prefix, safeShort(choice.network.Name), choice.network.ChainID)
		}
		builder.WriteString("\nEnter: select • Esc: back")
	case contractCallContract, contractCallABI, contractCallMethod, contractCallArgs, contractCallValue:
		field := contractCallFieldForPhase(state.phase)
		_, _ = fmt.Fprintf(&builder, "%s:\n%s\n\nEnter: continue • Esc: back", state.inputs[field].Placeholder, state.inputs[field].View())
	case contractCallPreview:
		if state.prepared == nil {
			builder.WriteString("Preparing contract call...")
			break
		}
		plan := state.prepared.Plan()
		preview := plan.ContractCallPreview()
		_, _ = fmt.Fprintf(&builder, "Contract: %s\nMethod: %s\nABI source: %s\nABI hash: %s\nValue: %s\nSimulated output: %s\nCalldata: 0x%x\n",
			safeShort(preview.Contract.Hex()), safeShort(preview.Method), safeShort(string(preview.ABISource)), safeShort(preview.ABIHash.Hex()), preview.Value, safeInline(preview.Output), preview.Calldata)
		_, _ = fmt.Fprintf(&builder, "Nonce: %d\nGas limit: %d\n", plan.Transaction().Nonce(), plan.Transaction().Gas())
		for _, finding := range state.prepared.Findings() {
			_, _ = fmt.Fprintf(&builder, "Risk [%s]: %s (%s)\n", safeShort(string(finding.Severity)), safeShort(string(finding.ID)), safeShort(finding.Subject.Hex()))
		}
		_, _ = fmt.Fprintf(&builder, "\nPlan: 0x%x\nDigest: 0x%x\n", plan.PlanHash(), plan.TransactionDigest())
		builder.WriteString("\nEnter: approve exact digest • Esc: cancel")
	case contractCallReinforced:
		builder.WriteString("Critical: this call moves native value or grants broad control. Verify the contract independently.\n\nType APPROVE to perform the second confirmation:\n" + state.inputs["confirm"].View() + "\n\nEnter: continue • Esc: cancel")
	case contractCallPassword:
		builder.WriteString("Enter storage password to sign this approved digest:\n" + state.inputs["password"].View() + "\n\nEnter: sign and broadcast • Esc: cancel")
	case contractCallSubmitting:
		builder.WriteString("Signing approved digest and broadcasting exact bytes...")
	case contractCallTracking:
		_, _ = fmt.Fprintf(&builder, "Transaction: %s\nTracking receipt and confirmations", safeShort(state.result.Hash.Hex()))
		if state.tracking != nil {
			_, _ = fmt.Fprintf(&builder, "\nState: %s\nConfirmations: %d", safeShort(string(state.tracking.State)), state.tracking.Confirmations)
		}
		builder.WriteString("\n\nB: rebroadcast persisted bytes • Esc: return; tracking resumes after restart")
	case contractCallComplete:
		_, _ = fmt.Fprintf(&builder, "Transaction %s\nTransaction hash: %s\n\nEnter or Esc: return", safeShort(string(state.tracking.State)), safeShort(state.result.Hash.Hex()))
	}
	return builder.String()
}

func (model *CLIModel) clearContractCall() {
	model.contractCallGeneration++
	if model.contractCall != nil && model.contractCall.cancel != nil {
		model.contractCall.cancel()
	}
	model.contractCall = nil
	model.contractCallEngineValue = nil
}

func (model *CLIModel) nextContractCallGeneration() uint64 {
	model.contractCallGeneration++
	return model.contractCallGeneration
}

func (model *CLIModel) contractCallEngine() TransactionEngine {
	return model.contractCallEngineValue
}
