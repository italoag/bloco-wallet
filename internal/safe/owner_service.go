package safe

import (
	"context"
	"fmt"
	"math/big"

	"blocowallet/internal/wallet"

	"github.com/ethereum/go-ethereum/common"
)

// OwnerAction is the kind of owner-management proposal to build.
type OwnerAction string

const (
	OwnerActionAdd             OwnerAction = "add"
	OwnerActionRemove          OwnerAction = "remove"
	OwnerActionChangeThreshold OwnerAction = "threshold"
)

// OwnerProposalRequest describes an owner-management proposal.
type OwnerProposalRequest struct {
	SafeAccountID string
	ChainID       uint64
	Action        OwnerAction
	Owner         common.Address // add: new owner; remove: owner to remove
	Threshold     uint64         // new threshold (add/remove/threshold)
}

// ProposeOwnerChange builds the OwnerManager calldata from the live owner
// list and proposes it as a normal Safe transaction.
func (service *Service) ProposeOwnerChange(ctx context.Context, request OwnerProposalRequest) (*Proposal, error) {
	if request.ChainID == 0 || request.SafeAccountID == "" {
		return nil, fmt.Errorf("safe owner change: account and chain are required")
	}
	account, err := service.accounts.GetAccount(ctx, request.SafeAccountID)
	if err != nil {
		return nil, err
	}
	if account.SignerKind != wallet.SignerKindMultisig || account.SignerReference != "safe:v1:"+account.Address {
		return nil, fmt.Errorf("safe owner change: account is not a Safe")
	}
	safeAddress := common.HexToAddress(account.Address)
	owners, err := service.rpc.Owners(ctx, safeAddress)
	if err != nil {
		return nil, fmt.Errorf("safe owner change: owners: %w", err)
	}
	threshold, err := service.rpc.Threshold(ctx, safeAddress)
	if err != nil {
		return nil, fmt.Errorf("safe owner change: threshold: %w", err)
	}
	var calldata []byte
	switch request.Action {
	case OwnerActionAdd:
		if request.Owner == (common.Address{}) || request.Threshold == 0 || request.Threshold > uint64(len(owners)+1) {
			return nil, fmt.Errorf("safe owner change: invalid add parameters")
		}
		for _, owner := range owners {
			if owner == request.Owner {
				return nil, fmt.Errorf("safe owner change: owner already exists")
			}
		}
		calldata, err = AddOwnerCalldata(request.Owner, request.Threshold)
	case OwnerActionRemove:
		if request.Owner == (common.Address{}) {
			return nil, fmt.Errorf("safe owner change: owner is required")
		}
		if len(owners) <= 1 {
			return nil, fmt.Errorf("safe owner change: cannot remove the last owner")
		}
		prevOwner, found := findOwnerLink(owners, request.Owner)
		if !found {
			return nil, fmt.Errorf("safe owner change: owner not found")
		}
		if request.Threshold == 0 {
			request.Threshold = threshold
		}
		if request.Threshold > uint64(len(owners)-1) {
			request.Threshold = uint64(len(owners) - 1)
		}
		if request.Threshold == 0 {
			return nil, fmt.Errorf("safe owner change: threshold cannot be zero")
		}
		calldata, err = RemoveOwnerCalldata(prevOwner, request.Owner, request.Threshold)
	case OwnerActionChangeThreshold:
		if request.Threshold == 0 || request.Threshold > uint64(len(owners)) {
			return nil, fmt.Errorf("safe owner change: threshold must be between 1 and the owner count")
		}
		calldata, err = ChangeThresholdCalldata(request.Threshold)
	default:
		return nil, fmt.Errorf("safe owner change: unknown action %q", request.Action)
	}
	if err != nil {
		return nil, err
	}
	return service.Propose(ctx, SafeProposalRequest{
		SafeAccountID: request.SafeAccountID, ChainID: request.ChainID,
		To: safeAddress, Value: big.NewInt(0), Data: calldata,
	})
}

// findOwnerLink returns the predecessor of owner in the Safe linked owner
// list (the Safe itself precedes the first owner).
func findOwnerLink(owners []common.Address, owner common.Address) (prev common.Address, found bool) {
	if len(owners) == 0 {
		return common.Address{}, false
	}
	if owners[0] == owner {
		return common.Address{}, true
	}
	for index := 1; index < len(owners); index++ {
		if owners[index] == owner {
			return owners[index-1], true
		}
	}
	return common.Address{}, false
}
