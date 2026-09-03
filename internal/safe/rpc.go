package safe

import (
	"context"
	"fmt"
	"math/big"

	"blocowallet/internal/blockchain"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// OnChainReader queries Safe contract state through a validated RPC session.
type OnChainReader interface {
	Owners(context.Context, common.Address) ([]common.Address, error)
	Threshold(context.Context, common.Address) (uint64, error)
	Nonce(context.Context, common.Address) (*big.Int, error)
	CodeAt(context.Context, common.Address) ([]byte, error)
	ProxyCreationCode(context.Context, common.Address) ([]byte, error)
	PendingNonce(context.Context, common.Address) (uint64, error)
	SuggestGasPrice(context.Context) (*big.Int, error)
	Broadcast(context.Context, []byte) (common.Hash, error)
}

// RPCAdapter exposes Safe contract queries and execution broadcast over the
// application's validated RPC session.
type RPCAdapter struct {
	gateway *blockchain.RPCGateway
	session *blockchain.ValidatedRPCSession
}

// NewRPCAdapter creates the on-chain adapter. The session must already be
// chain-validated; the adapter refuses sessions bound to another chain.
func NewRPCAdapter(gateway *blockchain.RPCGateway, session *blockchain.ValidatedRPCSession) (*RPCAdapter, error) {
	if gateway == nil || session == nil || session.ChainID() <= 0 {
		return nil, fmt.Errorf("safe rpc: validated session is required")
	}
	return &RPCAdapter{gateway: gateway, session: session}, nil
}

// Owners reads getOwners() and ABI-decodes the address array.
func (adapter *RPCAdapter) Owners(ctx context.Context, safe common.Address) ([]common.Address, error) {
	result, err := adapter.call(ctx, safe, "getOwners()")
	if err != nil {
		return nil, err
	}
	return decodeAddressArray(result)
}

// Threshold reads getThreshold().
func (adapter *RPCAdapter) Threshold(ctx context.Context, safe common.Address) (uint64, error) {
	result, err := adapter.call(ctx, safe, "getThreshold()")
	if err != nil {
		return 0, err
	}
	return decodeUint64(result)
}

// Nonce reads nonce().
func (adapter *RPCAdapter) Nonce(ctx context.Context, safe common.Address) (*big.Int, error) {
	result, err := adapter.call(ctx, safe, "nonce()")
	if err != nil {
		return nil, err
	}
	return decodeBigInt(result)
}

// ProxyCreationCode reads proxyCreationCode() from the Safe factory so the
// CREATE2 address can be derived locally.
func (adapter *RPCAdapter) ProxyCreationCode(ctx context.Context, factory common.Address) ([]byte, error) {
	result, err := adapter.call(ctx, factory, "proxyCreationCode()")
	if err != nil {
		return nil, err
	}
	return decodeBytesABI(result)
}

// PendingNonce reads the pending nonce of the gas-payer account.
func (adapter *RPCAdapter) PendingNonce(ctx context.Context, account common.Address) (uint64, error) {
	client, err := blockchain.NewEVMRPC(adapter.gateway, adapter.session)
	if err != nil {
		return 0, err
	}
	return client.PendingNonceAt(ctx, account)
}

// SuggestGasPrice reads the network gas price for the execution envelope.
func (adapter *RPCAdapter) SuggestGasPrice(ctx context.Context) (*big.Int, error) {
	client, err := blockchain.NewEVMRPC(adapter.gateway, adapter.session)
	if err != nil {
		return nil, err
	}
	return client.SuggestGasPrice(ctx)
}

// CodeAt reads the deployed code of the Safe proxy.
func (adapter *RPCAdapter) CodeAt(ctx context.Context, safe common.Address) ([]byte, error) {
	var encoded string
	if err := adapter.gateway.Call(ctx, adapter.session, "eth_getCode", []any{safe.Hex(), "latest"}, &encoded); err != nil {
		return nil, fmt.Errorf("safe rpc: get code: %w", err)
	}
	return common.FromHex(encoded), nil
}

// Broadcast sends a signed execTransaction payload. The sender is the Safe
// contract itself, so the EOA-recovery check is intentionally skipped.
func (adapter *RPCAdapter) Broadcast(ctx context.Context, raw []byte) (common.Hash, error) {
	client, err := blockchain.NewEVMRPC(adapter.gateway, adapter.session)
	if err != nil {
		return common.Hash{}, err
	}
	return client.SendRawContractTransaction(ctx, raw)
}

func (adapter *RPCAdapter) call(ctx context.Context, to common.Address, signature string) ([]byte, error) {
	payload := crypto.Keccak256([]byte(signature))[:4]
	var encoded string
	if err := adapter.gateway.Call(ctx, adapter.session, "eth_call", []any{
		map[string]any{"to": to.Hex(), "data": "0x" + common.Bytes2Hex(payload)},
		"latest",
	}, &encoded); err != nil {
		return nil, fmt.Errorf("safe rpc: %s: %w", signature, err)
	}
	decoded := common.FromHex(encoded)
	if len(decoded) == 0 {
		return nil, fmt.Errorf("safe rpc: %s returned no data", signature)
	}
	return decoded, nil
}

func decodeBytesABI(result []byte) ([]byte, error) {
	if len(result) < 64 {
		return nil, fmt.Errorf("safe rpc: malformed bytes result")
	}
	offset := new(big.Int).SetBytes(result[0:32]).Uint64()
	if offset != 32 {
		return nil, fmt.Errorf("safe rpc: unexpected bytes offset %d", offset)
	}
	length := new(big.Int).SetBytes(result[32:64]).Uint64()
	if length > uint64(len(result)-64) {
		return nil, fmt.Errorf("safe rpc: bytes length out of bounds")
	}
	return append([]byte(nil), result[64:64+length]...), nil
}

func decodeAddressArray(data []byte) ([]common.Address, error) {
	if len(data) < 64 {
		return nil, fmt.Errorf("safe rpc: malformed owners result")
	}
	offset := new(big.Int).SetBytes(data[0:32]).Uint64()
	if offset < 32 || offset > uint64(len(data)) || uint64(len(data))-offset < 32 {
		return nil, fmt.Errorf("safe rpc: invalid owners offset")
	}
	count := new(big.Int).SetBytes(data[offset : offset+32]).Uint64()
	if count > 1024 || offset+32+count*32 > uint64(len(data)) {
		return nil, fmt.Errorf("safe rpc: owners length out of bounds")
	}
	owners := make([]common.Address, 0, count)
	for index := uint64(0); index < count; index++ {
		start := offset + 32 + index*32
		owners = append(owners, common.BytesToAddress(data[start:start+32]))
	}
	return owners, nil
}

func decodeUint64(data []byte) (uint64, error) {
	value, err := decodeBigInt(data)
	if err != nil {
		return 0, err
	}
	if !value.IsUint64() {
		return 0, fmt.Errorf("safe rpc: value exceeds uint64")
	}
	return value.Uint64(), nil
}

func decodeBigInt(data []byte) (*big.Int, error) {
	if len(data) != 32 {
		return nil, fmt.Errorf("safe rpc: malformed uint256 result")
	}
	return new(big.Int).SetBytes(data), nil
}
