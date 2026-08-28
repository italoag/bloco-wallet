package wallet

import (
	"context"
	"crypto/rand"
	"fmt"
	"strings"

	"github.com/ethereum/go-ethereum/common"
)

// ExternalSignerImportRequest describes a cloud or multisig account import.
type ExternalSignerImportRequest struct {
	Name               string
	Address            string
	SignerKind         SignerKind
	Reference          string
	AuthorizationEpoch uint64
}

// ImportExternalSignerAccount creates a custodial-free account backed by an
// external signer reference (cloud endpoint or Safe address). The account
// never stores secrets or signing capabilities.
func ImportExternalSignerAccount(ctx context.Context, repository AccountRepository, request ExternalSignerImportRequest) (*Account, error) {
	if request.SignerKind != SignerKindCloud && request.SignerKind != SignerKindMultisig {
		return nil, fmt.Errorf("unsupported external signer kind")
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
		Capabilities:       0,
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

func newAccountID() string {
	value, err := newUUID(rand.Reader)
	if err != nil {
		panic(fmt.Sprintf("account id entropy: %v", err))
	}
	return value
}
