package evm_test

import (
	"context"
	"fmt"
	"math/big"
	"testing"

	"blocowallet/internal/evm"
)

func TestFeeOracleSelectsDynamicAndLegacyModels(t *testing.T) {
	dynamicRPC := &simulationRPC{}
	dynamicRPC.callResult = nil
	oracle := evm.NewFeeOracle()
	dynamic, err := oracle.Suggest(context.Background(), dynamicRPC, evm.BlockHeader{BaseFeePerGas: big.NewInt(1_000_000_000)})
	if err != nil {
		t.Fatal(err)
	}
	if dynamic.Model != evm.FeeDynamic || dynamic.GasTipCap.Cmp(big.NewInt(1)) != 0 || dynamic.GasFeeCap.Cmp(big.NewInt(2_000_000_001)) != 0 || dynamic.GasPrice != nil {
		t.Fatalf("unexpected dynamic fee suggestion: %+v", dynamic)
	}
	dynamicRPC.gasTipError = fmt.Errorf("method unavailable")
	fallback, err := oracle.Suggest(context.Background(), dynamicRPC, evm.BlockHeader{BaseFeePerGas: big.NewInt(1)})
	if err != nil || fallback.Model != evm.FeeLegacy || fallback.GasPrice.Cmp(big.NewInt(1)) != 0 {
		t.Fatalf("dynamic fee fallback failed: %+v %v", fallback, err)
	}
	dynamicRPC.gasTipError = nil
	legacy, err := oracle.Suggest(context.Background(), dynamicRPC, evm.BlockHeader{})
	if err != nil {
		t.Fatal(err)
	}
	if legacy.Model != evm.FeeLegacy || legacy.GasPrice.Cmp(big.NewInt(1)) != 0 || legacy.GasFeeCap != nil || legacy.GasTipCap != nil {
		t.Fatalf("unexpected legacy fee suggestion: %+v", legacy)
	}
}
