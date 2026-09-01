package wallet_test

import (
	"context"
	"testing"

	"blocowallet/internal/wallet"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

func TestImportHardwareSignerAccountBindsDevicePublicKey(t *testing.T) {
	privateKey, err := crypto.HexToECDSA("4646464646464646464646464646464646464646464646464646464646464646")
	if err != nil {
		t.Fatal(err)
	}
	address := crypto.PubkeyToAddress(privateKey.PublicKey)
	for _, deviceKind := range []string{"ledger", "trezor"} {
		t.Run(deviceKind, func(t *testing.T) {
			repository := &importAccountRepository{}
			account, err := wallet.ImportHardwareSignerAccount(context.Background(), repository, wallet.HardwareSignerImportRequest{
				Name: "Hardware wallet", DeviceKind: deviceKind,
				DerivationPath: "m/44'/60'/0'/0/0",
				PublicKey:      crypto.CompressPubkey(&privateKey.PublicKey), ExpectedAddress: address,
			})
			if err != nil {
				t.Fatal(err)
			}
			if account != repository.created || account.SignerKind != wallet.SignerKindHardware || account.Address != address.Hex() || account.SignerReference != deviceKind+":v1:m/44'/60'/0'/0/0" {
				t.Fatalf("unexpected hardware account: %+v", account)
			}
			if account.Capabilities != wallet.CapabilitySignTransaction|wallet.CapabilitySignMessage || account.Capabilities&wallet.CapabilityExportSecret != 0 || len(account.SecretEnvelope) != 0 {
				t.Fatalf("unsafe hardware capabilities: %b", account.Capabilities)
			}
		})
	}
}

func TestImportHardwareSignerAccountRejectsUnboundIdentity(t *testing.T) {
	privateKey, err := crypto.HexToECDSA("4646464646464646464646464646464646464646464646464646464646464646")
	if err != nil {
		t.Fatal(err)
	}
	base := wallet.HardwareSignerImportRequest{
		Name: "Hardware wallet", DeviceKind: "ledger", DerivationPath: "m/44'/60'/0'/0/0",
		PublicKey: crypto.CompressPubkey(&privateKey.PublicKey), ExpectedAddress: crypto.PubkeyToAddress(privateKey.PublicKey),
	}
	tests := []struct {
		name   string
		mutate func(*wallet.HardwareSignerImportRequest)
	}{
		{name: "foreign address", mutate: func(request *wallet.HardwareSignerImportRequest) {
			request.ExpectedAddress = common.HexToAddress("0x1111111111111111111111111111111111111111")
		}},
		{name: "bad path", mutate: func(request *wallet.HardwareSignerImportRequest) { request.DerivationPath = "m/not-a-path" }},
		{name: "unknown device", mutate: func(request *wallet.HardwareSignerImportRequest) { request.DeviceKind = "unknown" }},
		{name: "bad public key", mutate: func(request *wallet.HardwareSignerImportRequest) { request.PublicKey = []byte{1, 2, 3} }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := base
			request.PublicKey = append([]byte(nil), base.PublicKey...)
			test.mutate(&request)
			if _, err := wallet.ImportHardwareSignerAccount(context.Background(), &importAccountRepository{}, request); err == nil {
				t.Fatal("unbound hardware identity was accepted")
			}
		})
	}
}
