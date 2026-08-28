package evm_test

import (
	"context"
	"math/big"
	"testing"
	"time"

	"blocowallet/internal/evm"
	"blocowallet/internal/wallet"

	"github.com/ethereum/go-ethereum/common"
)

func TestERC1155SingleAndBatchEndToEndWithEffectVerification(t *testing.T) {
	from := common.HexToAddress("0x9d8A62f656a8d1615C1294fd71e9CFb3E4855A4F")
	contract := common.HexToAddress("0x3333333333333333333333333333333333333333")
	to := common.HexToAddress("0x2222222222222222222222222222222222222222")
	blockHash := common.HexToHash("0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")

	setupEngine := func(repository *engineRepository) (*evm.Engine, *erc721TestRPC, time.Time, func() string) {
		rpc := &erc721TestRPC{
			engineRPC: &engineRPC{
				simulationRPC: simulationRPC{callResult: []byte{}, estimate: 150_000, gasPrice: big.NewInt(1_000_000_000)},
				header:        evm.BlockHeader{BlockIdentity: evm.BlockIdentity{Number: 100, Hash: blockHash}, GasLimit: 30_000_000},
			},
			owner:   from,
			balance: common.LeftPadBytes(big.NewInt(1_000_000).Bytes(), 32),
		}
		ids := []string{
			"31111111-1111-4111-8111-111111111111",
			"51111111-1111-4111-8111-111111111111",
			"61111111-1111-4111-8111-111111111111",
		}
		index := 0
		now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
		engine, err := evm.NewEngine(repository, rpc, vectorSigner{}, evm.EngineOptions{
			Now: func() time.Time { return now }, NewID: func() (string, error) {
				value := ids[index]
				index++
				return value, nil
			}, ReservationTTL: time.Minute, ApprovalTTL: time.Minute,
		})
		if err != nil {
			t.Fatal(err)
		}
		return engine, rpc, now, func() string {
			if index < len(ids) {
				return ids[index]
			}
			return "61111111-1111-4111-8111-111111111111"
		}
	}

	t.Run("single", func(t *testing.T) {
		repository := &engineRepository{}
		engine, rpc, now, _ := setupEngine(repository)
		prepared, err := engine.PrepareERC1155SafeTransfer(context.Background(), evm.PrepareERC1155SafeTransferRequest{
			OperationID: "41111111-1111-4111-8111-111111111111", PlanGeneration: 1,
			AccountID: "8b9b0587-388e-4fca-bba4-bf544ebe53ca", ChainID: 1, From: from,
			Contract: contract, To: to, TokenID: big.NewInt(7), Amount: big.NewInt(3),
		})
		if err != nil {
			t.Fatal(err)
		}
		if prepared.Plan().TokenID().Cmp(big.NewInt(7)) != 0 || prepared.Plan().Amount().Cmp(big.NewInt(3)) != 0 {
			t.Fatalf("unexpected ERC-1155 single plan: %+v", prepared.Plan())
		}
		result, err := engine.ApproveSignAndBroadcast(context.Background(), wallet.CapabilityHandle{}, prepared, evm.ApprovalRequest{
			AuthorizationEpoch: 1, RiskLevel: evm.RiskNormal, ConfirmationLevel: evm.ConfirmationStandard,
		})
		if err != nil {
			t.Fatal(err)
		}
		if repository.record.TokenID == nil || repository.record.TokenID.Cmp(big.NewInt(7)) != 0 || repository.record.AssetAmount.Cmp(big.NewInt(3)) != 0 {
			t.Fatalf("ERC-1155 single authorization was not bound: %+v", repository.record)
		}
		repository.record.TransactionHash = result.Hash
		repository.record.State = evm.TransactionSubmitted
		trackerRPC := &trackingRPC{
			simulationRPC: simulationRPC{},
			receipt: evm.Receipt{
				TransactionHash: result.Hash,
				Block:           evm.BlockIdentity{Number: 100, Hash: blockHash},
				Status:          1, GasUsed: 100_000, EffectiveGasPrice: big.NewInt(1_000_000_000),
				Logs: []evm.ReceiptLog{{
					Address: contract,
					Topics: []common.Hash{
						common.HexToHash("0xc3d58168c5ae7397731d063d5bbf3d657854427343f4c083240f7aacaa2d0f62"),
						common.BytesToHash(from.Bytes()), common.BytesToHash(from.Bytes()), common.BytesToHash(to.Bytes()),
					},
					Data: append(common.LeftPadBytes(big.NewInt(7).Bytes(), 32), common.LeftPadBytes(big.NewInt(3).Bytes(), 32)...),
				}},
			},
			receiptFound: true,
			canonical:    evm.BlockHeader{BlockIdentity: evm.BlockIdentity{Number: 100, Hash: blockHash}},
			head:         evm.BlockHeader{BlockIdentity: evm.BlockIdentity{Number: 101, Hash: common.HexToHash("0xdddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd")}},
		}
		tracker := evm.NewReceiptTracker(repository, trackerRPC)
		tracking, err := tracker.TrackOnce(context.Background(), result.TransactionID, 1, now.Add(2*time.Minute))
		if err != nil {
			t.Fatal(err)
		}
		if tracking.State != evm.TransactionConfirmed {
			t.Fatalf("ERC-1155 single effect was not confirmed: %+v", tracking)
		}
		_ = rpc
	})

	t.Run("batch", func(t *testing.T) {
		repository := &engineRepository{}
		engine, _, now, _ := setupEngine(repository)
		effects := []evm.EffectEntry{{TokenID: big.NewInt(7), Amount: big.NewInt(3)}, {TokenID: big.NewInt(9), Amount: big.NewInt(5)}}
		prepared, err := engine.PrepareERC1155BatchTransfer(context.Background(), evm.PrepareERC1155BatchTransferRequest{
			OperationID: "41111111-1111-4111-8111-111111111111", PlanGeneration: 1,
			AccountID: "8b9b0587-388e-4fca-bba4-bf544ebe53ca", ChainID: 1, From: from,
			Contract: contract, To: to, Effects: effects,
		})
		if err != nil {
			t.Fatal(err)
		}
		planEffects := prepared.Plan().Effects()
		if len(planEffects) != 2 || planEffects[1].TokenID.Cmp(big.NewInt(9)) != 0 {
			t.Fatalf("unexpected ERC-1155 batch plan effects: %+v", planEffects)
		}
		result, err := engine.ApproveSignAndBroadcast(context.Background(), wallet.CapabilityHandle{}, prepared, evm.ApprovalRequest{
			AuthorizationEpoch: 1, RiskLevel: evm.RiskNormal, ConfirmationLevel: evm.ConfirmationStandard,
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(repository.record.Effects) != 2 || repository.record.Effects[0].Amount.Cmp(big.NewInt(3)) != 0 {
			t.Fatalf("ERC-1155 batch effects were not bound: %+v", repository.record)
		}
		repository.record.TransactionHash = result.Hash
		repository.record.State = evm.TransactionSubmitted
		batchData := make([]byte, 0, 256)
		batchData = append(batchData, common.LeftPadBytes(big.NewInt(64).Bytes(), 32)...)
		batchData = append(batchData, common.LeftPadBytes(big.NewInt(160).Bytes(), 32)...)
		batchData = append(batchData, common.LeftPadBytes(big.NewInt(2).Bytes(), 32)...)
		batchData = append(batchData, common.LeftPadBytes(big.NewInt(7).Bytes(), 32)...)
		batchData = append(batchData, common.LeftPadBytes(big.NewInt(9).Bytes(), 32)...)
		batchData = append(batchData, common.LeftPadBytes(big.NewInt(2).Bytes(), 32)...)
		batchData = append(batchData, common.LeftPadBytes(big.NewInt(3).Bytes(), 32)...)
		batchData = append(batchData, common.LeftPadBytes(big.NewInt(5).Bytes(), 32)...)
		trackerRPC := &trackingRPC{
			simulationRPC: simulationRPC{},
			receipt: evm.Receipt{
				TransactionHash: result.Hash,
				Block:           evm.BlockIdentity{Number: 100, Hash: blockHash},
				Status:          1, GasUsed: 100_000, EffectiveGasPrice: big.NewInt(1_000_000_000),
				Logs: []evm.ReceiptLog{{
					Address: contract,
					Topics: []common.Hash{
						common.HexToHash("0x4a39dc06d4c0dbc64b70af90fd698a233a518aa5d07e595d983b8c0526c8f7fb"),
						common.BytesToHash(from.Bytes()), common.BytesToHash(from.Bytes()), common.BytesToHash(to.Bytes()),
					},
					Data: batchData,
				}},
			},
			receiptFound: true,
			canonical:    evm.BlockHeader{BlockIdentity: evm.BlockIdentity{Number: 100, Hash: blockHash}},
			head:         evm.BlockHeader{BlockIdentity: evm.BlockIdentity{Number: 101, Hash: common.HexToHash("0xdddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd")}},
		}
		tracker := evm.NewReceiptTracker(repository, trackerRPC)
		tracking, err := tracker.TrackOnce(context.Background(), result.TransactionID, 1, now.Add(2*time.Minute))
		if err != nil {
			t.Fatal(err)
		}
		if tracking.State != evm.TransactionConfirmed {
			t.Fatalf("ERC-1155 batch effect was not confirmed: %+v", tracking)
		}
		missing := trackerRPC.receipt
		missing.Logs = nil
		trackerRPC.receipt = missing
		unverified, err := tracker.TrackOnce(context.Background(), result.TransactionID, 1, now.Add(3*time.Minute))
		if err != nil {
			t.Fatal(err)
		}
		if unverified.State != evm.TransactionEffectUnverified {
			t.Fatalf("missing ERC-1155 batch log was not flagged: %+v", unverified)
		}
	})
}
