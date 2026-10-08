package ui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"blocowallet/internal/constants"
	"blocowallet/internal/walletconnect"
	"blocowallet/pkg/localization"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type walletConnectPhase string

const (
	walletConnectList     walletConnectPhase = "list"
	walletConnectProposal walletConnectPhase = "proposal"
	walletConnectSession  walletConnectPhase = "session"
	walletConnectRequest  walletConnectPhase = "request"
)

type walletConnectState struct {
	phase          walletConnectPhase
	accountID      string
	sessions       []walletconnect.Session
	selected       int
	proposal       *walletconnect.Proposal
	request        *walletconnect.SessionRequestParams
	requestSession *walletconnect.Session
	revoke         bool
	err            string
	keys           walletConnectKeyMap
	help           help.Model
}

type walletConnectKeyMap struct {
	Up      key.Binding
	Down    key.Binding
	Select  key.Binding
	Revoke  key.Binding
	Approve key.Binding
	Reject  key.Binding
	Back    key.Binding
}

func newWalletConnectKeyMap() walletConnectKeyMap {
	return walletConnectKeyMap{
		Up:      key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", localization.Get("wc_previous"))),
		Down:    key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", localization.Get("wc_next"))),
		Select:  key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", localization.Get("wc_select"))),
		Revoke:  key.NewBinding(key.WithKeys("r"), key.WithHelp("r", localization.Get("wc_revoke"))),
		Approve: key.NewBinding(key.WithKeys("a"), key.WithHelp("a", localization.Get("wc_approve"))),
		Reject:  key.NewBinding(key.WithKeys("x"), key.WithHelp("x", localization.Get("wc_reject"))),
		Back:    key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", localization.Get("hist_back"))),
	}
}

func (keys walletConnectKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{keys.Select, keys.Revoke, keys.Approve, keys.Reject, keys.Back}
}

func (keys walletConnectKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{keys.Up, keys.Down, keys.Select}, {keys.Revoke, keys.Approve, keys.Reject, keys.Back}}
}

// WalletConnectService bundles the session store and approval hooks used by
// the TUI.
type WalletConnectService interface {
	ListSessions(context.Context, string, bool) ([]walletconnect.Session, error)
	RevokeSession(context.Context, string, int64) error
	ApproveProposal(context.Context, int64, string, string) (*walletconnect.Session, error)
	RejectProposal(int64)
	PendingProposal(int64) (*walletconnect.Proposal, bool)
}

// WalletConnectSessionReader lists sessions for the TUI.
type WalletConnectSessionReader interface {
	ListSessions(context.Context, string, bool) ([]walletconnect.Session, error)
}

// ConfigureWalletConnect wires the session store and approval service.
func (model *CLIModel) ConfigureWalletConnect(service WalletConnectService, reader WalletConnectSessionReader) {
	model.walletConnectService = service
	model.walletConnectReader = reader
}

func (model *CLIModel) initWalletConnect() {
	if model.selectedAccount == nil {
		return
	}
	state := &walletConnectState{
		phase:     walletConnectList,
		accountID: model.selectedAccount.AccountID,
		keys:      newWalletConnectKeyMap(),
		help:      help.New(),
	}
	state.help.Width = max(40, model.width-6)
	model.walletConnect = state
	model.currentView = constants.WalletConnectView
	if model.walletConnectReader != nil {
		sessions, err := model.walletConnectReader.ListSessions(context.Background(), state.accountID, false)
		if err == nil {
			sort.Slice(sessions, func(left, right int) bool { return sessions[left].CreatedAt < sessions[right].CreatedAt })
			state.sessions = sessions
		}
	}
}

func (model *CLIModel) updateWalletConnect(message tea.Msg) (tea.Model, tea.Cmd) {
	state := model.walletConnect
	if state == nil {
		model.currentView = constants.WalletDetailsView
		return model, nil
	}
	if message, ok := message.(tea.KeyMsg); ok {
		if key.Matches(message, state.keys.Back) {
			model.clearWalletConnect()
			model.currentView = constants.WalletDetailsView
			model.refreshWalletDetailsComponents()
			return model, nil
		}
		switch state.phase {
		case walletConnectList:
			if len(state.sessions) > 0 && key.Matches(message, state.keys.Down) {
				state.selected = (state.selected + 1) % len(state.sessions)
			}
			if len(state.sessions) > 0 && key.Matches(message, state.keys.Up) {
				state.selected = (state.selected + len(state.sessions) - 1) % len(state.sessions)
			}
			if len(state.sessions) > 0 && key.Matches(message, state.keys.Revoke) {
				state.revoke = true
				state.phase = walletConnectSession
			}
		case walletConnectSession:
			if key.Matches(message, state.keys.Select) {
				if state.revoke && model.walletConnectService != nil {
					session := state.sessions[state.selected]
					if err := model.walletConnectService.RevokeSession(context.Background(), session.Topic, time.Now().UnixMilli()); err != nil {
						state.err = safeError(err)
					} else {
						state.sessions = append(state.sessions[:state.selected], state.sessions[state.selected+1:]...)
						if state.selected >= len(state.sessions) {
							state.selected = 0
						}
						state.err = ""
					}
					state.revoke = false
					state.phase = walletConnectList
				}
			}
		case walletConnectProposal:
			if key.Matches(message, state.keys.Approve) && model.walletConnectService != nil {
				proposal := state.proposal
				if proposal != nil {
					session, err := model.walletConnectService.ApproveProposal(context.Background(), proposal.ID, state.accountID, model.selectedAccount.Address)
					if err != nil {
						state.err = safeError(err)
					} else {
						state.sessions = append(state.sessions, *session)
						state.err = ""
						state.proposal = nil
						state.phase = walletConnectList
					}
				}
			}
			if key.Matches(message, state.keys.Reject) && model.walletConnectService != nil {
				if state.proposal != nil {
					model.walletConnectService.RejectProposal(state.proposal.ID)
				}
				state.proposal = nil
				state.phase = walletConnectList
			}
		}
	}
	return model, nil
}

func (model *CLIModel) walletConnectHandleProposal(proposal *walletconnect.Proposal) {
	state := model.walletConnect
	if state == nil {
		return
	}
	state.proposal = proposal
	state.phase = walletConnectProposal
	state.err = ""
}

func (model *CLIModel) walletConnectHandleRequest(session *walletconnect.Session, params *walletconnect.SessionRequestParams) {
	state := model.walletConnect
	if state == nil {
		return
	}
	state.request = params
	state.requestSession = session
	state.phase = walletConnectRequest
}

func (model *CLIModel) viewWalletConnect() string {
	state := model.walletConnect
	if state == nil {
		return localization.Get("wc_unavailable")
	}
	title := lipgloss.NewStyle().Bold(true).Render("WalletConnect v2")
	var builder strings.Builder
	builder.WriteString(title + "\n\n")
	if state.err != "" {
		builder.WriteString(model.styles.ErrorStyle.Render(safeInline(state.err)))
		builder.WriteString("\n\n")
	}
	switch state.phase {
	case walletConnectList:
		if len(state.sessions) == 0 {
			builder.WriteString(localization.Get("wc_no_sessions"))
		} else {
			builder.WriteString(localization.Get("wc_active_sessions") + "\n")
			for index, session := range state.sessions {
				marker := "  "
				if index == state.selected {
					marker = "> "
				}
				expires := time.UnixMilli(session.ExpiresAt).Format("2006-01-02 15:04")
				_, _ = builder.WriteString(marker + safeShort(session.PeerName) + localization.T("wc_session_expires", map[string]interface{}{"Time": expires}) + "\n")
				chains := sessionChainSummary(session)
				if chains != "" {
					_, _ = builder.WriteString(localization.T("wc_chains_line", map[string]interface{}{"Chains": safeShort(chains)}) + "\n")
				}
			}
			builder.WriteString("\n" + localization.Get("wc_sessions_hint"))
		}
	case walletConnectSession:
		session := state.sessions[state.selected]
		_, _ = builder.WriteString(localization.T("wc_session_lines", map[string]interface{}{
			"Peer": safeShort(session.PeerName), "Topic": safeShort(session.Topic), "Account": safeShort(session.AccountID),
			"Expires": time.UnixMilli(session.ExpiresAt).Format("2006-01-02 15:04"),
		}) + "\n")
		builder.WriteString("\n" + localization.Get("wc_namespaces_header") + "\n")
		for namespace, scope := range session.Namespaces {
			_, _ = builder.WriteString(localization.T("wc_namespace_line", map[string]interface{}{
				"Ns": safeShort(namespace), "Chains": safeShort(strings.Join(scope.Chains, ",")),
				"Methods": safeShort(strings.Join(scope.Methods, ",")), "Accounts": safeShort(strings.Join(scope.Accounts, ",")),
			}) + "\n")
		}
		if state.revoke {
			builder.WriteString("\n" + localization.Get("wc_revoke_confirm"))
		}
	case walletConnectProposal:
		proposal := state.proposal
		if proposal == nil {
			builder.WriteString(localization.Get("wc_no_proposal"))
			break
		}
		_, _ = builder.WriteString(localization.T("wc_proposal_incoming", map[string]interface{}{
			"Name": safeShort(proposal.Proposer.Metadata.Name), "URL": safeShort(proposal.Proposer.Metadata.URL),
		}) + "\n")
		builder.WriteString(localization.Get("wc_required_namespaces") + "\n")
		for namespace, scope := range proposal.RequiredNamespaces {
			_, _ = builder.WriteString(localization.T("wc_namespace_req", map[string]interface{}{
				"Ns": safeShort(namespace), "Chains": safeShort(strings.Join(scope.Chains, ",")),
				"Methods": safeShort(strings.Join(scope.Methods, ",")), "Events": safeShort(strings.Join(scope.Events, ",")),
			}) + "\n")
		}
		_, _ = builder.WriteString(localization.T("wc_binding_account", map[string]interface{}{"Account": safeShort(model.selectedAccount.Address)}))
	case walletConnectRequest:
		params := state.request
		if params == nil {
			builder.WriteString(localization.Get("wc_no_request"))
			break
		}
		_, _ = builder.WriteString(localization.T("wc_request_lines", map[string]interface{}{
			"Peer": safeShort(state.requestSession.PeerName), "Chain": safeShort(params.ChainID), "Method": safeShort(params.Request.Method),
		}) + "\n")
		builder.WriteString("\n" + localization.Get("wc_request_approve"))
	}
	return builder.String()
}

func sessionChainSummary(session walletconnect.Session) string {
	var chains []string
	for _, scope := range session.Namespaces {
		chains = append(chains, scope.Chains...)
	}
	return strings.Join(chains, ", ")
}

type walletConnectProposalMsg struct {
	proposal *walletconnect.Proposal
}

type walletConnectRequestMsg struct {
	session *walletconnect.Session
	params  *walletconnect.SessionRequestParams
}

func waitForWalletConnectEvent(events <-chan tea.Msg) tea.Cmd {
	return func() tea.Msg { return <-events }
}

// WalletConnectProposalHandler exposes the proposal hook for the composition root.
func (model *CLIModel) WalletConnectProposalHandler() func(ctx context.Context, proposal *walletconnect.Proposal) error {
	return func(ctx context.Context, proposal *walletconnect.Proposal) error {
		select {
		case model.walletConnectEvents <- walletConnectProposalMsg{proposal: proposal}:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		default:
			return fmt.Errorf("walletconnect proposal queue is full")
		}
	}
}

// WalletConnectRequestHandler exposes the session request hook for the composition root.
func (model *CLIModel) WalletConnectRequestHandler() func(ctx context.Context, session *walletconnect.Session, params *walletconnect.SessionRequestParams) error {
	return func(ctx context.Context, session *walletconnect.Session, params *walletconnect.SessionRequestParams) error {
		select {
		case model.walletConnectEvents <- walletConnectRequestMsg{session: session, params: params}:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		default:
			return fmt.Errorf("walletconnect request queue is full")
		}
	}
}

func (model *CLIModel) clearWalletConnect() {
	model.walletConnectGeneration++
	model.walletConnect = nil
}
