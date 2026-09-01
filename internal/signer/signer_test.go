package signer

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"encoding/json"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"blocowallet/internal/blockchain"
	"blocowallet/internal/wallet"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

type fakeAccountLookup struct {
	account *wallet.Account
}

func (lookup fakeAccountLookup) GetAccount(context.Context, string) (*wallet.Account, error) {
	return lookup.account, nil
}

type fakeApprovalVerifier struct {
	requireError error
}

func (verifier fakeApprovalVerifier) VerifyTransactionApproval(context.Context, wallet.TransactionApprovalBinding) error {
	return verifier.requireError
}
func (verifier fakeApprovalVerifier) VerifyMessageApproval(context.Context, wallet.MessageApprovalBinding) error {
	return verifier.requireError
}

func testCloudAccount(privateKey *ecdsa.PrivateKey, references ...string) *wallet.Account {
	reference := "cloud:v1:https://vault.example"
	if len(references) == 1 {
		reference = references[0]
	}
	return &wallet.Account{
		AccountID: "11111111-1111-4111-8111-111111111111",
		Name:      "cloud", Address: crypto.PubkeyToAddress(privateKey.PublicKey).Hex(),
		SignerKind: wallet.SignerKindCloud, SignerReference: reference,
		State: wallet.AccountStateActive,
	}
}

func testGatewayForServer(t *testing.T, rawURL string) *blockchain.RPCGateway {
	t.Helper()
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	return blockchain.NewRPCGateway(blockchain.RPCGatewayOptions{
		AllowedLocalTargets: []string{parsed.Host}, MaxRequestsPerSecond: 256,
	})
}

func signForTest(privateKey *ecdsa.PrivateKey, digest [32]byte) []byte {
	signature, err := crypto.Sign(digest[:], privateKey)
	if err != nil {
		panic(err)
	}
	return signature
}

func TestCloudSignerRejectsUnsafeEndpoints(t *testing.T) {
	gateway := blockchain.NewRPCGateway(blockchain.RPCGatewayOptions{})
	for _, endpoint := range []string{
		"http://example.com/sign", "https://user:secret@example.com/sign", "https://example.com/sign?token=secret", "https://example.com/sign#secret",
	} {
		if _, err := NewVaultCompatibleAPI(endpoint, func() (string, error) { return "token", nil }, gateway); err == nil {
			t.Fatalf("unsafe cloud signer endpoint was accepted: %s", endpoint)
		}
	}
}

func TestCloudSignerSignsApprovedDigestAndVerifiesAddress(t *testing.T) {
	privateKey, err := ecdsa.GenerateKey(crypto.S256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	var receivedToken string
	var receivedBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		receivedToken = strings.TrimPrefix(request.Header.Get("Authorization"), "Bearer ")
		body, _ := io.ReadAll(request.Body)
		_ = json.Unmarshal(body, &receivedBody)
		_, _ = writer.Write([]byte(`{"signature":"0x` + common.Bytes2Hex(signForTest(privateKey, [32]byte{7})) + `"}`))
	}))
	defer server.Close()
	api, err := NewVaultCompatibleAPI(server.URL, func() (string, error) { return "tok-123", nil }, testGatewayForServer(t, server.URL))
	if err != nil {
		t.Fatal(err)
	}
	account := testCloudAccount(privateKey, api.Reference())
	signer, err := NewCloudSigner(api, fakeAccountLookup{account: account}, fakeApprovalVerifier{}, fakeApprovalVerifier{})
	if err != nil {
		t.Fatal(err)
	}
	digest := [32]byte{7}
	result, err := signer.Sign(context.Background(), wallet.CapabilityHandle{}, wallet.SoftwareSigningRequest{
		AccountID: account.AccountID, Purpose: wallet.SigningPurposeTransaction, ChainID: 1,
		Digest: digest, ApprovalID: "51111111-1111-4111-8111-111111111111",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Signature) != 65 {
		t.Fatalf("unexpected signature size: %d", len(result.Signature))
	}
	recovered, err := crypto.SigToPub(digest[:], result.Signature)
	if err != nil {
		t.Fatal(err)
	}
	if crypto.PubkeyToAddress(*recovered) != common.HexToAddress(account.Address) {
		t.Fatal("cloud signer returned a signature from the wrong key")
	}
	if receivedToken != "tok-123" {
		t.Fatalf("token was not forwarded: %q", receivedToken)
	}
	if receivedBody["account_id"] != account.AccountID || receivedBody["chain_id"] != float64(1) || receivedBody["digest"] != "0x0700000000000000000000000000000000000000000000000000000000000000" {
		t.Fatalf("unexpected remote request: %+v", receivedBody)
	}
}

func TestCloudSignerRejectsForeignSignatureAndDeniedApproval(t *testing.T) {
	ownerKey, err := ecdsa.GenerateKey(crypto.S256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	foreignKey, err := ecdsa.GenerateKey(crypto.S256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = writer.Write([]byte(`{"signature":"0x` + common.Bytes2Hex(signForTest(foreignKey, [32]byte{7})) + `"}`))
	}))
	defer server.Close()
	api, err := NewVaultCompatibleAPI(server.URL, func() (string, error) { return "tok", nil }, testGatewayForServer(t, server.URL))
	if err != nil {
		t.Fatal(err)
	}
	account := testCloudAccount(ownerKey, api.Reference())
	signer, err := NewCloudSigner(api, fakeAccountLookup{account: account}, fakeApprovalVerifier{}, fakeApprovalVerifier{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := signer.Sign(context.Background(), wallet.CapabilityHandle{}, wallet.SoftwareSigningRequest{
		AccountID: account.AccountID, Purpose: wallet.SigningPurposeTransaction, ChainID: 1,
		Digest: [32]byte{7}, ApprovalID: "51111111-1111-4111-8111-111111111111",
	}); err == nil {
		t.Fatal("foreign signature was accepted")
	}
	deniedSigner, err := NewCloudSigner(api, fakeAccountLookup{account: account},
		fakeApprovalVerifier{requireError: wallet.ErrCapabilityDenied}, fakeApprovalVerifier{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := deniedSigner.Sign(context.Background(), wallet.CapabilityHandle{}, wallet.SoftwareSigningRequest{
		AccountID: account.AccountID, Purpose: wallet.SigningPurposeTransaction, ChainID: 1,
		Digest: [32]byte{7}, ApprovalID: "51111111-1111-4111-8111-111111111111",
	}); err == nil {
		t.Fatal("signing without approval was accepted")
	}
	// Non-cloud accounts are rejected before any remote call.
	software := testCloudAccount(ownerKey)
	software.SignerKind = wallet.SignerKindSoftware
	softwareSigner, err := NewCloudSigner(api, fakeAccountLookup{account: software}, fakeApprovalVerifier{}, fakeApprovalVerifier{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := softwareSigner.Sign(context.Background(), wallet.CapabilityHandle{}, wallet.SoftwareSigningRequest{
		AccountID: software.AccountID, Purpose: wallet.SigningPurposeTransaction, ChainID: 1,
		Digest: [32]byte{7}, ApprovalID: "51111111-1111-4111-8111-111111111111",
	}); err == nil {
		t.Fatal("software account was signed by the cloud signer")
	}
}

func TestSignatureVerificationRejectsMalleableValues(t *testing.T) {
	key, err := ecdsa.GenerateKey(crypto.S256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	digest := [32]byte{7}
	signature := signForTest(key, digest)
	expected := crypto.PubkeyToAddress(key.PublicKey)
	if err := verifyECDSASignature(expected, digest, signature); err != nil {
		t.Fatal(err)
	}
	highS := new(big.Int).Sub(crypto.S256().Params().N, new(big.Int).SetBytes(signature[32:64]))
	highSignature := append([]byte(nil), signature...)
	highS.FillBytes(highSignature[32:64])
	highSignature[64] ^= 1
	if err := verifyECDSASignature(expected, digest, highSignature); err == nil {
		t.Fatal("high-S signature was accepted")
	}
	zeroR := append([]byte(nil), signature...)
	clear(zeroR[:32])
	if err := verifyECDSASignature(expected, digest, zeroR); err == nil {
		t.Fatal("zero-r signature was accepted")
	}
}

func TestSafeMessageDigestAndSignatureComposition(t *testing.T) {
	safe := common.HexToAddress("0xCcCCccccCCCCcCCCCCCcCcCccCcCCCcCcccccccC")
	owner := common.HexToAddress("0x1111111111111111111111111111111111111111")
	messageHash := crypto.Keccak256Hash([]byte("bloco safe tx"))
	digest, err := SafeMessageDigest(safe, 1, messageHash)
	if err != nil {
		t.Fatal(err)
	}
	if digest == ([32]byte{}) {
		t.Fatal("safe digest is empty")
	}
	// Golden pin: the digest must never change for this binding. The value
	// is frozen from the Safe EIP-712 scheme; any change here is a breaking
	// compatibility failure.
	if common.BytesToHash(digest[:]).Hex() != "0x2a6a2777af314d84e6e802a8489938329641e864c52c64e8fb7c8ba396357c0f" {
		t.Fatalf("SafeMessage digest changed: %s", common.BytesToHash(digest[:]).Hex())
	}
	// The digest is deterministic and bound to the safe and chain.
	otherChain, err := SafeMessageDigest(safe, 5, messageHash)
	if err != nil {
		t.Fatal(err)
	}
	if otherChain == digest {
		t.Fatal("safe digest ignored the chain")
	}
	otherSafe, err := SafeMessageDigest(common.HexToAddress("0x2222222222222222222222222222222222222222"), 1, messageHash)
	if err != nil {
		t.Fatal(err)
	}
	if otherSafe == digest {
		t.Fatal("safe digest ignored the safe address")
	}
	contractSignature := bytes.Repeat([]byte{0xAB}, 65)
	composed, err := ComposeSafeContractSignature(owner, contractSignature)
	if err != nil {
		t.Fatal(err)
	}
	if len(composed) != 193 {
		t.Fatalf("unexpected composed safe signature size: %d", len(composed))
	}
	if common.BytesToAddress(composed[12:32]) != owner || new(big.Int).SetBytes(composed[32:64]).Uint64() != 65 || composed[64] != 0 {
		t.Fatalf("unexpected static contract signature: %x", composed[:65])
	}
	if new(big.Int).SetBytes(composed[65:97]).Uint64() != 65 || !bytes.Equal(composed[97:162], contractSignature) {
		t.Fatalf("unexpected dynamic contract signature: %x", composed[65:])
	}
	// execTransaction calldata binds the target, value, data, and signatures.
	calldata, err := EncodeExecTransaction(safe, big.NewInt(1_000_000), messageHash[:], composed)
	if err != nil {
		t.Fatal(err)
	}
	if len(calldata) < 4 {
		t.Fatalf("empty safe calldata")
	}
	huge := new(big.Int).Lsh(big.NewInt(1), 256)
	invalidTransaction := SafeTransaction{
		To: safe, Value: huge, SafeTxGas: big.NewInt(0), BaseGas: big.NewInt(0),
		GasPrice: big.NewInt(0), Nonce: big.NewInt(0),
	}
	if _, err := SafeTransactionDigest(safe, 1, invalidTransaction); err == nil {
		t.Fatal("257-bit Safe value was accepted by the digest")
	}
	if _, err := EncodeSafeExecTransaction(invalidTransaction, []byte{1}); err == nil {
		t.Fatal("257-bit Safe value was accepted by the encoder")
	}
}
