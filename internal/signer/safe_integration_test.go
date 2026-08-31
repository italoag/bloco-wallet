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
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

const (
	safeMagicValue = "0x1626ba7e" // EIP-1271 isValidSignature magic
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

// TestSafeEIP1271AgainstRealContract deploys the official Safe v1.3.0
// contracts on an Anvil node and verifies isValidSignature with the
// composed owner signature. Skipped unless BLOCO_WALLET_ANVIL_URL is set.
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

	// Deploy the Safe L2 singleton and the proxy factory.
	singleton, err := rpc.sendTransaction(ctx, deployer, nil, common.FromHex(safeL2Bytecode))
	if err != nil {
		t.Fatal(err)
	}
	singletonAddress, err := rpc.waitReceipt(ctx, singleton)
	if err != nil {
		t.Fatal(err)
	}
	factoryTx, err := rpc.sendTransaction(ctx, deployer, nil, common.FromHex(safeProxyFactoryBytecode))
	if err != nil {
		t.Fatal(err)
	}
	factoryAddress, err := rpc.waitReceipt(ctx, factoryTx)
	if err != nil {
		t.Fatal(err)
	}

	// Create a Safe with one owner (threshold 1).
	ownerKey, err := ecdsa.GenerateKey(crypto.S256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	owner := crypto.PubkeyToAddress(ownerKey.PublicKey)
	setupData, err := encodeSafeSetup([]common.Address{owner}, common.Address{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	proxyTx, err := rpc.sendTransaction(ctx, deployer, &factoryAddress,
		encodeCreateProxyWithNonce(singletonAddress, setupData, 1))
	if err != nil {
		t.Fatal(err)
	}
	safeAddress, err := rpc.waitReceipt(ctx, proxyTx)
	if err != nil {
		t.Fatal(err)
	}
	if safeAddress == (common.Address{}) {
		t.Fatal("proxy deployment returned no address")
	}

	// Owner signs the SafeMessage digest; compose the EIP-1271 payload.
	messageHash := crypto.Keccak256Hash([]byte("bloco safe onchain vector"))
	digest, err := SafeMessageDigest(safeAddress, 31337, messageHash)
	if err != nil {
		t.Fatal(err)
	}
	ownerSignature, err := crypto.Sign(digest[:], ownerKey)
	if err != nil {
		t.Fatal(err)
	}
	composed := ComposeSafeSignature(array65(ownerSignature), owner)

	// isValidSignature must return the EIP-1271 magic value.
	magic, err := rpc.callContract(ctx, safeAddress, encodeIsValidSignature(digest, composed))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(magic, common.FromHex(safeMagicValue)) {
		t.Fatalf("isValidSignature returned %x, want %s", magic, safeMagicValue)
	}

	// A signature from a non-owner must be rejected.
	foreignKey, err := ecdsa.GenerateKey(crypto.S256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	foreignSignature, err := crypto.Sign(digest[:], foreignKey)
	if err != nil {
		t.Fatal(err)
	}
	foreignComposed := ComposeSafeSignature(array65(foreignSignature), crypto.PubkeyToAddress(foreignKey.PublicKey))
	rejected, err := rpc.callContract(ctx, safeAddress, encodeIsValidSignature(digest, foreignComposed))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(rejected, common.FromHex(safeMagicValue)) {
		t.Fatal("non-owner signature was accepted by the real Safe")
	}
	// A tampered message hash must be rejected too.
	tampered := digest
	tampered[0] ^= 0x01
	tamperedResult, err := rpc.callContract(ctx, safeAddress, encodeIsValidSignature(tampered, composed))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(tamperedResult, common.FromHex(safeMagicValue)) {
		t.Fatal("tampered message hash was accepted by the real Safe")
	}
}

func array65(value []byte) [65]byte {
	var result [65]byte
	copy(result[:], value)
	return result
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
