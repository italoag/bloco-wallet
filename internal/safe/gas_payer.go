package safe

import (
	"context"
	"fmt"
	"math/big"

	"blocowallet/internal/evm"
	"blocowallet/internal/wallet"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

// GasPayerSignerAdapter signs the execTransaction envelope with an app
// account through the existing structured signing path.
type GasPayerSignerAdapter struct {
	signer evm.StructuredSigner
}

// NewGasPayerSignerAdapter wraps the structured signer used by the engine.
func NewGasPayerSignerAdapter(signer evm.StructuredSigner) (*GasPayerSignerAdapter, error) {
	if signer == nil {
		return nil, fmt.Errorf("safe gas payer: signer required")
	}
	return &GasPayerSignerAdapter{signer: signer}, nil
}

// SignGasPayer builds and signs the legacy execution envelope. The digest is
// the transaction hash itself, so the signed payload binds the Safe
// execution to the gas payer and chain.
func (adapter *GasPayerSignerAdapter) SignGasPayer(ctx context.Context, handle wallet.CapabilityHandle, request GasPayerSignRequest) ([]byte, error) {
	if request.AccountID == "" || request.From == (common.Address{}) || request.ChainID == 0 || request.To == (common.Address{}) || len(request.Data) == 0 {
		return nil, fmt.Errorf("safe gas payer: incomplete execution envelope")
	}
	transaction := types.NewTx(&types.LegacyTx{
		Nonce: request.Nonce, GasPrice: new(big.Int).Set(request.GasPrice), Gas: request.GasLimit,
		To: &request.To, Value: big.NewInt(0), Data: append([]byte(nil), request.Data...),
	})
	signer := types.NewEIP155Signer(new(big.Int).SetUint64(request.ChainID))
	digest := signer.Hash(transaction)
	unsigned, err := transaction.MarshalBinary()
	if err != nil {
		return nil, err
	}
	result, err := adapter.signer.SignTransaction(ctx, handle, evm.TransactionSigningIntent{
		AccountID: request.AccountID, From: request.From, ChainID: request.ChainID,
		Digest: digest, PlanHash: [32]byte{1}, ApprovalID: "00000000-0000-4000-8000-000000000000",
		UnsignedTransaction: unsigned,
	})
	if err != nil {
		return nil, err
	}
	signed, err := transaction.WithSignature(signer, result.Signature)
	if err != nil {
		return nil, fmt.Errorf("safe gas payer: apply signature: %w", err)
	}
	raw, err := signed.MarshalBinary()
	if err != nil {
		return nil, err
	}
	if crypto.Keccak256Hash(raw) != signed.Hash() {
		return nil, fmt.Errorf("safe gas payer: signed payload hash mismatch")
	}
	return raw, nil
}
