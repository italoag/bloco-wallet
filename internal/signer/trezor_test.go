package signer

import (
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"errors"
	"testing"

	"blocowallet/internal/wallet"

	gethaccounts "github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

type fakeTrezorDevice struct {
	key            *ecdsa.PrivateKey
	path           string
	features       TrezorFeatures
	foreignKey     bool
	initCalls      int
	signCalls      int
	publicKeyCalls int
}

func (device *fakeTrezorDevice) Initialize(context.Context) (TrezorFeatures, error) {
	device.initCalls++
	return device.features, nil
}

func (device *fakeTrezorDevice) EthereumGetPublicKey(_ context.Context, derivationPath string) ([]byte, error) {
	device.publicKeyCalls++
	if device.path != "" && derivationPath != device.path {
		return nil, errors.New("path mismatch")
	}
	return crypto.CompressPubkey(&device.key.PublicKey), nil
}

func (device *fakeTrezorDevice) EthereumSignTypedHash(_ context.Context, derivationPath string, domainHash, messageHash [32]byte) ([]byte, error) {
	device.signCalls++
	if device.path != "" && derivationPath != device.path {
		return nil, errors.New("path mismatch")
	}
	key := device.signingKey()
	digest := crypto.Keccak256Hash([]byte{0x19, 0x01}, domainHash[:], messageHash[:])
	return crypto.Sign(digest[:], key)
}

func (device *fakeTrezorDevice) EthereumSignMessage(_ context.Context, derivationPath string, message []byte) ([]byte, error) {
	device.signCalls++
	if device.path != "" && derivationPath != device.path {
		return nil, errors.New("path mismatch")
	}
	return crypto.Sign(gethaccounts.TextHash(message), device.signingKey())
}

func (device *fakeTrezorDevice) signingKey() *ecdsa.PrivateKey {
	if !device.foreignKey {
		return device.key
	}
	key, _ := ecdsa.GenerateKey(crypto.S256(), rand.Reader)
	return key
}

func testHardwareAccount(key *ecdsa.PrivateKey) *wallet.Account {
	return &wallet.Account{
		AccountID: "11111111-1111-4111-8111-111111111111",
		Name:      "trezor", Address: crypto.PubkeyToAddress(key.PublicKey).Hex(),
		SignerKind: wallet.SignerKindHardware, SignerReference: "trezor:v1:m/44'/60'/0'/0/0",
		State: wallet.AccountStateActive,
	}
}

func TestTrezorSignerRequiresStructuredIntent(t *testing.T) {
	key, err := ecdsa.GenerateKey(crypto.S256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	device := &fakeTrezorDevice{key: key, features: TrezorFeatures{Initialized: true}}
	signer, err := NewTrezorSigner(device, fakeAccountLookup{account: testHardwareAccount(key)}, fakeApprovalVerifier{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = signer.Sign(context.Background(), wallet.CapabilityHandle{}, wallet.SoftwareSigningRequest{
		AccountID: testHardwareAccount(key).AccountID, Purpose: wallet.SigningPurposeMessage,
	})
	if !errors.Is(err, ErrHardwareIntentRequired) {
		t.Fatalf("opaque digest was not rejected: %v", err)
	}
	if device.initCalls != 0 || device.signCalls != 0 {
		t.Fatal("device was touched for an opaque digest")
	}
}

func TestTrezorSignerSignsTypedHashAndPersonalMessage(t *testing.T) {
	key, err := ecdsa.GenerateKey(crypto.S256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	account := testHardwareAccount(key)
	device := &fakeTrezorDevice{
		key: key, path: "m/44'/60'/0'/0/0",
		features: TrezorFeatures{Model: "1", Version: "1.14.1", Initialized: true, PinProtection: true},
	}
	signer, err := NewTrezorSigner(device, fakeAccountLookup{account: account}, fakeApprovalVerifier{})
	if err != nil {
		t.Fatal(err)
	}
	var domainHash, messageHash [32]byte
	copy(domainHash[:], crypto.Keccak256([]byte("domain")))
	copy(messageHash[:], crypto.Keccak256([]byte("message")))
	typedResult, err := signer.SignTypedHash(context.Background(), TrezorTypedHashRequest{
		AccountID: account.AccountID, ChainID: 1,
		DomainSeparatorHash: domainHash, MessageHash: messageHash,
		IntentHash: [32]byte{1}, ApprovalID: "51111111-1111-4111-8111-111111111111",
	})
	if err != nil {
		t.Fatal(err)
	}
	typedDigest := crypto.Keccak256Hash([]byte{0x19, 0x01}, domainHash[:], messageHash[:])
	assertRecoveredAddress(t, typedDigest, typedResult.Signature, common.HexToAddress(account.Address))
	if typedResult.MessageScheme != wallet.MessageSigningEIP712 {
		t.Fatalf("unexpected typed scheme: %s", typedResult.MessageScheme)
	}

	message := []byte("bloco personal message")
	personalResult, err := signer.SignPersonalMessage(context.Background(), TrezorPersonalMessageRequest{
		AccountID: account.AccountID, ChainID: 0, Message: message,
		IntentHash: [32]byte{2}, ApprovalID: "61111111-1111-4111-8111-111111111111",
	})
	if err != nil {
		t.Fatal(err)
	}
	var personalDigest [32]byte
	copy(personalDigest[:], gethaccounts.TextHash(message))
	assertRecoveredAddress(t, personalDigest, personalResult.Signature, common.HexToAddress(account.Address))
	if personalResult.MessageScheme != wallet.MessageSigningEIP191Personal {
		t.Fatalf("unexpected personal scheme: %s", personalResult.MessageScheme)
	}
	if device.signCalls != 2 {
		t.Fatalf("unexpected device sign calls: %d", device.signCalls)
	}
}

func TestTrezorSignerFailsClosed(t *testing.T) {
	key, err := ecdsa.GenerateKey(crypto.S256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	account := testHardwareAccount(key)
	request := TrezorPersonalMessageRequest{
		AccountID: account.AccountID, ChainID: 0, Message: []byte("message"),
		IntentHash: [32]byte{9}, ApprovalID: "51111111-1111-4111-8111-111111111111",
	}

	deniedSigner, err := NewTrezorSigner(
		&fakeTrezorDevice{key: key, features: TrezorFeatures{Initialized: true}},
		fakeAccountLookup{account: account}, fakeApprovalVerifier{requireError: wallet.ErrCapabilityDenied},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := deniedSigner.SignPersonalMessage(context.Background(), request); err == nil {
		t.Fatal("signing without approval was accepted")
	}

	chainDevice := &fakeTrezorDevice{key: key, features: TrezorFeatures{Model: "1", Initialized: true}}
	chainSigner, err := NewTrezorSigner(chainDevice, fakeAccountLookup{account: account}, fakeApprovalVerifier{})
	if err != nil {
		t.Fatal(err)
	}
	wrongChain := request
	wrongChain.ChainID = 1
	if _, err := chainSigner.SignPersonalMessage(context.Background(), wrongChain); err == nil {
		t.Fatal("chain-bound personal_sign request was accepted")
	}
	if chainDevice.initCalls != 0 || chainDevice.signCalls != 0 {
		t.Fatal("invalid personal_sign binding reached the Trezor")
	}

	uninitializedSigner, err := NewTrezorSigner(
		&fakeTrezorDevice{key: key, features: TrezorFeatures{Initialized: false}},
		fakeAccountLookup{account: account}, fakeApprovalVerifier{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := uninitializedSigner.SignPersonalMessage(context.Background(), request); err == nil {
		t.Fatal("uninitialized device signed")
	}

	modelTDevice := &fakeTrezorDevice{key: key, features: TrezorFeatures{Model: "T2T1", Version: "2.12.4", Initialized: true}}
	modelTSigner, err := NewTrezorSigner(modelTDevice, fakeAccountLookup{account: account}, fakeApprovalVerifier{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := modelTSigner.SignTypedHash(context.Background(), TrezorTypedHashRequest{
		AccountID: account.AccountID, ChainID: 1, ApprovalID: "71111111-1111-4111-8111-111111111111",
		IntentHash: [32]byte{1},
	}); !errors.Is(err, ErrTrezorTypedHashUnsupported) {
		t.Fatalf("Model T accepted legacy typed-hash signing: %v", err)
	}
	if modelTDevice.signCalls != 0 {
		t.Fatal("Model T was sent a legacy typed-hash request")
	}

	foreignSigner, err := NewTrezorSigner(
		&fakeTrezorDevice{key: key, foreignKey: true, features: TrezorFeatures{Initialized: true}},
		fakeAccountLookup{account: account}, fakeApprovalVerifier{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := foreignSigner.SignPersonalMessage(context.Background(), request); !errors.Is(err, ErrTrezorSignature) {
		t.Fatalf("foreign signature was accepted: %v", err)
	}

	software := testHardwareAccount(key)
	software.SignerKind = wallet.SignerKindSoftware
	softwareSigner, err := NewTrezorSigner(
		&fakeTrezorDevice{key: key, features: TrezorFeatures{Initialized: true}},
		fakeAccountLookup{account: software}, fakeApprovalVerifier{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := softwareSigner.SignPersonalMessage(context.Background(), request); err == nil {
		t.Fatal("software account was signed by the Trezor signer")
	}

	badRef := testHardwareAccount(key)
	badRef.SignerReference = "ledger:v1:m/44'/60'/0'/0/0"
	badRefSigner, err := NewTrezorSigner(
		&fakeTrezorDevice{key: key, features: TrezorFeatures{Initialized: true}},
		fakeAccountLookup{account: badRef}, fakeApprovalVerifier{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := badRefSigner.SignPersonalMessage(context.Background(), request); err == nil {
		t.Fatal("non-Trezor reference was accepted")
	}

	badPath := testHardwareAccount(key)
	badPath.SignerReference = "trezor:v1:not/a/path"
	badPathSigner, err := NewTrezorSigner(
		&fakeTrezorDevice{key: key, features: TrezorFeatures{Initialized: true}},
		fakeAccountLookup{account: badPath}, fakeApprovalVerifier{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := badPathSigner.SignPersonalMessage(context.Background(), request); err == nil {
		t.Fatal("invalid derivation path was accepted")
	}

	if _, err := deniedSigner.SignPersonalMessage(context.Background(), TrezorPersonalMessageRequest{}); err == nil {
		t.Fatal("empty message was accepted")
	}
}

func assertRecoveredAddress(t *testing.T, digest [32]byte, signature []byte, expected common.Address) {
	t.Helper()
	if len(signature) != 65 || (signature[64] != 0 && signature[64] != 1) {
		t.Fatalf("invalid signature shape: %x", signature)
	}
	publicKey, err := crypto.SigToPub(digest[:], signature)
	if err != nil {
		t.Fatal(err)
	}
	if crypto.PubkeyToAddress(*publicKey) != expected {
		t.Fatalf("signature recovered %s, want %s", crypto.PubkeyToAddress(*publicKey).Hex(), expected.Hex())
	}
}
