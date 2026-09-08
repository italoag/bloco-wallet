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
	"github.com/charmbracelet/lipgloss"
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

// SafeSummary is the live on-chain state shown for a Safe account.
type SafeSummary struct {
	Address   common.Address
	ChainID   uint64
	Owners    []common.Address
	Threshold uint64
	Nonce     *big.Int
	Deployed  bool
}

// SafeNetwork is one active network a Safe proposal can run on.
type SafeNetwork struct {
	ChainID uint64
	Name    string
}

// SafeService is the TUI-facing view over the Safe service.
type SafeService interface {
	ListNetworks(ctx context.Context) ([]SafeNetwork, error)
	ListSafeAccounts(ctx context.Context) ([]SafeAccountSummary, error)
	SummarizeSafe(ctx context.Context, accountID string, chainID uint64) (*SafeSummary, error)
	ListProposals(ctx context.Context, accountID string, chainID uint64, limit int) ([]SafeProposalSummary, error)
	ListOwnerAccounts(ctx context.Context) ([]wallet.Account, error)
	GetProposalOwners(ctx context.Context, proposalID string) ([]common.Address, error)
	ImportSafe(ctx context.Context, name, address string, chainID uint64) error
	PrepareDeploy(ctx context.Context, chainID uint64, name string, owners []string, threshold uint64) (*SafeDeploymentSummary, error)
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
	phase        safeViewPhase
	accounts     []SafeAccountSummary
	selected     int
	proposals    []SafeProposalSummary
	proposal     *SafeProposalSummary
	chainID      uint64
	networks     []SafeNetwork
	networkIndex int
	ownerIndex   int
	owners       []wallet.Account
	gasPayers    []wallet.Account
	gasIndex     int
	password     string
	err          string
	done         string
	generation   uint64
	deployment   *SafeDeploymentSummary

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
	networks, err := model.safeService.ListNetworks(context.Background())
	if err != nil {
		return err
	}
	if len(networks) == 0 {
		return fmt.Errorf("no active network with a Safe service")
	}
	state.networks = networks
	if state.chainID == 0 {
		state.chainID = networks[0].ChainID
	}
	state.networkIndex = 0
	for index, network := range networks {
		if network.ChainID == state.chainID {
			state.networkIndex = index
			break
		}
	}
	accounts, err := model.safeService.ListSafeAccounts(context.Background())
	if err != nil {
		return err
	}
	state.accounts = accounts
	state.selected = 0
	if len(accounts) > 0 {
		proposals, err := model.safeService.ListProposals(context.Background(), accounts[0].AccountID, state.chainID, 20)
		if err != nil {
			return err
		}
		state.proposals = proposals
	}
	return nil
}

func (model *CLIModel) cycleSafeNetwork(state *safeViewState) error {
	if len(state.networks) < 2 {
		return nil
	}
	state.networkIndex = (state.networkIndex + 1) % len(state.networks)
	state.chainID = state.networks[state.networkIndex].ChainID
	state.err = ""
	if len(state.accounts) > 0 {
		if err := model.reloadSafeProposals(state); err != nil {
			return err
		}
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
			if err := model.reloadSafeProposals(state); err != nil {
				state.err = safeError(err)
			}
		}
	case keyIs(message, "up", "k"):
		if len(state.accounts) > 0 && state.selected > 0 {
			state.selected--
			if err := model.reloadSafeProposals(state); err != nil {
				state.err = safeError(err)
			}
		}
	case keyIs(message, "r"):
		if err := model.cycleSafeNetwork(state); err != nil {
			state.err = safeError(err)
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
	chainID := state.chainID
	state.phase = safeViewDeployRun
	state.err = ""
	return func() tea.Msg {
		deployment, err := service.PrepareDeploy(context.Background(), chainID, name, owners, threshold)
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
				calldata = common.FromHex(trimmed)
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
			model.selectSafeProposal(state, 0)
		}
	case keyIs(message, "a", "e") && !anyFocused(inputs):
		if len(state.proposals) > 0 {
			model.selectSafeProposal(state, 0)
		}
	}
	for index := range inputs {
		updated, _ := inputs[index].Update(message)
		*inputs[index] = updated
	}
	return nil
}

func (model *CLIModel) selectSafeProposal(state *safeViewState, index int) {
	if index < 0 || index >= len(state.proposals) {
		return
	}
	proposal := state.proposals[index]
	state.proposal = &SafeProposalSummary{
		ProposalID: proposal.ProposalID, SafeAddress: proposal.SafeAddress,
		To: proposal.To, Value: proposal.Value, Digest: proposal.Digest,
		Signatures: proposal.Signatures, Threshold: proposal.Threshold, Status: proposal.Status,
	}
	state.ownerIndex = 0
	state.err = ""
	state.phase = safeViewDetails
	model.loadSafeOwners(state)
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
	proposals, err := model.safeService.ListProposals(context.Background(), account.AccountID, state.chainID, 20)
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
	if state.err != "" {
		return model.styles.ErrorStyle.Render(safeInline(state.err))
	}
	switch state.phase {
	case safeViewList:
		return model.viewSafeList(state)
	case safeViewDeploy:
		return model.viewSafeDeploy(state)
	case safeViewDeployRun:
		return model.viewSafeDeployRun(state)
	case safeViewImport:
		return model.viewSafeImport(state)
	case safeViewProposals:
		return model.viewSafeProposals(state)
	case safeViewDetails:
		return model.viewSafeDetails(state)
	case safeViewSigning:
		return model.viewSafeSigning(state)
	case safeViewExecute:
		return model.viewSafeExecute(state)
	}
	return ""
}

func (model *CLIModel) safeHelpBar(keys string) string {
	return model.styles.SafeHelpBar.Render(safeInline(keys))
}

func (model *CLIModel) safeRows(rows []string, selected int) string {
	rendered := make([]string, 0, len(rows))
	for index, row := range rows {
		if index == selected {
			rendered = append(rendered, model.styles.SafeSelectedRow.Render(row))
			continue
		}
		rendered = append(rendered, model.styles.SafeRow.Render(row))
	}
	return strings.Join(rendered, "\n")
}

func (model *CLIModel) safeTitle(title string) string {
	return model.styles.SafeSectionTitle.Render(title)
}

func (model *CLIModel) viewSafeList(state *safeViewState) string {
	var body strings.Builder
	networkName := ""
	if state.networkIndex < len(state.networks) {
		networkName = state.networks[state.networkIndex].Name
	}
	networkLine := model.styles.SafeFieldLabel.Render("Network: ") +
		model.styles.SelectedStyle.Render(safeShort(networkName)) +
		model.styles.SafeFieldLabel.Render("  (r: switch)")
	body.WriteString(networkLine + "\n\n")
	if len(state.accounts) == 0 {
		body.WriteString(model.styles.SafeFieldLabel.Render("No Safe accounts yet. Press n to deploy one or i to import.") + "\n")
	} else {
		rows := make([]string, 0, len(state.accounts))
		for _, account := range state.accounts {
			rows = append(rows, fmt.Sprintf("%-18s  %s", safeShort(account.Name), safeShort(account.Address.Hex())))
		}
		body.WriteString(model.safeRows(rows, state.selected) + "\n")
		selected := state.accounts[state.selected]
		if summary, err := model.safeService.SummarizeSafe(context.Background(), selected.AccountID, state.chainID); err == nil {
			_, _ = fmt.Fprintf(&body, "\n%s\n", safeShort(selected.Address.Hex()))
			if summary.Deployed {
				_, _ = fmt.Fprintf(&body, "Deployed: yes • owners: %d • threshold: %d • nonce: %s\n",
					len(summary.Owners), summary.Threshold, summary.Nonce.String())
			} else {
				body.WriteString("Deployed: no (pending deployment)\n")
			}
		}
	}
	panel := model.styles.SafePanel.Render(body.String())
	help := model.safeHelpBar("n/c deploy • i import • enter proposals • r network • esc back")
	return lipgloss.JoinVertical(lipgloss.Left, model.safeTitle("Safe multisig"), panel, state.done, state.err, help)
}

func (model *CLIModel) viewSafeDeploy(state *safeViewState) string {
	var body strings.Builder
	body.WriteString(model.styles.SafeFieldLabel.Render("Name") + "\n")
	body.WriteString(state.nameInput.View() + "\n\n")
	body.WriteString(model.styles.SafeFieldLabel.Render("Owners (checksummed, comma separated)") + "\n")
	body.WriteString(state.ownersInput.View() + "\n\n")
	body.WriteString(model.styles.SafeFieldLabel.Render("Threshold") + "\n")
	body.WriteString(state.thresholdInput.View())
	panel := model.styles.SafePanel.Render(body.String())
	help := model.safeHelpBar("tab: next field • enter: prepare • esc: back")
	return lipgloss.JoinVertical(lipgloss.Left, model.safeTitle("Deploy a new Safe — official v1.5.0 contracts"), panel, state.err, help)
}

func (model *CLIModel) viewSafeDeployRun(state *safeViewState) string {
	if state.deployment == nil {
		state.phase = safeViewDeploy
		return model.viewSafeDeploy(state)
	}
	var rows strings.Builder
	rows.WriteString(model.styles.SafeFieldLabel.Render("Fund the predicted Safe address first:") + "\n")
	_, _ = fmt.Fprintf(&rows, "%s\n\n", safeShort(state.deployment.SafeAddress.Hex()))
	rows.WriteString(model.styles.SafeFieldLabel.Render("Deployer paying the factory call:") + "\n")
	for _, account := range state.deployAccounts {
		_, _ = fmt.Fprintf(&rows, "%-18s  %s\n", safeShort(account.Name), safeShort(account.Address))
	}
	panel := model.styles.SafePanel.Render(model.safeRows(splitLines(rows.String()), state.deployAccount))
	password := model.styles.SafeFieldLabel.Render("Storage password: " + strings.Repeat("•", len(state.password)))
	help := model.safeHelpBar("up/down: deployer • enter: broadcast • esc: back")
	return lipgloss.JoinVertical(lipgloss.Left, model.safeTitle("Deploy Safe"), panel, password, state.err, help)
}

func splitLines(value string) []string {
	if value == "" {
		return nil
	}
	lines := strings.Split(value, "\n")
	return lines
}

func (model *CLIModel) viewSafeImport(state *safeViewState) string {
	var body strings.Builder
	body.WriteString(model.styles.SafeFieldLabel.Render("Name") + "\n")
	body.WriteString(state.importNameInput.View() + "\n\n")
	body.WriteString(model.styles.SafeFieldLabel.Render("Safe address (checksummed)") + "\n")
	body.WriteString(state.importAddrInput.View())
	panel := model.styles.SafePanel.Render(body.String())
	help := model.safeHelpBar("enter: import • tab: next field • esc: back")
	return lipgloss.JoinVertical(lipgloss.Left, model.safeTitle("Import an existing Safe"), panel, state.err, help)
}

func (model *CLIModel) viewSafeProposals(state *safeViewState) string {
	account := state.accounts[state.selected]
	var form strings.Builder
	form.WriteString(model.styles.SafeFieldLabel.Render("New proposal — Recipient") + "\n")
	form.WriteString(state.proposeToInput.View() + "\n")
	form.WriteString(model.styles.SafeFieldLabel.Render("Value (wei)") + "\n")
	form.WriteString(state.proposeValueInput.View() + "\n")
	form.WriteString(model.styles.SafeFieldLabel.Render("Calldata (hex, optional)") + "\n")
	form.WriteString(state.proposeDataInput.View())
	var list strings.Builder
	if len(state.proposals) == 0 {
		list.WriteString(model.styles.SafeFieldLabel.Render("(no proposals yet)"))
	} else {
		rows := make([]string, 0, len(state.proposals))
		for _, proposal := range state.proposals {
			rows = append(rows, fmt.Sprintf("%s  to=%s  value=%s  %d/%d  %s",
				safeShort(proposal.ProposalID), safeShort(proposal.To.Hex()), proposal.Value.String(),
				proposal.Signatures, proposal.Threshold, proposal.Status))
		}
		list.WriteString(model.safeRows(rows, 0))
	}
	panel := model.styles.SafePanel.Render(
		lipgloss.JoinVertical(lipgloss.Left,
			model.styles.SafeSectionTitle.Render("New proposal"),
			form.String(),
			"",
			model.styles.SafeSectionTitle.Render("Proposals"),
			list.String(),
		),
	)
	help := model.safeHelpBar("enter: propose • tab: next field • enter/a: review proposal • esc: back")
	return lipgloss.JoinVertical(lipgloss.Left, model.safeTitle(safeShort(account.Name)+" — proposals"), panel, state.done, state.err, help)
}

func (model *CLIModel) viewSafeDetails(state *safeViewState) string {
	if state.proposal == nil {
		return model.safeTitle("No proposal selected")
	}
	var body strings.Builder
	_, _ = fmt.Fprintf(&body, "Proposal: %s\n", safeShort(state.proposal.ProposalID))
	_, _ = fmt.Fprintf(&body, "To: %s\n", safeShort(state.proposal.To.Hex()))
	_, _ = fmt.Fprintf(&body, "Value: %s\n", state.proposal.Value.String())
	_, _ = fmt.Fprintf(&body, "Digest: %x\n", state.proposal.Digest)
	_, _ = fmt.Fprintf(&body, "Signatures: %d/%d\n", state.proposal.Signatures, state.proposal.Threshold)
	body.WriteString("\nOwners available to sign:\n")
	for index, owner := range state.owners {
		marker := "  "
		if index == state.ownerIndex {
			marker = "▸ "
		}
		_, _ = fmt.Fprintf(&body, "%s%s  %s\n", marker, safeShort(owner.Name), safeShort(owner.Address))
	}
	panel := model.styles.SafePanel.Render(body.String())
	help := model.safeHelpBar("up/down: owner • a: sign • x: execute • esc: back")
	return lipgloss.JoinVertical(lipgloss.Left, model.safeTitle("Proposal details"), panel, state.done, state.err, help)
}

func (model *CLIModel) viewSafeSigning(state *safeViewState) string {
	owner := state.owners[state.ownerIndex]
	var body strings.Builder
	_, _ = fmt.Fprintf(&body, "Signing as %s  %s\n\n", safeShort(owner.Name), safeShort(owner.Address))
	body.WriteString("Storage password: " + strings.Repeat("•", len(state.password)))
	panel := model.styles.SafePanel.Render(body.String())
	help := model.safeHelpBar("enter: sign • esc: back")
	return lipgloss.JoinVertical(lipgloss.Left, model.safeTitle("Confirm owner signature"), panel, state.err, help)
}

func (model *CLIModel) viewSafeExecute(state *safeViewState) string {
	var rows strings.Builder
	rows.WriteString("Gas payer (pays the execution fee):\n\n")
	for index, account := range state.gasPayers {
		marker := "  "
		if index == state.gasIndex {
			marker = "▸ "
		}
		_, _ = fmt.Fprintf(&rows, "%s%s  %s\n", marker, safeShort(account.Name), safeShort(account.Address))
	}
	panel := model.styles.SafePanel.Render(rows.String())
	password := model.styles.SafeFieldLabel.Render("Storage password: " + strings.Repeat("•", len(state.password)))
	help := model.safeHelpBar("up/down: gas payer • enter: execute • esc: back")
	return lipgloss.JoinVertical(lipgloss.Left, model.safeTitle("Execute proposal"), panel, password, state.err, help)
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
