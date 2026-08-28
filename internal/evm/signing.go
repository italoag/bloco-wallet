package evm

import (
	"context"
	"fmt"
	"math/big"

	"blocowallet/internal/wallet"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

type ApprovedDigestSigner interface {
	Sign(context.Context, wallet.CapabilityHandle, wallet.SoftwareSigningRequest) (wallet.SoftwareSigningResult, error)
}

type SigningAdapter struct {
	signer ApprovedDigestSigner
}

type SignedTransaction struct {
	raw    []byte
	hash   common.Hash
	sender common.Address
}

func NewSigningAdapter(signer ApprovedDigestSigner) *SigningAdapter {
	return &SigningAdapter{signer: signer}
}

func (adapter *SigningAdapter) Sign(ctx context.Context, handle wallet.CapabilityHandle, plan *FrozenPlan, approval SigningApproval) (SignedTransaction, error) {
	if adapter == nil || adapter.signer == nil {
		return SignedTransaction{}, &EngineError{Code: ErrorSigningFailed, Field: "signer"}
	}
	if plan == nil || plan.transaction == nil || plan.chainID == nil || !plan.chainID.IsUint64() {
		return SignedTransaction{}, &EngineError{Code: ErrorSigningFailed, Field: "frozen plan"}
	}
	if approval.ApprovalID == "" || approval.AccountID != plan.accountID || approval.Sender != plan.from || approval.ChainID != plan.chainID.Uint64() || approval.PlanHash != plan.planHash || approval.TransactionDigest != plan.transactionDigest {
		return SignedTransaction{}, &EngineError{Code: ErrorPolicyDenied, Field: "approval binding"}
	}
	request := wallet.SoftwareSigningRequest{
		AccountID:  plan.accountID,
		Purpose:    wallet.SigningPurposeTransaction,
		ChainID:    plan.chainID.Uint64(),
		Digest:     plan.transactionDigest,
		ApprovalID: approval.ApprovalID,
	}
	result, err := adapter.signer.Sign(ctx, handle, request)
	if err != nil {
		return SignedTransaction{}, &EngineError{Code: ErrorSigningFailed, Field: "software signer", Cause: err}
	}
	if result.AccountID != request.AccountID || result.Purpose != request.Purpose || result.ChainID != request.ChainID || result.Digest != request.Digest {
		return SignedTransaction{}, &EngineError{Code: ErrorSigningFailed, Field: "signer result binding"}
	}
	if err := validateTransactionSignature(result.Signature, plan.from, plan.transactionDigest); err != nil {
		return SignedTransaction{}, err
	}
	transaction := plan.Transaction()
	if transaction == nil {
		return SignedTransaction{}, &EngineError{Code: ErrorSigningFailed, Field: "transaction clone"}
	}
	transactionSigner, err := signerForTransaction(transaction, plan.chainID)
	if err != nil {
		return SignedTransaction{}, err
	}
	signed, err := transaction.WithSignature(transactionSigner, result.Signature)
	if err != nil {
		return SignedTransaction{}, &EngineError{Code: ErrorSigningFailed, Field: "apply signature", Cause: err}
	}
	sender, err := types.Sender(transactionSigner, signed)
	if err != nil || sender != plan.from {
		return SignedTransaction{}, &EngineError{Code: ErrorSigningFailed, Field: "recovered sender", Cause: err}
	}
	raw, err := signed.MarshalBinary()
	if err != nil {
		return SignedTransaction{}, &EngineError{Code: ErrorSigningFailed, Field: "signed payload", Cause: err}
	}
	hash := crypto.Keccak256Hash(raw)
	if hash != signed.Hash() {
		return SignedTransaction{}, &EngineError{Code: ErrorSigningFailed, Field: "transaction hash"}
	}
	return SignedTransaction{raw: append([]byte(nil), raw...), hash: hash, sender: sender}, nil
}

func signerForTransaction(transaction *types.Transaction, chainID *big.Int) (types.Signer, error) {
	switch transaction.Type() {
	case types.LegacyTxType:
		return types.NewEIP155Signer(new(big.Int).Set(chainID)), nil
	case types.DynamicFeeTxType:
		return types.NewLondonSigner(new(big.Int).Set(chainID)), nil
	default:
		return nil, &EngineError{Code: ErrorSigningFailed, Field: "transaction type"}
	}
}

func validateTransactionSignature(signature []byte, expected common.Address, digest [32]byte) error {
	if len(signature) != crypto.SignatureLength || signature[crypto.RecoveryIDOffset] > 1 {
		return &EngineError{Code: ErrorSigningFailed, Field: "signature encoding"}
	}
	r := new(big.Int).SetBytes(signature[:32])
	s := new(big.Int).SetBytes(signature[32:64])
	if !crypto.ValidateSignatureValues(signature[64], r, s, true) {
		return &EngineError{Code: ErrorSigningFailed, Field: "signature values"}
	}
	publicKey, err := crypto.SigToPub(digest[:], signature)
	if err != nil {
		return &EngineError{Code: ErrorSigningFailed, Field: "signature recovery", Cause: err}
	}
	if crypto.PubkeyToAddress(*publicKey) != expected {
		return &EngineError{Code: ErrorSigningFailed, Field: "signature account"}
	}
	return nil
}

func (transaction SignedTransaction) Raw() []byte {
	return append([]byte(nil), transaction.raw...)
}

func (transaction SignedTransaction) Hash() common.Hash { return transaction.hash }
func (transaction SignedTransaction) Sender() common.Address {
	return transaction.sender
}

func (transaction SignedTransaction) String() string {
	return fmt.Sprintf("signed EVM transaction %s", transaction.hash.Hex())
}
