package evm_test

import (
	"math/big"
	"testing"

	"blocowallet/internal/evm"

	"github.com/ethereum/go-ethereum/common"
)

func TestPlannerMatchesEIP155SigningVector(t *testing.T) {
	intent, err := evm.NewNativeTransferIntent(
		"8b9b0587-388e-4fca-bba4-bf544ebe53ca",
		1,
		common.HexToAddress("0x9d8a62f656a8d1615c1294fd71e9cfb3e4855a4f"),
		common.HexToAddress("0x3535353535353535353535353535353535353535"),
		big.NewInt(1_000_000_000_000_000_000),
	)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := evm.NewPlanner().PlanNative(intent, evm.NativePlanInput{
		ProviderBinding:       evm.ProviderBinding{1},
		Nonce:                 9,
		GasLimit:              21_000,
		LegacyGasPrice:        big.NewInt(20_000_000_000),
		SimulationBlockNumber: 1,
		SimulationBlockHash:   common.HexToHash("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
	})
	if err != nil {
		t.Fatal(err)
	}
	want := common.HexToHash("0xdaf5a779ae972f972197303d7b574746c7ef83eadac0f2791ad23db92e4c8e53")
	if common.Hash(plan.TransactionDigest()) != want {
		t.Fatalf("unexpected EIP-155 digest: %x", plan.TransactionDigest())
	}
}

func TestPlannerBuildsERC20TransferWithCanonicalCalldata(t *testing.T) {
	intent, err := evm.NewERC20TransferIntent(
		"8b9b0587-388e-4fca-bba4-bf544ebe53ca",
		1,
		common.HexToAddress("0x1111111111111111111111111111111111111111"),
		common.HexToAddress("0x3333333333333333333333333333333333333333"),
		common.HexToAddress("0x2222222222222222222222222222222222222222"),
		big.NewInt(1_500_000),
	)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := evm.NewPlanner().PlanERC20Transfer(intent, evm.ERC20PlanInput{
		NativePlanInput: evm.NativePlanInput{
			ProviderBinding:       evm.ProviderBinding{1},
			Nonce:                 3,
			GasLimit:              65_000,
			LegacyGasPrice:        big.NewInt(10_000_000_000),
			SimulationBlockNumber: 21_000_000,
			SimulationBlockHash:   common.HexToHash("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
		},
		Metadata: evm.TokenMetadata{Name: "USD Coin", Symbol: "USDC", Decimals: 6, BlockNumber: 21_000_000},
	})
	if err != nil {
		t.Fatal(err)
	}
	transaction := plan.Transaction()
	wantData := common.FromHex("0xa9059cbb0000000000000000000000002222222222222222222222222222222222222222000000000000000000000000000000000000000000000000000000000016e360")
	if transaction.To() == nil || *transaction.To() != common.HexToAddress("0x3333333333333333333333333333333333333333") || transaction.Value().Sign() != 0 {
		t.Fatalf("unexpected ERC-20 transaction target/value: to=%v value=%s", transaction.To(), transaction.Value())
	}
	if string(transaction.Data()) != string(wantData) {
		t.Fatalf("unexpected ERC-20 calldata: %x", transaction.Data())
	}
	transaction.Data()[0] = 0
	if string(plan.Transaction().Data()) != string(wantData) {
		t.Fatal("caller mutated frozen transaction calldata")
	}
	asset := plan.Asset()
	if asset.Symbol != "USDC" || asset.Decimals != 6 || asset.Contract != common.HexToAddress("0x3333333333333333333333333333333333333333") {
		t.Fatalf("unexpected token asset: %+v", asset)
	}
}

func TestPlannerBuildsNativeDynamicFeePlan(t *testing.T) {
	intent, err := evm.NewNativeTransferIntent(
		"8b9b0587-388e-4fca-bba4-bf544ebe53ca",
		11_155_111,
		common.HexToAddress("0x1111111111111111111111111111111111111111"),
		common.HexToAddress("0x2222222222222222222222222222222222222222"),
		big.NewInt(5_000_000_000_000_000),
	)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := evm.NewPlanner().PlanNativeDynamicFee(intent, evm.DynamicFeePlanInput{
		ProviderBinding:       evm.ProviderBinding{1},
		Nonce:                 9,
		GasLimit:              25_000,
		GasFeeCap:             big.NewInt(30_000_000_000),
		GasTipCap:             big.NewInt(2_000_000_000),
		SimulationBlockNumber: 6_000_000,
		SimulationBlockHash:   common.HexToHash("0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"),
	})
	if err != nil {
		t.Fatal(err)
	}
	transaction := plan.Transaction()
	if transaction.Type() != 2 || transaction.GasFeeCap().Cmp(big.NewInt(30_000_000_000)) != 0 || transaction.GasTipCap().Cmp(big.NewInt(2_000_000_000)) != 0 {
		t.Fatalf("unexpected dynamic fee transaction: type=%d feeCap=%s tipCap=%s", transaction.Type(), transaction.GasFeeCap(), transaction.GasTipCap())
	}
	if plan.MaximumGasCost().Cmp(big.NewInt(750_000_000_000_000)) != 0 {
		t.Fatalf("unexpected maximum gas cost: %s", plan.MaximumGasCost())
	}
}

func TestPlannerBuildsDeterministicNativeLegacyPlan(t *testing.T) {
	amount := big.NewInt(1_000_000_000_000_000_000)
	intent, err := evm.NewNativeTransferIntent(
		"8b9b0587-388e-4fca-bba4-bf544ebe53ca",
		1,
		common.HexToAddress("0x1111111111111111111111111111111111111111"),
		common.HexToAddress("0x2222222222222222222222222222222222222222"),
		amount,
	)
	if err != nil {
		t.Fatal(err)
	}
	input := evm.NativePlanInput{
		ProviderBinding:       evm.ProviderBinding{1},
		Nonce:                 7,
		GasLimit:              21_000,
		LegacyGasPrice:        big.NewInt(20_000_000_000),
		SimulationBlockNumber: 21_000_000,
		SimulationBlockHash:   common.HexToHash("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
	}
	planner := evm.NewPlanner()
	first, err := planner.PlanNative(intent, input)
	if err != nil {
		t.Fatal(err)
	}

	amount.SetInt64(1)
	input.LegacyGasPrice.SetInt64(1)
	transaction := first.Transaction()
	if first.ChainID().Cmp(big.NewInt(1)) != 0 || transaction.Nonce() != 7 || transaction.Gas() != 21_000 {
		t.Fatalf("unexpected transaction identity: chain=%s nonce=%d gas=%d", first.ChainID(), transaction.Nonce(), transaction.Gas())
	}
	if transaction.To() == nil || *transaction.To() != common.HexToAddress("0x2222222222222222222222222222222222222222") {
		t.Fatalf("unexpected recipient: %v", transaction.To())
	}
	if transaction.Value().Cmp(big.NewInt(1_000_000_000_000_000_000)) != 0 || transaction.GasPrice().Cmp(big.NewInt(20_000_000_000)) != 0 {
		t.Fatalf("planner retained mutable caller values: value=%s gasPrice=%s", transaction.Value(), transaction.GasPrice())
	}

	secondIntent, err := evm.NewNativeTransferIntent(
		"8b9b0587-388e-4fca-bba4-bf544ebe53ca",
		1,
		common.HexToAddress("0x1111111111111111111111111111111111111111"),
		common.HexToAddress("0x2222222222222222222222222222222222222222"),
		big.NewInt(1_000_000_000_000_000_000),
	)
	if err != nil {
		t.Fatal(err)
	}
	second, err := planner.PlanNative(secondIntent, evm.NativePlanInput{
		ProviderBinding:       evm.ProviderBinding{1},
		Nonce:                 7,
		GasLimit:              21_000,
		LegacyGasPrice:        big.NewInt(20_000_000_000),
		SimulationBlockNumber: 21_000_000,
		SimulationBlockHash:   common.HexToHash("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.PlanHash() != second.PlanHash() || first.TransactionDigest() != second.TransactionDigest() {
		t.Fatal("equivalent native intents produced different canonical plans")
	}
}
