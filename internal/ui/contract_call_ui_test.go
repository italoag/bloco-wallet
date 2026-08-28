package ui

import (
	"context"
	"fmt"
	"testing"

	"blocowallet/internal/evm"
	"blocowallet/pkg/config"

	"github.com/ethereum/go-ethereum/common"
)

type failingContractCallUIEngine struct {
	cancellableUIEngine
}

func (engine *failingContractCallUIEngine) PrepareContractCall(context.Context, evm.PrepareContractCallRequest) (*evm.PreparedNativeTransfer, error) {
	return nil, fmt.Errorf("prepare failed")
}

func TestContractCallFlowValidatesFieldsAndSurfacesPrepareErrors(t *testing.T) {
	model := eligibleNativeTransferModel()
	model.transactionEngineFactory = func(ctx context.Context, _ config.Network) (TransactionEngine, error) {
		return &failingContractCallUIEngine{cancellableUIEngine: cancellableUIEngine{}}, nil
	}
	model.initContractCall()
	if model.contractCall == nil || model.contractCall.phase != contractCallSelectNetwork {
		t.Fatal("contract call flow did not initialize")
	}
	state := model.contractCall
	if len(state.networks) == 0 {
		t.Fatal("contract call flow requires networks")
	}
	state.inputs["contract"].SetValue("0x3333333333333333333333333333333333333333")
	if err := model.commitContractCallField(state, "contract"); err != nil {
		t.Fatal(err)
	}
	if state.contract != common.HexToAddress("0x3333333333333333333333333333333333333333") {
		t.Fatal("contract field was not committed")
	}
	state.inputs["contract"].SetValue("not-an-address")
	if err := model.commitContractCallField(state, "contract"); err == nil {
		t.Fatal("invalid contract address was accepted")
	}
	state.inputs["value"].SetValue("")
	if err := model.commitContractCallField(state, "value"); err != nil {
		t.Fatal(err)
	}
	if state.value == nil || state.value.Sign() != 0 {
		t.Fatalf("default contract call value is not zero: %v", state.value)
	}
	state.inputs["value"].SetValue("-5")
	if err := model.commitContractCallField(state, "value"); err == nil {
		t.Fatal("negative contract call value was accepted")
	}
	state.inputs["method"].SetValue("")
	if err := model.commitContractCallField(state, "method"); err == nil {
		t.Fatal("empty method was accepted")
	}
	state.inputs["abi"].SetValue("[]")
	state.inputs["method"].SetValue("deposit")
	state.inputs["args"].SetValue("[]")
	model.contractCall.phase = contractCallValue
	model.contractCall.generation = model.nextContractCallGeneration()
	command := model.startContractCallPrepare()
	if command == nil {
		t.Fatal("contract call prepare command was not created")
	}
	message := command()
	preparedMessage, ok := message.(contractCallPreparedMsg)
	if !ok || preparedMessage.err == nil {
		t.Fatalf("contract call prepare did not surface engine error: %+v", message)
	}
	updated, _ := model.updateContractCall(preparedMessage)
	updatedModel, ok := updated.(*CLIModel)
	if !ok {
		t.Fatal("contract call update returned the wrong model")
	}
	if updatedModel.contractCall.phase != contractCallValue || updatedModel.contractCall.err == "" {
		t.Fatalf("contract call prepare error did not restore input phase: %+v", updatedModel.contractCall)
	}
}

func TestContractCallFieldNavigation(t *testing.T) {
	model := eligibleNativeTransferModel()
	model.initContractCall()
	if model.contractCall == nil {
		t.Fatal("contract call flow did not initialize")
	}
	if contractCallNextPhase(contractCallContract) != contractCallABI || contractCallNextPhase(contractCallArgs) != contractCallValue {
		t.Fatal("contract call forward navigation is broken")
	}
	if contractCallPreviousPhase(contractCallValue) != contractCallArgs || contractCallPreviousPhase(contractCallABI) != contractCallContract {
		t.Fatal("contract call backward navigation is broken")
	}
	if contractCallFieldForPhase(contractCallMethod) != "method" {
		t.Fatal("contract call field mapping is broken")
	}
}
