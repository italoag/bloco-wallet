package signer

import (
	"bytes"
	"context"
	"errors"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

func TestTrezorTransactionLegacyAndDynamicFee(t *testing.T) {
	key := ledgerTransactionTestKey(t, 0x21)
	to := common.HexToAddress("0x1111111111111111111111111111111111111111")
	tests := []struct {
		name        string
		transaction *types.Transaction
		chainID     *big.Int
		wireType    int
		signer      types.Signer
	}{
		{
			name: "legacy",
			transaction: types.NewTx(&types.LegacyTx{
				Nonce: 1, GasPrice: big.NewInt(2), Gas: 21_000, To: &to, Value: big.NewInt(3), Data: []byte{0xaa},
			}),
			chainID: big.NewInt(1), wireType: trezorMessageEthereumSignTx,
			signer: types.NewEIP155Signer(big.NewInt(1)),
		},
		{
			name: "dynamic fee",
			transaction: types.NewTx(&types.DynamicFeeTx{
				ChainID: big.NewInt(1), Nonce: 1, GasTipCap: big.NewInt(2), GasFeeCap: big.NewInt(3),
				Gas: 21_000, To: &to, Value: big.NewInt(4), Data: []byte{0xbb},
			}),
			chainID: big.NewInt(1), wireType: trezorMessageEthereumSignTxEIP1559,
			signer: types.NewLondonSigner(big.NewInt(1)),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			digest := test.signer.Hash(test.transaction)
			signature, err := crypto.Sign(digest[:], key)
			if err != nil {
				t.Fatal(err)
			}
			wireV := uint32(signature[64])
			if test.transaction.Type() == types.LegacyTxType {
				wireV += uint32(2*test.chainID.Uint64() + 35)
			}
			transport := newTrezorTypedDataScriptTransport(trezorTransactionSignatureResponse(wireV, signature))
			device := &UDPDevice{transport: transport}
			result, err := device.SignTransaction(context.Background(), TrezorTransactionIntent{
				UnsignedTransaction: test.transaction, ChainID: test.chainID,
				DerivationPath: "m/44'/60'/0'/0/0", Digest: digest,
				ExpectedAddress: crypto.PubkeyToAddress(key.PublicKey),
			})
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(result, signature) {
				t.Fatalf("signature mismatch: %x != %x", result, signature)
			}
			if len(transport.writes) != 1 || transport.writes[0].messageType != test.wireType {
				t.Fatalf("unexpected Trezor writes: %+v", transport.writes)
			}
		})
	}
}

func TestTrezorTransactionStreamsRequestedData(t *testing.T) {
	key := ledgerTransactionTestKey(t, 0x22)
	to := common.HexToAddress("0x2222222222222222222222222222222222222222")
	data := bytes.Repeat([]byte{0x42}, 2500)
	transaction := types.NewTx(&types.DynamicFeeTx{
		ChainID: big.NewInt(1), Nonce: 3, GasTipCap: big.NewInt(2), GasFeeCap: big.NewInt(4),
		Gas: 500_000, To: &to, Value: big.NewInt(5), Data: data,
	})
	digest := types.NewLondonSigner(big.NewInt(1)).Hash(transaction)
	signature, err := crypto.Sign(digest[:], key)
	if err != nil {
		t.Fatal(err)
	}
	transport := newTrezorTypedDataScriptTransport(
		trezorTransactionDataRequest(1024),
		trezorTransactionDataRequest(452),
		trezorTransactionSignatureResponse(uint32(signature[64]), signature),
	)
	device := &UDPDevice{transport: transport}
	result, err := device.SignTransaction(context.Background(), TrezorTransactionIntent{
		UnsignedTransaction: transaction, ChainID: big.NewInt(1),
		DerivationPath: "m/44'/60'/0'/0/0", Digest: digest,
		ExpectedAddress: crypto.PubkeyToAddress(key.PublicKey),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(result, signature) {
		t.Fatal("streamed signature mismatch")
	}
	if len(transport.writes) != 3 || transport.writes[0].messageType != trezorMessageEthereumSignTxEIP1559 || transport.writes[1].messageType != trezorMessageEthereumTxAck || transport.writes[2].messageType != trezorMessageEthereumTxAck {
		t.Fatalf("unexpected transaction state machine: %+v", transport.writes)
	}
	firstChunk := trezorFieldValue(t, transport.writes[0].payload, 8)
	secondChunk := trezorFieldValue(t, transport.writes[1].payload, 1)
	thirdChunk := trezorFieldValue(t, transport.writes[2].payload, 1)
	joined := append(append(append([]byte(nil), firstChunk...), secondChunk...), thirdChunk...)
	if !bytes.Equal(joined, data) {
		t.Fatal("Trezor data chunk reconstruction mismatch")
	}
}

func TestTrezorTransactionFailsClosed(t *testing.T) {
	key := ledgerTransactionTestKey(t, 0x23)
	to := common.HexToAddress("0x3333333333333333333333333333333333333333")
	transaction := types.NewTx(&types.LegacyTx{Nonce: 1, GasPrice: big.NewInt(2), Gas: 21_000, To: &to, Value: big.NewInt(3)})
	digest := types.NewEIP155Signer(big.NewInt(1)).Hash(transaction)
	validIntent := TrezorTransactionIntent{
		UnsignedTransaction: transaction, ChainID: big.NewInt(1),
		DerivationPath: "m/44'/60'/0'/0/0", Digest: digest,
		ExpectedAddress: crypto.PubkeyToAddress(key.PublicKey),
	}
	tests := []struct {
		name      string
		responses []trezorTypedDataWireMessage
		mutate    func(*TrezorTransactionIntent)
	}{
		{name: "zero data request", responses: []trezorTypedDataWireMessage{trezorTransactionDataRequest(0)}},
		{name: "oversized data request", responses: []trezorTypedDataWireMessage{trezorTransactionDataRequest(1025)}},
		{name: "malformed signature", responses: []trezorTypedDataWireMessage{{messageType: trezorMessageEthereumTxRequest, payload: appendTrezorVarint(nil, 2, 27)}}},
		{name: "wrong digest", mutate: func(intent *TrezorTransactionIntent) { intent.Digest[0] ^= 1 }},
		{name: "wrong address", responses: []trezorTypedDataWireMessage{func() trezorTypedDataWireMessage {
			signature, _ := crypto.Sign(digest[:], key)
			return trezorTransactionSignatureResponse(uint32(signature[64]+37), signature)
		}()}, mutate: func(intent *TrezorTransactionIntent) { intent.ExpectedAddress[0] ^= 1 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			intent := validIntent
			if test.mutate != nil {
				test.mutate(&intent)
			}
			transport := newTrezorTypedDataScriptTransport(test.responses...)
			device := &UDPDevice{transport: transport}
			if _, err := device.SignTransaction(context.Background(), intent); err == nil {
				t.Fatal("invalid Trezor transaction was accepted")
			}
			if test.name == "wrong digest" && len(transport.writes) != 0 {
				t.Fatal("digest mismatch reached Trezor")
			}
		})
	}

	foreignKey := ledgerTransactionTestKey(t, 0x24)
	foreignSignature, err := crypto.Sign(digest[:], foreignKey)
	if err != nil {
		t.Fatal(err)
	}
	transport := newTrezorTypedDataScriptTransport(trezorTransactionSignatureResponse(uint32(foreignSignature[64]+37), foreignSignature))
	if _, err := (&UDPDevice{transport: transport}).SignTransaction(context.Background(), validIntent); !errors.Is(err, ErrTrezorSignature) {
		t.Fatalf("foreign transaction signature returned %v", err)
	}
}

func trezorTransactionDataRequest(length uint32) trezorTypedDataWireMessage {
	return trezorTypedDataWireMessage{
		messageType: trezorMessageEthereumTxRequest,
		payload:     appendTrezorVarint(nil, 1, uint64(length)),
	}
}

func trezorTransactionSignatureResponse(v uint32, signature []byte) trezorTypedDataWireMessage {
	payload := appendTrezorVarint(nil, 2, uint64(v))
	payload = appendBytesField(payload, 3, signature[:32])
	payload = appendBytesField(payload, 4, signature[32:64])
	return trezorTypedDataWireMessage{messageType: trezorMessageEthereumTxRequest, payload: payload}
}

func trezorFieldValue(t *testing.T, payload []byte, wanted uint64) []byte {
	t.Helper()
	for len(payload) > 0 {
		tag, value, rest, err := decodeField(payload)
		if err != nil {
			t.Fatal(err)
		}
		if tag>>3 == wanted {
			return append([]byte(nil), value...)
		}
		payload = rest
	}
	t.Fatalf("field %d missing", wanted)
	return nil
}
