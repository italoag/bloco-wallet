package wallet

import (
	"context"
	"crypto/sha256"
	"fmt"

	"blocowallet/internal/terminal"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// HardwareSignerImportRequest binds an account to a public key discovered
// from a Ledger or Trezor at a canonical derivation path.
type HardwareSignerImportRequest struct {
	Name               string
	DeviceKind         string
	DerivationPath     string
	PublicKey          []byte
	ExpectedAddress    common.Address
	AuthorizationEpoch uint64
}

// ImportHardwareSignerAccount creates a custody-free account only after the
// device public key proves its address and derivation binding.
func ImportHardwareSignerAccount(ctx context.Context, repository AccountRepository, request HardwareSignerImportRequest) (*Account, error) {
	if repository == nil {
		return nil, fmt.Errorf("hardware signer repository is required")
	}
	if request.Name == "" || len(request.Name) > 64 || terminal.SanitizeInline(request.Name, 64) != request.Name {
		return nil, fmt.Errorf("hardware account name is invalid")
	}
	if request.DeviceKind != "ledger" && request.DeviceKind != "trezor" {
		return nil, fmt.Errorf("unsupported hardware signer kind")
	}
	path, err := ParseDerivationPath(request.DerivationPath)
	if err != nil {
		return nil, fmt.Errorf("hardware derivation path: %w", err)
	}
	var publicKeyAddress common.Address
	switch len(request.PublicKey) {
	case 33:
		publicKey, err := crypto.DecompressPubkey(request.PublicKey)
		if err != nil {
			return nil, fmt.Errorf("hardware public key: %w", err)
		}
		publicKeyAddress = crypto.PubkeyToAddress(*publicKey)
	case 65:
		publicKey, err := crypto.UnmarshalPubkey(request.PublicKey)
		if err != nil {
			return nil, fmt.Errorf("hardware public key: %w", err)
		}
		publicKeyAddress = crypto.PubkeyToAddress(*publicKey)
	default:
		return nil, fmt.Errorf("hardware public key length is invalid")
	}
	if publicKeyAddress == (common.Address{}) || request.ExpectedAddress == (common.Address{}) || publicKeyAddress != request.ExpectedAddress {
		return nil, fmt.Errorf("hardware public key does not match expected address")
	}
	sourceIdentity := sha256.Sum256([]byte(request.DeviceKind + "\x00" + path.String() + "\x00" + publicKeyAddress.Hex()))
	epoch := request.AuthorizationEpoch
	if epoch == 0 {
		epoch = 1
	}
	account := &Account{
		AccountID: newAccountID(), Name: request.Name, Address: publicKeyAddress.Hex(),
		SignerKind: SignerKindHardware, SignerReference: request.DeviceKind + ":v1:" + path.String(),
		DerivationScheme: "bip44", DerivationPath: path.String(),
		Capabilities: CapabilitySignTransaction | CapabilitySignMessage,
		State:        AccountStateActive, SourceIdentity: fmt.Sprintf("%x", sourceIdentity),
		AuthorizationEpoch: epoch, BackupGeneration: 1,
	}
	if err := account.Validate(); err != nil {
		return nil, err
	}
	if err := repository.CreateAccount(ctx, account); err != nil {
		return nil, err
	}
	return account, nil
}
