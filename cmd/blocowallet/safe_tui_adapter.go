package main

import (
	"context"
	"fmt"
	"math/big"
	"sort"

	"blocowallet/internal/safe"
	"blocowallet/internal/storage"
	"blocowallet/internal/ui"
	"blocowallet/internal/wallet"

	"github.com/ethereum/go-ethereum/common"
)

// tuiSafeService adapts per-chain safe.Service instances and the account
// repository to the TUI's SafeService interface.
type tuiSafeService struct {
	services          map[uint64]*safe.Service
	networkNames      map[uint64]string
	repo              *storage.GORMRepository
	pendingDeployName string
	authorize         func(ctx context.Context, accountID string, password []byte, operation func(wallet.CapabilityHandle) error) error
}

var _ ui.SafeService = (*tuiSafeService)(nil)

func (adapter *tuiSafeService) serviceFor(chainID uint64) (*safe.Service, error) {
	service, exists := adapter.services[chainID]
	if !exists {
		return nil, fmt.Errorf("no Safe service for chain %d", chainID)
	}
	return service, nil
}

// ListNetworks returns the active networks with a Safe service.
func (adapter *tuiSafeService) ListNetworks(ctx context.Context) ([]ui.SafeNetwork, error) {
	var networks []ui.SafeNetwork
	for chainID, name := range adapter.networkNames {
		networks = append(networks, ui.SafeNetwork{ChainID: chainID, Name: name})
	}
	sort.Slice(networks, func(i, j int) bool { return networks[i].ChainID < networks[j].ChainID })
	return networks, nil
}

func (adapter *tuiSafeService) ListSafeAccounts(ctx context.Context) ([]ui.SafeAccountSummary, error) {
	accounts, err := adapter.repo.ListAccounts(ctx)
	if err != nil {
		return nil, err
	}
	var summaries []ui.SafeAccountSummary
	for _, account := range accounts {
		if account.SignerKind != wallet.SignerKindMultisig {
			continue
		}
		summaries = append(summaries, ui.SafeAccountSummary{
			AccountID: account.AccountID, Name: account.Name, Address: common.HexToAddress(account.Address),
		})
	}
	return summaries, nil
}

func (adapter *tuiSafeService) SummarizeSafe(ctx context.Context, accountID string, chainID uint64) (*ui.SafeSummary, error) {
	service, err := adapter.serviceFor(chainID)
	if err != nil {
		return nil, err
	}
	summary, err := service.SummarizeSafe(ctx, accountID, chainID)
	if err != nil {
		return nil, err
	}
	return &ui.SafeSummary{
		Address: summary.Address, ChainID: summary.ChainID, Owners: summary.Owners,
		Threshold: summary.Threshold, Nonce: summary.Nonce, Deployed: summary.Deployed,
	}, nil
}

func (adapter *tuiSafeService) ListProposals(ctx context.Context, accountID string, chainID uint64, limit int) ([]ui.SafeProposalSummary, error) {
	service, err := adapter.serviceFor(chainID)
	if err != nil {
		return nil, err
	}
	proposals, err := service.ListProposals(ctx, accountID, chainID, limit)
	if err != nil {
		return nil, err
	}
	var summaries []ui.SafeProposalSummary
	for _, proposal := range proposals {
		summaries = append(summaries, ui.SafeProposalSummary{
			ProposalID: proposal.ProposalID, SafeAddress: proposal.SafeAddress,
			To: proposal.Transaction.To, Value: proposal.Transaction.Value, Digest: proposal.Digest,
			Signatures: len(proposal.Signatures), Threshold: proposal.Threshold, Status: string(proposal.Status),
		})
	}
	return summaries, nil
}

func (adapter *tuiSafeService) ListOwnerAccounts(ctx context.Context) ([]wallet.Account, error) {
	accounts, err := adapter.repo.ListAccounts(ctx)
	if err != nil {
		return nil, err
	}
	var owners []wallet.Account
	for _, account := range accounts {
		if account.SignerKind.SupportsEOASigning() && account.State == wallet.AccountStateActive {
			owners = append(owners, account)
		}
	}
	return owners, nil
}

func (adapter *tuiSafeService) GetProposalOwners(ctx context.Context, proposalID string) ([]common.Address, error) {
	// The proposal store is shared across chains; any service can read it.
	for _, service := range adapter.services {
		proposal, err := service.GetProposal(ctx, proposalID)
		if err == nil && proposal != nil {
			owners := make([]common.Address, 0, len(proposal.Owners))
			for _, owner := range proposal.Owners {
				owners = append(owners, owner.Address)
			}
			return owners, nil
		}
	}
	return nil, fmt.Errorf("proposal %s not found", proposalID)
}

func (adapter *tuiSafeService) ImportSafe(ctx context.Context, name, address string, chainID uint64) error {
	service, err := adapter.serviceFor(chainID)
	if err != nil {
		return err
	}
	_, err = service.ImportSafe(ctx, safe.SafeImportRequest{Name: name, Address: address, ChainID: chainID})
	return err
}

func (adapter *tuiSafeService) PrepareDeploy(ctx context.Context, chainID uint64, name string, owners []string, threshold uint64) (*ui.SafeDeploymentSummary, error) {
	service, err := adapter.serviceFor(chainID)
	if err != nil {
		return nil, err
	}
	ownerAddresses := make([]common.Address, 0, len(owners))
	for _, owner := range owners {
		if !common.IsHexAddress(owner) || common.HexToAddress(owner).Hex() != owner {
			return nil, fmt.Errorf("owner %q must be a checksummed address", owner)
		}
		ownerAddresses = append(ownerAddresses, common.HexToAddress(owner))
	}
	adapter.pendingDeployName = name
	deployment, err := service.PrepareDeploy(ctx, safe.DeployRequest{
		ChainID: int64(chainID), Name: name, Owners: ownerAddresses, Threshold: threshold, SaltNonce: 1,
	})
	if err != nil {
		return nil, err
	}
	return &ui.SafeDeploymentSummary{
		SafeAddress: deployment.SafeAddress, ChainID: deployment.ChainID,
		Factory: deployment.Factory, Singleton: deployment.Singleton,
	}, nil
}

func (adapter *tuiSafeService) BroadcastDeploy(ctx context.Context, deployment *ui.SafeDeploymentSummary, deployerAccountID string, password []byte) (string, error) {
	service, err := adapter.serviceFor(uint64(deployment.ChainID))
	if err != nil {
		return "", err
	}
	if adapter.authorize == nil {
		return "", fmt.Errorf("safe deployment authorization is unavailable")
	}
	hash, err := service.BroadcastDeploy(ctx, &safe.Deployment{
		ChainID: deployment.ChainID, SafeAddress: deployment.SafeAddress,
		Factory: deployment.Factory, Singleton: deployment.Singleton,
	}, deployerAccountID, func(accountID string, operation func(wallet.CapabilityHandle) error) error {
		return adapter.authorize(ctx, accountID, password, operation)
	})
	if err != nil {
		return "", err
	}
	name := adapter.pendingDeployName
	if name == "" {
		name = "Safe " + deployment.SafeAddress.Hex()[:10]
	}
	if _, err := service.ImportSafe(ctx, safe.SafeImportRequest{
		Name: name, Address: deployment.SafeAddress.Hex(), ChainID: uint64(deployment.ChainID),
	}); err != nil {
		return "", err
	}
	return hash.Hex(), nil
}

func (adapter *tuiSafeService) Propose(ctx context.Context, accountID string, chainID uint64, to string, value *big.Int, data []byte) (string, error) {
	service, err := adapter.serviceFor(chainID)
	if err != nil {
		return "", err
	}
	if !common.IsHexAddress(to) || common.HexToAddress(to).Hex() != to {
		return "", fmt.Errorf("recipient must be a checksummed address")
	}
	if len(data) > 128<<10 {
		return "", fmt.Errorf("calldata exceeds the 128 KiB policy")
	}
	proposal, err := service.Propose(ctx, safe.SafeProposalRequest{
		SafeAccountID: accountID, ChainID: chainID, To: common.HexToAddress(to), Value: value, Data: data,
	})
	if err != nil {
		return "", err
	}
	return proposal.ProposalID, nil
}

func (adapter *tuiSafeService) Sign(ctx context.Context, proposalID, ownerAccountID string, chainID uint64, password []byte) error {
	service, err := adapter.serviceFor(chainID)
	if err != nil {
		return err
	}
	if adapter.authorize == nil {
		return fmt.Errorf("safe signing authorization is unavailable")
	}
	_, err = service.Sign(ctx, safe.SafeSignRequest{
		ProposalID: proposalID, OwnerAccountID: ownerAccountID, ChainID: chainID,
	}, func(accountID string, operation func(wallet.CapabilityHandle) error) error {
		return adapter.authorize(ctx, accountID, password, operation)
	})
	return err
}

func (adapter *tuiSafeService) Execute(ctx context.Context, proposalID, gasPayerAccountID string, chainID uint64, password []byte) (string, error) {
	service, err := adapter.serviceFor(chainID)
	if err != nil {
		return "", err
	}
	if adapter.authorize == nil {
		return "", fmt.Errorf("safe execution authorization is unavailable")
	}
	hash, err := service.Execute(ctx, proposalID, gasPayerAccountID, func(accountID string, operation func(wallet.CapabilityHandle) error) error {
		return adapter.authorize(ctx, accountID, password, operation)
	})
	if err != nil {
		return "", err
	}
	return hash.Hex(), nil
}
