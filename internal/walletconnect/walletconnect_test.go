package walletconnect_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"blocowallet/internal/blockchain"
	"blocowallet/internal/walletconnect"

	"github.com/gorilla/websocket"
)

// memorySessionStore is an in-memory SessionStore for tests.
type memorySessionStore struct {
	mu       sync.Mutex
	sessions map[string]*walletconnect.Session
}

func newMemorySessionStore() *memorySessionStore {
	return &memorySessionStore{sessions: make(map[string]*walletconnect.Session)}
}

func (store *memorySessionStore) SaveSession(_ context.Context, session *walletconnect.Session) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.sessions[session.Topic] = session
	return nil
}
func (store *memorySessionStore) GetSession(_ context.Context, topic string) (*walletconnect.Session, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	session, exists := store.sessions[topic]
	if !exists {
		return nil, fmt.Errorf("session not found")
	}
	copied := *session
	return &copied, nil
}
func (store *memorySessionStore) ListSessions(_ context.Context, accountID string, includeRevoked bool) ([]walletconnect.Session, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	var result []walletconnect.Session
	for _, session := range store.sessions {
		if session.AccountID == accountID && (includeRevoked || !session.Revoked) {
			result = append(result, *session)
		}
	}
	return result, nil
}
func (store *memorySessionStore) RevokeSession(_ context.Context, topic string, revokedAt int64) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	session, exists := store.sessions[topic]
	if !exists || session.Revoked {
		return fmt.Errorf("session not revocable")
	}
	session.Revoked = true
	used := revokedAt
	session.LastUsedAt = &used
	return nil
}
func (store *memorySessionStore) TouchSession(_ context.Context, topic string, usedAt int64) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	session, exists := store.sessions[topic]
	if !exists || session.Revoked {
		return fmt.Errorf("session not active")
	}
	used := usedAt
	session.LastUsedAt = &used
	return nil
}

// mockRelay is a local WalletConnect relay implementing the subset used by
// the client: publish, subscribe, unsubscribe, getMessage, and ping.
type mockRelay struct {
	server *httptest.Server
	mu     sync.Mutex
	queues map[string][][]byte
	subs   map[string][]*websocket.Conn
	write  sync.Mutex
}

func (relay *mockRelay) send(conn *websocket.Conn, value any) {
	relay.write.Lock()
	defer relay.write.Unlock()
	_ = conn.WriteJSON(value)
}

func newMockRelay(t *testing.T) *mockRelay {
	relay := &mockRelay{queues: make(map[string][][]byte), subs: make(map[string][]*websocket.Conn)}
	var upgrader = websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	relay.server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		conn, err := upgrader.Upgrade(writer, request, nil)
		if err != nil {
			return
		}
		defer func() {
			relay.mu.Lock()
			for topic, connections := range relay.subs {
				kept := connections[:0]
				for _, connection := range connections {
					if connection != conn {
						kept = append(kept, connection)
					}
				}
				relay.subs[topic] = kept
			}
			relay.mu.Unlock()
			_ = conn.Close()
		}()
		for {
			_, raw, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var envelope struct {
				ID     int64           `json:"id"`
				Method string          `json:"method"`
				Params json.RawMessage `json:"params"`
			}
			if err := json.Unmarshal(raw, &envelope); err != nil {
				relay.send(conn, map[string]any{"id": envelope.ID, "error": map[string]any{"code": -32700, "message": "parse error"}})
				continue
			}
			switch envelope.Method {
			case "irn_ping":
				relay.send(conn, map[string]any{"id": envelope.ID, "jsonrpc": "2.0", "result": true})
			case "irn_subscribe":
				var params struct {
					Topic string `json:"topic"`
				}
				_ = json.Unmarshal(envelope.Params, &params)
				relay.mu.Lock()
				relay.subs[params.Topic] = append(relay.subs[params.Topic], conn)
				relay.mu.Unlock()
				relay.send(conn, map[string]any{"id": envelope.ID, "jsonrpc": "2.0", "result": fmt.Sprintf("sub-%d", envelope.ID)})
			case "irn_unsubscribe":
				var params struct {
					Topic string `json:"topic"`
				}
				_ = json.Unmarshal(envelope.Params, &params)
				relay.mu.Lock()
				connections := relay.subs[params.Topic]
				kept := connections[:0]
				for _, connection := range connections {
					if connection != conn {
						kept = append(kept, connection)
					}
				}
				relay.subs[params.Topic] = kept
				relay.mu.Unlock()
				relay.send(conn, map[string]any{"id": envelope.ID, "jsonrpc": "2.0", "result": true})
			case "irn_getMessage":
				var params struct {
					Topic string `json:"topic"`
				}
				_ = json.Unmarshal(envelope.Params, &params)
				relay.mu.Lock()
				queue := relay.queues[params.Topic]
				var message []byte
				if len(queue) > 0 {
					message = queue[0]
					relay.queues[params.Topic] = queue[1:]
				}
				relay.mu.Unlock()
				relay.send(conn, map[string]any{"id": envelope.ID, "jsonrpc": "2.0", "result": hexString(message)})
			case "irn_publish":
				var params struct {
					Topic   string `json:"topic"`
					Message string `json:"message"`
				}
				if err := json.Unmarshal(envelope.Params, &params); err != nil {
					relay.send(conn, map[string]any{"id": envelope.ID, "jsonrpc": "2.0", "result": true})
					continue
				}
				message := decodeHex(params.Message)
				relay.mu.Lock()
				relay.queues[params.Topic] = append(relay.queues[params.Topic], message)
				subscribers := append([]*websocket.Conn(nil), relay.subs[params.Topic]...)
				relay.mu.Unlock()
				for _, subscriber := range subscribers {
					if subscriber == conn {
						continue
					}
					relay.send(subscriber, map[string]any{
						"id": 0, "jsonrpc": "2.0", "method": "irn_subscription",
						"params": map[string]any{"id": fmt.Sprintf("push-%d", envelope.ID), "topic": params.Topic, "message": params.Message},
					})
				}
				relay.send(conn, map[string]any{"id": envelope.ID, "jsonrpc": "2.0", "result": true})
			default:
				relay.send(conn, map[string]any{"id": envelope.ID, "jsonrpc": "2.0", "error": map[string]any{"code": -32601, "message": "method not found"}})
			}
		}
	}))
	t.Cleanup(relay.server.Close)
	return relay
}

func (relay *mockRelay) URL() string {
	return "ws" + strings.TrimPrefix(relay.server.URL, "http")
}

func relayGateway(t *testing.T, rawURL string) *blockchain.RPCGateway {
	t.Helper()
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	return blockchain.NewRPCGateway(blockchain.RPCGatewayOptions{
		AllowedLocalTargets: []string{parsed.Host}, MaxRequestsPerSecond: 256,
	})
}

func hexString(message []byte) string {
	const digits = "0123456789abcdef"
	encoded := make([]byte, len(message)*2)
	for index, value := range message {
		encoded[index*2] = digits[value>>4]
		encoded[index*2+1] = digits[value&0x0f]
	}
	return string(encoded)
}

func decodeHex(encoded string) []byte {
	if len(encoded)%2 != 0 {
		return nil
	}
	decoded := make([]byte, len(encoded)/2)
	for index := 0; index < len(decoded); index++ {
		decoded[index] = byte(hexNibble(encoded[index*2]))<<4 | byte(hexNibble(encoded[index*2+1]))
	}
	return decoded
}

func hexNibble(value byte) int {
	switch {
	case value >= '0' && value <= '9':
		return int(value - '0')
	case value >= 'a' && value <= 'f':
		return int(value - 'a' + 10)
	case value >= 'A' && value <= 'F':
		return int(value - 'A' + 10)
	default:
		return 0
	}
}

func TestWalletConnectCryptoRoundTrip(t *testing.T) {
	initiator, err := walletconnect.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	responder, err := walletconnect.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	initiatorKey, err := walletconnect.SymmetricKey(initiator, responder.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	responderKey, err := walletconnect.SymmetricKey(responder, initiator.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	if len(initiatorKey) != 32 || len(responderKey) != 32 {
		t.Fatal("invalid symmetric key size")
	}
	plaintext := []byte(`{"type":"session_proposal","payload":{"id":7}}`)
	envelope, err := walletconnect.EncryptEnvelope(initiatorKey, plaintext)
	if err != nil {
		t.Fatal(err)
	}
	decrypted, err := walletconnect.DecryptEnvelope(responderKey, envelope)
	if err != nil {
		t.Fatal(err)
	}
	if string(decrypted) != string(plaintext) {
		t.Fatal("envelope round trip mismatch")
	}
	wrongKey, err := walletconnect.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	attackerKey, err := walletconnect.SymmetricKey(wrongKey, initiator.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := walletconnect.DecryptEnvelope(attackerKey, envelope); err == nil {
		t.Fatal("envelope opened with the wrong key")
	}
	tampered := append([]byte(nil), envelope...)
	tampered[len(tampered)-1] ^= 0x01
	if _, err := walletconnect.DecryptEnvelope(responderKey, tampered); err == nil {
		t.Fatal("tampered envelope was accepted")
	}
	topic := walletconnect.PairingTopic(initiator.PublicKey, responder.PublicKey)
	if len(topic) != 64 {
		t.Fatalf("invalid pairing topic: %q", topic)
	}
	if err := walletconnect.ValidateTopic(topic); err != nil {
		t.Fatal(err)
	}
}

func TestWalletConnectProposalValidation(t *testing.T) {
	proposal := &walletconnect.Proposal{
		ID: 7, PairingTopic: walletconnect.PairingTopic([]byte{1}, []byte{2}), Expiry: 100,
		RequiredNamespaces: walletconnect.RequiredNamespaces{
			"eip155": {Chains: []string{"eip155:1"}, Methods: []string{"personal_sign", "eth_sendTransaction"}, Events: []string{"chainChanged"}},
		},
		Proposer: walletconnect.PeerMetadata{PublicKey: base64.RawURLEncoding.EncodeToString([]byte("key"))},
	}
	if _, err := walletconnect.ValidateProposal(proposal); err != nil {
		t.Fatal(err)
	}
	approved, err := walletconnect.ApproveNamespaces(proposal.RequiredNamespaces, "0x1111111111111111111111111111111111111111")
	if err != nil {
		t.Fatal(err)
	}
	if len(approved["eip155"].Accounts) != 1 || approved["eip155"].Accounts[0] != "eip155:1:0x1111111111111111111111111111111111111111" {
		t.Fatalf("approval did not bind the account: %+v", approved)
	}
	wildcard := *proposal
	wildcard.RequiredNamespaces = walletconnect.RequiredNamespaces{
		"eip155": {Chains: []string{"eip155:*"}, Methods: []string{"personal_sign"}},
	}
	if _, err := walletconnect.ValidateProposal(&wildcard); err == nil {
		t.Fatal("wildcard chain was accepted")
	}
	emptyMethods := *proposal
	emptyMethods.RequiredNamespaces = walletconnect.RequiredNamespaces{
		"eip155": {Chains: []string{"eip155:1"}, Methods: nil},
	}
	if _, err := walletconnect.ValidateProposal(&emptyMethods); err == nil {
		t.Fatal("proposal without methods was accepted")
	}
	tooMany := *proposal
	chains := make([]string, 0, 40)
	for index := 0; index < 40; index++ {
		chains = append(chains, fmt.Sprintf("eip155:%d", index))
	}
	tooMany.RequiredNamespaces = walletconnect.RequiredNamespaces{
		"eip155": {Chains: chains, Methods: []string{"personal_sign"}},
	}
	if _, err := walletconnect.ValidateProposal(&tooMany); err == nil {
		t.Fatal("chains budget overflow was accepted")
	}
}

func TestRelayClientSerializesConcurrentWrites(t *testing.T) {
	relay := newMockRelay(t)
	client, err := walletconnect.NewRelayClient(context.Background(), relay.URL(), relayGateway(t, relay.URL()))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	topic := strings.Repeat("a", 64)
	start := make(chan struct{})
	errorsChannel := make(chan error, 32)
	var wait sync.WaitGroup
	for index := 0; index < 32; index++ {
		wait.Add(1)
		go func(value byte) {
			defer wait.Done()
			<-start
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			errorsChannel <- client.Publish(ctx, topic, []byte{value})
		}(byte(index))
	}
	close(start)
	wait.Wait()
	close(errorsChannel)
	for err := range errorsChannel {
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestRelayClientRemoteCloseWakesPendingCall(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		connection, err := upgrader.Upgrade(writer, request, nil)
		if err != nil {
			return
		}
		_, _, _ = connection.ReadMessage()
		_ = connection.Close()
	}))
	defer server.Close()
	relayURL := "ws" + strings.TrimPrefix(server.URL, "http")
	client, err := walletconnect.NewRelayClient(context.Background(), relayURL, relayGateway(t, relayURL))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err = client.Publish(ctx, strings.Repeat("b", 64), []byte("message"))
	if err == nil || !strings.Contains(err.Error(), "relay closed") {
		t.Fatalf("remote close returned %v", err)
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestWalletConnectServiceEndToEndWithMockRelay(t *testing.T) {
	relay := newMockRelay(t)
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)

	// Dapp side.
	dappPair, err := walletconnect.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	dappRelay, err := walletconnect.NewRelayClient(context.Background(), relay.URL(), relayGateway(t, relay.URL()))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = dappRelay.Close() }()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Wallet side.
	walletPair, err := walletconnect.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	walletRelay, err := walletconnect.NewRelayClient(context.Background(), relay.URL(), relayGateway(t, relay.URL()))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = walletRelay.Close() }()
	store := newMemorySessionStore()
	service, err := walletconnect.NewService(walletRelay, store, walletconnect.Options{
		Now:         func() time.Time { return now },
		ProposalTTL: time.Minute,
		SessionTTL:  time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	pairingTopic := walletconnect.PairingTopic(walletPair.PublicKey, dappPair.PublicKey)
	pairingKey, err := walletconnect.SymmetricKey(walletPair, dappPair.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.SetKey(pairingTopic, pairingKey); err != nil {
		t.Fatal(err)
	}
	if err := walletRelay.Subscribe(ctx, pairingTopic); err != nil {
		t.Fatal(err)
	}
	go service.Run(ctx)

	dappKey, err := walletconnect.SymmetricKey(dappPair, walletPair.PublicKey)
	if err != nil {
		t.Fatal(err)
	}

	proposalReceived := make(chan *walletconnect.Proposal, 1)
	service.OnProposal(func(ctx context.Context, proposal *walletconnect.Proposal) error {
		proposalReceived <- proposal
		return nil
	})
	proposal := walletconnect.Proposal{
		ID: 99, PairingTopic: pairingTopic, Expiry: now.Add(time.Minute).UnixMilli(),
		RequiredNamespaces: walletconnect.RequiredNamespaces{
			"eip155": {Chains: []string{"eip155:1"}, Methods: []string{"personal_sign"}, Events: []string{"chainChanged"}},
		},
		Proposer: walletconnect.PeerMetadata{PublicKey: base64.RawURLEncoding.EncodeToString(dappPair.PublicKey)},
	}
	proposal.Proposer.Metadata.Name = "Mock Dapp"
	proposalPayload, err := json.Marshal(map[string]any{"type": "session_proposal", "payload": proposal})
	if err != nil {
		t.Fatal(err)
	}
	encrypted, err := walletconnect.EncryptEnvelope(dappKey, proposalPayload)
	if err != nil {
		t.Fatal(err)
	}
	if err := dappRelay.Publish(ctx, pairingTopic, encrypted); err != nil {
		t.Fatal(err)
	}

	select {
	case received := <-proposalReceived:
		if received.ID != 99 || received.Proposer.Metadata.Name != "Mock Dapp" {
			t.Fatalf("unexpected proposal: %+v", received)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("proposal was not delivered")
	}

	session, err := service.ApproveProposal(ctx, 99, "11111111-1111-4111-8111-111111111111", "0x1111111111111111111111111111111111111111")
	if err != nil {
		t.Fatal(err)
	}
	if session.Topic == "" || session.PeerName != "Mock Dapp" || session.ExpiresAt <= now.UnixMilli() {
		t.Fatalf("unexpected approved session: %+v", session)
	}
	if err := service.SetKey(session.Topic, pairingKey); err != nil {
		t.Fatal(err)
	}
	if err := walletRelay.Subscribe(ctx, session.Topic); err != nil {
		t.Fatal(err)
	}
	if err := dappRelay.Subscribe(ctx, session.Topic); err != nil {
		t.Fatal(err)
	}

	// Dapp sends a session_request on the session topic.
	requestReceived := make(chan *walletconnect.SessionRequestParams, 1)
	service.OnRequest(func(ctx context.Context, active *walletconnect.Session, params *walletconnect.SessionRequestParams) error {
		if active.Topic != session.Topic {
			t.Fatalf("request arrived on the wrong session: %s", active.Topic)
		}
		requestReceived <- params
		return nil
	})
	sessionKey, err := walletconnect.SymmetricKey(dappPair, walletPair.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	requestPayload, err := json.Marshal(map[string]any{
		"type": "session_request",
		"payload": map[string]any{
			"id": 77, "chainId": "eip155:1",
			"request": map[string]any{"id": 77, "method": "personal_sign", "params": []any{"0x68656c6c6f", "0x1111111111111111111111111111111111111111"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	encryptedRequest, err := walletconnect.EncryptEnvelope(sessionKey, requestPayload)
	if err != nil {
		t.Fatal(err)
	}
	if err := dappRelay.Publish(ctx, session.Topic, encryptedRequest); err != nil {
		t.Fatal(err)
	}
	select {
	case params := <-requestReceived:
		if params.Request.Method != "personal_sign" || params.ChainID != "eip155:1" {
			t.Fatalf("unexpected session request: %+v", params)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("session request was not delivered")
	}

	// A request outside the approved scope is rejected before any handler.
	outOfScope, err := json.Marshal(map[string]any{
		"type": "session_request",
		"payload": map[string]any{
			"id": 78, "chainId": "eip155:1",
			"request": map[string]any{"id": 78, "method": "eth_sendTransaction", "params": []any{}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	encryptedOutOfScope, err := walletconnect.EncryptEnvelope(dappKey, outOfScope)
	if err != nil {
		t.Fatal(err)
	}
	requestReceived = make(chan *walletconnect.SessionRequestParams, 1)
	if err := dappRelay.Publish(ctx, session.Topic, encryptedOutOfScope); err != nil {
		t.Fatal(err)
	}
	select {
	case <-requestReceived:
		t.Fatal("out-of-scope session request reached the handler")
	case <-time.After(300 * time.Millisecond):
	}

	stored, err := store.GetSession(ctx, session.Topic)
	if err != nil {
		t.Fatal(err)
	}
	if stored.LastUsedAt == nil {
		t.Fatal("session was not touched by the request")
	}
	// Deleting the session revokes it.
	deletePayload, err := json.Marshal(map[string]any{"type": "session_delete"})
	if err != nil {
		t.Fatal(err)
	}
	encryptedDelete, err := walletconnect.EncryptEnvelope(dappKey, deletePayload)
	if err != nil {
		t.Fatal(err)
	}
	if err := dappRelay.Publish(ctx, session.Topic, encryptedDelete); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		revoked, getErr := store.GetSession(ctx, session.Topic)
		if getErr != nil {
			t.Fatal(getErr)
		}
		if revoked.Revoked {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("session_delete did not revoke the session")
		}
		time.Sleep(20 * time.Millisecond)
	}
}
