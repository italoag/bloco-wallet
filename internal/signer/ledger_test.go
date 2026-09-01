package signer

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"blocowallet/internal/wallet"

	gethaccounts "github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

// ledgerMockTransport responds to APDUs like the Ethereum app: it derives
// the key from the path (single path supported) and returns canonical
// response payloads.
type ledgerMockTransport struct {
	key              *ecdsa.PrivateKey
	deny             bool
	malformed        bool
	wrongAddress     bool
	extraResponse    bool
	appConfiguration []byte
	received         []byte
	exchangeCalls    int
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
	case ledgerINSGetAppConfiguration:
		if transport.appConfiguration != nil {
			return append([]byte(nil), transport.appConfiguration...), ledgerSWOK, nil
		}
		return []byte{0x00, 0x01, 0x16, 0x03}, ledgerSWOK, nil
	case ledgerINSGetPublicKey:
		if transport.malformed {
			return []byte{0x01}, ledgerSWOK, nil
		}
		uncompressed := crypto.FromECDSAPub(&transport.key.PublicKey)
		address := crypto.PubkeyToAddress(transport.key.PublicKey)
		if transport.wrongAddress {
			address[0] ^= 0x01
		}
		addressHex := []byte(hex.EncodeToString(address.Bytes()))
		response := make([]byte, 0, 1+65+1+40+1)
		response = append(response, 65)
		response = append(response, uncompressed...)
		response = append(response, 40)
		response = append(response, addressHex...)
		if transport.extraResponse {
			response = append(response, 0)
		}
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
			domainHash := data[len(data)-64 : len(data)-32]
			messageHash := data[len(data)-32:]
			copy(digest[:], crypto.Keccak256([]byte{0x19, 0x01}, domainHash, messageHash))
		} else {
			message := data[1+4*int(data[0])+4:]
			prefix := []byte("\x19Ethereum Signed Message:\n")
			prefix = append(prefix, []byte(itoa2(len(message)))...)
			copy(digest[:], crypto.Keccak256(prefix, message))
		}
		signature, err := crypto.Sign(digest[:], transport.key)
		if err != nil {
			return nil, 0x6a80, nil
		}
		response := make([]byte, 0, 65)
		response = append(response, signature[64]+27)
		response = append(response, signature[:64]...)
		return response, ledgerSWOK, nil
	default:
		return nil, 0x6d00, nil
	}
}

type concurrentLedgerTransport struct {
	delegate APDUTransport
	active   atomic.Int32
	maximum  atomic.Int32
}

func (transport *concurrentLedgerTransport) Exchange(ctx context.Context, cla, ins, p1, p2 byte, data []byte) ([]byte, uint16, error) {
	active := transport.active.Add(1)
	defer transport.active.Add(-1)
	for {
		maximum := transport.maximum.Load()
		if active <= maximum || transport.maximum.CompareAndSwap(maximum, active) {
			break
		}
	}
	time.Sleep(5 * time.Millisecond)
	return transport.delegate.Exchange(ctx, cla, ins, p1, p2, data)
}

type chunkedLedgerTransport struct {
	key     *ecdsa.PrivateKey
	message []byte
	total   int
	p1      []byte
	sizes   []int
}

func (transport *chunkedLedgerTransport) Exchange(_ context.Context, cla, ins, p1, p2 byte, data []byte) ([]byte, uint16, error) {
	if cla != ledgerCLA || ins != ledgerINSSignPersonalMessage || p2 != 0 {
		return nil, 0x6a80, nil
	}
	transport.p1 = append(transport.p1, p1)
	transport.sizes = append(transport.sizes, len(data))
	switch p1 {
	case 0:
		if len(data) < 1+4*int(data[0])+4 {
			return nil, 0x6a80, nil
		}
		headerEnd := 1 + 4*int(data[0])
		transport.total = int(binary.BigEndian.Uint32(data[headerEnd : headerEnd+4]))
		transport.message = append(transport.message, data[headerEnd+4:]...)
	case 0x80:
		transport.message = append(transport.message, data...)
	default:
		return nil, 0x6a80, nil
	}
	if len(transport.message) < transport.total {
		return nil, ledgerSWOK, nil
	}
	signature, err := crypto.Sign(gethaccounts.TextHash(transport.message), transport.key)
	if err != nil {
		return nil, 0x6a80, nil
	}
	response := append([]byte{signature[64] + 27}, signature[:64]...)
	return response, ledgerSWOK, nil
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
	if len(signature) != 65 || (signature[64] != 0 && signature[64] != 1) {
		t.Fatalf("unexpected signature: %x", signature)
	}
	// The request ends with domain || message (P2=0 selects the v0
	// implementation per Ledger's ethapp.adoc; no flag byte in payload).
	request := transport.received
	if len(request) != 1+20+64+1 {
		t.Fatalf("unexpected request size: %d", len(request))
	}
	if !bytes.Equal(request[21:53], domain[:]) || !bytes.Equal(request[53:85], message[:]) {
		t.Fatal("domain/message hashes were not forwarded in order")
	}
	// The signature recovers the device address over the EIP-712 digest
	// keccak256(0x1901 || domainSeparator || messageHash).
	typedDigest := crypto.Keccak256Hash([]byte{0x19, 0x01}, domain[:], message[:])
	recovered, err := crypto.SigToPub(typedDigest[:], signature)
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
	recovered, err := crypto.SigToPub(digest[:], signature)
	if err != nil {
		t.Fatal(err)
	}
	if crypto.PubkeyToAddress(*recovered) != crypto.PubkeyToAddress(privateKey.PublicKey) {
		t.Fatal("personal signature does not recover the device key")
	}
}

func TestLedgerDeviceStreamsLongPersonalMessage(t *testing.T) {
	key, err := ecdsa.GenerateKey(crypto.S256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	transport := &chunkedLedgerTransport{key: key}
	device, err := NewLedgerDevice(transport)
	if err != nil {
		t.Fatal(err)
	}
	message := bytes.Repeat([]byte("ledger-stream-"), 60)
	signature, err := device.SignPersonalMessage(context.Background(), "m/44'/60'/0'/0/0", message)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(transport.message, message) {
		t.Fatal("streamed message differs from the input")
	}
	if len(transport.p1) < 3 || transport.p1[0] != 0 || transport.p1[1] != 0x80 {
		t.Fatalf("unexpected APDU continuation sequence: %x", transport.p1)
	}
	for _, size := range transport.sizes {
		if size > 255 {
			t.Fatalf("APDU chunk exceeds 255 bytes: %d", size)
		}
	}
	var digest [32]byte
	copy(digest[:], gethaccounts.TextHash(message))
	assertRecoveredAddress(t, digest, signature, crypto.PubkeyToAddress(key.PublicKey))
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
	wrongAddress, err := NewLedgerDevice(&ledgerMockTransport{key: privateKey, wrongAddress: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := wrongAddress.GetPublicKey(context.Background(), "m/44'/60'/0'/0/0"); !errors.Is(err, ErrLedgerTransport) {
		t.Fatalf("public-key/address mismatch was accepted: %v", err)
	}
	extraResponse, err := NewLedgerDevice(&ledgerMockTransport{key: privateKey, extraResponse: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := extraResponse.GetPublicKey(context.Background(), "m/44'/60'/0'/0/0"); !errors.Is(err, ErrLedgerTransport) {
		t.Fatalf("trailing response bytes were accepted: %v", err)
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

func TestLedgerSecureAppBaseline(t *testing.T) {
	key, err := ecdsa.GenerateKey(crypto.S256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	secureTransport := &ledgerMockTransport{key: key}
	secureDevice, err := NewLedgerDevice(secureTransport)
	if err != nil {
		t.Fatal(err)
	}
	configuration, err := secureDevice.GetAppConfiguration(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !configuration.Secure() || configuration.Major != 1 || configuration.Minor != 22 || configuration.Patch != 3 {
		t.Fatalf("unexpected secure configuration: %+v", configuration)
	}
	account := &wallet.Account{
		AccountID: "11111111-1111-4111-8111-111111111111",
		Address:   crypto.PubkeyToAddress(key.PublicKey).Hex(), SignerKind: wallet.SignerKindHardware,
		SignerReference: "ledger:v1:m/44'/60'/0'/0/0", State: wallet.AccountStateActive,
	}
	insecureTransport := &ledgerMockTransport{key: key, appConfiguration: []byte{0, 1, 22, 1}}
	insecureDevice, err := NewLedgerDevice(insecureTransport)
	if err != nil {
		t.Fatal(err)
	}
	insecureSigner, err := NewLedgerSigner(insecureDevice, fakeAccountLookup{account: account}, fakeApprovalVerifier{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := insecureSigner.SignPersonalMessage(context.Background(), LedgerPersonalMessageRequest{
		AccountID: account.AccountID, Message: []byte("message"), IntentHash: [32]byte{1},
		ApprovalID: "21111111-1111-4111-8111-111111111111",
	}); !errors.Is(err, ErrLedgerInsecureApp) {
		t.Fatalf("insecure Ledger app was accepted: %v", err)
	}
	if insecureTransport.exchangeCalls != 1 {
		t.Fatalf("insecure app reached signing APDU: %d calls", insecureTransport.exchangeCalls)
	}
	if !errors.Is(mapLedgerStatus(ledgerSWCanceled), ErrLedgerDenied) {
		t.Fatal("Ledger cancellation status was not mapped to denial")
	}
}

func TestLedgerDeviceSerializesConcurrentAPDUs(t *testing.T) {
	key, err := ecdsa.GenerateKey(crypto.S256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	transport := &concurrentLedgerTransport{delegate: &ledgerMockTransport{key: key}}
	device, err := NewLedgerDevice(transport)
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	errorsChannel := make(chan error, 2)
	var wait sync.WaitGroup
	wait.Add(2)
	go func() {
		defer wait.Done()
		<-start
		_, err := device.GetAppConfiguration(context.Background())
		errorsChannel <- err
	}()
	go func() {
		defer wait.Done()
		<-start
		_, err := device.GetPublicKey(context.Background(), "m/44'/60'/0'/0/0")
		errorsChannel <- err
	}()
	close(start)
	wait.Wait()
	close(errorsChannel)
	for err := range errorsChannel {
		if err != nil {
			t.Fatal(err)
		}
	}
	if transport.maximum.Load() != 1 {
		t.Fatalf("Ledger APDUs overlapped: maximum=%d", transport.maximum.Load())
	}
}

func TestLedgerSignerRequiresStructuredApprovedIntent(t *testing.T) {
	key, err := ecdsa.GenerateKey(crypto.S256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	account := &wallet.Account{
		AccountID:  "11111111-1111-4111-8111-111111111111",
		Address:    crypto.PubkeyToAddress(key.PublicKey).Hex(),
		SignerKind: wallet.SignerKindHardware, SignerReference: "ledger:v1:m/44'/60'/0'/0/0",
		State: wallet.AccountStateActive,
	}
	device, err := NewLedgerDevice(&ledgerMockTransport{key: key})
	if err != nil {
		t.Fatal(err)
	}
	signer, err := NewLedgerSigner(device, fakeAccountLookup{account: account}, fakeApprovalVerifier{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := signer.Sign(context.Background(), wallet.CapabilityHandle{}, wallet.SoftwareSigningRequest{}); !errors.Is(err, ErrHardwareIntentRequired) {
		t.Fatalf("opaque digest was not rejected: %v", err)
	}
	var domainHash, messageHash [32]byte
	copy(domainHash[:], crypto.Keccak256([]byte("domain")))
	copy(messageHash[:], crypto.Keccak256([]byte("message")))
	typedResult, err := signer.SignTypedHash(context.Background(), LedgerTypedHashRequest{
		AccountID: account.AccountID, ChainID: 1,
		DomainSeparatorHash: domainHash, MessageHash: messageHash,
		IntentHash: [32]byte{1}, ApprovalID: "51111111-1111-4111-8111-111111111111",
	})
	if err != nil {
		t.Fatal(err)
	}
	typedDigest := crypto.Keccak256Hash([]byte{0x19, 0x01}, domainHash[:], messageHash[:])
	assertRecoveredAddress(t, typedDigest, typedResult.Signature, common.HexToAddress(account.Address))

	message := []byte("ledger personal message")
	personalResult, err := signer.SignPersonalMessage(context.Background(), LedgerPersonalMessageRequest{
		AccountID: account.AccountID, ChainID: 0, Message: message,
		IntentHash: [32]byte{2}, ApprovalID: "61111111-1111-4111-8111-111111111111",
	})
	if err != nil {
		t.Fatal(err)
	}
	var personalDigest [32]byte
	copy(personalDigest[:], gethaccounts.TextHash(message))
	assertRecoveredAddress(t, personalDigest, personalResult.Signature, common.HexToAddress(account.Address))
}

func TestLedgerSignerRejectsDeniedApprovalAndForeignKey(t *testing.T) {
	key, err := ecdsa.GenerateKey(crypto.S256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	foreignKey, err := ecdsa.GenerateKey(crypto.S256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	account := &wallet.Account{
		AccountID:  "11111111-1111-4111-8111-111111111111",
		Address:    crypto.PubkeyToAddress(key.PublicKey).Hex(),
		SignerKind: wallet.SignerKindHardware, SignerReference: "ledger:v1:m/44'/60'/0'/0/0",
		State: wallet.AccountStateActive,
	}
	deniedDevice, err := NewLedgerDevice(&ledgerMockTransport{key: key})
	if err != nil {
		t.Fatal(err)
	}
	deniedSigner, err := NewLedgerSigner(deniedDevice, fakeAccountLookup{account: account}, fakeApprovalVerifier{requireError: wallet.ErrCapabilityDenied})
	if err != nil {
		t.Fatal(err)
	}
	request := LedgerPersonalMessageRequest{
		AccountID: account.AccountID, ChainID: 0, Message: []byte("message"),
		IntentHash: [32]byte{1}, ApprovalID: "51111111-1111-4111-8111-111111111111",
	}
	if _, err := deniedSigner.SignPersonalMessage(context.Background(), request); err == nil {
		t.Fatal("denied approval reached the device")
	}

	chainTransport := &ledgerMockTransport{key: key}
	chainDevice, err := NewLedgerDevice(chainTransport)
	if err != nil {
		t.Fatal(err)
	}
	chainSigner, err := NewLedgerSigner(chainDevice, fakeAccountLookup{account: account}, fakeApprovalVerifier{})
	if err != nil {
		t.Fatal(err)
	}
	wrongChain := request
	wrongChain.ChainID = 1
	if _, err := chainSigner.SignPersonalMessage(context.Background(), wrongChain); err == nil {
		t.Fatal("chain-bound personal_sign request was accepted")
	}
	if chainTransport.exchangeCalls != 0 {
		t.Fatal("invalid personal_sign binding reached the Ledger")
	}

	foreignDevice, err := NewLedgerDevice(&ledgerMockTransport{key: foreignKey})
	if err != nil {
		t.Fatal(err)
	}
	foreignSigner, err := NewLedgerSigner(foreignDevice, fakeAccountLookup{account: account}, fakeApprovalVerifier{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := foreignSigner.SignPersonalMessage(context.Background(), request); !errors.Is(err, ErrLedgerSignature) {
		t.Fatalf("foreign signature was accepted: %v", err)
	}
}

type speculosSignResult struct {
	signature []byte
	err       error
}

func speculosCurrentScreen(ctx context.Context, baseURL string) (string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSuffix(baseURL, "/")+"/events?currentscreenonly=true", nil)
	if err != nil {
		return "", err
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return "", err
	}
	defer func() { _ = response.Body.Close() }()
	var decoded struct {
		Events []struct {
			Text string `json:"text"`
		} `json:"events"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&decoded); err != nil {
		return "", err
	}
	texts := make([]string, 0, len(decoded.Events))
	for _, event := range decoded.Events {
		texts = append(texts, event.Text)
	}
	return strings.Join(texts, " "), nil
}

func speculosPress(ctx context.Context, baseURL, button string) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimSuffix(baseURL, "/")+"/button/"+button,
		strings.NewReader(`{"action":"press-and-release"}`))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("speculos button %s: status %d", button, response.StatusCode)
	}
	return nil
}

func ensureSpeculosBlindSigning(ctx context.Context, baseURL string) error {
	for attempt := 0; attempt < 20; attempt++ {
		screen, err := speculosCurrentScreen(ctx, baseURL)
		if err != nil {
			return err
		}
		switch {
		case strings.Contains(screen, "Blind signing") && strings.Contains(screen, "Enabled"):
			return nil
		case strings.Contains(screen, "Blind signing") && strings.Contains(screen, "Disabled"):
			if err := speculosPress(ctx, baseURL, "both"); err != nil {
				return err
			}
		case strings.Contains(screen, "app is ready"):
			if err := speculosPress(ctx, baseURL, "right"); err != nil {
				return err
			}
		case strings.Contains(screen, "App settings"):
			if err := speculosPress(ctx, baseURL, "both"); err != nil {
				return err
			}
		case strings.Contains(screen, "App info") || strings.Contains(screen, "Quit app"):
			if err := speculosPress(ctx, baseURL, "left"); err != nil {
				return err
			}
		case strings.Contains(screen, "Back"):
			if err := speculosPress(ctx, baseURL, "both"); err != nil {
				return err
			}
		default:
			if err := speculosPress(ctx, baseURL, "right"); err != nil {
				return err
			}
		}
		time.Sleep(150 * time.Millisecond)
	}
	return fmt.Errorf("speculos blind signing setting not reachable")
}

func runSpeculosSigning(ctx context.Context, baseURL string, sign func(context.Context) ([]byte, error)) ([]byte, error) {
	resultChannel := make(chan speculosSignResult, 1)
	go func() {
		signature, err := sign(ctx)
		resultChannel <- speculosSignResult{signature: signature, err: err}
	}()
	lastScreen := ""
	reviewing := false
	for {
		select {
		case result := <-resultChannel:
			return result.signature, result.err
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(100 * time.Millisecond):
			screen, err := speculosCurrentScreen(ctx, baseURL)
			if err != nil {
				return nil, err
			}
			if screen == "" || screen == lastScreen {
				continue
			}
			lastScreen = screen
			switch {
			case strings.Contains(screen, "Blind signing ahead"):
				if err := speculosPress(ctx, baseURL, "both"); err != nil {
					return nil, err
				}
			case strings.Contains(screen, "Sign message"), strings.Contains(screen, "Sign transaction"), strings.Contains(screen, "Accept transaction"), strings.Contains(screen, "Accept and send"):
				if err := speculosPress(ctx, baseURL, "both"); err != nil {
					return nil, err
				}
			case strings.Contains(screen, "Reject"):
				if err := speculosPress(ctx, baseURL, "left"); err != nil {
					return nil, err
				}
			case strings.Contains(screen, "Review"):
				reviewing = true
				if err := speculosPress(ctx, baseURL, "right"); err != nil {
					return nil, err
				}
			case reviewing:
				if err := speculosPress(ctx, baseURL, "right"); err != nil {
					return nil, err
				}
			}
		}
	}
}

// TestLedgerSpeculosIntegration requires a running Speculos container with
// the Ethereum app; skipped unless BLOCO_WALLET_SPECULOS_URL is set.
func TestLedgerSpeculosIntegration(t *testing.T) {
	speculosURL := os.Getenv("BLOCO_WALLET_SPECULOS_URL")
	if speculosURL == "" {
		t.Skip("BLOCO_WALLET_SPECULOS_URL not set; skipping Speculos integration")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := ensureSpeculosBlindSigning(ctx, speculosURL); err != nil {
		t.Fatal(err)
	}
	transport, err := NewSpeculosTransport(speculosURL, testGatewayForServer(t, speculosURL))
	if err != nil {
		t.Fatal(err)
	}
	device, err := NewLedgerDevice(transport)
	if err != nil {
		t.Fatal(err)
	}
	configuration, err := device.GetAppConfiguration(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !configuration.Secure() {
		t.Fatalf("Speculos Ethereum app is below 1.22.3: %+v", configuration)
	}
	result, err := device.GetPublicKey(ctx, "m/44'/60'/0'/0/0")
	if err != nil {
		t.Fatal(err)
	}
	if result.Address == (common.Address{}) {
		t.Fatal("speculos returned the zero address")
	}
	const expectedSpeculosAddress = "0xDad77910DbDFdE764fC21FCD4E74D71bBACA6D8D"
	if result.Address.Hex() != expectedSpeculosAddress {
		t.Fatalf("unexpected deterministic-seed address: %s", result.Address.Hex())
	}
	publicKey, err := crypto.UnmarshalPubkey(result.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	if crypto.PubkeyToAddress(*publicKey) != result.Address {
		t.Fatal("speculos address does not match its public key")
	}
	to := common.HexToAddress("0x1111111111111111111111111111111111111111")
	transactionTests := []struct {
		transaction *types.Transaction
		signer      types.Signer
	}{
		{
			transaction: types.NewTx(&types.LegacyTx{Nonce: 1, GasPrice: big.NewInt(2_000_000_000), Gas: 21_000, To: &to, Value: big.NewInt(1)}),
			signer:      types.NewEIP155Signer(big.NewInt(1)),
		},
		{
			transaction: types.NewTx(&types.DynamicFeeTx{ChainID: big.NewInt(1), Nonce: 2, GasTipCap: big.NewInt(1_000_000_000), GasFeeCap: big.NewInt(2_000_000_000), Gas: 21_000, To: &to, Value: big.NewInt(1)}),
			signer:      types.NewLondonSigner(big.NewInt(1)),
		},
	}
	for _, transactionTest := range transactionTests {
		digest := transactionTest.signer.Hash(transactionTest.transaction)
		signature, err := runSpeculosSigning(ctx, speculosURL, func(signContext context.Context) ([]byte, error) {
			return device.SignTransaction(signContext, LedgerTransactionIntent{
				UnsignedTransaction: transactionTest.transaction, ChainID: big.NewInt(1),
				DerivationPath: "m/44'/60'/0'/0/0", Digest: digest, ExpectedAddress: result.Address,
			})
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := verifyECDSASignature(result.Address, digest, signature); err != nil {
			t.Fatal(err)
		}
	}

	var domain, message [32]byte
	copy(domain[:], bytes.Repeat([]byte{0x11}, 32))
	copy(message[:], bytes.Repeat([]byte{0x22}, 32))
	typedSignature, err := runSpeculosSigning(ctx, speculosURL, func(signContext context.Context) ([]byte, error) {
		return device.SignTypedMessage(signContext, "m/44'/60'/0'/0/0", domain, message)
	})
	if err != nil {
		t.Fatal(err)
	}
	typedDigest := crypto.Keccak256Hash([]byte{0x19, 0x01}, domain[:], message[:])
	typedPublicKey, err := crypto.SigToPub(typedDigest[:], typedSignature)
	if err != nil {
		t.Fatal(err)
	}
	if crypto.PubkeyToAddress(*typedPublicKey) != result.Address {
		t.Fatal("Speculos EIP-712 signature does not recover the device address")
	}

	personalMessage := []byte("bloco Speculos personal message")
	personalSignature, err := runSpeculosSigning(ctx, speculosURL, func(signContext context.Context) ([]byte, error) {
		return device.SignPersonalMessage(signContext, "m/44'/60'/0'/0/0", personalMessage)
	})
	if err != nil {
		t.Fatal(err)
	}
	personalDigest := crypto.Keccak256Hash(
		[]byte("\x19Ethereum Signed Message:\n"+itoa2(len(personalMessage))), personalMessage,
	)
	personalPublicKey, err := crypto.SigToPub(personalDigest[:], personalSignature)
	if err != nil {
		t.Fatal(err)
	}
	if crypto.PubkeyToAddress(*personalPublicKey) != result.Address {
		t.Fatal("Speculos EIP-191 signature does not recover the device address")
	}
}
