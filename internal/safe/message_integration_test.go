package safe

import (
	"bytes"
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

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

const eip1271Magic = "0x1626ba7e"

// TestSafeMessageEIP1271SingleOwner verifies the EIP-1271 path with a single
// owner first, isolating aggregate encoding from signature validity.
func TestSafeMessageEIP1271SingleOwner(t *testing.T) {
	anvilURL := os.Getenv("BLOCO_WALLET_ANVIL_URL")
	if anvilURL == "" {
		t.Skip("BLOCO_WALLET_ANVIL_URL not set; skipping EIP-1271 integration")
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
	handler := deployedAddress(ctx, t, rpc, deployer, common.FromHex(signer.CompatibilityFallbackHandlerBytecode))
	RegisterTestContracts(31337, singleton, factory)

	ownerKey, err := ecdsa.GenerateKey(crypto.S256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	owner := crypto.PubkeyToAddress(ownerKey.PublicKey)
	setupData := encodeSafeSetupWithHandler([]common.Address{owner}, big.NewInt(1), handler)
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
	accountRepo := &fakeAccountRepo{accounts: []*wallet.Account{ownerAccount}}
	service, err := NewWithGasPayer(newFakeProposalRepo(), accountRepo, &fakeImporter{repo: accountRepo}, &fakeMessages{}, &multiOwnerSigner{keys: map[common.Address]*ecdsa.PrivateKey{owner: ownerKey}}, &gasPayerTestSigner{key: ownerKey}, adapter, Options{ApprovalTTL: 10 * time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	imported, err := service.ImportSafe(ctx, SafeImportRequest{Name: "Message Safe", Address: safeAddress.Hex(), ChainID: 31337})
	if err != nil {
		t.Fatal(err)
	}
	authorize := func(accountID string, operation func(wallet.CapabilityHandle) error) error {
		return operation(wallet.CapabilityHandle{})
	}
	message := []byte("bloco Safe EIP-1271 single owner")
	proposed, err := service.ProposeMessage(ctx, SafeMessageProposalRequest{
		SafeAccountID: imported.AccountID, ChainID: 31337, Message: message,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.SignMessage(ctx, proposed, ownerAccount.AccountID, authorize); err != nil {
		t.Fatal(err)
	}
	aggregate, err := service.VerifyMessage(proposed)
	if err != nil {
		t.Fatal(err)
	}
	if len(aggregate) != 65 {
		t.Fatalf("unexpected aggregate length %d", len(aggregate))
	}
	messageHash := crypto.Keccak256Hash(message)
	callData, err := encodeIsValidSignatureABI(messageHash, aggregate)
	if err != nil {
		t.Fatal(err)
	}
	result, err := rpc.call(ctx, "eth_call", map[string]any{"to": safeAddress.Hex(), "data": "0x" + hex.EncodeToString(callData)}, "latest")
	if err != nil {
		t.Fatalf("single-owner isValidSignature reverted: %v", err)
	}
	var magicHex string
	if err := json.Unmarshal(result, &magicHex); err != nil {
		t.Fatal(err)
	}
	magic := common.FromHex(magicHex)
	if len(magic) < 4 || !bytes.Equal(magic[:4], common.FromHex(eip1271Magic)) {
		t.Fatalf("single-owner isValidSignature returned %x, want %s", magic, eip1271Magic)
	}
}

// TestSafeMessageEIP1271AgainstRealContracts signs an off-chain message with
// two owners and verifies the aggregate through the real Safe contract's
// isValidSignature (CompatibilityFallbackHandler path).
func TestSafeMessageEIP1271AgainstRealContracts(t *testing.T) {
	anvilURL := os.Getenv("BLOCO_WALLET_ANVIL_URL")
	if anvilURL == "" {
		t.Skip("BLOCO_WALLET_ANVIL_URL not set; skipping EIP-1271 integration")
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
	handler := deployedAddress(ctx, t, rpc, deployer, common.FromHex(signer.CompatibilityFallbackHandlerBytecode))
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

	setupData := encodeSafeSetupWithHandler([]common.Address{owner1, owner2}, big.NewInt(2), handler)
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

	gateway := blockchain.NewRPCGateway(blockchain.RPCGatewayOptions{AllowedLocalTargets: []string{anvilHost(anvilURL)}})
	session, err := gateway.ValidateChain(ctx, anvilURL, 31337)
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := NewRPCAdapter(gateway, session)
	if err != nil {
		t.Fatal(err)
	}
	ownerAccounts := []*wallet.Account{
		{AccountID: "21111111-1111-4111-8111-111111111111", Name: "Owner1", Address: owner1.Hex(), SignerKind: wallet.SignerKindSoftware, State: wallet.AccountStateActive, Capabilities: wallet.CapabilitySignMessage, AuthorizationEpoch: 1},
		{AccountID: "41111111-1111-4111-8111-111111111111", Name: "Owner2", Address: owner2.Hex(), SignerKind: wallet.SignerKindSoftware, State: wallet.AccountStateActive, Capabilities: wallet.CapabilitySignMessage, AuthorizationEpoch: 1},
	}
	accountRepo := &fakeAccountRepo{accounts: ownerAccounts}
	service, err := NewWithGasPayer(newFakeProposalRepo(), accountRepo, &fakeImporter{repo: accountRepo}, &fakeMessages{}, &multiOwnerSigner{keys: map[common.Address]*ecdsa.PrivateKey{owner1: owner1Key, owner2: owner2Key}}, &gasPayerTestSigner{key: owner1Key}, adapter, Options{ApprovalTTL: 10 * time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	imported, err := service.ImportSafe(ctx, SafeImportRequest{Name: "Message Safe", Address: safeAddress.Hex(), ChainID: 31337})
	if err != nil {
		t.Fatal(err)
	}
	authorize := func(accountID string, operation func(wallet.CapabilityHandle) error) error {
		return operation(wallet.CapabilityHandle{})
	}

	message := []byte("bloco Safe EIP-1271 message")
	proposed, err := service.ProposeMessage(ctx, SafeMessageProposalRequest{
		SafeAccountID: imported.AccountID, ChainID: 31337, Message: message,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, ownerAccount := range ownerAccounts {
		if err := service.SignMessage(ctx, proposed, ownerAccount.AccountID, authorize); err != nil {
			t.Fatal(err)
		}
	}
	if len(proposed.Signatures) != 2 {
		t.Fatalf("expected 2 signatures, got %d", len(proposed.Signatures))
	}
	aggregate, err := service.VerifyMessage(proposed)
	if err != nil {
		t.Fatal(err)
	}
	if len(aggregate) != 130 {
		t.Fatalf("unexpected aggregate length %d", len(aggregate))
	}
	// Sanity: each stored signature must recover the owner over the digest.
	for _, collected := range proposed.Signatures {
		raw := append([]byte(nil), collected.Signature...)
		raw[64] -= 27
		publicKey, err := crypto.SigToPub(proposed.Digest[:], raw)
		if err != nil {
			t.Fatalf("signature recovery failed: %v", err)
		}
		if crypto.PubkeyToAddress(*publicKey) != collected.Owner {
			t.Fatalf("signature owner mismatch: %s", collected.Owner)
		}
	}

	// Verify against the real contract: isValidSignature(messageHash, aggregate).
	messageHash := crypto.Keccak256Hash(message)
	callData, err := encodeIsValidSignatureABI(messageHash, aggregate)
	if err != nil {
		t.Fatal(err)
	}
	result, err := rpc.call(ctx, "eth_call", map[string]any{"to": safeAddress.Hex(), "data": "0x" + hex.EncodeToString(callData)}, "latest")
	if err != nil {
		t.Fatal(err)
	}
	var magicHex string
	if err := json.Unmarshal(result, &magicHex); err != nil {
		t.Fatal(err)
	}
	magic := common.FromHex(magicHex)
	if len(magic) < 4 || !bytes.Equal(magic[:4], common.FromHex(eip1271Magic)) {
		t.Fatalf("isValidSignature returned %x, want %s", magic, eip1271Magic)
	}
}

func encodeIsValidSignatureABI(messageHash [32]byte, signature []byte) ([]byte, error) {
	method := abi.NewMethod("isValidSignature", "isValidSignature", abi.Function, "view", false, false,
		abi.Arguments{
			{Name: "_dataHash", Type: mustABIType("bytes32")},
			{Name: "_signature", Type: mustABIType("bytes")},
		},
		abi.Arguments{{Name: "", Type: mustABIType("bytes4")}})
	packed, err := method.Inputs.Pack(messageHash, signature)
	if err != nil {
		return nil, err
	}
	return append(append([]byte(nil), method.ID...), packed...), nil
}
