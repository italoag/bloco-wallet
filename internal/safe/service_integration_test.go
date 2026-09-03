package safe

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

	"blocowallet/internal/blockchain"
	"blocowallet/internal/evm"
	"blocowallet/internal/signer"
	"blocowallet/internal/wallet"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

type anvilRPC struct {
	url    string
	client *http.Client
}

func newAnvilRPC(url string) *anvilRPC {
	return &anvilRPC{url: url, client: &http.Client{Timeout: 30 * time.Second}}
}

func (rpc *anvilRPC) call(ctx context.Context, method string, params ...any) (json.RawMessage, error) {
	payload, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
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

func (rpc *anvilRPC) send(ctx context.Context, from common.Address, to *common.Address, value *big.Int, data []byte) (common.Hash, error) {
	params := map[string]any{"from": from.Hex(), "data": "0x" + hex.EncodeToString(data)}
	if to != nil {
		params["to"] = to.Hex()
	}
	if value != nil {
		params["value"] = "0x" + value.Text(16)
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

func (rpc *anvilRPC) waitReceipt(ctx context.Context, hash common.Hash) error {
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		result, err := rpc.call(ctx, "eth_getTransactionReceipt", hash.Hex())
		if err != nil {
			return err
		}
		var receipt struct {
			Status string `json:"status"`
		}
		if err := json.Unmarshal(result, &receipt); err == nil && receipt.Status != "" {
			if receipt.Status == "0x0" {
				return fmt.Errorf("transaction reverted")
			}
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("receipt timeout")
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

func selectorOf(signature string) []byte {
	return crypto.Keccak256([]byte(signature))[:4]
}

func mustABIType(kind string) abi.Type {
	parsed, err := abi.NewType(kind, "", nil)
	if err != nil {
		panic(err)
	}
	return parsed
}

func encodeSafeSetup(owners []common.Address, threshold *big.Int, handler common.Address) []byte {
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
	packed, err := method.Inputs.Pack(owners, threshold, common.Address{}, []byte{}, handler, common.Address{}, big.NewInt(0), common.Address{})
	if err != nil {
		panic(err)
	}
	return append(append([]byte(nil), method.ID...), packed...)
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
		panic(err)
	}
	return append(append([]byte(nil), method.ID...), packed...)
}

func decodeBytesABI(result []byte) ([]byte, error) {
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

// TestSafeServiceAgainstRealContracts exercises import, propose, sign, and
// execute against the official Safe v1.5.0 contracts on Anvil.
func TestSafeServiceAgainstRealContracts(t *testing.T) {
	anvilURL := os.Getenv("BLOCO_WALLET_ANVIL_URL")
	if anvilURL == "" {
		t.Skip("BLOCO_WALLET_ANVIL_URL not set; skipping Safe service integration")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	rpc := newAnvilRPC(anvilURL)
	if _, err := rpc.call(ctx, "eth_blockNumber"); err != nil {
		t.Fatalf("anvil unreachable: %v", err)
	}
	accountsResult, err := rpc.call(ctx, "eth_accounts")
	if err != nil {
		t.Fatal(err)
	}
	var accounts []string
	if err := json.Unmarshal(accountsResult, &accounts); err != nil || len(accounts) < 2 {
		t.Fatalf("anvil has no unlocked accounts: %v", err)
	}
	deployer := common.HexToAddress(accounts[0])

	// Deploy the official Safe v1.5.0 singleton, factory, and handler.
	singleton := deployedAddress(ctx, t, rpc, deployer, common.FromHex(signer.SafeBytecode))
	factory := deployedAddress(ctx, t, rpc, deployer, common.FromHex(signer.SafeProxyFactoryBytecode))
	handler := deployedAddress(ctx, t, rpc, deployer, common.FromHex(signer.CompatibilityFallbackHandlerBytecode))

	ownerKey, err := ecdsa.GenerateKey(crypto.S256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	owner := crypto.PubkeyToAddress(ownerKey.PublicKey)
	deployerKey, err := crypto.HexToECDSA("ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80")
	if err != nil {
		t.Fatal(err)
	}
	setupData := encodeSafeSetup([]common.Address{owner}, big.NewInt(1), handler)
	creationCodeResult, err := rpc.call(ctx, "eth_call", map[string]any{
		"to": factory.Hex(), "data": "0x" + hex.EncodeToString(selectorOf("proxyCreationCode()")),
	}, "latest")
	if err != nil {
		t.Fatal(err)
	}
	var creationCodeHex string
	if err := json.Unmarshal(creationCodeResult, &creationCodeHex); err != nil {
		t.Fatal(err)
	}
	creationCode, err := decodeBytesABI(common.FromHex(creationCodeHex))
	if err != nil {
		t.Fatal(err)
	}
	proxyTx, err := rpc.send(ctx, deployer, &factory, nil, encodeCreateProxyWithNonce(singleton, setupData, 1))
	if err != nil {
		t.Fatal(err)
	}
	safeAddress := safeProxyAddress(factory, singleton, creationCode, setupData, big.NewInt(1))
	if err := rpc.waitReceipt(ctx, proxyTx); err != nil {
		t.Fatal(err)
	}

	gateway := blockchain.NewRPCGateway(blockchain.RPCGatewayOptions{AllowedLocalTargets: []string{anvilHost(anvilURL)}})
	session, err := gateway.ValidateChain(ctx, anvilURL, 31337)
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := NewRPCAdapter(gateway, session)
	if err != nil {
		t.Fatal(err)
	}
	proposals := newFakeProposalRepo()
	ownerAccount := &wallet.Account{
		AccountID: "21111111-1111-4111-8111-111111111111", Name: "Owner", Address: owner.Hex(),
		SignerKind: wallet.SignerKindSoftware, State: wallet.AccountStateActive,
		Capabilities: wallet.CapabilitySignMessage, AuthorizationEpoch: 1,
	}
	deployerAccount := &wallet.Account{
		AccountID: "31111111-1111-4111-8111-111111111111", Name: "Deployer", Address: deployer.Hex(),
		SignerKind: wallet.SignerKindSoftware, State: wallet.AccountStateActive,
		Capabilities: wallet.CapabilitySignTransaction, AuthorizationEpoch: 1,
	}
	accountRepo := &fakeAccountRepo{accounts: []*wallet.Account{ownerAccount, deployerAccount}}
	importer := &fakeImporter{repo: accountRepo}
	messages := &fakeMessages{}
	signerService := &ownerDigestTestSigner{key: ownerKey}
	gasPayer := &gasPayerTestSigner{key: deployerKey}
	service, err := NewWithGasPayer(proposals, accountRepo, importer, messages, signerService, gasPayer, adapter, Options{ApprovalTTL: 10 * time.Minute})
	if err != nil {
		t.Fatal(err)
	}

	imported, err := service.ImportSafe(ctx, SafeImportRequest{Name: "Real Safe", Address: safeAddress.Hex(), ChainID: 31337})
	if err != nil {
		t.Fatal(err)
	}
	if imported.Threshold != 1 || len(imported.Owners) != 1 || imported.Owners[0] != owner {
		t.Fatalf("on-chain import mismatch: %+v", imported)
	}

	funding := new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)
	fundTx, err := rpc.send(ctx, deployer, &safeAddress, funding, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := rpc.waitReceipt(ctx, fundTx); err != nil {
		t.Fatal(err)
	}
	recipient := common.HexToAddress(accounts[1])
	balanceBefore, err := rpc.balance(ctx, recipient)
	if err != nil {
		t.Fatal(err)
	}

	proposal, err := service.Propose(ctx, SafeProposalRequest{
		SafeAccountID: imported.AccountID, ChainID: 31337, To: recipient, Value: big.NewInt(12345),
	})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := service.Sign(ctx, SafeSignRequest{
		ProposalID: proposal.ProposalID, OwnerAccountID: ownerAccount.AccountID, ChainID: 31337,
	}, func(accountID string, operation func(wallet.CapabilityHandle) error) error {
		return operation(wallet.CapabilityHandle{})
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.Signatures) != 1 {
		t.Fatalf("owner signature missing: %+v", updated)
	}
	executionHash, err := service.Execute(ctx, proposal.ProposalID, deployerAccount.AccountID, func(accountID string, operation func(wallet.CapabilityHandle) error) error {
		return operation(wallet.CapabilityHandle{})
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := rpc.waitReceipt(ctx, executionHash); err != nil {
		t.Fatal(err)
	}
	balanceAfter, err := rpc.balance(ctx, recipient)
	if err != nil {
		t.Fatal(err)
	}
	if new(big.Int).Sub(balanceAfter, balanceBefore).Cmp(big.NewInt(12345)) != 0 {
		t.Fatalf("Safe transfer effect mismatch: before=%s after=%s", balanceBefore, balanceAfter)
	}
}

func deployedAddress(ctx context.Context, t *testing.T, rpc *anvilRPC, deployer common.Address, bytecode []byte) common.Address {
	t.Helper()
	tx, err := rpc.send(ctx, deployer, nil, nil, bytecode)
	if err != nil {
		t.Fatal(err)
	}
	if err := rpc.waitReceipt(ctx, tx); err != nil {
		t.Fatal(err)
	}
	result, err := rpc.call(ctx, "eth_getTransactionReceipt", tx.Hex())
	if err != nil {
		t.Fatal(err)
	}
	var receipt struct {
		ContractAddress string `json:"contractAddress"`
	}
	if err := json.Unmarshal(result, &receipt); err != nil || receipt.ContractAddress == "" {
		t.Fatalf("no contract address in receipt: %v", err)
	}
	return common.HexToAddress(receipt.ContractAddress)
}

func anvilHost(rawURL string) string {
	trimmed := strings.TrimPrefix(strings.TrimPrefix(rawURL, "http://"), "https://")
	if index := strings.Index(trimmed, "/"); index >= 0 {
		trimmed = trimmed[:index]
	}
	return trimmed
}

var _ = evm.MessageSigningInProgress
