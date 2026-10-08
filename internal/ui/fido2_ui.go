package ui

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"

	"blocowallet/internal/constants"
	"blocowallet/internal/fido2"
	"blocowallet/pkg/localization"

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
	challengeID string
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
		Up:       key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", localization.Get("fido2_previous"))),
		Down:     key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", localization.Get("fido2_next"))),
		Register: key.NewBinding(key.WithKeys("r"), key.WithHelp("r", localization.Get("fido2_register"))),
		Auth:     key.NewBinding(key.WithKeys("a"), key.WithHelp("a", localization.Get("fido2_authenticate"))),
		Submit:   key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", localization.Get("fido2_submit"))),
		Back:     key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", localization.Get("hist_back"))),
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
	response.Placeholder = localization.Get("fido2_response_placeholder")
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
					state.err = localization.Get("fido2_err_unavailable")
					return model, nil
				}
				challenge, err := model.fido2Service.BeginRegistration(context.Background(), defaultFIDO2RPID, defaultFIDO2Origin, state.accountID, []byte(state.accountID))
				if err != nil {
					state.err = safeError(err)
					return model, nil
				}
				state.challengeID = challenge.ChallengeID
				state.challenge = base64.RawURLEncoding.EncodeToString(challenge.Challenge)
				state.status = localization.Get("fido2_reg_challenge") + "\n" + safeInline(state.challenge) + "\n\n" + localization.Get("fido2_paste_response")
				state.phase = fido2Register
				state.response.Focus()
				return model, nil
			}
			if len(state.credentials) > 0 && key.Matches(message, state.keys.Auth) {
				if model.fido2Service == nil {
					state.err = localization.Get("fido2_err_unavailable")
					return model, nil
				}
				credential := state.credentials[state.selected]
				challenge, err := model.fido2Service.BeginAuthentication(context.Background(), defaultFIDO2RPID, defaultFIDO2Origin, credential.CredentialID)
				if err != nil {
					state.err = safeError(err)
					return model, nil
				}
				state.challengeID = challenge.ChallengeID
				state.challenge = base64.RawURLEncoding.EncodeToString(challenge.Challenge)
				state.status = localization.Get("fido2_auth_challenge") + "\n" + safeInline(state.challenge) + "\n\n" + localization.Get("fido2_paste_response")
				state.phase = fido2Authenticate
				state.response.Focus()
				return model, nil
			}
		case fido2Register:
			if key.Matches(message, state.keys.Submit) {
				var response fido2.RegistrationResponse
				if err := json.Unmarshal([]byte(state.response.Value()), &response); err != nil {
					state.err = localization.Get("fido2_err_response")
					return model, nil
				}
				result, err := model.fido2Service.FinishRegistration(context.Background(), state.challengeID, response, false)
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
					state.err = localization.Get("fido2_err_response")
					return model, nil
				}
				result, err := model.fido2Service.FinishAuthentication(context.Background(), state.challengeID, response, false)
				if err != nil {
					state.err = safeError(err)
					return model, nil
				}
				state.err = ""
				state.status = localization.T("fido2_authenticated", map[string]interface{}{"Count": result.SignCount})
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
		return localization.Get("fido2_unavailable")
	}
	title := lipgloss.NewStyle().Bold(true).Render(localization.Get("fido2_title"))
	var builder strings.Builder
	builder.WriteString(title + "\n\n")
	if state.err != "" {
		builder.WriteString(model.styles.ErrorStyle.Render(safeInline(state.err)))
		builder.WriteString("\n\n")
	}
	switch state.phase {
	case fido2List:
		if len(state.credentials) == 0 {
			builder.WriteString(localization.Get("fido2_no_keys"))
		} else {
			builder.WriteString(localization.Get("fido2_registered") + "\n")
			for index, credential := range state.credentials {
				marker := "  "
				if index == state.selected {
					marker = "> "
				}
				_, _ = builder.WriteString(marker + safeShort(hexShort(credential.CredentialID)) + localization.T("fido2_credential_line", map[string]interface{}{"RP": safeShort(credential.RPID), "Count": credential.SignCount}) + "\n")
			}
			builder.WriteString("\n" + localization.Get("fido2_list_hint"))
		}
	case fido2Register:
		builder.WriteString(state.status + "\n\n")
		builder.WriteString(state.response.View() + "\n\n" + localization.Get("fido2_verify_store"))
	case fido2Authenticate:
		builder.WriteString(state.status + "\n\n")
		builder.WriteString(state.response.View() + "\n\n" + localization.Get("fido2_verify"))
	case fido2RegisterDone:
		builder.WriteString(localization.Get("fido2_key_registered"))
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
