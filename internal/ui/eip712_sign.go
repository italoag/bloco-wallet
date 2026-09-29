package ui

import (
	"context"
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

type eip712SignPhase string

const (
	eip712SignSelectNetwork eip712SignPhase = "select_network"
	eip712SignEntry         eip712SignPhase = "entry"
	eip712SignPreview       eip712SignPhase = "preview"
	eip712SignPassword      eip712SignPhase = "password"
	eip712SignSubmitting    eip712SignPhase = "submitting"
	eip712SignComplete      eip712SignPhase = "complete"
)

type eip712SignKeyMap struct {
	Up      key.Binding
	Down    key.Binding
	Prev    key.Binding
	Next    key.Binding
	Approve key.Binding
	Back    key.Binding
}

func newEIP712SignKeyMap() eip712SignKeyMap {
	return eip712SignKeyMap{
		Up:      key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", localization.Get("eip712_prev_network"))),
		Down:    key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", localization.Get("eip712_next_network"))),
		Prev:    key.NewBinding(key.WithKeys("p"), key.WithHelp("p", localization.Get("sign_prev_step"))),
		Next:    key.NewBinding(key.WithKeys("n"), key.WithHelp("n", localization.Get("sign_next_step"))),
		Approve: key.NewBinding(key.WithKeys("a"), key.WithHelp("a", localization.Get("sign_approve"))),
		Back:    key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", localization.Get("hist_back"))),
	}
}

func (keys eip712SignKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{keys.Prev, keys.Next, keys.Approve, keys.Back}
}

func (keys eip712SignKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{keys.Up, keys.Down}, {keys.Prev, keys.Next, keys.Approve, keys.Back}}
}

type eip712SignState struct {
	phase      eip712SignPhase
	account    wallet.AccountSummary
	service    MessageSigningService
	networks   []nativeNetworkChoice
	selected   int
	typedData  textinput.Model
	password   textinput.Model
	prepared   *evm.PreparedEIP712Sign
	result     *evm.PersonalSignResult
	cancel     context.CancelFunc
	generation uint64
	err        string
	keys       eip712SignKeyMap
	help       help.Model
}

type eip712SignResultMsg struct {
	generation uint64
	result     evm.PersonalSignResult
	err        error
}

func (model *CLIModel) initEIP712Sign(service MessageSigningService) {
	if service == nil || model.selectedAccount == nil {
		return
	}
	choices := make([]nativeNetworkChoice, 0, len(model.currentConfig.Networks))
	if model.currentConfig != nil {
		for key, network := range model.currentConfig.Networks {
			if network.IsActive && network.ChainID > 0 {
				choices = append(choices, nativeNetworkChoice{key: key, network: network})
			}
		}
	}
	sort.Slice(choices, func(left, right int) bool { return choices[left].key < choices[right].key })
	state := &eip712SignState{
		phase:     eip712SignSelectNetwork,
		account:   *model.selectedAccount,
		service:   service,
		networks:  choices,
		typedData: textinput.New(),
		password:  textinput.New(),
		keys:      newEIP712SignKeyMap(),
		help:      help.New(),
	}
	state.typedData.Placeholder = localization.Get("eip712_placeholder")
	state.typedData.CharLimit = evm.MaxEIP712TypedDataBytes
	state.typedData.Width = 110
	state.password.Placeholder = localization.Get("sign_storage_password_placeholder")
	state.password.CharLimit = constants.PasswordCharLimit
	state.password.Width = constants.PasswordWidth
	state.password.EchoMode = textinput.EchoPassword
	state.password.EchoCharacter = '•'
	state.help.Width = max(40, model.width-6)
	model.credentialUseKeePass = false
	if len(choices) == 0 {
		state.err = localization.Get("eip712_no_network")
	}
	model.eip712Sign = state
	model.currentView = constants.EIP712SignView
}

func (model *CLIModel) updateEIP712Sign(message tea.Msg) (tea.Model, tea.Cmd) {
	state := model.eip712Sign
	if state == nil {
		model.currentView = constants.WalletDetailsView
		return model, nil
	}
	switch message := message.(type) {
	case eip712SignResultMsg:
		if message.generation != state.generation {
			return model, nil
		}
		if state.cancel != nil {
			state.cancel()
			state.cancel = nil
		}
		state.password.SetValue("")
		if message.err != nil {
			state.phase = eip712SignPreview
			state.err = safeError(message.err)
			return model, nil
		}
		state.result = &message.result
		state.err = ""
		state.phase = eip712SignComplete
		return model, nil
	case tea.KeyMsg:
		if key.Matches(message, state.keys.Back) {
			model.clearEIP712Sign()
			model.currentView = constants.WalletDetailsView
			model.refreshWalletDetailsComponents()
			return model, nil
		}
		if state.phase == eip712SignComplete || state.phase == eip712SignSubmitting {
			return model, nil
		}
		switch state.phase {
		case eip712SignSelectNetwork:
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
				state.phase = eip712SignEntry
				state.typedData.Focus()
				return model, nil
			}
		case eip712SignEntry:
			if key.Matches(message, state.keys.Prev) {
				state.phase = eip712SignSelectNetwork
				return model, nil
			}
			if key.Matches(message, state.keys.Next) {
				value := state.typedData.Value()
				if len(value) > evm.MaxEIP712TypedDataBytes {
					state.err = localization.Get("eip712_err_too_long")
					return model, nil
				}
				prepared, err := evm.PrepareEIP712Sign(evm.PrepareEIP712SignRequest{
					AccountID: state.account.AccountID, Signer: common.HexToAddress(state.account.Address),
					ChainID: uint64(state.networks[state.selected].network.ChainID), TypedData: []byte(value), Origin: localPersonalSignOrigin,
				})
				if err != nil {
					state.err = safeError(err)
					return model, nil
				}
				state.prepared = prepared
				state.err = ""
				state.phase = eip712SignPreview
				return model, nil
			}
			var command tea.Cmd
			state.typedData, command = state.typedData.Update(message)
			return model, command
		case eip712SignPreview:
			if key.Matches(message, state.keys.Prev) {
				state.phase = eip712SignEntry
				return model, nil
			}
			if key.Matches(message, state.keys.Next) || key.Matches(message, state.keys.Approve) {
				state.password.Focus()
				state.phase = eip712SignPassword
				return model, nil
			}
		case eip712SignPassword:
			if key.Matches(message, state.keys.Prev) {
				state.password.SetValue("")
				state.phase = eip712SignPreview
				return model, nil
			}
			if message.String() == "ctrl+k" && model.credentialToggleEligible(state.account) {
				model.credentialUseKeePass = !model.credentialUseKeePass
				if model.credentialUseKeePass {
					state.password.SetValue("")
				}
				return model, nil
			}
			if message.String() == "enter" {
				password := []byte(state.password.Value())
				useKeePass := model.credentialUseKeePass && model.credentialToggleEligible(state.account)
				if len(password) == 0 && !useKeePass && (model.transactionAuthorizer == nil || !model.transactionAuthorizer.HasActiveSession(context.Background(), state.account.AccountID)) {
					state.err = localization.Get("sign_err_password_required")
					return model, nil
				}
				state.password.SetValue("")
				state.err = ""
				submit := func() tea.Cmd {
					state.phase = eip712SignSubmitting
					model.eip712SignGeneration++
					state.generation = model.eip712SignGeneration
					generation := state.generation
					service := state.service
					authorizer := model.transactionAuthorizer
					prepared := state.prepared
					accountID := state.account.AccountID
					authCtx, cancel, op := model.credentialWorkerContext(5 * time.Minute)
					state.cancel = cancel
					worker := func() tea.Msg {
						defer clear(password)
						defer cancel()
						if op != nil {
							defer op.Close()
						}
						var result evm.PersonalSignResult
						operationErr := runWithAccountCredential(authCtx, op, accountID, password, useKeePass, func(resolved []byte) error {
							return authorizer.Authorize(authCtx, accountID, resolved, func(handle wallet.CapabilityHandle, epoch uint64) error {
								var signErr error
								result, signErr = service.ApproveAndSignEIP712(authCtx, handle, prepared, evm.PersonalSignApprovalRequest{
									AuthorizationEpoch: epoch, ConfirmedIntentHash: prepared.Preview().IntentHash, ConfirmationLevel: evm.ConfirmationReinforced,
								})
								return signErr
							})
						})
						return eip712SignResultMsg{generation: generation, result: result, err: operationErr}
					}
					return tea.Sequence(worker, func() tea.Msg { return credentialOpDoneMsg{op: op} })
				}
				return model, model.submitWithCredentialCancel(useKeePass, submit, func() { clear(password) })
			}
			var command tea.Cmd
			state.password, command = state.password.Update(message)
			return model, command
		}
	}
	return model, nil
}

func (model *CLIModel) viewEIP712Sign() string {
	state := model.eip712Sign
	if state == nil {
		return localization.Get("eip712_unavailable")
	}
	title := lipgloss.NewStyle().Bold(true).Render(localization.Get("eip712_title"))
	var content strings.Builder
	content.WriteString(title)
	switch state.phase {
	case eip712SignSelectNetwork:
		content.WriteString("\n\n" + localization.Get("account_prefix") + safeShort(state.account.Address) + "\n" + localization.Get("eip712_select_chain"))
		for index, choice := range state.networks {
			marker := "  "
			if index == state.selected {
				marker = "> "
			}
			_, _ = content.WriteString("\n" + marker + safeShort(choice.key) + localization.T("eip712_choice_line", map[string]interface{}{"ID": choice.network.ChainID}))
		}
		content.WriteString("\n\n" + localization.Get("eip712_press_n_continue"))
	case eip712SignEntry:
		content.WriteString("\n\n" + localization.Get("eip712_chain_prefix") + safeShort(state.networks[state.selected].key) + "\n\n" + state.typedData.View() + "\n\n" + localization.Get("eip712_press_n_preview"))
	case eip712SignPreview:
		preview := state.prepared.Preview()
		_, _ = content.WriteString("\n\n" + localization.T("eip712_preview_body", map[string]interface{}{
			"Chain": safeShort(state.networks[state.selected].key), "ID": preview.DomainChainID,
			"Digest": preview.Digest.Hex(), "Intent": preview.IntentHash.Hex(),
		}) + "\n\n" + preview.Rendered + "\n\n" + localization.Get("eip712_preview_hint"))
	case eip712SignPassword:
		if state.account.SignerKind == wallet.SignerKindSoftware {
			content.WriteString("\n\n" + state.password.View() + "\n" + model.credentialMethodLabel(model.credentialToggleEligible(state.account)))
		} else {
			content.WriteString("\n\n" + localization.Get("eip712_external_prompt"))
		}
	case eip712SignSubmitting:
		content.WriteString("\n\n" + localization.Get("sign_submitting"))
	case eip712SignComplete:
		if state.result == nil {
			content.WriteString("\n\n" + localization.Get("eip712_failed"))
		} else {
			content.WriteString("\n\n" + localization.Get("sign_result_signature") + "\n" + safeInline("0x"+toHex(state.result.Signature)))
			content.WriteString("\n\n" + localization.Get("sign_result_record") + safeShort(state.result.SigningID))
			content.WriteString("\n" + localization.Get("sign_result_recovered") + safeShort(state.result.Signer.Hex()))
			content.WriteString("\n\n" + localization.Get("sign_result_return"))
		}
	}
	if state.err != "" {
		content.WriteString("\n\n" + model.styles.ErrorStyle.Render(safeInline(state.err)))
	}
	if state.phase != eip712SignSubmitting && state.phase != eip712SignComplete {
		content.WriteString("\n\n" + state.help.View(state.keys))
	}
	return content.String()
}

func (model *CLIModel) clearEIP712Sign() {
	model.credentialUseKeePass = false

	model.eip712SignGeneration++
	if model.eip712Sign != nil {
		if model.eip712Sign.cancel != nil {
			model.eip712Sign.cancel()
			model.eip712Sign.cancel = nil
		}
		model.eip712Sign.typedData.SetValue("")
		model.eip712Sign.password.SetValue("")
	}
	model.eip712Sign = nil
}
