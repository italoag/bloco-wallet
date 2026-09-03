package main

import (
	"context"
	"fmt"
	"math/big"

	"blocowallet/internal/safe"
	"blocowallet/internal/storage"
	"blocowallet/internal/ui"
	"blocowallet/internal/wallet"

	"github.com/ethereum/go-ethereum/common"
)

// tuiSafeService adapts the safe.Service and the account repository to the
// TUI's SafeService interface.
type tuiSafeService struct {
	service           *safe.Service
	repo              *storage.GORMRepository
	chainID           uint64
	pendingDeployName string
	authorize         func(ctx context.Context, accountID string, password []byte, operation func(wallet.CapabilityHandle) error) error
}

var _ ui.SafeService = (*tuiSafeService)(nil)

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
			ChainID: adapter.chainID, Owners: 0, Threshold: 0,
		})
	}
	return summaries, nil
}

func (adapter *tuiSafeService) ListProposals(ctx context.Context, accountID string, chainID uint64, limit int) ([]ui.SafeProposalSummary, error) {
	proposals, err := adapter.service.ListProposals(ctx, accountID, chainID, limit)
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
	proposal, err := adapter.service.GetProposal(ctx, proposalID)
	if err != nil {
		return nil, err
	}
	owners := make([]common.Address, 0, len(proposal.Owners))
	for _, owner := range proposal.Owners {
		owners = append(owners, owner.Address)
	}
	return owners, nil
}

func (adapter *tuiSafeService) ImportSafe(ctx context.Context, name, address string, chainID uint64) error {
	_, err := adapter.service.ImportSafe(ctx, safe.SafeImportRequest{Name: name, Address: address, ChainID: chainID})
	return err
}

func (adapter *tuiSafeService) PrepareDeploy(ctx context.Context, name string, owners []string, threshold uint64) (*ui.SafeDeploymentSummary, error) {
	ownerAddresses := make([]common.Address, 0, len(owners))
	for _, owner := range owners {
		if !common.IsHexAddress(owner) || common.HexToAddress(owner).Hex() != owner {
			return nil, fmt.Errorf("owner %q must be a checksummed address", owner)
		}
		ownerAddresses = append(ownerAddresses, common.HexToAddress(owner))
	}
	adapter.pendingDeployName = name
	deployment, err := adapter.service.PrepareDeploy(ctx, safe.DeployRequest{
		ChainID: int64(adapter.chainID), Name: name, Owners: ownerAddresses, Threshold: threshold, SaltNonce: 1,
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
	if adapter.authorize == nil {
		return "", fmt.Errorf("safe deployment authorization is unavailable")
	}
	hash, err := adapter.service.BroadcastDeploy(ctx, &safe.Deployment{
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
	if _, err := adapter.service.ImportSafe(ctx, safe.SafeImportRequest{
		Name: name, Address: deployment.SafeAddress.Hex(), ChainID: uint64(deployment.ChainID),
	}); err != nil {
		return "", err
	}
	return hash.Hex(), nil
}

func (adapter *tuiSafeService) Propose(ctx context.Context, accountID string, chainID uint64, to string, value *big.Int) (string, error) {
	if !common.IsHexAddress(to) || common.HexToAddress(to).Hex() != to {
		return "", fmt.Errorf("recipient must be a checksummed address")
	}
	proposal, err := adapter.service.Propose(ctx, safe.SafeProposalRequest{
		SafeAccountID: accountID, ChainID: chainID, To: common.HexToAddress(to), Value: value,
	})
	if err != nil {
		return "", err
	}
	return proposal.ProposalID, nil
}

func (adapter *tuiSafeService) Sign(ctx context.Context, proposalID, ownerAccountID string, chainID uint64, password []byte) error {
	if adapter.authorize == nil {
		return fmt.Errorf("safe signing authorization is unavailable")
	}
	_, err := adapter.service.Sign(ctx, safe.SafeSignRequest{
		ProposalID: proposalID, OwnerAccountID: ownerAccountID, ChainID: chainID,
	}, func(accountID string, operation func(wallet.CapabilityHandle) error) error {
		return adapter.authorize(ctx, accountID, password, operation)
	})
	return err
}

func (adapter *tuiSafeService) Execute(ctx context.Context, proposalID, gasPayerAccountID string, chainID uint64, password []byte) (string, error) {
	if adapter.authorize == nil {
		return "", fmt.Errorf("safe execution authorization is unavailable")
	}
	hash, err := adapter.service.Execute(ctx, proposalID, gasPayerAccountID, func(accountID string, operation func(wallet.CapabilityHandle) error) error {
		return adapter.authorize(ctx, accountID, password, operation)
	})
	if err != nil {
		return "", err
	}
	return hash.Hex(), nil
}
