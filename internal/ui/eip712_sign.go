package ui

import (
	"context"
	"fmt"
	"sort"
	"strings"

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
		Up:      key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "previous network")),
		Down:    key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "next network")),
		Prev:    key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "previous step")),
		Next:    key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "next step")),
		Approve: key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "approve and sign")),
		Back:    key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
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
	state.typedData.Placeholder = "EIP-712 typed data JSON"
	state.typedData.CharLimit = evm.MaxEIP712TypedDataBytes
	state.typedData.Width = 110
	state.password.Placeholder = "Storage password"
	state.password.CharLimit = constants.PasswordCharLimit
	state.password.Width = constants.PasswordWidth
	state.password.EchoMode = textinput.EchoPassword
	state.password.EchoCharacter = '•'
	state.help.Width = max(40, model.width-6)
	if len(choices) == 0 {
		state.err = "No active network with chain ID is configured"
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
					state.err = "typed data exceeds the 64 KiB policy limit"
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
			if message.String() == "enter" {
				password := state.password.Value()
				if len(password) == 0 {
					state.err = "storage password is required"
					return model, nil
				}
				state.err = ""
				state.phase = eip712SignSubmitting
				model.eip712SignGeneration++
				state.generation = model.eip712SignGeneration
				generation := state.generation
				service := state.service
				authorizer := model.transactionAuthorizer
				prepared := state.prepared
				accountID := state.account.AccountID
				return model, func() tea.Msg {
					payload := make([]byte, len(password))
					copy(payload, password)
					var result evm.PersonalSignResult
					var err error
					err = authorizer.Authorize(context.Background(), accountID, payload, func(handle wallet.CapabilityHandle, epoch uint64) error {
						result, err = service.ApproveAndSignEIP712(context.Background(), handle, prepared, evm.PersonalSignApprovalRequest{
							AuthorizationEpoch: epoch, ConfirmedIntentHash: prepared.Preview().IntentHash, ConfirmationLevel: evm.ConfirmationReinforced,
						})
						return err
					})
					clear(payload)
					return eip712SignResultMsg{generation: generation, result: result, err: err}
				}
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
		return "EIP-712 signing is unavailable."
	}
	title := lipgloss.NewStyle().Bold(true).Render("Sign EIP-712 Typed Data")
	var content strings.Builder
	content.WriteString(title)
	switch state.phase {
	case eip712SignSelectNetwork:
		content.WriteString("\n\nAccount: " + safeShort(state.account.Address) + "\nSelect the chain for this signature:")
		for index, choice := range state.networks {
			marker := "  "
			if index == state.selected {
				marker = "> "
			}
			_, _ = fmt.Fprintf(&content, "\n%s%s (chain %d)", marker, safeShort(choice.key), choice.network.ChainID)
		}
		content.WriteString("\n\nPress n to continue or esc to cancel.")
	case eip712SignEntry:
		content.WriteString("\n\nChain: " + safeShort(state.networks[state.selected].key) + "\n\n" + state.typedData.View() + "\n\nPress n to validate and preview or esc to cancel.")
	case eip712SignPreview:
		preview := state.prepared.Preview()
		_, _ = fmt.Fprintf(&content, "\n\nChain: %s (%d)\nDigest: %s\nIntent: %s\n\n%s\n\nPress a to approve and sign, p to edit, or esc to cancel.",
			safeShort(state.networks[state.selected].key), preview.DomainChainID, preview.Digest.Hex(), preview.IntentHash.Hex(), preview.Rendered)
	case eip712SignPassword:
		content.WriteString("\n\n" + state.password.View())
	case eip712SignSubmitting:
		content.WriteString("\n\nSigning with reinforced confirmation after durable approval...")
	case eip712SignComplete:
		if state.result == nil {
			content.WriteString("\n\nEIP-712 signing failed.")
		} else {
			content.WriteString("\n\nSignature (Ethereum V=27/28):\n" + safeInline("0x"+toHex(state.result.Signature)))
			content.WriteString("\n\nSigning record: " + safeShort(state.result.SigningID))
			content.WriteString("\nRecovered account: " + safeShort(state.result.Signer.Hex()))
			content.WriteString("\n\nPress esc to return to wallet details.")
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
	model.eip712SignGeneration++
	if model.eip712Sign != nil {
		model.eip712Sign.typedData.SetValue("")
		model.eip712Sign.password.SetValue("")
	}
	model.eip712Sign = nil
}
