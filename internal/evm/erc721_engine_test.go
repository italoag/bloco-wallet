package evm_test

import (
	"bytes"
	"context"
	"math/big"
	"testing"
	"time"

	"blocowallet/internal/evm"
	"blocowallet/internal/wallet"

	"github.com/ethereum/go-ethereum/common"
)

type erc721TestRPC struct {
	*engineRPC
	owner   common.Address
	balance []byte
}

func (rpc *erc721TestRPC) CallContract(ctx context.Context, call evm.TransactionCall, block evm.BlockIdentity) ([]byte, error) {
	if len(call.Input) >= 4 && bytes.Equal(call.Input[:4], common.FromHex("0x6352211e")) {
		return common.LeftPadBytes(rpc.owner.Bytes(), 32), nil
	}
	if len(call.Input) >= 4 && bytes.Equal(call.Input[:4], common.FromHex("0x00fdd58e")) {
		return append([]byte(nil), rpc.balance...), nil
	}
	return rpc.engineRPC.CallContract(ctx, call, block)
}

func TestERC721SafeTransferEndToEndWithEffectVerification(t *testing.T) {
	from := common.HexToAddress("0x9d8A62f656a8d1615C1294fd71e9CFb3E4855A4F")
	contract := common.HexToAddress("0x3333333333333333333333333333333333333333")
	to := common.HexToAddress("0x2222222222222222222222222222222222222222")
	tokenID := big.NewInt(42)
	blockHash := common.HexToHash("0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	repository := &engineRepository{}
	rpc := &erc721TestRPC{
		engineRPC: &engineRPC{
			simulationRPC: simulationRPC{callResult: []byte{}, estimate: 120_000, gasPrice: big.NewInt(1_000_000_000)},
			header:        evm.BlockHeader{BlockIdentity: evm.BlockIdentity{Number: 100, Hash: blockHash}, GasLimit: 30_000_000},
		},
		owner: from,
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
	prepared, err := engine.PrepareERC721SafeTransfer(context.Background(), evm.PrepareERC721SafeTransferRequest{
		OperationID: "41111111-1111-4111-8111-111111111111", PlanGeneration: 1,
		AccountID: "8b9b0587-388e-4fca-bba4-bf544ebe53ca", ChainID: 1, From: from,
		Contract: contract, To: to, TokenID: tokenID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Plan().Operation() != evm.OperationERC721SafeTransfer || prepared.Plan().TokenID().Cmp(tokenID) != 0 {
		t.Fatalf("unexpected ERC-721 prepared plan: %+v", prepared.Plan())
	}
	result, err := engine.ApproveSignAndBroadcast(context.Background(), wallet.CapabilityHandle{}, prepared, evm.ApprovalRequest{
		AuthorizationEpoch: 1, RiskLevel: evm.RiskNormal, ConfirmationLevel: evm.ConfirmationStandard,
	})
	if err != nil {
		t.Fatal(err)
	}
	if repository.record.Operation != evm.OperationERC721SafeTransfer || repository.record.Counterparty != to || repository.record.AssetContract != contract || repository.record.AssetAmount.Cmp(tokenID) != 0 {
		t.Fatalf("ERC-721 authorization was not persisted: %+v", repository.record)
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
					common.HexToHash("0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef"),
					common.BytesToHash(from.Bytes()),
					common.BytesToHash(to.Bytes()),
					common.BytesToHash(common.LeftPadBytes(tokenID.Bytes(), 32)),
				},
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
		t.Fatalf("ERC-721 effect was not confirmed: %+v", tracking)
	}
	missingLog := trackerRPC.receipt
	missingLog.Logs = nil
	trackerRPC.receipt = missingLog
	missing, err := tracker.TrackOnce(context.Background(), result.TransactionID, 1, now.Add(3*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if missing.State != evm.TransactionEffectUnverified {
		t.Fatalf("missing ERC-721 log was not flagged: %+v", missing)
	}
}

type codeAtRPC struct {
	simulationRPC
	header evm.BlockHeader
	code   []byte
}

func (rpc *codeAtRPC) LatestHeader(context.Context) (evm.BlockHeader, error) {
	return rpc.header, nil
}

func (rpc *codeAtRPC) CodeAt(context.Context, common.Address, evm.BlockIdentity) ([]byte, error) {
	return rpc.code, nil
}

func TestERC721PrepareRejectsNonOwnerSender(t *testing.T) {
	blockHash := common.HexToHash("0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	repository := &engineRepository{}
	rpc := &erc721TestRPC{
		engineRPC: &engineRPC{
			simulationRPC: simulationRPC{callResult: []byte{}, estimate: 120_000, gasPrice: big.NewInt(1_000_000_000)},
			header:        evm.BlockHeader{BlockIdentity: evm.BlockIdentity{Number: 100, Hash: blockHash}, GasLimit: 30_000_000},
		},
		owner: common.HexToAddress("0x2222222222222222222222222222222222222222"),
	}
	engine, err := evm.NewEngine(repository, rpc, vectorSigner{}, evm.EngineOptions{
		Now: time.Now, NewID: func() (string, error) { return "31111111-1111-4111-8111-111111111111", nil },
		ReservationTTL: time.Minute, ApprovalTTL: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.PrepareERC721SafeTransfer(context.Background(), evm.PrepareERC721SafeTransferRequest{
		OperationID: "41111111-1111-4111-8111-111111111111", PlanGeneration: 1,
		AccountID: "8b9b0587-388e-4fca-bba4-bf544ebe53ca", ChainID: 1,
		From: common.HexToAddress("0x9d8A62f656a8d1615C1294fd71e9CFb3E4855A4F"), Contract: common.HexToAddress("0x3333333333333333333333333333333333333333"),
		To: common.HexToAddress("0x2222222222222222222222222222222222222222"), TokenID: big.NewInt(42),
	}); !evm.IsErrorCode(err, evm.ErrorPolicyDenied) {
		t.Fatalf("ERC-721 prepare from non-owner returned %v", err)
	}
}

func TestERC721PrepareRejectsContractWithoutCode(t *testing.T) {
	blockHash := common.HexToHash("0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	repository := &engineRepository{}
	rpc := &codeAtRPC{
		simulationRPC: simulationRPC{callResult: []byte{}, estimate: 21_000},
		header:        evm.BlockHeader{BlockIdentity: evm.BlockIdentity{Number: 100, Hash: blockHash}, GasLimit: 30_000_000},
		code:          []byte{},
	}
	engine, err := evm.NewEngine(repository, rpc, vectorSigner{}, evm.EngineOptions{
		Now: time.Now, NewID: func() (string, error) { return "31111111-1111-4111-8111-111111111111", nil },
		ReservationTTL: time.Minute, ApprovalTTL: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.PrepareERC721SafeTransfer(context.Background(), evm.PrepareERC721SafeTransferRequest{
		OperationID: "41111111-1111-4111-8111-111111111111", PlanGeneration: 1,
		AccountID: "8b9b0587-388e-4fca-bba4-bf544ebe53ca", ChainID: 1,
		From: common.HexToAddress("0x1111111111111111111111111111111111111111"), Contract: common.HexToAddress("0x3333333333333333333333333333333333333333"),
		To: common.HexToAddress("0x2222222222222222222222222222222222222222"), TokenID: big.NewInt(42),
	}); !evm.IsErrorCode(err, evm.ErrorPolicyDenied) {
		t.Fatalf("ERC-721 prepare without contract code returned %v", err)
	}
}
