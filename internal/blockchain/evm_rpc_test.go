package blockchain

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"blocowallet/internal/evm"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

func TestEVMRPCHandlesNullableReceiptAndCanonicalHeader(t *testing.T) {
	receiptCalls := 0
	transactionHash := common.HexToHash("0xcccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var payload json.RawMessage
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			http.Error(writer, "bad request", http.StatusBadRequest)
			return
		}
		if len(payload) == 0 || payload[0] != '[' {
			_, _ = fmt.Fprint(writer, `{"jsonrpc":"2.0","id":1,"result":"0x1"}`)
			return
		}
		var batch []struct {
			Method string `json:"method"`
		}
		if err := json.Unmarshal(payload, &batch); err != nil || len(batch) != 2 {
			http.Error(writer, "bad batch", http.StatusBadRequest)
			return
		}
		switch batch[1].Method {
		case "eth_getTransactionReceipt":
			receiptCalls++
			if receiptCalls == 1 {
				_, _ = fmt.Fprint(writer, `[{"jsonrpc":"2.0","id":1,"result":"0x1"},{"jsonrpc":"2.0","id":2,"result":null}]`)
				return
			}
			_, _ = fmt.Fprintf(writer, `[{"jsonrpc":"2.0","id":1,"result":"0x1"},{"jsonrpc":"2.0","id":2,"result":{"transactionHash":%q,"blockNumber":"0x10","blockHash":"0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","transactionIndex":"0x0","status":"0x1","gasUsed":"0x5208","effectiveGasPrice":"0x3b9aca00"}}]`, transactionHash.Hex())
		case "eth_getBlockByNumber":
			_, _ = fmt.Fprint(writer, `[{"jsonrpc":"2.0","id":1,"result":"0x1"},{"jsonrpc":"2.0","id":2,"result":{"number":"0x10","hash":"0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","parentHash":"0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","gasLimit":"0x1c9c380","baseFeePerGas":"0x3b9aca00"}}]`)
		}
	}))
	defer server.Close()
	parsed, _ := url.Parse(server.URL)
	gateway := NewRPCGateway(RPCGatewayOptions{AllowedLocalTargets: []string{parsed.Host}})
	session, err := gateway.ValidateChain(context.Background(), server.URL, 1)
	if err != nil {
		t.Fatal(err)
	}
	client, err := NewEVMRPC(gateway, session)
	if err != nil {
		t.Fatal(err)
	}
	if _, found, err := client.TransactionReceipt(context.Background(), transactionHash); err != nil || found {
		t.Fatalf("pending receipt was not represented as absent: found=%t err=%v", found, err)
	}
	receipt, found, err := client.TransactionReceipt(context.Background(), transactionHash)
	if err != nil || !found || receipt.Status != 1 || receipt.Block.Number != 16 || receipt.Block.Hash == (common.Hash{}) || receipt.GasUsed != 21_000 {
		t.Fatalf("unexpected receipt: %+v found=%t err=%v", receipt, found, err)
	}
	header, found, err := client.HeaderByNumber(context.Background(), 16)
	if err != nil || !found || header.Hash != receipt.Block.Hash {
		t.Fatalf("canonical header lookup failed: %+v found=%t err=%v", header, found, err)
	}
}

func TestEVMRPCBroadcastsExactSignedBytesAndVerifiesHash(t *testing.T) {
	privateKey, err := crypto.HexToECDSA(strings.Repeat("1", 64))
	if err != nil {
		t.Fatal(err)
	}
	transaction, err := types.SignNewTx(privateKey, types.LatestSignerForChainID(big.NewInt(1)), &types.DynamicFeeTx{
		ChainID: big.NewInt(1), Nonce: 1, GasTipCap: big.NewInt(1), GasFeeCap: big.NewInt(2),
		Gas: 21_000, To: &common.Address{1}, Value: big.NewInt(3),
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := transaction.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	localHash := transaction.Hash()
	expectedRaw := "0x" + common.Bytes2Hex(raw)
	var alreadyKnown atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var payload struct {
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
		}
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			http.Error(writer, "bad request", http.StatusBadRequest)
			return
		}
		if payload.Method == "eth_chainId" {
			_, _ = fmt.Fprint(writer, `{"jsonrpc":"2.0","id":1,"result":"0x1"}`)
			return
		}
		if payload.Method != "eth_sendRawTransaction" || len(payload.Params) != 1 {
			http.Error(writer, "bad method", http.StatusBadRequest)
			return
		}
		var sent string
		if err := json.Unmarshal(payload.Params[0], &sent); err != nil || sent != expectedRaw {
			http.Error(writer, "wrong payload", http.StatusBadRequest)
			return
		}
		if alreadyKnown.Load() {
			_, _ = fmt.Fprint(writer, `{"jsonrpc":"2.0","id":1,"error":{"code":-32000,"message":"already known"}}`)
			return
		}
		_, _ = fmt.Fprintf(writer, `{"jsonrpc":"2.0","id":1,"result":%q}`, localHash.Hex())
	}))
	defer server.Close()
	parsed, _ := url.Parse(server.URL)
	gateway := NewRPCGateway(RPCGatewayOptions{AllowedLocalTargets: []string{parsed.Host}})
	session, err := gateway.ValidateChain(context.Background(), server.URL, 1)
	if err != nil {
		t.Fatal(err)
	}
	client, err := NewEVMRPC(gateway, session)
	if err != nil {
		t.Fatal(err)
	}
	remoteHash, err := client.SendRawTransaction(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	if remoteHash != localHash {
		t.Fatalf("unexpected broadcast hash: %s", remoteHash)
	}
	alreadyKnown.Store(true)
	remoteHash, err = client.SendRawTransaction(context.Background(), raw)
	if err != nil || remoteHash != localHash {
		t.Fatalf("already-known broadcast was not idempotent: %s %v", remoteHash, err)
	}
}

func TestEVMRPCExposesBoundedRevertDataWithoutRemoteMessage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var payload json.RawMessage
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			http.Error(writer, "bad request", http.StatusBadRequest)
			return
		}
		if len(payload) == 0 || payload[0] != '[' {
			_, _ = fmt.Fprint(writer, `{"jsonrpc":"2.0","id":1,"result":"0x1"}`)
			return
		}
		_, _ = fmt.Fprint(writer, `[{"jsonrpc":"2.0","id":1,"result":"0x1"},{"jsonrpc":"2.0","id":2,"error":{"code":3,"message":"execution reverted super-secret-token","data":"0x08c379a0"}}]`)
	}))
	defer server.Close()
	parsed, _ := url.Parse(server.URL)
	gateway := NewRPCGateway(RPCGatewayOptions{AllowedLocalTargets: []string{parsed.Host}})
	session, err := gateway.ValidateChain(context.Background(), server.URL, 1)
	if err != nil {
		t.Fatal(err)
	}
	client, err := NewEVMRPC(gateway, session)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.CallContract(context.Background(), evm.TransactionCall{
		From: common.HexToAddress("0x1111111111111111111111111111111111111111"),
		To:   common.HexToAddress("0x2222222222222222222222222222222222222222"), Value: new(big.Int),
	}, evm.BlockIdentity{Number: 1, Hash: common.HexToHash("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")})
	var remoteError *RPCRemoteError
	if !errors.As(err, &remoteError) || remoteError.Kind != RPCErrorExecutionReverted || fmt.Sprintf("%x", remoteError.Data) != "08c379a0" {
		t.Fatalf("revert error was not typed: %T %+v", err, remoteError)
	}
	if strings.Contains(err.Error(), "super-secret-token") {
		t.Fatalf("remote message crossed error boundary: %v", err)
	}
}

func TestEVMRPCSimulatesAndEstimatesTypedCall(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var payload json.RawMessage
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			http.Error(writer, "bad request", http.StatusBadRequest)
			return
		}
		if len(payload) == 0 || payload[0] != '[' {
			_, _ = fmt.Fprint(writer, `{"jsonrpc":"2.0","id":1,"result":"0x1"}`)
			return
		}
		var batch []struct {
			Method string `json:"method"`
		}
		if err := json.Unmarshal(payload, &batch); err != nil || len(batch) != 2 {
			http.Error(writer, "bad batch", http.StatusBadRequest)
			return
		}
		switch batch[1].Method {
		case "eth_call":
			_, _ = fmt.Fprint(writer, `[{"jsonrpc":"2.0","id":1,"result":"0x1"},{"jsonrpc":"2.0","id":2,"result":"0x01"}]`)
		case "eth_estimateGas":
			_, _ = fmt.Fprint(writer, `[{"jsonrpc":"2.0","id":1,"result":"0x1"},{"jsonrpc":"2.0","id":2,"result":"0x5208"}]`)
		case "eth_gasPrice":
			_, _ = fmt.Fprint(writer, `[{"jsonrpc":"2.0","id":1,"result":"0x1"},{"jsonrpc":"2.0","id":2,"result":"0x3b9aca00"}]`)
		case "eth_maxPriorityFeePerGas":
			_, _ = fmt.Fprint(writer, `[{"jsonrpc":"2.0","id":1,"result":"0x1"},{"jsonrpc":"2.0","id":2,"result":"0x77359400"}]`)
		case "eth_getCode":
			_, _ = fmt.Fprint(writer, `[{"jsonrpc":"2.0","id":1,"result":"0x1"},{"jsonrpc":"2.0","id":2,"result":"0x60016000"}]`)
		default:
			_, _ = fmt.Fprint(writer, `[{"jsonrpc":"2.0","id":1,"result":"0x1"},{"jsonrpc":"2.0","id":2,"error":{"code":-32601,"message":"unknown"}}]`)
		}
	}))
	defer server.Close()
	parsed, _ := url.Parse(server.URL)
	gateway := NewRPCGateway(RPCGatewayOptions{AllowedLocalTargets: []string{parsed.Host}})
	session, err := gateway.ValidateChain(context.Background(), server.URL, 1)
	if err != nil {
		t.Fatal(err)
	}
	client, err := NewEVMRPC(gateway, session)
	if err != nil {
		t.Fatal(err)
	}
	call := evm.TransactionCall{
		From:  common.HexToAddress("0x1111111111111111111111111111111111111111"),
		To:    common.HexToAddress("0x2222222222222222222222222222222222222222"),
		Value: big.NewInt(5),
		Input: []byte{0xaa, 0xbb},
	}
	block := evm.BlockIdentity{Number: 16, Hash: common.HexToHash("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")}
	result, err := client.CallContract(context.Background(), call, block)
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 1 || result[0] != 1 {
		t.Fatalf("unexpected eth_call result: %x", result)
	}
	gas, err := client.EstimateGas(context.Background(), call, block)
	if err != nil {
		t.Fatal(err)
	}
	if gas != 21_000 {
		t.Fatalf("unexpected gas estimate: %d", gas)
	}
	gasPrice, err := client.SuggestGasPrice(context.Background())
	if err != nil || gasPrice.Cmp(big.NewInt(1_000_000_000)) != 0 {
		t.Fatalf("unexpected legacy gas price: %v, %v", gasPrice, err)
	}
	tip, err := client.SuggestGasTipCap(context.Background())
	if err != nil || tip.Cmp(big.NewInt(2_000_000_000)) != 0 {
		t.Fatalf("unexpected priority fee: %v, %v", tip, err)
	}
	code, err := client.CodeAt(context.Background(), call.To, block)
	if err != nil || fmt.Sprintf("%x", code) != "60016000" {
		t.Fatalf("unexpected contract code: %x, %v", code, err)
	}
}

func TestEVMRPCParsesLatestHeaderAndPendingNonce(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var payload json.RawMessage
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			http.Error(writer, "bad request", http.StatusBadRequest)
			return
		}
		if len(payload) == 0 || payload[0] != '[' {
			_, _ = fmt.Fprint(writer, `{"jsonrpc":"2.0","id":1,"result":"0x1"}`)
			return
		}
		var batch []struct {
			Method string `json:"method"`
		}
		if err := json.Unmarshal(payload, &batch); err != nil || len(batch) != 2 {
			http.Error(writer, "bad batch", http.StatusBadRequest)
			return
		}
		switch batch[1].Method {
		case "eth_getBlockByNumber":
			_, _ = fmt.Fprint(writer, `[{"jsonrpc":"2.0","id":1,"result":"0x1"},{"jsonrpc":"2.0","id":2,"result":{"number":"0x10","hash":"0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","parentHash":"0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","gasLimit":"0x1c9c380","baseFeePerGas":"0x3b9aca00"}}]`)
		case "eth_getTransactionCount":
			_, _ = fmt.Fprint(writer, `[{"jsonrpc":"2.0","id":1,"result":"0x1"},{"jsonrpc":"2.0","id":2,"result":"0x7"}]`)
		default:
			_, _ = fmt.Fprint(writer, `[{"jsonrpc":"2.0","id":1,"result":"0x1"},{"jsonrpc":"2.0","id":2,"error":{"code":-32601,"message":"unknown"}}]`)
		}
	}))
	defer server.Close()
	parsed, _ := url.Parse(server.URL)
	gateway := NewRPCGateway(RPCGatewayOptions{AllowedLocalTargets: []string{parsed.Host}})
	session, err := gateway.ValidateChain(context.Background(), server.URL, 1)
	if err != nil {
		t.Fatal(err)
	}
	client, err := NewEVMRPC(gateway, session)
	if err != nil {
		t.Fatal(err)
	}
	if client.ProviderBinding() == (evm.ProviderBinding{}) {
		t.Fatal("validated provider session has no opaque binding")
	}
	header, err := client.LatestHeader(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if header.Number != 16 || header.Hash != common.HexToHash("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa") || header.GasLimit != 30_000_000 || header.BaseFeePerGas == nil || header.BaseFeePerGas.Uint64() != 1_000_000_000 {
		t.Fatalf("unexpected latest header: %+v", header)
	}
	nonce, err := client.PendingNonceAt(context.Background(), common.HexToAddress("0x1111111111111111111111111111111111111111"))
	if err != nil {
		t.Fatal(err)
	}
	if nonce != 7 {
		t.Fatalf("unexpected pending nonce: %d", nonce)
	}
}
