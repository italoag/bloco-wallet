package signer

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

func TestTrezorModernBridgeRoundTrip(t *testing.T) {
	features := []byte{0x10, 0x01, 0x18, 0x0e, 0x20, 0x01, 0x60, 0x01, 0xaa, 0x01, 0x01, '1'}
	responseData := make([]byte, 0, 9+len(features))
	responseData = append(responseData, '?', '#', '#')
	var header [6]byte
	binary.BigEndian.PutUint16(header[:2], trezorMessageFeatures)
	binary.BigEndian.PutUint32(header[2:], uint32(len(features)))
	responseData = append(responseData, header[:]...)
	responseData = append(responseData, features...)

	var mutex sync.Mutex
	calls := make(map[string]int)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		mutex.Lock()
		calls[request.URL.Path]++
		mutex.Unlock()
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/configure":
			writeBridgeJSON(t, writer, map[string]any{"version": "3.2.1", "protocolMessages": true})
		case "/enumerate":
			writeBridgeJSON(t, writer, []map[string]any{{"path": "1", "session": nil}})
		case "/acquire/1/null":
			writeBridgeJSON(t, writer, map[string]string{"session": "session-1"})
		case "/post/session-1":
			var message bridgeProtocolMessage
			if err := json.NewDecoder(request.Body).Decode(&message); err != nil {
				t.Errorf("decode post: %v", err)
			}
			packet, err := hex.DecodeString(message.Data)
			if err != nil || len(packet) != trezorPacketSize || string(packet[:3]) != "?##" {
				t.Errorf("malformed posted packet: %x %v", packet, err)
			}
			writeBridgeJSON(t, writer, bridgeProtocolMessage{Protocol: "v1"})
		case "/read/session-1":
			writeBridgeJSON(t, writer, bridgeProtocolMessage{Protocol: "v1", Data: hex.EncodeToString(responseData)})
		case "/release/session-1":
			writeBridgeJSON(t, writer, map[string]string{"session": "session-1"})
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	device, err := NewBridgeDevice(context.Background(), server.URL, testGatewayForServer(t, server.URL))
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := device.Initialize(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Model != "1" || parsed.Version != "1.14.1" || !parsed.Initialized {
		t.Fatalf("unexpected bridge features: %+v", parsed)
	}
	if err := device.Close(); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/configure", "/enumerate", "/acquire/1/null", "/post/session-1", "/read/session-1", "/release/session-1"} {
		if calls[path] != 1 {
			t.Fatalf("bridge path %s called %d times", path, calls[path])
		}
	}
}

func TestTrezorStableBridgeRoundTrip(t *testing.T) {
	features := []byte{0x10, 0x01, 0x18, 0x0e, 0x20, 0x01, 0x60, 0x01, 0xaa, 0x01, 0x01, '1'}
	responseFrame := make([]byte, 6+len(features))
	binary.BigEndian.PutUint16(responseFrame[:2], trezorMessageFeatures)
	binary.BigEndian.PutUint32(responseFrame[2:6], uint32(len(features)))
	copy(responseFrame[6:], features)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/configure":
			writer.Header().Set("Content-Type", "application/json")
			writeBridgeJSON(t, writer, map[string]any{"version": "2.0.33"})
		case "/enumerate":
			writer.Header().Set("Content-Type", "application/json")
			writeBridgeJSON(t, writer, []map[string]any{{"path": "usb:001"}})
		case "/acquire/usb:001/null":
			writer.Header().Set("Content-Type", "application/json")
			writeBridgeJSON(t, writer, map[string]string{"session": "stable-session"})
		case "/post/stable-session":
			body, err := io.ReadAll(request.Body)
			if err != nil {
				t.Errorf("read stable post: %v", err)
			}
			if request.Header.Get("Content-Type") != "text/plain" || string(body) != "000000000000" {
				t.Errorf("unexpected stable post: content-type=%q body=%q", request.Header.Get("Content-Type"), body)
			}
		case "/read/stable-session":
			writer.Header().Set("Content-Type", "text/plain")
			_, _ = writer.Write([]byte(hex.EncodeToString(responseFrame)))
		case "/release/stable-session":
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()
	device, err := NewBridgeDevice(context.Background(), server.URL, testGatewayForServer(t, server.URL))
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := device.Initialize(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Model != "1" || parsed.Version != "1.14.1" || !parsed.Initialized {
		t.Fatalf("unexpected stable bridge features: %+v", parsed)
	}
	if err := device.Close(); err != nil {
		t.Fatal(err)
	}
}

func writeBridgeJSON(t *testing.T, writer http.ResponseWriter, value any) {
	t.Helper()
	if err := json.NewEncoder(writer).Encode(value); err != nil {
		t.Errorf("encode bridge response: %v", err)
	}
}
