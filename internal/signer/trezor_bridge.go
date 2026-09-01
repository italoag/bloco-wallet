package signer

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	"blocowallet/internal/blockchain"
)

type bridgeProtocolMessage struct {
	Protocol string `json:"protocol"`
	Data     string `json:"data,omitempty"`
}

type bridgePacketTransport struct {
	baseURL          string
	origin           string
	session          string
	gateway          *blockchain.RPCGateway
	protocolMessages bool
	writeBuffer      []byte
	pending          [][]byte
}

type BridgeDevice struct {
	*UDPDevice
}

func NewBridgeDevice(ctx context.Context, baseURL string, gateway *blockchain.RPCGateway) (*BridgeDevice, error) {
	if gateway == nil {
		return nil, fmt.Errorf("trezor signer: RPC gateway required")
	}
	baseURL = strings.TrimSuffix(baseURL, "/")
	parsed, err := url.Parse(baseURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return nil, fmt.Errorf("trezor signer: invalid bridge URL")
	}
	host := parsed.Hostname()
	ip := net.ParseIP(host)
	loopback := host == "localhost" || (ip != nil && ip.IsLoopback())
	if !loopback {
		return nil, fmt.Errorf("trezor signer: bridge must be loopback")
	}
	transport := &bridgePacketTransport{
		baseURL: baseURL,
		origin:  "https://python.trezor.io",
		gateway: gateway,
	}
	var configuration struct {
		Version          string `json:"version"`
		ProtocolMessages bool   `json:"protocolMessages"`
	}
	if err := transport.postJSON(ctx, "/configure", nil, &configuration); err != nil {
		return nil, fmt.Errorf("trezor signer: bridge configure: %w", err)
	}
	if configuration.Version == "" {
		return nil, fmt.Errorf("trezor signer: bridge returned no version")
	}
	transport.protocolMessages = configuration.ProtocolMessages
	var devices []struct {
		Path string `json:"path"`
	}
	if err := transport.postJSON(ctx, "/enumerate", nil, &devices); err != nil {
		return nil, fmt.Errorf("trezor signer: bridge enumerate: %w", err)
	}
	if len(devices) == 0 || devices[0].Path == "" {
		return nil, fmt.Errorf("trezor signer: bridge has no device")
	}
	var acquired struct {
		Session string `json:"session"`
	}
	acquirePath := "/acquire/" + url.PathEscape(devices[0].Path) + "/null"
	if err := transport.postJSON(ctx, acquirePath, nil, &acquired); err != nil {
		return nil, fmt.Errorf("trezor signer: bridge acquire: %w", err)
	}
	if acquired.Session == "" {
		return nil, fmt.Errorf("trezor signer: bridge returned no session")
	}
	transport.session = acquired.Session
	device := &UDPDevice{address: baseURL, transport: transport}
	return &BridgeDevice{UDPDevice: device}, nil
}

func (transport *bridgePacketTransport) WritePacket(ctx context.Context, packet []byte) error {
	if len(packet) != trezorPacketSize {
		return fmt.Errorf("trezor signer: invalid packet size")
	}
	if transport.protocolMessages {
		request := bridgeProtocolMessage{Protocol: "v1", Data: hex.EncodeToString(packet)}
		var response bridgeProtocolMessage
		if err := transport.postJSON(ctx, "/post/"+url.PathEscape(transport.session), request, &response); err != nil {
			return fmt.Errorf("trezor signer: bridge post: %w", err)
		}
		if response.Protocol != "v1" {
			return fmt.Errorf("trezor signer: bridge protocol mismatch")
		}
		return nil
	}
	if len(transport.writeBuffer) == 0 {
		if packet[0] != '?' || packet[1] != '#' || packet[2] != '#' {
			return fmt.Errorf("trezor signer: malformed first bridge packet")
		}
		transport.writeBuffer = append(transport.writeBuffer, packet[1:]...)
	} else {
		if packet[0] != '?' {
			transport.writeBuffer = nil
			return fmt.Errorf("trezor signer: malformed continuation bridge packet")
		}
		transport.writeBuffer = append(transport.writeBuffer, packet[1:]...)
	}
	if len(transport.writeBuffer) < 8 {
		return nil
	}
	messageLength := int(binary.BigEndian.Uint32(transport.writeBuffer[4:8]))
	frameLength := 8 + messageLength
	if messageLength > trezorMaxMessageBytes || frameLength < 8 {
		transport.writeBuffer = nil
		return fmt.Errorf("trezor signer: bridge message too large")
	}
	if len(transport.writeBuffer) < frameLength {
		return nil
	}
	frame := append([]byte(nil), transport.writeBuffer[2:frameLength]...)
	transport.writeBuffer = nil
	if _, err := transport.postPlain(ctx, "/post/"+url.PathEscape(transport.session), hex.EncodeToString(frame)); err != nil {
		return fmt.Errorf("trezor signer: bridge post: %w", err)
	}
	return nil
}

func (transport *bridgePacketTransport) ReadPacket(ctx context.Context) ([]byte, error) {
	if len(transport.pending) > 0 {
		packet := transport.pending[0]
		transport.pending = transport.pending[1:]
		return packet, nil
	}
	if !transport.protocolMessages {
		encoded, err := transport.postPlain(ctx, "/read/"+url.PathEscape(transport.session), "")
		if err != nil {
			return nil, fmt.Errorf("trezor signer: bridge read: %w", err)
		}
		decoded, err := hex.DecodeString(strings.TrimSpace(encoded))
		if err != nil || len(decoded) < 6 {
			return nil, fmt.Errorf("trezor signer: malformed bridge message")
		}
		messageLength := int(binary.BigEndian.Uint32(decoded[2:6]))
		if messageLength > trezorMaxMessageBytes || len(decoded) != 6+messageLength {
			return nil, fmt.Errorf("trezor signer: malformed bridge message")
		}
		return transport.queueBridgeMessage(decoded)
	}
	request := bridgeProtocolMessage{Protocol: "v1"}
	var response bridgeProtocolMessage
	if err := transport.postJSON(ctx, "/read/"+url.PathEscape(transport.session), request, &response); err != nil {
		return nil, fmt.Errorf("trezor signer: bridge read: %w", err)
	}
	if response.Protocol != "v1" {
		return nil, fmt.Errorf("trezor signer: bridge protocol mismatch")
	}
	decoded, err := hex.DecodeString(response.Data)
	if err != nil || len(decoded) == 0 {
		return nil, fmt.Errorf("trezor signer: malformed bridge packet")
	}
	firstEnd := trezorPacketSize
	if firstEnd > len(decoded) {
		firstEnd = len(decoded)
	}
	first := make([]byte, trezorPacketSize)
	copy(first, decoded[:firstEnd])
	for offset := firstEnd; offset < len(decoded); {
		end := offset + trezorPacketDataSize
		if end > len(decoded) {
			end = len(decoded)
		}
		packet := make([]byte, trezorPacketSize)
		packet[0] = '?'
		copy(packet[1:], decoded[offset:end])
		transport.pending = append(transport.pending, packet)
		offset = end
	}
	return first, nil
}

func (transport *bridgePacketTransport) queueBridgeMessage(frame []byte) ([]byte, error) {
	buffer := make([]byte, 2+len(frame))
	copy(buffer, "##")
	copy(buffer[2:], frame)
	for offset := 0; offset < len(buffer); {
		end := offset + trezorPacketDataSize
		if end > len(buffer) {
			end = len(buffer)
		}
		packet := make([]byte, trezorPacketSize)
		packet[0] = '?'
		copy(packet[1:], buffer[offset:end])
		transport.pending = append(transport.pending, packet)
		offset = end
	}
	if len(transport.pending) == 0 {
		return nil, fmt.Errorf("trezor signer: empty bridge message")
	}
	packet := transport.pending[0]
	transport.pending = transport.pending[1:]
	return packet, nil
}

func (transport *bridgePacketTransport) Close() error {
	if transport == nil || transport.session == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	response, err := transport.gateway.Request(ctx, blockchain.OutboundRequest{
		Method: "POST", URL: transport.baseURL + "/release/" + url.PathEscape(transport.session),
		Headers: map[string]string{"Origin": transport.origin}, MaxResponseBytes: 4 << 10,
	})
	if err != nil {
		return err
	}
	transport.session = ""
	if response.StatusCode != 200 {
		return fmt.Errorf("trezor signer: bridge release status %d", response.StatusCode)
	}
	return nil
}

func (transport *bridgePacketTransport) postPlain(ctx context.Context, path, body string) (string, error) {
	response, err := transport.gateway.Request(ctx, blockchain.OutboundRequest{
		Method: "POST", URL: transport.baseURL + path,
		Headers: map[string]string{"Origin": transport.origin, "Content-Type": "text/plain"},
		Body:    []byte(body), MaxResponseBytes: 2*trezorMaxMessageBytes + 64, Timeout: 5 * time.Minute,
	})
	if err != nil {
		return "", err
	}
	if response.StatusCode != 200 {
		return "", fmt.Errorf("trezor signer: bridge status %d", response.StatusCode)
	}
	return string(response.Body), nil
}

func (transport *bridgePacketTransport) postJSON(ctx context.Context, path string, input any, output any) error {
	var body []byte
	headers := map[string]string{"Origin": transport.origin}
	if input != nil {
		encoded, err := json.Marshal(input)
		if err != nil {
			return err
		}
		body = encoded
		headers["Content-Type"] = "application/json"
	}
	response, err := transport.gateway.Request(ctx, blockchain.OutboundRequest{
		Method: "POST", URL: transport.baseURL + path, Headers: headers,
		Body: body, MaxResponseBytes: trezorMaxMessageBytes, Timeout: 5 * time.Minute,
	})
	if err != nil {
		return err
	}
	if response.StatusCode != 200 {
		return fmt.Errorf("trezor signer: bridge status %d", response.StatusCode)
	}
	if output == nil {
		return nil
	}
	if err := json.Unmarshal(response.Body, output); err != nil {
		return err
	}
	return nil
}
