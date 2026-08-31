package signer

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"net"
	"os"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
)

// TestTrezorCodecAgainstHandBuiltMessages pins the protobuf codec against
// messages built byte by byte from the Trezor protobuf schema.
func TestTrezorCodecAgainstHandBuiltMessages(t *testing.T) {
	// Features: vendor(1) absent, major=2 (tag 0x10), minor=6 (0x18),
	// patch=4 (0x20), model="T" (0x3a len 1), initialized=1 (0x58),
	// pin_protection=0 (0x70), passphrase_protection=1 (0x78).
	features := []byte{
		0x10, 0x02,
		0x18, 0x06,
		0x20, 0x04,
		0x3a, 0x01, 'T',
		0x58, 0x01,
		0x70, 0x00,
		0x78, 0x01,
	}
	parsed, err := parseFeatures(features)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Model != "T" || parsed.Version != "2.6.4" || !parsed.Initialized || parsed.PinProtection || !parsed.PassphraseProtection {
		t.Fatalf("unexpected features: %+v", parsed)
	}

	// EthereumPublicKey: node(1) = HDNodeType{ public_key(6) = 33 bytes }.
	publicKey := bytes.Repeat([]byte{0x02}, 33)
	node := []byte{0x32, 0x21}
	node = append(node, publicKey...)
	response := []byte{0x0a, byte(len(node))}
	response = append(response, node...)
	extracted, err := parseEthereumPublicKey(response)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(extracted, publicKey) {
		t.Fatal("public key mismatch")
	}

	// EthereumTypedMessageSignature: signature(2) = 65 bytes.
	signature := bytes.Repeat([]byte{0xAB}, 65)
	typedResponse := []byte{0x12, 0x41}
	typedResponse = append(typedResponse, signature...)
	decoded, err := parseEthereumTypedMessageSignature(typedResponse)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(decoded, signature) {
		t.Fatal("signature mismatch")
	}
	if _, err := parseEthereumTypedMessageSignature([]byte{0x0a, 0x01, 0x00}); err == nil {
		t.Fatal("signature-less response was accepted")
	}

	// Derivation path conversion follows BIP-32 hardening
	// (m/44'/60'/0'/0/0 → hardened 44, 60, account 0, change 0, index 0).
	numbers, err := derivationPathToNumbers("m/44'/60'/0'/0/0")
	if err != nil {
		t.Fatal(err)
	}
	expected := []uint32{0x8000002c, 0x8000003c, 0x80000000, 0, 0}
	if len(numbers) != len(expected) {
		t.Fatalf("path depth: %d", len(numbers))
	}
	for index := range expected {
		if numbers[index] != expected[index] {
			t.Fatalf("path component %d: %#x", index, numbers[index])
		}
	}
	// The request encoding for EthereumSignTypedMessage carries the packed
	// path and the message hash.
	request := encodeFields([][]byte{
		encodePackedVarints(numbers),
		nil,
		nil,
		bytes.Repeat([]byte{0xEE}, 32),
	})
	// packed path: 5 components (3 hardened 5-byte varints + 2 single) = 17
	if len(request) != 1+1+17+1+1+32 {
		t.Fatalf("unexpected request length: %d", len(request))
	}
	if request[0] != 0x0a || request[1] != 0x11 {
		t.Fatalf("unexpected address_n field: %x", request[:2])
	}
	hashOffset := len(request) - 32
	if request[hashOffset-2] != 0x22 || request[hashOffset-1] != 0x20 {
		t.Fatalf("unexpected message_hash field: %x", request[hashOffset-2:])
	}
}

// TestTrezorUDPDeviceAgainstMockBridge exercises the framing against a UDP
// responder that speaks the trezord envelope (no firmware required).
func TestTrezorUDPDeviceAgainstMockBridge(t *testing.T) {
	address := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)}
	bridge, err := net.ListenUDP("udp", address)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = bridge.Close() }()
	deviceConn, err := net.DialUDP("udp", nil, bridge.LocalAddr().(*net.UDPAddr))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = deviceConn.Close() }()
	go func() {
		buffer := make([]byte, trezorMaxDatagram)
		count, peer, readErr := bridge.ReadFromUDP(buffer)
		if readErr != nil {
			return
		}
		if count < 40 || string(buffer[:2]) != "##" || string(buffer[34:36]) != "##" {
			return
		}
		session := append([]byte(nil), buffer[2:34]...)
		features := []byte{0x10, 0x02, 0x18, 0x06, 0x20, 0x04, 0x3a, 0x01, 'T', 0x58, 0x01, 0x70, 0x00, 0x78, 0x01}
		message := []byte{0x00, 0x11} // Features (17)
		message = append(message, features...)
		response := []byte("##")
		response = append(response, session...)
		response = append(response, []byte("##")...)
		var length [4]byte
		binary.BigEndian.PutUint32(length[:], uint32(len(message)))
		response = append(response, length[:]...)
		response = append(response, message...)
		_, _ = bridge.WriteToUDP(response, peer)
	}()
	device := &UDPDevice{conn: deviceConn, timeout: 5 * time.Second}
	features, err := device.Initialize(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if features.Model != "T" || features.Version != "2.6.4" || !features.Initialized {
		t.Fatalf("unexpected features: %+v", features)
	}
	if len(device.session) != trezorSessionSize {
		t.Fatalf("session was not assigned: %d", len(device.session))
	}
}

// TestTrezorEmulatorIntegration requires a running Trezor emulator bridge
// (trezor-user-env / trezord -e 21324) and is skipped otherwise. It drives
// the real firmware: initialize, public key, and typed message signing.
func TestTrezorEmulatorIntegration(t *testing.T) {
	if os.Getenv("BLOCO_WALLET_TREZOR_EMULATOR") == "" {
		t.Skip("BLOCO_WALLET_TREZOR_EMULATOR not set; skipping emulator integration")
	}
	device, err := NewUDPDevice(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = device.Close() }()
	features, err := device.Initialize(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !features.Initialized {
		t.Skip("emulator device is not initialized; skipping signing")
	}
	publicKey, err := device.EthereumGetPublicKey(context.Background(), "m/44'/60'/0'/0/0")
	if err != nil {
		t.Fatal(err)
	}
	if len(publicKey) != 33 {
		t.Fatalf("unexpected public key size: %d", len(publicKey))
	}
	digest := sha256.Sum256([]byte("bloco emulator vector"))
	signature, err := device.EthereumSignTypedMessage(context.Background(), "m/44'/60'/0'/0/0", digest)
	if err != nil {
		t.Fatal(err)
	}
	if len(signature) != 65 {
		t.Fatalf("unexpected signature size: %d", len(signature))
	}
	if err := verifySignatureAgainstPublicKey(publicKey, digest, signature); err != nil {
		t.Fatal(err)
	}
}

// verifySignatureAgainstPublicKey checks the emulator signature against the
// returned public key without an account binding.
func verifySignatureAgainstPublicKey(compressed []byte, digest [32]byte, signature []byte) error {
	if len(signature) != 65 || (signature[64] != 0 && signature[64] != 1) {
		return errSignatureShape
	}
	pub, err := crypto.DecompressPubkey(compressed)
	if err != nil {
		return err
	}
	recovered, err := crypto.SigToPub(digest[:], signature)
	if err != nil {
		return err
	}
	if !bytes.Equal(crypto.CompressPubkey(pub), crypto.CompressPubkey(recovered)) {
		return errSignatureMismatch
	}
	return nil
}

var (
	errSignatureShape    = errors.New("signature shape")
	errSignatureMismatch = errors.New("signature mismatch")
)
