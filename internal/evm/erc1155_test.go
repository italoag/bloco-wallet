package evm

import (
	"bytes"
	"encoding/hex"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

func TestEncodeERC1155MethodsMatchGoldenVectors(t *testing.T) {
	from := common.HexToAddress("0x1111111111111111111111111111111111111111")
	to := common.HexToAddress("0x2222222222222222222222222222222222222222")
	tokenID := big.NewInt(7)
	amount := big.NewInt(3)

	single := encodeERC1155SafeTransferMethod(from, to, tokenID, amount)
	if !bytes.Equal(single[:4], crypto.Keccak256([]byte("safeTransferFrom(address,address,uint256,uint256,bytes)"))[:4]) || len(single) != 196 {
		t.Fatalf("unexpected ERC-1155 single calldata: %x", single)
	}
	if !bytes.Equal(single[4:36], common.LeftPadBytes(from.Bytes(), 32)) || !bytes.Equal(single[36:68], common.LeftPadBytes(to.Bytes(), 32)) || !bytes.Equal(single[68:100], common.LeftPadBytes(tokenID.Bytes(), 32)) || !bytes.Equal(single[100:132], common.LeftPadBytes(amount.Bytes(), 32)) || !bytes.Equal(single[132:164], common.LeftPadBytes(big.NewInt(0x80).Bytes(), 32)) || !bytes.Equal(single[164:196], make([]byte, 32)) {
		t.Fatalf("unexpected ERC-1155 single arguments: %x", single)
	}

	effects := []EffectEntry{{TokenID: big.NewInt(7), Amount: big.NewInt(3)}, {TokenID: big.NewInt(9), Amount: big.NewInt(5)}}
	batch := encodeERC1155BatchTransferMethod(from, to, effects)
	if !bytes.Equal(batch[:4], crypto.Keccak256([]byte("safeBatchTransferFrom(address,address,uint256[],uint256[],bytes)"))[:4]) || len(batch) != 260+64*len(effects) {
		t.Fatalf("unexpected ERC-1155 batch calldata length: %d", len(batch))
	}
	if !bytes.Equal(batch[4:36], common.LeftPadBytes(from.Bytes(), 32)) || !bytes.Equal(batch[36:68], common.LeftPadBytes(to.Bytes(), 32)) {
		t.Fatalf("unexpected ERC-1155 batch addresses: %x", batch[:68])
	}
	if !bytes.Equal(batch[68:100], common.LeftPadBytes(big.NewInt(0x80).Bytes(), 32)) || !bytes.Equal(batch[100:132], common.LeftPadBytes(big.NewInt(224).Bytes(), 32)) || !bytes.Equal(batch[132:164], common.LeftPadBytes(big.NewInt(320).Bytes(), 32)) {
		t.Fatalf("unexpected ERC-1155 batch offsets: %x", batch[68:164])
	}
	if !bytes.Equal(batch[164:196], common.LeftPadBytes(big.NewInt(2).Bytes(), 32)) || !bytes.Equal(batch[196:228], common.LeftPadBytes(big.NewInt(7).Bytes(), 32)) || !bytes.Equal(batch[228:260], common.LeftPadBytes(big.NewInt(9).Bytes(), 32)) {
		t.Fatalf("unexpected ERC-1155 batch ids: %x", batch[164:260])
	}
	if !bytes.Equal(batch[260:292], common.LeftPadBytes(big.NewInt(2).Bytes(), 32)) || !bytes.Equal(batch[292:324], common.LeftPadBytes(big.NewInt(3).Bytes(), 32)) || !bytes.Equal(batch[324:356], common.LeftPadBytes(big.NewInt(5).Bytes(), 32)) {
		t.Fatalf("unexpected ERC-1155 batch amounts: %x", batch[260:356])
	}
	if !bytes.Equal(batch[356:388], make([]byte, 32)) {
		t.Fatalf("unexpected ERC-1155 batch data length: %x", batch[356:388])
	}
}

func TestERC1155EffectsMatchCanonicalEvents(t *testing.T) {
	contract := common.HexToAddress("0x3333333333333333333333333333333333333333")
	from := common.HexToAddress("0x1111111111111111111111111111111111111111")
	to := common.HexToAddress("0x2222222222222222222222222222222222222222")
	tokenID := big.NewInt(7)
	amount := big.NewInt(3)

	single := &TransferEffect{
		Kind: EffectERC1155Transfer, AssetContract: contract, From: from, To: to,
		TokenID: tokenID, Amount: amount,
	}
	singleLog := ReceiptLog{
		Address: contract,
		Topics: []common.Hash{
			common.HexToHash("0xc3d58168c5ae7397731d063d5bbf3d657854427343f4c083240f7aacaa2d0f62"),
			common.BytesToHash(from.Bytes()), common.BytesToHash(from.Bytes()), common.BytesToHash(to.Bytes()),
		},
		Data: append(common.LeftPadBytes(tokenID.Bytes(), 32), common.LeftPadBytes(amount.Bytes(), 32)...),
	}
	if !single.Matches(singleLog) {
		t.Fatal("canonical TransferSingle event was rejected")
	}
	record := TransactionRecord{
		Operation: OperationERC1155SafeTransfer, Sender: from, Counterparty: to,
		AssetContract: contract, AssetAmount: new(big.Int).Set(amount), TokenID: new(big.Int).Set(tokenID),
	}
	if !HasExpectedEffect(record, Receipt{Logs: []ReceiptLog{singleLog}}) {
		t.Fatal("ERC-1155 single record did not confirm its effect")
	}
	wrongValue := singleLog
	wrongValue.Data = append(common.LeftPadBytes(tokenID.Bytes(), 32), common.LeftPadBytes(big.NewInt(4).Bytes(), 32)...)
	if single.Matches(wrongValue) {
		t.Fatal("TransferSingle with wrong amount was accepted")
	}

	entries := []EffectEntry{{TokenID: big.NewInt(7), Amount: big.NewInt(3)}, {TokenID: big.NewInt(9), Amount: big.NewInt(5)}}
	batch := &TransferEffect{
		Kind: EffectERC1155Batch, AssetContract: contract, From: from, To: to, Entries: CloneEffectEntries(entries),
	}
	batchLog := ReceiptLog{
		Address: contract,
		Topics: []common.Hash{
			common.HexToHash("0x4a39dc06d4c0dbc64b70af90fd698a233a518aa5d07e595d983b8c0526c8f7fb"),
			common.BytesToHash(from.Bytes()), common.BytesToHash(from.Bytes()), common.BytesToHash(to.Bytes()),
		},
	}
	// data = abi.encode(uint256[] ids, uint256[] values)
	batchData := make([]byte, 0, 256)
	batchData = append(batchData, common.LeftPadBytes(big.NewInt(64).Bytes(), 32)...)
	batchData = append(batchData, common.LeftPadBytes(big.NewInt(160).Bytes(), 32)...)
	batchData = append(batchData, common.LeftPadBytes(big.NewInt(2).Bytes(), 32)...)
	batchData = append(batchData, common.LeftPadBytes(big.NewInt(7).Bytes(), 32)...)
	batchData = append(batchData, common.LeftPadBytes(big.NewInt(9).Bytes(), 32)...)
	batchData = append(batchData, common.LeftPadBytes(big.NewInt(2).Bytes(), 32)...)
	batchData = append(batchData, common.LeftPadBytes(big.NewInt(3).Bytes(), 32)...)
	batchData = append(batchData, common.LeftPadBytes(big.NewInt(5).Bytes(), 32)...)
	batchLog.Data = batchData
	if !batch.Matches(batchLog) {
		t.Fatal("canonical TransferBatch event was rejected")
	}
	batchRecord := TransactionRecord{
		Operation: OperationERC1155BatchTransfer, Sender: from, Counterparty: to,
		AssetContract: contract, AssetAmount: new(big.Int), Effects: CloneEffectEntries(entries),
	}
	if !HasExpectedEffect(batchRecord, Receipt{Logs: []ReceiptLog{batchLog}}) {
		t.Fatal("ERC-1155 batch record did not confirm every effect")
	}
	reordered := batchLog
	reordered.Data = append(append([]byte(nil), batchData[:128]...), append(common.LeftPadBytes(big.NewInt(9).Bytes(), 32), common.LeftPadBytes(big.NewInt(7).Bytes(), 32)...)...)
	reordered.Data = append(reordered.Data, append(common.LeftPadBytes(big.NewInt(5).Bytes(), 32), common.LeftPadBytes(big.NewInt(3).Bytes(), 32)...)...)
	if batch.Matches(reordered) {
		t.Fatal("TransferBatch with reordered effects was accepted")
	}
}

func TestERC1155IntentValidation(t *testing.T) {
	accountID := "11111111-1111-4111-8111-111111111111"
	from := common.HexToAddress("0x1111111111111111111111111111111111111111")
	contract := common.HexToAddress("0x3333333333333333333333333333333333333333")
	to := common.HexToAddress("0x2222222222222222222222222222222222222222")
	if _, err := NewERC1155SafeTransferIntent(accountID, 1, from, contract, to, big.NewInt(7), big.NewInt(0)); err == nil {
		t.Fatal("zero ERC-1155 amount was accepted")
	}
	if _, err := NewERC1155SafeTransferIntent(accountID, 1, from, contract, to, nil, big.NewInt(3)); err == nil {
		t.Fatal("nil ERC-1155 token ID was accepted")
	}
	intent, err := NewERC1155SafeTransferIntent(accountID, 1, from, contract, to, big.NewInt(7), big.NewInt(3))
	if err != nil {
		t.Fatal(err)
	}
	if intent.TokenID().Cmp(big.NewInt(7)) != 0 || intent.Amount().Cmp(big.NewInt(3)) != 0 {
		t.Fatal("ERC-1155 intent did not preserve token and amount")
	}
	if _, err := NewERC1155BatchTransferIntent(accountID, 1, from, contract, to, nil); err == nil {
		t.Fatal("empty ERC-1155 batch was accepted")
	}
	if _, err := NewERC1155BatchTransferIntent(accountID, 1, from, contract, to, []EffectEntry{{TokenID: big.NewInt(-1), Amount: big.NewInt(1)}}); err == nil {
		t.Fatal("negative ERC-1155 batch token ID was accepted")
	}
	batch, err := NewERC1155BatchTransferIntent(accountID, 1, from, contract, to, []EffectEntry{{TokenID: big.NewInt(7), Amount: big.NewInt(3)}, {TokenID: big.NewInt(9), Amount: big.NewInt(5)}})
	if err != nil {
		t.Fatal(err)
	}
	if len(batch.Effects()) != 2 || batch.Effects()[1].TokenID.Cmp(big.NewInt(9)) != 0 {
		t.Fatal("ERC-1155 batch intent did not preserve effects")
	}
}

var _ = hex.EncodeToString
