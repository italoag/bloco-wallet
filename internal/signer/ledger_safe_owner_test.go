package signer

import (
	"context"
	"math/big"
	"os"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// TestLedgerSignsSafeOwnerTypedData signs the EIP-712 SafeTx payload with
// the Speculos Ethereum app and verifies the signature recovers the device
// owner over the Safe transaction digest (the exact check Safe performs).
func TestLedgerSignsSafeOwnerTypedData(t *testing.T) {
	speculosURL := os.Getenv("BLOCO_WALLET_SPECULOS_URL")
	if speculosURL == "" {
		t.Skip("BLOCO_WALLET_SPECULOS_URL not set; skipping Speculos integration")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := ensureSpeculosBlindSigning(ctx, speculosURL); err != nil {
		t.Fatal(err)
	}
	transport, err := NewSpeculosTransport(speculosURL, testGatewayForServer(t, speculosURL))
	if err != nil {
		t.Fatal(err)
	}
	device, err := NewLedgerDevice(transport)
	if err != nil {
		t.Fatal(err)
	}
	result, err := device.GetPublicKey(ctx, "m/44'/60'/0'/0/0")
	if err != nil {
		t.Fatal(err)
	}
	safe := common.HexToAddress("0x3C44CdDdB6a900fa2b585dd299e03d12FA4293BC")
	transaction := SafeTransaction{
		To:    common.HexToAddress("0x70997970C51812dc3A010C7d01b50e0d17dc79C8"),
		Value: big.NewInt(12345), Data: nil, Operation: 0,
		SafeTxGas: big.NewInt(0), BaseGas: big.NewInt(0), GasPrice: big.NewInt(0),
		GasToken: common.Address{}, RefundReceiver: common.Address{}, Nonce: big.NewInt(0),
	}
	typed, err := EncodeSafeTransactionEIP712(safe, 31337, transaction)
	if err != nil {
		t.Fatal(err)
	}
	signature, err := runSpeculosSigning(ctx, speculosURL, func(signContext context.Context) ([]byte, error) {
		return device.SignTypedMessage(signContext, "m/44'/60'/0'/0/0", typed.DomainSeparatorHash, typed.MessageHash)
	})
	if err != nil {
		t.Fatal(err)
	}
	publicKey, err := crypto.SigToPub(typed.Digest[:], signature)
	if err != nil {
		t.Fatal(err)
	}
	if crypto.PubkeyToAddress(*publicKey) != result.Address {
		t.Fatalf("Safe owner signature recovered %s, want %s", crypto.PubkeyToAddress(*publicKey), result.Address)
	}
	if err := verifyECDSASignature(result.Address, typed.Digest, signature); err != nil {
		t.Fatalf("Safe owner signature does not verify: %v", err)
	}
}
