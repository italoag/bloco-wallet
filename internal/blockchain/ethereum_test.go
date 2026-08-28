package blockchain

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func TestEthereumRejectsProviderChangingChainIdentity(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var payload json.RawMessage
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			http.Error(writer, "bad request", http.StatusBadRequest)
			return
		}
		if len(payload) > 0 && payload[0] == '[' {
			_, _ = fmt.Fprint(writer, `[{"jsonrpc":"2.0","id":1,"result":"0x2"},{"jsonrpc":"2.0","id":2,"result":"0x2a"}]`)
			return
		}
		_, _ = fmt.Fprint(writer, `{"jsonrpc":"2.0","id":1,"result":"0x1"}`)
	}))
	defer server.Close()
	parsed, _ := url.Parse(server.URL)
	gateway := NewRPCGateway(RPCGatewayOptions{AllowedLocalTargets: []string{parsed.Host}})
	provider, err := NewEthereum(context.Background(), gateway, server.URL, 1, time.Second, "ETH", 18)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.GetBalance(context.Background(), "0x0000000000000000000000000000000000000001"); err == nil {
		t.Fatal("provider chain identity change was accepted")
	}
}

func TestEthereumUsesValidatedGatewaySession(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var payload json.RawMessage
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			http.Error(writer, "bad request", http.StatusBadRequest)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		if len(payload) > 0 && payload[0] == '[' {
			_, _ = fmt.Fprint(writer, `[{"jsonrpc":"2.0","id":1,"result":"0x1"},{"jsonrpc":"2.0","id":2,"result":"0x2a"}]`)
			return
		}
		_, _ = fmt.Fprint(writer, `{"jsonrpc":"2.0","id":1,"result":"0x1"}`)
	}))
	defer server.Close()
	parsed, _ := url.Parse(server.URL)
	gateway := NewRPCGateway(RPCGatewayOptions{AllowedLocalTargets: []string{parsed.Host}})
	provider, err := NewEthereum(context.Background(), gateway, server.URL, 1, time.Second, "ETH", 18)
	if err != nil {
		t.Fatal(err)
	}
	balance, err := provider.GetBalance(context.Background(), "0x0000000000000000000000000000000000000001")
	if err != nil {
		t.Fatal(err)
	}
	if balance.Int64() != 42 {
		t.Fatalf("unexpected balance: %s", balance)
	}
}
