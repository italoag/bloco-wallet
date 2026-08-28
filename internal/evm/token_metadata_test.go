package evm_test

import (
	"context"
	"fmt"
	"math/big"
	"testing"

	"blocowallet/internal/evm"

	"github.com/ethereum/go-ethereum/common"
)

type metadataRPC struct {
	simulationRPC
	allowance *big.Int
}

func (rpc *metadataRPC) CodeAt(context.Context, common.Address, evm.BlockIdentity) ([]byte, error) {
	return []byte{0x60, 0x01}, nil
}
func (rpc *metadataRPC) CallContract(_ context.Context, call evm.TransactionCall, _ evm.BlockIdentity) ([]byte, error) {
	if len(call.Input) != 4 && len(call.Input) != 68 {
		return nil, fmt.Errorf("unexpected metadata calldata")
	}
	switch fmt.Sprintf("%x", call.Input[:4]) {
	case "06fdde03":
		return common.FromHex("0x0000000000000000000000000000000000000000000000000000000000000020000000000000000000000000000000000000000000000000000000000000000855534420436f696e000000000000000000000000000000000000000000000000"), nil
	case "95d89b41":
		return common.FromHex("0x000000000000000000000000000000000000000000000000000000000000002000000000000000000000000000000000000000000000000000000000000000045553444300000000000000000000000000000000000000000000000000000000"), nil
	case "313ce567":
		return common.LeftPadBytes(big.NewInt(6).Bytes(), 32), nil
	case "dd62ed3e":
		if rpc.allowance != nil {
			return common.LeftPadBytes(rpc.allowance.Bytes(), 32), nil
		}
		return make([]byte, 32), nil
	default:
		return nil, fmt.Errorf("unknown selector")
	}
}

func TestTokenMetadataResolverUsesOnChainValuesAtCanonicalBlock(t *testing.T) {
	rpc := &metadataRPC{}
	block := evm.BlockIdentity{Number: 100, Hash: common.HexToHash("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")}
	contract := common.HexToAddress("0x3333333333333333333333333333333333333333")
	sender := common.HexToAddress("0x1111111111111111111111111111111111111111")
	metadata, err := evm.NewTokenMetadataResolver().Resolve(context.Background(), rpc, sender, contract, block)
	if err != nil {
		t.Fatal(err)
	}
	if metadata.Name != "USD Coin" || metadata.Symbol != "USDC" || metadata.Decimals != 6 || metadata.BlockNumber != 100 {
		t.Fatalf("unexpected on-chain metadata: %+v", metadata)
	}
}
