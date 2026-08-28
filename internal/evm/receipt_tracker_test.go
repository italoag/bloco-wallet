package evm_test

import (
	"context"
	"math/big"
	"testing"
	"time"

	"blocowallet/internal/evm"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

type trackingRPC struct {
	simulationRPC
	receipt      evm.Receipt
	receiptFound bool
	canonical    evm.BlockHeader
	head         evm.BlockHeader
}

func (rpc *trackingRPC) TransactionReceipt(context.Context, common.Hash) (evm.Receipt, bool, error) {
	return rpc.receipt, rpc.receiptFound, nil
}
func (rpc *trackingRPC) HeaderByNumber(context.Context, uint64) (evm.BlockHeader, bool, error) {
	return rpc.canonical, true, nil
}
func (rpc *trackingRPC) LatestHeader(context.Context) (evm.BlockHeader, error) {
	return rpc.head, nil
}

type trackingRepository struct {
	*engineRepository
	observation evm.ReceiptObservation
	reorg       evm.ReorgObservation
}

func (repository *trackingRepository) RecordReceipt(_ context.Context, observation evm.ReceiptObservation) error {
	repository.observation = observation
	return nil
}
func (repository *trackingRepository) MarkReorged(_ context.Context, observation evm.ReorgObservation) error {
	repository.reorg = observation
	return nil
}

func TestReceiptTrackerRequiresCanonicalERC20EffectLog(t *testing.T) {
	txHash := common.HexToHash("0xcccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc")
	blockHash := common.HexToHash("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	from := common.HexToAddress("0x1111111111111111111111111111111111111111")
	to := common.HexToAddress("0x2222222222222222222222222222222222222222")
	contract := common.HexToAddress("0x3333333333333333333333333333333333333333")
	amount := big.NewInt(42)
	amountData := make([]byte, 32)
	amount.FillBytes(amountData)
	receipt := evm.Receipt{
		TransactionHash: txHash, Block: evm.BlockIdentity{Number: 10, Hash: blockHash}, Status: 1, GasUsed: 21_000, EffectiveGasPrice: big.NewInt(1),
		Logs: []evm.ReceiptLog{{
			Address: contract,
			Topics:  []common.Hash{crypto.Keccak256Hash([]byte("Transfer(address,address,uint256)")), common.BytesToHash(from.Bytes()), common.BytesToHash(to.Bytes())},
			Data:    amountData,
		}},
	}
	rpc := &trackingRPC{receiptFound: true, receipt: receipt,
		canonical: evm.BlockHeader{BlockIdentity: evm.BlockIdentity{Number: 10, Hash: blockHash}},
		head:      evm.BlockHeader{BlockIdentity: evm.BlockIdentity{Number: 10, Hash: blockHash}},
	}
	repository := &trackingRepository{engineRepository: &engineRepository{record: evm.TransactionRecord{
		TransactionID: "61111111-1111-4111-8111-111111111111", TransactionHash: txHash, ChainID: 1, ConfirmationTarget: 1,
		State: evm.TransactionSubmitted, Operation: evm.OperationERC20Transfer, Sender: from, Counterparty: to, AssetContract: contract, AssetAmount: amount,
	}}}
	tracker := evm.NewReceiptTracker(repository, rpc)
	result, err := tracker.TrackOnce(context.Background(), repository.record.TransactionID, 1, time.Now())
	if err != nil || result.State != evm.TransactionConfirmed {
		t.Fatalf("canonical token effect was not confirmed: %+v %v", result, err)
	}
	rpc.receipt.Logs = nil
	result, err = tracker.TrackOnce(context.Background(), repository.record.TransactionID, 1, time.Now())
	if err != nil || result.State != evm.TransactionEffectUnverified {
		t.Fatalf("missing token effect log was accepted: %+v %v", result, err)
	}
}

func TestReceiptTrackerWaitsForRevertConfirmationTarget(t *testing.T) {
	txHash := common.HexToHash("0xcccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc")
	blockHash := common.HexToHash("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	rpc := &trackingRPC{
		receiptFound: true,
		receipt:      evm.Receipt{TransactionHash: txHash, Block: evm.BlockIdentity{Number: 10, Hash: blockHash}, Status: 0, GasUsed: 21_000, EffectiveGasPrice: big.NewInt(1)},
		canonical:    evm.BlockHeader{BlockIdentity: evm.BlockIdentity{Number: 10, Hash: blockHash}},
		head:         evm.BlockHeader{BlockIdentity: evm.BlockIdentity{Number: 11, Hash: common.HexToHash("0xdddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd")}},
	}
	repository := &trackingRepository{engineRepository: &engineRepository{record: evm.TransactionRecord{
		TransactionID: "61111111-1111-4111-8111-111111111111", TransactionHash: txHash, ChainID: 1, ConfirmationTarget: 3, State: evm.TransactionSubmitted,
	}}}
	tracker := evm.NewReceiptTracker(repository, rpc)
	result, err := tracker.TrackOnce(context.Background(), repository.record.TransactionID, 3, time.Now())
	if err != nil || result.State != evm.TransactionConfirming {
		t.Fatalf("single-confirmation revert became terminal: %+v %v", result, err)
	}
	rpc.head.Number = 12
	result, err = tracker.TrackOnce(context.Background(), repository.record.TransactionID, 3, time.Now())
	if err != nil || result.State != evm.TransactionReverted {
		t.Fatalf("confirmed revert did not become terminal: %+v %v", result, err)
	}
}

func TestReceiptTrackerCountsCanonicalConfirmationsAndDetectsReorg(t *testing.T) {
	txHash := common.HexToHash("0xcccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc")
	blockHash := common.HexToHash("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	rpc := &trackingRPC{
		receiptFound: true,
		receipt:      evm.Receipt{TransactionHash: txHash, Block: evm.BlockIdentity{Number: 10, Hash: blockHash}, Status: 1, GasUsed: 21_000, EffectiveGasPrice: big.NewInt(1)},
		canonical:    evm.BlockHeader{BlockIdentity: evm.BlockIdentity{Number: 10, Hash: blockHash}},
		head:         evm.BlockHeader{BlockIdentity: evm.BlockIdentity{Number: 11, Hash: common.HexToHash("0xdddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd")}},
	}
	repository := &trackingRepository{engineRepository: &engineRepository{record: evm.TransactionRecord{
		TransactionID: "61111111-1111-4111-8111-111111111111", TransactionHash: txHash, ChainID: 1, ConfirmationTarget: 3, State: evm.TransactionSubmitted,
	}}}
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	tracker := evm.NewReceiptTracker(repository, rpc)
	result, err := tracker.TrackOnce(context.Background(), repository.record.TransactionID, 3, now)
	if err != nil {
		t.Fatal(err)
	}
	if result.State != evm.TransactionConfirming || result.Confirmations != 2 || repository.observation.Confirmations != 2 {
		t.Fatalf("unexpected confirmation result: %+v observation=%+v", result, repository.observation)
	}

	rpc.canonical.Hash = common.HexToHash("0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	result, err = tracker.TrackOnce(context.Background(), repository.record.TransactionID, 3, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if result.State != evm.TransactionReorged || repository.reorg.TransactionID != repository.record.TransactionID {
		t.Fatalf("reorg was not detected: %+v observation=%+v", result, repository.reorg)
	}
}
