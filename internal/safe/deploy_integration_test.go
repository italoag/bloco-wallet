package safe

import (
	"context"
	"crypto/ecdsa"
	"crypto/rand"
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

// TestSafeDeployAgainstRealContracts deploys a new Safe through the service
// using the official v1.5.0 contracts on Anvil, then verifies the proxy was
// set up with the requested owners and threshold.
func TestSafeDeployAgainstRealContracts(t *testing.T) {
	anvilURL := os.Getenv("BLOCO_WALLET_ANVIL_URL")
	if anvilURL == "" {
		t.Skip("BLOCO_WALLET_ANVIL_URL not set; skipping Safe deploy integration")
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

	gateway := blockchain.NewRPCGateway(blockchain.RPCGatewayOptions{AllowedLocalTargets: []string{anvilHost(anvilURL)}})
	session, err := gateway.ValidateChain(ctx, anvilURL, 31337)
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := NewRPCAdapter(gateway, session)
	if err != nil {
		t.Fatal(err)
	}
	deployerAccount := &wallet.Account{
		AccountID: "31111111-1111-4111-8111-111111111111", Name: "Deployer", Address: deployer.Hex(),
		SignerKind: wallet.SignerKindSoftware, State: wallet.AccountStateActive,
		Capabilities: wallet.CapabilitySignTransaction, AuthorizationEpoch: 1,
	}
	accountRepo := &fakeAccountRepo{accounts: []*wallet.Account{deployerAccount}}
	service, err := NewWithGasPayer(newFakeProposalRepo(), accountRepo, &fakeImporter{repo: accountRepo}, &fakeMessages{}, &ownerDigestTestSigner{key: ownerKey}, &gasPayerTestSigner{key: deployerKey}, adapter, Options{ApprovalTTL: 10 * time.Minute})
	if err != nil {
		t.Fatal(err)
	}

	deployment, err := service.PrepareDeploy(ctx, DeployRequest{
		ChainID: 31337, Name: "Deployed Safe", Owners: []common.Address{owner}, Threshold: 1, SaltNonce: 99,
	})
	if err != nil {
		t.Fatal(err)
	}
	if deployment.SafeAddress == (common.Address{}) || deployment.Factory != factory || deployment.Singleton != singleton {
		t.Fatalf("unexpected deployment plan: %+v", deployment)
	}
	// The predicted address must be funded before the factory can deploy the
	// proxy (the proxy pays for its own setup call).
	funding := new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)
	fundTx, err := rpc.send(ctx, deployer, &deployment.SafeAddress, funding, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := rpc.waitReceipt(ctx, fundTx); err != nil {
		t.Fatal(err)
	}
	deployTx, err := service.BroadcastDeploy(ctx, deployment, deployerAccount.AccountID, func(accountID string, operation func(wallet.CapabilityHandle) error) error {
		return operation(wallet.CapabilityHandle{})
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := rpc.waitReceipt(ctx, deployTx); err != nil {
		t.Fatal(err)
	}
	code, err := adapter.CodeAt(ctx, deployment.SafeAddress)
	if err != nil {
		t.Fatal(err)
	}
	if len(code) == 0 {
		t.Fatal("deployed Safe has no code")
	}
	owners, err := adapter.Owners(ctx, deployment.SafeAddress)
	if err != nil {
		t.Fatal(err)
	}
	threshold, err := adapter.Threshold(ctx, deployment.SafeAddress)
	if err != nil {
		t.Fatal(err)
	}
	if len(owners) != 1 || owners[0] != owner || threshold != 1 {
		t.Fatalf("deployed Safe setup mismatch: owners=%v threshold=%d", owners, threshold)
	}
}
