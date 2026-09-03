package signer

import (
	"context"
	"math/big"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// TestTrezorSignsSafeOwnerTypedData drives the emulated Trezor (T2T1 or T1)
// to sign the EIP-712 SafeTx payload and verifies the signature recovers the
// device owner over the Safe transaction digest.
func TestTrezorSignsSafeOwnerTypedData(t *testing.T) {
	emulatorURL := os.Getenv("BLOCO_WALLET_TREZOR_EMULATOR")
	controllerURL := os.Getenv("BLOCO_WALLET_TREZOR_CONTROLLER_URL")
	if emulatorURL == "" || controllerURL == "" {
		t.Skip("Trezor emulator environment not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	var device *UDPDevice
	if strings.HasPrefix(emulatorURL, "http://") || strings.HasPrefix(emulatorURL, "https://") {
		bridgeDevice, err := NewBridgeDevice(ctx, emulatorURL, testGatewayForServer(t, emulatorURL))
		if err != nil {
			t.Fatal(err)
		}
		device = bridgeDevice.UDPDevice
	} else {
		var err error
		device, err = NewUDPDevice(ctx, emulatorURL)
		if err != nil {
			t.Fatal(err)
		}
	}
	defer func() { _ = device.Close() }()
	device.SetButtonHandler(func(handlerContext context.Context) error {
		return trezorControllerCommand(handlerContext, controllerURL, map[string]any{"type": "emulator-press-yes"})
	})
	features, err := device.Initialize(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !features.Initialized {
		t.Fatal("Trezor emulator is not initialized")
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
	path := "m/44'/60'/0'/0/0"
	var signature []byte
	if features.Model == "1" {
		signature, err = device.EthereumSignTypedHash(ctx, path, typed.DomainSeparatorHash, typed.MessageHash)
	} else {
		signature, err = device.EthereumSignTypedData(ctx, path, typed.CanonicalJSON, true)
	}
	if err != nil {
		t.Fatal(err)
	}
	signature, err = normalizeSignature(signature)
	if err != nil {
		t.Fatal(err)
	}
	publicKey, err := crypto.SigToPub(typed.Digest[:], signature)
	if err != nil {
		t.Fatal(err)
	}
	signerAddress := crypto.PubkeyToAddress(*publicKey)
	// The emulator is set up with "all all all ... all" and no passphrase;
	// account m/44'/60'/0'/0/0 is the well-known Trezor test address.
	expected := common.HexToAddress("0x73d0385F4d8E00C5e6504C6030F47BF6212736A8")
	if signerAddress != expected {
		t.Fatalf("Safe owner signature recovered %s, want %s", signerAddress, expected)
	}
	if err := verifyECDSASignature(expected, typed.Digest, signature); err != nil {
		t.Fatalf("Safe owner signature does not verify: %v", err)
	}
}
