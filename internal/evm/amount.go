package evm

import (
	"math/big"
	"strings"
)

func ParseUnits(value string, decimals uint8) (*big.Int, error) {
	if value == "" || value != strings.TrimSpace(value) || decimals > 36 {
		return nil, invalidIntent("amount")
	}
	parts := strings.Split(value, ".")
	if len(parts) > 2 || parts[0] == "" || !decimalDigits(parts[0]) {
		return nil, invalidIntent("amount")
	}
	fraction := ""
	if len(parts) == 2 {
		fraction = parts[1]
		if fraction == "" || !decimalDigits(fraction) || len(fraction) > int(decimals) {
			return nil, invalidIntent("amount precision")
		}
	}
	if decimals == 0 && fraction != "" {
		return nil, invalidIntent("amount precision")
	}
	fraction += strings.Repeat("0", int(decimals)-len(fraction))
	units, ok := new(big.Int).SetString(parts[0]+fraction, 10)
	if !ok || units.Sign() <= 0 || units.BitLen() > 256 {
		return nil, invalidIntent("amount")
	}
	return units, nil
}

func FormatUnits(value *big.Int, decimals uint8) string {
	if value == nil {
		return "0"
	}
	negative := value.Sign() < 0
	absolute := new(big.Int).Abs(new(big.Int).Set(value))
	digits := absolute.String()
	if decimals > 0 {
		if len(digits) <= int(decimals) {
			digits = strings.Repeat("0", int(decimals)-len(digits)+1) + digits
		}
		split := len(digits) - int(decimals)
		fraction := strings.TrimRight(digits[split:], "0")
		digits = digits[:split]
		if fraction != "" {
			digits += "." + fraction
		}
	}
	if negative {
		return "-" + digits
	}
	return digits
}

func decimalDigits(value string) bool {
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return value != ""
}
