package safe

import (
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// Safe OwnerManager selectors (Safe v1.5.0).
const (
	selectorAddOwner        = "addOwnerWithThreshold(address,uint256)"
	selectorRemoveOwner     = "removeOwner(address,address,uint256)"
	selectorChangeThreshold = "changeThreshold(uint256)"
	selectorSwapOwner       = "swapOwner(address,address,address)"
)

// AddOwnerCalldata builds addOwnerWithThreshold(newOwner, threshold).
func AddOwnerCalldata(newOwner common.Address, threshold uint64) ([]byte, error) {
	method, err := safeOwnerMethod(selectorAddOwner, "nonpayable", "address", "uint256")
	if err != nil {
		return nil, err
	}
	packed, err := method.Inputs.Pack(newOwner, new(big.Int).SetUint64(threshold))
	if err != nil {
		return nil, err
	}
	return append(append([]byte(nil), method.ID...), packed...), nil
}

// RemoveOwnerCalldata builds removeOwner(prevOwner, owner, threshold). The
// Safe owner list is a linked list; prevOwner is the address that points to
// the removed owner (the Safe contract address itself for the first owner).
func RemoveOwnerCalldata(prevOwner, owner common.Address, threshold uint64) ([]byte, error) {
	method, err := safeOwnerMethod(selectorRemoveOwner, "nonpayable", "address", "address", "uint256")
	if err != nil {
		return nil, err
	}
	packed, err := method.Inputs.Pack(prevOwner, owner, new(big.Int).SetUint64(threshold))
	if err != nil {
		return nil, err
	}
	return append(append([]byte(nil), method.ID...), packed...), nil
}

// ChangeThresholdCalldata builds changeThreshold(threshold).
func ChangeThresholdCalldata(threshold uint64) ([]byte, error) {
	method, err := safeOwnerMethod(selectorChangeThreshold, "nonpayable", "uint256")
	if err != nil {
		return nil, err
	}
	packed, err := method.Inputs.Pack(new(big.Int).SetUint64(threshold))
	if err != nil {
		return nil, err
	}
	return append(append([]byte(nil), method.ID...), packed...), nil
}

func safeOwnerMethod(signature, stateMutability string, inputTypes ...string) (*abi.Method, error) {
	arguments := make(abi.Arguments, 0, len(inputTypes))
	for _, kind := range inputTypes {
		parsed, err := abi.NewType(kind, "", nil)
		if err != nil {
			return nil, err
		}
		arguments = append(arguments, abi.Argument{Type: parsed})
	}
	selector := mustSelector(signature)
	return &abi.Method{
		Name:            "ownerManager",
		RawName:         "ownerManager",
		Sig:             signature,
		ID:              selector[:],
		StateMutability: stateMutability,
		Inputs:          arguments,
		Outputs:         abi.Arguments{},
	}, nil
}

func mustSelector(signature string) [4]byte {
	var selector [4]byte
	copy(selector[:], crypto.Keccak256([]byte(signature))[:4])
	return selector
}

// OwnerManagement describes one pre-built owner-management proposal.
type OwnerManagement struct {
	Operation string
	Calldata  []byte
}

// BuildAddOwnerProposal creates a proposal that adds an owner.
func BuildAddOwnerProposal(newOwner common.Address, threshold uint64) (OwnerManagement, error) {
	calldata, err := AddOwnerCalldata(newOwner, threshold)
	if err != nil {
		return OwnerManagement{}, err
	}
	return OwnerManagement{Operation: fmt.Sprintf("add owner %s (threshold %d)", newOwner.Hex(), threshold), Calldata: calldata}, nil
}

// BuildRemoveOwnerProposal creates a proposal that removes an owner.
func BuildRemoveOwnerProposal(prevOwner, owner common.Address, threshold uint64) (OwnerManagement, error) {
	calldata, err := RemoveOwnerCalldata(prevOwner, owner, threshold)
	if err != nil {
		return OwnerManagement{}, err
	}
	return OwnerManagement{Operation: fmt.Sprintf("remove owner %s (threshold %d)", owner.Hex(), threshold), Calldata: calldata}, nil
}

// BuildChangeThresholdProposal creates a proposal that changes the threshold.
func BuildChangeThresholdProposal(threshold uint64) (OwnerManagement, error) {
	calldata, err := ChangeThresholdCalldata(threshold)
	if err != nil {
		return OwnerManagement{}, err
	}
	return OwnerManagement{Operation: fmt.Sprintf("change threshold to %d", threshold), Calldata: calldata}, nil
}
