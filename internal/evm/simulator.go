package evm

import (
	"context"
	"errors"
	"fmt"
	"math/big"
)

type SimulationResult struct {
	Block      BlockIdentity
	GasLimit   uint64
	ReturnData []byte
	Trace      *TraceResult
}

type TraceResult struct {
	Provider string
	Summary  string
}

type Simulator struct{}

func NewSimulator() *Simulator { return &Simulator{} }

func (simulator *Simulator) Simulate(ctx context.Context, rpc RPC, call TransactionCall, block BlockIdentity) (SimulationResult, error) {
	if rpc == nil || call.Value == nil {
		return SimulationResult{}, &EngineError{Code: ErrorSimulationFailed, Field: "RPC client or call value"}
	}
	if err := ctx.Err(); err != nil {
		return SimulationResult{}, &EngineError{Code: ErrorSimulationFailed, Field: "context", Cause: err}
	}
	gasLimit, err := rpc.EstimateGas(ctx, call, block)
	if err != nil {
		return SimulationResult{}, &EngineError{Code: ErrorSimulationFailed, Field: "gas estimate", Cause: err}
	}
	if gasLimit == 0 || gasLimit > 30_000_000 {
		return SimulationResult{}, &EngineError{Code: ErrorSimulationFailed, Field: "gas estimate", Cause: fmt.Errorf("outside policy")}
	}
	simulatedCall := call
	simulatedCall.Value = new(big.Int).Set(call.Value)
	simulatedCall.Input = append([]byte(nil), call.Input...)
	simulatedCall.Gas = gasLimit
	returnData, err := rpc.CallContract(ctx, simulatedCall, block)
	if err != nil {
		var revertError *RevertError
		if errors.As(err, &revertError) {
			err = decodeRevertError(revertError)
		}
		return SimulationResult{}, &EngineError{Code: ErrorSimulationFailed, Field: "eth_call", Cause: err}
	}
	return SimulationResult{
		Block:      block,
		GasLimit:   gasLimit,
		ReturnData: append([]byte(nil), returnData...),
	}, nil
}

func decodeRevertError(source *RevertError) *RevertError {
	result := &RevertError{Kind: RevertUnknown, Data: append([]byte(nil), source.Data...), Cause: source.Cause}
	if len(source.Data) < 4 {
		return result
	}
	switch {
	case string(source.Data[:4]) == string([]byte{0x08, 0xc3, 0x79, 0xa0}):
		reason, err := decodeABIString(source.Data[4:], 256)
		if err == nil {
			result.Kind = RevertErrorString
			result.Reason = reason
		}
	case string(source.Data[:4]) == string([]byte{0x4e, 0x48, 0x7b, 0x71}) && len(source.Data) == 36:
		result.Kind = RevertPanic
		result.PanicCode = new(big.Int).SetBytes(source.Data[4:])
	}
	return result
}
