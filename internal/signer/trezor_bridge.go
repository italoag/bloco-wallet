package signer

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type bridgeProtocolMessage struct {
	Protocol string `json:"protocol"`
	Data     string `json:"data,omitempty"`
}

type bridgePacketTransport struct {
	baseURL string
	origin  string
	session string
	client  *http.Client
	pending [][]byte
}

type BridgeDevice struct {
	*UDPDevice
}

func NewBridgeDevice(ctx context.Context, baseURL string) (*BridgeDevice, error) {
	baseURL = strings.TrimSuffix(baseURL, "/")
	parsed, err := url.Parse(baseURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return nil, fmt.Errorf("trezor signer: invalid bridge URL")
	}
	host := parsed.Hostname()
	ip := net.ParseIP(host)
	loopback := host == "localhost" || (ip != nil && ip.IsLoopback())
	if parsed.Scheme == "http" && !loopback {
		return nil, fmt.Errorf("trezor signer: plaintext bridge must be loopback")
	}
	transport := &bridgePacketTransport{
		baseURL: baseURL,
		origin:  "https://python.trezor.io",
		client: &http.Client{
			Timeout:       30 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}
	var configuration struct {
		Version          string `json:"version"`
		ProtocolMessages bool   `json:"protocolMessages"`
	}
	if err := transport.postJSON(ctx, "/configure", nil, &configuration); err != nil {
		return nil, fmt.Errorf("trezor signer: bridge configure: %w", err)
	}
	if configuration.Version == "" || !configuration.ProtocolMessages {
		return nil, fmt.Errorf("trezor signer: bridge protocol messages unsupported")
	}
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

func (transport *bridgePacketTransport) ReadPacket(ctx context.Context) ([]byte, error) {
	if len(transport.pending) > 0 {
		packet := transport.pending[0]
		transport.pending = transport.pending[1:]
		return packet, nil
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

func (transport *bridgePacketTransport) Close() error {
	if transport == nil || transport.session == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost,
		transport.baseURL+"/release/"+url.PathEscape(transport.session), nil)
	if err != nil {
		return err
	}
	request.Header.Set("Origin", transport.origin)
	response, err := transport.client.Do(request)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	transport.session = ""
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("trezor signer: bridge release status %d", response.StatusCode)
	}
	return nil
}

func (transport *bridgePacketTransport) postJSON(ctx context.Context, path string, input any, output any) error {
	var body io.Reader
	if input != nil {
		encoded, err := json.Marshal(input)
		if err != nil {
			return err
		}
		body = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, transport.baseURL+path, body)
	if err != nil {
		return err
	}
	request.Header.Set("Origin", transport.origin)
	if input != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := transport.client.Do(request)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, trezorMaxMessageBytes))
	if err != nil {
		return err
	}
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("status %d: %s", response.StatusCode, strings.TrimSpace(string(responseBody)))
	}
	if output == nil {
		return nil
	}
	if err := json.Unmarshal(responseBody, output); err != nil {
		return err
	}
	return nil
}
