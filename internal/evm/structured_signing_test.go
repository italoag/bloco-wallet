package evm_test

import (
	"context"
	"math/big"
	"testing"

	"blocowallet/internal/evm"
	"blocowallet/internal/wallet"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

type captureDigestSigner struct {
	request wallet.SoftwareSigningRequest
	calls   int
}

func (signer *captureDigestSigner) Sign(_ context.Context, _ wallet.CapabilityHandle, request wallet.SoftwareSigningRequest) (wallet.SoftwareSigningResult, error) {
	signer.calls++
	signer.request = request
	return wallet.SoftwareSigningResult{
		AccountID: request.AccountID, Purpose: request.Purpose, MessageScheme: request.MessageScheme,
		ChainID: request.ChainID, Digest: request.Digest, IntentHash: request.IntentHash,
		Signature: make([]byte, 65),
	}, nil
}

func TestTransactionSigningIntentValidatesFrozenPayload(t *testing.T) {
	chainID := big.NewInt(1)
	transaction := types.NewTx(&types.LegacyTx{
		Nonce: 1, GasPrice: big.NewInt(2), Gas: 21_000,
		To:    addressPointerForStructuredTest(common.HexToAddress("0x1111111111111111111111111111111111111111")),
		Value: big.NewInt(3),
	})
	encoded, err := transaction.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	digest := types.NewEIP155Signer(chainID).Hash(transaction)
	intent := evm.TransactionSigningIntent{
		AccountID: "11111111-1111-4111-8111-111111111111",
		From:      common.HexToAddress("0x2222222222222222222222222222222222222222"), ChainID: 1,
		Digest: digest, PlanHash: [32]byte{1}, ApprovalID: "21111111-1111-4111-8111-111111111111",
		UnsignedTransaction: encoded,
	}
	if err := intent.Validate(); err != nil {
		t.Fatal(err)
	}
	reconstructed, err := intent.Transaction()
	if err != nil {
		t.Fatal(err)
	}
	if reconstructed.Hash() != transaction.Hash() || reconstructed.Nonce() != 1 {
		t.Fatal("unsigned transaction did not round-trip")
	}
	mutated := intent
	mutated.Digest[0] ^= 1
	if err := mutated.Validate(); err == nil {
		t.Fatal("mismatched transaction digest was accepted")
	}
}

func TestDigestSignerAdapterPreservesStructuredBindings(t *testing.T) {
	legacy := &captureDigestSigner{}
	adapter, err := evm.NewDigestSignerAdapter(legacy)
	if err != nil {
		t.Fatal(err)
	}
	message := []byte("bloco structured message")
	personalSigner := common.HexToAddress("0x1111111111111111111111111111111111111111")
	preparedPersonal, err := evm.PreparePersonalSign(evm.PreparePersonalSignRequest{
		AccountID: "11111111-1111-4111-8111-111111111111", Signer: personalSigner,
		Message: message, Origin: "local-user",
	})
	if err != nil {
		t.Fatal(err)
	}
	personalPreview := preparedPersonal.Preview()
	personalIntent := evm.PersonalMessageSigningIntent{
		AccountID: personalPreview.AccountID, Signer: personalPreview.Signer,
		Message: message, Origin: personalPreview.Origin, Digest: personalPreview.Digest,
		IntentHash: personalPreview.IntentHash, ApprovalID: "21111111-1111-4111-8111-111111111111",
	}
	if _, err := adapter.SignPersonalMessage(context.Background(), wallet.CapabilityHandle{}, personalIntent); err != nil {
		t.Fatal(err)
	}
	if legacy.calls != 1 || legacy.request.MessageScheme != wallet.MessageSigningEIP191Personal || legacy.request.ChainID != 0 || legacy.request.Digest != personalPreview.Digest {
		t.Fatalf("personal intent binding changed: %+v", legacy.request)
	}

	preparedTyped, err := evm.PrepareEIP712Sign(evm.PrepareEIP712SignRequest{
		AccountID: "11111111-1111-4111-8111-111111111111", Signer: personalSigner,
		ChainID: 1, TypedData: []byte(eip712MailFixture), Origin: "local-user",
	})
	if err != nil {
		t.Fatal(err)
	}
	typedPreview := preparedTyped.Preview()
	typedIntent := evm.EIP712SigningIntent{
		AccountID: typedPreview.AccountID, Signer: typedPreview.Signer, ChainID: 1, Origin: "local-user",
		CanonicalJSON: typedPreview.CanonicalJSON, DomainSeparatorHash: typedPreview.DomainSeparatorHash,
		MessageHash: typedPreview.MessageHash, Digest: typedPreview.Digest,
		IntentHash: typedPreview.IntentHash, ApprovalID: "31111111-1111-4111-8111-111111111111",
	}
	if _, err := adapter.SignEIP712(context.Background(), wallet.CapabilityHandle{}, typedIntent); err != nil {
		t.Fatal(err)
	}
	if legacy.calls != 2 || legacy.request.MessageScheme != wallet.MessageSigningEIP712 || legacy.request.ChainID != 1 || legacy.request.Digest != typedPreview.Digest {
		t.Fatalf("typed intent binding changed: %+v", legacy.request)
	}
	tampered := typedIntent
	tampered.MessageHash[0] ^= 1
	if _, err := adapter.SignEIP712(context.Background(), wallet.CapabilityHandle{}, tampered); err == nil {
		t.Fatal("tampered typed-data hashes reached the digest signer")
	}
	if legacy.calls != 2 {
		t.Fatal("invalid structured intent reached the digest signer")
	}
}

func addressPointerForStructuredTest(address common.Address) *common.Address {
	return &address
}
