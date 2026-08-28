package evm

import (
	"bytes"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

func FuzzEncodeERC721Calldata(fuzzer *testing.F) {
	fuzzer.Add([]byte{0x11, 0x22, 0x33, 0x44}, []byte{0x55, 0x66, 0x77, 0x88}, []byte{0x2a})
	fuzzer.Add([]byte{0}, []byte{0}, []byte{})
	fuzzer.Fuzz(func(t *testing.T, fromBytes, toBytes, tokenIDBytes []byte) {
		if len(fromBytes) != 20 || len(toBytes) != 20 || len(tokenIDBytes) > 32 {
			return
		}
		from := common.BytesToAddress(fromBytes)
		to := common.BytesToAddress(toBytes)
		tokenID := new(big.Int).SetBytes(tokenIDBytes)
		encoded := encodeERC721SafeTransferMethod(from, to, tokenID)
		if len(encoded) != 100 {
			t.Fatalf("ERC-721 calldata length is %d", len(encoded))
		}
		if !bytes.Equal(encoded[:4], common.FromHex("0x42842e0e")) {
			t.Fatalf("ERC-721 selector is wrong: %x", encoded[:4])
		}
		if !bytes.Equal(encoded[4:36], common.LeftPadBytes(from.Bytes(), 32)) || !bytes.Equal(encoded[36:68], common.LeftPadBytes(to.Bytes(), 32)) || !bytes.Equal(encoded[68:100], common.LeftPadBytes(tokenID.Bytes(), 32)) {
			t.Fatalf("ERC-721 calldata arguments are not canonical: %x", encoded)
		}
	})
}

func FuzzERC721EffectMatcher(fuzzer *testing.F) {
	fuzzer.Add([]byte{0x11}, []byte{0x22}, []byte{0x33}, []byte{0x2a}, uint8(4), uint8(1))
	fuzzer.Fuzz(func(t *testing.T, fromBytes, toBytes, contractBytes, tokenIDBytes []byte, topicCount, dataLength uint8) {
		if len(fromBytes) != 20 || len(toBytes) != 20 || len(contractBytes) != 20 {
			return
		}
		effect := &TransferEffect{
			Kind: EffectERC721Transfer, AssetContract: common.BytesToAddress(contractBytes),
			From: common.BytesToAddress(fromBytes), To: common.BytesToAddress(toBytes),
			TokenID: new(big.Int).SetBytes(tokenIDBytes),
		}
		topics := make([]common.Hash, 0, topicCount)
		for index := uint8(0); index < topicCount; index++ {
			topics = append(topics, common.HexToHash("0x01"))
		}
		log := ReceiptLog{Address: common.BytesToAddress(contractBytes), Topics: topics, Data: make([]byte, int(dataLength)%128)}
		_ = effect.Matches(log)
	})
}
