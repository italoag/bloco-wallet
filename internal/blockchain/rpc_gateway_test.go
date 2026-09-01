package blockchain

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

type blockingIPResolver struct{}

func (blockingIPResolver) LookupNetIP(ctx context.Context, _, _ string) ([]netip.Addr, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

type rotatingIPResolver struct {
	calls atomic.Int32
}

func (resolver *rotatingIPResolver) LookupNetIP(context.Context, string, string) ([]netip.Addr, error) {
	if resolver.calls.Add(1) == 1 {
		return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
	}
	return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
}

type staticIPResolver struct {
	addresses []netip.Addr
	err       error
}

func (resolver staticIPResolver) LookupNetIP(context.Context, string, string) ([]netip.Addr, error) {
	return append([]netip.Addr(nil), resolver.addresses...), resolver.err
}

func TestRPCGatewayOptionsAndHelperErrorPaths(t *testing.T) {
	gateway := NewRPCGateway(RPCGatewayOptions{MaxRPCRequestBytes: 2 << 20, MaxConcurrentRequests: 100, MaxRequestsPerSecond: 1000})
	if gateway.maxRPCRequestBytes != 1<<20 || cap(gateway.requestSemaphore) != 64 || gateway.maxRequestsPerSecond != 256 {
		t.Fatal("gateway option caps were not applied")
	}
	var nilSession *ValidatedRPCSession
	if nilSession.ChainID() != 0 || nilSession.DisplayURL() != "" {
		t.Fatal("nil session metadata is unsafe")
	}
	transportErr := &networkTransportError{message: "safe", cause: context.Canceled}
	if transportErr.Error() != "safe" || !errors.Is(transportErr, context.Canceled) {
		t.Fatal("transport error contract failed")
	}
	if _, err := (*RPCGateway)(nil).ValidateEndpoint(context.Background(), "https://example.com"); err == nil {
		t.Fatal("nil gateway accepted endpoint")
	}
	if _, err := gateway.ValidateChain(context.Background(), "https://example.com", 0); err == nil {
		t.Fatal("non-positive expected chain ID was accepted")
	}
	if err := gateway.Call(context.Background(), nil, "eth_test", nil, nil); err == nil {
		t.Fatal("nil session was accepted")
	}
	foreign := NewRPCGateway(RPCGatewayOptions{})
	if err := gateway.Call(context.Background(), &ValidatedRPCSession{gateway: foreign}, "eth_test", nil, nil); err == nil {
		t.Fatal("foreign session was accepted")
	}
	if err := gateway.Call(context.Background(), &ValidatedRPCSession{gateway: gateway}, "", nil, nil); err == nil {
		t.Fatal("empty RPC method was accepted")
	}
	if err := gateway.Call(context.Background(), &ValidatedRPCSession{gateway: gateway}, strings.Repeat("m", 129), nil, nil); err == nil {
		t.Fatal("oversized RPC method was accepted")
	}
	if _, err := parseRPCQuantity("0x100", 8); err == nil {
		t.Fatal("oversized RPC quantity was accepted")
	}
	if allDecimal("") || allDecimal("12a") || !allDecimal("12") {
		t.Fatal("decimal hostname classifier failed")
	}
	if looksLikeAlternativeIP("rpc.example.com") || !looksLikeAlternativeIP("0x7f000001") || !looksLikeAlternativeIP("0177.0.0.1") {
		t.Fatal("alternative IP classifier failed")
	}
	if isAllowedLocalNodeAddress(netip.Addr{}) || isAllowedLocalNodeAddress(netip.MustParseAddr("93.184.216.34")) || !isAllowedLocalNodeAddress(netip.MustParseAddr("127.0.0.1")) {
		t.Fatal("local node classifier failed")
	}
	if isUnsafeNetworkAddress(netip.MustParseAddr("93.184.216.34")) || !isUnsafeNetworkAddress(netip.MustParseAddr("64:ff9b::1")) || !isUnsafeNetworkAddress(netip.MustParseAddr("64:ff9b:1::7f00:1")) || !isUnsafeNetworkAddress(netip.MustParseAddr("::7f00:1")) {
		t.Fatal("special address classifier failed")
	}
}

func TestRPCGatewayRejectsResolverAndPortErrors(t *testing.T) {
	for _, resolver := range []netIPResolver{
		staticIPResolver{err: errors.New("resolver failed")},
		staticIPResolver{},
		staticIPResolver{addresses: make([]netip.Addr, 17)},
		staticIPResolver{addresses: []netip.Addr{{}}},
		staticIPResolver{addresses: []netip.Addr{netip.MustParseAddr("::ffff:192.0.2.1")}},
	} {
		gateway := NewRPCGateway(RPCGatewayOptions{Resolver: resolver})
		if _, err := gateway.ValidateEndpoint(context.Background(), "https://rpc.example.com"); err == nil {
			t.Fatal("invalid DNS result was accepted")
		}
	}
	gateway := NewRPCGateway(RPCGatewayOptions{Resolver: staticIPResolver{addresses: []netip.Addr{netip.MustParseAddr("93.184.216.34")}}})
	for _, rawURL := range []string{"ftp://example.com", "https://example.com:0", "https://example.com:invalid", "https://[fe80::1%25eth0]"} {
		if _, err := gateway.ValidateEndpoint(context.Background(), rawURL); err == nil {
			t.Fatalf("invalid endpoint %q was accepted", rawURL)
		}
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := gateway.acquire(cancelled); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled admission returned %v", err)
	}
}

func TestRPCQuantityRequiresCanonicalEncoding(t *testing.T) {
	for _, value := range []string{"1", "0X1", "0x", "0x00", "0x01", "0xA", "0xg"} {
		if _, err := parseRPCQuantity(value, 256); err == nil {
			t.Fatalf("non-canonical RPC quantity %q was accepted", value)
		}
	}
	if value, err := parseRPCQuantity("0x0", 256); err != nil || value.Sign() != 0 {
		t.Fatalf("canonical zero was rejected: %v", err)
	}
	if value, err := parseRPCQuantity("0x2a", 256); err != nil || value.Int64() != 42 {
		t.Fatalf("canonical quantity was rejected: %v", err)
	}
}

func TestRPCGatewayRejectsUnsafeDestinations(t *testing.T) {
	privateHosts := []string{
		"127.0.0.1",
		"10.0.0.1",
		"172.16.0.1",
		"192.168.1.1",
		"169.254.169.254",
		"0.0.0.0",
		"0.1.2.3",
		"168.63.129.16",
		"224.0.0.1",
		"[::1]",
		"[fe80::1]",
		"[::ffff:127.0.0.1]",
	}
	gateway := NewRPCGateway(RPCGatewayOptions{})
	for _, host := range privateHosts {
		if _, err := gateway.ValidateEndpoint(context.Background(), "https://"+host); err == nil {
			t.Fatalf("unsafe destination %s was accepted", host)
		}
	}
	publicResolverGateway := NewRPCGateway(RPCGatewayOptions{Resolver: staticIPResolver{addresses: []netip.Addr{netip.MustParseAddr("93.184.216.34")}}})
	if _, err := publicResolverGateway.ValidateEndpoint(context.Background(), "http://example.com"); err == nil {
		t.Fatal("remote plain HTTP endpoint was accepted")
	}
	publicAllowlist := NewRPCGateway(RPCGatewayOptions{Resolver: staticIPResolver{addresses: []netip.Addr{netip.MustParseAddr("93.184.216.34")}}, AllowedLocalTargets: []string{"example.com"}})
	if _, err := publicAllowlist.ValidateEndpoint(context.Background(), "http://example.com"); err == nil {
		t.Fatal("local allowlist enabled HTTP to a public address")
	}
	metadataAllowlist := NewRPCGateway(RPCGatewayOptions{AllowedLocalTargets: []string{"169.254.169.254"}})
	if _, err := metadataAllowlist.ValidateEndpoint(context.Background(), "http://169.254.169.254"); err == nil {
		t.Fatal("local allowlist enabled metadata access")
	}
	for _, rawURL := range []string{
		"https://user:password@example.com",
		"https://example.com/#fragment",
		"https://example.com\x00.invalid",
		"https://2130706433",
		"https://0x7f000001",
		"https://0177.0.0.1",
	} {
		if _, err := publicResolverGateway.ValidateEndpoint(context.Background(), rawURL); err == nil {
			t.Fatalf("malformed endpoint %q was accepted", rawURL)
		}
	}
}

func TestRPCGatewayPinsFirstValidatedDNSAddress(t *testing.T) {
	resolver := &rotatingIPResolver{}
	gateway := NewRPCGateway(RPCGatewayOptions{Resolver: resolver})
	destination, err := gateway.ValidateEndpoint(context.Background(), "https://rpc.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if destination.pinnedIP.String() != "93.184.216.34" || resolver.calls.Load() != 1 {
		t.Fatalf("destination was not pinned to validated DNS result: %s", destination.pinnedIP)
	}
}

func TestRPCGatewayAppliesTimeoutToDNSResolution(t *testing.T) {
	gateway := NewRPCGateway(RPCGatewayOptions{Resolver: blockingIPResolver{}, RequestTimeout: 20 * time.Millisecond})
	started := time.Now()
	if _, err := gateway.ValidateEndpoint(context.Background(), "https://rpc.example.com"); err == nil {
		t.Fatal("blocking DNS resolution succeeded")
	}
	if time.Since(started) > time.Second {
		t.Fatal("DNS resolution ignored gateway timeout")
	}
}

func TestRPCGatewayRejectsDNSResolvingToPrivateAddress(t *testing.T) {
	for _, addresses := range [][]netip.Addr{
		{netip.MustParseAddr("127.0.0.1")},
		{netip.MustParseAddr("93.184.216.34"), netip.MustParseAddr("10.0.0.1")},
	} {
		gateway := NewRPCGateway(RPCGatewayOptions{Resolver: staticIPResolver{addresses: addresses}})
		if _, err := gateway.ValidateEndpoint(context.Background(), "https://rpc.example.com"); err == nil {
			t.Fatal("hostname resolving to a private address was accepted")
		}
	}
}

func TestRPCGatewayValidatesChainOnPinnedLocalSession(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var payload struct {
			Method string `json:"method"`
		}
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			http.Error(writer, "bad request", http.StatusBadRequest)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(writer, `{"jsonrpc":"2.0","id":1,"result":"0x1"}`)
	}))
	defer server.Close()
	parsed, _ := url.Parse(server.URL)
	gateway := NewRPCGateway(RPCGatewayOptions{AllowedLocalTargets: []string{parsed.Host}})
	session, err := gateway.ValidateChain(context.Background(), server.URL, 1)
	if err != nil {
		t.Fatal(err)
	}
	if session.ChainID() != 1 || session.DisplayURL() != "http://<redacted-host>" {
		t.Fatal("validated session metadata mismatch")
	}
	if _, err := gateway.ValidateChain(context.Background(), server.URL, 137); err == nil {
		t.Fatal("chain mismatch was accepted")
	}
}

func TestRPCGatewayEnforcesTLSIdentityAndMinimumVersion(t *testing.T) {
	handler := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = writer.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":"0x1"}`))
	})
	server := httptest.NewTLSServer(handler)
	defer server.Close()
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	parsed, _ := url.Parse(server.URL)
	gateway := NewRPCGateway(RPCGatewayOptions{AllowedLocalTargets: []string{parsed.Host}, TLSRootCAs: roots})
	if _, err := gateway.ValidateChain(context.Background(), server.URL, 1); err != nil {
		t.Fatalf("valid TLS endpoint was rejected: %v", err)
	}
	_, port, _ := net.SplitHostPort(parsed.Host)
	wrongHost := "wrong.invalid:" + port
	wrongIdentityGateway := NewRPCGateway(RPCGatewayOptions{
		Resolver:            staticIPResolver{addresses: []netip.Addr{netip.MustParseAddr("127.0.0.1")}},
		AllowedLocalTargets: []string{wrongHost},
		TLSRootCAs:          roots,
	})
	if _, err := wrongIdentityGateway.ValidateChain(context.Background(), "https://"+wrongHost, 1); err == nil {
		t.Fatal("TLS endpoint with wrong SAN was accepted")
	}
	legacy := httptest.NewUnstartedServer(handler)
	legacy.TLS = &tls.Config{MinVersion: tls.VersionTLS11, MaxVersion: tls.VersionTLS11}
	legacy.StartTLS()
	defer legacy.Close()
	legacyRoots := x509.NewCertPool()
	legacyRoots.AddCert(legacy.Certificate())
	legacyURL, _ := url.Parse(legacy.URL)
	legacyGateway := NewRPCGateway(RPCGatewayOptions{AllowedLocalTargets: []string{legacyURL.Host}, TLSRootCAs: legacyRoots})
	if _, err := legacyGateway.ValidateChain(context.Background(), legacy.URL, 1); err == nil {
		t.Fatal("TLS 1.1 endpoint was accepted")
	}
}

func TestRPCGatewayRejectsRedirectsAndOversizedResponses(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/redirect":
			http.Redirect(writer, request, server.URL+"/rpc", http.StatusTemporaryRedirect)
		case "/large":
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":"` + strings.Repeat("0", 4096) + `"}`))
		case "/compressed":
			writer.Header().Set("Content-Encoding", "gzip")
			_, _ = writer.Write([]byte("compressed"))
		case "/status":
			http.Error(writer, "failure", http.StatusInternalServerError)
		case "/length":
			writer.Header().Set("Content-Length", "4096")
		default:
			_, _ = writer.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":"0x1"}`))
		}
	}))
	defer server.Close()
	parsed, _ := url.Parse(server.URL)
	gateway := NewRPCGateway(RPCGatewayOptions{AllowedLocalTargets: []string{parsed.Host}, MaxRPCResponseBytes: 256})
	if _, err := gateway.ChainID(context.Background(), server.URL+"/redirect"); err == nil {
		t.Fatal("redirect was accepted")
	}
	if _, err := gateway.ChainID(context.Background(), server.URL+"/large"); err == nil {
		t.Fatal("oversized RPC response was accepted")
	}
	if _, err := gateway.ChainID(context.Background(), server.URL+"/compressed"); err == nil {
		t.Fatal("compressed RPC response was accepted")
	}
	if _, err := gateway.ChainID(context.Background(), server.URL+"/status"); err == nil {
		t.Fatal("error HTTP status was accepted")
	}
	if _, err := gateway.ChainID(context.Background(), server.URL+"/length"); err == nil {
		t.Fatal("oversized Content-Length was accepted")
	}
}

func TestRPCGatewayRejectsMalformedJSONRPCResponses(t *testing.T) {
	var response atomic.Value
	response.Store(`{"jsonrpc":"2.0","id":1,"result":"0x1"}`)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = writer.Write([]byte(response.Load().(string)))
	}))
	defer server.Close()
	parsed, _ := url.Parse(server.URL)
	gateway := NewRPCGateway(RPCGatewayOptions{AllowedLocalTargets: []string{parsed.Host}, MaxRequestsPerSecond: 256})
	invalidResponses := []string{
		`{"jsonrpc":"2.0","id":2,"result":"0x1"}`,
		`{"jsonrpc":"2.0","id":1,"id":1,"result":"0x1"}`,
		`{"jsonrpc":"2.0","id":1,"result":"0x1","error":{"code":-1,"message":"bad"}}`,
		`{"jsonrpc":"2.0","id":1,"result":"0x01"}`,
		`{"jsonrpc":"2.0","id":1,"result":"0x1"} {}`,
	}
	for _, invalid := range invalidResponses {
		response.Store(invalid)
		if _, err := gateway.ChainID(context.Background(), server.URL); err == nil {
			t.Fatalf("malformed RPC response was accepted: %s", invalid)
		}
	}
	response.Store(`{"jsonrpc":"2.0","id":1,"error":{"code":-32000,"message":"invalid token reflected-super-secret"}}`)
	_, err := gateway.ChainID(context.Background(), server.URL)
	if err == nil || strings.Contains(err.Error(), "reflected-super-secret") {
		t.Fatalf("remote RPC message crossed redaction boundary: %v", err)
	}
}

func TestRPCGatewayRejectsMalformedBatchResponses(t *testing.T) {
	var response atomic.Value
	response.Store(`[{"jsonrpc":"2.0","id":1,"result":"0x1"},{"jsonrpc":"2.0","id":2,"result":"0x2a"}]`)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = writer.Write([]byte(response.Load().(string)))
	}))
	defer server.Close()
	parsed, _ := url.Parse(server.URL)
	gateway := NewRPCGateway(RPCGatewayOptions{AllowedLocalTargets: []string{parsed.Host}, MaxRequestsPerSecond: 256})
	destination, err := gateway.ValidateEndpoint(context.Background(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	invalidResponses := []string{
		`[{"jsonrpc":"2.0","id":1,"result":"0x1"}]`,
		`[{"jsonrpc":"2.0","id":1,"result":"0x1"},{"jsonrpc":"2.0","id":1,"result":"0x2a"}]`,
		`[{"jsonrpc":"2.0","id":1,"result":"0x1"},{"jsonrpc":"2.0","id":3,"result":"0x2a"}]`,
		`[{"jsonrpc":"2.0","id":1,"result":"0x2"},{"jsonrpc":"2.0","id":2,"result":"0x2a"}]`,
		`[{"jsonrpc":"2.0","id":1,"result":"0x1"},{"jsonrpc":"2.0","id":2,"error":{"code":0,"message":""}}]`,
		`[{"jsonrpc":"2.0","id":1,"result":"0x1"},{"jsonrpc":"2.0","id":2,"result":"bad"}] trailing`,
	}
	for _, invalid := range invalidResponses {
		response.Store(invalid)
		var result string
		if err := gateway.callValidatedDestination(context.Background(), destination, 1, "eth_test", []any{}, &result); err == nil {
			t.Fatalf("malformed RPC batch was accepted: %s", invalid)
		}
	}
	response.Store(`[{"jsonrpc":"2.0","id":1,"result":"0x1"},{"jsonrpc":"2.0","id":2,"error":{"code":-1,"message":"remote failure"}}]`)
	if err := gateway.callValidatedDestination(context.Background(), destination, 1, "eth_test", []any{}, nil); err == nil || strings.Contains(err.Error(), "remote failure") {
		t.Fatalf("RPC batch error message crossed redaction boundary: %v", err)
	}
	response.Store(`[{"jsonrpc":"2.0","id":2,"result":"0x2a"},{"jsonrpc":"2.0","id":1,"result":"0x1"}]`)
	var result string
	if err := gateway.callValidatedDestination(context.Background(), destination, 1, "eth_test", []any{}, &result); err != nil || result != "0x2a" {
		t.Fatalf("valid out-of-order batch failed: %v, %s", err, result)
	}
}

func TestRPCGatewayRejectsOversizedRequestBeforeNetwork(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		_, _ = writer.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":"0x1"}`))
	}))
	defer server.Close()
	parsed, _ := url.Parse(server.URL)
	gateway := NewRPCGateway(RPCGatewayOptions{AllowedLocalTargets: []string{parsed.Host}, MaxRPCRequestBytes: 256})
	session, err := gateway.ValidateChain(context.Background(), server.URL, 1)
	if err != nil {
		t.Fatal(err)
	}
	before := requests.Load()
	if err := gateway.Call(context.Background(), session, "eth_test", []any{strings.Repeat("x", 1024)}, nil); err == nil {
		t.Fatal("oversized RPC request was accepted")
	}
	if requests.Load() != before {
		t.Fatal("oversized RPC request reached network")
	}
}

func TestRPCGatewayCancellationReachesRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		<-request.Context().Done()
	}))
	defer server.Close()
	parsed, _ := url.Parse(server.URL)
	gateway := NewRPCGateway(RPCGatewayOptions{AllowedLocalTargets: []string{parsed.Host}, RequestTimeout: time.Minute})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := gateway.ChainID(ctx, server.URL); err == nil {
		t.Fatal("cancelled request succeeded")
	}
}

func TestRPCGatewayRateLimitIncludesDestinationValidation(t *testing.T) {
	gateway := NewRPCGateway(RPCGatewayOptions{
		Resolver:             staticIPResolver{addresses: []netip.Addr{netip.MustParseAddr("93.184.216.34")}},
		RequestTimeout:       20 * time.Millisecond,
		MaxRequestsPerSecond: 1,
	})
	if _, err := gateway.ValidateEndpoint(context.Background(), "https://rpc.example.com"); err != nil {
		t.Fatal(err)
	}
	if _, err := gateway.ValidateEndpoint(context.Background(), "https://rpc.example.com"); err == nil {
		t.Fatal("gateway rate limit allowed an immediate second validation")
	}
}

func TestRPCGatewayLimitsConcurrentRequests(t *testing.T) {
	release := make(chan struct{})
	entered := make(chan struct{}, 3)
	var current atomic.Int32
	var maximum atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		active := current.Add(1)
		defer current.Add(-1)
		for {
			observed := maximum.Load()
			if active <= observed || maximum.CompareAndSwap(observed, active) {
				break
			}
		}
		entered <- struct{}{}
		<-release
		_, _ = writer.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":"0x1"}`))
	}))
	defer server.Close()
	parsed, _ := url.Parse(server.URL)
	gateway := NewRPCGateway(RPCGatewayOptions{AllowedLocalTargets: []string{parsed.Host}, MaxConcurrentRequests: 2})
	var waitGroup sync.WaitGroup
	waitGroup.Add(3)
	for range 3 {
		go func() {
			defer waitGroup.Done()
			_, _ = gateway.ChainID(context.Background(), server.URL)
		}()
	}
	for range 2 {
		select {
		case <-entered:
		case <-time.After(time.Second):
			t.Fatal("requests did not reach concurrency budget")
		}
	}
	select {
	case <-entered:
		t.Fatal("gateway exceeded concurrent request budget")
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	waitGroup.Wait()
	if maximum.Load() > 2 {
		t.Fatalf("maximum concurrency was %d", maximum.Load())
	}
}

func TestRPCGatewayBoundedOutboundRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.Header.Get("Authorization") != "Bearer test" || request.Header.Get("Origin") != "https://python.trezor.io" {
			t.Error("outbound request binding changed")
		}
		writer.WriteHeader(http.StatusForbidden)
		_, _ = writer.Write([]byte(`{"error":"denied"}`))
	}))
	defer server.Close()
	parsed, _ := url.Parse(server.URL)
	gateway := NewRPCGateway(RPCGatewayOptions{AllowedLocalTargets: []string{parsed.Host}})
	response, err := gateway.Request(context.Background(), OutboundRequest{
		Method: http.MethodPost, URL: server.URL, Body: []byte(`{"digest":"0x01"}`),
		Headers:          map[string]string{"Authorization": "Bearer test", "Origin": "https://python.trezor.io"},
		MaxResponseBytes: 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusForbidden || string(response.Body) != `{"error":"denied"}` {
		t.Fatalf("unexpected outbound response: %+v", response)
	}
	if _, err := gateway.Request(context.Background(), OutboundRequest{
		Method: http.MethodPost, URL: server.URL,
		Headers: map[string]string{"Host": "attacker.invalid"},
	}); err == nil {
		t.Fatal("forbidden outbound header was accepted")
	}
	if _, err := gateway.Request(context.Background(), OutboundRequest{
		Method: "DELETE", URL: server.URL,
	}); err == nil {
		t.Fatal("unsupported outbound method was accepted")
	}
}

func TestRPCGatewayHonorsBoundedPerRequestTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		time.Sleep(75 * time.Millisecond)
		_, _ = writer.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()
	parsed, _ := url.Parse(server.URL)
	gateway := NewRPCGateway(RPCGatewayOptions{AllowedLocalTargets: []string{parsed.Host}, RequestTimeout: 20 * time.Millisecond})
	response, err := gateway.Request(context.Background(), OutboundRequest{
		Method: http.MethodGet, URL: server.URL, MaxResponseBytes: 1024, Timeout: 500 * time.Millisecond,
	})
	if err != nil || response.StatusCode != http.StatusOK {
		t.Fatalf("per-request timeout was truncated: status=%d err=%v", response.StatusCode, err)
	}
}

func TestRPCGatewayTracksCanceledSideEffectAsNotSent(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		_, _ = writer.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":true}`))
	}))
	defer server.Close()
	parsed, _ := url.Parse(server.URL)
	gateway := NewRPCGateway(RPCGatewayOptions{AllowedLocalTargets: []string{parsed.Host}})
	destination, err := gateway.ValidateEndpoint(context.Background(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	sent, err := gateway.callDestinationTracked(ctx, destination, "eth_sendRawTransaction", []any{"0x01"}, nil)
	if err == nil || sent || requests.Load() != 0 {
		t.Fatalf("canceled side effect classification: sent=%t requests=%d err=%v", sent, requests.Load(), err)
	}
}

func TestRPCGatewayDialsPinnedWebSocket(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		connection, err := upgrader.Upgrade(writer, request, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		defer func() { _ = connection.Close() }()
		messageType, message, err := connection.ReadMessage()
		if err != nil {
			return
		}
		_ = connection.WriteMessage(messageType, message)
	}))
	defer server.Close()
	parsed, _ := url.Parse(server.URL)
	gateway := NewRPCGateway(RPCGatewayOptions{AllowedLocalTargets: []string{parsed.Host}})
	websocketURL := "ws" + strings.TrimPrefix(server.URL, "http")
	connection, err := gateway.DialWebSocket(context.Background(), websocketURL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = connection.Close() }()
	if err := connection.WriteMessage(websocket.TextMessage, []byte("ping")); err != nil {
		t.Fatal(err)
	}
	_, response, err := connection.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	if string(response) != "ping" {
		t.Fatalf("unexpected WebSocket response %q", response)
	}
}

func TestRPCGatewayRejectsOversizedWebSocketHandshake(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		header := http.Header{"X-Oversized": []string{strings.Repeat("a", 70<<10)}}
		connection, err := upgrader.Upgrade(writer, request, header)
		if err == nil {
			_ = connection.Close()
		}
	}))
	defer server.Close()
	parsed, _ := url.Parse(server.URL)
	gateway := NewRPCGateway(RPCGatewayOptions{AllowedLocalTargets: []string{parsed.Host}})
	websocketURL := "ws" + strings.TrimPrefix(server.URL, "http")
	if connection, err := gateway.DialWebSocket(context.Background(), websocketURL); err == nil {
		_ = connection.Close()
		t.Fatal("oversized WebSocket handshake was accepted")
	}
}

func TestRPCGatewayTransportErrorDoesNotLeakURLCredentials(t *testing.T) {
	gateway := NewRPCGateway(RPCGatewayOptions{
		Resolver: staticIPResolver{addresses: []netip.Addr{netip.MustParseAddr("93.184.216.34")}},
		DialContext: func(context.Context, string, string) (net.Conn, error) {
			return nil, errors.New("dial failed")
		},
	})
	_, err := gateway.ChainID(context.Background(), "https://rpc.example.com/v3/super-secret?key=token")
	if err == nil || strings.Contains(err.Error(), "super-secret") || strings.Contains(err.Error(), "key=token") {
		t.Fatalf("transport error leaked URL credentials: %v", err)
	}
	for cause := err; cause != nil; cause = errors.Unwrap(cause) {
		if strings.Contains(cause.Error(), "super-secret") || strings.Contains(cause.Error(), "key=token") {
			t.Fatalf("transport error cause leaked URL credentials: %v", cause)
		}
	}
	var urlError *url.Error
	if errors.As(err, &urlError) {
		t.Fatalf("transport error retained credential-bearing URL: %v", urlError)
	}
}

func FuzzRPCGatewayValidateEndpoint(f *testing.F) {
	f.Add("https://example.com")
	f.Add("http://127.0.0.1")
	f.Fuzz(func(t *testing.T, rawURL string) {
		gateway := NewRPCGateway(RPCGatewayOptions{Resolver: staticIPResolver{addresses: []netip.Addr{netip.MustParseAddr("93.184.216.34")}}})
		_, _ = gateway.ValidateEndpoint(context.Background(), rawURL)
	})
}

func FuzzRPCJSONDuplicateKeyValidation(f *testing.F) {
	f.Add([]byte(`{"jsonrpc":"2.0","id":1,"result":"0x1"}`))
	f.Add([]byte(`{"id":1,"id":2}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > defaultRPCResponseBytes {
			return
		}
		_ = rejectDuplicateRPCJSONKeys(data)
	})
}

func TestRedactNetworkURLRemovesCredentialsAndPath(t *testing.T) {
	redacted := RedactNetworkURL("https://user:password@rpc.example.com/v3/token?key=secret#fragment")
	if redacted != "https://<redacted-host>" {
		t.Fatalf("unexpected redacted URL: %s", redacted)
	}
	if redacted := RedactNetworkURL("not a URL secret"); redacted != "<invalid-network-url>" {
		t.Fatalf("invalid URL was echoed: %s", redacted)
	}
}
