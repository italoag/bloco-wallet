package signer

import (
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"encoding/json"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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

func testCloudAccount(privateKey *ecdsa.PrivateKey) *wallet.Account {
	return &wallet.Account{
		AccountID: "11111111-1111-4111-8111-111111111111",
		Name:      "cloud", Address: crypto.PubkeyToAddress(privateKey.PublicKey).Hex(),
		SignerKind: wallet.SignerKindCloud, SignerReference: "cloud:v1:https://vault.example",
		State: wallet.AccountStateActive,
	}
}

func signForTest(privateKey *ecdsa.PrivateKey, digest [32]byte) []byte {
	signature, err := crypto.Sign(digest[:], privateKey)
	if err != nil {
		panic(err)
	}
	return signature
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
	api, err := NewVaultCompatibleAPI(server.URL, func() (string, error) { return "tok-123", nil })
	if err != nil {
		t.Fatal(err)
	}
	account := testCloudAccount(privateKey)
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
	api, err := NewVaultCompatibleAPI(server.URL, func() (string, error) { return "tok", nil })
	if err != nil {
		t.Fatal(err)
	}
	account := testCloudAccount(ownerKey)
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
	// A signature with v=27 normalizes to the Safe contract format.
	ownerKey, err := ecdsa.GenerateKey(crypto.S256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	var signature [65]byte
	copy(signature[:], signForTest(ownerKey, digest)[:64])
	signature[64] = 27
	composed := ComposeSafeSignature(signature, owner)
	if len(composed) != 86 {
		t.Fatalf("unexpected composed safe signature size: %d", len(composed))
	}
	if composed[64] != 0 || composed[65] != 0x01 || common.BytesToAddress(composed[66:]) != owner {
		t.Fatalf("unexpected composed safe signature: %x", composed)
	}
	// execTransaction calldata binds the target, value, data, and signatures.
	calldata, err := EncodeExecTransaction(safe, big.NewInt(1_000_000), messageHash[:], composed)
	if err != nil {
		t.Fatal(err)
	}
	if len(calldata) < 4 {
		t.Fatalf("empty safe calldata")
	}
}
