package evm

import (
	"math/big"
	"regexp"

	"github.com/ethereum/go-ethereum/common"
)

var accountIDPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

type NativeTransferIntent struct {
	accountID string
	chainID   uint64
	from      common.Address
	to        common.Address
	amount    *big.Int
}

func NewNativeTransferIntent(accountID string, chainID uint64, from, to common.Address, amount *big.Int) (NativeTransferIntent, error) {
	if !accountIDPattern.MatchString(accountID) {
		return NativeTransferIntent{}, invalidIntent("account ID")
	}
	if chainID == 0 {
		return NativeTransferIntent{}, invalidIntent("chain ID")
	}
	if from == (common.Address{}) {
		return NativeTransferIntent{}, invalidIntent("sender")
	}
	if to == (common.Address{}) {
		return NativeTransferIntent{}, invalidIntent("recipient")
	}
	if amount == nil || amount.Sign() <= 0 || amount.BitLen() > 256 {
		return NativeTransferIntent{}, invalidIntent("amount")
	}
	return NativeTransferIntent{
		accountID: accountID,
		chainID:   chainID,
		from:      from,
		to:        to,
		amount:    new(big.Int).Set(amount),
	}, nil
}

func (intent NativeTransferIntent) AccountID() string { return intent.accountID }
func (intent NativeTransferIntent) ChainID() uint64   { return intent.chainID }
func (intent NativeTransferIntent) From() common.Address {
	return intent.from
}
func (intent NativeTransferIntent) To() common.Address { return intent.to }
func (intent NativeTransferIntent) Amount() *big.Int {
	if intent.amount == nil {
		return nil
	}
	return new(big.Int).Set(intent.amount)
}

type ERC20TransferIntent struct {
	accountID string
	chainID   uint64
	from      common.Address
	contract  common.Address
	to        common.Address
	amount    *big.Int
}

type ERC20ApproveIntent struct {
	accountID string
	chainID   uint64
	from      common.Address
	contract  common.Address
	spender   common.Address
	amount    *big.Int
}

func NewERC20ApproveIntent(accountID string, chainID uint64, from, contract, spender common.Address, amount *big.Int) (ERC20ApproveIntent, error) {
	if !accountIDPattern.MatchString(accountID) || chainID == 0 || from == (common.Address{}) || contract == (common.Address{}) || spender == (common.Address{}) || amount == nil || amount.Sign() < 0 || amount.BitLen() > 256 {
		return ERC20ApproveIntent{}, invalidIntent("ERC-20 approval")
	}
	unlimited := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
	if amount.Cmp(unlimited) == 0 || amount.BitLen() > 128 {
		return ERC20ApproveIntent{}, &EngineError{Code: ErrorPolicyDenied, Field: "unlimited or excessive ERC-20 approval"}
	}
	return ERC20ApproveIntent{
		accountID: accountID, chainID: chainID, from: from, contract: contract, spender: spender,
		amount: new(big.Int).Set(amount),
	}, nil
}

type ERC721SafeTransferIntent struct {
	accountID string
	chainID   uint64
	from      common.Address
	contract  common.Address
	to        common.Address
	tokenID   *big.Int
}

func NewERC721SafeTransferIntent(accountID string, chainID uint64, from, contract, to common.Address, tokenID *big.Int) (ERC721SafeTransferIntent, error) {
	if !accountIDPattern.MatchString(accountID) {
		return ERC721SafeTransferIntent{}, invalidIntent("account ID")
	}
	if chainID == 0 {
		return ERC721SafeTransferIntent{}, invalidIntent("chain ID")
	}
	if from == (common.Address{}) {
		return ERC721SafeTransferIntent{}, invalidIntent("sender")
	}
	if contract == (common.Address{}) {
		return ERC721SafeTransferIntent{}, invalidIntent("token contract")
	}
	if to == (common.Address{}) {
		return ERC721SafeTransferIntent{}, invalidIntent("recipient")
	}
	if tokenID == nil || tokenID.Sign() < 0 || tokenID.BitLen() > 256 {
		return ERC721SafeTransferIntent{}, invalidIntent("token ID")
	}
	return ERC721SafeTransferIntent{
		accountID: accountID, chainID: chainID, from: from, contract: contract, to: to,
		tokenID: new(big.Int).Set(tokenID),
	}, nil
}

func (intent ERC721SafeTransferIntent) AccountID() string { return intent.accountID }
func (intent ERC721SafeTransferIntent) ChainID() uint64   { return intent.chainID }
func (intent ERC721SafeTransferIntent) From() common.Address {
	return intent.from
}
func (intent ERC721SafeTransferIntent) Contract() common.Address {
	return intent.contract
}
func (intent ERC721SafeTransferIntent) To() common.Address { return intent.to }
func (intent ERC721SafeTransferIntent) TokenID() *big.Int {
	if intent.tokenID == nil {
		return nil
	}
	return new(big.Int).Set(intent.tokenID)
}

type ERC1155SafeTransferIntent struct {
	accountID string
	chainID   uint64
	from      common.Address
	contract  common.Address
	to        common.Address
	tokenID   *big.Int
	amount    *big.Int
}

func NewERC1155SafeTransferIntent(accountID string, chainID uint64, from, contract, to common.Address, tokenID, amount *big.Int) (ERC1155SafeTransferIntent, error) {
	if !accountIDPattern.MatchString(accountID) || chainID == 0 || from == (common.Address{}) || contract == (common.Address{}) || to == (common.Address{}) {
		return ERC1155SafeTransferIntent{}, invalidIntent("ERC-1155 transfer")
	}
	if tokenID == nil || tokenID.Sign() < 0 || tokenID.BitLen() > 256 || amount == nil || amount.Sign() <= 0 || amount.BitLen() > 256 {
		return ERC1155SafeTransferIntent{}, invalidIntent("ERC-1155 token")
	}
	return ERC1155SafeTransferIntent{
		accountID: accountID, chainID: chainID, from: from, contract: contract, to: to,
		tokenID: new(big.Int).Set(tokenID), amount: new(big.Int).Set(amount),
	}, nil
}

func (intent ERC1155SafeTransferIntent) AccountID() string { return intent.accountID }
func (intent ERC1155SafeTransferIntent) ChainID() uint64   { return intent.chainID }
func (intent ERC1155SafeTransferIntent) From() common.Address {
	return intent.from
}
func (intent ERC1155SafeTransferIntent) Contract() common.Address {
	return intent.contract
}
func (intent ERC1155SafeTransferIntent) To() common.Address { return intent.to }
func (intent ERC1155SafeTransferIntent) TokenID() *big.Int {
	return new(big.Int).Set(intent.tokenID)
}
func (intent ERC1155SafeTransferIntent) Amount() *big.Int {
	return new(big.Int).Set(intent.amount)
}

type ERC1155BatchTransferIntent struct {
	accountID string
	chainID   uint64
	from      common.Address
	contract  common.Address
	to        common.Address
	effects   []EffectEntry
}

func NewERC1155BatchTransferIntent(accountID string, chainID uint64, from, contract, to common.Address, effects []EffectEntry) (ERC1155BatchTransferIntent, error) {
	if !accountIDPattern.MatchString(accountID) || chainID == 0 || from == (common.Address{}) || contract == (common.Address{}) || to == (common.Address{}) {
		return ERC1155BatchTransferIntent{}, invalidIntent("ERC-1155 batch")
	}
	if len(effects) == 0 || len(effects) > 64 {
		return ERC1155BatchTransferIntent{}, invalidIntent("ERC-1155 batch effects")
	}
	for _, effect := range effects {
		if effect.TokenID == nil || effect.Amount == nil || effect.TokenID.Sign() < 0 || effect.TokenID.BitLen() > 256 || effect.Amount.Sign() <= 0 || effect.Amount.BitLen() > 256 {
			return ERC1155BatchTransferIntent{}, invalidIntent("ERC-1155 batch effect")
		}
	}
	return ERC1155BatchTransferIntent{
		accountID: accountID, chainID: chainID, from: from, contract: contract, to: to,
		effects: CloneEffectEntries(effects),
	}, nil
}

func (intent ERC1155BatchTransferIntent) AccountID() string { return intent.accountID }
func (intent ERC1155BatchTransferIntent) ChainID() uint64   { return intent.chainID }
func (intent ERC1155BatchTransferIntent) From() common.Address {
	return intent.from
}
func (intent ERC1155BatchTransferIntent) Contract() common.Address {
	return intent.contract
}
func (intent ERC1155BatchTransferIntent) To() common.Address { return intent.to }
func (intent ERC1155BatchTransferIntent) Effects() []EffectEntry {
	return CloneEffectEntries(intent.effects)
}

func NewERC20TransferIntent(accountID string, chainID uint64, from, contract, to common.Address, amount *big.Int) (ERC20TransferIntent, error) {
	if !accountIDPattern.MatchString(accountID) {
		return ERC20TransferIntent{}, invalidIntent("account ID")
	}
	if chainID == 0 {
		return ERC20TransferIntent{}, invalidIntent("chain ID")
	}
	if from == (common.Address{}) {
		return ERC20TransferIntent{}, invalidIntent("sender")
	}
	if contract == (common.Address{}) {
		return ERC20TransferIntent{}, invalidIntent("token contract")
	}
	if to == (common.Address{}) {
		return ERC20TransferIntent{}, invalidIntent("recipient")
	}
	if amount == nil || amount.Sign() <= 0 || amount.BitLen() > 256 {
		return ERC20TransferIntent{}, invalidIntent("amount")
	}
	return ERC20TransferIntent{
		accountID: accountID,
		chainID:   chainID,
		from:      from,
		contract:  contract,
		to:        to,
		amount:    new(big.Int).Set(amount),
	}, nil
}
