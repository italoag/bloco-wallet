package ui

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"strings"
	"time"

	"blocowallet/internal/constants"
	"blocowallet/internal/evm"
	"blocowallet/internal/wallet"
	"blocowallet/pkg/localization"

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
		Up:      key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", localization.Get("eip712_prev_network"))),
		Down:    key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", localization.Get("eip712_next_network"))),
		Prev:    key.NewBinding(key.WithKeys("p"), key.WithHelp("p", localization.Get("sign_prev_step"))),
		Next:    key.NewBinding(key.WithKeys("n"), key.WithHelp("n", localization.Get("sign_next_step"))),
		Approve: key.NewBinding(key.WithKeys("a"), key.WithHelp("a", localization.Get("call_approve_send"))),
		Back:    key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", localization.Get("hist_back"))),
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
			input.Placeholder = localization.Get("call_addr_placeholder")
			input.CharLimit = 42
			input.Width = 44
		case "abi":
			input.Placeholder = localization.Get("call_abi_placeholder")
			input.CharLimit = evm.MaxCallABIBytes
			input.Width = 110
		case "method":
			input.Placeholder = localization.Get("call_method_placeholder")
			input.CharLimit = 128
			input.Width = 40
		case "args":
			input.Placeholder = localization.Get("call_args_placeholder")
			input.CharLimit = evm.MaxCallArgsJSON
			input.Width = 110
		case "value":
			input.Placeholder = localization.Get("call_value_placeholder")
			input.CharLimit = 96
			input.Width = 32
		}
		state.inputs[field] = &input
	}
	state.help.Width = max(40, model.width-6)
	model.credentialUseKeePass = false
	if len(choices) == 0 {
		state.err = localization.Get("eip712_no_network")
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
							confirm.Placeholder = localization.Get("call_approve_placeholder")
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
				password.Placeholder = localization.Get("sign_storage_password_placeholder")
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
					state.err = localization.Get("call_err_type_approve")
					return model, nil
				}
				state.inputs["confirm"].SetValue("")
				state.phase = contractCallPassword
				password := textinput.New()
				password.Placeholder = localization.Get("sign_storage_password_placeholder")
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
			if message.String() == "ctrl+k" && model.credentialToggleEligible(state.account) {
				model.credentialUseKeePass = !model.credentialUseKeePass
				if model.credentialUseKeePass {
					state.inputs["password"].SetValue("")
				}
				return model, nil
			}
			if message.String() == "enter" {
				password := []byte(state.inputs["password"].Value())
				authorizer := model.transactionAuthorizer
				accountID := state.account.AccountID
				useKeePass := model.credentialUseKeePass && model.credentialToggleEligible(state.account)
				if authorizer == nil || (len(password) == 0 && !useKeePass && !authorizer.HasActiveSession(context.Background(), accountID)) {
					state.err = localization.Get("call_err_password_session")
					state.inputs["password"].Focus()
					return model, nil
				}
				state.inputs["password"].SetValue("")
				state.err = ""
				submit := func() tea.Cmd {
					state.generation = model.nextContractCallGeneration()
					generation := state.generation
					engine := model.contractCallEngine()
					prepared := state.prepared
					confirmationTarget := uint64(state.networks[state.selected].network.ConfirmationTarget)
					if confirmationTarget == 0 {
						confirmationTarget = 12
					}
					authCtx, cancel, op := model.credentialWorkerContext(canonicalImportTimeout)
					state.cancel = cancel
					state.phase = contractCallSubmitting
					worker := func() tea.Msg {
						defer clear(password)
						defer cancel()
						if op != nil {
							defer op.Close()
						}
						var result evm.ExecutionResult
						err := runWithAccountCredential(authCtx, op, accountID, password, useKeePass, func(resolved []byte) error {
							return authorizer.Authorize(authCtx, accountID, resolved, func(handle wallet.CapabilityHandle, epoch uint64) error {
								riskLevel := evm.RiskNormal
								confirmationLevel := evm.ConfirmationStandard
								for _, finding := range prepared.Findings() {
									if finding.Severity == evm.RiskSeverityCritical {
										riskLevel = evm.RiskCritical
										confirmationLevel = evm.ConfirmationReinforced
									}
								}
								var operationErr error
								result, operationErr = engine.ApproveSignAndBroadcast(authCtx, handle, prepared, evm.ApprovalRequest{
									AuthorizationEpoch: epoch, RiskLevel: riskLevel, ConfirmationLevel: confirmationLevel, ConfirmationTarget: confirmationTarget,
								})
								return operationErr
							})
						})
						return contractCallSubmittedMsg{generation: generation, result: result, err: err}
					}
					return tea.Sequence(worker, func() tea.Msg { return credentialOpDoneMsg{op: op} })
				}
				return model, model.submitWithCredentialCancel(useKeePass, submit, func() { clear(password) })
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
			return errors.New(localization.Get("call_validate_address"))
		}
		state.contract = common.HexToAddress(value)
	case "abi":
		state.abiJSON = value
	case "method":
		state.method = strings.TrimSpace(value)
		if state.method == "" {
			return errors.New(localization.Get("call_validate_method"))
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
			return errors.New(localization.Get("call_validate_value"))
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
		return localization.Get("call_unavailable")
	}
	title := lipgloss.NewStyle().Bold(true).Render(localization.Get("call_title"))
	var builder strings.Builder
	builder.WriteString(title + "\n\n")
	if state.err != "" {
		builder.WriteString(model.styles.ErrorStyle.Render(safeInline(state.err)))
		builder.WriteString("\n\n")
	}
	switch state.phase {
	case contractCallSelectNetwork:
		builder.WriteString(localization.Get("call_select_network") + "\n")
		for index, choice := range state.networks {
			prefix := "  "
			if index == state.selected {
				prefix = "> "
			}
			_, _ = builder.WriteString(prefix + safeShort(choice.network.Name) + localization.T("call_choice_chain", map[string]interface{}{"ID": choice.network.ChainID}) + "\n")
		}
		builder.WriteString("\n" + localization.Get("call_enter_select_esc"))
	case contractCallContract, contractCallABI, contractCallMethod, contractCallArgs, contractCallValue:
		field := contractCallFieldForPhase(state.phase)
		_, _ = fmt.Fprintf(&builder, "%s:\n%s\n\n%s", state.inputs[field].Placeholder, state.inputs[field].View(), localization.Get("call_enter_continue_esc"))
	case contractCallPreview:
		if state.prepared == nil {
			builder.WriteString(localization.Get("call_preparing"))
			break
		}
		plan := state.prepared.Plan()
		preview := plan.ContractCallPreview()
		builder.WriteString(localization.T("call_preview_body", map[string]interface{}{
			"Contract": safeShort(preview.Contract.Hex()), "Method": safeShort(preview.Method),
			"Source": safeShort(string(preview.ABISource)), "Hash": safeShort(preview.ABIHash.Hex()),
			"Value": preview.Value, "Output": safeInline(preview.Output),
		}))
		builder.WriteString(renderCalldataLine(preview.Calldata) + "\n")
		_, _ = builder.WriteString(localization.T("call_nonce_gas", map[string]interface{}{"Nonce": plan.Transaction().Nonce(), "Gas": plan.Transaction().Gas()}))
		for _, finding := range state.prepared.Findings() {
			_, _ = builder.WriteString(localization.T("call_risk_line", map[string]interface{}{"Severity": safeShort(string(finding.Severity)), "ID": safeShort(string(finding.ID)), "Subject": safeShort(finding.Subject.Hex())}))
		}
		_, _ = builder.WriteString("\n" + localization.T("call_plan_digest", map[string]interface{}{"Plan": fmt.Sprintf("%x", plan.PlanHash()), "Digest": fmt.Sprintf("%x", plan.TransactionDigest())}))
		builder.WriteString("\n" + localization.Get("call_approve_intent"))
	case contractCallReinforced:
		builder.WriteString(localization.Get("call_critical_approve") + "\n" + state.inputs["confirm"].View() + "\n\n" + localization.Get("call_enter_continue_esc"))
	case contractCallPassword:
		if state.account.SignerKind == wallet.SignerKindSoftware {
			builder.WriteString(localization.Get("call_enter_password") + "\n" + state.inputs["password"].View() + "\n" + model.credentialMethodLabel(model.credentialToggleEligible(state.account)) + "\n\n" + localization.Get("call_enter_sign_broadcast"))
		} else {
			builder.WriteString(localization.Get("call_external_review"))
		}
	case contractCallSubmitting:
		builder.WriteString(localization.Get("call_submitting"))
	case contractCallTracking:
		_, _ = builder.WriteString(localization.T("call_tracking_line", map[string]interface{}{"Hash": safeShort(state.result.Hash.Hex())}))
		if state.tracking != nil {
			_, _ = builder.WriteString("\n" + localization.T("call_tracking_state", map[string]interface{}{"State": safeShort(string(state.tracking.State)), "Count": state.tracking.Confirmations}))
		}
		builder.WriteString("\n\n" + localization.Get("call_rebroadcast_hint"))
	case contractCallComplete:
		_, _ = builder.WriteString(localization.T("call_final_line", map[string]interface{}{"State": safeShort(string(state.tracking.State)), "Hash": safeShort(state.result.Hash.Hex())}))
	}
	return builder.String()
}

func (model *CLIModel) clearContractCall() {
	model.credentialUseKeePass = false

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
