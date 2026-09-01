package signer

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	gethaccounts "github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/gorilla/websocket"
)

func TestTrezorCodecAgainstFirmwareSchema(t *testing.T) {
	features := []byte{
		0x10, 0x02,
		0x18, 0x0c,
		0x20, 0x04,
		0x38, 0x01,
		0x40, 0x00,
		0x60, 0x01,
		0xaa, 0x01, 0x04, 'T', '2', 'T', '1',
	}
	parsed, err := parseFeatures(features)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Model != "T2T1" || parsed.Version != "2.12.4" || !parsed.Initialized || !parsed.PinProtection || parsed.PassphraseProtection {
		t.Fatalf("unexpected features: %+v", parsed)
	}

	publicKey := bytes.Repeat([]byte{0x02}, 33)
	node := append([]byte{0x32, 0x21}, publicKey...)
	response := append([]byte{0x0a, byte(len(node))}, node...)
	extracted, err := parseEthereumPublicKey(response)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(extracted, publicKey) {
		t.Fatal("public key mismatch")
	}

	signature := bytes.Repeat([]byte{0xAB}, 65)
	typedResponse := append([]byte{0x0a, 0x41}, signature...)
	decoded, err := parseSignatureField(typedResponse, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(decoded, signature) {
		t.Fatal("typed signature mismatch")
	}
	messageResponse := append([]byte{0x12, 0x41}, signature...)
	decoded, err = parseSignatureField(messageResponse, 2)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(decoded, signature) {
		t.Fatal("message signature mismatch")
	}

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
	typedRequest := encodeAddressPath(numbers)
	typedRequest = appendBytesField(typedRequest, 2, bytes.Repeat([]byte{0x11}, 32))
	typedRequest = appendBytesField(typedRequest, 3, bytes.Repeat([]byte{0x22}, 32))
	expectedPath, err := hex.DecodeString("08ac8080800808bc8080800808808080800808000800")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(typedRequest, expectedPath) {
		t.Fatalf("unexpected address_n encoding: %x", typedRequest[:len(expectedPath)])
	}
	if !bytes.Contains(typedRequest, append([]byte{0x12, 0x20}, bytes.Repeat([]byte{0x11}, 32)...)) {
		t.Fatal("domain separator hash field missing")
	}
	if !bytes.Contains(typedRequest, append([]byte{0x1a, 0x20}, bytes.Repeat([]byte{0x22}, 32)...)) {
		t.Fatal("message hash field missing")
	}
}

type recordingPacketTransport struct {
	writes    []int
	responses [][]byte
}

func (transport *recordingPacketTransport) WritePacket(_ context.Context, packet []byte) error {
	if len(packet) != trezorPacketSize || string(packet[:3]) != "?##" {
		return errors.New("malformed write packet")
	}
	transport.writes = append(transport.writes, int(binary.BigEndian.Uint16(packet[3:5])))
	return nil
}

func (transport *recordingPacketTransport) ReadPacket(context.Context) ([]byte, error) {
	if len(transport.responses) == 0 {
		return nil, errors.New("no response")
	}
	packet := transport.responses[0]
	transport.responses = transport.responses[1:]
	return packet, nil
}

func (*recordingPacketTransport) Close() error { return nil }

func trezorResponsePacket(messageType int, payload []byte) []byte {
	packet := make([]byte, trezorPacketSize)
	packet[0], packet[1], packet[2] = '?', '#', '#'
	binary.BigEndian.PutUint16(packet[3:5], uint16(messageType))
	binary.BigEndian.PutUint32(packet[5:9], uint32(len(payload)))
	copy(packet[9:], payload)
	return packet
}

func TestTrezorButtonAckPrecedesConfirmationAndCancel(t *testing.T) {
	features := []byte{0x10, 0x01, 0x18, 0x0e, 0x20, 0x01, 0x60, 0x01, 0xaa, 0x01, 0x01, '1'}
	transport := &recordingPacketTransport{responses: [][]byte{
		trezorResponsePacket(trezorMessageButtonRequest, nil),
		trezorResponsePacket(trezorMessageFeatures, features),
	}}
	device := &UDPDevice{transport: transport}
	device.SetButtonHandler(func(context.Context) error {
		if len(transport.writes) != 2 || transport.writes[1] != trezorMessageButtonAck {
			return errors.New("button ack did not precede confirmation")
		}
		return nil
	})
	if _, err := device.call(context.Background(), trezorMessageInitialize, trezorMessageFeatures, nil); err != nil {
		t.Fatal(err)
	}
	if len(transport.writes) != 2 || transport.writes[0] != trezorMessageInitialize || transport.writes[1] != trezorMessageButtonAck {
		t.Fatalf("unexpected write sequence: %v", transport.writes)
	}

	cancelTransport := &recordingPacketTransport{responses: [][]byte{trezorResponsePacket(trezorMessageButtonRequest, nil)}}
	cancelDevice := &UDPDevice{transport: cancelTransport}
	if _, err := cancelDevice.call(context.Background(), trezorMessageInitialize, trezorMessageFeatures, nil); !errors.Is(err, ErrTrezorInteractionRequired) {
		t.Fatalf("missing handler was not rejected: %v", err)
	}
	if len(cancelTransport.writes) != 2 || cancelTransport.writes[1] != trezorMessageCancel {
		t.Fatalf("cancel was not sent after missing handler: %v", cancelTransport.writes)
	}
}

func TestTrezorBridgeRejectsUnsafeURLs(t *testing.T) {
	urls := []string{
		"http://example.com:21328",
		"http://user@localhost:21328",
		"http://localhost:21328?device=1",
		"http://localhost:21328/#fragment",
		"ftp://localhost:21328",
	}
	for _, bridgeURL := range urls {
		t.Run(bridgeURL, func(t *testing.T) {
			if _, err := NewBridgeDevice(context.Background(), bridgeURL); err == nil {
				t.Fatal("unsafe bridge URL was accepted")
			}
		})
	}
	if _, err := NewUDPDevice(context.Background(), "192.0.2.1:21324"); err == nil {
		t.Fatal("non-loopback emulator address was accepted")
	}
}

func TestTrezorUDPDeviceUsesProtocolV1Packets(t *testing.T) {
	bridge, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = bridge.Close() }()

	serverDone := make(chan error, 1)
	go func() {
		ping := make([]byte, 8)
		count, peer, readErr := bridge.ReadFromUDP(ping)
		if readErr != nil {
			serverDone <- readErr
			return
		}
		if count != 8 || string(ping) != "PINGPING" {
			serverDone <- errors.New("invalid ping")
			return
		}
		if _, writeErr := bridge.WriteToUDP([]byte("PONGPONG"), peer); writeErr != nil {
			serverDone <- writeErr
			return
		}
		packet := make([]byte, trezorPacketSize)
		count, peer, readErr = bridge.ReadFromUDP(packet)
		if readErr != nil {
			serverDone <- readErr
			return
		}
		if count != trezorPacketSize || string(packet[:3]) != "?##" || binary.BigEndian.Uint16(packet[3:5]) != trezorMessageInitialize {
			serverDone <- errors.New("invalid initialize packet")
			return
		}
		features := []byte{0x10, 0x02, 0x18, 0x0c, 0x20, 0x04, 0x60, 0x01, 0xaa, 0x01, 0x04, 'T', '2', 'T', '1'}
		if writeErr := writeTrezorResponse(bridge, peer, trezorMessageFeatures, features); writeErr != nil {
			serverDone <- writeErr
			return
		}
		serverDone <- nil
	}()

	device, err := NewUDPDevice(context.Background(), bridge.LocalAddr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = device.Close() }()
	features, err := device.Initialize(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if features.Model != "T2T1" || features.Version != "2.12.4" || !features.Initialized {
		t.Fatalf("unexpected features: %+v", features)
	}
	if err := <-serverDone; err != nil {
		t.Fatal(err)
	}
}

func writeTrezorResponse(conn *net.UDPConn, peer *net.UDPAddr, messageType int, payload []byte) error {
	buffer := make([]byte, 0, 8+len(payload))
	buffer = append(buffer, '#', '#')
	var header [6]byte
	binary.BigEndian.PutUint16(header[:2], uint16(messageType))
	binary.BigEndian.PutUint32(header[2:], uint32(len(payload)))
	buffer = append(buffer, header[:]...)
	buffer = append(buffer, payload...)
	for offset := 0; offset < len(buffer); {
		end := offset + trezorPacketDataSize
		if end > len(buffer) {
			end = len(buffer)
		}
		packet := make([]byte, trezorPacketSize)
		packet[0] = '?'
		copy(packet[1:], buffer[offset:end])
		if _, err := conn.WriteToUDP(packet, peer); err != nil {
			return err
		}
		offset = end
	}
	return nil
}

func TestTrezorEmulatorIntegration(t *testing.T) {
	emulatorAddress := os.Getenv("BLOCO_WALLET_TREZOR_EMULATOR")
	if emulatorAddress == "" {
		t.Skip("BLOCO_WALLET_TREZOR_EMULATOR not set; skipping emulator integration")
	}
	controllerURL := os.Getenv("BLOCO_WALLET_TREZOR_CONTROLLER_URL")
	if controllerURL == "" {
		controllerURL = "ws://127.0.0.1:9001/"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	var device *UDPDevice
	if strings.HasPrefix(emulatorAddress, "http://") || strings.HasPrefix(emulatorAddress, "https://") {
		bridgeDevice, err := NewBridgeDevice(ctx, emulatorAddress)
		if err != nil {
			t.Fatal(err)
		}
		device = bridgeDevice.UDPDevice
	} else {
		var err error
		device, err = NewUDPDevice(ctx, emulatorAddress)
		if err != nil {
			t.Fatal(err)
		}
	}
	defer func() { _ = device.Close() }()
	device.SetButtonHandler(func(handlerContext context.Context) error {
		return trezorControllerCommand(handlerContext, controllerURL, map[string]any{"type": "emulator-press-yes"})
	})
	features, err := device.Initialize(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !features.Initialized {
		if err := trezorControllerCommand(ctx, controllerURL, map[string]any{
			"type": "emulator-setup", "mnemonic": "all all all all all all all all all all all all",
			"pin": "", "passphrase_protection": false, "label": "Bloco Test",
		}); err != nil {
			t.Fatal(err)
		}
		time.Sleep(500 * time.Millisecond)
		features, err = device.Initialize(ctx)
		if err != nil {
			t.Fatal(err)
		}
	}
	if !features.Initialized {
		t.Fatal("emulator setup did not initialize the device")
	}
	publicKey, err := device.EthereumGetPublicKey(ctx, "m/44'/60'/0'/0/0")
	if err != nil {
		t.Fatal(err)
	}
	if len(publicKey) != 33 {
		t.Fatalf("unexpected public key size: %d", len(publicKey))
	}
	parsedPublicKey, err := crypto.DecompressPubkey(publicKey)
	if err != nil {
		t.Fatal(err)
	}
	const expectedSeedAddress = "0x73d0385F4d8E00C5e6504C6030F47BF6212736A8"
	if crypto.PubkeyToAddress(*parsedPublicKey).Hex() != expectedSeedAddress {
		t.Fatalf("unexpected deterministic-seed address: %s", crypto.PubkeyToAddress(*parsedPublicKey).Hex())
	}

	var domainHash, messageHash [32]byte
	copy(domainHash[:], crypto.Keccak256([]byte("bloco trezor domain")))
	copy(messageHash[:], crypto.Keccak256([]byte("bloco trezor message")))
	if features.Model == "1" {
		typedSignature, err := device.EthereumSignTypedHash(ctx, "m/44'/60'/0'/0/0", domainHash, messageHash)
		if err != nil {
			t.Fatal(err)
		}
		typedDigest := crypto.Keccak256Hash([]byte{0x19, 0x01}, domainHash[:], messageHash[:])
		if err := verifyTrezorSignature(publicKey, typedDigest, typedSignature); err != nil {
			t.Fatal(err)
		}
	} else {
		if _, err := device.EthereumSignTypedHash(ctx, "m/44'/60'/0'/0/0", domainHash, messageHash); err == nil {
			t.Fatal("core-model Trezor accepted legacy typed-hash signing")
		}
	}

	personalMessage := []byte("bloco Trezor personal message")
	personalSignature, err := device.EthereumSignMessage(ctx, "m/44'/60'/0'/0/0", personalMessage)
	if err != nil {
		t.Fatal(err)
	}
	var personalDigest [32]byte
	copy(personalDigest[:], gethaccounts.TextHash(personalMessage))
	if err := verifyTrezorSignature(publicKey, personalDigest, personalSignature); err != nil {
		t.Fatal(err)
	}
}

func trezorControllerCommand(ctx context.Context, controllerURL string, command map[string]any) error {
	connection, _, err := websocket.DefaultDialer.DialContext(ctx, controllerURL, nil)
	if err != nil {
		return err
	}
	defer func() { _ = connection.Close() }()
	if _, _, err := connection.ReadMessage(); err != nil {
		return err
	}
	command["id"] = 9001
	if err := connection.WriteJSON(command); err != nil {
		return err
	}
	for {
		_, responseBytes, err := connection.ReadMessage()
		if err != nil {
			return err
		}
		var response struct {
			ID      int  `json:"id"`
			Success bool `json:"success"`
		}
		if err := json.Unmarshal(responseBytes, &response); err != nil || response.ID != 9001 {
			continue
		}
		if !response.Success {
			return errors.New("Trezor controller command failed")
		}
		return nil
	}
}

func verifyTrezorSignature(compressed []byte, digest [32]byte, signature []byte) error {
	normalized, err := normalizeSignature(signature)
	if err != nil {
		return err
	}
	publicKey, err := crypto.DecompressPubkey(compressed)
	if err != nil {
		return err
	}
	recovered, err := crypto.SigToPub(digest[:], normalized)
	if err != nil {
		return err
	}
	if !bytes.Equal(crypto.CompressPubkey(publicKey), crypto.CompressPubkey(recovered)) {
		return ErrTrezorSignature
	}
	return nil
}
