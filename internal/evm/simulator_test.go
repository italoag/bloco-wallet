package evm_test

import (
	"context"
	"errors"
	"math/big"
	"strings"
	"testing"

	"blocowallet/internal/evm"

	"github.com/ethereum/go-ethereum/common"
)

type simulationRPC struct {
	callResult    []byte
	callError     error
	estimate      uint64
	estimateError error
	gasPrice      *big.Int
	gasTip        *big.Int
	gasPriceError error
	gasTipError   error
	callCount     int
	estimateCount int
	simulatedGas  uint64
}

func (rpc *simulationRPC) ProviderBinding() evm.ProviderBinding { return evm.ProviderBinding{1} }
func (rpc *simulationRPC) ChainID() uint64                      { return 1 }
func (rpc *simulationRPC) LatestHeader(context.Context) (evm.BlockHeader, error) {
	return evm.BlockHeader{}, nil
}
func (rpc *simulationRPC) PendingNonceAt(context.Context, common.Address) (uint64, error) {
	return 0, nil
}
func (rpc *simulationRPC) SuggestGasPrice(context.Context) (*big.Int, error) {
	if rpc.gasPriceError != nil {
		return nil, rpc.gasPriceError
	}
	if rpc.gasPrice != nil {
		return new(big.Int).Set(rpc.gasPrice), nil
	}
	return big.NewInt(1), nil
}
func (rpc *simulationRPC) SuggestGasTipCap(context.Context) (*big.Int, error) {
	if rpc.gasTipError != nil {
		return nil, rpc.gasTipError
	}
	if rpc.gasTip != nil {
		return new(big.Int).Set(rpc.gasTip), nil
	}
	return big.NewInt(1), nil
}
func (rpc *simulationRPC) SendRawTransaction(context.Context, []byte) (common.Hash, error) {
	return common.Hash{}, nil
}
func (rpc *simulationRPC) CodeAt(context.Context, common.Address, evm.BlockIdentity) ([]byte, error) {
	return []byte{1}, nil
}
func (rpc *simulationRPC) TransactionReceipt(context.Context, common.Hash) (evm.Receipt, bool, error) {
	return evm.Receipt{}, false, nil
}
func (rpc *simulationRPC) HeaderByNumber(context.Context, uint64) (evm.BlockHeader, bool, error) {
	return evm.BlockHeader{}, false, nil
}
func (rpc *simulationRPC) CallContract(_ context.Context, call evm.TransactionCall, _ evm.BlockIdentity) ([]byte, error) {
	rpc.callCount++
	rpc.simulatedGas = call.Gas
	return append([]byte(nil), rpc.callResult...), rpc.callError
}
func (rpc *simulationRPC) EstimateGas(context.Context, evm.TransactionCall, evm.BlockIdentity) (uint64, error) {
	rpc.estimateCount++
	return rpc.estimate, rpc.estimateError
}

func TestSimulatorDecodesBoundedRevertReason(t *testing.T) {
	data := common.FromHex("0x08c379a0000000000000000000000000000000000000000000000000000000000000002000000000000000000000000000000000000000000000000000000000000000046f6f707300000000000000000000000000000000000000000000000000000000")
	rpc := &simulationRPC{estimate: 21_000, callError: &evm.RevertError{Kind: evm.RevertUnknown, Data: data}}
	_, err := evm.NewSimulator().Simulate(context.Background(), rpc, evm.TransactionCall{Value: new(big.Int)}, evm.BlockIdentity{})
	var revertError *evm.RevertError
	if !errors.As(err, &revertError) || revertError.Kind != evm.RevertErrorString || revertError.Reason != "oops" || strings.Contains(err.Error(), "oops") {
		t.Fatalf("revert was not decoded safely: %v %+v", err, revertError)
	}
}

func TestSimulatorRequiresConclusiveCallAndGas(t *testing.T) {
	block := evm.BlockIdentity{Number: 10, Hash: common.HexToHash("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")}
	call := evm.TransactionCall{
		From:  common.HexToAddress("0x1111111111111111111111111111111111111111"),
		To:    common.HexToAddress("0x2222222222222222222222222222222222222222"),
		Value: new(big.Int),
	}
	rpc := &simulationRPC{callResult: []byte{1}, estimate: 21_000}
	result, err := evm.NewSimulator().Simulate(context.Background(), rpc, call, block)
	if err != nil {
		t.Fatal(err)
	}
	if result.GasLimit != 21_000 || len(result.ReturnData) != 1 || result.Block != block || rpc.callCount != 1 || rpc.estimateCount != 1 || rpc.simulatedGas != 21_000 {
		t.Fatalf("unexpected simulation result: %+v calls=%d estimates=%d", result, rpc.callCount, rpc.estimateCount)
	}

	remoteFailure := errors.New("remote reflected secret")
	failedRPC := &simulationRPC{callError: remoteFailure, estimate: 21_000}
	_, err = evm.NewSimulator().Simulate(context.Background(), failedRPC, call, block)
	if !evm.IsErrorCode(err, evm.ErrorSimulationFailed) || failedRPC.estimateCount != 1 || errors.Is(err, remoteFailure) == false {
		t.Fatalf("simulation did not fail closed: %v estimates=%d", err, failedRPC.estimateCount)
	}
}
