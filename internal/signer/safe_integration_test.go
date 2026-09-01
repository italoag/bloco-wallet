package signer

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

const (
	safeMagicValue          = "0x1626ba7e"
	safeCreationCodeHash    = "0x3c32e7eb3961c145eef8620b24e8b829e6d2062a5730629d6169319f29fc1d07"
	factoryCreationCodeHash = "0xd8803696e4c627ba4cab33cfab2625cf66c88f218c017306e392fcf88cc4e0c6"
	handlerCreationCodeHash = "0xe7213ad39e55911aa31c9d631addd737170d46e9074db5a5cf60fc3f78d6e1c1"
)

// anvilRPC is a minimal JSON-RPC client for the integration test.
type anvilRPC struct {
	url    string
	client *http.Client
}

func newAnvilRPC(url string) *anvilRPC {
	return &anvilRPC{url: url, client: &http.Client{Timeout: 30 * time.Second}}
}

func (rpc *anvilRPC) call(ctx context.Context, method string, params ...any) (json.RawMessage, error) {
	payload, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": method, "params": params,
	})
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, rpc.url, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := rpc.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	var decoded struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		return nil, err
	}
	if decoded.Error != nil {
		return nil, fmt.Errorf("rpc %s: %s", method, decoded.Error.Message)
	}
	return decoded.Result, nil
}

func (rpc *anvilRPC) blockNumber(ctx context.Context) (uint64, error) {
	result, err := rpc.call(ctx, "eth_blockNumber")
	if err != nil {
		return 0, err
	}
	var value string
	if err := json.Unmarshal(result, &value); err != nil {
		return 0, err
	}
	return new(big.Int).SetBytes(common.FromHex(value)).Uint64(), nil
}

func (rpc *anvilRPC) sendTransaction(ctx context.Context, from common.Address, to *common.Address, data []byte) (common.Hash, error) {
	params := map[string]any{
		"from": from.Hex(), "data": "0x" + hex.EncodeToString(data),
	}
	if to != nil {
		params["to"] = to.Hex()
	}
	result, err := rpc.call(ctx, "eth_sendTransaction", params)
	if err != nil {
		return common.Hash{}, err
	}
	var hash string
	if err := json.Unmarshal(result, &hash); err != nil {
		return common.Hash{}, err
	}
	return common.HexToHash(hash), nil
}

func (rpc *anvilRPC) sendTransactionWithValue(ctx context.Context, from, to common.Address, value *big.Int, data []byte) (common.Hash, error) {
	params := map[string]any{
		"from": from.Hex(), "to": to.Hex(), "value": "0x" + value.Text(16),
		"data": "0x" + hex.EncodeToString(data),
	}
	result, err := rpc.call(ctx, "eth_sendTransaction", params)
	if err != nil {
		return common.Hash{}, err
	}
	var hash string
	if err := json.Unmarshal(result, &hash); err != nil {
		return common.Hash{}, err
	}
	return common.HexToHash(hash), nil
}

func (rpc *anvilRPC) balance(ctx context.Context, address common.Address) (*big.Int, error) {
	result, err := rpc.call(ctx, "eth_getBalance", address.Hex(), "latest")
	if err != nil {
		return nil, err
	}
	var value string
	if err := json.Unmarshal(result, &value); err != nil {
		return nil, err
	}
	balance, ok := new(big.Int).SetString(strings.TrimPrefix(value, "0x"), 16)
	if !ok {
		return nil, fmt.Errorf("invalid balance %q", value)
	}
	return balance, nil
}

func (rpc *anvilRPC) waitReceipt(ctx context.Context, hash common.Hash) (common.Address, error) {
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		result, err := rpc.call(ctx, "eth_getTransactionReceipt", hash.Hex())
		if err != nil {
			return common.Address{}, err
		}
		var receipt struct {
			ContractAddress string `json:"contractAddress"`
			Status          string `json:"status"`
		}
		if err := json.Unmarshal(result, &receipt); err == nil && receipt.Status != "" {
			if receipt.Status == "0x0" {
				return common.Address{}, fmt.Errorf("transaction reverted")
			}
			return common.HexToAddress(receipt.ContractAddress), nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return common.Address{}, fmt.Errorf("receipt timeout")
}

func (rpc *anvilRPC) callContract(ctx context.Context, to common.Address, data []byte) ([]byte, error) {
	result, err := rpc.call(ctx, "eth_call", map[string]any{
		"to": to.Hex(), "data": "0x" + hex.EncodeToString(data),
	}, "latest")
	if err != nil {
		return nil, err
	}
	var value string
	if err := json.Unmarshal(result, &value); err != nil {
		return nil, err
	}
	return common.FromHex(value), nil
}

func TestSafeOfficialArtifactHashes(t *testing.T) {
	artifacts := []struct {
		name     string
		bytecode string
		expected string
	}{
		{name: "Safe", bytecode: safeBytecode, expected: safeCreationCodeHash},
		{name: "SafeProxyFactory", bytecode: safeProxyFactoryBytecode, expected: factoryCreationCodeHash},
		{name: "CompatibilityFallbackHandler", bytecode: compatibilityFallbackHandlerBytecode, expected: handlerCreationCodeHash},
	}
	for _, artifact := range artifacts {
		t.Run(artifact.name, func(t *testing.T) {
			actual := crypto.Keccak256Hash(common.FromHex(artifact.bytecode)).Hex()
			if actual != artifact.expected {
				t.Fatalf("creation code hash changed: %s", actual)
			}
		})
	}
}

// TestSafeEIP1271AgainstRealContract deploys the official Safe v1.5.0
// contracts (Safe singleton, proxy factory, CompatibilityFallbackHandler)
// on an Anvil node and verifies EIP-1271 isValidSignature with the owner's
// SafeMessage signature. Skipped unless BLOCO_WALLET_ANVIL_URL is set.
func TestSafeEIP1271AgainstRealContract(t *testing.T) {
	anvilURL := os.Getenv("BLOCO_WALLET_ANVIL_URL")
	if anvilURL == "" {
		t.Skip("BLOCO_WALLET_ANVIL_URL not set; skipping Safe contract integration")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	rpc := newAnvilRPC(anvilURL)
	if _, err := rpc.blockNumber(ctx); err != nil {
		t.Fatalf("anvil unreachable: %v", err)
	}
	accountsResult, err := rpc.call(ctx, "eth_accounts")
	if err != nil {
		t.Fatal(err)
	}
	var accounts []string
	if err := json.Unmarshal(accountsResult, &accounts); err != nil || len(accounts) == 0 {
		t.Fatalf("anvil has no unlocked accounts: %v", err)
	}
	deployer := common.HexToAddress(accounts[0])

	// Deploy the Safe v1.5.0 singleton, proxy factory, and EIP-1271 handler.
	singletonTx, err := rpc.sendTransaction(ctx, deployer, nil, common.FromHex(safeBytecode))
	if err != nil {
		t.Fatal(err)
	}
	singletonAddress, err := rpc.waitReceipt(ctx, singletonTx)
	if err != nil {
		t.Fatal(err)
	}
	versionResult, err := rpc.callContract(ctx, singletonAddress, selectorOf("VERSION()"))
	if err != nil {
		t.Fatal(err)
	}
	version, err := decodeBytesResult(versionResult)
	if err != nil {
		t.Fatal(err)
	}
	if string(version) != "1.5.0" {
		t.Fatalf("unexpected Safe version %q", version)
	}
	factoryTx, err := rpc.sendTransaction(ctx, deployer, nil, common.FromHex(safeProxyFactoryBytecode))
	if err != nil {
		t.Fatal(err)
	}
	factoryAddress, err := rpc.waitReceipt(ctx, factoryTx)
	if err != nil {
		t.Fatal(err)
	}
	handlerTx, err := rpc.sendTransaction(ctx, deployer, nil, common.FromHex(compatibilityFallbackHandlerBytecode))
	if err != nil {
		t.Fatal(err)
	}
	handlerAddress, err := rpc.waitReceipt(ctx, handlerTx)
	if err != nil {
		t.Fatal(err)
	}

	// Create a Safe with one owner (threshold 1) and the EIP-1271 handler.
	ownerKey, err := ecdsa.GenerateKey(crypto.S256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	owner := crypto.PubkeyToAddress(ownerKey.PublicKey)
	setupData, err := encodeSafeSetup([]common.Address{owner}, handlerAddress, nil)
	if err != nil {
		t.Fatal(err)
	}
	proxyTx, err := rpc.sendTransaction(ctx, deployer, &factoryAddress,
		encodeCreateProxyWithNonce(singletonAddress, setupData, 1))
	if err != nil {
		t.Fatal(err)
	}
	creationCodeResult, err := rpc.callContract(ctx, factoryAddress, selectorOf("proxyCreationCode()"))
	if err != nil {
		t.Fatal(err)
	}
	creationCode, err := decodeBytesResult(creationCodeResult)
	if err != nil {
		t.Fatal(err)
	}
	safeAddress := safeProxyAddress(factoryAddress, singletonAddress, creationCode, setupData, big.NewInt(1))
	if _, err := rpc.waitReceipt(ctx, proxyTx); err != nil {
		t.Fatal(err)
	}
	codeResult, err := rpc.call(ctx, "eth_getCode", safeAddress.Hex(), "latest")
	if err != nil {
		t.Fatal(err)
	}
	var deployedCode string
	if err := json.Unmarshal(codeResult, &deployedCode); err != nil || len(deployedCode) < 10 {
		t.Fatalf("proxy has no deployed code: %v %q", err, deployedCode)
	}

	// The data hash is the signed intent; the SafeMessage digest binds it
	// to this Safe and chain through the CompatibilityFallbackHandler.
	dataHash := crypto.Keccak256Hash([]byte("bloco safe onchain vector"))
	messageHash, err := SafeMessageDigest(safeAddress, 31337, dataHash)
	if err != nil {
		t.Fatal(err)
	}
	// Compare against the real contract: getMessageHashForSafe(safe, abi.encode(dataHash)).
	handlerMessageHash, err := rpc.callContract(ctx, handlerAddress,
		encodeGetMessageHashForSafe(safeAddress, dataHash))
	if err != nil {
		t.Fatal(err)
	}
	if common.BytesToHash(handlerMessageHash) != messageHash {
		t.Fatal("SafeMessage digest differs from the real compatibility handler")
	}
	ownerSignature, err := crypto.Sign(messageHash[:], ownerKey)
	if err != nil {
		t.Fatal(err)
	}
	// Safe v1.5.0 reads the v byte directly: 27/28 is ECDSA, 0 is the
	// pre-validated (approved hash) form.
	ownerSignature[64] += 27

	// isValidSignature must return the EIP-1271 magic value.
	magic, err := rpc.callContract(ctx, safeAddress, encodeIsValidSignature(dataHash, ownerSignature))
	if err != nil {
		t.Fatal(err)
	}
	if len(magic) < 4 || !bytes.Equal(magic[:4], common.FromHex(safeMagicValue)) {
		t.Fatalf("isValidSignature returned %x, want %s", magic, safeMagicValue)
	}

	// A signature from a non-owner must be rejected (the Safe reverts, so
	// an RPC error or non-magic result is the expected rejection signal).
	foreignKey, err := ecdsa.GenerateKey(crypto.S256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	foreignSignature, err := crypto.Sign(messageHash[:], foreignKey)
	if err != nil {
		t.Fatal(err)
	}
	foreignSignature[64] += 27
	rejected, err := rpc.callContract(ctx, safeAddress, encodeIsValidSignature(dataHash, foreignSignature))
	if err == nil && len(rejected) >= 4 && bytes.Equal(rejected[:4], common.FromHex(safeMagicValue)) {
		t.Fatal("non-owner signature was accepted by the real Safe")
	}
	// A tampered data hash must be rejected too.
	tampered := dataHash
	tampered[0] ^= 0x01
	tamperedResult, err := rpc.callContract(ctx, safeAddress, encodeIsValidSignature(tampered, ownerSignature))
	if err == nil && len(tamperedResult) >= 4 && bytes.Equal(tamperedResult[:4], common.FromHex(safeMagicValue)) {
		t.Fatal("tampered data hash was accepted by the real Safe")
	}

	// Use the EOA-owned Safe as a contract owner of a second Safe and prove
	// the dynamic contract-signature encoding against the real contracts.
	outerSetup, err := encodeSafeSetup([]common.Address{safeAddress}, handlerAddress, nil)
	if err != nil {
		t.Fatal(err)
	}
	outerProxyTx, err := rpc.sendTransaction(ctx, deployer, &factoryAddress,
		encodeCreateProxyWithNonce(singletonAddress, outerSetup, 2))
	if err != nil {
		t.Fatal(err)
	}
	outerSafe := safeProxyAddress(factoryAddress, singletonAddress, creationCode, outerSetup, big.NewInt(2))
	if _, err := rpc.waitReceipt(ctx, outerProxyTx); err != nil {
		t.Fatal(err)
	}
	outerDataHash := crypto.Keccak256Hash([]byte("bloco nested Safe vector"))
	outerMessageHash, err := SafeMessageDigest(outerSafe, 31337, outerDataHash)
	if err != nil {
		t.Fatal(err)
	}
	innerMessageHash, err := SafeMessageDigest(safeAddress, 31337, outerMessageHash)
	if err != nil {
		t.Fatal(err)
	}
	innerOwnerSignature, err := crypto.Sign(innerMessageHash[:], ownerKey)
	if err != nil {
		t.Fatal(err)
	}
	innerOwnerSignature[64] += 27
	contractOwnerSignature, err := ComposeSafeContractSignature(safeAddress, innerOwnerSignature)
	if err != nil {
		t.Fatal(err)
	}
	nestedMagic, err := rpc.callContract(ctx, outerSafe, encodeIsValidSignature(outerDataHash, contractOwnerSignature))
	if err != nil {
		t.Fatal(err)
	}
	if len(nestedMagic) < 4 || !bytes.Equal(nestedMagic[:4], common.FromHex(safeMagicValue)) {
		t.Fatalf("nested Safe contract signature returned %x", nestedMagic)
	}

	// Fund the Safe, sign a native transfer, execute it, and verify both the
	// recipient balance and Safe nonce changed exactly once.
	funding := new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)
	fundTx, err := rpc.sendTransactionWithValue(ctx, deployer, safeAddress, funding, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rpc.waitReceipt(ctx, fundTx); err != nil {
		t.Fatal(err)
	}
	if len(accounts) < 2 {
		t.Fatal("Anvil did not expose a recipient account")
	}
	recipient := common.HexToAddress(accounts[1])
	balanceBefore, err := rpc.balance(ctx, recipient)
	if err != nil {
		t.Fatal(err)
	}
	nonceBytes, err := rpc.callContract(ctx, safeAddress, selectorOf("nonce()"))
	if err != nil {
		t.Fatal(err)
	}
	if len(nonceBytes) != 32 {
		t.Fatalf("unexpected Safe nonce encoding: %x", nonceBytes)
	}
	nonce := new(big.Int).SetBytes(nonceBytes)
	transferValue := big.NewInt(12345)
	safeTransaction := SafeTransaction{
		To: recipient, Value: transferValue, Operation: 0,
		SafeTxGas: big.NewInt(0), BaseGas: big.NewInt(0), GasPrice: big.NewInt(0),
		Nonce: nonce,
	}
	transactionDigest, err := SafeTransactionDigest(safeAddress, 31337, safeTransaction)
	if err != nil {
		t.Fatal(err)
	}
	contractDigest, err := rpc.callContract(ctx, safeAddress, encodeGetTransactionHash(safeTransaction))
	if err != nil {
		t.Fatal(err)
	}
	if common.BytesToHash(contractDigest) != common.BytesToHash(transactionDigest[:]) {
		t.Fatalf("Safe transaction digest differs: local=%x contract=%x", transactionDigest, contractDigest)
	}
	transactionSignature, err := crypto.Sign(transactionDigest[:], ownerKey)
	if err != nil {
		t.Fatal(err)
	}
	transactionSignature[64] += 27
	execData, err := EncodeSafeExecTransaction(safeTransaction, transactionSignature)
	if err != nil {
		t.Fatal(err)
	}
	execTx, err := rpc.sendTransaction(ctx, deployer, &safeAddress, execData)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rpc.waitReceipt(ctx, execTx); err != nil {
		t.Fatal(err)
	}
	balanceAfter, err := rpc.balance(ctx, recipient)
	if err != nil {
		t.Fatal(err)
	}
	if new(big.Int).Sub(balanceAfter, balanceBefore).Cmp(transferValue) != 0 {
		t.Fatalf("Safe transfer effect mismatch: before=%s after=%s", balanceBefore, balanceAfter)
	}
	nonceAfterBytes, err := rpc.callContract(ctx, safeAddress, selectorOf("nonce()"))
	if err != nil {
		t.Fatal(err)
	}
	nonceAfter := new(big.Int).SetBytes(nonceAfterBytes)
	if nonceAfter.Cmp(new(big.Int).Add(nonce, big.NewInt(1))) != 0 {
		t.Fatalf("Safe nonce did not advance once: %s -> %s", nonce, nonceAfter)
	}
}

// selectorOf computes the 4-byte function selector.
func selectorOf(signature string) []byte {
	return crypto.Keccak256([]byte(signature))[:4]
}

// decodeBytesResult decodes an ABI-encoded bytes return value
// (offset + length + data).
func decodeBytesResult(result []byte) ([]byte, error) {
	if len(result) < 64 {
		return nil, fmt.Errorf("short bytes result")
	}
	offset := new(big.Int).SetBytes(result[0:32]).Uint64()
	if offset != 32 {
		return nil, fmt.Errorf("unexpected bytes offset %d", offset)
	}
	length := new(big.Int).SetBytes(result[32:64]).Uint64()
	if length > uint64(len(result)-64) {
		return nil, fmt.Errorf("bytes length out of range")
	}
	return append([]byte(nil), result[64:64+length]...), nil
}

// create2Address derives an EIP-1014 address.
func create2Address(factory common.Address, salt []byte, initCode []byte) common.Address {
	initCodeHash := crypto.Keccak256(initCode)
	input := make([]byte, 0, 1+20+32+32)
	input = append(input, 0xff)
	input = append(input, factory.Bytes()...)
	input = append(input, salt...)
	input = append(input, initCodeHash...)
	hash := crypto.Keccak256(input)
	return common.BytesToAddress(hash[12:])
}

func safeProxyAddress(factory, singleton common.Address, creationCode, initializer []byte, nonce *big.Int) common.Address {
	initCode := append(append([]byte(nil), creationCode...), make([]byte, 12)...)
	initCode = append(initCode, singleton.Bytes()...)
	saltBinding := make([]byte, 0, 64)
	saltBinding = append(saltBinding, crypto.Keccak256(initializer)...)
	nonceWord := make([]byte, 32)
	nonce.FillBytes(nonceWord)
	saltBinding = append(saltBinding, nonceWord...)
	return create2Address(factory, crypto.Keccak256(saltBinding), initCode)
}

func encodeSafeSetup(owners []common.Address, fallbackHandler common.Address, data []byte) ([]byte, error) {
	method := abi.NewMethod("setup", "setup", abi.Function, "nonpayable", false, false,
		abi.Arguments{
			{Name: "owners", Type: mustABIType("address[]")},
			{Name: "threshold", Type: mustABIType("uint256")},
			{Name: "to", Type: mustABIType("address")},
			{Name: "data", Type: mustABIType("bytes")},
			{Name: "fallbackHandler", Type: mustABIType("address")},
			{Name: "paymentToken", Type: mustABIType("address")},
			{Name: "payment", Type: mustABIType("uint256")},
			{Name: "paymentReceiver", Type: mustABIType("address")},
		},
		abi.Arguments{})
	packed, err := method.Inputs.Pack(owners, big.NewInt(1), common.Address{}, data, fallbackHandler, common.Address{}, big.NewInt(0), common.Address{})
	if err != nil {
		return nil, err
	}
	return append(append([]byte(nil), method.ID...), packed...), nil
}

func encodeCreateProxyWithNonce(singleton common.Address, initializer []byte, nonce uint64) []byte {
	method := abi.NewMethod("createProxyWithNonce", "createProxyWithNonce", abi.Function, "nonpayable", false, false,
		abi.Arguments{
			{Name: "singleton", Type: mustABIType("address")},
			{Name: "initializer", Type: mustABIType("bytes")},
			{Name: "saltNonce", Type: mustABIType("uint256")},
		},
		abi.Arguments{})
	packed, err := method.Inputs.Pack(singleton, initializer, new(big.Int).SetUint64(nonce))
	if err != nil {
		panic(fmt.Sprintf("createProxyWithNonce encode: %v", err))
	}
	return append(append([]byte(nil), method.ID...), packed...)
}

func encodeGetTransactionHash(transaction SafeTransaction) []byte {
	method := abi.NewMethod("getTransactionHash", "getTransactionHash", abi.Function, "view", false, false,
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
			{Name: "_nonce", Type: mustABIType("uint256")},
		},
		abi.Arguments{{Name: "", Type: mustABIType("bytes32")}})
	packed, err := method.Inputs.Pack(
		transaction.To, transaction.Value, transaction.Data, transaction.Operation,
		transaction.SafeTxGas, transaction.BaseGas, transaction.GasPrice,
		transaction.GasToken, transaction.RefundReceiver, transaction.Nonce,
	)
	if err != nil {
		panic(fmt.Sprintf("getTransactionHash encode: %v", err))
	}
	return append(append([]byte(nil), method.ID...), packed...)
}

func encodeGetMessageHashForSafe(safe common.Address, message [32]byte) []byte {
	method := abi.NewMethod("getMessageHashForSafe", "getMessageHashForSafe", abi.Function, "view", false, false,
		abi.Arguments{
			{Name: "safe", Type: mustABIType("address")},
			{Name: "message", Type: mustABIType("bytes")},
		},
		abi.Arguments{{Name: "", Type: mustABIType("bytes32")}})
	packed, err := method.Inputs.Pack(safe, message[:])
	if err != nil {
		panic(fmt.Sprintf("getMessageHashForSafe encode: %v", err))
	}
	return append(append([]byte(nil), method.ID...), packed...)
}

func encodeIsValidSignature(hash [32]byte, signature []byte) []byte {
	method := abi.NewMethod("isValidSignature", "isValidSignature", abi.Function, "view", false, false,
		abi.Arguments{
			{Name: "_dataHash", Type: mustABIType("bytes32")},
			{Name: "_signature", Type: mustABIType("bytes")},
		},
		abi.Arguments{{Name: "", Type: mustABIType("bytes4")}})
	packed, err := method.Inputs.Pack(hash, signature)
	if err != nil {
		panic(fmt.Sprintf("isValidSignature encode: %v", err))
	}
	return append(append([]byte(nil), method.ID...), packed...)
}
