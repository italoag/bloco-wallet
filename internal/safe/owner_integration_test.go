package safe

import (
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"strings"
	"testing"
	"time"

	"blocowallet/internal/blockchain"
	"blocowallet/internal/evm"
	"blocowallet/internal/signer"
	"blocowallet/internal/wallet"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// TestSafeOwnerManagementAgainstRealContracts exercises add-owner,
// remove-owner, and change-threshold proposals end to end on the official
// Safe v1.5.0 contracts.
func TestSafeOwnerManagementAgainstRealContracts(t *testing.T) {
	anvilURL := os.Getenv("BLOCO_WALLET_ANVIL_URL")
	if anvilURL == "" {
		t.Skip("BLOCO_WALLET_ANVIL_URL not set; skipping owner management integration")
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

	owner1Key, err := ecdsa.GenerateKey(crypto.S256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	owner2Key, err := ecdsa.GenerateKey(crypto.S256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	owner1 := crypto.PubkeyToAddress(owner1Key.PublicKey)
	owner2 := crypto.PubkeyToAddress(owner2Key.PublicKey)
	newOwnerKey, err := ecdsa.GenerateKey(crypto.S256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	newOwner := crypto.PubkeyToAddress(newOwnerKey.PublicKey)
	deployerKey, err := crypto.HexToECDSA("ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80")
	if err != nil {
		t.Fatal(err)
	}

	setupData := encodeSafeSetupWithHandler([]common.Address{owner1, owner2}, big.NewInt(2), common.Address{})
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
	proposals := newFakeProposalRepo()
	ownerAccounts := []*wallet.Account{
		{AccountID: "21111111-1111-4111-8111-111111111111", Name: "Owner1", Address: owner1.Hex(), SignerKind: wallet.SignerKindSoftware, State: wallet.AccountStateActive, Capabilities: wallet.CapabilitySignMessage, AuthorizationEpoch: 1},
		{AccountID: "41111111-1111-4111-8111-111111111111", Name: "Owner2", Address: owner2.Hex(), SignerKind: wallet.SignerKindSoftware, State: wallet.AccountStateActive, Capabilities: wallet.CapabilitySignMessage, AuthorizationEpoch: 1},
		{AccountID: "51111111-1111-4111-8111-111111111111", Name: "Deployer", Address: deployer.Hex(), SignerKind: wallet.SignerKindSoftware, State: wallet.AccountStateActive, Capabilities: wallet.CapabilitySignTransaction, AuthorizationEpoch: 1},
	}
	accountRepo := &fakeAccountRepo{accounts: ownerAccounts}
	importer := &fakeImporter{repo: accountRepo}
	messages := &fakeMessages{}
	service, err := NewWithGasPayer(proposals, accountRepo, importer, messages, &multiOwnerSigner{keys: map[common.Address]*ecdsa.PrivateKey{owner1: owner1Key, owner2: owner2Key, newOwner: newOwnerKey}}, &gasPayerTestSigner{key: deployerKey}, adapter, Options{ApprovalTTL: 10 * time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	imported, err := service.ImportSafe(ctx, SafeImportRequest{Name: "Managed Safe", Address: safeAddress.Hex(), ChainID: 31337})
	if err != nil {
		t.Fatal(err)
	}
	authorize := func(accountID string, operation func(wallet.CapabilityHandle) error) error {
		return operation(wallet.CapabilityHandle{})
	}

	// Add owner 3 with threshold 3 (both owners must sign).
	addProposal, err := service.ProposeOwnerChange(ctx, OwnerProposalRequest{
		SafeAccountID: imported.AccountID, ChainID: 31337, Action: OwnerActionAdd, Owner: newOwner, Threshold: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, ownerAccount := range ownerAccounts[:2] {
		if _, err := service.Sign(ctx, SafeSignRequest{ProposalID: addProposal.ProposalID, OwnerAccountID: ownerAccount.AccountID, ChainID: 31337}, authorize); err != nil {
			t.Fatal(err)
		}
	}
	executionHash, err := service.Execute(ctx, addProposal.ProposalID, ownerAccounts[2].AccountID, authorize)
	if err != nil {
		t.Fatal(err)
	}
	if err := rpc.waitReceipt(ctx, executionHash); err != nil {
		t.Fatal(err)
	}
	receiptResult, err := rpc.call(ctx, "eth_getTransactionReceipt", executionHash.Hex())
	if err != nil {
		t.Fatal(err)
	}
	var receiptStatus struct {
		Status string `json:"status"`
		To     string `json:"to"`
		Logs   []struct {
			Topics []string `json:"topics"`
		} `json:"logs"`
	}
	if err := json.Unmarshal(receiptResult, &receiptStatus); err != nil {
		t.Fatal(err)
	}
	if receiptStatus.Status != "0x1" {
		t.Fatalf("add-owner execution reverted")
	}
	if !strings.EqualFold(receiptStatus.To, safeAddress.Hex()) {
		t.Fatalf("execution went to %s, want Safe %s", receiptStatus.To, safeAddress.Hex())
	}
	nonceResult, err := rpc.call(ctx, "eth_call", map[string]any{"to": safeAddress.Hex(), "data": "0x" + hex.EncodeToString(selectorOf("nonce()"))}, "latest")
	if err != nil {
		t.Fatal(err)
	}
	var nonceHex string
	if err := json.Unmarshal(nonceResult, &nonceHex); err != nil {
		t.Fatal(err)
	}
	owners, err := adapter.Owners(ctx, safeAddress)
	if err != nil {
		t.Fatal(err)
	}
	if len(owners) != 3 {
		t.Fatalf("add owner failed: %v", owners)
	}
	foundNew := false
	for _, owner := range owners {
		if owner == newOwner {
			foundNew = true
		}
	}
	if !foundNew {
		t.Fatalf("new owner missing after add: %v", owners)
	}

	// Change threshold to 1 (still two owners sign at this point).
	thresholdProposal, err := service.ProposeOwnerChange(ctx, OwnerProposalRequest{
		SafeAccountID: imported.AccountID, ChainID: 31337, Action: OwnerActionChangeThreshold, Threshold: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, ownerAccount := range ownerAccounts[:2] {
		if _, err := service.Sign(ctx, SafeSignRequest{ProposalID: thresholdProposal.ProposalID, OwnerAccountID: ownerAccount.AccountID, ChainID: 31337}, authorize); err != nil {
			t.Fatal(err)
		}
	}
	thresholdExec, err := service.Execute(ctx, thresholdProposal.ProposalID, ownerAccounts[2].AccountID, authorize)
	if err != nil {
		t.Fatal(err)
	}
	if err := rpc.waitReceipt(ctx, thresholdExec); err != nil {
		t.Fatal(err)
	}
	thresholdReceipt, err := rpc.call(ctx, "eth_getTransactionReceipt", thresholdExec.Hex())
	if err != nil {
		t.Fatal(err)
	}
	var thresholdStatus struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(thresholdReceipt, &thresholdStatus); err != nil {
		t.Fatal(err)
	}
	if thresholdStatus.Status != "0x1" {
		t.Fatalf("change-threshold execution reverted")
	}
	threshold, err := adapter.Threshold(ctx, safeAddress)
	if err != nil {
		t.Fatal(err)
	}
	if threshold != 1 {
		t.Fatalf("change threshold failed: %d", threshold)
	}

	// Remove owner 1 (threshold clamps to owner count - 1 = 2).
	removeProposal, err := service.ProposeOwnerChange(ctx, OwnerProposalRequest{
		SafeAccountID: imported.AccountID, ChainID: 31337, Action: OwnerActionRemove, Owner: owner1,
	})
	if err != nil {
		t.Fatal(err)
	}
	// With threshold 1, a single owner (owner2) can now sign and execute.
	if _, err := service.Sign(ctx, SafeSignRequest{ProposalID: removeProposal.ProposalID, OwnerAccountID: ownerAccounts[1].AccountID, ChainID: 31337}, authorize); err != nil {
		t.Fatal(err)
	}
	removeExec, err := service.Execute(ctx, removeProposal.ProposalID, ownerAccounts[2].AccountID, authorize)
	if err != nil {
		t.Fatal(err)
	}
	if err := rpc.waitReceipt(ctx, removeExec); err != nil {
		t.Fatal(err)
	}
	removeReceipt, err := rpc.call(ctx, "eth_getTransactionReceipt", removeExec.Hex())
	if err != nil {
		t.Fatal(err)
	}
	var removeStatus struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(removeReceipt, &removeStatus); err != nil {
		t.Fatal(err)
	}
	if removeStatus.Status != "0x1" {
		t.Fatalf("remove-owner execution reverted")
	}
	owners, err = adapter.Owners(ctx, safeAddress)
	if err != nil {
		t.Fatal(err)
	}
	if len(owners) != 2 {
		t.Fatalf("remove owner failed: %v", owners)
	}
	for _, owner := range owners {
		if owner == owner1 {
			t.Fatalf("owner1 still present after remove: %v", owners)
		}
	}
}

func rawSignaturesFromProposal(proposal *Proposal) []byte {
	coordinator, err := signer.NewSafeCoordinator(signer.SafeTransactionIntent{
		SafeAddress: proposal.SafeAddress, ChainID: proposal.ChainID, Nonce: proposal.Nonce,
		Owners: func() []signer.SafeOwnerSnapshot {
			snapshot := make([]signer.SafeOwnerSnapshot, 0, len(proposal.Owners))
			for _, owner := range proposal.Owners {
				snapshot = append(snapshot, signer.SafeOwnerSnapshot{Address: owner.Address, Kind: signer.SafeOwnerKind(owner.Kind)})
			}
			return snapshot
		}(),
		Threshold: proposal.Threshold,
		Transaction: signer.SafeTransaction{
			To: proposal.Transaction.To, Value: proposal.Transaction.Value, Data: proposal.Transaction.Data,
			Operation: proposal.Transaction.Operation, SafeTxGas: proposal.Transaction.SafeTxGas,
			BaseGas: proposal.Transaction.BaseGas, GasPrice: proposal.Transaction.GasPrice,
			GasToken: proposal.Transaction.GasToken, RefundReceiver: proposal.Transaction.RefundReceiver,
			Nonce: proposal.Transaction.Nonce,
		},
		Digest: proposal.Digest, Commitment: proposal.Commitment,
	})
	if err != nil {
		panic(err)
	}
	for _, collected := range proposal.Signatures {
		if err := coordinator.AddSignature(signer.SafeOwnerSignature{
			Owner: collected.Owner, Kind: signer.SafeOwnerKind(collected.Kind),
			Digest: proposal.Digest, Commitment: proposal.Commitment, Signature: collected.Signature,
		}); err != nil {
			panic(err)
		}
	}
	signatures, err := coordinator.Signatures()
	if err != nil {
		panic(err)
	}
	return signatures
}

// multiOwnerSigner signs Safe digests with the key of the requesting owner.
type multiOwnerSigner struct {
	keys map[common.Address]*ecdsa.PrivateKey
}

func (signer *multiOwnerSigner) SignSafeOwnerDigest(_ context.Context, _ wallet.CapabilityHandle, request evm.SafeOwnerDigestRequest) (wallet.SoftwareSigningResult, error) {
	key := signer.keys[request.Signer]
	if key == nil {
		return wallet.SoftwareSigningResult{}, fmt.Errorf("no key for owner %s", request.Signer)
	}
	signature, err := crypto.Sign(request.Digest[:], key)
	if err != nil {
		return wallet.SoftwareSigningResult{}, err
	}
	return wallet.SoftwareSigningResult{
		AccountID: request.AccountID, Purpose: wallet.SigningPurposeMessage,
		MessageScheme: wallet.MessageSigningSafeOwner, ChainID: request.ChainID,
		Digest: request.Digest, IntentHash: request.IntentHash, Signature: signature,
	}, nil
}
