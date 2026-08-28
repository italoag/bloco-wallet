package evm_test

import (
	"context"
	"encoding/hex"
	"errors"
	"math/big"
	"strings"
	"testing"
	"time"

	"blocowallet/internal/evm"
	"blocowallet/internal/wallet"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

type signerFunc func(context.Context, wallet.CapabilityHandle, wallet.SoftwareSigningRequest) (wallet.SoftwareSigningResult, error)

func (function signerFunc) Sign(ctx context.Context, handle wallet.CapabilityHandle, request wallet.SoftwareSigningRequest) (wallet.SoftwareSigningResult, error) {
	return function(ctx, handle, request)
}

type vectorSigner struct{}

func (vectorSigner) Sign(_ context.Context, _ wallet.CapabilityHandle, request wallet.SoftwareSigningRequest) (wallet.SoftwareSigningResult, error) {
	privateKey, err := crypto.HexToECDSA("4646464646464646464646464646464646464646464646464646464646464646")
	if err != nil {
		return wallet.SoftwareSigningResult{}, err
	}
	signature, err := crypto.Sign(request.Digest[:], privateKey)
	if err != nil {
		return wallet.SoftwareSigningResult{}, err
	}
	return wallet.SoftwareSigningResult{
		AccountID: request.AccountID, Purpose: request.Purpose, ChainID: request.ChainID,
		Digest: request.Digest, Signature: signature,
	}, nil
}

func TestSigningAdapterRejectsUnboundAndInvalidSignatures(t *testing.T) {
	plan, approval := signingPlanAndApproval(t)
	if _, err := evm.NewSigningAdapter(nil).Sign(context.Background(), wallet.CapabilityHandle{}, plan, approval); !evm.IsErrorCode(err, evm.ErrorSigningFailed) {
		t.Fatalf("nil signer returned %v", err)
	}
	if _, err := evm.NewSigningAdapter(vectorSigner{}).Sign(context.Background(), wallet.CapabilityHandle{}, nil, approval); !evm.IsErrorCode(err, evm.ErrorSigningFailed) {
		t.Fatalf("nil plan returned %v", err)
	}
	mismatch := approval
	mismatch.PlanHash[0] ^= 0xff
	if _, err := evm.NewSigningAdapter(vectorSigner{}).Sign(context.Background(), wallet.CapabilityHandle{}, plan, mismatch); !evm.IsErrorCode(err, evm.ErrorPolicyDenied) {
		t.Fatalf("approval mismatch returned %v", err)
	}
	remoteFailure := errors.New("signer reflected super-secret")
	failing := signerFunc(func(context.Context, wallet.CapabilityHandle, wallet.SoftwareSigningRequest) (wallet.SoftwareSigningResult, error) {
		return wallet.SoftwareSigningResult{}, remoteFailure
	})
	if _, err := evm.NewSigningAdapter(failing).Sign(context.Background(), wallet.CapabilityHandle{}, plan, approval); !errors.Is(err, remoteFailure) || strings.Contains(err.Error(), "super-secret") {
		t.Fatalf("signer failure was not safely wrapped: %v", err)
	}
	badEcho := signerFunc(func(_ context.Context, _ wallet.CapabilityHandle, request wallet.SoftwareSigningRequest) (wallet.SoftwareSigningResult, error) {
		return wallet.SoftwareSigningResult{AccountID: "wrong", Purpose: request.Purpose, ChainID: request.ChainID, Digest: request.Digest, Signature: make([]byte, 65)}, nil
	})
	if _, err := evm.NewSigningAdapter(badEcho).Sign(context.Background(), wallet.CapabilityHandle{}, plan, approval); !evm.IsErrorCode(err, evm.ErrorSigningFailed) {
		t.Fatalf("signer echo mismatch returned %v", err)
	}
	badLength := signerFunc(func(_ context.Context, _ wallet.CapabilityHandle, request wallet.SoftwareSigningRequest) (wallet.SoftwareSigningResult, error) {
		return wallet.SoftwareSigningResult{AccountID: request.AccountID, Purpose: request.Purpose, ChainID: request.ChainID, Digest: request.Digest, Signature: []byte{1}}, nil
	})
	if _, err := evm.NewSigningAdapter(badLength).Sign(context.Background(), wallet.CapabilityHandle{}, plan, approval); !evm.IsErrorCode(err, evm.ErrorSigningFailed) {
		t.Fatalf("invalid signature length returned %v", err)
	}
	highS := signerFunc(func(_ context.Context, _ wallet.CapabilityHandle, request wallet.SoftwareSigningRequest) (wallet.SoftwareSigningResult, error) {
		signature := make([]byte, 65)
		signature[31] = 1
		new(big.Int).Add(new(big.Int).Rsh(crypto.S256().Params().N, 1), big.NewInt(1)).FillBytes(signature[32:64])
		return wallet.SoftwareSigningResult{AccountID: request.AccountID, Purpose: request.Purpose, ChainID: request.ChainID, Digest: request.Digest, Signature: signature}, nil
	})
	if _, err := evm.NewSigningAdapter(highS).Sign(context.Background(), wallet.CapabilityHandle{}, plan, approval); !evm.IsErrorCode(err, evm.ErrorSigningFailed) {
		t.Fatalf("high-S signature returned %v", err)
	}
	wrongKey := signerFunc(func(_ context.Context, _ wallet.CapabilityHandle, request wallet.SoftwareSigningRequest) (wallet.SoftwareSigningResult, error) {
		key, err := crypto.GenerateKey()
		if err != nil {
			return wallet.SoftwareSigningResult{}, err
		}
		signature, err := crypto.Sign(request.Digest[:], key)
		if err != nil {
			return wallet.SoftwareSigningResult{}, err
		}
		return wallet.SoftwareSigningResult{AccountID: request.AccountID, Purpose: request.Purpose, ChainID: request.ChainID, Digest: request.Digest, Signature: signature}, nil
	})
	if _, err := evm.NewSigningAdapter(wrongKey).Sign(context.Background(), wallet.CapabilityHandle{}, plan, approval); !evm.IsErrorCode(err, evm.ErrorSigningFailed) {
		t.Fatalf("wrong signer key returned %v", err)
	}
}

func signingPlanAndApproval(t *testing.T) (*evm.FrozenPlan, evm.SigningApproval) {
	t.Helper()
	privateKey, err := crypto.HexToECDSA("4646464646464646464646464646464646464646464646464646464646464646")
	if err != nil {
		t.Fatal(err)
	}
	from := crypto.PubkeyToAddress(privateKey.PublicKey)
	intent, err := evm.NewNativeTransferIntent("8b9b0587-388e-4fca-bba4-bf544ebe53ca", 1, from, common.HexToAddress("0x3535353535353535353535353535353535353535"), big.NewInt(1))
	if err != nil {
		t.Fatal(err)
	}
	plan, err := evm.NewPlanner().PlanNative(intent, evm.NativePlanInput{
		ProviderBinding: evm.ProviderBinding{1}, Nonce: 1, GasLimit: 21_000, LegacyGasPrice: big.NewInt(1),
		SimulationBlockNumber: 1, SimulationBlockHash: common.HexToHash("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
	})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	return plan, evm.SigningApproval{
		ApprovalID: "51111111-1111-4111-8111-111111111111", AccountID: plan.AccountID(), Sender: from, ChainID: 1,
		PlanHash: plan.PlanHash(), TransactionDigest: plan.TransactionDigest(), CreatedAt: now, ConfirmedAt: now, ExpiresAt: now.Add(time.Minute),
	}
}

func TestSigningAdapterMatchesEIP155PayloadAndSender(t *testing.T) {
	privateKey, err := crypto.HexToECDSA("4646464646464646464646464646464646464646464646464646464646464646")
	if err != nil {
		t.Fatal(err)
	}
	from := crypto.PubkeyToAddress(privateKey.PublicKey)
	intent, err := evm.NewNativeTransferIntent(
		"8b9b0587-388e-4fca-bba4-bf544ebe53ca", 1, from,
		common.HexToAddress("0x3535353535353535353535353535353535353535"),
		big.NewInt(1_000_000_000_000_000_000),
	)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := evm.NewPlanner().PlanNative(intent, evm.NativePlanInput{
		ProviderBinding: evm.ProviderBinding{1}, Nonce: 9, GasLimit: 21_000,
		LegacyGasPrice: big.NewInt(20_000_000_000), SimulationBlockNumber: 1,
		SimulationBlockHash: common.HexToHash("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
	})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	approval := evm.SigningApproval{
		ApprovalID: "51111111-1111-4111-8111-111111111111", AccountID: plan.AccountID(), Sender: from,
		ChainID: 1, PlanHash: plan.PlanHash(), TransactionDigest: plan.TransactionDigest(),
		CreatedAt: now, ConfirmedAt: now, ExpiresAt: now.Add(time.Minute),
	}
	signed, err := evm.NewSigningAdapter(vectorSigner{}).Sign(context.Background(), wallet.CapabilityHandle{}, plan, approval)
	if err != nil {
		t.Fatal(err)
	}
	want := "f86c098504a817c800825208943535353535353535353535353535353535353535880de0b6b3a76400008025a028ef61340bd939bc2195fe537567866003e1a15d3c71ff63e1590620aa636276a067cbe9d8997f761aecb703304b3800ccf555c9f3dc64214b297fb1966a3b6d83"
	if hex.EncodeToString(signed.Raw()) != want {
		t.Fatalf("unexpected signed payload: %x", signed.Raw())
	}
	if signed.Sender() != from || signed.Hash() != crypto.Keccak256Hash(signed.Raw()) {
		t.Fatalf("signed transaction identity mismatch: sender=%s hash=%s", signed.Sender(), signed.Hash())
	}
	if !strings.Contains(signed.String(), signed.Hash().Hex()) {
		t.Fatalf("signed transaction string omitted hash: %s", signed.String())
	}
}
