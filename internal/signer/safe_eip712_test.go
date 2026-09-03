package signer

import (
	"encoding/json"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

func TestEncodeSafeTransactionEIP712MatchesContractDigest(t *testing.T) {
	safe := common.HexToAddress("0x9d8A62f656a8d1615C1294fd71e9CFb3E4855A4F")
	transaction := SafeTransaction{
		To:    common.HexToAddress("0x2222222222222222222222222222222222222222"),
		Value: big.NewInt(12345), Data: []byte{1, 2, 3}, Operation: 0,
		SafeTxGas: big.NewInt(0), BaseGas: big.NewInt(0), GasPrice: big.NewInt(7),
		GasToken: common.Address{}, RefundReceiver: common.Address{}, Nonce: big.NewInt(4),
	}
	expected, err := SafeTransactionDigest(safe, 31337, transaction)
	if err != nil {
		t.Fatal(err)
	}
	typed, err := EncodeSafeTransactionEIP712(safe, 31337, transaction)
	if err != nil {
		t.Fatal(err)
	}
	if typed.Digest != expected {
		t.Fatalf("EIP-712 digest %x != contract digest %x", typed.Digest, expected)
	}
	if typed.DomainSeparatorHash == ([32]byte{}) || typed.MessageHash == ([32]byte{}) || len(typed.CanonicalJSON) == 0 {
		t.Fatal("EIP-712 components are incomplete")
	}
	var decoded struct {
		PrimaryType string `json:"primaryType"`
		Domain      map[string]any
		Message     map[string]any
	}
	if err := json.Unmarshal(typed.CanonicalJSON, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.PrimaryType != "SafeTx" || decoded.Domain["verifyingContract"] != safe.Hex() {
		t.Fatalf("unexpected EIP-712 payload: %s", typed.CanonicalJSON)
	}
	if _, exists := decoded.Message["nonce"]; !exists {
		t.Fatal("EIP-712 message missing nonce")
	}
}
