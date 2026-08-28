package evm

import (
	"context"
	"fmt"
	"math/big"
	"unicode/utf8"

	"github.com/ethereum/go-ethereum/common"
)

type TokenMetadataResolver struct{}

func NewTokenMetadataResolver() *TokenMetadataResolver { return &TokenMetadataResolver{} }

func (resolver *TokenMetadataResolver) Resolve(ctx context.Context, rpc RPC, sender, contract common.Address, block BlockIdentity) (TokenMetadata, error) {
	if rpc == nil || sender == (common.Address{}) || contract == (common.Address{}) || block.Number == 0 || block.Hash == (common.Hash{}) {
		return TokenMetadata{}, &EngineError{Code: ErrorInvalidIntent, Field: "token metadata request"}
	}
	code, err := rpc.CodeAt(ctx, contract, block)
	if err != nil {
		return TokenMetadata{}, &EngineError{Code: ErrorProviderUnavailable, Field: "token bytecode", Cause: err}
	}
	if len(code) == 0 {
		return TokenMetadata{}, &EngineError{Code: ErrorPolicyDenied, Field: "token bytecode"}
	}
	nameData, err := callTokenMetadata(ctx, rpc, sender, contract, block, common.FromHex("0x06fdde03"))
	if err != nil {
		return TokenMetadata{}, err
	}
	symbolData, err := callTokenMetadata(ctx, rpc, sender, contract, block, common.FromHex("0x95d89b41"))
	if err != nil {
		return TokenMetadata{}, err
	}
	decimalsData, err := callTokenMetadata(ctx, rpc, sender, contract, block, common.FromHex("0x313ce567"))
	if err != nil {
		return TokenMetadata{}, err
	}
	name, err := decodeABIString(nameData, 64)
	if err != nil || !validMetadataText(name, 64) {
		return TokenMetadata{}, &EngineError{Code: ErrorPolicyDenied, Field: "token name", Cause: err}
	}
	symbol, err := decodeABIString(symbolData, 16)
	if err != nil || !validMetadataText(symbol, 16) {
		return TokenMetadata{}, &EngineError{Code: ErrorPolicyDenied, Field: "token symbol", Cause: err}
	}
	decimals, err := decodeABIDecimals(decimalsData)
	if err != nil {
		return TokenMetadata{}, &EngineError{Code: ErrorPolicyDenied, Field: "token decimals", Cause: err}
	}
	return TokenMetadata{Name: name, Symbol: symbol, Decimals: decimals, BlockNumber: block.Number}, nil
}

func callTokenMetadata(ctx context.Context, rpc RPC, sender, contract common.Address, block BlockIdentity, selector []byte) ([]byte, error) {
	result, err := rpc.CallContract(ctx, TransactionCall{
		From: sender, To: contract, Value: new(big.Int), Input: append([]byte(nil), selector...),
	}, block)
	if err != nil {
		return nil, &EngineError{Code: ErrorProviderUnavailable, Field: "token metadata call", Cause: err}
	}
	if len(result) == 0 || len(result) > 4<<10 {
		return nil, &EngineError{Code: ErrorPolicyDenied, Field: "token metadata response"}
	}
	return result, nil
}

func decodeABIString(data []byte, maxRunes int) (string, error) {
	if len(data) < 64 || len(data) > 4<<10 {
		return "", fmt.Errorf("invalid ABI string length")
	}
	offsetValue := new(big.Int).SetBytes(data[:32])
	if !offsetValue.IsUint64() {
		return "", fmt.Errorf("invalid ABI string offset")
	}
	offset := offsetValue.Uint64()
	if offset != 32 || offset > uint64(len(data)-32) {
		return "", fmt.Errorf("invalid ABI string offset")
	}
	lengthValue := new(big.Int).SetBytes(data[offset : offset+32])
	if !lengthValue.IsUint64() {
		return "", fmt.Errorf("invalid ABI string size")
	}
	length := lengthValue.Uint64()
	start := offset + 32
	if length == 0 || start > uint64(len(data)) || length > uint64(len(data))-start {
		return "", fmt.Errorf("invalid ABI string payload")
	}
	end := start + length
	paddedEnd := start + ((length + 31) / 32 * 32)
	if paddedEnd != uint64(len(data)) {
		return "", fmt.Errorf("invalid ABI string padding length")
	}
	for _, paddingByte := range data[end:paddedEnd] {
		if paddingByte != 0 {
			return "", fmt.Errorf("invalid ABI string padding")
		}
	}
	payload := data[start:end]
	if !utf8.Valid(payload) {
		return "", fmt.Errorf("ABI string is not valid UTF-8")
	}
	value := string(payload)
	if len([]rune(value)) > maxRunes {
		return "", fmt.Errorf("ABI string exceeds policy")
	}
	return value, nil
}

func decodeABIDecimals(data []byte) (uint8, error) {
	if len(data) != 32 {
		return 0, fmt.Errorf("invalid ABI decimals length")
	}
	value := new(big.Int).SetBytes(data)
	if !value.IsUint64() || value.Uint64() > 36 {
		return 0, fmt.Errorf("ABI decimals outside policy")
	}
	return uint8(value.Uint64()), nil
}
