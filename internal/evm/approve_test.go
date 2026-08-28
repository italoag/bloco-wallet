package evm_test

import (
	"math/big"
	"testing"

	"blocowallet/internal/evm"

	"github.com/ethereum/go-ethereum/common"
)

func TestPlannerBuildsFiniteERC20ApproveAndBlocksUnlimited(t *testing.T) {
	accountID := "8b9b0587-388e-4fca-bba4-bf544ebe53ca"
	from := common.HexToAddress("0x1111111111111111111111111111111111111111")
	contract := common.HexToAddress("0x3333333333333333333333333333333333333333")
	spender := common.HexToAddress("0x2222222222222222222222222222222222222222")
	unlimited := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
	if _, err := evm.NewERC20ApproveIntent(accountID, 1, from, contract, spender, unlimited); !evm.IsErrorCode(err, evm.ErrorPolicyDenied) {
		t.Fatalf("unlimited approval was not blocked: %v", err)
	}
	intent, err := evm.NewERC20ApproveIntent(accountID, 1, from, contract, spender, big.NewInt(1_500_000))
	if err != nil {
		t.Fatal(err)
	}
	plan, err := evm.NewPlanner().PlanERC20Approve(intent, evm.ERC20PlanInput{
		NativePlanInput: evm.NativePlanInput{
			ProviderBinding: evm.ProviderBinding{1}, Nonce: 3, GasLimit: 50_000,
			LegacyGasPrice: big.NewInt(1), SimulationBlockNumber: 100,
			SimulationBlockHash: common.HexToHash("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
		},
		Metadata: evm.TokenMetadata{Name: "USD Coin", Symbol: "USDC", Decimals: 6, BlockNumber: 100},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := common.FromHex("0x095ea7b30000000000000000000000002222222222222222222222222222222222222222000000000000000000000000000000000000000000000000000000000016e360")
	if string(plan.Transaction().Data()) != string(want) || plan.Operation() != evm.OperationERC20Approve {
		t.Fatalf("unexpected approve plan: operation=%s data=%x", plan.Operation(), plan.Transaction().Data())
	}
	if _, err := evm.NewERC20ApproveIntent(accountID, 1, from, contract, spender, new(big.Int)); err != nil {
		t.Fatalf("zero-value revoke approval was rejected: %v", err)
	}
}
