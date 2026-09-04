package safe

import (
	"bytes"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

func TestOwnerManagerCalldataSelectors(t *testing.T) {
	tests := []struct {
		name      string
		signature string
		build     func() ([]byte, error)
	}{
		{
			name: "addOwnerWithThreshold", signature: "addOwnerWithThreshold(address,uint256)",
			build: func() ([]byte, error) {
				return AddOwnerCalldata(common.HexToAddress("0x1111111111111111111111111111111111111111"), 2)
			},
		},
		{
			name: "removeOwner", signature: "removeOwner(address,address,uint256)",
			build: func() ([]byte, error) {
				return RemoveOwnerCalldata(common.HexToAddress("0x2222222222222222222222222222222222222222"), common.HexToAddress("0x1111111111111111111111111111111111111111"), 1)
			},
		},
		{
			name: "changeThreshold", signature: "changeThreshold(uint256)",
			build: func() ([]byte, error) {
				return ChangeThresholdCalldata(1)
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			calldata, err := test.build()
			if err != nil {
				t.Fatal(err)
			}
			selector := crypto.Keccak256([]byte(test.signature))[:4]
			if len(calldata) < 4 || !bytes.Equal(calldata[:4], selector) {
				t.Fatalf("selector mismatch: %x != %x", calldata[:4], selector)
			}
		})
	}
}

func TestFindOwnerLink(t *testing.T) {
	ownerA := common.HexToAddress("0x1111111111111111111111111111111111111111")
	ownerB := common.HexToAddress("0x2222222222222222222222222222222222222222")
	ownerC := common.HexToAddress("0x3333333333333333333333333333333333333333")
	owners := []common.Address{ownerA, ownerB, ownerC}
	prev, found := findOwnerLink(owners, ownerA)
	if !found || prev != (common.Address{}) {
		t.Fatalf("first owner predecessor must be zero: %s %v", prev, found)
	}
	prev, found = findOwnerLink(owners, ownerB)
	if !found || prev != ownerA {
		t.Fatalf("owner B predecessor must be A: %s %v", prev, found)
	}
	if _, found := findOwnerLink(owners, common.HexToAddress("0x4444444444444444444444444444444444444444")); found {
		t.Fatal("unknown owner must not be found")
	}
	_ = big.NewInt
}
