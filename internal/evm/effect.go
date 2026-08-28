package evm

import (
	"bytes"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

type EffectKind string

const (
	EffectERC20Transfer   EffectKind = "erc20_transfer"
	EffectERC20Approval   EffectKind = "erc20_approval"
	EffectERC721Transfer  EffectKind = "erc721_transfer"
	EffectERC1155Transfer EffectKind = "erc1155_transfer"
	EffectERC1155Batch    EffectKind = "erc1155_batch"
)

const (
	erc721TransferTopic = "0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef"
	erc1155SingleTopic  = "0xc3d58168c5ae7397731d063d5bbf3d657854427343f4c083240f7aacaa2d0f62"
	erc1155BatchTopic   = "0x4a39dc06d4c0dbc64b70af90fd698a233a518aa5d07e595d983b8c0526c8f7fb"
)

type EffectEntry struct {
	TokenID *big.Int
	Amount  *big.Int
}

func CloneEffectEntries(entries []EffectEntry) []EffectEntry {
	cloned := make([]EffectEntry, len(entries))
	for index, entry := range entries {
		cloned[index] = EffectEntry{
			TokenID: new(big.Int).Set(entry.TokenID),
			Amount:  new(big.Int).Set(entry.Amount),
		}
	}
	return cloned
}

type TransferEffect struct {
	Kind          EffectKind
	AssetContract common.Address
	From          common.Address
	To            common.Address
	Amount        *big.Int
	TokenID       *big.Int
	Entries       []EffectEntry
}

func EffectFromRecord(record TransactionRecord) (*TransferEffect, error) {
	if record.AssetContract == (common.Address{}) || record.Counterparty == (common.Address{}) || record.AssetAmount == nil || record.Sender == (common.Address{}) {
		return nil, invalidIntent("effect record")
	}
	effect := &TransferEffect{
		Kind:          EffectERC20Transfer,
		AssetContract: record.AssetContract,
		From:          record.Sender,
		To:            record.Counterparty,
		Amount:        new(big.Int).Set(record.AssetAmount),
	}
	switch record.Operation {
	case OperationERC20Transfer:
	case OperationERC20Approve:
		effect.Kind = EffectERC20Approval
	case OperationERC721SafeTransfer:
		effect.Kind = EffectERC721Transfer
		effect.Amount = nil
		effect.TokenID = new(big.Int).Set(record.AssetAmount)
	case OperationERC1155SafeTransfer:
		if record.TokenID == nil {
			return nil, invalidIntent("ERC-1155 effect token")
		}
		effect.Kind = EffectERC1155Transfer
		effect.TokenID = new(big.Int).Set(record.TokenID)
	case OperationERC1155BatchTransfer:
		effect.Kind = EffectERC1155Batch
		effect.TokenID = nil
		effect.Amount = nil
		effect.Entries = CloneEffectEntries(record.Effects)
	default:
		return nil, invalidIntent("effect operation")
	}
	return effect, nil
}

func (effect *TransferEffect) Matches(log ReceiptLog) bool {
	if effect == nil || log.Address != effect.AssetContract {
		return false
	}
	switch effect.Kind {
	case EffectERC20Transfer, EffectERC20Approval:
		return effect.matchesERC20(log)
	case EffectERC721Transfer:
		return effect.matchesERC721(log)
	case EffectERC1155Transfer:
		return effect.matchesERC1155Single(log)
	case EffectERC1155Batch:
		return effect.matchesERC1155Batch(log)
	default:
		return false
	}
}

func (effect *TransferEffect) matchesERC20(log ReceiptLog) bool {
	if len(log.Topics) < 3 || log.Topics[1] != common.BytesToHash(effect.From.Bytes()) || log.Topics[2] != common.BytesToHash(effect.To.Bytes()) {
		return false
	}
	eventSignature := "Transfer(address,address,uint256)"
	if effect.Kind == EffectERC20Approval {
		eventSignature = "Approval(address,address,uint256)"
	}
	if log.Topics[0] != crypto.Keccak256Hash([]byte(eventSignature)) {
		return false
	}
	if effect.Amount == nil {
		return false
	}
	expectedAmount := make([]byte, 32)
	effect.Amount.FillBytes(expectedAmount)
	return len(log.Data) == 32 && bytes.Equal(log.Data, expectedAmount)
}

func (effect *TransferEffect) matchesERC721(log ReceiptLog) bool {
	if len(log.Topics) != 4 || len(log.Data) != 0 || log.Topics[0] != common.HexToHash(erc721TransferTopic) {
		return false
	}
	if log.Topics[1] != common.BytesToHash(effect.From.Bytes()) || log.Topics[2] != common.BytesToHash(effect.To.Bytes()) || effect.TokenID == nil {
		return false
	}
	expectedTokenID := make([]byte, 32)
	effect.TokenID.FillBytes(expectedTokenID)
	return log.Topics[3] == common.BytesToHash(expectedTokenID)
}

func (effect *TransferEffect) matchesERC1155Single(log ReceiptLog) bool {
	if len(log.Topics) != 4 || log.Topics[0] != common.HexToHash(erc1155SingleTopic) {
		return false
	}
	if log.Topics[2] != common.BytesToHash(effect.From.Bytes()) || log.Topics[3] != common.BytesToHash(effect.To.Bytes()) || effect.TokenID == nil || effect.Amount == nil {
		return false
	}
	if len(log.Data) != 64 {
		return false
	}
	expectedID := make([]byte, 32)
	expectedAmount := make([]byte, 32)
	effect.TokenID.FillBytes(expectedID)
	effect.Amount.FillBytes(expectedAmount)
	return bytes.Equal(log.Data[:32], expectedID) && bytes.Equal(log.Data[32:], expectedAmount)
}

func (effect *TransferEffect) matchesERC1155Batch(log ReceiptLog) bool {
	if len(log.Topics) != 4 || log.Topics[0] != common.HexToHash(erc1155BatchTopic) {
		return false
	}
	if log.Topics[2] != common.BytesToHash(effect.From.Bytes()) || log.Topics[3] != common.BytesToHash(effect.To.Bytes()) || len(effect.Entries) == 0 {
		return false
	}
	ids, amounts, err := decodeERC1155BatchData(log.Data)
	if err != nil || len(ids) != len(effect.Entries) || len(amounts) != len(effect.Entries) {
		return false
	}
	for index, entry := range effect.Entries {
		if entry.TokenID == nil || entry.Amount == nil || ids[index].Cmp(entry.TokenID) != 0 || amounts[index].Cmp(entry.Amount) != 0 {
			return false
		}
	}
	return true
}

func decodeERC1155BatchData(data []byte) ([]*big.Int, []*big.Int, error) {
	if len(data) < 128 || len(data)%32 != 0 {
		return nil, nil, invalidIntent("ERC-1155 batch data")
	}
	idsOffset := new(big.Int).SetBytes(data[:32])
	amountsOffset := new(big.Int).SetBytes(data[32:64])
	if !idsOffset.IsUint64() || idsOffset.Uint64() != 64 {
		return nil, nil, invalidIntent("ERC-1155 batch offsets")
	}
	idsLength := new(big.Int).SetBytes(data[64:96])
	if !idsLength.IsUint64() || idsLength.Uint64() == 0 || idsLength.Uint64() > MaxEIP712ArrayLength {
		return nil, nil, invalidIntent("ERC-1155 batch lengths")
	}
	count := int(idsLength.Uint64())
	expectedAmountsOffset := uint64(64) + 32 + uint64(count)*32
	if !amountsOffset.IsUint64() || amountsOffset.Uint64() != expectedAmountsOffset {
		return nil, nil, invalidIntent("ERC-1155 batch offsets")
	}
	if uint64(len(data)) != expectedAmountsOffset+32+uint64(count)*32 {
		return nil, nil, invalidIntent("ERC-1155 batch data size")
	}
	amountsLength := new(big.Int).SetBytes(data[96+count*32 : 128+count*32])
	if !amountsLength.IsUint64() || amountsLength.Uint64() != uint64(count) {
		return nil, nil, invalidIntent("ERC-1155 batch lengths")
	}
	ids := make([]*big.Int, 0, count)
	amounts := make([]*big.Int, 0, count)
	for index := 0; index < count; index++ {
		ids = append(ids, new(big.Int).SetBytes(data[96+index*32:128+index*32]))
		amounts = append(amounts, new(big.Int).SetBytes(data[128+count*32+index*32:160+count*32+index*32]))
	}
	return ids, amounts, nil
}

func HasExpectedEffect(record TransactionRecord, receipt Receipt) bool {
	effect, err := EffectFromRecord(record)
	if err != nil {
		return false
	}
	for _, logEntry := range receipt.Logs {
		if effect.Matches(logEntry) {
			return true
		}
	}
	return false
}
