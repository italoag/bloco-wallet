package signer

import (
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/signer/core/apitypes"
)

// SafeMessage types per the Safe EIP-712 scheme:
//
//	domain: {verifyingContract: safe, chainId}
//	primary type: SafeMessage(bytes message)
var safeMessageTypes = apitypes.Types{
	"EIP712Domain": {
		{Name: "verifyingContract", Type: "address"},
		{Name: "chainId", Type: "uint256"},
	},
	"SafeMessage": {
		{Name: "message", Type: "bytes"},
	},
}

// SafeMessageDigest computes the EIP-712 digest a Safe owner signs to
// approve a transaction hash (EIP-1271 over the SafeMessage scheme).
func SafeMessageDigest(safe common.Address, chainID uint64, messageHash [32]byte) ([32]byte, error) {
	if safe == (common.Address{}) || chainID == 0 {
		return [32]byte{}, fmt.Errorf("safe signer: invalid binding")
	}
	typedData := apitypes.TypedData{
		Types:       safeMessageTypes,
		PrimaryType: "SafeMessage",
		Domain: apitypes.TypedDataDomain{
			VerifyingContract: safe.Hex(),
			ChainId:           math.NewHexOrDecimal256(int64(chainID)),
		},
		Message: apitypes.TypedDataMessage{
			"message": messageHash[:],
		},
	}
	digest, _, err := apitypes.TypedDataAndHash(typedData)
	if err != nil {
		return [32]byte{}, fmt.Errorf("safe signer: typed data: %w", err)
	}
	var result [32]byte
	copy(result[:], digest)
	return result, nil
}

// ComposeSafeSignature builds the EIP-1271 signature payload: the 65-byte
// owner signature (v normalized to 0/1), the contract signature type byte
// 0x01, and the owner address.
func ComposeSafeSignature(signature [65]byte, owner common.Address) []byte {
	normalized := signature
	switch normalized[64] {
	case 27:
		normalized[64] = 0
	case 28:
		normalized[64] = 1
	}
	composed := make([]byte, 0, 86)
	composed = append(composed, normalized[:]...)
	composed = append(composed, 0x01)
	composed = append(composed, owner.Bytes()...)
	return composed
}

// EncodeExecTransaction builds the Safe execTransaction calldata for a
// single owner signature payload.
func EncodeExecTransaction(to common.Address, value *big.Int, data []byte, signatures []byte) ([]byte, error) {
	if to == (common.Address{}) || value == nil || value.Sign() < 0 {
		return nil, fmt.Errorf("safe signer: invalid transaction")
	}
	if len(data) > 512<<10 || len(signatures) > 4<<10 {
		return nil, fmt.Errorf("safe signer: payload bounds")
	}
	method := abi.NewMethod(
		"execTransaction",
		"execTransaction",
		abi.Function, "nonpayable", false, false,
		abi.Arguments{
			{Name: "to", Type: mustABIType("address")},
			{Name: "value", Type: mustABIType("uint256")},
			{Name: "data", Type: mustABIType("bytes")},
			{Name: "operation", Type: mustABIType("uint8")},
			{Name: "safeTxGas", Type: mustABIType("uint256")},
			{Name: "baseGas", Type: mustABIType("uint256")},
			{Name: "gasPrice", Type: mustABIType("uint256")},
			{Name: "gasToken", Type: mustABIType("address")},
			{Name: "refundReceiver", Type: mustABIType("address")},
			{Name: "signatures", Type: mustABIType("bytes")},
		},
		abi.Arguments{},
	)
	packed, err := method.Inputs.Pack(to, value, data, uint8(0), big.NewInt(0), big.NewInt(0), big.NewInt(0), common.Address{}, common.Address{}, signatures)
	if err != nil {
		return nil, fmt.Errorf("safe signer: encode: %w", err)
	}
	calldata := make([]byte, 0, 4+len(packed))
	calldata = append(calldata, method.ID...)
	calldata = append(calldata, packed...)
	return calldata, nil
}

func mustABIType(spec string) abi.Type {
	argumentType, err := abi.NewType(spec, "", nil)
	if err != nil {
		panic(fmt.Sprintf("safe signer: abi type %s: %v", spec, err))
	}
	return argumentType
}
