package walletconnect

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"blocowallet/internal/blockchain"
)

const (
	// MaxRelayPayload bounds relay publish payloads.
	MaxRelayPayload = 1 << 20
	// relayPingInterval keeps the connection alive.
	relayPingInterval = 30 * time.Second
)

// Subscription delivers a relay message to the service.
type Subscription struct {
	Topic string
	Data  []byte
}

type relayWebSocket interface {
	ReadMessage() (int, []byte, error)
	WriteJSON(any) error
	SetReadLimit(int64)
	SetWriteDeadline(time.Time) error
	Close() error
}

// RelayClient is a WalletConnect relay JSON-RPC client over WebSocket.
type RelayClient struct {
	url       string
	conn      relayWebSocket
	mu        sync.Mutex
	writeMu   sync.Mutex
	wg        sync.WaitGroup
	nextID    int64
	pending   map[int64]chan *jsonrpcResponse
	messages  chan Subscription
	closed    chan struct{}
	closeOnce sync.Once
}

type jsonrpcRequest struct {
	ID      int64  `json:"id"`
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params"`
}

type jsonrpcResponse struct {
	ID     int64           `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// NewRelayClient connects to the relay URL.
func NewRelayClient(ctx context.Context, relayURL string, gateway *blockchain.RPCGateway) (*RelayClient, error) {
	if relayURL == "" || gateway == nil {
		return nil, fmt.Errorf("walletconnect: relay url and RPC gateway required")
	}
	parsed, err := url.Parse(relayURL)
	if err != nil || (parsed.Scheme != "ws" && parsed.Scheme != "wss") {
		return nil, fmt.Errorf("walletconnect: invalid relay url")
	}
	conn, err := gateway.DialWebSocket(ctx, relayURL)
	if err != nil {
		return nil, fmt.Errorf("walletconnect: relay dial: %w", err)
	}
	conn.SetReadLimit(MaxRelayPayload)
	client := &RelayClient{
		url: relayURL, conn: conn, nextID: 1,
		pending:  make(map[int64]chan *jsonrpcResponse),
		messages: make(chan Subscription, 64),
		closed:   make(chan struct{}),
	}
	client.wg.Add(2)
	go func() {
		defer client.wg.Done()
		client.readLoop()
	}()
	go func() {
		defer client.wg.Done()
		client.pingLoop()
	}()
	return client, nil
}

// Messages returns the subscription push channel.
func (client *RelayClient) Messages() <-chan Subscription {
	return client.messages
}

func (client *RelayClient) readLoop() {
	defer close(client.messages)
	client.mu.Lock()
	connection := client.conn
	client.mu.Unlock()
	if connection == nil {
		return
	}
	for {
		_, raw, err := connection.ReadMessage()
		if err != nil {
			client.close()
			return
		}
		if len(raw) > MaxRelayPayload {
			continue
		}
		var push struct {
			ID     int64           `json:"id"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal(raw, &push); err != nil {
			continue
		}
		if push.ID != 0 {
			// Response to a request we issued.
			var response jsonrpcResponse
			if err := json.Unmarshal(raw, &response); err != nil {
				continue
			}
			client.mu.Lock()
			channel, exists := client.pending[response.ID]
			if exists {
				delete(client.pending, response.ID)
			}
			client.mu.Unlock()
			if exists {
				channel <- &response
			}
			continue
		}
		// Relay push (irn_subscription): {id, jsonrpc, method, params:{id, topic, message}}
		var subscription struct {
			Method string `json:"method"`
			Params struct {
				Topic   string `json:"topic"`
				Message string `json:"message"`
			} `json:"params"`
		}
		if err := json.Unmarshal(raw, &subscription); err != nil {
			continue
		}
		if subscription.Method != "irn_subscription" {
			continue
		}
		message, err := decodeRelayMessage(subscription.Params.Message)
		if err != nil {
			continue
		}
		select {
		case client.messages <- Subscription{Topic: subscription.Params.Topic, Data: message}:
		default:
			// Drop the oldest push when the consumer is slow.
			select {
			case <-client.messages:
			default:
			}
			select {
			case client.messages <- Subscription{Topic: subscription.Params.Topic, Data: message}:
			default:
			}
		}
	}
}

func (client *RelayClient) pingLoop() {
	ticker := time.NewTicker(relayPingInterval)
	defer ticker.Stop()
	for {
		select {
		case <-client.closed:
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			_, err := client.call(ctx, "irn_ping", nil)
			cancel()
			if err != nil {
				client.close()
				return
			}
		}
	}
}

func (client *RelayClient) call(ctx context.Context, method string, params any) (*jsonrpcResponse, error) {
	if client == nil || ctx == nil {
		return nil, fmt.Errorf("walletconnect: relay call is invalid")
	}
	select {
	case <-client.closed:
		return nil, fmt.Errorf("walletconnect: relay closed")
	default:
	}
	client.mu.Lock()
	if client.conn == nil {
		client.mu.Unlock()
		return nil, fmt.Errorf("walletconnect: relay closed")
	}
	connection := client.conn
	id := client.nextID
	client.nextID++
	channel := make(chan *jsonrpcResponse, 1)
	client.pending[id] = channel
	client.mu.Unlock()
	defer func() {
		client.mu.Lock()
		delete(client.pending, id)
		client.mu.Unlock()
	}()
	request := jsonrpcRequest{ID: id, JSONRPC: "2.0", Method: method, Params: params}
	client.writeMu.Lock()
	deadline := time.Now().Add(10 * time.Second)
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
		deadline = contextDeadline
	}
	if err := connection.SetWriteDeadline(deadline); err != nil {
		client.writeMu.Unlock()
		return nil, fmt.Errorf("walletconnect: relay write deadline: %w", err)
	}
	writeErr := connection.WriteJSON(request)
	clearDeadlineErr := connection.SetWriteDeadline(time.Time{})
	client.writeMu.Unlock()
	if writeErr == nil {
		writeErr = clearDeadlineErr
	}
	if writeErr != nil {
		client.close()
		return nil, fmt.Errorf("walletconnect: relay write: %w", writeErr)
	}
	select {
	case response := <-channel:
		if response == nil {
			return nil, fmt.Errorf("walletconnect: relay closed")
		}
		if response.Error != nil {
			return nil, fmt.Errorf("walletconnect: relay error %d: %s", response.Error.Code, response.Error.Message)
		}
		return response, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-client.closed:
		return nil, fmt.Errorf("walletconnect: relay closed")
	}
}

// Publish publishes an encrypted message on a topic.
func (client *RelayClient) Publish(ctx context.Context, topic string, message []byte) error {
	if err := ValidateTopic(topic); err != nil {
		return err
	}
	if len(message) == 0 || len(message) > MaxRelayPayload {
		return fmt.Errorf("walletconnect: publish payload size")
	}
	_, err := client.call(ctx, "irn_publish", map[string]any{
		"topic":   topic,
		"message": encodeRelayMessage(message),
		"ttl":     300,
		"prompt":  false,
		"tag":     1101,
	})
	return err
}

// Subscribe subscribes to a topic.
func (client *RelayClient) Subscribe(ctx context.Context, topic string) error {
	if err := ValidateTopic(topic); err != nil {
		return err
	}
	_, err := client.call(ctx, "irn_subscribe", map[string]any{"topic": topic})
	return err
}

// Unsubscribe removes a topic subscription.
func (client *RelayClient) Unsubscribe(ctx context.Context, topic string) error {
	if err := ValidateTopic(topic); err != nil {
		return err
	}
	_, err := client.call(ctx, "irn_unsubscribe", map[string]any{"topic": topic})
	return err
}

// GetMessage fetches queued messages for a topic.
func (client *RelayClient) GetMessage(ctx context.Context, topic string) ([]byte, error) {
	if err := ValidateTopic(topic); err != nil {
		return nil, err
	}
	response, err := client.call(ctx, "irn_getMessage", map[string]any{"topic": topic})
	if err != nil {
		return nil, err
	}
	var encoded string
	if err := json.Unmarshal(response.Result, &encoded); err != nil {
		return nil, fmt.Errorf("walletconnect: relay getMessage decode: %w", err)
	}
	return decodeRelayMessage(encoded)
}

// Close terminates the relay connection.
func (client *RelayClient) Close() error {
	if client == nil {
		return nil
	}
	client.close()
	client.wg.Wait()
	return nil
}

func (client *RelayClient) close() {
	client.closeOnce.Do(func() {
		close(client.closed)
		client.mu.Lock()
		connection := client.conn
		client.conn = nil
		client.pending = make(map[int64]chan *jsonrpcResponse)
		client.mu.Unlock()
		client.writeMu.Lock()
		if connection != nil {
			_ = connection.Close()
		}
		client.writeMu.Unlock()
	})
}

// encodeRelayMessage hex-encodes relay payloads.
func encodeRelayMessage(message []byte) string {
	const hexDigits = "0123456789abcdef"
	encoded := make([]byte, len(message)*2)
	for index, value := range message {
		encoded[index*2] = hexDigits[value>>4]
		encoded[index*2+1] = hexDigits[value&0x0f]
	}
	return string(encoded)
}

// decodeRelayMessage decodes hex relay payloads, tolerating a 0x prefix.
func decodeRelayMessage(encoded string) ([]byte, error) {
	encoded = strings.TrimPrefix(strings.TrimSpace(encoded), "0x")
	if len(encoded) == 0 || len(encoded)%2 != 0 {
		return nil, fmt.Errorf("walletconnect: invalid relay message encoding")
	}
	decoded := make([]byte, len(encoded)/2)
	for index := 0; index < len(decoded); index++ {
		high, errHigh := hexValue(encoded[index*2])
		low, errLow := hexValue(encoded[index*2+1])
		if errHigh != nil || errLow != nil {
			return nil, fmt.Errorf("walletconnect: invalid relay message encoding")
		}
		decoded[index] = high<<4 | low
	}
	if len(decoded) > MaxRelayPayload {
		return nil, fmt.Errorf("walletconnect: relay message too large")
	}
	return decoded, nil
}

func hexValue(value byte) (byte, error) {
	switch {
	case value >= '0' && value <= '9':
		return value - '0', nil
	case value >= 'a' && value <= 'f':
		return value - 'a' + 10, nil
	case value >= 'A' && value <= 'F':
		return value - 'A' + 10, nil
	default:
		return 0, fmt.Errorf("invalid hex")
	}
}
