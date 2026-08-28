package evm_test

import (
	"errors"
	"math/big"
	"strings"
	"testing"

	"blocowallet/internal/evm"

	"github.com/ethereum/go-ethereum/common"
)

func TestTransferIntentsValidateAndCopyInputs(t *testing.T) {
	accountID := "8b9b0587-388e-4fca-bba4-bf544ebe53ca"
	from := common.HexToAddress("0x1111111111111111111111111111111111111111")
	to := common.HexToAddress("0x2222222222222222222222222222222222222222")
	contract := common.HexToAddress("0x3333333333333333333333333333333333333333")

	invalidNative := []struct {
		name      string
		accountID string
		chainID   uint64
		from      common.Address
		to        common.Address
		amount    *big.Int
	}{
		{"account", "invalid", 1, from, to, big.NewInt(1)},
		{"chain", accountID, 0, from, to, big.NewInt(1)},
		{"sender", accountID, 1, common.Address{}, to, big.NewInt(1)},
		{"recipient", accountID, 1, from, common.Address{}, big.NewInt(1)},
		{"nil amount", accountID, 1, from, to, nil},
		{"zero amount", accountID, 1, from, to, new(big.Int)},
		{"negative amount", accountID, 1, from, to, big.NewInt(-1)},
		{"oversized amount", accountID, 1, from, to, new(big.Int).Lsh(big.NewInt(1), 256)},
	}
	for _, test := range invalidNative {
		t.Run("native "+test.name, func(t *testing.T) {
			_, err := evm.NewNativeTransferIntent(test.accountID, test.chainID, test.from, test.to, test.amount)
			assertInvalidIntent(t, err)
		})
	}

	invalidERC20 := []struct {
		name     string
		chainID  uint64
		from     common.Address
		contract common.Address
		to       common.Address
		amount   *big.Int
	}{
		{"chain", 0, from, contract, to, big.NewInt(1)},
		{"sender", 1, common.Address{}, contract, to, big.NewInt(1)},
		{"contract", 1, from, common.Address{}, to, big.NewInt(1)},
		{"recipient", 1, from, contract, common.Address{}, big.NewInt(1)},
		{"amount", 1, from, contract, to, nil},
	}
	for _, test := range invalidERC20 {
		t.Run("erc20 "+test.name, func(t *testing.T) {
			_, err := evm.NewERC20TransferIntent(accountID, test.chainID, test.from, test.contract, test.to, test.amount)
			assertInvalidIntent(t, err)
		})
	}
	if _, err := evm.NewERC20TransferIntent("invalid", 1, from, contract, to, big.NewInt(1)); err == nil {
		t.Fatal("invalid ERC-20 account ID was accepted")
	}

	amount := big.NewInt(42)
	intent, err := evm.NewNativeTransferIntent(accountID, 1, from, to, amount)
	if err != nil {
		t.Fatal(err)
	}
	amount.SetInt64(7)
	copyAmount := intent.Amount()
	copyAmount.SetInt64(8)
	if intent.AccountID() != accountID || intent.ChainID() != 1 || intent.From() != from || intent.To() != to || intent.Amount().Cmp(big.NewInt(42)) != 0 {
		t.Fatal("native intent did not preserve immutable values")
	}
}

func TestEngineErrorDoesNotExposeCause(t *testing.T) {
	secret := errors.New("provider reflected super-secret-token")
	err := &evm.EngineError{Code: evm.ErrorProviderUnavailable, Cause: secret}
	if fieldError := (&evm.EngineError{Code: evm.ErrorInvalidIntent, Field: "amount"}).Error(); !strings.Contains(fieldError, "amount") {
		t.Fatalf("field error omitted stable context: %s", fieldError)
	}
	if strings.Contains(err.Error(), "super-secret-token") || !errors.Is(err, secret) {
		t.Fatalf("engine error contract failed: %v", err)
	}
	var nilError *evm.EngineError
	if nilError.Error() == "" || nilError.Unwrap() != nil {
		t.Fatal("nil engine error contract failed")
	}
}

func assertInvalidIntent(t *testing.T, err error) {
	t.Helper()
	var engineError *evm.EngineError
	if !errors.As(err, &engineError) || engineError.Code != evm.ErrorInvalidIntent || engineError.Field == "" {
		t.Fatalf("expected typed invalid intent error, got %T: %v", err, err)
	}
}
