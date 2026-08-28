package evm_test

import (
	"math/big"
	"testing"

	"blocowallet/internal/evm"
)

func TestParseUnitsIsExactAndNeverRounds(t *testing.T) {
	tests := []struct {
		value    string
		decimals uint8
		want     *big.Int
	}{
		{"1", 18, new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)},
		{"1.5", 6, big.NewInt(1_500_000)},
		{"0.000001", 6, big.NewInt(1)},
		{"42", 0, big.NewInt(42)},
	}
	for _, test := range tests {
		got, err := evm.ParseUnits(test.value, test.decimals)
		if err != nil || got.Cmp(test.want) != 0 {
			t.Fatalf("ParseUnits(%q, %d)=%v,%v want %s", test.value, test.decimals, got, err, test.want)
		}
	}
	for _, invalid := range []string{"", "0", "-1", "+1", "1e18", "1.0000001", " 1", "1 ", "1..0", "."} {
		if _, err := evm.ParseUnits(invalid, 6); err == nil {
			t.Fatalf("invalid amount %q was accepted", invalid)
		}
	}
}
