package ui

import (
	"context"
	"fmt"
	"math/big"
	"strconv"
	"strings"

	"blocowallet/internal/constants"
	"blocowallet/internal/wallet"

	"github.com/charmbracelet/bubbles/textinput"
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

// SafeDeploymentSummary is the prepared deployment result.
type SafeDeploymentSummary struct {
	SafeAddress common.Address
	ChainID     int64
	Factory     common.Address
	Singleton   common.Address
}

// SafeService is the TUI-facing view over the Safe service.
type SafeService interface {
	ListSafeAccounts(ctx context.Context) ([]SafeAccountSummary, error)
	ListProposals(ctx context.Context, accountID string, chainID uint64, limit int) ([]SafeProposalSummary, error)
	ListOwnerAccounts(ctx context.Context) ([]wallet.Account, error)
	GetProposalOwners(ctx context.Context, proposalID string) ([]common.Address, error)
	ImportSafe(ctx context.Context, name, address string, chainID uint64) error
	PrepareDeploy(ctx context.Context, name string, owners []string, threshold uint64) (*SafeDeploymentSummary, error)
	BroadcastDeploy(ctx context.Context, deployment *SafeDeploymentSummary, deployerAccountID string, password []byte) (string, error)
	Propose(ctx context.Context, accountID string, chainID uint64, to string, value *big.Int, data []byte) (string, error)
	Sign(ctx context.Context, proposalID, ownerAccountID string, chainID uint64, password []byte) error
	Execute(ctx context.Context, proposalID, gasPayerAccountID string, chainID uint64, password []byte) (string, error)
}

type safeViewPhase string

const (
	safeViewList      safeViewPhase = "list"
	safeViewDeploy    safeViewPhase = "deploy"
	safeViewDeployRun safeViewPhase = "deploy_run"
	safeViewImport    safeViewPhase = "import"
	safeViewProposals safeViewPhase = "proposals"
	safeViewDetails   safeViewPhase = "details"
	safeViewSigning   safeViewPhase = "signing"
	safeViewExecute   safeViewPhase = "execute"
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
	gasPayers  []wallet.Account
	gasIndex   int
	password   string
	err        string
	done       string
	generation uint64
	deployment *SafeDeploymentSummary

	// Deploy form
	nameInput      textinput.Model
	ownersInput    textinput.Model
	thresholdInput textinput.Model
	deployAccount  int
	deployAccounts []wallet.Account

	// Import form
	importNameInput textinput.Model
	importAddrInput textinput.Model

	// Proposal form
	proposeToInput    textinput.Model
	proposeValueInput textinput.Model
	proposeDataInput  textinput.Model
}

type safeResultMsg struct {
	generation uint64
	kind       string
	result     string
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

func (model *CLIModel) initSafeDeploy() {
	state := model.safeView
	state.phase = safeViewDeploy
	state.nameInput = textinput.New()
	state.nameInput.Placeholder = "Safe name"
	state.nameInput.CharLimit = 64
	state.ownersInput = textinput.New()
	state.ownersInput.Placeholder = "Owner addresses, comma separated"
	state.ownersInput.CharLimit = 1024
	state.thresholdInput = textinput.New()
	state.thresholdInput.Placeholder = "Threshold (e.g. 2)"
	state.thresholdInput.CharLimit = 4
	state.deployAccount = 0
	state.deployAccounts = nil
	state.err = ""
	if model.safeService != nil {
		if accounts, err := model.safeService.ListOwnerAccounts(context.Background()); err == nil {
			state.deployAccounts = accounts
		}
	}
	state.nameInput.Focus()
}

func (model *CLIModel) initSafeImport() {
	state := model.safeView
	state.phase = safeViewImport
	state.importNameInput = textinput.New()
	state.importNameInput.Placeholder = "Safe name"
	state.importNameInput.CharLimit = 64
	state.importAddrInput = textinput.New()
	state.importAddrInput.Placeholder = "Safe address (checksummed)"
	state.importAddrInput.CharLimit = 42
	state.err = ""
	state.importNameInput.Focus()
}

func (model *CLIModel) initSafeProposals() {
	state := model.safeView
	state.phase = safeViewProposals
	state.proposeToInput = textinput.New()
	state.proposeToInput.Placeholder = "Recipient address"
	state.proposeToInput.CharLimit = 42
	state.proposeValueInput = textinput.New()
	state.proposeValueInput.Placeholder = "Value in wei"
	state.proposeValueInput.CharLimit = 96
	state.proposeDataInput = textinput.New()
	state.proposeDataInput.Placeholder = "Calldata (hex, optional)"
	state.proposeDataInput.CharLimit = 4096
	state.err = ""
	if err := model.reloadSafeProposals(state); err != nil {
		state.err = safeError(err)
	}
}

func (model *CLIModel) updateSafeView(msg tea.Msg) (tea.Model, tea.Cmd) {
	state := model.safeView
	if state == nil {
		model.currentView = constants.DefaultView
		return model, nil
	}
	switch message := msg.(type) {
	case safeResultMsg:
		if message.generation != state.generation {
			return model, nil
		}
		if message.err != nil {
			state.err = safeError(message.err)
			state.phase = safeViewList
			return model, nil
		}
		state.err = ""
		state.done = message.result
		switch message.kind {
		case "deploy":
			state.phase = safeViewList
			if err := model.refreshSafeAccounts(); err != nil {
				state.err = safeError(err)
			}
		case "propose":
			state.phase = safeViewProposals
			if err := model.reloadSafeProposals(state); err != nil {
				state.err = safeError(err)
			}
		case "sign":
			state.phase = safeViewDetails
			if err := model.reloadSafeProposals(state); err != nil {
				state.err = safeError(err)
			}
		case "execute":
			state.phase = safeViewProposals
			if err := model.reloadSafeProposals(state); err != nil {
				state.err = safeError(err)
			}
		}
		return model, nil
	case tea.KeyMsg:
		if keyIs(message, "esc") {
			switch state.phase {
			case safeViewList:
				model.safeView = nil
				model.currentView = constants.DefaultView
			default:
				state.phase = safeViewList
				state.err = ""
				state.password = ""
				if err := model.refreshSafeAccounts(); err != nil {
					state.err = safeError(err)
				}
			}
			return model, nil
		}
		switch state.phase {
		case safeViewList:
			return model, model.updateSafeList(message, state)
		case safeViewDeploy:
			return model, model.updateSafeDeploy(message, state)
		case safeViewDeployRun:
			return model, model.updateSafeDeployRun(message, state)
		case safeViewImport:
			return model, model.updateSafeImport(message, state)
		case safeViewProposals:
			return model, model.updateSafeProposals(message, state)
		case safeViewDetails:
			return model, model.updateSafeDetails(message, state)
		case safeViewSigning:
			return model, model.updateSafeSigning(message, state)
		case safeViewExecute:
			return model, model.updateSafeExecute(message, state)
		}
	}
	return model, nil
}

func (model *CLIModel) updateSafeList(message tea.KeyMsg, state *safeViewState) tea.Cmd {
	switch {
	case keyIs(message, "down", "j"):
		if len(state.accounts) > 0 && state.selected < len(state.accounts)-1 {
			state.selected++
			state.chainID = state.accounts[state.selected].ChainID
			if err := model.reloadSafeProposals(state); err != nil {
				state.err = safeError(err)
			}
		}
	case keyIs(message, "up", "k"):
		if len(state.accounts) > 0 && state.selected > 0 {
			state.selected--
			state.chainID = state.accounts[state.selected].ChainID
			if err := model.reloadSafeProposals(state); err != nil {
				state.err = safeError(err)
			}
		}
	case keyIs(message, "enter"):
		if len(state.accounts) == 0 {
			model.initSafeDeploy()
			return nil
		}
		model.initSafeProposals()
		if err := model.reloadSafeProposals(state); err != nil {
			state.err = safeError(err)
		}
	case keyIs(message, "n", "c"):
		model.initSafeDeploy()
	case keyIs(message, "i"):
		model.initSafeImport()
	}
	return nil
}

func (model *CLIModel) updateSafeDeploy(message tea.KeyMsg, state *safeViewState) tea.Cmd {
	inputs := []*textinput.Model{&state.nameInput, &state.ownersInput, &state.thresholdInput}
	switch {
	case keyIs(message, "tab", "down", "j"):
		for index := range inputs {
			if inputs[index].Focused() && index < len(inputs)-1 {
				inputs[index].Blur()
				inputs[index+1].Focus()
				break
			}
		}
	case keyIs(message, "shift+tab", "up", "k"):
		for index := range inputs {
			if inputs[index].Focused() && index > 0 {
				inputs[index].Blur()
				inputs[index-1].Focus()
				break
			}
		}
	case keyIs(message, "enter"):
		if state.thresholdInput.Focused() {
			state.thresholdInput.Blur()
			return model.runSafeDeploy(state)
		}
		if state.ownersInput.Focused() {
			state.ownersInput.Blur()
			state.thresholdInput.Focus()
		} else if state.nameInput.Focused() {
			state.nameInput.Blur()
			state.ownersInput.Focus()
		}
	}
	if len(state.deployAccounts) == 0 {
		state.deployAccounts, _ = model.safeService.ListOwnerAccounts(context.Background())
	}
	for index := range inputs {
		updated, _ := inputs[index].Update(message)
		*inputs[index] = updated
	}
	return nil
}

func anyFocused(inputs []*textinput.Model) bool {
	for _, input := range inputs {
		if input.Focused() {
			return true
		}
	}
	return false
}

func (model *CLIModel) runSafeDeploy(state *safeViewState) tea.Cmd {
	name := strings.TrimSpace(state.nameInput.Value())
	rawOwners := strings.TrimSpace(state.ownersInput.Value())
	thresholdText := strings.TrimSpace(state.thresholdInput.Value())
	if name == "" || rawOwners == "" || thresholdText == "" {
		state.err = "Name, owners, and threshold are required."
		return nil
	}
	threshold, err := strconv.ParseUint(thresholdText, 10, 64)
	if err != nil {
		state.err = "Threshold must be a positive integer."
		return nil
	}
	owners := strings.Split(rawOwners, ",")
	for index := range owners {
		owners[index] = strings.TrimSpace(owners[index])
		if !common.IsHexAddress(owners[index]) || common.HexToAddress(owners[index]).Hex() != owners[index] {
			state.err = fmt.Sprintf("Owner %q must be a checksummed address.", owners[index])
			return nil
		}
	}
	if threshold == 0 || threshold > uint64(len(owners)) {
		state.err = "Threshold must be between 1 and the owner count."
		return nil
	}
	state.generation++
	generation := state.generation
	service := model.safeService
	state.phase = safeViewDeployRun
	state.err = ""
	return func() tea.Msg {
		deployment, err := service.PrepareDeploy(context.Background(), name, owners, threshold)
		if err != nil {
			return safeResultMsg{generation: generation, kind: "deploy", err: err}
		}
		state.deployment = deployment
		return safeResultMsg{generation: generation, kind: "deploy_prepared", result: deployment.SafeAddress.Hex()}
	}
}

func (model *CLIModel) updateSafeDeployRun(message tea.KeyMsg, state *safeViewState) tea.Cmd {
	if state.deployment == nil {
		state.phase = safeViewDeploy
		return nil
	}
	switch {
	case keyIs(message, "down", "j"):
		if state.deployAccount < len(state.deployAccounts)-1 {
			state.deployAccount++
		}
	case keyIs(message, "up", "k"):
		if state.deployAccount > 0 {
			state.deployAccount--
		}
	case keyIs(message, "enter"):
		if len(state.deployAccounts) == 0 {
			state.err = "No local account can pay the deployment fee."
			return nil
		}
		password := []byte(state.password)
		state.password = ""
		state.generation++
		generation := state.generation
		deployment := state.deployment
		accountID := state.deployAccounts[state.deployAccount].AccountID
		service := model.safeService
		return func() tea.Msg {
			defer clear(password)
			hash, err := service.BroadcastDeploy(context.Background(), deployment, accountID, password)
			if err != nil {
				return safeResultMsg{generation: generation, kind: "deploy", err: err}
			}
			return safeResultMsg{generation: generation, kind: "deploy", result: "Deployed " + deployment.SafeAddress.Hex() + " tx=" + hash}
		}
	case message.Type == tea.KeyBackspace:
		if len(state.password) > 0 {
			state.password = state.password[:len(state.password)-1]
		}
	case isPrintableKey(message):
		if len(state.password) < 256 {
			state.password += message.String()
		}
	}
	return nil
}

func (model *CLIModel) updateSafeImport(message tea.KeyMsg, state *safeViewState) tea.Cmd {
	inputs := []*textinput.Model{&state.importNameInput, &state.importAddrInput}
	if keyIs(message, "enter") {
		name := strings.TrimSpace(state.importNameInput.Value())
		address := strings.TrimSpace(state.importAddrInput.Value())
		if name == "" || !common.IsHexAddress(address) || common.HexToAddress(address).Hex() != address {
			state.err = "A name and a checksummed address are required."
			return nil
		}
		state.generation++
		generation := state.generation
		chainID := state.chainID
		service := model.safeService
		return func() tea.Msg {
			err := service.ImportSafe(context.Background(), name, address, chainID)
			if err != nil {
				return safeResultMsg{generation: generation, kind: "import", err: err}
			}
			return safeResultMsg{generation: generation, kind: "import", result: "Imported " + address}
		}
	}
	if keyIs(message, "tab") {
		if state.importNameInput.Focused() {
			state.importNameInput.Blur()
			state.importAddrInput.Focus()
		} else {
			state.importAddrInput.Blur()
			state.importNameInput.Focus()
		}
	}
	for index := range inputs {
		updated, _ := inputs[index].Update(message)
		*inputs[index] = updated
	}
	return nil
}

func (model *CLIModel) updateSafeProposals(message tea.KeyMsg, state *safeViewState) tea.Cmd {
	inputs := []*textinput.Model{&state.proposeToInput, &state.proposeValueInput, &state.proposeDataInput}
	switch {
	case keyIs(message, "enter"):
		if state.proposeDataInput.Focused() || state.proposeValueInput.Focused() {
			to := strings.TrimSpace(state.proposeToInput.Value())
			value := strings.TrimSpace(state.proposeValueInput.Value())
			if !common.IsHexAddress(to) {
				state.err = "Recipient must be a valid address."
				return nil
			}
			amount, ok := new(big.Int).SetString(value, 10)
			if !ok || amount.Sign() < 0 {
				state.err = "Value must be a non-negative integer in wei."
				return nil
			}
			var calldata []byte
			dataText := strings.TrimSpace(state.proposeDataInput.Value())
			if dataText != "" {
				trimmed := strings.TrimPrefix(dataText, "0x")
				if len(trimmed)%2 != 0 {
					state.err = "Calldata must be even-length hex."
					return nil
				}
				decoded := common.FromHex(trimmed)
				if decoded == nil {
					state.err = "Calldata must be even-length hex."
					return nil
				}
				calldata = decoded
			}
			account := state.accounts[state.selected]
			state.generation++
			generation := state.generation
			service := model.safeService
			return func() tea.Msg {
				proposalID, err := service.Propose(context.Background(), account.AccountID, account.ChainID, to, amount, calldata)
				if err != nil {
					return safeResultMsg{generation: generation, kind: "propose", err: err}
				}
				return safeResultMsg{generation: generation, kind: "propose", result: "Proposed " + proposalID}
			}
		}
		if state.proposeToInput.Focused() {
			state.proposeToInput.Blur()
			state.proposeValueInput.Focus()
		}
	case keyIs(message, "tab"):
		switch {
		case state.proposeToInput.Focused():
			state.proposeToInput.Blur()
			state.proposeValueInput.Focus()
		case state.proposeValueInput.Focused():
			state.proposeValueInput.Blur()
			state.proposeDataInput.Focus()
		default:
			state.proposeDataInput.Blur()
			state.proposeToInput.Focus()
		}
	case keyIs(message, "down", "j") && !anyFocused(inputs):
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
	case keyIs(message, "a", "e") && !anyFocused(inputs):
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
	for index := range inputs {
		updated, _ := inputs[index].Update(message)
		*inputs[index] = updated
	}
	return nil
}

func (model *CLIModel) updateSafeDetails(message tea.KeyMsg, state *safeViewState) tea.Cmd {
	switch {
	case keyIs(message, "enter", "a"):
		if state.proposal == nil {
			return nil
		}
		if len(state.owners) == 0 {
			state.err = "No local owner account can sign this proposal."
			return nil
		}
		state.phase = safeViewSigning
		state.err = ""
	case keyIs(message, "x", "e"):
		if state.proposal == nil {
			return nil
		}
		state.gasPayers, _ = model.safeService.ListOwnerAccounts(context.Background())
		state.gasIndex = 0
		state.phase = safeViewExecute
		state.err = ""
	case keyIs(message, "down", "j"):
		if state.ownerIndex < len(state.owners)-1 {
			state.ownerIndex++
		}
	case keyIs(message, "up", "k"):
		if state.ownerIndex > 0 {
			state.ownerIndex--
		}
	}
	return nil
}

func (model *CLIModel) updateSafeSigning(message tea.KeyMsg, state *safeViewState) tea.Cmd {
	switch {
	case keyIs(message, "enter"):
		password := []byte(state.password)
		state.password = ""
		state.err = ""
		state.generation++
		generation := state.generation
		proposalID := state.proposal.ProposalID
		ownerAccountID := state.owners[state.ownerIndex].AccountID
		chainID := state.chainID
		service := model.safeService
		return func() tea.Msg {
			defer clear(password)
			err := service.Sign(context.Background(), proposalID, ownerAccountID, chainID, password)
			if err != nil {
				return safeResultMsg{generation: generation, kind: "sign", err: err}
			}
			return safeResultMsg{generation: generation, kind: "sign", result: "Owner signature recorded."}
		}
	case message.Type == tea.KeyBackspace:
		if len(state.password) > 0 {
			state.password = state.password[:len(state.password)-1]
		}
	case isPrintableKey(message):
		if len(state.password) < 256 {
			state.password += message.String()
		}
	}
	return nil
}

func (model *CLIModel) updateSafeExecute(message tea.KeyMsg, state *safeViewState) tea.Cmd {
	switch {
	case keyIs(message, "down", "j"):
		if state.gasIndex < len(state.gasPayers)-1 {
			state.gasIndex++
		}
	case keyIs(message, "up", "k"):
		if state.gasIndex > 0 {
			state.gasIndex--
		}
	case keyIs(message, "enter"):
		if len(state.gasPayers) == 0 {
			state.err = "No local account can pay the execution fee."
			return nil
		}
		password := []byte(state.password)
		state.password = ""
		state.generation++
		generation := state.generation
		proposalID := state.proposal.ProposalID
		gasPayerID := state.gasPayers[state.gasIndex].AccountID
		chainID := state.chainID
		service := model.safeService
		return func() tea.Msg {
			defer clear(password)
			hash, err := service.Execute(context.Background(), proposalID, gasPayerID, chainID, password)
			if err != nil {
				return safeResultMsg{generation: generation, kind: "execute", err: err}
			}
			return safeResultMsg{generation: generation, kind: "execute", result: "Executed tx=" + hash}
		}
	case message.Type == tea.KeyBackspace:
		if len(state.password) > 0 {
			state.password = state.password[:len(state.password)-1]
		}
	case isPrintableKey(message):
		if len(state.password) < 256 {
			state.password += message.String()
		}
	}
	return nil
}

func (model *CLIModel) reloadSafeProposals(state *safeViewState) error {
	if len(state.accounts) == 0 {
		state.proposals = nil
		return nil
	}
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
	builder.WriteString("Safe multisig\n")
	switch state.phase {
	case safeViewList:
		builder.WriteString("\nActions: n/c: deploy new Safe • i: import Safe • enter: open proposals • esc: back\n\n")
		if len(state.accounts) == 0 {
			builder.WriteString("No Safe accounts yet. Press n to deploy one or i to import.")
		} else {
			for index, account := range state.accounts {
				marker := " "
				if index == state.selected {
					marker = ">"
				}
				_, _ = fmt.Fprintf(&builder, "%s %s %s\n", marker, safeShort(account.Name), safeShort(account.Address.Hex()))
			}
		}
	case safeViewDeploy:
		builder.WriteString("\nDeploy a new Safe (official v1.5.0 contracts)\n\n")
		builder.WriteString("Name:\n" + state.nameInput.View() + "\n\n")
		builder.WriteString("Owners (checksummed, comma separated):\n" + state.ownersInput.View() + "\n\n")
		builder.WriteString("Threshold:\n" + state.thresholdInput.View() + "\n\n")
		builder.WriteString("tab: next field • enter: prepare • esc: back")
	case safeViewDeployRun:
		builder.WriteString("\nFund the predicted Safe address first:\n\n")
		_, _ = fmt.Fprintf(&builder, "Safe: %s\n\n", state.deployment.SafeAddress.Hex())
		builder.WriteString("Deployer paying the factory call (up/down, enter: broadcast):\n")
		for index, account := range state.deployAccounts {
			marker := " "
			if index == state.deployAccount {
				marker = ">"
			}
			_, _ = fmt.Fprintf(&builder, "%s %s %s\n", marker, safeShort(account.Name), safeShort(account.Address))
		}
		builder.WriteString("\nStorage password: " + strings.Repeat("•", len(state.password)) + "\nenter: broadcast • esc: back")
	case safeViewImport:
		builder.WriteString("\nImport an existing Safe\n\n")
		builder.WriteString("Name:\n" + state.importNameInput.View() + "\n\n")
		builder.WriteString("Safe address (checksummed):\n" + state.importAddrInput.View() + "\n\n")
		builder.WriteString("enter: import • esc: back")
	case safeViewProposals:
		account := state.accounts[state.selected]
		_, _ = fmt.Fprintf(&builder, "\n%s — proposals (enter/a: review, esc: back)\n", safeShort(account.Name))
		builder.WriteString("New proposal — Recipient:\n" + state.proposeToInput.View() + "\n")
		builder.WriteString("Value (wei):\n" + state.proposeValueInput.View() + "\n")
		builder.WriteString("Calldata (hex, optional):\n" + state.proposeDataInput.View() + "\n")
		builder.WriteString("enter: propose • tab: next • esc: back\n\n")
		if len(state.proposals) == 0 {
			builder.WriteString("(no proposals yet)")
		} else {
			for _, proposal := range state.proposals {
				_, _ = fmt.Fprintf(&builder, "  %s to=%s value=%s %d/%d %s\n",
					safeShort(proposal.ProposalID), safeShort(proposal.To.Hex()), proposal.Value.String(),
					proposal.Signatures, proposal.Threshold, proposal.Status)
			}
		}
	case safeViewDetails:
		if state.proposal != nil {
			_, _ = fmt.Fprintf(&builder, "\nProposal %s\nTo: %s\nValue: %s\nDigest: %x\nSignatures: %d/%d\n",
				safeShort(state.proposal.ProposalID), safeShort(state.proposal.To.Hex()), state.proposal.Value.String(),
				state.proposal.Digest, state.proposal.Signatures, state.proposal.Threshold)
			builder.WriteString("\nOwners available to sign (up/down, a: sign, x: execute, esc: back):\n")
			for index, owner := range state.owners {
				marker := " "
				if index == state.ownerIndex {
					marker = ">"
				}
				_, _ = fmt.Fprintf(&builder, "%s %s %s\n", marker, safeShort(owner.Name), safeShort(owner.Address))
			}
		}
	case safeViewSigning:
		owner := state.owners[state.ownerIndex]
		_, _ = fmt.Fprintf(&builder, "\nSigning as %s %s\n", safeShort(owner.Name), safeShort(owner.Address))
		builder.WriteString("Storage password: " + strings.Repeat("•", len(state.password)) + "\nenter: sign • esc: back")
	case safeViewExecute:
		builder.WriteString("\nExecute proposal (gas payer selects the account that pays):\n")
		for index, account := range state.gasPayers {
			marker := " "
			if index == state.gasIndex {
				marker = ">"
			}
			_, _ = fmt.Fprintf(&builder, "%s %s %s\n", marker, safeShort(account.Name), safeShort(account.Address))
		}
		builder.WriteString("\nStorage password: " + strings.Repeat("•", len(state.password)) + "\nenter: execute • esc: back")
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
	if value == "" || value == "enter" || value == "esc" || value == "backspace" || value == "tab" || value == "shift+tab" {
		return false
	}
	return message.Type == tea.KeyRunes
}
