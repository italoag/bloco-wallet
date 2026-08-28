package evm

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

func FuzzERC1155BatchDecoder(fuzzer *testing.F) {
	fuzzer.Add([]byte{})
	fuzzer.Add(make([]byte, 128))
	fuzzer.Add(make([]byte, 256))
	fuzzer.Fuzz(func(t *testing.T, data []byte) {
		ids, amounts, err := decodeERC1155BatchData(data)
		if err == nil {
			if len(ids) == 0 || len(ids) != len(amounts) || len(ids) > MaxEIP712ArrayLength {
				t.Fatalf("decoder returned invalid batch: %d", len(ids))
			}
			for index := range ids {
				if ids[index] == nil || amounts[index] == nil || ids[index].BitLen() > 256 || amounts[index].BitLen() > 256 {
					t.Fatal("decoder returned out-of-range values")
				}
			}
		}
	})
}

func FuzzEncodeERC1155Methods(fuzzer *testing.F) {
	fuzzer.Add([]byte{1}, []byte{2}, []byte{3}, []byte{4})
	fuzzer.Fuzz(func(t *testing.T, fromBytes, toBytes, idBytes, amountBytes []byte) {
		if len(fromBytes) != 20 || len(toBytes) != 20 {
			return
		}
		from := common.BytesToAddress(fromBytes)
		to := common.BytesToAddress(toBytes)
		if len(idBytes) > 32 || len(amountBytes) > 32 {
			return
		}
		id := new(big.Int).SetBytes(idBytes)
		amount := new(big.Int).SetBytes(amountBytes)
		single := encodeERC1155SafeTransferMethod(from, to, id, amount)
		if len(single) != 196 {
			t.Fatalf("single calldata length %d", len(single))
		}
		batch := encodeERC1155BatchTransferMethod(from, to, []EffectEntry{{TokenID: id, Amount: amount}})
		if len(batch) != 324 {
			t.Fatalf("batch calldata length %d", len(batch))
		}
	})
}
