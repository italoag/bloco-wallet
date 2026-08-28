package storage

import (
	"context"
	"fmt"
	"math"
	"math/big"
	"sort"

	"blocowallet/internal/evm"

	"github.com/ethereum/go-ethereum/common"
	"gorm.io/gorm"
)

const historySelectColumns = "transaction_id, account_id, sender_address, chain_id, nonce, operation, counterparty_address, asset_contract, asset_amount, token_id, state, last_result_code, transaction_hash, broadcast_attempts, receipt_status, receipt_block_number, receipt_gas_used, effective_gas_price, confirmations, confirmation_target, reorg_count, created_at_ms, updated_at_ms"

var _ evm.HistoryReader = (*GORMRepository)(nil)

func (repository *GORMRepository) ListTransactions(ctx context.Context, query evm.HistoryQuery) (evm.HistoryPage, error) {
	if err := evm.ValidateHistoryQuery(query); err != nil {
		return evm.HistoryPage{}, err
	}
	database := applyHistoryScope(repository.db.WithContext(ctx).Model(&evmTransactionRow{}), query.AccountID, query.Sender, query.ChainID)
	if len(query.States) > 0 {
		states := make([]string, len(query.States))
		for index, state := range query.States {
			states[index] = string(state)
		}
		database = database.Where("state IN ?", states)
	}
	if len(query.Operations) > 0 {
		operations := make([]string, len(query.Operations))
		for index, operation := range query.Operations {
			operations[index] = string(operation)
		}
		database = database.Where("operation IN ?", operations)
	}
	if query.Cursor != nil {
		createdAt := query.Cursor.CreatedAt.UTC().UnixMilli()
		database = database.Where("(created_at_ms < ?) OR (created_at_ms = ? AND transaction_id < ?)", createdAt, createdAt, query.Cursor.TransactionID)
	}
	var rows []evmTransactionRow
	if err := database.Select(historySelectColumns).Order("created_at_ms DESC, transaction_id DESC").Limit(query.Limit + 1).Find(&rows).Error; err != nil {
		return evm.HistoryPage{}, fmt.Errorf("list local EVM history: %w", err)
	}
	hasMore := len(rows) > query.Limit
	if hasMore {
		rows = rows[:query.Limit]
	}
	page := evm.HistoryPage{Entries: make([]evm.HistoryEntry, 0, len(rows))}
	for _, row := range rows {
		page.Entries = append(page.Entries, historyEntryFromRow(row))
	}
	if hasMore && len(rows) > 0 {
		last := rows[len(rows)-1]
		page.NextCursor = &evm.HistoryCursor{CreatedAt: unixMilliTime(last.CreatedAtMS), TransactionID: last.TransactionID}
	}
	return page, nil
}

func (repository *GORMRepository) Analytics(ctx context.Context, query evm.AnalyticsQuery) (_ evm.AnalyticsSnapshot, returnErr error) {
	if err := evm.ValidateAnalyticsQuery(query); err != nil {
		return evm.AnalyticsSnapshot{}, err
	}
	database := applyHistoryScope(repository.db.WithContext(ctx).Model(&evmTransactionRow{}), query.AccountID, query.Sender, query.ChainID)
	rows, err := database.Select("chain_id", "operation", "asset_contract", "asset_amount", "state", "receipt_gas_used", "effective_gas_price", "reorg_count").Rows()
	if err != nil {
		return evm.AnalyticsSnapshot{}, fmt.Errorf("query local EVM analytics: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); returnErr == nil && closeErr != nil {
			returnErr = fmt.Errorf("close local EVM analytics rows: %w", closeErr)
		}
	}()
	type assetKey struct {
		chainID   uint64
		operation evm.Operation
		contract  common.Address
	}
	stateCounts := make(map[string]uint64)
	operationCounts := make(map[string]uint64)
	assets := make(map[assetKey]*evm.AssetAnalytics)
	fees := make(map[uint64]*evm.ChainFeeAnalytics)
	snapshot := evm.AnalyticsSnapshot{}
	for rows.Next() {
		var row evmTransactionRow
		if err := repository.db.ScanRows(rows, &row); err != nil {
			return evm.AnalyticsSnapshot{}, fmt.Errorf("scan local EVM analytics: %w", err)
		}
		if row.ChainID <= 0 || row.ReorgCount < 0 || len(row.AssetContract) != common.AddressLength || len(row.AssetAmount) != 32 {
			return evm.AnalyticsSnapshot{}, fmt.Errorf("local EVM analytics row is invalid")
		}
		if snapshot.TransactionCount == math.MaxUint64 || snapshot.ReorgCount > math.MaxUint64-uint64(row.ReorgCount) {
			return evm.AnalyticsSnapshot{}, fmt.Errorf("local EVM analytics counters overflow")
		}
		snapshot.TransactionCount++
		snapshot.ReorgCount += uint64(row.ReorgCount)
		if err := incrementAnalyticsCount(stateCounts, row.State); err != nil {
			return evm.AnalyticsSnapshot{}, err
		}
		if err := incrementAnalyticsCount(operationCounts, row.Operation); err != nil {
			return evm.AnalyticsSnapshot{}, err
		}
		key := assetKey{chainID: uint64(row.ChainID), operation: evm.Operation(row.Operation), contract: common.BytesToAddress(row.AssetContract)}
		aggregate := assets[key]
		if aggregate == nil {
			aggregate = &evm.AssetAnalytics{ChainID: key.chainID, Operation: key.operation, AssetContract: key.contract, Amount: new(big.Int)}
			assets[key] = aggregate
		}
		if aggregate.TransactionCount == math.MaxUint64 {
			return evm.AnalyticsSnapshot{}, fmt.Errorf("local EVM asset counter overflow")
		}
		aggregate.TransactionCount++
		if evm.Operation(row.Operation) != evm.OperationERC721SafeTransfer {
			aggregate.Amount.Add(aggregate.Amount, new(big.Int).SetBytes(row.AssetAmount))
		}
		if row.ReceiptGasUsed != nil && *row.ReceiptGasUsed > 0 && len(row.EffectiveGasPrice) == 32 {
			chainID := uint64(row.ChainID)
			aggregateFee := fees[chainID]
			if aggregateFee == nil {
				aggregateFee = &evm.ChainFeeAnalytics{ChainID: chainID, ActualFee: new(big.Int)}
				fees[chainID] = aggregateFee
			}
			if aggregateFee.TransactionCount == math.MaxUint64 {
				return evm.AnalyticsSnapshot{}, fmt.Errorf("local EVM fee counter overflow")
			}
			aggregateFee.TransactionCount++
			fee := new(big.Int).Mul(new(big.Int).SetUint64(uint64(*row.ReceiptGasUsed)), new(big.Int).SetBytes(row.EffectiveGasPrice))
			aggregateFee.ActualFee.Add(aggregateFee.ActualFee, fee)
		}
	}
	if err := rows.Err(); err != nil {
		return evm.AnalyticsSnapshot{}, fmt.Errorf("iterate local EVM analytics: %w", err)
	}
	snapshot.States = sortedAnalyticsCounts(stateCounts)
	snapshot.Operations = sortedAnalyticsCounts(operationCounts)
	for _, aggregate := range assets {
		snapshot.Assets = append(snapshot.Assets, *aggregate)
	}
	sort.Slice(snapshot.Assets, func(first, second int) bool {
		if snapshot.Assets[first].ChainID != snapshot.Assets[second].ChainID {
			return snapshot.Assets[first].ChainID < snapshot.Assets[second].ChainID
		}
		if snapshot.Assets[first].Operation != snapshot.Assets[second].Operation {
			return snapshot.Assets[first].Operation < snapshot.Assets[second].Operation
		}
		return snapshot.Assets[first].AssetContract.Hex() < snapshot.Assets[second].AssetContract.Hex()
	})
	for _, aggregate := range fees {
		snapshot.Fees = append(snapshot.Fees, *aggregate)
	}
	sort.Slice(snapshot.Fees, func(first, second int) bool { return snapshot.Fees[first].ChainID < snapshot.Fees[second].ChainID })
	return snapshot, nil
}

func applyHistoryScope(database *gorm.DB, accountID string, sender common.Address, chainID uint64) *gorm.DB {
	if accountID != "" {
		database = database.Where("account_id = ?", accountID)
	}
	if sender != (common.Address{}) {
		database = database.Where("sender_address = ?", sender.Bytes())
	}
	if chainID != 0 {
		database = database.Where("chain_id = ?", int64(chainID))
	}
	return database
}

func historyEntryFromRow(row evmTransactionRow) evm.HistoryEntry {
	entry := evm.HistoryEntry{
		TransactionID: row.TransactionID, AccountID: row.AccountID, Sender: common.BytesToAddress(row.SenderAddress),
		ChainID: uint64(row.ChainID), Nonce: uint64(row.Nonce), Operation: evm.Operation(row.Operation),
		Counterparty: common.BytesToAddress(row.CounterpartyAddress), AssetContract: common.BytesToAddress(row.AssetContract), AssetAmount: new(big.Int).SetBytes(row.AssetAmount),
		TokenID: optionalBytesToBigInt(row.TokenID),
		State:   evm.TransactionState(row.State), LastResultCode: row.LastResultCode, TransactionHash: common.BytesToHash(row.TransactionHash),
		BroadcastAttempts: uint64(row.BroadcastAttempts), Confirmations: uint64(row.Confirmations), ConfirmationTarget: uint64(row.ConfirmationTarget), ReorgCount: uint64(row.ReorgCount),
		CreatedAt: unixMilliTime(row.CreatedAtMS), UpdatedAt: unixMilliTime(row.UpdatedAtMS),
	}
	if row.ReceiptStatus != nil && row.ReceiptBlockNumber != nil && row.ReceiptGasUsed != nil && *row.ReceiptStatus >= 0 && *row.ReceiptBlockNumber >= 0 && *row.ReceiptGasUsed > 0 && len(row.EffectiveGasPrice) == 32 {
		price := new(big.Int).SetBytes(row.EffectiveGasPrice)
		entry.Receipt = &evm.HistoryReceipt{
			Status: uint64(*row.ReceiptStatus), BlockNumber: uint64(*row.ReceiptBlockNumber), GasUsed: uint64(*row.ReceiptGasUsed), EffectiveGasPrice: price,
			ActualFee: new(big.Int).Mul(new(big.Int).SetUint64(uint64(*row.ReceiptGasUsed)), new(big.Int).Set(price)),
		}
	}
	return entry
}

func incrementAnalyticsCount(counts map[string]uint64, key string) error {
	if counts[key] == math.MaxUint64 {
		return fmt.Errorf("local EVM analytics counter overflow")
	}
	counts[key]++
	return nil
}

func sortedAnalyticsCounts(counts map[string]uint64) []evm.AnalyticsCount {
	result := make([]evm.AnalyticsCount, 0, len(counts))
	for key, count := range counts {
		result = append(result, evm.AnalyticsCount{Key: key, Count: count})
	}
	sort.Slice(result, func(first, second int) bool { return result[first].Key < result[second].Key })
	return result
}
