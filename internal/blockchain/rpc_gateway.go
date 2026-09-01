package blockchain

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

const (
	defaultRPCResponseBytes      = 64 << 10
	defaultRegistryResponseBytes = 8 << 20
	defaultRequestTimeout        = 10 * time.Second
	hardRPCResponseBytes         = 1 << 20
	hardRegistryResponseBytes    = 16 << 20
	hardRequestTimeout           = 5 * time.Minute
)

type netIPResolver interface {
	LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error)
}

type RPCGatewayOptions struct {
	Resolver                 netIPResolver
	Random                   io.Reader
	DialContext              func(context.Context, string, string) (net.Conn, error)
	AllowedLocalTargets      []string
	RequestTimeout           time.Duration
	MaxRPCRequestBytes       int
	MaxRPCResponseBytes      int64
	MaxRegistryResponseBytes int64
	MaxConcurrentRequests    int
	MaxRequestsPerSecond     int
	TLSRootCAs               *x509.CertPool
}

// OutboundRequest describes a bounded HTTP request through the gateway.
type OutboundRequest struct {
	Method           string
	URL              string
	Headers          map[string]string
	Body             []byte
	MaxResponseBytes int64
	Timeout          time.Duration
}

// OutboundResponse contains a bounded HTTP response.
type OutboundResponse struct {
	StatusCode int
	Body       []byte
}

type RPCErrorKind string

const (
	RPCErrorUnknown                RPCErrorKind = "unknown"
	RPCErrorMethodNotFound         RPCErrorKind = "method_not_found"
	RPCErrorExecutionReverted      RPCErrorKind = "execution_reverted"
	RPCErrorAlreadyKnown           RPCErrorKind = "already_known"
	RPCErrorNonceTooLow            RPCErrorKind = "nonce_too_low"
	RPCErrorInsufficientFunds      RPCErrorKind = "insufficient_funds"
	RPCErrorReplacementUnderpriced RPCErrorKind = "replacement_underpriced"
)

type RPCRemoteError struct {
	Code int
	Kind RPCErrorKind
	Data []byte
}

func (remoteError *RPCRemoteError) Error() string {
	if remoteError == nil {
		return "RPC remote error"
	}
	return fmt.Sprintf("RPC remote error code %d (%s)", remoteError.Code, remoteError.Kind)
}

type safeNetworkError struct {
	timeout   bool
	temporary bool
}

func (networkError *safeNetworkError) Error() string   { return "network transport failed" }
func (networkError *safeNetworkError) Timeout() bool   { return networkError.timeout }
func (networkError *safeNetworkError) Temporary() bool { return networkError.temporary }

type networkTransportError struct {
	message string
	cause   error
}

func (transportError *networkTransportError) Error() string {
	return transportError.message
}

func (transportError *networkTransportError) Unwrap() error {
	return transportError.cause
}

type RPCGateway struct {
	resolver                 netIPResolver
	random                   io.Reader
	dialContext              func(context.Context, string, string) (net.Conn, error)
	allowedLocalTargets      map[string]struct{}
	requestTimeout           time.Duration
	maxRPCRequestBytes       int
	maxRPCResponseBytes      int64
	maxRegistryResponseBytes int64
	requestSemaphore         chan struct{}
	webSocketSemaphore       chan struct{}
	maxRequestsPerSecond     int
	rateMu                   sync.Mutex
	requestTimes             []time.Time
	tlsRootCAs               *x509.CertPool
}

type validatedDestination struct {
	rawURL   string
	parsed   *url.URL
	pinnedIP netip.Addr
	hostPort string
	local    bool
}

type rpcResponseEnvelope struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *struct {
		Code    int             `json:"code"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	} `json:"error"`
}

type RPCSessionID [32]byte

type ValidatedRPCSession struct {
	gateway     *RPCGateway
	destination validatedDestination
	chainID     int64
	id          RPCSessionID
}

func NewRPCGateway(options RPCGatewayOptions) *RPCGateway {
	resolver := options.Resolver
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	random := options.Random
	if random == nil {
		random = rand.Reader
	}
	dialContext := options.DialContext
	if dialContext == nil {
		dialer := &net.Dialer{Timeout: defaultRequestTimeout, KeepAlive: 30 * time.Second}
		dialContext = dialer.DialContext
	}
	requestTimeout := options.RequestTimeout
	if requestTimeout <= 0 {
		requestTimeout = defaultRequestTimeout
	}
	if requestTimeout > hardRequestTimeout {
		requestTimeout = hardRequestTimeout
	}
	maxRPCRequestBytes := options.MaxRPCRequestBytes
	if maxRPCRequestBytes <= 0 {
		maxRPCRequestBytes = 64 << 10
	}
	if maxRPCRequestBytes > 1<<20 {
		maxRPCRequestBytes = 1 << 20
	}
	maxRPCResponseBytes := options.MaxRPCResponseBytes
	if maxRPCResponseBytes <= 0 {
		maxRPCResponseBytes = defaultRPCResponseBytes
	}
	if maxRPCResponseBytes > hardRPCResponseBytes {
		maxRPCResponseBytes = hardRPCResponseBytes
	}
	maxRegistryResponseBytes := options.MaxRegistryResponseBytes
	if maxRegistryResponseBytes <= 0 {
		maxRegistryResponseBytes = defaultRegistryResponseBytes
	}
	if maxRegistryResponseBytes > hardRegistryResponseBytes {
		maxRegistryResponseBytes = hardRegistryResponseBytes
	}
	if maxRegistryResponseBytes < maxRPCResponseBytes {
		maxRegistryResponseBytes = maxRPCResponseBytes
	}
	maxConcurrentRequests := options.MaxConcurrentRequests
	if maxConcurrentRequests <= 0 {
		maxConcurrentRequests = 16
	}
	if maxConcurrentRequests > 64 {
		maxConcurrentRequests = 64
	}
	maxRequestsPerSecond := options.MaxRequestsPerSecond
	if maxRequestsPerSecond <= 0 {
		maxRequestsPerSecond = 32
	}
	if maxRequestsPerSecond > 256 {
		maxRequestsPerSecond = 256
	}
	allowedTargets := make(map[string]struct{}, len(options.AllowedLocalTargets))
	for _, target := range options.AllowedLocalTargets {
		allowedTargets[strings.ToLower(target)] = struct{}{}
	}
	return &RPCGateway{
		resolver:                 resolver,
		random:                   random,
		dialContext:              dialContext,
		allowedLocalTargets:      allowedTargets,
		requestTimeout:           requestTimeout,
		maxRPCRequestBytes:       maxRPCRequestBytes,
		maxRPCResponseBytes:      maxRPCResponseBytes,
		maxRegistryResponseBytes: maxRegistryResponseBytes,
		requestSemaphore:         make(chan struct{}, maxConcurrentRequests),
		webSocketSemaphore:       make(chan struct{}, maxConcurrentRequests),
		maxRequestsPerSecond:     maxRequestsPerSecond,
		requestTimes:             make([]time.Time, 0, maxRequestsPerSecond),
		tlsRootCAs:               options.TLSRootCAs,
	}
}

func RedactNetworkURL(rawURL string) string {
	parsed, err := url.ParseRequestURI(rawURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "<invalid-network-url>"
	}
	return strings.ToLower(parsed.Scheme) + "://<redacted-host>"
}

func (session *ValidatedRPCSession) ChainID() int64 {
	if session == nil {
		return 0
	}
	return session.chainID
}

func (session *ValidatedRPCSession) DisplayURL() string {
	if session == nil {
		return ""
	}
	return RedactNetworkURL(session.destination.rawURL)
}

func (session *ValidatedRPCSession) ID() RPCSessionID {
	if session == nil {
		return RPCSessionID{}
	}
	return session.id
}

func (gateway *RPCGateway) ValidateEndpoint(ctx context.Context, rawURL string) (validatedDestination, error) {
	if gateway == nil {
		return validatedDestination{}, fmt.Errorf("RPC gateway is required")
	}
	validationContext, cancel := context.WithTimeout(ctx, gateway.requestTimeout)
	defer cancel()
	if err := gateway.acquire(validationContext); err != nil {
		return validatedDestination{}, err
	}
	defer gateway.release()
	ctx = validationContext
	if len(rawURL) == 0 || len(rawURL) > 4096 || strings.ContainsAny(rawURL, "\x00\r\n\x1b#") {
		return validatedDestination{}, fmt.Errorf("invalid network URL")
	}
	parsed, err := url.ParseRequestURI(rawURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.Opaque != "" {
		return validatedDestination{}, fmt.Errorf("invalid network URL")
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return validatedDestination{}, fmt.Errorf("unsupported network URL scheme")
	}
	if parsed.User != nil || parsed.Fragment != "" {
		return validatedDestination{}, fmt.Errorf("network URL cannot contain userinfo or fragment")
	}
	hostname := parsed.Hostname()
	if hostname == "" || strings.Contains(hostname, "%") || allDecimal(hostname) || looksLikeAlternativeIP(hostname) {
		return validatedDestination{}, fmt.Errorf("invalid network host")
	}
	port := parsed.Port()
	if port == "" {
		if parsed.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	portNumber, err := strconv.ParseUint(port, 10, 16)
	if err != nil || portNumber == 0 {
		return validatedDestination{}, fmt.Errorf("invalid network port")
	}
	hostPort := net.JoinHostPort(strings.ToLower(hostname), port)
	_, localAllowed := gateway.allowedLocalTargets[strings.ToLower(parsed.Host)]
	addresses, err := gateway.resolveAddresses(ctx, hostname)
	if err != nil {
		return validatedDestination{}, &networkTransportError{message: "resolve network host " + RedactNetworkURL(rawURL) + " failed", cause: safeTransportCause(err)}
	}
	for _, address := range addresses {
		if localAllowed {
			if !isAllowedLocalNodeAddress(address) {
				return validatedDestination{}, fmt.Errorf("allowed local target resolved outside local-node policy")
			}
		} else if isUnsafeNetworkAddress(address) {
			return validatedDestination{}, fmt.Errorf("network destination is blocked")
		}
	}
	if parsed.Scheme == "http" && !localAllowed {
		return validatedDestination{}, fmt.Errorf("plain HTTP requires an explicitly allowed local target")
	}
	return validatedDestination{rawURL: rawURL, parsed: parsed, pinnedIP: addresses[0], hostPort: hostPort, local: localAllowed}, nil
}

func (gateway *RPCGateway) resolveAddresses(ctx context.Context, hostname string) ([]netip.Addr, error) {
	if address, err := netip.ParseAddr(hostname); err == nil {
		if address.Is4In6() {
			return nil, fmt.Errorf("IPv4-mapped IPv6 is not allowed")
		}
		return []netip.Addr{address}, nil
	}
	addresses, err := gateway.resolver.LookupNetIP(ctx, "ip", hostname)
	if err != nil {
		return nil, err
	}
	if len(addresses) == 0 || len(addresses) > 16 {
		return nil, fmt.Errorf("network host resolved outside address budget")
	}
	for _, address := range addresses {
		if !address.IsValid() || address.Is4In6() {
			return nil, fmt.Errorf("invalid resolved address")
		}
	}
	return addresses, nil
}

func (gateway *RPCGateway) ValidateChain(ctx context.Context, rawURL string, expectedChainID int64) (*ValidatedRPCSession, error) {
	if expectedChainID <= 0 {
		return nil, fmt.Errorf("expected chain ID must be positive")
	}
	destination, err := gateway.ValidateEndpoint(ctx, rawURL)
	if err != nil {
		return nil, err
	}
	chainID, err := gateway.chainIDForDestination(ctx, destination)
	if err != nil {
		return nil, err
	}
	if chainID != expectedChainID {
		return nil, fmt.Errorf("RPC chain ID mismatch: expected %d, received %d", expectedChainID, chainID)
	}
	var sessionID RPCSessionID
	if _, err := io.ReadFull(gateway.random, sessionID[:]); err != nil {
		return nil, fmt.Errorf("generate RPC session identity")
	}
	return &ValidatedRPCSession{gateway: gateway, destination: destination, chainID: chainID, id: sessionID}, nil
}

func (gateway *RPCGateway) ChainID(ctx context.Context, rawURL string) (int64, error) {
	destination, err := gateway.ValidateEndpoint(ctx, rawURL)
	if err != nil {
		return 0, err
	}
	return gateway.chainIDForDestination(ctx, destination)
}

func (gateway *RPCGateway) chainIDForDestination(ctx context.Context, destination validatedDestination) (int64, error) {
	var result string
	if err := gateway.callDestination(ctx, destination, "eth_chainId", []any{}, &result); err != nil {
		return 0, err
	}
	chainID, err := parseRPCQuantity(result, 63)
	if err != nil || chainID.Sign() <= 0 {
		return 0, fmt.Errorf("RPC returned an invalid chain ID")
	}
	return chainID.Int64(), nil
}

func (gateway *RPCGateway) Call(ctx context.Context, session *ValidatedRPCSession, method string, params any, result any) error {
	if session == nil || session.gateway != gateway {
		return fmt.Errorf("validated RPC session is required")
	}
	if method == "" || len(method) > 128 {
		return fmt.Errorf("invalid RPC method")
	}
	if method == "eth_chainId" {
		return gateway.callDestination(ctx, session.destination, method, params, result)
	}
	return gateway.callValidatedDestination(ctx, session.destination, session.chainID, method, params, result)
}

func (gateway *RPCGateway) callSideEffect(ctx context.Context, session *ValidatedRPCSession, method string, params any, result any) (bool, error) {
	if session == nil || session.gateway != gateway || method == "" || len(method) > 128 {
		return false, fmt.Errorf("validated RPC side-effect session is required")
	}
	chainID, err := gateway.chainIDForDestination(ctx, session.destination)
	if err != nil {
		return false, err
	}
	if chainID != session.chainID {
		return false, fmt.Errorf("RPC chain identity changed")
	}
	return gateway.callDestinationTracked(ctx, session.destination, method, params, result)
}

func (gateway *RPCGateway) callDestination(ctx context.Context, destination validatedDestination, method string, params any, result any) error {
	_, err := gateway.callDestinationTracked(ctx, destination, method, params, result)
	return err
}

func (gateway *RPCGateway) callDestinationTracked(ctx context.Context, destination validatedDestination, method string, params any, result any) (bool, error) {
	requestPayload, err := json.Marshal(struct {
		JSONRPC string `json:"jsonrpc"`
		Method  string `json:"method"`
		Params  any    `json:"params"`
		ID      int    `json:"id"`
	}{JSONRPC: "2.0", Method: method, Params: params, ID: 1})
	if err != nil {
		return false, fmt.Errorf("encode RPC request")
	}
	if len(requestPayload) > gateway.maxRPCRequestBytes {
		return false, fmt.Errorf("RPC request exceeds size policy")
	}
	response, sent, err := gateway.doResponseTracked(ctx, destination, http.MethodPost, requestPayload, nil, gateway.maxRPCResponseBytes, gateway.requestTimeout)
	if err != nil {
		return sent, err
	}
	responseBody := response.Body
	if err := rejectDuplicateRPCJSONKeys(responseBody); err != nil {
		return true, fmt.Errorf("RPC response contains duplicate or invalid JSON keys")
	}
	var envelope rpcResponseEnvelope
	decoder := json.NewDecoder(bytes.NewReader(responseBody))
	if err := decoder.Decode(&envelope); err != nil {
		return true, fmt.Errorf("decode RPC response: %w", err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return true, err
	}
	if envelope.JSONRPC != "2.0" || string(envelope.ID) != "1" || (envelope.Error == nil) == (len(envelope.Result) == 0 || string(envelope.Result) == "null") {
		return true, fmt.Errorf("RPC response shape is invalid")
	}
	if envelope.Error != nil {
		if envelope.Error.Code == 0 || envelope.Error.Message == "" || len(envelope.Error.Message) > 4096 {
			return true, fmt.Errorf("RPC error object is invalid")
		}
		return true, newRPCRemoteError(envelope.Error.Code, envelope.Error.Message, envelope.Error.Data)
	}
	if result == nil {
		return true, nil
	}
	if err := json.Unmarshal(envelope.Result, result); err != nil {
		return true, fmt.Errorf("decode RPC result: %w", err)
	}
	return true, nil
}

func (gateway *RPCGateway) callValidatedDestination(ctx context.Context, destination validatedDestination, expectedChainID int64, method string, params any, result any) error {
	_, err := gateway.callValidatedDestinationMode(ctx, destination, expectedChainID, method, params, result, false)
	return err
}

func (gateway *RPCGateway) callValidatedNullable(ctx context.Context, session *ValidatedRPCSession, method string, params any, result any) (bool, error) {
	if session == nil || session.gateway != gateway {
		return false, fmt.Errorf("validated RPC session is required")
	}
	return gateway.callValidatedDestinationMode(ctx, session.destination, session.chainID, method, params, result, true)
}

func (gateway *RPCGateway) callValidatedDestinationMode(ctx context.Context, destination validatedDestination, expectedChainID int64, method string, params any, result any, allowNull bool) (bool, error) {
	requestPayload, err := json.Marshal([]struct {
		JSONRPC string `json:"jsonrpc"`
		Method  string `json:"method"`
		Params  any    `json:"params"`
		ID      int    `json:"id"`
	}{
		{JSONRPC: "2.0", Method: "eth_chainId", Params: []any{}, ID: 1},
		{JSONRPC: "2.0", Method: method, Params: params, ID: 2},
	})
	if err != nil {
		return false, fmt.Errorf("encode RPC request")
	}
	if len(requestPayload) > gateway.maxRPCRequestBytes {
		return false, fmt.Errorf("RPC request exceeds size policy")
	}
	responseBody, err := gateway.do(ctx, destination, http.MethodPost, requestPayload, gateway.maxRPCResponseBytes)
	if err != nil {
		return false, err
	}
	if err := rejectDuplicateRPCJSONKeys(responseBody); err != nil {
		return false, fmt.Errorf("RPC response contains duplicate or invalid JSON keys")
	}
	var responses []rpcResponseEnvelope
	decoder := json.NewDecoder(bytes.NewReader(responseBody))
	if err := decoder.Decode(&responses); err != nil {
		return false, fmt.Errorf("decode RPC batch response: %w", err)
	}
	if err := ensureJSONEOF(decoder); err != nil || len(responses) != 2 {
		return false, fmt.Errorf("RPC batch response shape is invalid")
	}
	var chainResponse *rpcResponseEnvelope
	var methodResponse *rpcResponseEnvelope
	for index := range responses {
		switch string(responses[index].ID) {
		case "1":
			if chainResponse != nil {
				return false, fmt.Errorf("RPC batch response contains duplicate IDs")
			}
			chainResponse = &responses[index]
		case "2":
			if methodResponse != nil {
				return false, fmt.Errorf("RPC batch response contains duplicate IDs")
			}
			methodResponse = &responses[index]
		default:
			return false, fmt.Errorf("RPC batch response contains an unexpected ID")
		}
	}
	if chainResponse == nil || methodResponse == nil || chainResponse.JSONRPC != "2.0" || methodResponse.JSONRPC != "2.0" || chainResponse.Error != nil || len(chainResponse.Result) == 0 {
		return false, fmt.Errorf("RPC batch response shape is invalid")
	}
	var encodedChainID string
	if err := json.Unmarshal(chainResponse.Result, &encodedChainID); err != nil {
		return false, fmt.Errorf("RPC chain identity result is invalid")
	}
	chainID, err := parseRPCQuantity(encodedChainID, 63)
	if err != nil || chainID.Sign() <= 0 || chainID.Int64() != expectedChainID {
		return false, fmt.Errorf("RPC chain identity changed")
	}
	if methodResponse.Error != nil {
		if len(methodResponse.Result) != 0 || methodResponse.Error.Code == 0 || methodResponse.Error.Message == "" || len(methodResponse.Error.Message) > 4096 {
			return false, fmt.Errorf("RPC error object is invalid")
		}
		return false, newRPCRemoteError(methodResponse.Error.Code, methodResponse.Error.Message, methodResponse.Error.Data)
	}
	if len(methodResponse.Result) == 0 {
		return false, fmt.Errorf("RPC method response shape is invalid")
	}
	if string(methodResponse.Result) == "null" {
		if !allowNull {
			return false, fmt.Errorf("RPC method response shape is invalid")
		}
		return false, nil
	}
	if result != nil {
		if err := json.Unmarshal(methodResponse.Result, result); err != nil {
			return false, fmt.Errorf("decode RPC result: %w", err)
		}
	}
	return true, nil
}

func newRPCRemoteError(code int, message string, rawData json.RawMessage) error {
	lowerMessage := strings.ToLower(message)
	kind := RPCErrorUnknown
	switch {
	case code == -32601:
		kind = RPCErrorMethodNotFound
	case code == 3 || strings.Contains(lowerMessage, "execution reverted"):
		kind = RPCErrorExecutionReverted
	case strings.Contains(lowerMessage, "already known"):
		kind = RPCErrorAlreadyKnown
	case strings.Contains(lowerMessage, "nonce too low"):
		kind = RPCErrorNonceTooLow
	case strings.Contains(lowerMessage, "insufficient funds"):
		kind = RPCErrorInsufficientFunds
	case strings.Contains(lowerMessage, "replacement transaction underpriced") || strings.Contains(lowerMessage, "replacement underpriced"):
		kind = RPCErrorReplacementUnderpriced
	}
	var data []byte
	if len(rawData) > 0 && len(rawData) <= (8<<10)+2 {
		var encoded string
		if err := json.Unmarshal(rawData, &encoded); err == nil {
			if decoded, err := decodeRPCData(encoded, 4<<10); err == nil {
				data = append([]byte(nil), decoded...)
			}
		}
	}
	return &RPCRemoteError{Code: code, Kind: kind, Data: data}
}

// Request performs a bounded HTTP request through the validated gateway.
func (gateway *RPCGateway) Request(ctx context.Context, request OutboundRequest) (OutboundResponse, error) {
	if gateway == nil {
		return OutboundResponse{}, fmt.Errorf("RPC gateway is required")
	}
	if request.Method != http.MethodGet && request.Method != http.MethodPost {
		return OutboundResponse{}, fmt.Errorf("unsupported outbound method")
	}
	if len(request.Body) > gateway.maxRPCRequestBytes {
		return OutboundResponse{}, fmt.Errorf("outbound request exceeds size policy")
	}
	if err := validateOutboundHeaders(request.Headers); err != nil {
		return OutboundResponse{}, err
	}
	maxBytes := request.MaxResponseBytes
	if maxBytes <= 0 {
		maxBytes = gateway.maxRPCResponseBytes
	}
	if maxBytes > gateway.maxRegistryResponseBytes {
		return OutboundResponse{}, fmt.Errorf("outbound response limit exceeds policy")
	}
	requestTimeout := gateway.requestTimeout
	if request.Timeout < 0 || request.Timeout > hardRequestTimeout {
		return OutboundResponse{}, fmt.Errorf("outbound request timeout exceeds policy")
	}
	if request.Timeout > 0 {
		requestTimeout = request.Timeout
	}
	destination, err := gateway.ValidateEndpoint(ctx, request.URL)
	if err != nil {
		return OutboundResponse{}, err
	}
	return gateway.doResponse(ctx, destination, request.Method, request.Body, request.Headers, maxBytes, requestTimeout)
}

type managedWebSocket struct {
	*websocket.Conn
	release func()
	once    sync.Once
}

func (connection *managedWebSocket) Close() error {
	if connection == nil {
		return nil
	}
	err := connection.Conn.Close()
	connection.once.Do(connection.release)
	return err
}

type handshakeLimitConn struct {
	net.Conn
	mu        sync.Mutex
	remaining int
	enabled   bool
}

func (connection *handshakeLimitConn) Read(buffer []byte) (int, error) {
	connection.mu.Lock()
	defer connection.mu.Unlock()
	if !connection.enabled {
		return connection.Conn.Read(buffer)
	}
	if connection.remaining <= 0 {
		return 0, fmt.Errorf("WebSocket handshake headers exceed policy")
	}
	if len(buffer) > connection.remaining {
		buffer = buffer[:connection.remaining]
	}
	count, err := connection.Conn.Read(buffer)
	connection.remaining -= count
	return count, err
}

func (connection *handshakeLimitConn) disable() {
	connection.mu.Lock()
	connection.enabled = false
	connection.mu.Unlock()
}

// DialWebSocket opens a validated, DNS-pinned WebSocket connection.
func (gateway *RPCGateway) DialWebSocket(ctx context.Context, rawURL string) (*managedWebSocket, error) {
	if gateway == nil {
		return nil, fmt.Errorf("RPC gateway is required")
	}
	parsed, err := url.ParseRequestURI(rawURL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "ws" && parsed.Scheme != "wss") {
		return nil, fmt.Errorf("invalid WebSocket URL")
	}
	validationURL := *parsed
	if parsed.Scheme == "wss" {
		validationURL.Scheme = "https"
	} else {
		validationURL.Scheme = "http"
	}
	destination, err := gateway.ValidateEndpoint(ctx, validationURL.String())
	if err != nil {
		return nil, err
	}
	requestContext, cancel := context.WithTimeout(ctx, gateway.requestTimeout)
	defer cancel()
	if err := gateway.acquire(requestContext); err != nil {
		return nil, err
	}
	defer gateway.release()
	select {
	case gateway.webSocketSemaphore <- struct{}{}:
	case <-requestContext.Done():
		return nil, requestContext.Err()
	}
	releaseSocket := func() { <-gateway.webSocketSemaphore }
	releaseOnFailure := true
	defer func() {
		if releaseOnFailure {
			releaseSocket()
		}
	}()
	dialer := websocket.Dialer{
		Proxy:            nil,
		HandshakeTimeout: gateway.requestTimeout,
		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
			ServerName: parsed.Hostname(),
			RootCAs:    gateway.tlsRootCAs,
		},
	}
	var handshakeConnection *handshakeLimitConn
	dialer.NetDialContext = func(dialContext context.Context, network, address string) (net.Conn, error) {
		requestedHost, requestedPort, err := net.SplitHostPort(address)
		if err != nil || net.JoinHostPort(strings.ToLower(requestedHost), requestedPort) != destination.hostPort {
			return nil, fmt.Errorf("network destination changed after validation")
		}
		connection, err := gateway.dialContext(dialContext, network, net.JoinHostPort(destination.pinnedIP.String(), requestedPort))
		if err != nil {
			return nil, err
		}
		handshakeConnection = &handshakeLimitConn{Conn: connection, remaining: 64 << 10, enabled: true}
		return handshakeConnection, nil
	}
	connection, response, err := dialer.DialContext(requestContext, rawURL, nil)
	if response != nil && response.Body != nil {
		_ = response.Body.Close()
	}
	if err != nil {
		return nil, &networkTransportError{message: fmt.Sprintf("WebSocket connection to %s failed", RedactNetworkURL(validationURL.String())), cause: safeTransportCause(err)}
	}
	if handshakeConnection != nil {
		handshakeConnection.disable()
	}
	connection.SetReadLimit(hardRPCResponseBytes)
	releaseOnFailure = false
	return &managedWebSocket{Conn: connection, release: releaseSocket}, nil
}

func validateOutboundHeaders(headers map[string]string) error {
	for name, value := range headers {
		canonical := http.CanonicalHeaderKey(name)
		switch canonical {
		case "Accept", "Authorization", "Content-Type", "Origin":
		default:
			return fmt.Errorf("outbound header is not allowed")
		}
		if value == "" || len(value) > 4096 || strings.ContainsAny(value, "\x00\r\n") {
			return fmt.Errorf("outbound header value is invalid")
		}
	}
	return nil
}

func (gateway *RPCGateway) Fetch(ctx context.Context, rawURL string) ([]byte, error) {
	destination, err := gateway.ValidateEndpoint(ctx, rawURL)
	if err != nil {
		return nil, err
	}
	return gateway.do(ctx, destination, http.MethodGet, nil, gateway.maxRegistryResponseBytes)
}

func (gateway *RPCGateway) acquire(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case gateway.requestSemaphore <- struct{}{}:
		if err := gateway.waitForRateLimit(ctx); err != nil {
			<-gateway.requestSemaphore
			return err
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (gateway *RPCGateway) waitForRateLimit(ctx context.Context) error {
	for {
		now := time.Now()
		gateway.rateMu.Lock()
		cutoff := now.Add(-time.Second)
		firstActive := 0
		for firstActive < len(gateway.requestTimes) && gateway.requestTimes[firstActive].Before(cutoff) {
			firstActive++
		}
		if firstActive > 0 {
			gateway.requestTimes = append(gateway.requestTimes[:0], gateway.requestTimes[firstActive:]...)
		}
		if len(gateway.requestTimes) < gateway.maxRequestsPerSecond {
			gateway.requestTimes = append(gateway.requestTimes, now)
			gateway.rateMu.Unlock()
			return nil
		}
		wait := time.Until(gateway.requestTimes[0].Add(time.Second))
		gateway.rateMu.Unlock()
		timer := time.NewTimer(wait)
		select {
		case <-timer.C:
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return ctx.Err()
		}
	}
}

func (gateway *RPCGateway) release() {
	<-gateway.requestSemaphore
}

func safeTransportCause(err error) error {
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	var networkError net.Error
	if errors.As(err, &networkError) {
		temporary := false
		type temporaryError interface{ Temporary() bool }
		if candidate, ok := any(networkError).(temporaryError); ok {
			temporary = candidate.Temporary()
		}
		return &safeNetworkError{timeout: networkError.Timeout(), temporary: temporary}
	}
	return nil
}

func (gateway *RPCGateway) do(ctx context.Context, destination validatedDestination, method string, body []byte, maxBytes int64) ([]byte, error) {
	response, err := gateway.doResponse(ctx, destination, method, body, nil, maxBytes, gateway.requestTimeout)
	if err != nil {
		return nil, err
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("network request to %s returned status %d", RedactNetworkURL(destination.rawURL), response.StatusCode)
	}
	return response.Body, nil
}

func (gateway *RPCGateway) doResponse(ctx context.Context, destination validatedDestination, method string, body []byte, headers map[string]string, maxBytes int64, timeout time.Duration) (OutboundResponse, error) {
	response, _, err := gateway.doResponseTracked(ctx, destination, method, body, headers, maxBytes, timeout)
	return response, err
}

func (gateway *RPCGateway) doResponseTracked(ctx context.Context, destination validatedDestination, method string, body []byte, headers map[string]string, maxBytes int64, timeout time.Duration) (OutboundResponse, bool, error) {
	requestContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if err := gateway.acquire(requestContext); err != nil {
		return OutboundResponse{}, false, err
	}
	defer gateway.release()
	request, err := http.NewRequestWithContext(requestContext, method, destination.rawURL, bytes.NewReader(body))
	if err != nil {
		return OutboundResponse{}, false, fmt.Errorf("build network request")
	}
	var wroteRequest atomic.Bool
	request = request.WithContext(httptrace.WithClientTrace(request.Context(), &httptrace.ClientTrace{
		WroteRequest: func(httptrace.WroteRequestInfo) { wroteRequest.Store(true) },
	}))
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Accept-Encoding", "identity")
	if method == http.MethodPost {
		request.Header.Set("Content-Type", "application/json")
	}
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	transport := &http.Transport{
		Proxy:                  nil,
		DisableCompression:     true,
		ForceAttemptHTTP2:      true,
		MaxIdleConns:           2,
		MaxIdleConnsPerHost:    2,
		MaxResponseHeaderBytes: 32 << 10,
		IdleConnTimeout:        30 * time.Second,
		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
			ServerName: destination.parsed.Hostname(),
			RootCAs:    gateway.tlsRootCAs,
		},
	}
	transport.DialContext = func(dialContext context.Context, network, address string) (net.Conn, error) {
		requestedHost, requestedPort, err := net.SplitHostPort(address)
		if err != nil || net.JoinHostPort(strings.ToLower(requestedHost), requestedPort) != destination.hostPort {
			return nil, fmt.Errorf("network destination changed after validation")
		}
		return gateway.dialContext(dialContext, network, net.JoinHostPort(destination.pinnedIP.String(), requestedPort))
	}
	client := &http.Client{
		Transport: transport,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("network redirects are not allowed")
		},
	}
	defer transport.CloseIdleConnections()
	response, err := client.Do(request)
	if err != nil {
		return OutboundResponse{}, wroteRequest.Load(), &networkTransportError{message: fmt.Sprintf("network request to %s failed", RedactNetworkURL(destination.rawURL)), cause: safeTransportCause(err)}
	}
	defer func() { _ = response.Body.Close() }()
	if encoding := strings.TrimSpace(response.Header.Get("Content-Encoding")); encoding != "" && !strings.EqualFold(encoding, "identity") {
		return OutboundResponse{}, true, fmt.Errorf("compressed network responses are not accepted")
	}
	if response.ContentLength > maxBytes {
		return OutboundResponse{}, true, fmt.Errorf("network response exceeds size policy")
	}
	limited := io.LimitReader(response.Body, maxBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return OutboundResponse{}, true, fmt.Errorf("read network response: %w", err)
	}
	if int64(len(data)) > maxBytes {
		return OutboundResponse{}, true, fmt.Errorf("network response exceeds size policy")
	}
	return OutboundResponse{StatusCode: response.StatusCode, Body: data}, true, nil
}

func rejectDuplicateRPCJSONKeys(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	type frame struct {
		object    bool
		expectKey bool
		keys      map[string]struct{}
	}
	stack := make([]frame, 0)
	completeValue := func() {
		if len(stack) > 0 && stack[len(stack)-1].object && !stack[len(stack)-1].expectKey {
			stack[len(stack)-1].expectKey = true
		}
	}
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if delimiter, ok := token.(json.Delim); ok {
			switch delimiter {
			case '{':
				stack = append(stack, frame{object: true, expectKey: true, keys: make(map[string]struct{})})
			case '[':
				stack = append(stack, frame{})
			case '}', ']':
				if len(stack) == 0 {
					return fmt.Errorf("unexpected JSON delimiter")
				}
				stack = stack[:len(stack)-1]
				completeValue()
			}
			continue
		}
		if len(stack) > 0 && stack[len(stack)-1].object && stack[len(stack)-1].expectKey {
			key, ok := token.(string)
			if !ok {
				return fmt.Errorf("JSON object key is not a string")
			}
			normalizedKey := strings.ToLower(key)
			if _, exists := stack[len(stack)-1].keys[normalizedKey]; exists {
				return fmt.Errorf("duplicate JSON key")
			}
			stack[len(stack)-1].keys[normalizedKey] = struct{}{}
			stack[len(stack)-1].expectKey = false
			continue
		}
		completeValue()
	}
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err == io.EOF {
		return nil
	}
	return fmt.Errorf("RPC response contains trailing JSON")
}

func parseRPCQuantity(value string, maxBits int) (*big.Int, error) {
	if len(value) < 3 || !strings.HasPrefix(value, "0x") {
		return nil, fmt.Errorf("RPC quantity must use canonical hexadecimal form")
	}
	digits := value[2:]
	if len(digits) > 1 && digits[0] == '0' {
		return nil, fmt.Errorf("RPC quantity contains a leading zero")
	}
	for _, digit := range digits {
		if (digit < '0' || digit > '9') && (digit < 'a' || digit > 'f') {
			return nil, fmt.Errorf("RPC quantity contains an invalid digit")
		}
	}
	quantity := new(big.Int)
	if _, ok := quantity.SetString(digits, 16); !ok || quantity.BitLen() > maxBits {
		return nil, fmt.Errorf("RPC quantity is outside numeric policy")
	}
	return quantity, nil
}

func allDecimal(value string) bool {
	if value == "" {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

func looksLikeAlternativeIP(hostname string) bool {
	lower := strings.ToLower(hostname)
	if strings.HasPrefix(lower, "0x") {
		return true
	}
	parts := strings.Split(lower, ".")
	if len(parts) != 4 {
		return false
	}
	for _, part := range parts {
		if strings.HasPrefix(part, "0x") || (len(part) > 1 && part[0] == '0') {
			return true
		}
	}
	return false
}

func isAllowedLocalNodeAddress(address netip.Addr) bool {
	if !address.IsValid() || address.Is4In6() {
		return false
	}
	address = address.Unmap()
	return address.IsLoopback() || address.IsPrivate()
}

func isUnsafeNetworkAddress(address netip.Addr) bool {
	if address.Is4In6() {
		return true
	}
	address = address.Unmap()
	if address.IsLoopback() || address.IsPrivate() || address.IsLinkLocalUnicast() || address.IsLinkLocalMulticast() || address.IsMulticast() || address.IsUnspecified() {
		return true
	}
	blockedPrefixes := []netip.Prefix{
		netip.MustParsePrefix("0.0.0.0/8"),
		netip.MustParsePrefix("100.64.0.0/10"),
		netip.MustParsePrefix("168.63.129.16/32"),
		netip.MustParsePrefix("192.0.0.0/24"),
		netip.MustParsePrefix("192.0.2.0/24"),
		netip.MustParsePrefix("198.18.0.0/15"),
		netip.MustParsePrefix("198.51.100.0/24"),
		netip.MustParsePrefix("203.0.113.0/24"),
		netip.MustParsePrefix("240.0.0.0/4"),
		netip.MustParsePrefix("::/96"),
		netip.MustParsePrefix("64:ff9b::/96"),
		netip.MustParsePrefix("64:ff9b:1::/48"),
		netip.MustParsePrefix("2001::/32"),
		netip.MustParsePrefix("2001:db8::/32"),
		netip.MustParsePrefix("2002::/16"),
		netip.MustParsePrefix("fec0::/10"),
	}
	for _, prefix := range blockedPrefixes {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}
