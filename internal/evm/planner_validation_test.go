package evm_test

import (
	"errors"
	"math/big"
	"testing"

	"blocowallet/internal/evm"

	"github.com/ethereum/go-ethereum/common"
)

func TestNilDomainValuesFailClosed(t *testing.T) {
	var native evm.NativeTransferIntent
	if native.Amount() != nil {
		t.Fatal("zero native intent exposed an amount")
	}
	if _, err := evm.NewPlanner().PlanNative(native, evm.NativePlanInput{}); err == nil {
		t.Fatal("zero native intent was planned")
	}
	var token evm.ERC20TransferIntent
	if _, err := evm.NewPlanner().PlanERC20Transfer(token, evm.ERC20PlanInput{}); err == nil {
		t.Fatal("zero ERC-20 intent was planned")
	}
	var plan *evm.FrozenPlan
	if plan.ChainID() != nil || plan.Transaction() != nil || plan.Asset() != (evm.Asset{}) || plan.MaximumGasCost() != nil {
		t.Fatal("nil frozen plan did not fail closed")
	}
}

func TestPlannerRejectsUnsafeFeeGasMetadataAndSimulation(t *testing.T) {
	intent := mustNativeIntent(t)
	validLegacy := evm.NativePlanInput{
		ProviderBinding:       evm.ProviderBinding{1},
		Nonce:                 1,
		GasLimit:              21_000,
		LegacyGasPrice:        big.NewInt(1),
		SimulationBlockNumber: 1,
		SimulationBlockHash:   common.HexToHash("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
	}
	legacyCases := []struct {
		name   string
		mutate func(*evm.NativePlanInput)
	}{
		{"provider", func(input *evm.NativePlanInput) { input.ProviderBinding = evm.ProviderBinding{} }},
		{"low gas", func(input *evm.NativePlanInput) { input.GasLimit = 20_999 }},
		{"high gas", func(input *evm.NativePlanInput) { input.GasLimit = 30_000_001 }},
		{"nil price", func(input *evm.NativePlanInput) { input.LegacyGasPrice = nil }},
		{"zero price", func(input *evm.NativePlanInput) { input.LegacyGasPrice = new(big.Int) }},
		{"oversized price", func(input *evm.NativePlanInput) { input.LegacyGasPrice = new(big.Int).Lsh(big.NewInt(1), 256) }},
		{"missing block number", func(input *evm.NativePlanInput) { input.SimulationBlockNumber = 0 }},
		{"missing block hash", func(input *evm.NativePlanInput) { input.SimulationBlockHash = common.Hash{} }},
	}
	for _, test := range legacyCases {
		t.Run("legacy "+test.name, func(t *testing.T) {
			input := validLegacy
			input.LegacyGasPrice = new(big.Int).Set(validLegacy.LegacyGasPrice)
			test.mutate(&input)
			_, err := evm.NewPlanner().PlanNative(intent, input)
			assertPlannerInvalid(t, err)
		})
	}

	validDynamic := evm.DynamicFeePlanInput{
		ProviderBinding:       evm.ProviderBinding{1},
		Nonce:                 1,
		GasLimit:              21_000,
		GasFeeCap:             big.NewInt(2),
		GasTipCap:             big.NewInt(1),
		SimulationBlockNumber: 1,
		SimulationBlockHash:   validLegacy.SimulationBlockHash,
	}
	dynamicCases := []struct {
		name   string
		mutate func(*evm.DynamicFeePlanInput)
	}{
		{"provider", func(input *evm.DynamicFeePlanInput) { input.ProviderBinding = evm.ProviderBinding{} }},
		{"low gas", func(input *evm.DynamicFeePlanInput) { input.GasLimit = 1 }},
		{"nil fee", func(input *evm.DynamicFeePlanInput) { input.GasFeeCap = nil }},
		{"zero fee", func(input *evm.DynamicFeePlanInput) { input.GasFeeCap = new(big.Int) }},
		{"oversized fee", func(input *evm.DynamicFeePlanInput) { input.GasFeeCap = new(big.Int).Lsh(big.NewInt(1), 256) }},
		{"nil tip", func(input *evm.DynamicFeePlanInput) { input.GasTipCap = nil }},
		{"negative tip", func(input *evm.DynamicFeePlanInput) { input.GasTipCap = big.NewInt(-1) }},
		{"oversized tip", func(input *evm.DynamicFeePlanInput) { input.GasTipCap = new(big.Int).Lsh(big.NewInt(1), 256) }},
		{"tip above fee", func(input *evm.DynamicFeePlanInput) { input.GasTipCap = big.NewInt(3) }},
		{"missing block number", func(input *evm.DynamicFeePlanInput) { input.SimulationBlockNumber = 0 }},
		{"missing block", func(input *evm.DynamicFeePlanInput) { input.SimulationBlockHash = common.Hash{} }},
	}
	for _, test := range dynamicCases {
		t.Run("dynamic "+test.name, func(t *testing.T) {
			input := validDynamic
			input.GasFeeCap = new(big.Int).Set(validDynamic.GasFeeCap)
			input.GasTipCap = new(big.Int).Set(validDynamic.GasTipCap)
			test.mutate(&input)
			_, err := evm.NewPlanner().PlanNativeDynamicFee(intent, input)
			assertPlannerInvalid(t, err)
		})
	}

	tokenIntent, err := evm.NewERC20TransferIntent(
		intent.AccountID(), intent.ChainID(), intent.From(),
		common.HexToAddress("0x3333333333333333333333333333333333333333"), intent.To(), big.NewInt(1),
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range []evm.ERC20PlanInput{
		{NativePlanInput: evm.NativePlanInput{ProviderBinding: evm.ProviderBinding{1}, GasLimit: 1, LegacyGasPrice: big.NewInt(1), SimulationBlockNumber: 1, SimulationBlockHash: validLegacy.SimulationBlockHash}, Metadata: evm.TokenMetadata{Name: "Token", Symbol: "TOK", Decimals: 18, BlockNumber: 1}},
		{NativePlanInput: evm.NativePlanInput{ProviderBinding: evm.ProviderBinding{1}, GasLimit: 21_000, LegacyGasPrice: nil, SimulationBlockNumber: 1, SimulationBlockHash: validLegacy.SimulationBlockHash}, Metadata: evm.TokenMetadata{Name: "Token", Symbol: "TOK", Decimals: 18, BlockNumber: 1}},
	} {
		_, err := evm.NewPlanner().PlanERC20Transfer(tokenIntent, input)
		assertPlannerInvalid(t, err)
	}
	metadataCases := []evm.TokenMetadata{
		{Name: "", Symbol: "TOK", Decimals: 18, BlockNumber: 1},
		{Name: "Token\nInjected", Symbol: "TOK", Decimals: 18, BlockNumber: 1},
		{Name: "Token", Symbol: "", Decimals: 18, BlockNumber: 1},
		{Name: "Token", Symbol: "TOK", Decimals: 37, BlockNumber: 1},
		{Name: "Token", Symbol: "TOK", Decimals: 18, BlockNumber: 2},
	}
	for _, metadata := range metadataCases {
		input := evm.ERC20PlanInput{NativePlanInput: validLegacy, Metadata: metadata}
		_, err := evm.NewPlanner().PlanERC20Transfer(tokenIntent, input)
		assertPlannerInvalid(t, err)
	}
}

func TestERC20DynamicPlannersRejectInvalidInputs(t *testing.T) {
	accountID := "8b9b0587-388e-4fca-bba4-bf544ebe53ca"
	from := common.HexToAddress("0x1111111111111111111111111111111111111111")
	contract := common.HexToAddress("0x3333333333333333333333333333333333333333")
	counterparty := common.HexToAddress("0x2222222222222222222222222222222222222222")
	transfer, err := evm.NewERC20TransferIntent(accountID, 1, from, contract, counterparty, big.NewInt(1))
	if err != nil {
		t.Fatal(err)
	}
	approval, err := evm.NewERC20ApproveIntent(accountID, 1, from, contract, counterparty, big.NewInt(1))
	if err != nil {
		t.Fatal(err)
	}
	valid := evm.ERC20DynamicPlanInput{
		DynamicFeePlanInput: evm.DynamicFeePlanInput{
			ProviderBinding: evm.ProviderBinding{1}, Nonce: 1, GasLimit: 50_000,
			GasFeeCap: big.NewInt(2), GasTipCap: big.NewInt(1), SimulationBlockNumber: 10,
			SimulationBlockHash: common.HexToHash("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
		},
		Metadata: evm.TokenMetadata{Name: "Token", Symbol: "TOK", Decimals: 18, BlockNumber: 10},
	}
	mutations := []func(*evm.ERC20DynamicPlanInput){
		func(value *evm.ERC20DynamicPlanInput) { value.ProviderBinding = evm.ProviderBinding{} },
		func(value *evm.ERC20DynamicPlanInput) { value.GasLimit = 1 },
		func(value *evm.ERC20DynamicPlanInput) { value.GasFeeCap = nil },
		func(value *evm.ERC20DynamicPlanInput) { value.GasTipCap = nil },
		func(value *evm.ERC20DynamicPlanInput) { value.GasTipCap = big.NewInt(3) },
		func(value *evm.ERC20DynamicPlanInput) { value.SimulationBlockHash = common.Hash{} },
		func(value *evm.ERC20DynamicPlanInput) { value.Metadata.Name = "" },
	}
	for _, mutate := range mutations {
		input := valid
		input.GasFeeCap = new(big.Int).Set(valid.GasFeeCap)
		input.GasTipCap = new(big.Int).Set(valid.GasTipCap)
		mutate(&input)
		if _, err := evm.NewPlanner().PlanERC20TransferDynamicFee(transfer, input); err == nil {
			t.Fatal("invalid dynamic ERC-20 transfer input was accepted")
		}
		if _, err := evm.NewPlanner().PlanERC20ApproveDynamicFee(approval, input); err == nil {
			t.Fatal("invalid dynamic ERC-20 approval input was accepted")
		}
	}
	legacy := evm.ERC20PlanInput{NativePlanInput: evm.NativePlanInput{
		ProviderBinding: evm.ProviderBinding{1}, GasLimit: 50_000, LegacyGasPrice: big.NewInt(1),
		SimulationBlockNumber: 10, SimulationBlockHash: valid.SimulationBlockHash,
	}, Metadata: valid.Metadata}
	if _, err := evm.NewPlanner().PlanERC20Approve(approval, legacy); err != nil {
		t.Fatal(err)
	}
	legacy.LegacyGasPrice = nil
	if _, err := evm.NewPlanner().PlanERC20Approve(approval, legacy); err == nil {
		t.Fatal("invalid legacy ERC-20 approval fee was accepted")
	}
	legacy.LegacyGasPrice = big.NewInt(1)
	legacy.Metadata.Name = ""
	if _, err := evm.NewPlanner().PlanERC20Approve(approval, legacy); err == nil {
		t.Fatal("invalid legacy ERC-20 approval metadata was accepted")
	}
	if _, err := evm.NewPlanner().PlanERC20Approve(evm.ERC20ApproveIntent{}, legacy); err == nil {
		t.Fatal("zero legacy ERC-20 approval intent was accepted")
	}
}

func TestSimulationBlockChangesPlanButNotTransactionDigest(t *testing.T) {
	intent := mustNativeIntent(t)
	input := evm.NativePlanInput{
		ProviderBinding:       evm.ProviderBinding{1},
		Nonce:                 1,
		GasLimit:              21_000,
		LegacyGasPrice:        big.NewInt(2),
		SimulationBlockNumber: 10,
		SimulationBlockHash:   common.HexToHash("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
	}
	first, err := evm.NewPlanner().PlanNative(intent, input)
	if err != nil {
		t.Fatal(err)
	}
	input.SimulationBlockNumber = 11
	input.SimulationBlockHash = common.HexToHash("0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	second, err := evm.NewPlanner().PlanNative(intent, input)
	if err != nil {
		t.Fatal(err)
	}
	if first.TransactionDigest() != second.TransactionDigest() || first.PlanHash() == second.PlanHash() {
		t.Fatal("simulation provenance was not bound independently of transaction digest")
	}
	input.SimulationBlockNumber = 10
	input.SimulationBlockHash = common.HexToHash("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	input.SimulationResultHash = common.HexToHash("0xcccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc")
	third, err := evm.NewPlanner().PlanNative(intent, input)
	if err != nil {
		t.Fatal(err)
	}
	if third.TransactionDigest() != first.TransactionDigest() || third.PlanHash() == first.PlanHash() || third.SimulationResultHash() != input.SimulationResultHash {
		t.Fatal("simulation result was not bound independently of transaction digest")
	}
	blockNumber, blockHash := first.SimulationBlock()
	if first.AccountID() != intent.AccountID() || first.From() != intent.From() || blockNumber != 10 || blockHash == (common.Hash{}) {
		t.Fatal("frozen plan getters returned inconsistent identity")
	}
}

func mustNativeIntent(t *testing.T) evm.NativeTransferIntent {
	t.Helper()
	intent, err := evm.NewNativeTransferIntent(
		"8b9b0587-388e-4fca-bba4-bf544ebe53ca", 1,
		common.HexToAddress("0x1111111111111111111111111111111111111111"),
		common.HexToAddress("0x2222222222222222222222222222222222222222"), big.NewInt(1),
	)
	if err != nil {
		t.Fatal(err)
	}
	return intent
}

func assertPlannerInvalid(t *testing.T, err error) {
	t.Helper()
	var engineError *evm.EngineError
	if !errors.As(err, &engineError) || engineError.Code != evm.ErrorInvalidIntent {
		t.Fatalf("expected invalid planner input, got %T: %v", err, err)
	}
}
