package safe

import (
	"context"
	"fmt"
	"time"

	"blocowallet/internal/evm"
	"blocowallet/internal/signer"
	"blocowallet/internal/wallet"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// SafeMessageProposalRequest describes an off-chain message the Safe should
// sign through EIP-1271 (isValidSignature).
type SafeMessageProposalRequest struct {
	SafeAccountID string
	ChainID       uint64
	Message       []byte
}

// SafeMessage is the durable record of one EIP-1271 message signing.
type SafeMessage struct {
	MessageID   string
	SafeAddress common.Address
	AccountID   string
	ChainID     uint64
	Message     []byte
	Digest      [32]byte
	Commitment  [32]byte
	Owners      []OwnerSnapshot
	Threshold   uint64
	Signatures  []OwnerSignature
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// ProposeMessage freezes an EIP-1271 message for owner signature collection.
// No on-chain execution happens; the aggregate is verified off-chain.
func (service *Service) ProposeMessage(ctx context.Context, request SafeMessageProposalRequest) (*SafeMessage, error) {
	if request.ChainID == 0 || len(request.Message) == 0 || len(request.Message) > 64<<10 {
		return nil, fmt.Errorf("safe message: message is required")
	}
	account, err := service.accounts.GetAccount(ctx, request.SafeAccountID)
	if err != nil {
		return nil, err
	}
	if account.SignerKind != wallet.SignerKindMultisig || account.SignerReference != "safe:v1:"+account.Address {
		return nil, fmt.Errorf("safe message: account is not a Safe")
	}
	safeAddress := common.HexToAddress(account.Address)
	owners, err := service.rpc.Owners(ctx, safeAddress)
	if err != nil {
		return nil, fmt.Errorf("safe message: owners: %w", err)
	}
	threshold, err := service.rpc.Threshold(ctx, safeAddress)
	if err != nil {
		return nil, fmt.Errorf("safe message: threshold: %w", err)
	}
	if len(owners) == 0 || threshold == 0 || threshold > uint64(len(owners)) {
		return nil, fmt.Errorf("safe message: on-chain owner policy is invalid")
	}
	snapshot := make([]signer.SafeOwnerSnapshot, 0, len(owners))
	for _, owner := range owners {
		snapshot = append(snapshot, signer.SafeOwnerSnapshot{Address: owner, Kind: signer.SafeOwnerEOA})
	}
	messageHash := crypto.Keccak256Hash(request.Message)
	digest, err := signer.SafeMessageDigest(safeAddress, request.ChainID, messageHash)
	if err != nil {
		return nil, err
	}
	intent, err := signer.NewSafeMessageIntent(safeAddress, request.ChainID, snapshot, threshold, digest)
	if err != nil {
		return nil, err
	}
	messageID, err := service.newID()
	if err != nil {
		return nil, err
	}
	now := service.now().UTC()
	return &SafeMessage{
		MessageID: messageID, SafeAddress: safeAddress, AccountID: account.AccountID, ChainID: request.ChainID,
		Message: append([]byte(nil), request.Message...), Digest: digest, Commitment: intent.Commitment,
		Owners: snapshotToProposal(intent.Owners), Threshold: intent.Threshold,
		CreatedAt: now, UpdatedAt: now,
	}, nil
}

// SignMessage collects one owner EOA signature for an EIP-1271 message.
func (service *Service) SignMessage(ctx context.Context, message *SafeMessage, ownerAccountID string, authorize func(accountID string, operation func(wallet.CapabilityHandle) error) error) error {
	if message == nil || message.MessageID == "" {
		return fmt.Errorf("safe message is required")
	}
	ownerAccount, err := service.accounts.GetAccount(ctx, ownerAccountID)
	if err != nil {
		return err
	}
	ownerAddress := common.HexToAddress(ownerAccount.Address)
	found := false
	for _, owner := range message.Owners {
		if owner.Address == ownerAddress {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("safe message: account is not an owner")
	}
	for _, collected := range message.Signatures {
		if collected.Owner == ownerAddress {
			return fmt.Errorf("safe message: owner already signed")
		}
	}
	now := service.now().UTC()
	approvalID, err := service.newID()
	if err != nil {
		return err
	}
	approval := evm.MessageApproval{
		ApprovalID: approvalID, AccountID: ownerAccount.AccountID, Signer: ownerAddress,
		Scheme: wallet.MessageSigningSafeOwner, ChainID: message.ChainID,
		Digest: message.Digest, IntentHash: message.Commitment, PayloadSize: uint64(len(message.Message)),
		AuthorizationEpoch: ownerAccount.AuthorizationEpoch, ConfirmationLevel: evm.ConfirmationReinforced,
		CreatedAt: now, ConfirmedAt: now, ExpiresAt: now.Add(service.approval), State: evm.MessageApprovalPending, Revision: 1,
	}
	if err := service.messages.IssueMessageApproval(ctx, approval); err != nil {
		return err
	}
	signingID, err := service.newID()
	if err != nil {
		return err
	}
	if _, err := service.messages.AuthorizeMessageSigning(ctx, evm.AuthorizeMessageSigningRequest{
		SigningID: signingID, ApprovalID: approvalID, AccountID: ownerAccount.AccountID, Signer: ownerAddress,
		Scheme: wallet.MessageSigningSafeOwner, ChainID: message.ChainID, Digest: message.Digest,
		IntentHash: message.Commitment, AuthorizationEpoch: ownerAccount.AuthorizationEpoch, AuthorizedAt: now,
	}); err != nil {
		return err
	}
	var result wallet.SoftwareSigningResult
	signingErr := authorize(ownerAccount.AccountID, func(handle wallet.CapabilityHandle) error {
		signed, signErr := service.signer.SignSafeOwnerDigest(ctx, handle, evm.SafeOwnerDigestRequest{
			AccountID: ownerAccount.AccountID, Signer: ownerAddress, ChainID: message.ChainID,
			Digest: message.Digest, IntentHash: message.Commitment, ApprovalID: approvalID,
		})
		if signErr == nil {
			result = signed
		}
		return signErr
	})
	if signingErr != nil {
		_ = service.messages.FailMessageSigning(context.Background(), evm.FailMessageSigningRequest{
			SigningID: signingID, ResultCode: "signer_rejected", CompletedAt: service.now().UTC(),
		})
		return signingErr
	}
	signature := append([]byte(nil), result.Signature...)
	signature[crypto.RecoveryIDOffset] += 27
	signatureHash := crypto.Keccak256Hash(signature)
	if err := service.messages.CompleteMessageSigning(ctx, evm.CompleteMessageSigningRequest{
		SigningID: signingID, SignatureHash: signatureHash, CompletedAt: service.now().UTC(),
	}); err != nil {
		return err
	}
	normalized := append([]byte(nil), signature...)
	switch normalized[64] {
	case 27, 28:
		normalized[64] -= 27
	}
	publicKey, err := crypto.SigToPub(message.Digest[:], normalized)
	if err != nil {
		return fmt.Errorf("safe message: owner signature recovery: %w", err)
	}
	if crypto.PubkeyToAddress(*publicKey) != ownerAddress {
		return fmt.Errorf("safe message: owner signature mismatch")
	}
	normalized[64] += 27
	message.Signatures = append(message.Signatures, OwnerSignature{Owner: ownerAddress, Kind: uint8(signer.SafeOwnerEOA), Signature: normalized})
	message.UpdatedAt = service.now().UTC()
	return nil
}

// VerifyMessage aggregates the collected owner signatures and returns the
// EIP-1271 payload (nil until the threshold is reached).
func (service *Service) VerifyMessage(message *SafeMessage) ([]byte, error) {
	if message == nil || message.MessageID == "" {
		return nil, fmt.Errorf("safe message is required")
	}
	intent, err := signer.NewSafeMessageIntent(message.SafeAddress, message.ChainID,
		proposalSnapshot(message.Owners), message.Threshold, message.Digest)
	if err != nil {
		return nil, err
	}
	coordinator, err := signer.NewSafeMessageCoordinator(intent)
	if err != nil {
		return nil, err
	}
	for _, collected := range message.Signatures {
		if collected.Kind == uint8(signer.SafeOwnerEOA) {
			if err := coordinator.AddEOASignature(collected.Owner, collected.Signature); err != nil {
				return nil, err
			}
		}
	}
	return coordinator.Signatures()
}

func proposalSnapshot(owners []OwnerSnapshot) []signer.SafeOwnerSnapshot {
	snapshot := make([]signer.SafeOwnerSnapshot, 0, len(owners))
	for _, owner := range owners {
		snapshot = append(snapshot, signer.SafeOwnerSnapshot{Address: owner.Address, Kind: signer.SafeOwnerKind(owner.Kind)})
	}
	return snapshot
}
