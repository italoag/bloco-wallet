package signer

import (
	"encoding/json"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

var (
	// Safe-specific EIP-712 domain: Safe intentionally orders chainId before
	// verifyingContract (Safe.sol DOMAIN_SEPARATOR_TYPEHASH).
	safeDomainTypeHash = common.HexToHash("0x47e79534a245952e8b16893a336b85a3d9ea9fa8c573f3d803afb92a79469218")
	// keccak256("SafeMessage(bytes message)").
	safeMessageTypeHash = common.HexToHash("0x60b3cbf8b4a223d68d641b3b6ddf9a298e7f33710cf3d3a9d1146b5a6150fbca")
	// keccak256("SafeTx(address to,uint256 value,bytes data,uint8 operation,uint256 safeTxGas,uint256 baseGas,uint256 gasPrice,address gasToken,address refundReceiver,uint256 nonce)").
	safeTransactionTypeHash = crypto.Keccak256Hash([]byte("SafeTx(address to,uint256 value,bytes data,uint8 operation,uint256 safeTxGas,uint256 baseGas,uint256 gasPrice,address gasToken,address refundReceiver,uint256 nonce)"))
)

// SafeMessageDigest computes the EIP-712 digest the Safe compatibility
// fallback handler validates for EIP-1271:
// keccak256(0x1901 || domainSeparator || SafeMessage(dataHash)).
func SafeMessageDigest(safe common.Address, chainID uint64, messageHash [32]byte) ([32]byte, error) {
	if safe == (common.Address{}) || chainID == 0 {
		return [32]byte{}, fmt.Errorf("safe signer: invalid binding")
	}
	domainSeparator := safeDomainSeparator(safe, chainID)
	messageData := make([]byte, 0, 64)
	messageData = append(messageData, safeMessageTypeHash[:]...)
	messageData = append(messageData, crypto.Keccak256(messageHash[:])...)
	return safeTypedDigest(domainSeparator, crypto.Keccak256(messageData)), nil
}

// SafeTransaction contains every field signed by Safe.execTransaction.
type SafeTransaction struct {
	To             common.Address
	Value          *big.Int
	Data           []byte
	Operation      uint8
	SafeTxGas      *big.Int
	BaseGas        *big.Int
	GasPrice       *big.Int
	GasToken       common.Address
	RefundReceiver common.Address
	Nonce          *big.Int
}

// SafeTransactionDigest computes the exact digest returned by
// Safe.getTransactionHash.
func SafeTransactionDigest(safe common.Address, chainID uint64, transaction SafeTransaction) ([32]byte, error) {
	if safe == (common.Address{}) || chainID == 0 {
		return [32]byte{}, fmt.Errorf("safe signer: invalid transaction binding")
	}
	if err := validateSafeTransaction(transaction); err != nil {
		return [32]byte{}, err
	}
	structData := make([]byte, 0, 11*32)
	structData = append(structData, safeTransactionTypeHash[:]...)
	structData = append(structData, safeAddressWord(transaction.To)...)
	structData = append(structData, safeUint256Word(transaction.Value)...)
	structData = append(structData, crypto.Keccak256(transaction.Data)...)
	structData = append(structData, safeUint256Word(new(big.Int).SetUint64(uint64(transaction.Operation)))...)
	structData = append(structData, safeUint256Word(transaction.SafeTxGas)...)
	structData = append(structData, safeUint256Word(transaction.BaseGas)...)
	structData = append(structData, safeUint256Word(transaction.GasPrice)...)
	structData = append(structData, safeAddressWord(transaction.GasToken)...)
	structData = append(structData, safeAddressWord(transaction.RefundReceiver)...)
	structData = append(structData, safeUint256Word(transaction.Nonce)...)
	return safeTypedDigest(safeDomainSeparator(safe, chainID), crypto.Keccak256(structData)), nil
}

// SafeTransactionEIP712 exposes the EIP-712 components hardware wallets
// need to produce a valid Safe owner signature: the canonical typed-data
// JSON (Trezor Core), the domain separator, the message hash (Ledger V0),
// and the final digest the Safe contract recovers.
type SafeTransactionEIP712 struct {
	CanonicalJSON       []byte
	DomainSeparatorHash [32]byte
	MessageHash         [32]byte
	Digest              [32]byte
}

// EncodeSafeTransactionEIP712 builds the EIP-712 payload for one Safe
// transaction. The digest equals SafeTransactionDigest, so a device
// signature over the EIP-712 hash is accepted by Safe.checkNSignatures.
func EncodeSafeTransactionEIP712(safe common.Address, chainID uint64, transaction SafeTransaction) (*SafeTransactionEIP712, error) {
	if safe == (common.Address{}) || chainID == 0 {
		return nil, fmt.Errorf("safe signer: invalid transaction binding")
	}
	if err := validateSafeTransaction(transaction); err != nil {
		return nil, err
	}
	domainSeparator := safeDomainSeparator(safe, chainID)
	structData := make([]byte, 0, 11*32)
	structData = append(structData, safeTransactionTypeHash[:]...)
	structData = append(structData, safeAddressWord(transaction.To)...)
	structData = append(structData, safeUint256Word(transaction.Value)...)
	structData = append(structData, crypto.Keccak256(transaction.Data)...)
	structData = append(structData, safeUint256Word(new(big.Int).SetUint64(uint64(transaction.Operation)))...)
	structData = append(structData, safeUint256Word(transaction.SafeTxGas)...)
	structData = append(structData, safeUint256Word(transaction.BaseGas)...)
	structData = append(structData, safeUint256Word(transaction.GasPrice)...)
	structData = append(structData, safeAddressWord(transaction.GasToken)...)
	structData = append(structData, safeAddressWord(transaction.RefundReceiver)...)
	structData = append(structData, safeUint256Word(transaction.Nonce)...)
	messageHash := crypto.Keccak256(structData)
	digest := safeTypedDigest(domainSeparator, messageHash)
	canonical, err := encodeSafeTransactionTypedDataJSON(safe, chainID, transaction)
	if err != nil {
		return nil, err
	}
	var domainHash, messageHashArray, digestArray [32]byte
	copy(domainHash[:], domainSeparator)
	copy(messageHashArray[:], messageHash)
	copy(digestArray[:], digest[:])
	return &SafeTransactionEIP712{
		CanonicalJSON: canonical, DomainSeparatorHash: domainHash,
		MessageHash: messageHashArray, Digest: digestArray,
	}, nil
}

func encodeSafeTransactionTypedDataJSON(safe common.Address, chainID uint64, transaction SafeTransaction) ([]byte, error) {
	type typedData struct {
		Types       map[string][]map[string]string `json:"types"`
		PrimaryType string                         `json:"primaryType"`
		Domain      map[string]any                 `json:"domain"`
		Message     map[string]any                 `json:"message"`
	}
	payload := typedData{
		Types: map[string][]map[string]string{
			"EIP712Domain": {
				{"name": "chainId", "type": "uint256"},
				{"name": "verifyingContract", "type": "address"},
			},
			"SafeTx": {
				{"name": "to", "type": "address"},
				{"name": "value", "type": "uint256"},
				{"name": "data", "type": "bytes"},
				{"name": "operation", "type": "uint8"},
				{"name": "safeTxGas", "type": "uint256"},
				{"name": "baseGas", "type": "uint256"},
				{"name": "gasPrice", "type": "uint256"},
				{"name": "gasToken", "type": "address"},
				{"name": "refundReceiver", "type": "address"},
				{"name": "nonce", "type": "uint256"},
			},
		},
		PrimaryType: "SafeTx",
		Domain: map[string]any{
			"chainId":           new(big.Int).SetUint64(chainID),
			"verifyingContract": safe.Hex(),
		},
		Message: map[string]any{
			"to":             transaction.To.Hex(),
			"value":          new(big.Int).Set(transaction.Value),
			"data":           common.Bytes2Hex(transaction.Data),
			"operation":      uint8(transaction.Operation),
			"safeTxGas":      new(big.Int).Set(transaction.SafeTxGas),
			"baseGas":        new(big.Int).Set(transaction.BaseGas),
			"gasPrice":       new(big.Int).Set(transaction.GasPrice),
			"gasToken":       transaction.GasToken.Hex(),
			"refundReceiver": transaction.RefundReceiver.Hex(),
			"nonce":          new(big.Int).Set(transaction.Nonce),
		},
	}
	return json.Marshal(payload)
}

func validateSafeTransaction(transaction SafeTransaction) error {
	if transaction.To == (common.Address{}) || transaction.Operation > 1 || len(transaction.Data) > 512<<10 {
		return fmt.Errorf("safe signer: invalid transaction fields")
	}
	values := []*big.Int{transaction.Value, transaction.SafeTxGas, transaction.BaseGas, transaction.GasPrice, transaction.Nonce}
	for _, value := range values {
		if value == nil || value.Sign() < 0 || value.BitLen() > 256 {
			return fmt.Errorf("safe signer: invalid uint256 transaction field")
		}
	}
	return nil
}

func safeDomainSeparator(safe common.Address, chainID uint64) []byte {
	domainData := make([]byte, 0, 96)
	domainData = append(domainData, safeDomainTypeHash[:]...)
	domainData = append(domainData, safeUint256Word(new(big.Int).SetUint64(chainID))...)
	domainData = append(domainData, safeAddressWord(safe)...)
	return crypto.Keccak256(domainData)
}

func safeTypedDigest(domainSeparator, structHash []byte) [32]byte {
	encoded := make([]byte, 0, 66)
	encoded = append(encoded, 0x19, 0x01)
	encoded = append(encoded, domainSeparator...)
	encoded = append(encoded, structHash...)
	var result [32]byte
	copy(result[:], crypto.Keccak256(encoded))
	return result
}

func safeAddressWord(address common.Address) []byte {
	word := make([]byte, 32)
	copy(word[12:], address.Bytes())
	return word
}

func safeUint256Word(value *big.Int) []byte {
	word := make([]byte, 32)
	value.FillBytes(word)
	return word
}

// ComposeSafeContractSignature builds the single-owner contract-signature
// encoding consumed by Safe.checkNSignatures: r=owner, s=dynamic offset,
// v=0, followed by uint256 length and padded EIP-1271 signature bytes.
func ComposeSafeContractSignature(owner common.Address, signature []byte) ([]byte, error) {
	if owner == (common.Address{}) || len(signature) == 0 || len(signature) > 4<<10 {
		return nil, fmt.Errorf("safe signer: invalid contract signature")
	}
	staticPart := make([]byte, 65)
	copy(staticPart[12:32], owner.Bytes())
	copy(staticPart[32:64], safeUint256Word(big.NewInt(65)))
	staticPart[64] = 0
	dynamicLength := 32 + ((len(signature)+31)/32)*32
	composed := make([]byte, 0, len(staticPart)+dynamicLength)
	composed = append(composed, staticPart...)
	composed = append(composed, safeUint256Word(new(big.Int).SetUint64(uint64(len(signature))))...)
	composed = append(composed, signature...)
	composed = append(composed, make([]byte, dynamicLength-32-len(signature))...)
	return composed, nil
}

// EncodeExecTransaction builds zero-refund Safe calldata for compatibility.
func EncodeExecTransaction(to common.Address, value *big.Int, data []byte, signatures []byte) ([]byte, error) {
	return EncodeSafeExecTransaction(SafeTransaction{
		To: to, Value: value, Data: data,
		SafeTxGas: big.NewInt(0), BaseGas: big.NewInt(0), GasPrice: big.NewInt(0), Nonce: big.NewInt(0),
	}, signatures)
}

// EncodeSafeExecTransaction builds calldata for the complete Safe transaction.
func EncodeSafeExecTransaction(transaction SafeTransaction, signatures []byte) ([]byte, error) {
	if err := validateSafeTransaction(transaction); err != nil {
		return nil, err
	}
	if len(signatures) == 0 || len(signatures) > 4<<10 {
		return nil, fmt.Errorf("safe signer: signature bounds")
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
	packed, err := method.Inputs.Pack(
		transaction.To, transaction.Value, transaction.Data, transaction.Operation,
		transaction.SafeTxGas, transaction.BaseGas, transaction.GasPrice,
		transaction.GasToken, transaction.RefundReceiver, signatures,
	)
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
