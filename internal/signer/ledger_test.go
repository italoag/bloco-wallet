package signer

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// ledgerMockTransport responds to APDUs like the Ethereum app: it derives
// the key from the path (single path supported) and returns canonical
// response payloads.
type ledgerMockTransport struct {
	key           *ecdsa.PrivateKey
	deny          bool
	malformed     bool
	received      []byte
	exchangeCalls int
}

func (transport *ledgerMockTransport) Exchange(_ context.Context, cla, ins, p1, p2 byte, data []byte) ([]byte, uint16, error) {
	transport.exchangeCalls++
	transport.received = append(append([]byte(nil), data...), 0)
	if cla != ledgerCLA {
		return nil, 0x6e00, nil
	}
	if transport.deny {
		return nil, ledgerSWDeny, nil
	}
	switch ins {
	case ledgerINSGetPublicKey:
		if transport.malformed {
			return []byte{0x01}, ledgerSWOK, nil
		}
		uncompressed := crypto.FromECDSAPub(&transport.key.PublicKey)
		address := crypto.PubkeyToAddress(transport.key.PublicKey)
		response := make([]byte, 0, 65+1+20)
		response = append(response, uncompressed...)
		response = append(response, 20)
		response = append(response, address.Bytes()...)
		return response, ledgerSWOK, nil
	case ledgerINSSignEIP712, ledgerINSSignPersonalMessage:
		if transport.malformed {
			return []byte{0xAB, 0xCD}, ledgerSWOK, nil
		}
		var digest [32]byte
		if ins == ledgerINSSignEIP712 {
			if len(data) < 1+64 {
				return nil, 0x6a80, nil
			}
			copy(digest[:], data[len(data)-32:])
		} else {
			message := data[1+4*int(data[0])+2:]
			prefix := []byte("\x19Ethereum Signed Message:\n")
			prefix = append(prefix, []byte(itoa2(len(message)))...)
			copy(digest[:], crypto.Keccak256(prefix, message))
		}
		signature, err := crypto.Sign(digest[:], transport.key)
		if err != nil {
			return nil, 0x6a80, nil
		}
		signature[64] += 27 // Ledger v encoding
		return signature, ledgerSWOK, nil
	default:
		return nil, 0x6d00, nil
	}
}

func itoa2(value int) string {
	if value == 0 {
		return "0"
	}
	var digits []byte
	for value > 0 {
		digits = append([]byte{byte('0' + value%10)}, digits...)
		value /= 10
	}
	return string(digits)
}

func TestLedgerDeviceGetPublicKeyAgainstSpecFormat(t *testing.T) {
	privateKey, err := ecdsa.GenerateKey(crypto.S256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	transport := &ledgerMockTransport{key: privateKey}
	device, err := NewLedgerDevice(transport)
	if err != nil {
		t.Fatal(err)
	}
	result, err := device.GetPublicKey(context.Background(), "m/44'/60'/0'/0/0")
	if err != nil {
		t.Fatal(err)
	}
	if result.Address != crypto.PubkeyToAddress(privateKey.PublicKey) {
		t.Fatalf("address mismatch: %s", result.Address.Hex())
	}
	if len(result.PublicKey) != 65 {
		t.Fatalf("unexpected public key size: %d", len(result.PublicKey))
	}
	// The request carries the path: length byte + 5 hardened numbers.
	if len(transport.received) != 1+1+20 {
		t.Fatalf("unexpected request size: %d", len(transport.received))
	}
	if transport.received[0] != 5 {
		t.Fatalf("unexpected path length: %d", transport.received[0])
	}
	firstComponent := binary.BigEndian.Uint32(transport.received[1:5])
	if firstComponent != 0x8000002c {
		t.Fatalf("unexpected first path component: %#x", firstComponent)
	}
}

func TestLedgerDeviceSignTypedMessageUsesBothHashes(t *testing.T) {
	privateKey, err := ecdsa.GenerateKey(crypto.S256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	transport := &ledgerMockTransport{key: privateKey}
	device, err := NewLedgerDevice(transport)
	if err != nil {
		t.Fatal(err)
	}
	var domain, message [32]byte
	copy(domain[:], bytes.Repeat([]byte{0x11}, 32))
	copy(message[:], bytes.Repeat([]byte{0x22}, 32))
	signature, err := device.SignTypedMessage(context.Background(), "m/44'/60'/0'/0/0", domain, message)
	if err != nil {
		t.Fatal(err)
	}
	if len(signature) != 65 || (signature[64] != 27 && signature[64] != 28) {
		t.Fatalf("unexpected signature: %x", signature)
	}
	// The request ends with domain || message and the metamask flag.
	request := transport.received
	if len(request) != 1+20+1+64+1 {
		t.Fatalf("unexpected request size: %d", len(request))
	}
	if request[21] != 0x00 {
		t.Fatalf("metamask flag: %x", request[21])
	}
	if !bytes.Equal(request[22:54], domain[:]) || !bytes.Equal(request[54:86], message[:]) {
		t.Fatal("domain/message hashes were not forwarded in order")
	}
	// The signature recovers the device address with the Ledger v encoding.
	normalized := append([]byte(nil), signature...)
	normalized[64] -= 27
	recovered, err := crypto.SigToPub(message[:], normalized)
	if err != nil {
		t.Fatal(err)
	}
	if crypto.PubkeyToAddress(*recovered) != crypto.PubkeyToAddress(privateKey.PublicKey) {
		t.Fatal("signature does not recover the device key")
	}
}

func TestLedgerDeviceSignPersonalMessageAppliesEIP191Prefix(t *testing.T) {
	privateKey, err := ecdsa.GenerateKey(crypto.S256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	transport := &ledgerMockTransport{key: privateKey}
	device, err := NewLedgerDevice(transport)
	if err != nil {
		t.Fatal(err)
	}
	message := []byte("bloco personal vector")
	signature, err := device.SignPersonalMessage(context.Background(), "m/44'/60'/0'/0/0", message)
	if err != nil {
		t.Fatal(err)
	}
	if len(signature) != 65 {
		t.Fatalf("unexpected signature size: %d", len(signature))
	}
	digest := crypto.Keccak256Hash([]byte("\x19Ethereum Signed Message:\n"+itoa2(len(message))), message)
	normalized := append([]byte(nil), signature...)
	normalized[64] -= 27
	recovered, err := crypto.SigToPub(digest[:], normalized)
	if err != nil {
		t.Fatal(err)
	}
	if crypto.PubkeyToAddress(*recovered) != crypto.PubkeyToAddress(privateKey.PublicKey) {
		t.Fatal("personal signature does not recover the device key")
	}
}

func TestLedgerDeviceFailClosed(t *testing.T) {
	privateKey, err := ecdsa.GenerateKey(crypto.S256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	denied, err := NewLedgerDevice(&ledgerMockTransport{key: privateKey, deny: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := denied.GetPublicKey(context.Background(), "m/44'/60'/0'/0/0"); !errors.Is(err, ErrLedgerDenied) {
		t.Fatalf("denied request was not surfaced: %v", err)
	}
	malformed, err := NewLedgerDevice(&ledgerMockTransport{key: privateKey, malformed: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := malformed.GetPublicKey(context.Background(), "m/44'/60'/0'/0/0"); !errors.Is(err, ErrLedgerTransport) {
		t.Fatalf("malformed response was not rejected: %v", err)
	}
	if _, err := malformed.SignTypedMessage(context.Background(), "m/44'/60'/0'/0/0", [32]byte{1}, [32]byte{2}); !errors.Is(err, ErrLedgerTransport) {
		t.Fatalf("malformed signature was not rejected: %v", err)
	}
	if _, err := denied.GetPublicKey(context.Background(), "bad"); err == nil {
		t.Fatal("invalid path was accepted")
	}
	if _, err := denied.SignPersonalMessage(context.Background(), "m/44'/60'/0'/0/0", nil); err == nil {
		t.Fatal("empty message was accepted")
	}
}

// speculosHTTPTransport talks to the Speculos HTTP API (/apdu endpoint).
type speculosHTTPTransport struct {
	baseURL string
	client  *http.Client
}

func newSpeculosHTTPTransport(baseURL string) *speculosHTTPTransport {
	return &speculosHTTPTransport{
		baseURL: strings.TrimSuffix(baseURL, "/"),
		client:  &http.Client{Timeout: 30 * time.Second},
	}
}

func (transport *speculosHTTPTransport) Exchange(ctx context.Context, cla, ins, p1, p2 byte, data []byte) ([]byte, uint16, error) {
	payload := []byte{cla, ins, p1, p2, byte(len(data))}
	payload = append(payload, data...)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, transport.baseURL+"/apdu",
		strings.NewReader("0x"+hex.EncodeToString(payload)))
	if err != nil {
		return nil, 0, err
	}
	request.Header.Set("Content-Type", "text/plain")
	response, err := transport.client.Do(request)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return nil, 0, err
	}
	decoded, err := hex.DecodeString(strings.TrimPrefix(strings.TrimSpace(string(body)), "0x"))
	if err != nil || len(decoded) < 2 {
		return nil, 0, ErrLedgerTransport
	}
	status := uint16(decoded[len(decoded)-2])<<8 | uint16(decoded[len(decoded)-1])
	return decoded[:len(decoded)-2], status, nil
}

// TestLedgerSpeculosIntegration requires a running Speculos container with
// the Ethereum app; skipped unless BLOCO_WALLET_SPECULOS_URL is set.
func TestLedgerSpeculosIntegration(t *testing.T) {
	speculosURL := os.Getenv("BLOCO_WALLET_SPECULOS_URL")
	if speculosURL == "" {
		t.Skip("BLOCO_WALLET_SPECULOS_URL not set; skipping Speculos integration")
	}
	device, err := NewLedgerDevice(newSpeculosHTTPTransport(speculosURL))
	if err != nil {
		t.Fatal(err)
	}
	result, err := device.GetPublicKey(context.Background(), "m/44'/60'/0'/0/0")
	if err != nil {
		t.Fatal(err)
	}
	if result.Address == (common.Address{}) {
		t.Fatal("speculos returned the zero address")
	}
	var domain, message [32]byte
	copy(domain[:], bytes.Repeat([]byte{0x11}, 32))
	copy(message[:], bytes.Repeat([]byte{0x22}, 32))
	signature, err := device.SignTypedMessage(context.Background(), "m/44'/60'/0'/0/0", domain, message)
	if err != nil {
		t.Fatal(err)
	}
	if len(signature) != 65 {
		t.Fatalf("unexpected signature size: %d", len(signature))
	}
}
