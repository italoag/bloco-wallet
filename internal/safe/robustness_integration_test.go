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

// TestSafeExecutionStaleNonceRejected verifies that executing a proposal
// whose on-chain nonce moved is refused and marked failed.
func TestSafeExecutionStaleNonceRejected(t *testing.T) {
	anvilURL := os.Getenv("BLOCO_WALLET_ANVIL_URL")
	if anvilURL == "" {
		t.Skip("BLOCO_WALLET_ANVIL_URL not set; skipping robustness integration")
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
	imported, err := service.ImportSafe(ctx, SafeImportRequest{Name: "Stale Safe", Address: safeAddress.Hex(), ChainID: 31337})
	if err != nil {
		t.Fatal(err)
	}
	authorize := func(accountID string, operation func(wallet.CapabilityHandle) error) error {
		return operation(wallet.CapabilityHandle{})
	}
	recipient := common.HexToAddress(accounts[1])

	// Proposal at nonce 0.
	proposal, err := service.Propose(ctx, SafeProposalRequest{
		SafeAccountID: imported.AccountID, ChainID: 31337, To: recipient, Value: big.NewInt(1),
	})
	if err != nil {
		t.Fatal(err)
	}
	// Execute a DIFFERENT proposal to move the on-chain nonce to 1 first.
	proposalTwo, err := service.Propose(ctx, SafeProposalRequest{
		SafeAccountID: imported.AccountID, ChainID: 31337, To: recipient, Value: big.NewInt(2),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Sign(ctx, SafeSignRequest{ProposalID: proposalTwo.ProposalID, OwnerAccountID: ownerAccount.AccountID, ChainID: 31337}, authorize); err != nil {
		t.Fatal(err)
	}
	execTwo, err := service.Execute(ctx, proposalTwo.ProposalID, deployerAccount.AccountID, authorize)
	if err != nil {
		t.Fatal(err)
	}
	if err := rpc.waitReceipt(ctx, execTwo); err != nil {
		t.Fatal(err)
	}
	if _, err := service.CheckExecution(ctx, proposalTwo.ProposalID); err != nil {
		t.Fatal(err)
	}

	// Now the first proposal is stale: executing must fail and mark failed.
	if _, err := service.Sign(ctx, SafeSignRequest{ProposalID: proposal.ProposalID, OwnerAccountID: ownerAccount.AccountID, ChainID: 31337}, authorize); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Execute(ctx, proposal.ProposalID, deployerAccount.AccountID, authorize); err == nil {
		t.Fatal("stale nonce execution was not rejected")
	}
	stored, err := service.GetProposal(ctx, proposal.ProposalID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != ProposalFailed || stored.FailureCode != "stale_nonce" {
		t.Fatalf("expected stale_nonce failure, got %s/%s", stored.Status, stored.FailureCode)
	}
}
