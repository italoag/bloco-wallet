package signer

import (
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"errors"
	"testing"

	"blocowallet/internal/wallet"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// fakeTrezorDevice models the firmware contract: it holds the derived key
// for the path, reports features like the Initialize message, and returns
// signatures in the firmware v encoding.
type fakeTrezorDevice struct {
	key            *ecdsa.PrivateKey
	path           string
	features       TrezorFeatures
	signatureV     byte
	foreignKey     bool
	locked         bool
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
	if device.locked {
		return nil, ErrTrezorLocked
	}
	return crypto.CompressPubkey(&device.key.PublicKey), nil
}

func (device *fakeTrezorDevice) EthereumSignTypedMessage(_ context.Context, derivationPath string, messageHash [32]byte) ([]byte, error) {
	device.signCalls++
	if device.path != "" && derivationPath != device.path {
		return nil, errors.New("path mismatch")
	}
	if device.locked {
		return nil, ErrTrezorLocked
	}
	key := device.key
	if device.foreignKey {
		key, _ = ecdsa.GenerateKey(crypto.S256(), rand.Reader)
	}
	signature, err := crypto.Sign(messageHash[:], key)
	if err != nil {
		return nil, err
	}
	// The firmware reports the recovery id in the 27/28 encoding; convert
	// the natural 0/1 encoding to the firmware form.
	if device.signatureV == 27 || device.signatureV == 28 {
		signature[64] += 27
	}
	return signature, nil
}

func testHardwareAccount(key *ecdsa.PrivateKey) *wallet.Account {
	return &wallet.Account{
		AccountID: "11111111-1111-4111-8111-111111111111",
		Name:      "trezor", Address: crypto.PubkeyToAddress(key.PublicKey).Hex(),
		SignerKind: wallet.SignerKindHardware, SignerReference: "trezor:v1:m/44'/60'/0'/0/0",
		State: wallet.AccountStateActive,
	}
}

func TestTrezorSignerSignsApprovedDigestWithFirmwareV(t *testing.T) {
	privateKey, err := ecdsa.GenerateKey(crypto.S256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	device := &fakeTrezorDevice{
		key: privateKey, path: "m/44'/60'/0'/0/0", signatureV: 28,
		features: TrezorFeatures{Model: "T", Version: "2.6.4", Initialized: true},
	}
	signer, err := NewTrezorSigner(device, fakeAccountLookup{account: testHardwareAccount(privateKey)}, fakeApprovalVerifier{}, fakeApprovalVerifier{})
	if err != nil {
		t.Fatal(err)
	}
	digest := [32]byte{7}
	result, err := signer.Sign(context.Background(), wallet.CapabilityHandle{}, wallet.SoftwareSigningRequest{
		AccountID: "11111111-1111-4111-8111-111111111111",
		Purpose:   wallet.SigningPurposeMessage, MessageScheme: wallet.MessageSigningEIP712, ChainID: 1,
		Digest: digest, IntentHash: digest, ApprovalID: "51111111-1111-4111-8111-111111111111",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Signature) != 65 || (result.Signature[64] != 0 && result.Signature[64] != 1) {
		t.Fatalf("firmware v encoding was not normalized: %x", result.Signature)
	}
	recovered, err := crypto.SigToPub(digest[:], result.Signature)
	if err != nil {
		t.Fatal(err)
	}
	if crypto.PubkeyToAddress(*recovered) != common.HexToAddress(testHardwareAccount(privateKey).Address) {
		t.Fatal("trezor signer returned a signature from the wrong key")
	}
}

func TestTrezorSignerFailClosed(t *testing.T) {
	privateKey, err := ecdsa.GenerateKey(crypto.S256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	account := testHardwareAccount(privateKey)
	request := wallet.SoftwareSigningRequest{
		AccountID: account.AccountID, Purpose: wallet.SigningPurposeMessage,
		MessageScheme: wallet.MessageSigningEIP191Personal, ChainID: 1,
		Digest: [32]byte{9}, IntentHash: [32]byte{9}, ApprovalID: "51111111-1111-4111-8111-111111111111",
	}
	// Denied approval.
	deniedSigner, err := NewTrezorSigner(&fakeTrezorDevice{key: privateKey, features: TrezorFeatures{Initialized: true}},
		fakeAccountLookup{account: account}, fakeApprovalVerifier{requireError: wallet.ErrCapabilityDenied}, fakeApprovalVerifier{requireError: wallet.ErrCapabilityDenied})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := deniedSigner.Sign(context.Background(), wallet.CapabilityHandle{}, request); err == nil {
		t.Fatal("signing without approval was accepted")
	}
	// Locked device.
	lockedSigner, err := NewTrezorSigner(&fakeTrezorDevice{key: privateKey, locked: true, features: TrezorFeatures{Initialized: true, PinProtection: true}},
		fakeAccountLookup{account: account}, fakeApprovalVerifier{}, fakeApprovalVerifier{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lockedSigner.Sign(context.Background(), wallet.CapabilityHandle{}, request); !errors.Is(err, ErrTrezorLocked) {
		t.Fatalf("locked device was accepted: %v", err)
	}
	// Uninitialized device.
	uninitializedSigner, err := NewTrezorSigner(&fakeTrezorDevice{key: privateKey, features: TrezorFeatures{Initialized: false}},
		fakeAccountLookup{account: account}, fakeApprovalVerifier{}, fakeApprovalVerifier{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := uninitializedSigner.Sign(context.Background(), wallet.CapabilityHandle{}, request); err == nil {
		t.Fatal("uninitialized device signed")
	}
	// Foreign signature rejected.
	foreignSigner, err := NewTrezorSigner(&fakeTrezorDevice{key: privateKey, foreignKey: true, features: TrezorFeatures{Initialized: true}},
		fakeAccountLookup{account: account}, fakeApprovalVerifier{}, fakeApprovalVerifier{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := foreignSigner.Sign(context.Background(), wallet.CapabilityHandle{}, request); !errors.Is(err, ErrTrezorSignature) {
		t.Fatalf("foreign signature was accepted: %v", err)
	}
	// Non-hardware accounts and bad references are rejected.
	software := testHardwareAccount(privateKey)
	software.SignerKind = wallet.SignerKindSoftware
	softwareSigner, err := NewTrezorSigner(&fakeTrezorDevice{key: privateKey, features: TrezorFeatures{Initialized: true}},
		fakeAccountLookup{account: software}, fakeApprovalVerifier{}, fakeApprovalVerifier{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := softwareSigner.Sign(context.Background(), wallet.CapabilityHandle{}, request); err == nil {
		t.Fatal("software account was signed by the trezor signer")
	}
	badRef := testHardwareAccount(privateKey)
	badRef.SignerReference = "ledger:v1:m/44'/60'/0'/0/0"
	badRefSigner, err := NewTrezorSigner(&fakeTrezorDevice{key: privateKey, features: TrezorFeatures{Initialized: true}},
		fakeAccountLookup{account: badRef}, fakeApprovalVerifier{}, fakeApprovalVerifier{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := badRefSigner.Sign(context.Background(), wallet.CapabilityHandle{}, request); err == nil {
		t.Fatal("non-trezor reference was accepted")
	}
	badPath := testHardwareAccount(privateKey)
	badPath.SignerReference = "trezor:v1:not/a/path"
	badPathSigner, err := NewTrezorSigner(&fakeTrezorDevice{key: privateKey, features: TrezorFeatures{Initialized: true}},
		fakeAccountLookup{account: badPath}, fakeApprovalVerifier{}, fakeApprovalVerifier{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := badPathSigner.Sign(context.Background(), wallet.CapabilityHandle{}, request); err == nil {
		t.Fatal("invalid derivation path was accepted")
	}
}
