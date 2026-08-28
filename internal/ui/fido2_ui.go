package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"blocowallet/internal/constants"
	"blocowallet/internal/fido2"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type fido2Phase string

// Default local RP identity for WebAuthn ceremonies driven by the TUI.
const (
	defaultFIDO2RPID   = "bloco.local"
	defaultFIDO2Origin = "http://127.0.0.1:18080"
)

const (
	fido2List         fido2Phase = "list"
	fido2Register     fido2Phase = "register"
	fido2RegisterDone fido2Phase = "register_done"
	fido2Authenticate fido2Phase = "authenticate"
)

type fido2State struct {
	phase       fido2Phase
	accountID   string
	credentials []fido2.Credential
	selected    int
	response    textinput.Model
	challenge   string
	status      string
	err         string
	keys        fido2KeyMap
}

type fido2KeyMap struct {
	Up       key.Binding
	Down     key.Binding
	Register key.Binding
	Auth     key.Binding
	Submit   key.Binding
	Back     key.Binding
}

func newFIDO2KeyMap() fido2KeyMap {
	return fido2KeyMap{
		Up:       key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "previous")),
		Down:     key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "next")),
		Register: key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "register key")),
		Auth:     key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "authenticate")),
		Submit:   key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "submit")),
		Back:     key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
	}
}

func (keys fido2KeyMap) ShortHelp() []key.Binding {
	return []key.Binding{keys.Register, keys.Auth, keys.Submit, keys.Back}
}

func (keys fido2KeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{keys.Up, keys.Down}, {keys.Register, keys.Auth, keys.Submit, keys.Back}}
}

// FIDO2Service is the subset of the FIDO2 service the TUI drives.
type FIDO2Service interface {
	BeginRegistration(ctx context.Context, rpID, origin, accountID string, userHandle []byte) (*fido2.RegisterChallenge, error)
	FinishRegistration(ctx context.Context, challengeID string, response fido2.RegistrationResponse, requireUserVerification bool) (*fido2.Credential, error)
	BeginAuthentication(ctx context.Context, rpID, origin string, credentialID []byte) (*fido2.AuthenticateChallenge, error)
	FinishAuthentication(ctx context.Context, challengeID string, response fido2.AssertionResponse, requireUserVerification bool) (*fido2.AssertionResult, error)
}

// FIDO2CredentialReader lists credentials for the TUI.
type FIDO2CredentialReader interface {
	ListCredentials(ctx context.Context, rpID string) ([]fido2.Credential, error)
}

func (model *CLIModel) initFIDO2() {
	if model.selectedAccount == nil {
		return
	}
	response := textinput.New()
	response.Placeholder = "Paste the WebAuthn response JSON"
	response.CharLimit = 128 << 10
	response.Width = 110
	state := &fido2State{
		phase:     fido2List,
		accountID: model.selectedAccount.AccountID,
		response:  response,
		keys:      newFIDO2KeyMap(),
	}
	model.fido2 = state
	model.currentView = constants.FIDO2View
	model.refreshFIDO2Credentials()
}

func (model *CLIModel) refreshFIDO2Credentials() {
	state := model.fido2
	if state == nil || model.fido2Reader == nil {
		return
	}
	credentials, err := model.fido2Reader.ListCredentials(context.Background(), defaultFIDO2RPID)
	if err == nil {
		state.credentials = credentials
	}
}

func (model *CLIModel) updateFIDO2(message tea.Msg) (tea.Model, tea.Cmd) {
	state := model.fido2
	if state == nil {
		model.currentView = constants.WalletDetailsView
		return model, nil
	}
	if message, ok := message.(tea.KeyMsg); ok {
		if key.Matches(message, state.keys.Back) {
			model.clearFIDO2()
			model.currentView = constants.WalletDetailsView
			model.refreshWalletDetailsComponents()
			return model, nil
		}
		switch state.phase {
		case fido2List:
			if len(state.credentials) > 0 && key.Matches(message, state.keys.Down) {
				state.selected = (state.selected + 1) % len(state.credentials)
			}
			if len(state.credentials) > 0 && key.Matches(message, state.keys.Up) {
				state.selected = (state.selected + len(state.credentials) - 1) % len(state.credentials)
			}
			if key.Matches(message, state.keys.Register) {
				if model.fido2Service == nil {
					state.err = "FIDO2 service is unavailable"
					return model, nil
				}
				challenge, err := model.fido2Service.BeginRegistration(context.Background(), defaultFIDO2RPID, defaultFIDO2Origin, state.accountID, []byte(state.accountID))
				if err != nil {
					state.err = safeError(err)
					return model, nil
				}
				state.challenge = string(challenge.Challenge)
				state.status = "Registration challenge (base64url):\n" + safeInline(state.challenge) + "\n\nPaste the authenticator response JSON below."
				state.phase = fido2Register
				state.response.Focus()
				return model, nil
			}
			if len(state.credentials) > 0 && key.Matches(message, state.keys.Auth) {
				if model.fido2Service == nil {
					state.err = "FIDO2 service is unavailable"
					return model, nil
				}
				credential := state.credentials[state.selected]
				challenge, err := model.fido2Service.BeginAuthentication(context.Background(), defaultFIDO2RPID, defaultFIDO2Origin, credential.CredentialID)
				if err != nil {
					state.err = safeError(err)
					return model, nil
				}
				state.challenge = string(challenge.Challenge)
				state.status = "Authentication challenge (base64url):\n" + safeInline(state.challenge) + "\n\nPaste the authenticator response JSON below."
				state.phase = fido2Authenticate
				state.response.Focus()
				return model, nil
			}
		case fido2Register:
			if key.Matches(message, state.keys.Submit) {
				var response fido2.RegistrationResponse
				if err := json.Unmarshal([]byte(state.response.Value()), &response); err != nil {
					state.err = "Response must be the authenticator JSON"
					return model, nil
				}
				result, err := model.fido2Service.FinishRegistration(context.Background(), state.challenge, response, false)
				if err != nil {
					state.err = safeError(err)
					return model, nil
				}
				state.credentials = append(state.credentials, *result)
				state.err = ""
				state.response.SetValue("")
				state.phase = fido2RegisterDone
				return model, nil
			}
			var command tea.Cmd
			state.response, command = state.response.Update(message)
			return model, command
		case fido2Authenticate:
			if key.Matches(message, state.keys.Submit) {
				var response fido2.AssertionResponse
				if err := json.Unmarshal([]byte(state.response.Value()), &response); err != nil {
					state.err = "Response must be the authenticator JSON"
					return model, nil
				}
				result, err := model.fido2Service.FinishAuthentication(context.Background(), state.challenge, response, false)
				if err != nil {
					state.err = safeError(err)
					return model, nil
				}
				state.err = ""
				state.status = fmt.Sprintf("Authenticated. Counter now %d.", result.SignCount)
				state.response.SetValue("")
				state.phase = fido2List
				model.refreshFIDO2Credentials()
				return model, nil
			}
			var command tea.Cmd
			state.response, command = state.response.Update(message)
			return model, command
		case fido2RegisterDone:
			if key.Matches(message, state.keys.Submit) {
				state.phase = fido2List
				model.refreshFIDO2Credentials()
			}
		}
	}
	return model, nil
}

func (model *CLIModel) viewFIDO2() string {
	state := model.fido2
	if state == nil {
		return "FIDO2 is unavailable."
	}
	title := lipgloss.NewStyle().Bold(true).Render("FIDO2 Security Keys")
	var builder strings.Builder
	builder.WriteString(title + "\n\n")
	if state.err != "" {
		builder.WriteString(model.styles.ErrorStyle.Render(safeInline(state.err)))
		builder.WriteString("\n\n")
	}
	switch state.phase {
	case fido2List:
		if len(state.credentials) == 0 {
			builder.WriteString("No security keys registered.\n\nr: register a key • Esc: back")
		} else {
			builder.WriteString("Registered security keys:\n")
			for index, credential := range state.credentials {
				marker := "  "
				if index == state.selected {
					marker = "> "
				}
				_, _ = fmt.Fprintf(&builder, "%s%s (RP %s, counter %d)\n", marker, safeShort(hexShort(credential.CredentialID)), safeShort(credential.RPID), credential.SignCount)
			}
			builder.WriteString("\nr: register • a: authenticate • Esc: back")
		}
	case fido2Register:
		builder.WriteString(state.status + "\n\n")
		builder.WriteString(state.response.View() + "\n\nEnter: verify and store • Esc: back")
	case fido2Authenticate:
		builder.WriteString(state.status + "\n\n")
		builder.WriteString(state.response.View() + "\n\nEnter: verify • Esc: back")
	case fido2RegisterDone:
		builder.WriteString("Security key registered and stored.\n\nEnter: return")
	}
	return builder.String()
}

func hexShort(data []byte) string {
	const digits = "0123456789abcdef"
	if len(data) == 0 {
		return ""
	}
	encoded := make([]byte, len(data)*2)
	for index, value := range data {
		encoded[index*2] = digits[value>>4]
		encoded[index*2+1] = digits[value&0x0f]
	}
	if len(encoded) > 16 {
		return string(encoded[:16]) + "…"
	}
	return string(encoded)
}

// ConfigureFIDO2 wires the FIDO2 service and credential reader.
func (model *CLIModel) ConfigureFIDO2(service FIDO2Service, reader FIDO2CredentialReader) {
	model.fido2Service = service
	model.fido2Reader = reader
}

func (model *CLIModel) clearFIDO2() {
	model.fido2Generation++
	model.fido2 = nil
}
