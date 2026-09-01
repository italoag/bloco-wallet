package wallet

import (
	"context"
	"crypto/rand"
	"fmt"
	"net"
	"net/url"
	"strings"

	"github.com/ethereum/go-ethereum/common"
)

// ExternalSignerImportRequest describes a cloud or multisig account import.
type ExternalSignerImportRequest struct {
	Name               string
	Address            string
	SignerKind         SignerKind
	Reference          string
	Capabilities       AccountCapability
	AuthorizationEpoch uint64
}

// ImportExternalSignerAccount creates a custodial-free account backed by an
// external signer reference (cloud endpoint or Safe address). The account
// never stores secrets; cloud signing capabilities must already be verified.
func ImportExternalSignerAccount(ctx context.Context, repository AccountRepository, request ExternalSignerImportRequest) (*Account, error) {
	if request.SignerKind != SignerKindCloud && request.SignerKind != SignerKindMultisig {
		return nil, fmt.Errorf("unsupported external signer kind")
	}
	allowedCloudCapabilities := CapabilitySignTransaction | CapabilitySignMessage
	if request.SignerKind == SignerKindCloud && (request.Capabilities == 0 || request.Capabilities&^allowedCloudCapabilities != 0) {
		return nil, fmt.Errorf("cloud signer capabilities must be verified before import")
	}
	if request.SignerKind == SignerKindMultisig && request.Capabilities != 0 {
		return nil, fmt.Errorf("multisig capabilities are coordinator-managed")
	}
	if request.Name == "" || len(request.Name) > 64 {
		return nil, fmt.Errorf("account name is required")
	}
	if !common.IsHexAddress(request.Address) || common.HexToAddress(request.Address).Hex() != request.Address || common.HexToAddress(request.Address) == (common.Address{}) {
		return nil, fmt.Errorf("checksummed non-zero account address is required")
	}
	if request.Reference == "" || len(request.Reference) > 255 {
		return nil, fmt.Errorf("signer reference is required")
	}
	if strings.ContainsAny(request.Reference, "\x00\r\n") {
		return nil, fmt.Errorf("invalid signer reference")
	}
	if request.SignerKind == SignerKindCloud && !validCloudSignerReference(request.Reference) {
		return nil, fmt.Errorf("invalid cloud signer reference")
	}
	if request.SignerKind == SignerKindMultisig && request.Reference != "safe:v1:"+request.Address {
		return nil, fmt.Errorf("invalid multisig signer reference")
	}
	accountID := newAccountID()
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return nil, fmt.Errorf("source identity entropy: %w", err)
	}
	account := &Account{
		AccountID:          accountID,
		Name:               request.Name,
		Address:            request.Address,
		SignerKind:         request.SignerKind,
		SignerReference:    request.Reference,
		State:              AccountStateActive,
		Capabilities:       request.Capabilities,
		SourceIdentity:     fmt.Sprintf("%x", raw),
		AuthorizationEpoch: request.AuthorizationEpoch,
		BackupGeneration:   1,
	}
	if request.AuthorizationEpoch == 0 {
		account.AuthorizationEpoch = 1
	}
	if err := account.Validate(); err != nil {
		return nil, err
	}
	if err := repository.CreateAccount(ctx, account); err != nil {
		return nil, err
	}
	return account, nil
}

func validCloudSignerReference(reference string) bool {
	endpoint := strings.TrimPrefix(reference, "cloud:v1:")
	if endpoint == reference || endpoint == "" || strings.HasSuffix(endpoint, "/") {
		return false
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Scheme != "https" && parsed.Scheme != "http") {
		return false
	}
	if parsed.Scheme == "https" {
		return true
	}
	host := parsed.Hostname()
	ip := net.ParseIP(host)
	return host == "localhost" || (ip != nil && ip.IsLoopback())
}

func newAccountID() string {
	value, err := newUUID(rand.Reader)
	if err != nil {
		panic(fmt.Sprintf("account id entropy: %v", err))
	}
	return value
}
