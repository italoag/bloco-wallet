package ui

import (
	"context"
	"fmt"
	"math/big"
	"strings"

	"blocowallet/internal/constants"
	"blocowallet/internal/wallet"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/ethereum/go-ethereum/common"
)

// SafeAccountSummary is the multisig account summary shown by the Safe view.
type SafeAccountSummary struct {
	AccountID string
	Name      string
	Address   common.Address
	ChainID   uint64
	Owners    int
	Threshold uint64
}

// SafeProposalSummary is one pending or executed proposal.
type SafeProposalSummary struct {
	ProposalID  string
	SafeAddress common.Address
	To          common.Address
	Value       *big.Int
	Digest      [32]byte
	Signatures  int
	Threshold   uint64
	Status      string
}

// SafeService is the TUI-facing view over the Safe service.
type SafeService interface {
	ListSafeAccounts(ctx context.Context) ([]SafeAccountSummary, error)
	ListProposals(ctx context.Context, accountID string, chainID uint64, limit int) ([]SafeProposalSummary, error)
	ListOwnerAccounts(ctx context.Context) ([]wallet.Account, error)
	GetProposalOwners(ctx context.Context, proposalID string) ([]common.Address, error)
	Sign(ctx context.Context, proposalID, ownerAccountID string, chainID uint64, password []byte) error
}

type safeViewPhase string

const (
	safeViewList    safeViewPhase = "list"
	safeViewDetails safeViewPhase = "details"
	safeViewSigning safeViewPhase = "signing"
)

type safeViewState struct {
	phase      safeViewPhase
	accounts   []SafeAccountSummary
	selected   int
	proposals  []SafeProposalSummary
	proposal   *SafeProposalSummary
	chainID    uint64
	ownerIndex int
	owners     []wallet.Account
	password   string
	err        string
	done       string
	generation uint64
}

type safeSignResultMsg struct {
	generation uint64
	err        error
}

// ConfigureSafeService wires the Safe proposal view into the model.
func (model *CLIModel) ConfigureSafeService(service SafeService) {
	model.safeService = service
}

func (model *CLIModel) initSafeView() {
	model.safeView = &safeViewState{phase: safeViewList}
	model.currentView = constants.SafeView
	if err := model.refreshSafeAccounts(); err != nil {
		model.safeView.err = safeError(err)
	}
}

func (model *CLIModel) refreshSafeAccounts() error {
	state := model.safeView
	if state == nil || model.safeService == nil {
		return fmt.Errorf("safe service is unavailable")
	}
	accounts, err := model.safeService.ListSafeAccounts(context.Background())
	if err != nil {
		return err
	}
	state.accounts = accounts
	state.selected = 0
	if len(accounts) > 0 {
		state.chainID = accounts[0].ChainID
		proposals, err := model.safeService.ListProposals(context.Background(), accounts[0].AccountID, accounts[0].ChainID, 20)
		if err != nil {
			return err
		}
		state.proposals = proposals
	}
	return nil
}

func (model *CLIModel) updateSafeView(msg tea.Msg) (tea.Model, tea.Cmd) {
	state := model.safeView
	if state == nil {
		model.currentView = constants.DefaultView
		return model, nil
	}
	switch message := msg.(type) {
	case safeSignResultMsg:
		if message.generation != state.generation {
			return model, nil
		}
		if message.err != nil {
			state.err = safeError(message.err)
			state.phase = safeViewDetails
			return model, nil
		}
		state.done = "Owner signature recorded."
		state.err = ""
		state.phase = safeViewDetails
		if err := model.reloadSafeProposals(state); err != nil {
			state.err = safeError(err)
		}
		return model, nil
	case tea.KeyMsg:
		if keyIs(message, "esc") {
			if state.phase == safeViewList {
				model.safeView = nil
				model.currentView = constants.DefaultView
				return model, nil
			}
			state.phase = safeViewList
			state.err = ""
			return model, nil
		}
		switch state.phase {
		case safeViewList:
			switch {
			case keyIs(message, "down", "j"):
				if state.selected < len(state.accounts)-1 {
					state.selected++
					state.chainID = state.accounts[state.selected].ChainID
					if err := model.reloadSafeProposals(state); err != nil {
						state.err = safeError(err)
					}
				}
			case keyIs(message, "up", "k"):
				if state.selected > 0 {
					state.selected--
					state.chainID = state.accounts[state.selected].ChainID
					if err := model.reloadSafeProposals(state); err != nil {
						state.err = safeError(err)
					}
				}
			case keyIs(message, "enter"):
				if len(state.proposals) > 0 {
					state.proposal = &SafeProposalSummary{
						ProposalID: state.proposals[0].ProposalID, SafeAddress: state.proposals[0].SafeAddress,
						To: state.proposals[0].To, Value: state.proposals[0].Value, Digest: state.proposals[0].Digest,
						Signatures: state.proposals[0].Signatures, Threshold: state.proposals[0].Threshold, Status: state.proposals[0].Status,
					}
					state.ownerIndex = 0
					state.err = ""
					state.phase = safeViewDetails
					model.loadSafeOwners(state)
				}
			}
		case safeViewDetails:
			switch {
			case keyIs(message, "enter", "a"):
				if state.proposal != nil && len(state.owners) > 0 {
					state.phase = safeViewSigning
					state.err = ""
					return model, nil
				}
				if state.proposal != nil && len(state.owners) == 0 {
					state.err = "No local owner account can sign this proposal."
				}
			case keyIs(message, "down", "j"):
				if state.ownerIndex < len(state.owners)-1 {
					state.ownerIndex++
				}
			case keyIs(message, "up", "k"):
				if state.ownerIndex > 0 {
					state.ownerIndex--
				}
			}
		case safeViewSigning:
			if keyIs(message, "enter") {
				password := []byte(state.password)
				state.password = ""
				state.err = ""
				state.generation++
				generation := state.generation
				proposalID := state.proposal.ProposalID
				ownerAccountID := state.owners[state.ownerIndex].AccountID
				chainID := state.chainID
				service := model.safeService
				return model, func() tea.Msg {
					defer clear(password)
					err := service.Sign(context.Background(), proposalID, ownerAccountID, chainID, password)
					return safeSignResultMsg{generation: generation, err: err}
				}
			}
			if message.String() != "" && message.Type == tea.KeyBackspace {
				if len(state.password) > 0 {
					state.password = state.password[:len(state.password)-1]
				}
			} else if isPrintableKey(message) {
				if len(state.password) < 256 {
					state.password += message.String()
				}
			}
		}
	}
	return model, nil
}

func (model *CLIModel) reloadSafeProposals(state *safeViewState) error {
	account := state.accounts[state.selected]
	proposals, err := model.safeService.ListProposals(context.Background(), account.AccountID, account.ChainID, 20)
	if err != nil {
		return err
	}
	state.proposals = proposals
	return nil
}

func (model *CLIModel) loadSafeOwners(state *safeViewState) {
	if model.safeService == nil || state.proposal == nil {
		state.owners = nil
		return
	}
	accounts, err := model.safeService.ListOwnerAccounts(context.Background())
	if err != nil {
		state.err = safeError(err)
		state.owners = nil
		return
	}
	owners, err := model.safeService.GetProposalOwners(context.Background(), state.proposal.ProposalID)
	if err != nil {
		state.err = safeError(err)
		state.owners = nil
		return
	}
	eligible := make([]wallet.Account, 0, len(owners))
	for _, account := range accounts {
		if !account.SignerKind.SupportsEOASigning() {
			continue
		}
		for _, owner := range owners {
			if strings.EqualFold(account.Address, owner.Hex()) {
				eligible = append(eligible, account)
				break
			}
		}
	}
	state.owners = eligible
}

func (model *CLIModel) viewSafe() string {
	state := model.safeView
	if state == nil {
		return "Safe view unavailable."
	}
	var builder strings.Builder
	builder.WriteString("Safe multisig proposals\n")
	if len(state.accounts) == 0 {
		builder.WriteString("\nNo Safe accounts imported yet.\n\nUse: blocowallet safe import <network> <name> <safe-address>\n")
		builder.WriteString("\nesc: back to menu")
		return builder.String()
	}
	account := state.accounts[state.selected]
	_, _ = fmt.Fprintf(&builder, "\n%s (%s)  threshold %d/%d\n", safeShort(account.Name), safeShort(account.Address.Hex()), account.Threshold, account.Owners)
	if state.phase == safeViewList {
		builder.WriteString("\nProposals (enter: review, esc: back):\n")
		if len(state.proposals) == 0 {
			builder.WriteString("  (no proposals)\n\nUse: blocowallet safe propose <network> <safe-name> <to> <value> [data]")
		} else {
			for _, proposal := range state.proposals {
				_, _ = fmt.Fprintf(&builder, "  %s to=%s value=%s %d/%d %s\n",
					safeShort(proposal.ProposalID), safeShort(proposal.To.Hex()), proposal.Value.String(),
					proposal.Signatures, proposal.Threshold, proposal.Status)
			}
		}
		return builder.String()
	}
	if state.proposal != nil {
		_, _ = fmt.Fprintf(&builder, "\nProposal %s\nTo: %s\nValue: %s\nDigest: %x\nSignatures: %d/%d\n",
			safeShort(state.proposal.ProposalID), safeShort(state.proposal.To.Hex()), state.proposal.Value.String(),
			state.proposal.Digest, state.proposal.Signatures, state.proposal.Threshold)
		switch state.phase {
		case safeViewDetails:
			builder.WriteString("\nOwners available to sign (up/down, a: approve, esc: back):\n")
			for index, owner := range state.owners {
				marker := " "
				if index == state.ownerIndex {
					marker = ">"
				}
				_, _ = fmt.Fprintf(&builder, "%s %s %s\n", marker, safeShort(owner.Name), safeShort(owner.Address))
			}
		case safeViewSigning:
			owner := state.owners[state.ownerIndex]
			_, _ = fmt.Fprintf(&builder, "\nSigning as %s %s\n", safeShort(owner.Name), safeShort(owner.Address))
			builder.WriteString("Storage password: " + strings.Repeat("•", len(state.password)) + "\nenter: sign • esc: back")
		}
	}
	if state.err != "" {
		builder.WriteString("\n" + model.styles.ErrorStyle.Render(safeInline(state.err)))
	}
	if state.done != "" {
		builder.WriteString("\n" + state.done)
	}
	return builder.String()
}

func keyIs(message tea.KeyMsg, keys ...string) bool {
	for _, key := range keys {
		if message.String() == key {
			return true
		}
	}
	return false
}

func isPrintableKey(message tea.KeyMsg) bool {
	value := message.String()
	if value == "" || value == "enter" || value == "esc" || value == "backspace" {
		return false
	}
	return message.Type == tea.KeyRunes
}
