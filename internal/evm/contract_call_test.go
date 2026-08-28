package evm_test

import (
	"bytes"
	"context"
	"math/big"
	"testing"
	"time"

	"blocowallet/internal/evm"
	"blocowallet/internal/wallet"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

const callABIFixture = `[
	{"type":"function","name":"deposit","stateMutability":"payable","inputs":[],"outputs":[]},
	{"type":"function","name":"register","stateMutability":"nonpayable","inputs":[{"type":"address","name":"owner"},{"type":"uint256","name":"amount"}],"outputs":[{"type":"bool","name":"ok"}]}
]`

func TestParseCallABIStrictAndRoundTrip(t *testing.T) {
	callABI, err := evm.ParseCallABI([]byte(callABIFixture), evm.ABISourceLocal)
	if err != nil {
		t.Fatal(err)
	}
	if callABI.ABIHash() == (common.Hash{}) || len(callABI.Methods) != 2 {
		t.Fatalf("unexpected ABI handle: %+v", callABI)
	}
	method, exists := callABI.Methods["register"]
	if !exists || len(method.ID) != 4 {
		t.Fatalf("register method missing: %+v", callABI.Methods)
	}
	if !bytes.Equal(method.ID, crypto.Keccak256([]byte("register(address,uint256)"))[:4]) {
		t.Fatalf("register selector mismatch: %x", method.ID)
	}
	duplicate := bytes.Replace([]byte(callABIFixture), []byte(`"name":"deposit"`), []byte(`"name":"deposit","name":"deposit"`), 1)
	if _, err := evm.ParseCallABI(duplicate, evm.ABISourceLocal); err == nil {
		t.Fatal("duplicate ABI JSON keys were accepted")
	}
	trailing := append([]byte(callABIFixture), []byte(" {}")...)
	if _, err := evm.ParseCallABI(trailing, evm.ABISourceLocal); err == nil {
		t.Fatal("trailing ABI JSON was accepted")
	}
	if _, err := evm.ParseCallABI([]byte(`[{"type":"function","name":"f","inputs":[{"type":"tuple","name":"t"}]}]`), evm.ABISourceLocal); err == nil {
		t.Fatal("tuple ABI input was accepted")
	}
	if _, err := evm.ParseCallABI([]byte(`[]`), evm.ABISourceLocal); err == nil {
		t.Fatal("empty ABI was accepted")
	}
	if _, err := evm.ParseCallABI([]byte(callABIFixture), evm.ABISourceKind("remote")); err == nil {
		t.Fatal("unknown ABI source was accepted")
	}
}

func TestContractCallABISupportsBoundedIntegersFixedBytesAndArrays(t *testing.T) {
	from := common.HexToAddress("0x9d8A62f656a8d1615C1294fd71e9CFb3E4855A4F")
	contract := common.HexToAddress("0x3333333333333333333333333333333333333333")
	blockHash := common.HexToHash("0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	repository := &engineRepository{}
	rpc := &engineRPC{
		simulationRPC: simulationRPC{callResult: []byte{}, estimate: 150_000, gasPrice: big.NewInt(1_000_000_000)},
		header:        evm.BlockHeader{BlockIdentity: evm.BlockIdentity{Number: 100, Hash: blockHash}, GasLimit: 30_000_000},
	}
	ids := []string{
		"31111111-1111-4111-8111-111111111111",
		"51111111-1111-4111-8111-111111111111",
		"61111111-1111-4111-8111-111111111111",
		"32222222-2222-4222-8222-222222222222",
	}
	index := 0
	engine, err := evm.NewEngine(repository, rpc, vectorSigner{}, evm.EngineOptions{
		Now: time.Now, NewID: func() (string, error) {
			value := ids[index]
			index++
			return value, nil
		}, ReservationTTL: time.Minute, ApprovalTTL: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	abiJSON := `[
		{"type":"function","name":"submit","stateMutability":"nonpayable","inputs":[{"type":"uint64","name":"deadline"},{"type":"bytes32","name":"digest"},{"type":"uint256[2]","name":"pair"}],"outputs":[]},
		{"type":"function","name":"bounded","stateMutability":"nonpayable","inputs":[{"type":"int32","name":"delta"}],"outputs":[]}
	]`
	request := evm.PrepareContractCallRequest{
		OperationID: "41111111-1111-4111-8111-111111111111", PlanGeneration: 1,
		AccountID: "8b9b0587-388e-4fca-bba4-bf544ebe53ca", ChainID: 1, From: from,
		Contract: contract, Value: new(big.Int), ABI: []byte(abiJSON), ABISource: evm.ABISourceLocal,
		Method: "submit", Args: []byte(`["18446744073709551615","0x112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00",[7,9]]`),
	}
	prepared, err := engine.PrepareContractCall(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if len(prepared.Plan().Transaction().Data()) != 4+32*4 {
		t.Fatalf("unexpected bounded ABI calldata: %x", prepared.Plan().Transaction().Data())
	}
	overflow := request
	overflow.Args = []byte(`["18446744073709551616","0x112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00",[7,9]]`)
	if _, err := engine.PrepareContractCall(context.Background(), overflow); err == nil {
		t.Fatal("uint64 overflow was accepted")
	}
	fixedSize := request
	fixedSize.Args = []byte(`["1","0x112233445566778899aabbccddeeff00112233445566778899aabbccddeeff00",[7]]`)
	if _, err := engine.PrepareContractCall(context.Background(), fixedSize); err == nil {
		t.Fatal("fixed array size mismatch was accepted")
	}
	shortBytes := request
	shortBytes.Args = []byte(`["1","0x11",[7,9]]`)
	if _, err := engine.PrepareContractCall(context.Background(), shortBytes); err == nil {
		t.Fatal("short bytes32 argument was accepted")
	}
	bounded := request
	bounded.Method = "bounded"
	bounded.Args = []byte(`["2147483647"]`)
	if _, err := engine.PrepareContractCall(context.Background(), bounded); err != nil {
		t.Fatal(err)
	}
	boundedOverflow := bounded
	boundedOverflow.Args = []byte(`["2147483648"]`)
	if _, err := engine.PrepareContractCall(context.Background(), boundedOverflow); err == nil {
		t.Fatal("int32 overflow was accepted")
	}
}

func TestContractCallPrepareBindsABIAndRequiresCriticalConfirmationForValue(t *testing.T) {
	from := common.HexToAddress("0x9d8A62f656a8d1615C1294fd71e9CFb3E4855A4F")
	contract := common.HexToAddress("0x3333333333333333333333333333333333333333")
	blockHash := common.HexToHash("0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	repository := &engineRepository{}
	rpc := &engineRPC{
		simulationRPC: simulationRPC{callResult: common.LeftPadBytes([]byte{1}, 32), estimate: 150_000, gasPrice: big.NewInt(1_000_000_000)},
		header:        evm.BlockHeader{BlockIdentity: evm.BlockIdentity{Number: 100, Hash: blockHash}, GasLimit: 30_000_000},
	}
	ids := []string{
		"31111111-1111-4111-8111-111111111111",
		"51111111-1111-4111-8111-111111111111",
		"61111111-1111-4111-8111-111111111111",
		"32222222-2222-4222-8222-222222222222",
	}
	index := 0
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	engine, err := evm.NewEngine(repository, rpc, vectorSigner{}, evm.EngineOptions{
		Now: func() time.Time { return now }, NewID: func() (string, error) {
			value := ids[index]
			index++
			return value, nil
		}, ReservationTTL: time.Minute, ApprovalTTL: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	request := evm.PrepareContractCallRequest{
		OperationID: "41111111-1111-4111-8111-111111111111", PlanGeneration: 1,
		AccountID: "8b9b0587-388e-4fca-bba4-bf544ebe53ca", ChainID: 1, From: from,
		Contract: contract, Value: new(big.Int), ABI: []byte(callABIFixture), ABISource: evm.ABISourceLocal,
		Method: "register", Args: []byte(`["0x1111111111111111111111111111111111111111","7"]`),
	}
	prepared, err := engine.PrepareContractCall(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	preview := prepared.Plan().ContractCallPreview()
	if preview.Method != "register" || preview.ABISource != evm.ABISourceLocal || preview.Output != "true" || len(preview.Calldata) != 68 {
		t.Fatalf("unexpected contract call preview: %+v", preview)
	}
	if !bytes.Equal(preview.Calldata[:4], crypto.Keccak256([]byte("register(address,uint256)"))[:4]) {
		t.Fatalf("contract call selector mismatch: %x", preview.Calldata[:4])
	}
	if len(prepared.Findings()) == 0 || prepared.Findings()[0].ID != evm.RiskFindingContractCall {
		t.Fatalf("contract call risk finding missing: %+v", prepared.Findings())
	}
	// Zero value: standard confirmation suffices.
	result, err := engine.ApproveSignAndBroadcast(context.Background(), wallet.CapabilityHandle{}, prepared, evm.ApprovalRequest{
		AuthorizationEpoch: 1, RiskLevel: evm.RiskNormal, ConfirmationLevel: evm.ConfirmationStandard,
	})
	if err != nil {
		t.Fatal(err)
	}
	if repository.record.Operation != evm.OperationContractCall || repository.record.Counterparty != contract {
		t.Fatalf("contract call authorization was not bound: %+v", repository.record)
	}
	repository.record.TransactionHash = result.Hash
	repository.record.State = evm.TransactionSubmitted
	trackerRPC := &trackingRPC{
		simulationRPC: simulationRPC{},
		receipt: evm.Receipt{
			TransactionHash: result.Hash,
			Block:           evm.BlockIdentity{Number: 100, Hash: blockHash},
			Status:          1, GasUsed: 100_000, EffectiveGasPrice: big.NewInt(1_000_000_000),
		},
		receiptFound: true,
		canonical:    evm.BlockHeader{BlockIdentity: evm.BlockIdentity{Number: 100, Hash: blockHash}},
		head:         evm.BlockHeader{BlockIdentity: evm.BlockIdentity{Number: 101, Hash: common.HexToHash("0xdddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd")}},
	}
	tracking, err := evm.NewReceiptTracker(repository, trackerRPC).TrackOnce(context.Background(), result.TransactionID, 1, now.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if tracking.State != evm.TransactionConfirmed {
		t.Fatalf("contract call without expected effect was not confirmed by status: %+v", tracking)
	}

	// Payable call with native value is critical and requires reinforced confirmation.
	valueRequest := request
	valueRequest.Value = big.NewInt(1)
	valueRequest.Method = "deposit"
	valueRequest.Args = []byte(`[]`)
	rpc.callResult = []byte{}
	valuePrepared, err := engine.PrepareContractCall(context.Background(), valueRequest)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.ApproveSignAndBroadcast(context.Background(), wallet.CapabilityHandle{}, valuePrepared, evm.ApprovalRequest{
		AuthorizationEpoch: 1, RiskLevel: evm.RiskNormal, ConfirmationLevel: evm.ConfirmationStandard,
	}); !evm.IsErrorCode(err, evm.ErrorPolicyDenied) {
		t.Fatalf("payable contract call without reinforced confirmation returned %v", err)
	}
}
