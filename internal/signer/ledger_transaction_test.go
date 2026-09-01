package signer

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"errors"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rlp"
)

const ledgerTransactionTestPath = "m/44'/60'/0'/0/0"

type ledgerTransactionAPDUCall struct {
	cla  byte
	ins  byte
	p1   byte
	p2   byte
	data []byte
}

type ledgerTransactionTestTransport struct {
	key                 *ecdsa.PrivateKey
	chainID             *big.Int
	transactionType     byte
	digest              [32]byte
	expectedPayload     []byte
	calls               []ledgerTransactionAPDUCall
	received            []byte
	statuses            map[int]uint16
	exchangeErrors      map[int]error
	overrideResponse    []byte
	overrideResponseSet bool
	forceV              *byte
	highS               bool
	legacyDirectV       bool
	prematureResponse   bool
	cancel              context.CancelFunc
	cancelAfterCall     int
	lastResponse        []byte
}

func (transport *ledgerTransactionTestTransport) Exchange(_ context.Context, cla, ins, p1, p2 byte, data []byte) ([]byte, uint16, error) {
	if ins == ledgerINSGetAppConfiguration && cla == ledgerCLA && p1 == 0 && p2 == 0 && len(data) == 0 {
		return []byte{0x00, 0x01, 0x16, 0x03}, ledgerSWOK, nil
	}
	callIndex := len(transport.calls)
	transport.calls = append(transport.calls, ledgerTransactionAPDUCall{
		cla: cla, ins: ins, p1: p1, p2: p2, data: append([]byte(nil), data...),
	})
	if cla != ledgerCLA || ins != ledgerINSSign || p2 != 0 || (callIndex == 0 && p1 != 0) || (callIndex > 0 && p1 != 0x80) || len(data) > ledgerTransactionAPDUDataLimit {
		return nil, 0x6a80, nil
	}
	if transport.cancel != nil && callIndex == transport.cancelAfterCall {
		transport.cancel()
	}
	if err, ok := transport.exchangeErrors[callIndex]; ok {
		return nil, 0, err
	}
	if status, ok := transport.statuses[callIndex]; ok {
		return nil, status, nil
	}

	transport.received = append(transport.received, data...)
	if len(transport.received) > len(transport.expectedPayload) || !bytes.Equal(transport.received, transport.expectedPayload[:len(transport.received)]) {
		return nil, 0x6a80, nil
	}
	if len(transport.received) != len(transport.expectedPayload) {
		if transport.prematureResponse {
			return []byte{0x01}, ledgerSWOK, nil
		}
		return nil, ledgerSWOK, nil
	}
	if transport.overrideResponseSet {
		transport.lastResponse = append([]byte(nil), transport.overrideResponse...)
		return append([]byte(nil), transport.overrideResponse...), ledgerSWOK, nil
	}
	if transport.key == nil {
		return nil, 0x6a80, nil
	}
	signature, err := crypto.Sign(transport.digest[:], transport.key)
	if err != nil {
		return nil, 0, err
	}
	if transport.highS {
		highS := new(big.Int).Sub(crypto.S256().Params().N, new(big.Int).SetBytes(signature[32:64]))
		highS.FillBytes(signature[32:64])
		signature[64] ^= 1
	}
	v := signature[64]
	if transport.transactionType == types.LegacyTxType {
		if transport.legacyDirectV {
			v += 27
		} else {
			v += ledgerLegacyVBase(transport.chainID)
		}
	}
	if transport.forceV != nil {
		v = *transport.forceV
	}
	response := append([]byte{v}, signature[:64]...)
	transport.lastResponse = append([]byte(nil), response...)
	return response, ledgerSWOK, nil
}

func TestLedgerTransactionLegacyCanonicalAPDU(t *testing.T) {
	key := ledgerTransactionTestKey(t, 0x01)
	chainID := big.NewInt(1)
	to := common.HexToAddress("0x3535353535353535353535353535353535353535")
	transaction := types.NewTx(&types.LegacyTx{
		Nonce: 9, GasPrice: big.NewInt(20_000_000_000), Gas: 21_000,
		To: &to, Value: big.NewInt(1_000_000_000_000_000_000),
	})
	expected := ledgerTransactionMustDecodeHex(t, "ec098504a817c800825208943535353535353535353535353535353535353535880de0b6b3a764000080018080")
	intent := ledgerTransactionTestIntent(transaction, chainID, expected, key)
	transport := ledgerTransactionSigningTransport(key, intent, expected)
	device := ledgerTransactionTestDevice(t, transport)

	signature, err := device.SignTransaction(context.Background(), intent)
	if err != nil {
		t.Fatal(err)
	}
	if len(transport.calls) != 1 {
		t.Fatalf("expected one APDU, got %d", len(transport.calls))
	}
	call := transport.calls[0]
	if call.cla != 0xe0 || call.ins != 0x04 || call.p1 != 0 || call.p2 != 0 {
		t.Fatalf("unexpected APDU header: %02x %02x %02x %02x", call.cla, call.ins, call.p1, call.p2)
	}
	expectedRequest := append(ledgerTransactionTestPathBytes(), expected...)
	if !bytes.Equal(call.data, expectedRequest) {
		t.Fatalf("legacy APDU payload mismatch\n got: %x\nwant: %x", call.data, expectedRequest)
	}
	if transport.lastResponse[0] != byte(2*chainID.Uint64()+35)+signature[64] {
		t.Fatalf("Ledger EIP-155 v was not exercised: response=%d parity=%d", transport.lastResponse[0], signature[64])
	}
	ledgerTransactionAssertSignature(t, transaction, types.NewEIP155Signer(chainID), intent, signature)
}

func TestLedgerTransactionDynamicFeeCanonicalAPDU(t *testing.T) {
	key := ledgerTransactionTestKey(t, 0x01)
	chainID := big.NewInt(1)
	to := common.HexToAddress("0x1111111111111111111111111111111111111111")
	transaction := types.NewTx(&types.DynamicFeeTx{
		ChainID: chainID, Nonce: 2, GasTipCap: big.NewInt(3), GasFeeCap: big.NewInt(4),
		Gas: 21_000, To: &to, Value: big.NewInt(5), Data: []byte{0xaa, 0xbb},
	})
	expected := ledgerTransactionMustDecodeHex(t, "02e1010203048252089411111111111111111111111111111111111111110582aabbc0")
	intent := ledgerTransactionTestIntent(transaction, chainID, expected, key)
	transport := ledgerTransactionSigningTransport(key, intent, expected)
	device := ledgerTransactionTestDevice(t, transport)

	signature, err := device.SignTransaction(context.Background(), intent)
	if err != nil {
		t.Fatal(err)
	}
	if len(transport.calls) != 1 {
		t.Fatalf("expected one APDU, got %d", len(transport.calls))
	}
	expectedRequest := append(ledgerTransactionTestPathBytes(), expected...)
	if !bytes.Equal(transport.calls[0].data, expectedRequest) {
		t.Fatalf("EIP-1559 APDU payload mismatch\n got: %x\nwant: %x", transport.calls[0].data, expectedRequest)
	}
	if transport.lastResponse[0] != signature[64] {
		t.Fatalf("typed transaction v was not parity: response=%d signature=%d", transport.lastResponse[0], signature[64])
	}
	ledgerTransactionAssertSignature(t, transaction, types.NewLondonSigner(chainID), intent, signature)
}

func TestLedgerTransactionStreamsAPDUChunks(t *testing.T) {
	key := ledgerTransactionTestKey(t, 0x01)
	chainID := big.NewInt(11155111)
	to := common.HexToAddress("0x2222222222222222222222222222222222222222")
	transaction := types.NewTx(&types.DynamicFeeTx{
		ChainID: chainID, Nonce: 17, GasTipCap: big.NewInt(2_000_000_000), GasFeeCap: big.NewInt(30_000_000_000),
		Gas: 500_000, To: &to, Value: big.NewInt(42), Data: bytes.Repeat([]byte{0xa5}, 900),
		AccessList: types.AccessList{{Address: common.HexToAddress("0x3333333333333333333333333333333333333333"), StorageKeys: []common.Hash{{1}, {2}}}},
	})
	encoded := ledgerTransactionTestEncode(t, transaction, chainID)
	intent := ledgerTransactionTestIntent(transaction, chainID, encoded, key)
	transport := ledgerTransactionSigningTransport(key, intent, encoded)
	device := ledgerTransactionTestDevice(t, transport)

	signature, err := device.SignTransaction(context.Background(), intent)
	if err != nil {
		t.Fatal(err)
	}
	if len(transport.calls) < 4 {
		t.Fatalf("transaction was not streamed: %d calls", len(transport.calls))
	}
	for index, call := range transport.calls {
		if len(call.data) == 0 || len(call.data) > 255 {
			t.Fatalf("chunk %d has invalid length %d", index, len(call.data))
		}
		wantP1 := byte(0x80)
		if index == 0 {
			wantP1 = 0
		}
		if call.cla != ledgerCLA || call.ins != 0x04 || call.p1 != wantP1 || call.p2 != 0 {
			t.Fatalf("chunk %d header = %02x %02x %02x %02x", index, call.cla, call.ins, call.p1, call.p2)
		}
		if index < len(transport.calls)-1 && len(call.data) != 255 {
			t.Fatalf("non-final EIP-1559 chunk %d has length %d", index, len(call.data))
		}
	}
	expectedPayload := append(ledgerTransactionTestPathBytes(), encoded...)
	if !bytes.Equal(transport.received, expectedPayload) {
		t.Fatal("streamed APDU bytes differ from path || canonical transaction")
	}
	ledgerTransactionAssertSignature(t, transaction, types.NewLondonSigner(chainID), intent, signature)
}

func TestLedgerTransactionLegacyChunkKeepsEIP155SuffixTogether(t *testing.T) {
	key := ledgerTransactionTestKey(t, 0x01)
	chainID := big.NewInt(1)
	to := common.HexToAddress("0x4444444444444444444444444444444444444444")
	var (
		transaction *types.Transaction
		encoded     []byte
	)
	for dataLength := 150; dataLength < 800; dataLength++ {
		candidate := types.NewTx(&types.LegacyTx{
			Nonce: 1, GasPrice: big.NewInt(2), Gas: 500_000, To: &to, Value: big.NewInt(3),
			Data: bytes.Repeat([]byte{0x7f}, dataLength),
		})
		candidateEncoding := ledgerTransactionTestEncode(t, candidate, chainID)
		remainder := (len(ledgerTransactionTestPathBytes()) + len(candidateEncoding)) % 255
		if remainder > 0 && remainder <= ledgerEIP155SuffixLength(chainID) {
			transaction, encoded = candidate, candidateEncoding
			break
		}
	}
	if transaction == nil {
		t.Fatal("failed to construct legacy APDU boundary vector")
	}
	intent := ledgerTransactionTestIntent(transaction, chainID, encoded, key)
	transport := ledgerTransactionSigningTransport(key, intent, encoded)
	device := ledgerTransactionTestDevice(t, transport)

	if _, err := device.SignTransaction(context.Background(), intent); err != nil {
		t.Fatal(err)
	}
	if len(transport.calls) < 2 {
		t.Fatal("boundary vector did not stream")
	}
	last := transport.calls[len(transport.calls)-1].data
	suffixLength := ledgerEIP155SuffixLength(chainID)
	if len(last) <= suffixLength {
		t.Fatalf("final chunk contains no pre-suffix byte: final=%d suffix=%d", len(last), suffixLength)
	}
	if !bytes.Equal(last[len(last)-suffixLength:], encoded[len(encoded)-suffixLength:]) {
		t.Fatal("EIP-155 chainID,0,0 suffix was split or changed")
	}
	for index, call := range transport.calls {
		if len(call.data) > 255 {
			t.Fatalf("legacy chunk %d exceeds APDU bound: %d", index, len(call.data))
		}
	}
}

func TestLedgerTransactionMapsStatusAndRejectsMalformedResponses(t *testing.T) {
	key := ledgerTransactionTestKey(t, 0x01)
	chainID := big.NewInt(1)
	to := common.HexToAddress("0x5555555555555555555555555555555555555555")
	shortTransaction := types.NewTx(&types.DynamicFeeTx{
		ChainID: chainID, Nonce: 1, GasTipCap: big.NewInt(1), GasFeeCap: big.NewInt(2),
		Gas: 21_000, To: &to, Value: big.NewInt(1),
	})
	shortEncoded := ledgerTransactionTestEncode(t, shortTransaction, chainID)
	shortIntent := ledgerTransactionTestIntent(shortTransaction, chainID, shortEncoded, key)

	t.Run("deny", func(t *testing.T) {
		transport := ledgerTransactionSigningTransport(key, shortIntent, shortEncoded)
		transport.statuses = map[int]uint16{0: ledgerSWDeny}
		_, err := ledgerTransactionTestDevice(t, transport).SignTransaction(context.Background(), shortIntent)
		if !errors.Is(err, ErrLedgerDenied) {
			t.Fatalf("deny status returned %v", err)
		}
	})

	t.Run("unknown status", func(t *testing.T) {
		transport := ledgerTransactionSigningTransport(key, shortIntent, shortEncoded)
		transport.statuses = map[int]uint16{0: 0x6a80}
		_, err := ledgerTransactionTestDevice(t, transport).SignTransaction(context.Background(), shortIntent)
		if !errors.Is(err, ErrLedgerTransport) {
			t.Fatalf("unknown status returned %v", err)
		}
	})

	t.Run("transport error", func(t *testing.T) {
		transportFailure := errors.New("test transport disconnected")
		transport := ledgerTransactionSigningTransport(key, shortIntent, shortEncoded)
		transport.exchangeErrors = map[int]error{0: transportFailure}
		_, err := ledgerTransactionTestDevice(t, transport).SignTransaction(context.Background(), shortIntent)
		if !errors.Is(err, ErrLedgerTransport) || !errors.Is(err, transportFailure) {
			t.Fatalf("transport error lost identity: %v", err)
		}
	})

	for _, test := range []struct {
		name     string
		response []byte
	}{
		{name: "empty", response: nil},
		{name: "short", response: []byte{27, 1, 2}},
	} {
		t.Run(test.name+" signature", func(t *testing.T) {
			transport := ledgerTransactionSigningTransport(key, shortIntent, shortEncoded)
			transport.overrideResponseSet = true
			transport.overrideResponse = test.response
			_, err := ledgerTransactionTestDevice(t, transport).SignTransaction(context.Background(), shortIntent)
			if !errors.Is(err, ErrLedgerTransport) {
				t.Fatalf("malformed response returned %v", err)
			}
		})
	}

	t.Run("invalid typed v", func(t *testing.T) {
		invalidV := byte(2)
		transport := ledgerTransactionSigningTransport(key, shortIntent, shortEncoded)
		transport.forceV = &invalidV
		_, err := ledgerTransactionTestDevice(t, transport).SignTransaction(context.Background(), shortIntent)
		if !errors.Is(err, ErrLedgerTransport) {
			t.Fatalf("invalid v returned %v", err)
		}
	})

	longTransaction := types.NewTx(&types.DynamicFeeTx{
		ChainID: chainID, Nonce: 1, GasTipCap: big.NewInt(1), GasFeeCap: big.NewInt(2),
		Gas: 500_000, To: &to, Value: big.NewInt(1), Data: bytes.Repeat([]byte{0x99}, 600),
	})
	longEncoded := ledgerTransactionTestEncode(t, longTransaction, chainID)
	longIntent := ledgerTransactionTestIntent(longTransaction, chainID, longEncoded, key)

	t.Run("deny continuation", func(t *testing.T) {
		transport := ledgerTransactionSigningTransport(key, longIntent, longEncoded)
		transport.statuses = map[int]uint16{1: ledgerSWDeny}
		_, err := ledgerTransactionTestDevice(t, transport).SignTransaction(context.Background(), longIntent)
		if !errors.Is(err, ErrLedgerDenied) || len(transport.calls) != 2 {
			t.Fatalf("continuation deny returned %v after %d calls", err, len(transport.calls))
		}
	})

	t.Run("premature response", func(t *testing.T) {
		transport := ledgerTransactionSigningTransport(key, longIntent, longEncoded)
		transport.prematureResponse = true
		_, err := ledgerTransactionTestDevice(t, transport).SignTransaction(context.Background(), longIntent)
		if !errors.Is(err, ErrLedgerTransport) || len(transport.calls) != 1 {
			t.Fatalf("premature response returned %v after %d calls", err, len(transport.calls))
		}
	})
}

func TestLedgerTransactionRejectsHighSAndWrongKey(t *testing.T) {
	expectedKey := ledgerTransactionTestKey(t, 0x01)
	foreignKey := ledgerTransactionTestKey(t, 0x02)
	chainID := big.NewInt(1)
	to := common.HexToAddress("0x6666666666666666666666666666666666666666")
	transaction := types.NewTx(&types.DynamicFeeTx{
		ChainID: chainID, Nonce: 1, GasTipCap: big.NewInt(1), GasFeeCap: big.NewInt(2),
		Gas: 21_000, To: &to, Value: big.NewInt(1),
	})
	encoded := ledgerTransactionTestEncode(t, transaction, chainID)
	intent := ledgerTransactionTestIntent(transaction, chainID, encoded, expectedKey)

	t.Run("high S", func(t *testing.T) {
		transport := ledgerTransactionSigningTransport(expectedKey, intent, encoded)
		transport.highS = true
		_, err := ledgerTransactionTestDevice(t, transport).SignTransaction(context.Background(), intent)
		if !errors.Is(err, ErrLedgerSignature) {
			t.Fatalf("high-S signature returned %v", err)
		}
	})

	t.Run("wrong key", func(t *testing.T) {
		transport := ledgerTransactionSigningTransport(foreignKey, intent, encoded)
		_, err := ledgerTransactionTestDevice(t, transport).SignTransaction(context.Background(), intent)
		if !errors.Is(err, ErrLedgerSignature) {
			t.Fatalf("foreign signature returned %v", err)
		}
	})

	t.Run("legacy direct v compatibility", func(t *testing.T) {
		legacy := types.NewTx(&types.LegacyTx{
			Nonce: 1, GasPrice: big.NewInt(2), Gas: 21_000, To: &to, Value: big.NewInt(3),
		})
		legacyEncoded := ledgerTransactionTestEncode(t, legacy, chainID)
		legacyIntent := ledgerTransactionTestIntent(legacy, chainID, legacyEncoded, expectedKey)
		transport := ledgerTransactionSigningTransport(expectedKey, legacyIntent, legacyEncoded)
		transport.legacyDirectV = true
		signature, err := ledgerTransactionTestDevice(t, transport).SignTransaction(context.Background(), legacyIntent)
		if err != nil {
			t.Fatal(err)
		}
		ledgerTransactionAssertSignature(t, legacy, types.NewEIP155Signer(chainID), legacyIntent, signature)
	})
}

func TestLedgerTransactionValidatesDigestAndIntentBounds(t *testing.T) {
	key := ledgerTransactionTestKey(t, 0x01)
	chainID := big.NewInt(1)
	to := common.HexToAddress("0x7777777777777777777777777777777777777777")
	transaction := types.NewTx(&types.LegacyTx{
		Nonce: 1, GasPrice: big.NewInt(2), Gas: 21_000, To: &to, Value: big.NewInt(3),
	})
	encoded := ledgerTransactionTestEncode(t, transaction, chainID)
	validIntent := ledgerTransactionTestIntent(transaction, chainID, encoded, key)

	t.Run("digest mismatch", func(t *testing.T) {
		transport := &ledgerTransactionTestTransport{}
		intent := validIntent
		intent.Digest[0] ^= 0xff
		_, err := ledgerTransactionTestDevice(t, transport).SignTransaction(context.Background(), intent)
		if !errors.Is(err, ErrLedgerTransactionDigest) || len(transport.calls) != 0 {
			t.Fatalf("digest mismatch returned %v after %d calls", err, len(transport.calls))
		}
	})

	signature, err := crypto.Sign(validIntent.Digest[:], key)
	if err != nil {
		t.Fatal(err)
	}
	signedTransaction, err := transaction.WithSignature(types.NewEIP155Signer(chainID), signature)
	if err != nil {
		t.Fatal(err)
	}
	oversizedTransaction := types.NewTx(&types.LegacyTx{
		Nonce: 1, GasPrice: big.NewInt(2), Gas: 21_000, To: &to, Value: big.NewInt(3),
		Data: bytes.Repeat([]byte{1}, ledgerMaxTransactionRLPSize+1),
	})
	tooLargeValue := new(big.Int).Lsh(big.NewInt(1), 256)
	largeValueTransaction := types.NewTx(&types.LegacyTx{
		Nonce: 1, GasPrice: big.NewInt(2), Gas: 21_000, To: &to, Value: tooLargeValue,
	})
	accessListTransaction := types.NewTx(&types.AccessListTx{
		ChainID: chainID, Nonce: 1, GasPrice: big.NewInt(2), Gas: 21_000, To: &to, Value: big.NewInt(3),
	})
	mismatchedDynamic := types.NewTx(&types.DynamicFeeTx{
		ChainID: big.NewInt(2), Nonce: 1, GasTipCap: big.NewInt(1), GasFeeCap: big.NewInt(2),
		Gas: 21_000, To: &to, Value: big.NewInt(3),
	})
	invalidFees := types.NewTx(&types.DynamicFeeTx{
		ChainID: chainID, Nonce: 1, GasTipCap: big.NewInt(3), GasFeeCap: big.NewInt(2),
		Gas: 21_000, To: &to, Value: big.NewInt(3),
	})

	tests := []struct {
		name   string
		mutate func(*LedgerTransactionIntent)
	}{
		{name: "nil transaction", mutate: func(intent *LedgerTransactionIntent) { intent.UnsignedTransaction = nil }},
		{name: "nil chain", mutate: func(intent *LedgerTransactionIntent) { intent.ChainID = nil }},
		{name: "zero chain", mutate: func(intent *LedgerTransactionIntent) { intent.ChainID = new(big.Int) }},
		{name: "negative chain", mutate: func(intent *LedgerTransactionIntent) { intent.ChainID = big.NewInt(-1) }},
		{name: "chain over uint64", mutate: func(intent *LedgerTransactionIntent) { intent.ChainID = new(big.Int).Lsh(big.NewInt(1), 64) }},
		{name: "zero expected address", mutate: func(intent *LedgerTransactionIntent) { intent.ExpectedAddress = common.Address{} }},
		{name: "invalid path", mutate: func(intent *LedgerTransactionIntent) { intent.DerivationPath = "44'/60'/0'/0/0" }},
		{name: "signed transaction", mutate: func(intent *LedgerTransactionIntent) { intent.UnsignedTransaction = signedTransaction }},
		{name: "oversized data", mutate: func(intent *LedgerTransactionIntent) { intent.UnsignedTransaction = oversizedTransaction }},
		{name: "uint256 overflow", mutate: func(intent *LedgerTransactionIntent) { intent.UnsignedTransaction = largeValueTransaction }},
		{name: "unsupported type", mutate: func(intent *LedgerTransactionIntent) { intent.UnsignedTransaction = accessListTransaction }},
		{name: "dynamic chain mismatch", mutate: func(intent *LedgerTransactionIntent) { intent.UnsignedTransaction = mismatchedDynamic }},
		{name: "invalid dynamic fees", mutate: func(intent *LedgerTransactionIntent) { intent.UnsignedTransaction = invalidFees }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			transport := &ledgerTransactionTestTransport{}
			intent := validIntent
			test.mutate(&intent)
			_, err := ledgerTransactionTestDevice(t, transport).SignTransaction(context.Background(), intent)
			if !errors.Is(err, ErrLedgerTransactionIntent) || len(transport.calls) != 0 {
				t.Fatalf("invalid intent returned %v after %d calls", err, len(transport.calls))
			}
		})
	}
}

func TestLedgerTransactionHonorsContextCancellation(t *testing.T) {
	key := ledgerTransactionTestKey(t, 0x01)
	chainID := big.NewInt(1)
	to := common.HexToAddress("0x8888888888888888888888888888888888888888")
	transaction := types.NewTx(&types.DynamicFeeTx{
		ChainID: chainID, Nonce: 1, GasTipCap: big.NewInt(1), GasFeeCap: big.NewInt(2),
		Gas: 500_000, To: &to, Value: big.NewInt(1), Data: bytes.Repeat([]byte{0x42}, 600),
	})
	encoded := ledgerTransactionTestEncode(t, transaction, chainID)
	intent := ledgerTransactionTestIntent(transaction, chainID, encoded, key)

	t.Run("already canceled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		transport := &ledgerTransactionTestTransport{}
		_, err := ledgerTransactionTestDevice(t, transport).SignTransaction(ctx, intent)
		if !errors.Is(err, context.Canceled) || len(transport.calls) != 0 {
			t.Fatalf("pre-canceled context returned %v after %d calls", err, len(transport.calls))
		}
	})

	t.Run("between chunks", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		transport := ledgerTransactionSigningTransport(key, intent, encoded)
		transport.cancel = cancel
		transport.cancelAfterCall = 0
		_, err := ledgerTransactionTestDevice(t, transport).SignTransaction(ctx, intent)
		if !errors.Is(err, context.Canceled) || len(transport.calls) != 1 {
			t.Fatalf("stream cancellation returned %v after %d calls", err, len(transport.calls))
		}
	})
}

func ledgerTransactionSigningTransport(key *ecdsa.PrivateKey, intent LedgerTransactionIntent, encoded []byte) *ledgerTransactionTestTransport {
	return &ledgerTransactionTestTransport{
		key: key, chainID: new(big.Int).Set(intent.ChainID), transactionType: intent.UnsignedTransaction.Type(),
		digest: intent.Digest, expectedPayload: append(ledgerTransactionTestPathBytes(), encoded...),
	}
}

func ledgerTransactionTestIntent(transaction *types.Transaction, chainID *big.Int, encoded []byte, key *ecdsa.PrivateKey) LedgerTransactionIntent {
	return LedgerTransactionIntent{
		UnsignedTransaction: transaction,
		ChainID:             new(big.Int).Set(chainID),
		DerivationPath:      ledgerTransactionTestPath,
		Digest:              [32]byte(crypto.Keccak256Hash(encoded)),
		ExpectedAddress:     crypto.PubkeyToAddress(key.PublicKey),
	}
}

func ledgerTransactionTestEncode(t *testing.T, transaction *types.Transaction, chainID *big.Int) []byte {
	t.Helper()
	var (
		encoded []byte
		err     error
	)
	switch transaction.Type() {
	case types.LegacyTxType:
		encoded, err = rlp.EncodeToBytes([]any{
			transaction.Nonce(), transaction.GasPrice(), transaction.Gas(), transaction.To(),
			transaction.Value(), transaction.Data(), chainID, new(big.Int), new(big.Int),
		})
	case types.DynamicFeeTxType:
		encoded, err = rlp.EncodeToBytes([]any{
			chainID, transaction.Nonce(), transaction.GasTipCap(), transaction.GasFeeCap(),
			transaction.Gas(), transaction.To(), transaction.Value(), transaction.Data(), transaction.AccessList(),
		})
		encoded = append([]byte{types.DynamicFeeTxType}, encoded...)
	default:
		t.Fatalf("unsupported test transaction type %d", transaction.Type())
	}
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func ledgerTransactionTestDevice(t *testing.T, transport APDUTransport) *LedgerDevice {
	t.Helper()
	device, err := NewLedgerDevice(transport)
	if err != nil {
		t.Fatal(err)
	}
	return device
}

func ledgerTransactionTestKey(t *testing.T, fill byte) *ecdsa.PrivateKey {
	t.Helper()
	key, err := crypto.ToECDSA(bytes.Repeat([]byte{fill}, 32))
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func ledgerTransactionTestPathBytes() []byte {
	return []byte{
		5,
		0x80, 0x00, 0x00, 0x2c,
		0x80, 0x00, 0x00, 0x3c,
		0x80, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00,
	}
}

func ledgerTransactionMustDecodeHex(t *testing.T, value string) []byte {
	t.Helper()
	decoded, err := hex.DecodeString(value)
	if err != nil {
		t.Fatal(err)
	}
	return decoded
}

func ledgerTransactionAssertSignature(t *testing.T, transaction *types.Transaction, transactionSigner types.Signer, intent LedgerTransactionIntent, signature []byte) {
	t.Helper()
	if len(signature) != crypto.SignatureLength || signature[64] > 1 {
		t.Fatalf("invalid canonical signature: %x", signature)
	}
	if err := verifyECDSASignature(intent.ExpectedAddress, intent.Digest, signature); err != nil {
		t.Fatal(err)
	}
	signed, err := transaction.WithSignature(transactionSigner, signature)
	if err != nil {
		t.Fatal(err)
	}
	sender, err := types.Sender(transactionSigner, signed)
	if err != nil {
		t.Fatal(err)
	}
	if sender != intent.ExpectedAddress {
		t.Fatalf("sender = %s, want %s", sender, intent.ExpectedAddress)
	}
}
