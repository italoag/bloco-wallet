package ui

import (
	"context"
	"fmt"
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

const localPersonalSignOrigin = "local-user"

type MessageSigningService interface {
	ApproveAndSignPersonal(context.Context, wallet.CapabilityHandle, *evm.PreparedPersonalSign, evm.PersonalSignApprovalRequest) (evm.PersonalSignResult, error)
	ApproveAndSignEIP712(context.Context, wallet.CapabilityHandle, *evm.PreparedEIP712Sign, evm.PersonalSignApprovalRequest) (evm.PersonalSignResult, error)
}

type MessageSigningServiceFactory func(context.Context) (MessageSigningService, error)

type personalSignPhase string

const (
	personalSignEntry      personalSignPhase = "entry"
	personalSignPreview    personalSignPhase = "preview"
	personalSignPassword   personalSignPhase = "password"
	personalSignSubmitting personalSignPhase = "submitting"
	personalSignComplete   personalSignPhase = "complete"
)

type personalSignKeyMap struct {
	Message key.Binding
	Prev    key.Binding
	Next    key.Binding
	Approve key.Binding
	Back    key.Binding
}

func newPersonalSignKeyMap() personalSignKeyMap {
	return personalSignKeyMap{
		Message: key.NewBinding(key.WithKeys("m"), key.WithHelp("m", "edit message")),
		Prev:    key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "previous step")),
		Next:    key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "next step")),
		Approve: key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "approve and sign")),
		Back:    key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
	}
}

func (keys personalSignKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{keys.Message, keys.Next, keys.Back}
}

func (keys personalSignKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{keys.Message, keys.Prev, keys.Next}, {keys.Approve, keys.Back}}
}

type personalSignState struct {
	phase      personalSignPhase
	account    wallet.AccountSummary
	service    MessageSigningService
	message    textinput.Model
	password   textinput.Model
	prepared   *evm.PreparedPersonalSign
	result     *evm.PersonalSignResult
	generation uint64
	err        string
	keys       personalSignKeyMap
	help       help.Model
}

type personalSignResultMsg struct {
	generation uint64
	result     evm.PersonalSignResult
	err        error
}

func (model *CLIModel) initPersonalSign(service MessageSigningService) {
	if service == nil || model.selectedAccount == nil {
		return
	}
	model.personalSign = &personalSignState{
		phase:    personalSignEntry,
		account:  *model.selectedAccount,
		service:  service,
		message:  textinput.New(),
		password: textinput.New(),
		keys:     newPersonalSignKeyMap(),
		help:     help.New(),
	}
	model.personalSign.message.Placeholder = "Message to sign (EIP-191 personal_sign)"
	model.personalSign.message.CharLimit = evm.MaxPersonalSignMessageBytes
	model.personalSign.message.Width = 100
	model.personalSign.message.Focus()
	model.personalSign.password.Placeholder = "Storage password"
	model.personalSign.password.CharLimit = constants.PasswordCharLimit
	model.personalSign.password.Width = constants.PasswordWidth
	model.personalSign.password.EchoMode = textinput.EchoPassword
	model.personalSign.password.EchoCharacter = '•'
	model.personalSign.help.Width = max(40, model.width-6)
	model.currentView = constants.PersonalSignView
}

func (model *CLIModel) updatePersonalSign(message tea.Msg) (tea.Model, tea.Cmd) {
	state := model.personalSign
	if state == nil {
		model.currentView = constants.WalletDetailsView
		return model, nil
	}
	switch message := message.(type) {
	case personalSignResultMsg:
		if message.generation != state.generation {
			return model, nil
		}
		state.password.SetValue("")
		if message.err != nil {
			state.phase = personalSignPreview
			state.err = safeError(message.err)
			return model, nil
		}
		state.result = &message.result
		state.err = ""
		state.phase = personalSignComplete
		return model, nil
	case tea.KeyMsg:
		if key.Matches(message, state.keys.Back) {
			model.clearPersonalSign()
			model.currentView = constants.WalletDetailsView
			model.refreshWalletDetailsComponents()
			return model, nil
		}
		if state.phase == personalSignComplete {
			return model, nil
		}
		if state.phase == personalSignSubmitting {
			return model, nil
		}
		switch state.phase {
		case personalSignEntry:
			if key.Matches(message, state.keys.Next) {
				value := state.message.Value()
				if len(value) > evm.MaxPersonalSignMessageBytes {
					state.err = "message exceeds the 64 KiB policy limit"
					return model, nil
				}
				prepared, err := evm.PreparePersonalSign(evm.PreparePersonalSignRequest{
					AccountID: state.account.AccountID, Signer: common.HexToAddress(state.account.Address), Message: []byte(value), Origin: localPersonalSignOrigin,
				})
				if err != nil {
					state.err = safeError(err)
					return model, nil
				}
				state.prepared = prepared
				state.err = ""
				state.phase = personalSignPreview
				return model, nil
			}
			var command tea.Cmd
			state.message, command = state.message.Update(message)
			return model, command
		case personalSignPreview:
			if key.Matches(message, state.keys.Prev) {
				state.phase = personalSignEntry
				return model, nil
			}
			if key.Matches(message, state.keys.Next) || key.Matches(message, state.keys.Approve) {
				state.password.Focus()
				state.phase = personalSignPassword
				return model, nil
			}
		case personalSignPassword:
			if key.Matches(message, state.keys.Prev) {
				state.password.SetValue("")
				state.phase = personalSignPreview
				return model, nil
			}
			if message.String() == "enter" {
				password := state.password.Value()
				if len(password) == 0 {
					state.err = "storage password is required"
					return model, nil
				}
				state.err = ""
				state.phase = personalSignSubmitting
				model.personalSignGeneration++
				state.generation = model.personalSignGeneration
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
						result, err = service.ApproveAndSignPersonal(context.Background(), handle, prepared, evm.PersonalSignApprovalRequest{
							AuthorizationEpoch: epoch, ConfirmedIntentHash: prepared.Preview().IntentHash, ConfirmationLevel: evm.ConfirmationReinforced,
						})
						return err
					})
					clear(payload)
					return personalSignResultMsg{generation: generation, result: result, err: err}
				}
			}
			var command tea.Cmd
			state.password, command = state.password.Update(message)
			return model, command
		}
	}
	return model, nil
}

func (model *CLIModel) viewPersonalSign() string {
	state := model.personalSign
	if state == nil {
		return "Message signing is unavailable."
	}
	title := lipgloss.NewStyle().Bold(true).Render("Sign Message (EIP-191 personal_sign)")
	var content strings.Builder
	content.WriteString(title)
	switch state.phase {
	case personalSignEntry:
		_, _ = fmt.Fprintf(&content, "\n\nAccount: %s\n%s\n\nPress n to preview the exact signed message or esc to cancel.", safeShort(state.account.Address), state.message.View())
	case personalSignPreview:
		preview := state.prepared.Preview()
		_, _ = fmt.Fprintf(&content, "\n\nAccount: %s\nMessage length: %d bytes\nUTF-8: %t\nMessage:\n%s\n\nDigest: %s\nIntent: %s\n\nYou are signing exactly these bytes with personal_sign. The signature has no chain binding.\n\nPress a to approve and sign, p to edit the message, or esc to cancel.",
			safeShort(state.account.Address), preview.MessageLength, preview.UTF8, safeInline(string(preview.Message)), preview.Digest.Hex(), preview.IntentHash.Hex())
	case personalSignPassword:
		content.WriteString("\n\n" + state.password.View())
	case personalSignSubmitting:
		content.WriteString("\n\nSigning with reinforced confirmation after durable approval...")
	case personalSignComplete:
		if state.result == nil {
			content.WriteString("\n\nMessage signing failed.")
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
	if state.phase != personalSignSubmitting && state.phase != personalSignComplete {
		content.WriteString("\n\n" + state.help.View(state.keys))
	}
	return content.String()
}

func (model *CLIModel) clearPersonalSign() {
	model.personalSignGeneration++
	if model.personalSign != nil {
		model.personalSign.message.SetValue("")
		model.personalSign.password.SetValue("")
	}
	model.personalSign = nil
}

func (model *CLIModel) ConfigureMessageSigningFactory(factory MessageSigningServiceFactory) {
	model.messageSigningFactory = factory
}

func toHex(value []byte) string {
	const hexDigits = "0123456789abcdef"
	encoded := make([]byte, len(value)*2)
	for index, octet := range value {
		encoded[index*2] = hexDigits[octet>>4]
		encoded[index*2+1] = hexDigits[octet&0x0f]
	}
	return string(encoded)
}
