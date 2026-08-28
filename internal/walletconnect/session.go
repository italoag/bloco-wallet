package walletconnect

import (
	"encoding/json"
	"fmt"
	"strings"
)

const (
	// MaxNamespaces bounds session namespaces.
	MaxNamespaces = 32
	// MaxChainsPerNamespace bounds chains in a namespace.
	MaxChainsPerNamespace = 32
	// MaxMethodsPerNamespace bounds methods in a namespace.
	MaxMethodsPerNamespace = 64
	// MaxEventsPerNamespace bounds events in a namespace.
	MaxEventsPerNamespace = 32
	// MaxAccountsPerNamespace bounds approved accounts.
	MaxAccountsPerNamespace = 64
	// MaxMethodText bounds method identifiers.
	MaxMethodText = 128
	// MaxChainText bounds chain identifiers.
	MaxChainText = 128
)

// Namespace maps namespace keys (eip155) to their scopes.
type Namespace struct {
	Chains   []string `json:"chains"`
	Methods  []string `json:"methods"`
	Events   []string `json:"events"`
	Accounts []string `json:"accounts,omitempty"`
}

// Namespaces is a set of namespace scopes keyed by namespace name.
type Namespaces map[string]Namespace

// RequiredNamespaces mirrors the WalletConnect proposal structure.
type RequiredNamespaces map[string]Namespace

// Proposal is a parsed session proposal.
type Proposal struct {
	ID                 int64               `json:"id"`
	PairingTopic       string              `json:"pairingTopic"`
	Expiry             int64               `json:"expiry"`
	RequiredNamespaces RequiredNamespaces  `json:"requiredNamespaces"`
	OptionalNamespaces *RequiredNamespaces `json:"optionalNamespaces,omitempty"`
	Proposer           PeerMetadata        `json:"proposer"`
}

// PeerMetadata describes the dapp peer.
type PeerMetadata struct {
	PublicKey string `json:"publicKey"`
	Metadata  struct {
		Name        string   `json:"name"`
		Description string   `json:"description"`
		URL         string   `json:"url"`
		Icons       []string `json:"icons,omitempty"`
	} `json:"metadata"`
}

// ValidateProposal enforces strict scope budgets and rejects wildcards and
// unknown namespaces. It returns the required chains.
func ValidateProposal(proposal *Proposal) ([]string, error) {
	if proposal == nil {
		return nil, fmt.Errorf("walletconnect: nil proposal")
	}
	if proposal.ID <= 0 {
		return nil, fmt.Errorf("walletconnect: invalid proposal id")
	}
	if err := ValidateTopic(proposal.PairingTopic); err != nil {
		return nil, err
	}
	if proposal.Expiry <= 0 {
		return nil, fmt.Errorf("walletconnect: invalid proposal expiry")
	}
	if len(proposal.RequiredNamespaces) == 0 || len(proposal.RequiredNamespaces) > MaxNamespaces {
		return nil, fmt.Errorf("walletconnect: required namespaces budget")
	}
	if proposal.Proposer.PublicKey == "" {
		return nil, fmt.Errorf("walletconnect: proposer public key missing")
	}
	chains := make([]string, 0, MaxChainsPerNamespace)
	for name, namespace := range proposal.RequiredNamespaces {
		if name == "" || len(name) > 64 || strings.ContainsAny(name, " \t\r\n") {
			return nil, fmt.Errorf("walletconnect: invalid namespace key")
		}
		if len(namespace.Chains) == 0 || len(namespace.Chains) > MaxChainsPerNamespace {
			return nil, fmt.Errorf("walletconnect: chains budget for %s", name)
		}
		if len(namespace.Methods) == 0 || len(namespace.Methods) > MaxMethodsPerNamespace {
			return nil, fmt.Errorf("walletconnect: methods budget for %s", name)
		}
		if len(namespace.Events) > MaxEventsPerNamespace {
			return nil, fmt.Errorf("walletconnect: events budget for %s", name)
		}
		for _, chain := range namespace.Chains {
			if chain == "" || len(chain) > MaxChainText || strings.ContainsAny(chain, " \t\r\n*") {
				return nil, fmt.Errorf("walletconnect: invalid chain %q", chain)
			}
			chains = append(chains, chain)
		}
		for _, method := range namespace.Methods {
			if method == "" || len(method) > MaxMethodText || strings.ContainsAny(method, " \t\r\n*") {
				return nil, fmt.Errorf("walletconnect: invalid method %q", method)
			}
		}
		for _, event := range namespace.Events {
			if event == "" || len(event) > MaxMethodText || strings.ContainsAny(event, " \t\r\n*") {
				return nil, fmt.Errorf("walletconnect: invalid event %q", event)
			}
		}
	}
	return chains, nil
}

// ApproveNamespaces builds the approved namespaces for the given account
// address, mirroring the required scopes with the account bound.
func ApproveNamespaces(required RequiredNamespaces, address string) (Namespaces, error) {
	if len(address) == 0 {
		return nil, fmt.Errorf("walletconnect: approval address required")
	}
	approved := make(Namespaces, len(required))
	for name, namespace := range required {
		accounts := make([]string, 0, len(namespace.Chains))
		for _, chain := range namespace.Chains {
			accounts = append(accounts, chain+":"+address)
		}
		if len(accounts) > MaxAccountsPerNamespace {
			return nil, fmt.Errorf("walletconnect: accounts budget for %s", name)
		}
		approved[name] = Namespace{
			Chains:   append([]string(nil), namespace.Chains...),
			Methods:  append([]string(nil), namespace.Methods...),
			Events:   append([]string(nil), namespace.Events...),
			Accounts: accounts,
		}
	}
	return approved, nil
}

// ParseNamespaceChain splits "eip155:1" into its parts.
func ParseNamespaceChain(chain string) (namespace string, chainID string, err error) {
	parts := strings.Split(chain, ":")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("walletconnect: malformed chain %q", chain)
	}
	return parts[0], parts[1], nil
}

// SessionRequestParams is the decrypted session_request params.
type SessionRequestParams struct {
	Request struct {
		ID     int64           `json:"id"`
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
	} `json:"request"`
	ChainID string `json:"chainId"`
}

// ValidateSessionRequest enforces method and chain budgets.
func ValidateSessionRequest(params *SessionRequestParams) error {
	if params == nil {
		return fmt.Errorf("walletconnect: nil request params")
	}
	if params.Request.ID <= 0 || params.Request.Method == "" || len(params.Request.Method) > MaxMethodText {
		return fmt.Errorf("walletconnect: invalid request identity")
	}
	if params.ChainID == "" || len(params.ChainID) > MaxChainText {
		return fmt.Errorf("walletconnect: invalid request chain")
	}
	if len(params.Request.Params) == 0 || len(params.Request.Params) > MaxEnvelopeBytes {
		return fmt.Errorf("walletconnect: invalid request params")
	}
	return nil
}
