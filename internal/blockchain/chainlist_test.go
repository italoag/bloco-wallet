package blockchain

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
)

func TestChainListDecodesObjectAndStringExplorers(t *testing.T) {
	payload := []byte(`[
		{"chainId":1,"name":"Object","nativeCurrency":{"name":"Ether","symbol":"ETH","decimals":18},"rpc":[],"explorers":[{"name":"Explorer","url":"https://object.example.com"}]},
		{"chainId":2,"name":"String","nativeCurrency":{"name":"Coin","symbol":"COIN","decimals":18},"rpc":[],"explorers":["https://string.example.com"]}
	]`)
	var chains []ChainInfo
	if err := json.Unmarshal(payload, &chains); err != nil {
		t.Fatal(err)
	}
	if len(chains) != 2 || len(chains[0].Explorers) != 1 || len(chains[1].Explorers) != 1 || chains[1].Explorers[0].URL != "https://string.example.com" {
		t.Fatalf("explorer variants were not normalized: %+v", chains)
	}
	if err := validateChainCatalog(chains); err != nil {
		t.Fatal(err)
	}
}

func TestChainInfoSanitizesRegistryMetadata(t *testing.T) {
	chain := ChainInfo{Name: "evil\x1b]52;c;secret\x07\r\nnext"}
	chain.NativeCurrency.Name = "coin\u009b31m"
	chain.NativeCurrency.Symbol = "X\a"
	chain.Explorers = append(chain.Explorers, Explorer{Name: "explorer\x1b[31m", URL: "https://example.com"})
	sanitizeChainInfo(&chain)
	for _, value := range []string{chain.Name, chain.NativeCurrency.Name, chain.NativeCurrency.Symbol, chain.Explorers[0].Name} {
		if strings.ContainsAny(value, "\x1b\a\r\n") || strings.Contains(value, "\u009b") {
			t.Fatalf("registry metadata retained terminal controls: %q", value)
		}
	}
}

func TestChainListSearchUsesBoundedCachedCatalog(t *testing.T) {
	var requests atomic.Int32
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		chains := make([]string, 0, 12)
		for index := range 12 {
			chains = append(chains, fmt.Sprintf(`{"chainId":%d,"name":"Network %d","nativeCurrency":{"name":"Coin","symbol":"COIN","decimals":18},"rpc":[{"url":%q,"tracking":"none","isOpenSource":true}]}`, index+1, index+1, server.URL+fmt.Sprintf("/rpc/%d", index+1)))
		}
		_, _ = fmt.Fprintf(writer, "[%s]", strings.Join(chains, ","))
	}))
	defer server.Close()
	parsed, _ := url.Parse(server.URL)
	service := NewChainListServiceWithGateway(NewRPCGateway(RPCGatewayOptions{AllowedLocalTargets: []string{parsed.Host}}), server.URL)
	suggestions, err := service.SearchNetworksByNameContext(context.Background(), "Network")
	if err != nil {
		t.Fatal(err)
	}
	if len(suggestions) != 10 {
		t.Fatalf("search result budget was %d", len(suggestions))
	}
	if _, err := service.GetChainInfoContext(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 1 {
		t.Fatalf("ChainList cache issued %d registry requests", requests.Load())
	}
	if empty, err := service.SearchNetworksByNameContext(context.Background(), " "); err != nil || len(empty) != 0 {
		t.Fatal("empty search did not return an empty result")
	}
}

func TestChainListCatalogAndFanoutPolicies(t *testing.T) {
	if err := validateChainCatalog([]ChainInfo{{ChainID: 1, RPC: []RPCEndpoint{{URL: "https://rpc.example.com", Tracking: "unspecified"}}}}); err != nil {
		t.Fatalf("unknown tracking metadata rejected the catalog: %v", err)
	}
	duplicate := []ChainInfo{{ChainID: 1}, {ChainID: 1}}
	if err := validateChainCatalog(duplicate); err == nil {
		t.Fatal("duplicate chain IDs were accepted")
	}
	service := NewChainListServiceWithGateway(NewRPCGateway(RPCGatewayOptions{}), "https://chainlist.org")
	if _, err := service.findBestRPCEndpoint(context.Background(), make([]RPCEndpoint, 257), 1); err == nil {
		t.Fatal("endpoint fan-out over budget was accepted")
	}
	if _, err := service.findBestRPCEndpoint(context.Background(), []RPCEndpoint{{URL: "https://rpc.example.com", Tracking: "yes"}}, 1); err == nil {
		t.Fatal("tracked endpoints were probed without privacy-preserving option")
	}
}

func TestChainListGatewayFailsClosedOnChainMismatch(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/rpcs.json":
			writer.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(writer, `[{"chainId":1,"name":"Test","nativeCurrency":{"name":"Ether","symbol":"ETH","decimals":18},"rpc":[{"url":%q,"tracking":"none","isOpenSource":true}]}]`, server.URL+"/rpc")
		case "/rpc":
			_, _ = writer.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":"0x2"}`))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()
	parsed, _ := url.Parse(server.URL)
	gateway := NewRPCGateway(RPCGatewayOptions{AllowedLocalTargets: []string{parsed.Host}})
	service := NewChainListServiceWithGateway(gateway, server.URL)
	if _, _, err := service.GetChainInfoWithRetry(1); err == nil {
		t.Fatal("ChainList service fell back after chain mismatch")
	}
}

func TestValidateRPCEndpoint_EmptyURL(t *testing.T) {
	s := NewChainListServiceWithGateway(NewRPCGateway(RPCGatewayOptions{}), "https://chainlist.org")
	err := s.ValidateRPCEndpoint("")
	var opErr *NetworkOperationError
	if !errors.As(err, &opErr) {
		t.Fatalf("expected NetworkOperationError, got %T: %v", err, err)
	}
	if opErr.Operation != "validate" {
		t.Fatalf("expected operation 'validate', got %q", opErr.Operation)
	}
	if opErr.Message == "" {
		t.Fatalf("expected non-empty message")
	}
}

func TestNewNetworkOperationError_ErrorString(t *testing.T) {
	err := NewNetworkOperationError("search", "failed to fetch", assertErr("timeout"))
	if got := err.Error(); got == "" || !containsAll(got, []string{"failed to fetch", "timeout"}) {
		t.Fatalf("unexpected Error(): %q", got)
	}
	unsafe := NewNetworkOperationError("search", "bad\x1b]52;c;secret\x07", assertErr("remote\r\nline"))
	if got := unsafe.Error(); strings.ContainsAny(got, "\x1b\a\r\n") {
		t.Fatalf("network error reached terminal unsanitized: %q", got)
	}
}

// helpers copied from UI tests

type simpleErr string

func (e simpleErr) Error() string { return string(e) }

func assertErr(s string) error { return simpleErr(s) }

func containsAll(hay string, needles []string) bool {
	for _, n := range needles {
		if !stringsContains(hay, n) {
			return false
		}
	}
	return true
}

func stringsContains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
