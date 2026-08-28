package evm

import (
	"testing"
	"unicode/utf8"
)

func FuzzParseUnits(fuzzer *testing.F) {
	fuzzer.Add("1.0", uint8(18))
	fuzzer.Add("0.000001", uint8(6))
	fuzzer.Add("1e18", uint8(18))
	fuzzer.Fuzz(func(t *testing.T, value string, decimals uint8) {
		amount, err := ParseUnits(value, decimals)
		if err == nil && (amount == nil || amount.Sign() <= 0 || amount.BitLen() > 256) {
			t.Fatalf("ParseUnits returned invalid amount: %v", amount)
		}
	})
}

func FuzzDecodeABIString(fuzzer *testing.F) {
	fuzzer.Add([]byte{})
	fuzzer.Add([]byte{0x00, 0x01})
	fuzzer.Add(make([]byte, 64))
	fuzzer.Fuzz(func(t *testing.T, data []byte) {
		value, err := decodeABIString(data, 64)
		if err == nil && (!utf8.ValidString(value) || len([]rune(value)) == 0 || len([]rune(value)) > 64) {
			t.Fatalf("decoder returned invalid UTF-8 metadata: %q", value)
		}
	})
}

func FuzzDecodeRevertError(fuzzer *testing.F) {
	fuzzer.Add([]byte{0x08, 0xc3, 0x79, 0xa0})
	fuzzer.Add([]byte{0x4e, 0x48, 0x7b, 0x71})
	fuzzer.Fuzz(func(t *testing.T, data []byte) {
		decoded := decodeRevertError(&RevertError{Data: data})
		if decoded == nil || len(decoded.Data) != len(data) {
			t.Fatal("revert decoder lost bounded input")
		}
	})
}
