package ui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"blocowallet/internal/constants"
	"blocowallet/internal/walletconnect"

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
		Up:      key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "previous")),
		Down:    key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "next")),
		Select:  key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "select")),
		Revoke:  key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "revoke session")),
		Approve: key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "approve proposal")),
		Reject:  key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "reject proposal")),
		Back:    key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
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
		return "WalletConnect is unavailable."
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
			builder.WriteString("No active sessions for this account.\n\nPair a dApp to begin.")
		} else {
			builder.WriteString("Active sessions:\n")
			for index, session := range state.sessions {
				marker := "  "
				if index == state.selected {
					marker = "> "
				}
				expires := time.UnixMilli(session.ExpiresAt).Format("2006-01-02 15:04")
				_, _ = fmt.Fprintf(&builder, "%s%s — expires %s\n", marker, safeShort(session.PeerName), expires)
				chains := sessionChainSummary(session)
				if chains != "" {
					_, _ = fmt.Fprintf(&builder, "    chains: %s\n", safeShort(chains))
				}
			}
			builder.WriteString("\nEnter: session • r: revoke • Esc: back")
		}
	case walletConnectSession:
		session := state.sessions[state.selected]
		_, _ = fmt.Fprintf(&builder, "Session: %s\nTopic: %s\nAccount: %s\nExpires: %s\n",
			safeShort(session.PeerName), safeShort(session.Topic), safeShort(session.AccountID), time.UnixMilli(session.ExpiresAt).Format("2006-01-02 15:04"))
		builder.WriteString("\nNamespaces:\n")
		for namespace, scope := range session.Namespaces {
			_, _ = fmt.Fprintf(&builder, "  %s: chains [%s] methods [%s] accounts [%s]\n",
				safeShort(namespace), safeShort(strings.Join(scope.Chains, ",")), safeShort(strings.Join(scope.Methods, ",")), safeShort(strings.Join(scope.Accounts, ",")))
		}
		if state.revoke {
			builder.WriteString("\nThis revokes the session immediately. Enter: confirm • Esc: back")
		}
	case walletConnectProposal:
		proposal := state.proposal
		if proposal == nil {
			builder.WriteString("No pending proposal.")
			break
		}
		_, _ = fmt.Fprintf(&builder, "Incoming session proposal from %s\nURL: %s\n",
			safeShort(proposal.Proposer.Metadata.Name), safeShort(proposal.Proposer.Metadata.URL))
		builder.WriteString("Required namespaces:\n")
		for namespace, scope := range proposal.RequiredNamespaces {
			_, _ = fmt.Fprintf(&builder, "  %s: chains [%s]\n    methods [%s]\n    events [%s]\n",
				safeShort(namespace), safeShort(strings.Join(scope.Chains, ",")), safeShort(strings.Join(scope.Methods, ",")), safeShort(strings.Join(scope.Events, ",")))
		}
		_, _ = fmt.Fprintf(&builder, "Binding account: %s\n\na: approve • x: reject • Esc: back", safeShort(model.selectedAccount.Address))
	case walletConnectRequest:
		params := state.request
		if params == nil {
			builder.WriteString("No pending request.")
			break
		}
		_, _ = fmt.Fprintf(&builder, "Request from %s\nChain: %s\nMethod: %s\n",
			safeShort(state.requestSession.PeerName), safeShort(params.ChainID), safeShort(params.Request.Method))
		builder.WriteString("\nComplete the approval in the wallet's normal signing flow.")
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
