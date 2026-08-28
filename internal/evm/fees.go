package evm

import (
	"context"
	"errors"
	"math/big"
)

type FeeModel string

const (
	FeeLegacy  FeeModel = "legacy"
	FeeDynamic FeeModel = "eip1559"
)

type FeeSuggestion struct {
	Model     FeeModel
	GasPrice  *big.Int
	GasFeeCap *big.Int
	GasTipCap *big.Int
	BaseFee   *big.Int
}

type FeeOracle struct{}

func NewFeeOracle() *FeeOracle { return &FeeOracle{} }

func (oracle *FeeOracle) Suggest(ctx context.Context, rpc RPC, header BlockHeader) (FeeSuggestion, error) {
	if rpc == nil {
		return FeeSuggestion{}, &EngineError{Code: ErrorProviderUnavailable, Field: "fee RPC"}
	}
	if header.BaseFeePerGas == nil {
		gasPrice, err := rpc.SuggestGasPrice(ctx)
		if err != nil {
			return FeeSuggestion{}, &EngineError{Code: ErrorProviderUnavailable, Field: "legacy gas price", Cause: err}
		}
		if gasPrice == nil || gasPrice.Sign() <= 0 || gasPrice.BitLen() > 256 {
			return FeeSuggestion{}, &EngineError{Code: ErrorProviderUnavailable, Field: "legacy gas price"}
		}
		return FeeSuggestion{Model: FeeLegacy, GasPrice: new(big.Int).Set(gasPrice)}, nil
	}
	if header.BaseFeePerGas.Sign() < 0 || header.BaseFeePerGas.BitLen() > 256 {
		return FeeSuggestion{}, &EngineError{Code: ErrorProviderUnavailable, Field: "base fee"}
	}
	tip, err := rpc.SuggestGasTipCap(ctx)
	if err != nil {
		gasPrice, fallbackErr := rpc.SuggestGasPrice(ctx)
		if fallbackErr != nil || gasPrice == nil || gasPrice.Sign() <= 0 || gasPrice.BitLen() > 256 {
			return FeeSuggestion{}, &EngineError{Code: ErrorProviderUnavailable, Field: "priority fee and legacy fallback", Cause: errors.Join(err, fallbackErr)}
		}
		return FeeSuggestion{Model: FeeLegacy, GasPrice: new(big.Int).Set(gasPrice)}, nil
	}
	if tip == nil || tip.Sign() <= 0 || tip.BitLen() > 256 {
		return FeeSuggestion{}, &EngineError{Code: ErrorProviderUnavailable, Field: "priority fee"}
	}
	feeCap := new(big.Int).Mul(header.BaseFeePerGas, big.NewInt(2))
	feeCap.Add(feeCap, tip)
	if feeCap.BitLen() > 256 {
		return FeeSuggestion{}, &EngineError{Code: ErrorProviderUnavailable, Field: "max fee"}
	}
	return FeeSuggestion{
		Model:     FeeDynamic,
		GasFeeCap: feeCap,
		GasTipCap: new(big.Int).Set(tip),
		BaseFee:   new(big.Int).Set(header.BaseFeePerGas),
	}, nil
}
