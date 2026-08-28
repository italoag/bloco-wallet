package evm

import (
	"bytes"
	"encoding/hex"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

func TestEncodeERC721SafeTransferMatchesGoldenVector(t *testing.T) {
	from := common.HexToAddress("0x1111111111111111111111111111111111111111")
	to := common.HexToAddress("0x2222222222222222222222222222222222222222")
	tokenID := big.NewInt(42)
	encoded := encodeERC721SafeTransferMethod(from, to, tokenID)
	expected := "42842e0e" +
		"0000000000000000000000001111111111111111111111111111111111111111" +
		"0000000000000000000000002222222222222222222222222222222222222222" +
		"000000000000000000000000000000000000000000000000000000000000002a"
	if hex.EncodeToString(encoded) != expected || !bytes.Equal(encoded[:4], crypto.Keccak256([]byte("safeTransferFrom(address,address,uint256)"))[:4]) {
		t.Fatalf("ERC-721 calldata does not match golden vector: %x", encoded)
	}
	if len(encoded) != 100 {
		t.Fatalf("ERC-721 calldata length is %d", len(encoded))
	}
}

func TestERC721TransferEffectMatchesCanonicalEvent(t *testing.T) {
	contract := common.HexToAddress("0x3333333333333333333333333333333333333333")
	from := common.HexToAddress("0x1111111111111111111111111111111111111111")
	to := common.HexToAddress("0x2222222222222222222222222222222222222222")
	tokenID := big.NewInt(42)
	effect := &TransferEffect{
		Kind: EffectERC721Transfer, AssetContract: contract, From: from, To: to, TokenID: tokenID,
	}
	canonical := ReceiptLog{
		Address: contract,
		Topics: []common.Hash{
			common.HexToHash("0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef"),
			common.BytesToHash(from.Bytes()),
			common.BytesToHash(to.Bytes()),
			common.BytesToHash(common.LeftPadBytes(tokenID.Bytes(), 32)),
		},
	}
	if !effect.Matches(canonical) {
		t.Fatal("canonical ERC-721 Transfer event was rejected")
	}
	record := TransactionRecord{
		Operation: OperationERC721SafeTransfer, Sender: from, Counterparty: to,
		AssetContract: contract, AssetAmount: new(big.Int).Set(tokenID),
	}
	if !HasExpectedEffect(record, Receipt{Logs: []ReceiptLog{canonical}}) {
		t.Fatal("ERC-721 record did not confirm its effect")
	}
	mutations := []func(*ReceiptLog){
		func(log *ReceiptLog) { log.Address = common.HexToAddress("0x4444444444444444444444444444444444444444") },
		func(log *ReceiptLog) { log.Topics = log.Topics[:3] },
		func(log *ReceiptLog) {
			log.Topics[0] = common.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000000")
		},
		func(log *ReceiptLog) { log.Topics[1] = common.BytesToHash(to.Bytes()) },
		func(log *ReceiptLog) { log.Topics[2] = common.BytesToHash(from.Bytes()) },
		func(log *ReceiptLog) {
			log.Topics[3] = common.BytesToHash(common.LeftPadBytes(big.NewInt(43).Bytes(), 32))
		},
		func(log *ReceiptLog) { log.Data = []byte{0} },
	}
	for _, mutate := range mutations {
		invalid := canonical
		mutate(&invalid)
		if effect.Matches(invalid) {
			t.Fatal("invalid ERC-721 Transfer event was accepted")
		}
	}
}

func TestERC721IntentValidationAndPlanDeterminism(t *testing.T) {
	valid := ERC721SafeTransferIntent{
		accountID: "11111111-1111-4111-8111-111111111111", chainID: 1,
		from:     common.HexToAddress("0x1111111111111111111111111111111111111111"),
		contract: common.HexToAddress("0x3333333333333333333333333333333333333333"),
		to:       common.HexToAddress("0x2222222222222222222222222222222222222222"),
		tokenID:  big.NewInt(0),
	}
	intent, err := NewERC721SafeTransferIntent(valid.accountID, valid.chainID, valid.from, valid.contract, valid.to, valid.tokenID)
	if err != nil {
		t.Fatalf("zero token ID was rejected: %v", err)
	}
	if intent.TokenID().Sign() != 0 {
		t.Fatal("zero token ID was not preserved")
	}
	for _, value := range []*big.Int{nil, big.NewInt(-1), new(big.Int).Lsh(big.NewInt(1), 256)} {
		if _, err := NewERC721SafeTransferIntent(valid.accountID, valid.chainID, valid.from, valid.contract, valid.to, value); err == nil {
			t.Fatal("invalid token ID was accepted")
		}
	}
	intent, err = NewERC721SafeTransferIntent(valid.accountID, valid.chainID, valid.from, valid.contract, valid.to, big.NewInt(42))
	if err != nil {
		t.Fatal(err)
	}
	planner := NewPlanner()
	blockHash := common.HexToHash("0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	input := ERC721PlanInput{
		NativePlanInput: NativePlanInput{
			ProviderBinding: ProviderBinding{1}, Nonce: 7, GasLimit: 100_000, LegacyGasPrice: big.NewInt(1_000_000_000),
			SimulationBlockNumber: 100, SimulationBlockHash: blockHash, SimulationResultHash: common.HexToHash("0x01"),
		},
		TokenID: big.NewInt(42),
	}
	first, err := planner.PlanERC721SafeTransfer(intent, input)
	if err != nil {
		t.Fatal(err)
	}
	if first.Operation() != OperationERC721SafeTransfer || first.Counterparty() != intent.to || first.TokenID().Cmp(big.NewInt(42)) != 0 || first.Amount() != nil || first.Asset().Contract != intent.contract {
		t.Fatalf("unexpected ERC-721 plan: %+v", first)
	}
	transaction := first.Transaction()
	if transaction == nil || transaction.To() == nil || *transaction.To() != intent.contract || transaction.Value().Sign() != 0 || len(transaction.Data()) != 100 {
		t.Fatalf("unexpected ERC-721 transaction: %+v", transaction)
	}
	second, err := planner.PlanERC721SafeTransfer(intent, input)
	if err != nil {
		t.Fatal(err)
	}
	if first.PlanHash() != second.PlanHash() || first.TransactionDigest() != second.TransactionDigest() {
		t.Fatal("ERC-721 plan is not deterministic")
	}
	dynamicInput := ERC721DynamicPlanInput{
		DynamicFeePlanInput: DynamicFeePlanInput{
			ProviderBinding: ProviderBinding{1}, Nonce: 7, GasLimit: 100_000,
			GasFeeCap: big.NewInt(2_000_000_000), GasTipCap: big.NewInt(1_000_000_000),
			SimulationBlockNumber: 100, SimulationBlockHash: blockHash, SimulationResultHash: common.HexToHash("0x01"),
		},
		TokenID: big.NewInt(42),
	}
	dynamic, err := planner.PlanERC721SafeTransferDynamicFee(intent, dynamicInput)
	if err != nil {
		t.Fatal(err)
	}
	if dynamic.Transaction().Type() != 2 {
		t.Fatalf("unexpected ERC-721 dynamic transaction type: %d", dynamic.Transaction().Type())
	}
}
