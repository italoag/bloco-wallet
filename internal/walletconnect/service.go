package walletconnect

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// Session is a persisted WalletConnect v2 session bound to one account.
type Session struct {
	Topic        string
	PeerName     string
	PeerMetadata map[string]any
	AccountID    string
	Namespaces   Namespaces
	ExpiresAt    int64
	Revoked      bool
	CreatedAt    int64
	LastUsedAt   *int64
}

// SessionStore persists sessions.
type SessionStore interface {
	SaveSession(context.Context, *Session) error
	GetSession(ctx context.Context, topic string) (*Session, error)
	ListSessions(ctx context.Context, accountID string, includeRevoked bool) ([]Session, error)
	RevokeSession(ctx context.Context, topic string, revokedAt int64) error
	TouchSession(ctx context.Context, topic string, usedAt int64) error
}

// Relay is the WalletConnect relay transport.
type Relay interface {
	Publish(ctx context.Context, topic string, message []byte) error
	Subscribe(ctx context.Context, topic string) error
	Unsubscribe(ctx context.Context, topic string) error
	GetMessage(ctx context.Context, topic string) ([]byte, error)
	Messages() <-chan Subscription
	Close() error
}

// ProposalHandler receives validated session proposals awaiting approval.
type ProposalHandler func(ctx context.Context, proposal *Proposal) error

// RequestHandler receives session requests routed for approval.
type RequestHandler func(ctx context.Context, session *Session, params *SessionRequestParams) error

// Options configures the WalletConnect service.
type Options struct {
	Now   func() time.Time
	NewID func() (string, error)
	// ProposalTTL bounds how long a proposal waits for approval.
	ProposalTTL time.Duration
	// SessionTTL is the requested session lifetime.
	SessionTTL time.Duration
}

// Service orchestrates pairing, session approval, and request routing.
type Service struct {
	mu          sync.Mutex
	relay       Relay
	store       SessionStore
	now         func() time.Time
	newID       func() (string, error)
	proposalTTL time.Duration
	sessionTTL  time.Duration
	proposals   map[int64]*pendingProposal
	keys        map[string][]byte
	onProposal  ProposalHandler
	onRequest   RequestHandler
}

type pendingProposal struct {
	proposal *Proposal
	expires  time.Time
}

// NewService creates a WalletConnect v2 service. The relay may be attached
// later via AttachRelay; the store is required.
func NewService(relay Relay, store SessionStore, options Options) (*Service, error) {
	if store == nil {
		return nil, fmt.Errorf("walletconnect: store is required")
	}
	service := &Service{relay: relay, store: store, proposals: make(map[int64]*pendingProposal), keys: make(map[string][]byte)}
	service.now = options.Now
	if service.now == nil {
		service.now = time.Now
	}
	service.newID = options.NewID
	if service.newID == nil {
		service.newID = randomTopic
	}
	service.proposalTTL = options.ProposalTTL
	if service.proposalTTL <= 0 {
		service.proposalTTL = 5 * time.Minute
	}
	service.sessionTTL = options.SessionTTL
	if service.sessionTTL <= 0 {
		service.sessionTTL = 7 * 24 * time.Hour
	}
	return service, nil
}

// OnProposal registers the proposal approval hook.
func (service *Service) OnProposal(handler ProposalHandler) {
	service.mu.Lock()
	service.onProposal = handler
	service.mu.Unlock()
}

// OnRequest registers the session request approval hook.
func (service *Service) OnRequest(handler RequestHandler) {
	service.mu.Lock()
	service.onRequest = handler
	service.mu.Unlock()
}

// SetKey binds an AES-256 key to a topic for inbound envelope decryption.
func (service *Service) SetKey(topic string, key []byte) error {
	if err := ValidateTopic(topic); err != nil {
		return err
	}
	if len(key) != SymKeyBytes {
		return fmt.Errorf("walletconnect: invalid symmetric key size")
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	service.keys[topic] = append([]byte(nil), key...)
	return nil
}

// Topics returns the encrypted topics that must be restored after reconnect.
func (service *Service) Topics() []string {
	service.mu.Lock()
	defer service.mu.Unlock()
	topics := make([]string, 0, len(service.keys))
	for topic := range service.keys {
		topics = append(topics, topic)
	}
	return topics
}

// AttachRelay binds a live relay transport after construction, enabling the
// push loop and outbound publish/subscribe.
func (service *Service) AttachRelay(relay Relay) {
	service.mu.Lock()
	service.relay = relay
	service.mu.Unlock()
}

// Run consumes relay subscriptions and delivers them until the context ends.
func (service *Service) Run(ctx context.Context) {
	service.mu.Lock()
	relay := service.relay
	service.mu.Unlock()
	if relay == nil {
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case subscription, ok := <-relay.Messages():
			if !ok {
				return
			}
			service.mu.Lock()
			key, exists := service.keys[subscription.Topic]
			service.mu.Unlock()
			if !exists {
				continue
			}
			plaintext, err := DecryptEnvelope(key, subscription.Data)
			if err != nil {
				continue
			}
			if err := service.Deliver(ctx, subscription.Topic, plaintext); err != nil {
				_ = err
			}
		}
	}
}

// Deliver processes a decrypted relay message addressed to this wallet.
func (service *Service) Deliver(ctx context.Context, topic string, payload []byte) error {
	if err := ValidateTopic(topic); err != nil {
		return err
	}
	if len(payload) == 0 || len(payload) > MaxEnvelopeBytes {
		return fmt.Errorf("walletconnect: payload size")
	}
	envelope := struct {
		Type    string          `json:"type"`
		Payload json.RawMessage `json:"payload"`
	}{}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return fmt.Errorf("walletconnect: envelope decode: %w", err)
	}
	switch envelope.Type {
	case "session_proposal":
		var proposal Proposal
		if err := json.Unmarshal(envelope.Payload, &proposal); err != nil {
			return fmt.Errorf("walletconnect: proposal decode: %w", err)
		}
		return service.handleProposal(ctx, topic, &proposal)
	case "session_request":
		var params SessionRequestParams
		if err := json.Unmarshal(envelope.Payload, &params); err != nil {
			return fmt.Errorf("walletconnect: session request decode: %w", err)
		}
		return service.handleSessionRequest(ctx, topic, &params)
	case "session_delete":
		_ = service.store.RevokeSession(ctx, topic, service.now().UnixMilli())
		return nil
	default:
		return fmt.Errorf("walletconnect: unsupported envelope type %q", envelope.Type)
	}
}

func (service *Service) handleProposal(ctx context.Context, topic string, proposal *Proposal) error {
	if _, err := ValidateProposal(proposal); err != nil {
		return err
	}
	if proposal.Expiry <= service.now().UnixMilli() {
		return fmt.Errorf("walletconnect: proposal already expired")
	}
	service.mu.Lock()
	handler := service.onProposal
	if handler != nil {
		service.proposals[proposal.ID] = &pendingProposal{
			proposal: proposal,
			expires:  service.now().Add(service.proposalTTL),
		}
	}
	service.mu.Unlock()
	if handler == nil {
		return fmt.Errorf("walletconnect: proposal handler is unavailable")
	}
	return handler(ctx, proposal)
}

func (service *Service) handleSessionRequest(ctx context.Context, topic string, params *SessionRequestParams) error {
	if err := ValidateSessionRequest(params); err != nil {
		return err
	}
	session, err := service.store.GetSession(ctx, topic)
	if err != nil {
		return err
	}
	if session.Revoked || session.ExpiresAt <= service.now().UnixMilli() {
		return fmt.Errorf("walletconnect: session is not active")
	}
	if !sessionAllowsRequest(session, params) {
		return fmt.Errorf("walletconnect: request outside approved session scope")
	}
	service.mu.Lock()
	handler := service.onRequest
	service.mu.Unlock()
	if handler == nil {
		return fmt.Errorf("walletconnect: request handler is unavailable")
	}
	if err := handler(ctx, session, params); err != nil {
		return err
	}
	return service.store.TouchSession(ctx, topic, service.now().UnixMilli())
}

// sessionAllowsRequest enforces the approved namespaces: the request method
// must be approved and the chain must be covered by the session chains.
func sessionAllowsRequest(session *Session, params *SessionRequestParams) bool {
	chainNamespace, chainID, err := ParseNamespaceChain(params.ChainID)
	if err != nil {
		return false
	}
	namespace, exists := session.Namespaces[chainNamespace]
	if !exists {
		return false
	}
	methodApproved := false
	for _, method := range namespace.Methods {
		if method == params.Request.Method {
			methodApproved = true
			break
		}
	}
	if !methodApproved {
		return false
	}
	expectedChain := chainNamespace + ":" + chainID
	for _, chain := range namespace.Chains {
		if chain == expectedChain {
			return true
		}
	}
	return false
}

// ApproveProposal approves a pending proposal and persists the session.
func (service *Service) ApproveProposal(ctx context.Context, proposalID int64, accountID, address string) (*Session, error) {
	service.mu.Lock()
	pending, exists := service.proposals[proposalID]
	if !exists {
		service.mu.Unlock()
		return nil, fmt.Errorf("walletconnect: proposal not pending")
	}
	if service.now().After(pending.expires) || pending.proposal.Expiry <= service.now().UnixMilli() {
		delete(service.proposals, proposalID)
		service.mu.Unlock()
		return nil, fmt.Errorf("walletconnect: proposal expired")
	}
	proposal := pending.proposal
	delete(service.proposals, proposalID)
	service.mu.Unlock()
	namespaces, err := ApproveNamespaces(proposal.RequiredNamespaces, address)
	if err != nil {
		return nil, err
	}
	topic, err := service.newID()
	if err != nil {
		return nil, fmt.Errorf("walletconnect: session topic: %w", err)
	}
	if err := ValidateTopic(topic); err != nil {
		return nil, fmt.Errorf("walletconnect: session topic generator: %w", err)
	}
	now := service.now().UnixMilli()
	session := &Session{
		Topic:        topic,
		PeerName:     proposal.Proposer.Metadata.Name,
		PeerMetadata: peerMetadataMap(&proposal.Proposer),
		AccountID:    accountID,
		Namespaces:   namespaces,
		ExpiresAt:    now + service.sessionTTL.Milliseconds(),
		CreatedAt:    now,
	}
	if err := service.store.SaveSession(ctx, session); err != nil {
		return nil, err
	}
	return session, nil
}

// RejectProposal drops a pending proposal.
func (service *Service) RejectProposal(proposalID int64) {
	service.mu.Lock()
	defer service.mu.Unlock()
	delete(service.proposals, proposalID)
}

// ListSessions delegates to the session store.
func (service *Service) ListSessions(ctx context.Context, accountID string, includeRevoked bool) ([]Session, error) {
	return service.store.ListSessions(ctx, accountID, includeRevoked)
}

// RevokeSession delegates to the session store.
func (service *Service) RevokeSession(ctx context.Context, topic string, revokedAt int64) error {
	return service.store.RevokeSession(ctx, topic, revokedAt)
}

// PendingProposal returns a pending proposal if it exists and is fresh.
func (service *Service) PendingProposal(proposalID int64) (*Proposal, bool) {
	service.sweepProposals()
	service.mu.Lock()
	defer service.mu.Unlock()
	pending, exists := service.proposals[proposalID]
	if !exists {
		return nil, false
	}
	return pending.proposal, true
}

func peerMetadataMap(peer *PeerMetadata) map[string]any {
	return map[string]any{
		"name":        peer.Metadata.Name,
		"description": peer.Metadata.Description,
		"url":         peer.Metadata.URL,
		"icons":       peer.Metadata.Icons,
	}
}

func (service *Service) sweepProposals() {
	now := service.now()
	service.mu.Lock()
	defer service.mu.Unlock()
	for id, pending := range service.proposals {
		if now.After(pending.expires) || pending.proposal.Expiry <= now.UnixMilli() {
			delete(service.proposals, id)
		}
	}
}

func randomTopic() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("walletconnect: topic entropy: %w", err)
	}
	return hex.EncodeToString(raw), nil
}
