package safe

import (
	"context"
	"math/big"
	"testing"

	"blocowallet/internal/evm"
	"blocowallet/internal/wallet"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

type digestOnlySigner struct{}

func (digestOnlySigner) Sign(_ context.Context, _ wallet.CapabilityHandle, request wallet.SoftwareSigningRequest) (wallet.SoftwareSigningResult, error) {
	key, err := crypto.HexToECDSA("4646464646464646464646464646464646464646464646464646464646464646")
	if err != nil {
		return wallet.SoftwareSigningResult{}, err
	}
	signature, err := crypto.Sign(request.Digest[:], key)
	if err != nil {
		return wallet.SoftwareSigningResult{}, err
	}
	return wallet.SoftwareSigningResult{
		AccountID: request.AccountID, Purpose: request.Purpose, MessageScheme: request.MessageScheme,
		ChainID: request.ChainID, Digest: request.Digest, IntentHash: request.IntentHash, Signature: signature,
	}, nil
}

func TestGasPayerSignerAdapterProducesRecoverableEnvelope(t *testing.T) {
	key, err := crypto.HexToECDSA("4646464646464646464646464646464646464646464646464646464646464646")
	if err != nil {
		t.Fatal(err)
	}
	structured, err := evm.NewDigestSignerAdapter(digestOnlySigner{})
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := NewGasPayerSignerAdapter(structured)
	if err != nil {
		t.Fatal(err)
	}
	owner := crypto.PubkeyToAddress(key.PublicKey)
	raw, err := adapter.SignGasPayer(context.Background(), wallet.CapabilityHandle{}, GasPayerSignRequest{
		AccountID: "21111111-1111-4111-8111-111111111111", From: owner, ChainID: 31337,
		To:   common.HexToAddress("0x3C44CdDdB6a900fa2b585dd299e03d12FA4293BC"),
		Data: []byte{1, 2, 3}, Nonce: 7, GasPrice: big.NewInt(1000000000), GasLimit: 1_500_000,
	})
	if err != nil {
		t.Fatal(err)
	}
	var transaction types.Transaction
	if err := transaction.UnmarshalBinary(raw); err != nil {
		t.Fatal(err)
	}
	transactionSigner := types.NewEIP155Signer(big.NewInt(31337))
	if transaction.ChainId().Cmp(big.NewInt(31337)) != 0 || transaction.Nonce() != 7 {
		t.Fatalf("unexpected envelope: chain=%s nonce=%d", transaction.ChainId(), transaction.Nonce())
	}
	sender, err := types.Sender(transactionSigner, &transaction)
	if err != nil {
		t.Fatal(err)
	}
	if sender != owner {
		t.Fatalf("recovered sender %s, want %s", sender, owner)
	}
}
