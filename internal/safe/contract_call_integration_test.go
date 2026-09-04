package safe

import (
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"math/big"
	"os"
	"testing"
	"time"

	"blocowallet/internal/blockchain"
	"blocowallet/internal/signer"
	"blocowallet/internal/wallet"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// Counter contract: value() view and store(uint256) state change. The
// runtime is hand-assembled (PUSH4 dispatch with JUMPDEST targets,
// SSTORE(0) for store, SLOAD(0)+MSTORE+RETURN for value).
const counterBytecode = "0x602f600c600039602f6000f360003560e01c806360fe47b114601b5780636d4ce63c14602357fe5b600435600055005b60005460005260206000f3"

// TestSafeContractCallAgainstRealContracts proposes a contract call (store
// 42), executes it through the Safe, and verifies the contract state.
func TestSafeContractCallAgainstRealContracts(t *testing.T) {
	anvilURL := os.Getenv("BLOCO_WALLET_ANVIL_URL")
	if anvilURL == "" {
		t.Skip("BLOCO_WALLET_ANVIL_URL not set; skipping contract call integration")
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
	singleton := deployedAddress(ctx, t, rpc, deployer, common.FromHex(signer.SafeBytecode))
	factory := deployedAddress(ctx, t, rpc, deployer, common.FromHex(signer.SafeProxyFactoryBytecode))
	RegisterTestContracts(31337, singleton, factory)
	counter := deployedAddress(ctx, t, rpc, deployer, common.FromHex(counterBytecode))

	ownerKey, err := ecdsa.GenerateKey(crypto.S256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	owner := crypto.PubkeyToAddress(ownerKey.PublicKey)
	deployerKey, err := crypto.HexToECDSA("ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80")
	if err != nil {
		t.Fatal(err)
	}
	setupData := encodeSafeSetupWithHandler([]common.Address{owner}, big.NewInt(1), common.Address{})
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
	proxyCall, err := encodeCreateProxyWithNonceData(singleton, setupData, 1)
	if err != nil {
		t.Fatal(err)
	}
	proxyTx, err := rpc.send(ctx, deployer, &factory, nil, proxyCall)
	if err != nil {
		t.Fatal(err)
	}
	safeAddress := deriveSafeProxyAddress(factory, singleton, creationCode, setupData, big.NewInt(1))
	if err := rpc.waitReceipt(ctx, proxyTx); err != nil {
		t.Fatal(err)
	}
	funding := new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)
	fundTx, err := rpc.send(ctx, deployer, &safeAddress, funding, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := rpc.waitReceipt(ctx, fundTx); err != nil {
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
	service, err := NewWithGasPayer(newFakeProposalRepo(), accountRepo, &fakeImporter{repo: accountRepo}, &fakeMessages{}, &ownerDigestTestSigner{key: ownerKey}, &gasPayerTestSigner{key: deployerKey}, adapter, Options{ApprovalTTL: 10 * time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	imported, err := service.ImportSafe(ctx, SafeImportRequest{Name: "Call Safe", Address: safeAddress.Hex(), ChainID: 31337})
	if err != nil {
		t.Fatal(err)
	}
	authorize := func(accountID string, operation func(wallet.CapabilityHandle) error) error {
		return operation(wallet.CapabilityHandle{})
	}

	// Sanity-check the counter contract deploys and reads back zero.
	initialValue, err := rpc.call(ctx, "eth_call", map[string]any{"to": counter.Hex(), "data": "0x6d4ce63c"}, "latest")
	if err != nil {
		t.Fatalf("counter value() reverted: %v", err)
	}
	t.Logf("counter initial value: %s", string(initialValue))
	// store(uint256 42): 60fe47b1 + 32-byte word.
	storeCalldata := append([]byte{0x60, 0xfe, 0x47, 0xb1}, make([]byte, 32)...)
	storeCalldata[35] = 42
	proposal, err := service.Propose(ctx, SafeProposalRequest{
		SafeAccountID: imported.AccountID, ChainID: 31337, To: counter, Value: big.NewInt(0), Data: storeCalldata,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Sign(ctx, SafeSignRequest{ProposalID: proposal.ProposalID, OwnerAccountID: ownerAccount.AccountID, ChainID: 31337}, authorize); err != nil {
		t.Fatal(err)
	}
	stored, err := service.GetProposal(ctx, proposal.ProposalID)
	if err != nil {
		t.Fatal(err)
	}
	simExec, err := signer.EncodeSafeExecTransaction(signer.SafeTransaction{
		To: stored.Transaction.To, Value: stored.Transaction.Value, Data: stored.Transaction.Data,
		Operation: stored.Transaction.Operation, SafeTxGas: stored.Transaction.SafeTxGas,
		BaseGas: stored.Transaction.BaseGas, GasPrice: stored.Transaction.GasPrice,
		GasToken: stored.Transaction.GasToken, RefundReceiver: stored.Transaction.RefundReceiver,
		Nonce: stored.Transaction.Nonce,
	}, rawSignaturesFromProposal(stored))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rpc.call(ctx, "eth_call", map[string]any{
		"from": deployer.Hex(), "to": safeAddress.Hex(), "data": "0x" + hex.EncodeToString(simExec),
	}, "latest"); err != nil {
		t.Fatalf("execTransaction simulation reverted: %v", err)
	}
	executionHash, err := service.Execute(ctx, proposal.ProposalID, deployerAccount.AccountID, authorize)
	if err != nil {
		t.Fatal(err)
	}
	if err := rpc.waitReceipt(ctx, executionHash); err != nil {
		t.Fatal(err)
	}
	execReceipt, err := rpc.call(ctx, "eth_getTransactionReceipt", executionHash.Hex())
	if err != nil {
		t.Fatal(err)
	}
	var execStatus struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(execReceipt, &execStatus); err != nil {
		t.Fatal(err)
	}
	if execStatus.Status != "0x1" {
		t.Fatalf("contract-call execution reverted")
	}
	valueResult, err := rpc.call(ctx, "eth_call", map[string]any{
		"to": counter.Hex(), "data": "0x6d4ce63c",
	}, "latest")
	if err != nil {
		t.Fatal(err)
	}
	var valueHex string
	if err := json.Unmarshal(valueResult, &valueHex); err != nil {
		t.Fatal(err)
	}
	value := new(big.Int).SetBytes(common.FromHex(valueHex))
	if value.Cmp(big.NewInt(42)) != 0 {
		t.Fatalf("counter value %s, want 42", value)
	}
}
