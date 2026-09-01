package signer

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"blocowallet/internal/blockchain"
)

// SpeculosTransport exchanges Ledger APDUs through the bounded Speculos HTTP
// endpoint. It is intended for emulator development and release tests.
type SpeculosTransport struct {
	baseURL string
	gateway *blockchain.RPCGateway
}

// NewSpeculosTransport creates an emulator transport through the RPC gateway.
func NewSpeculosTransport(baseURL string, gateway *blockchain.RPCGateway) (*SpeculosTransport, error) {
	baseURL = strings.TrimSuffix(baseURL, "/")
	parsed, err := url.ParseRequestURI(baseURL)
	if err != nil || parsed.Scheme != "http" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") || gateway == nil {
		return nil, fmt.Errorf("ledger signer: invalid Speculos configuration")
	}
	return &SpeculosTransport{baseURL: baseURL, gateway: gateway}, nil
}

// Exchange implements APDUTransport.
func (transport *SpeculosTransport) Exchange(ctx context.Context, cla, ins, p1, p2 byte, data []byte) ([]byte, uint16, error) {
	if transport == nil || transport.gateway == nil || len(data) > 255 {
		return nil, 0, ErrLedgerTransport
	}
	payload := []byte{cla, ins, p1, p2, byte(len(data))}
	payload = append(payload, data...)
	requestBody, err := json.Marshal(map[string]string{"data": hex.EncodeToString(payload)})
	if err != nil {
		return nil, 0, err
	}
	response, err := transport.gateway.Request(ctx, blockchain.OutboundRequest{
		Method: "POST", URL: transport.baseURL + "/apdu", Body: requestBody,
		Headers: map[string]string{"Content-Type": "application/json"}, MaxResponseBytes: 1 << 20,
	})
	if err != nil {
		return nil, 0, err
	}
	if response.StatusCode != 200 {
		return nil, 0, fmt.Errorf("%w: Speculos status %d", ErrLedgerTransport, response.StatusCode)
	}
	var decodedResponse struct {
		Data string `json:"data"`
	}
	if err := json.Unmarshal(response.Body, &decodedResponse); err != nil {
		return nil, 0, ErrLedgerTransport
	}
	decoded, err := hex.DecodeString(decodedResponse.Data)
	if err != nil || len(decoded) < 2 {
		return nil, 0, ErrLedgerTransport
	}
	status := uint16(decoded[len(decoded)-2])<<8 | uint16(decoded[len(decoded)-1])
	return append([]byte(nil), decoded[:len(decoded)-2]...), status, nil
}
