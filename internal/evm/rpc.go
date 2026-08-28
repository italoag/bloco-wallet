package evm

import (
	"context"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
)

type RevertKind string

const (
	RevertErrorString RevertKind = "error_string"
	RevertPanic       RevertKind = "panic"
	RevertUnknown     RevertKind = "unknown"
)

type RevertError struct {
	Kind      RevertKind
	Reason    string
	PanicCode *big.Int
	Data      []byte
	Cause     error
}

func (revertError *RevertError) Error() string {
	if revertError == nil {
		return "EVM execution reverted"
	}
	return "EVM execution reverted: " + string(revertError.Kind)
}

func (revertError *RevertError) Unwrap() error {
	if revertError == nil {
		return nil
	}
	return revertError.Cause
}

type BroadcastFailureKind string

const (
	BroadcastFailureRejected  BroadcastFailureKind = "rejected"
	BroadcastFailureAmbiguous BroadcastFailureKind = "ambiguous"
	BroadcastFailureNonceLow  BroadcastFailureKind = "nonce_too_low"
)

type BroadcastError struct {
	Kind  BroadcastFailureKind
	Cause error
}

func (broadcastError *BroadcastError) Error() string {
	if broadcastError == nil {
		return "EVM broadcast failed"
	}
	return "EVM broadcast failed: " + string(broadcastError.Kind)
}

func (broadcastError *BroadcastError) Unwrap() error {
	if broadcastError == nil {
		return nil
	}
	return broadcastError.Cause
}

type ProviderBinding [32]byte

type BlockIdentity struct {
	Number uint64
	Hash   common.Hash
}

type BlockHeader struct {
	BlockIdentity
	ParentHash    common.Hash
	GasLimit      uint64
	BaseFeePerGas *big.Int
}

type ReceiptLog struct {
	Address common.Address
	Topics  []common.Hash
	Data    []byte
}

type Receipt struct {
	TransactionHash   common.Hash
	Block             BlockIdentity
	TransactionIndex  uint64
	Status            uint64
	GasUsed           uint64
	EffectiveGasPrice *big.Int
	Logs              []ReceiptLog
}

type TransactionCall struct {
	From                 common.Address
	To                   common.Address
	Value                *big.Int
	Input                []byte
	Gas                  uint64
	GasPrice             *big.Int
	MaxFeePerGas         *big.Int
	MaxPriorityFeePerGas *big.Int
}

type RPC interface {
	ProviderBinding() ProviderBinding
	ChainID() uint64
	LatestHeader(context.Context) (BlockHeader, error)
	PendingNonceAt(context.Context, common.Address) (uint64, error)
	CallContract(context.Context, TransactionCall, BlockIdentity) ([]byte, error)
	EstimateGas(context.Context, TransactionCall, BlockIdentity) (uint64, error)
	SuggestGasPrice(context.Context) (*big.Int, error)
	SuggestGasTipCap(context.Context) (*big.Int, error)
	SendRawTransaction(context.Context, []byte) (common.Hash, error)
	CodeAt(context.Context, common.Address, BlockIdentity) ([]byte, error)
	TransactionReceipt(context.Context, common.Hash) (Receipt, bool, error)
	HeaderByNumber(context.Context, uint64) (BlockHeader, bool, error)
}
