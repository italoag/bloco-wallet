package wallet

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

type AccountState string

const (
	AccountStatePendingBackup AccountState = "pending_backup"
	AccountStateActive        AccountState = "active"
	AccountStateLocked        AccountState = "locked"
	AccountStateUnavailable   AccountState = "unavailable"
	AccountStateTombstoned    AccountState = "tombstoned"
)

type SignerKind string

const (
	SignerKindSoftware  SignerKind = "software"
	SignerKindWatchOnly SignerKind = "watch_only"
	SignerKindHardware  SignerKind = "hardware"
	SignerKindCloud     SignerKind = "cloud"
	SignerKindMultisig  SignerKind = "multisig"
)

type AccountCapability uint64

const (
	CapabilitySignTransaction AccountCapability = 1 << iota
	CapabilitySignMessage
	CapabilityExportSecret
)

type Account struct {
	AccountID          string            `gorm:"primaryKey;size:36"`
	Name               string            `gorm:"not null"`
	Address            string            `gorm:"index;size:42;not null"`
	SignerKind         SignerKind        `gorm:"index;size:32;not null"`
	SignerReference    string            `gorm:"size:255;not null"`
	SecretType         SecretType        `gorm:"size:32"`
	DerivationScheme   string            `gorm:"size:32"`
	DerivationPath     string            `gorm:"size:255"`
	AccountIndex       uint32            `gorm:"not null;default:0"`
	ChangeIndex        uint32            `gorm:"not null;default:0"`
	AddressIndex       uint32            `gorm:"not null;default:0"`
	BIP39Language      string            `gorm:"size:32"`
	Capabilities       AccountCapability `gorm:"not null"`
	State              AccountState      `gorm:"index;size:32;not null"`
	SecretEnvelope     []byte            `gorm:"type:blob"`
	EnvelopeGeneration uint64            `gorm:"not null;default:1"`
	AuthorizationEpoch uint64            `gorm:"not null;default:1"`
	BackupGeneration   uint64            `gorm:"not null;default:1"`
	SourceIdentity     string            `gorm:"uniqueIndex;size:128;not null"`
	Revision           uint64            `gorm:"not null;default:1"`
	CreatedAt          time.Time         `gorm:"not null;autoCreateTime"`
	UpdatedAt          time.Time         `gorm:"not null;autoUpdateTime"`
}

func (Account) TableName() string {
	return "accounts"
}

func (account *Account) Validate() error {
	if account == nil {
		return fmt.Errorf("account is required")
	}
	matched, _ := regexp.MatchString(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`, account.AccountID)
	if !matched {
		return fmt.Errorf("account ID must be a canonical UUID v4")
	}
	if account.Name == "" || !common.IsHexAddress(account.Address) || common.HexToAddress(account.Address).Hex() != account.Address {
		return fmt.Errorf("account name and checksummed address are required")
	}
	if account.SignerKind == "" || account.SignerReference == "" || account.State == "" || account.SourceIdentity == "" || account.AuthorizationEpoch == 0 || account.BackupGeneration == 0 {
		return fmt.Errorf("account signer, state, source identity, and authorization epoch are required")
	}
	switch account.State {
	case AccountStatePendingBackup, AccountStateActive, AccountStateLocked, AccountStateUnavailable, AccountStateTombstoned:
	default:
		return fmt.Errorf("invalid account state")
	}
	switch account.SignerKind {
	case SignerKindSoftware:
		if (account.SecretType != SecretTypeMnemonic && account.SecretType != SecretTypePrivateKey) || len(account.SecretEnvelope) == 0 || account.EnvelopeGeneration == 0 {
			return fmt.Errorf("software account requires an encrypted secret")
		}
	case SignerKindWatchOnly:
		if account.Capabilities&(CapabilitySignTransaction|CapabilitySignMessage|CapabilityExportSecret) != 0 || len(account.SecretEnvelope) != 0 {
			return fmt.Errorf("watch-only account cannot sign or store secrets")
		}
	case SignerKindHardware, SignerKindCloud, SignerKindMultisig:
		if len(account.SecretEnvelope) != 0 {
			return fmt.Errorf("external signer account cannot store software secrets")
		}
	default:
		return fmt.Errorf("invalid signer kind")
	}
	return nil
}

var (
	ErrAccountNotFound         = errors.New("account not found")
	ErrAccountConflict         = errors.New("account identity conflict")
	ErrAccountRevisionConflict = errors.New("account revision conflict")
)

type AccountRepository interface {
	CreateAccount(ctx context.Context, account *Account) error
	GetAccount(ctx context.Context, accountID string) (*Account, error)
	FindAccountBySourceIdentity(ctx context.Context, sourceIdentity string) (*Account, error)
	ListAccounts(ctx context.Context) ([]Account, error)
	UpdateAccount(ctx context.Context, account *Account) error
	DeletePendingAccount(ctx context.Context, accountID string, backupGeneration uint64) error
	WithAccountTransaction(ctx context.Context, operation func(AccountRepository) error) error
}
