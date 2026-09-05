package safe

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"time"

	"blocowallet/internal/evm"
	"blocowallet/internal/signer"
	"blocowallet/internal/wallet"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// ErrNotFound is returned when a proposal or Safe account does not exist.
var ErrNotFound = errors.New("safe: not found")

// SafeImportRequest describes importing an existing Safe deployment.
type SafeImportRequest struct {
	Name         string
	Address      string
	ChainID      uint64
	Capabilities wallet.AccountCapability
}

// SafeAccount is the multisig account record returned after import.
type SafeAccount struct {
	AccountID string
	Address   common.Address
	ChainID   uint64
	Owners    []common.Address
	Threshold uint64
}

// SafeProposalRequest describes a new transaction proposal for one Safe.
type SafeProposalRequest struct {
	SafeAccountID string
	ChainID       uint64
	To            common.Address
	Value         *big.Int
	Data          []byte
}

// SafeSignRequest approves one proposal as one EOA owner.
type SafeSignRequest struct {
	ProposalID     string
	OwnerAccountID string
	ChainID        uint64
}

// AccountRepository is the subset of the wallet account store the Safe
// service needs.
type AccountRepository interface {
	GetAccount(context.Context, string) (*wallet.Account, error)
	FindAccountsByAddress(context.Context, string) ([]wallet.Account, error)
}

// AccountImporter creates or reuses the custody-free Safe account.
type AccountImporter interface {
	ImportSafeAccount(context.Context, wallet.ExternalSignerImportRequest) (*wallet.Account, error)
}

// MessageSigningRepository drives the durable message-approval lifecycle.
type MessageSigningRepository interface {
	IssueMessageApproval(context.Context, evm.MessageApproval) error
	AuthorizeMessageSigning(context.Context, evm.AuthorizeMessageSigningRequest) (evm.MessageSigningRecord, error)
	CompleteMessageSigning(context.Context, evm.CompleteMessageSigningRequest) error
	FailMessageSigning(context.Context, evm.FailMessageSigningRequest) error
}

// OwnerDigestSigner signs a raw Safe digest as an approved EOA owner.
type OwnerDigestSigner interface {
	SignSafeOwnerDigest(context.Context, wallet.CapabilityHandle, evm.SafeOwnerDigestRequest) (wallet.SoftwareSigningResult, error)
}

// GasPayerSignRequest is the unsigned execTransaction envelope a gas payer
// account must sign before broadcast.
type GasPayerSignRequest struct {
	AccountID string
	From      common.Address
	ChainID   uint64
	To        common.Address
	Data      []byte
	Nonce     uint64
	GasPrice  *big.Int
	GasLimit  uint64
}

// GasPayerSigner signs the execution envelope as the gas payer.
type GasPayerSigner interface {
	SignGasPayer(context.Context, wallet.CapabilityHandle, GasPayerSignRequest) ([]byte, error)
}

// Options configures proposal identity and expiry.
type Options struct {
	Now         func() time.Time
	NewID       func() (string, error)
	ApprovalTTL time.Duration
}

// Service orchestrates Safe imports, proposals, owner approvals, and
// execution on top of the existing approval and signing infrastructure.
type Service struct {
	proposals ProposalRepository
	accounts  AccountRepository
	importer  AccountImporter
	messages  MessageSigningRepository
	signer    OwnerDigestSigner
	gasPayer  GasPayerSigner
	rpc       OnChainReader
	now       func() time.Time
	newID     func() (string, error)
	approval  time.Duration
}

// New creates the Safe service.
func New(proposals ProposalRepository, accounts AccountRepository, importer AccountImporter, messages MessageSigningRepository, signer OwnerDigestSigner, rpc OnChainReader, options Options) (*Service, error) {
	return NewWithGasPayer(proposals, accounts, importer, messages, signer, nil, rpc, options)
}

// NewWithGasPayer creates the Safe service with a gas-payer signer used to
// authorize execTransaction broadcasts.
func NewWithGasPayer(proposals ProposalRepository, accounts AccountRepository, importer AccountImporter, messages MessageSigningRepository, signer OwnerDigestSigner, gasPayer GasPayerSigner, rpc OnChainReader, options Options) (*Service, error) {
	if proposals == nil || accounts == nil || importer == nil || messages == nil || signer == nil || rpc == nil {
		return nil, fmt.Errorf("safe service dependencies are required")
	}
	service := &Service{
		proposals: proposals, accounts: accounts, importer: importer,
		messages: messages, signer: signer, gasPayer: gasPayer, rpc: rpc,
		now: options.Now, newID: options.NewID, approval: options.ApprovalTTL,
	}
	if service.now == nil {
		service.now = time.Now
	}
	if service.newID == nil {
		service.newID = randomID
	}
	if service.approval <= 0 {
		service.approval = 15 * time.Minute
	}
	return service, nil
}

// ImportSafe loads the on-chain owner set and threshold, then creates or
// reuses the custody-free multisig account.
func (service *Service) ImportSafe(ctx context.Context, request SafeImportRequest) (*SafeAccount, error) {
	if request.Name == "" || len(request.Name) > 64 {
		return nil, fmt.Errorf("safe account name is required")
	}
	if !common.IsHexAddress(request.Address) || common.HexToAddress(request.Address).Hex() != request.Address {
		return nil, fmt.Errorf("safe address must be checksummed")
	}
	safeAddress := common.HexToAddress(request.Address)
	if safeAddress == (common.Address{}) || request.ChainID == 0 {
		return nil, fmt.Errorf("safe address and chain are required")
	}
	code, err := service.rpc.CodeAt(ctx, safeAddress)
	if err != nil {
		return nil, fmt.Errorf("safe import: code: %w", err)
	}
	if len(code) == 0 {
		return nil, fmt.Errorf("safe import: no contract deployed at address")
	}
	owners, err := service.rpc.Owners(ctx, safeAddress)
	if err != nil {
		return nil, fmt.Errorf("safe import: owners: %w", err)
	}
	threshold, err := service.rpc.Threshold(ctx, safeAddress)
	if err != nil {
		return nil, fmt.Errorf("safe import: threshold: %w", err)
	}
	if len(owners) == 0 || threshold == 0 || threshold > uint64(len(owners)) {
		return nil, fmt.Errorf("safe import: on-chain owner policy is invalid")
	}
	existing, err := service.accounts.FindAccountsByAddress(ctx, safeAddress.Hex())
	if err != nil {
		return nil, err
	}
	for _, account := range existing {
		if account.SignerKind == wallet.SignerKindMultisig && account.SignerReference == "safe:v1:"+safeAddress.Hex() {
			return &SafeAccount{
				AccountID: account.AccountID, Address: safeAddress, ChainID: request.ChainID,
				Owners: owners, Threshold: threshold,
			}, nil
		}
	}
	capabilities := request.Capabilities
	if capabilities == 0 {
		capabilities = wallet.CapabilitySignTransaction | wallet.CapabilitySignMessage
	}
	account, err := service.importer.ImportSafeAccount(ctx, wallet.ExternalSignerImportRequest{
		Name: request.Name, Address: safeAddress.Hex(), SignerKind: wallet.SignerKindMultisig,
		Reference: "safe:v1:" + safeAddress.Hex(), Capabilities: capabilities, AuthorizationEpoch: 1,
	})
	if err != nil {
		return nil, err
	}
	return &SafeAccount{
		AccountID: account.AccountID, Address: safeAddress, ChainID: request.ChainID,
		Owners: owners, Threshold: threshold,
	}, nil
}

// Propose freezes a transaction with the on-chain nonce and owner snapshot
// and persists it for owner approval collection.
func (service *Service) Propose(ctx context.Context, request SafeProposalRequest) (*Proposal, error) {
	if request.ChainID == 0 || request.To == (common.Address{}) || request.Value == nil || request.Value.Sign() < 0 {
		return nil, fmt.Errorf("safe proposal parameters are invalid")
	}
	if len(request.Data) > 128<<10 {
		return nil, fmt.Errorf("safe proposal data exceeds policy")
	}
	account, err := service.accounts.GetAccount(ctx, request.SafeAccountID)
	if err != nil {
		return nil, err
	}
	if account.SignerKind != wallet.SignerKindMultisig || account.SignerReference != "safe:v1:"+account.Address {
		return nil, fmt.Errorf("safe proposal: account is not a Safe")
	}
	safeAddress := common.HexToAddress(account.Address)
	owners, err := service.rpc.Owners(ctx, safeAddress)
	if err != nil {
		return nil, fmt.Errorf("safe proposal: owners: %w", err)
	}
	threshold, err := service.rpc.Threshold(ctx, safeAddress)
	if err != nil {
		return nil, fmt.Errorf("safe proposal: threshold: %w", err)
	}
	nonce, err := service.rpc.Nonce(ctx, safeAddress)
	if err != nil {
		return nil, fmt.Errorf("safe proposal: nonce: %w", err)
	}
	if len(owners) == 0 || threshold == 0 || threshold > uint64(len(owners)) {
		return nil, fmt.Errorf("safe proposal: on-chain owner policy is invalid")
	}
	snapshot := make([]signer.SafeOwnerSnapshot, 0, len(owners))
	for _, owner := range owners {
		snapshot = append(snapshot, signer.SafeOwnerSnapshot{Address: owner, Kind: signer.SafeOwnerEOA})
	}
	intent, err := signer.NewSafeTransactionIntent(safeAddress, request.ChainID, snapshot, threshold, signer.SafeTransaction{
		To: request.To, Value: request.Value, Data: append([]byte(nil), request.Data...),
		Operation: 0, SafeTxGas: big.NewInt(0), BaseGas: big.NewInt(0), GasPrice: big.NewInt(0),
		GasToken: common.Address{}, RefundReceiver: common.Address{}, Nonce: nonce,
	})
	if err != nil {
		return nil, err
	}
	proposalID, err := service.newID()
	if err != nil {
		return nil, err
	}
	proposal := &Proposal{
		ProposalID: proposalID, SafeAddress: safeAddress, AccountID: account.AccountID, ChainID: request.ChainID,
		Nonce: new(big.Int).Set(nonce), Transaction: safeTransactionFromIntent(intent.Transaction),
		Owners: snapshotToProposal(intent.Owners), Threshold: intent.Threshold,
		Digest: intent.Digest, Commitment: intent.Commitment, Status: ProposalPending,
		CreatedAt: service.now().UTC(), UpdatedAt: service.now().UTC(),
	}
	if err := service.proposals.CreateProposal(ctx, proposal); err != nil {
		return nil, err
	}
	return proposal, nil
}

// Sign collects one EOA owner signature for a pending proposal. The caller
// supplies the authorization callback so the owner's capability handle is
// produced by the app's authorizer (password or hardware session) exactly
// like the other signing flows.
func (service *Service) Sign(ctx context.Context, request SafeSignRequest, authorize func(accountID string, operation func(wallet.CapabilityHandle) error) error) (*Proposal, error) {
	proposal, err := service.proposals.GetProposal(ctx, request.ProposalID)
	if err != nil {
		return nil, err
	}
	if proposal.Status != ProposalPending {
		return nil, fmt.Errorf("safe proposal is not pending")
	}
	if request.ChainID != proposal.ChainID {
		return nil, fmt.Errorf("safe proposal chain mismatch")
	}
	ownerAccount, err := service.accounts.GetAccount(ctx, request.OwnerAccountID)
	if err != nil {
		return nil, err
	}
	ownerAddress := common.HexToAddress(ownerAccount.Address)
	kind := signer.SafeOwnerEOA
	found := false
	for _, owner := range proposal.Owners {
		if owner.Address == ownerAddress {
			kind = signer.SafeOwnerKind(owner.Kind)
			found = true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("safe proposal: account is not an owner")
	}
	if kind != signer.SafeOwnerEOA {
		return nil, fmt.Errorf("safe proposal: contract owners require EIP-1271 confirmation")
	}
	if !ownerAccount.SignerKind.SupportsEOASigning() {
		return nil, fmt.Errorf("safe proposal: owner account cannot produce EOA signatures")
	}
	for _, collected := range proposal.Signatures {
		if collected.Owner == ownerAddress {
			return nil, fmt.Errorf("safe proposal: owner already signed")
		}
	}
	now := service.now().UTC()
	approvalID, err := service.newID()
	if err != nil {
		return nil, err
	}
	signingID, err := service.newID()
	if err != nil {
		return nil, err
	}
	approval := evm.MessageApproval{
		ApprovalID: approvalID, AccountID: ownerAccount.AccountID, Signer: ownerAddress,
		Scheme: wallet.MessageSigningSafeOwner, ChainID: proposal.ChainID,
		Digest: proposal.Digest, IntentHash: proposal.Commitment, PayloadSize: 32,
		AuthorizationEpoch: ownerAccount.AuthorizationEpoch, ConfirmationLevel: evm.ConfirmationReinforced,
		CreatedAt: now, ConfirmedAt: now, ExpiresAt: now.Add(service.approval), State: evm.MessageApprovalPending, Revision: 1,
	}
	if err := service.messages.IssueMessageApproval(ctx, approval); err != nil {
		return nil, err
	}
	if _, err := service.messages.AuthorizeMessageSigning(ctx, evm.AuthorizeMessageSigningRequest{
		SigningID: signingID, ApprovalID: approvalID, AccountID: ownerAccount.AccountID, Signer: ownerAddress,
		Scheme: wallet.MessageSigningSafeOwner, ChainID: proposal.ChainID, Digest: proposal.Digest,
		IntentHash: proposal.Commitment, AuthorizationEpoch: ownerAccount.AuthorizationEpoch, AuthorizedAt: now,
	}); err != nil {
		return nil, err
	}
	var result wallet.SoftwareSigningResult
	if authorize == nil {
		return nil, fmt.Errorf("safe proposal: authorization callback is required")
	}
	typedData, typedDataErr := signer.EncodeSafeTransactionEIP712(proposal.SafeAddress, proposal.ChainID, signer.SafeTransaction{
		To: proposal.Transaction.To, Value: proposal.Transaction.Value, Data: proposal.Transaction.Data,
		Operation: proposal.Transaction.Operation, SafeTxGas: proposal.Transaction.SafeTxGas,
		BaseGas: proposal.Transaction.BaseGas, GasPrice: proposal.Transaction.GasPrice,
		GasToken: proposal.Transaction.GasToken, RefundReceiver: proposal.Transaction.RefundReceiver,
		Nonce: proposal.Transaction.Nonce,
	})
	if typedDataErr != nil {
		return nil, typedDataErr
	}
	if typedData.Digest != proposal.Digest {
		return nil, fmt.Errorf("safe proposal: EIP-712 digest mismatch")
	}
	signingErr := authorize(ownerAccount.AccountID, func(handle wallet.CapabilityHandle) error {
		signed, signErr := service.signer.SignSafeOwnerDigest(ctx, handle, evm.SafeOwnerDigestRequest{
			AccountID: ownerAccount.AccountID, Signer: ownerAddress, ChainID: proposal.ChainID,
			Digest: proposal.Digest, IntentHash: proposal.Commitment, ApprovalID: approvalID,
			DomainSeparatorHash: typedData.DomainSeparatorHash, MessageHash: typedData.MessageHash,
			CanonicalJSON: append([]byte(nil), typedData.CanonicalJSON...),
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
		return nil, signingErr
	}
	signature := append([]byte(nil), result.Signature...)
	signature[crypto.RecoveryIDOffset] += 27
	signatureHash := crypto.Keccak256Hash(signature)
	if err := service.messages.CompleteMessageSigning(ctx, evm.CompleteMessageSigningRequest{
		SigningID: signingID, SignatureHash: signatureHash, CompletedAt: service.now().UTC(),
	}); err != nil {
		return nil, err
	}
	coordinator, err := signer.NewSafeCoordinator(intentFromProposal(proposal))
	if err != nil {
		return nil, err
	}
	normalized, err := normalizeOwnerSignature(coordinator, ownerAddress, proposal.Digest, proposal.Commitment, signature)
	if err != nil {
		return nil, err
	}
	proposal.Signatures = append(proposal.Signatures, OwnerSignature{Owner: ownerAddress, Kind: uint8(signer.SafeOwnerEOA), Signature: normalized})
	proposal.UpdatedAt = service.now().UTC()
	if err := service.proposals.UpdateProposal(ctx, proposal); err != nil {
		return nil, err
	}
	return proposal, nil
}

// Execute signs the execTransaction envelope as the given gas payer and
// broadcasts it once the threshold is met.
func (service *Service) Execute(ctx context.Context, proposalID, gasPayerAccountID string, authorize func(accountID string, operation func(wallet.CapabilityHandle) error) error) (common.Hash, error) {
	proposal, err := service.proposals.GetProposal(ctx, proposalID)
	if err != nil {
		return common.Hash{}, err
	}
	if proposal.Status != ProposalPending {
		return common.Hash{}, fmt.Errorf("safe proposal is not pending")
	}
	if service.gasPayer == nil {
		return common.Hash{}, fmt.Errorf("safe execution: gas payer signer is unavailable")
	}
	if gasPayerAccountID == "" {
		return common.Hash{}, fmt.Errorf("safe execution: gas payer account is required")
	}
	// Revalidate the frozen proposal against the live on-chain state so a
	// stale nonce or changed owner set cannot be executed.
	liveNonce, err := service.rpc.Nonce(ctx, proposal.SafeAddress)
	if err != nil {
		return common.Hash{}, fmt.Errorf("safe execution: nonce revalidation: %w", err)
	}
	if liveNonce.Cmp(proposal.Nonce) != 0 {
		proposal.Status = ProposalFailed
		proposal.FailureCode = "stale_nonce"
		proposal.UpdatedAt = service.now().UTC()
		_ = service.proposals.UpdateProposal(context.Background(), proposal)
		return common.Hash{}, fmt.Errorf("safe execution: on-chain nonce %s differs from proposal nonce %s", liveNonce, proposal.Nonce)
	}
	liveOwners, err := service.rpc.Owners(ctx, proposal.SafeAddress)
	if err != nil {
		return common.Hash{}, fmt.Errorf("safe execution: owners revalidation: %w", err)
	}
	if !sameOwnerSet(liveOwners, proposal.Owners) {
		proposal.Status = ProposalFailed
		proposal.FailureCode = "owners_changed"
		proposal.UpdatedAt = service.now().UTC()
		_ = service.proposals.UpdateProposal(context.Background(), proposal)
		return common.Hash{}, fmt.Errorf("safe execution: on-chain owners changed since proposal")
	}
	coordinator, err := signer.NewSafeCoordinator(intentFromProposal(proposal))
	if err != nil {
		return common.Hash{}, err
	}
	for _, collected := range proposal.Signatures {
		if err := coordinator.AddSignature(signer.SafeOwnerSignature{
			Owner: collected.Owner, Kind: signer.SafeOwnerKind(collected.Kind),
			Digest: proposal.Digest, Commitment: proposal.Commitment, Signature: collected.Signature,
		}); err != nil {
			return common.Hash{}, err
		}
	}
	execData, err := coordinator.ExecTransaction()
	if err != nil {
		return common.Hash{}, err
	}
	if authorize == nil {
		return common.Hash{}, fmt.Errorf("safe execution: authorization callback is required")
	}
	var raw []byte
	executionErr := authorize(gasPayerAccountID, func(handle wallet.CapabilityHandle) error {
		gasPayerAccount, accountErr := service.accounts.GetAccount(ctx, gasPayerAccountID)
		if accountErr != nil {
			return accountErr
		}
		nonce, nonceErr := service.rpc.PendingNonce(ctx, common.HexToAddress(gasPayerAccount.Address))
		if nonceErr != nil {
			return nonceErr
		}
		gasPrice, gasPriceErr := service.rpc.SuggestGasPrice(ctx)
		if gasPriceErr != nil {
			return gasPriceErr
		}
		gasLimit := uint64(1_000_000)
		if proposal.Transaction.GasToken == (common.Address{}) {
			gasLimit = 1_500_000
		}
		raw, accountErr = service.gasPayer.SignGasPayer(ctx, handle, GasPayerSignRequest{
			AccountID: gasPayerAccount.AccountID, From: common.HexToAddress(gasPayerAccount.Address),
			ChainID: proposal.ChainID, To: proposal.SafeAddress, Data: execData,
			Nonce: nonce, GasPrice: gasPrice, GasLimit: gasLimit,
		})
		return accountErr
	})
	if executionErr != nil {
		return common.Hash{}, executionErr
	}
	if len(raw) == 0 {
		return common.Hash{}, fmt.Errorf("safe execution: no signed payload produced")
	}
	hash, err := service.rpc.Broadcast(ctx, raw)
	if err != nil {
		return common.Hash{}, err
	}
	proposal.Status = ProposalExecuted
	proposal.TxHash = hash
	proposal.UpdatedAt = service.now().UTC()
	if err := service.proposals.UpdateProposal(ctx, proposal); err != nil {
		return common.Hash{}, err
	}
	return hash, nil
}

// CheckExecution confirms the outcome of an executed proposal on-chain.
// A reverted execTransaction marks the proposal as failed.
func (service *Service) CheckExecution(ctx context.Context, proposalID string) (string, error) {
	proposal, err := service.proposals.GetProposal(ctx, proposalID)
	if err != nil {
		return "", err
	}
	if proposal.Status != ProposalExecuted || proposal.TxHash == (common.Hash{}) {
		return string(proposal.Status), nil
	}
	status, found, err := service.rpc.TransactionStatus(ctx, proposal.TxHash)
	if err != nil {
		return string(proposal.Status), err
	}
	if !found {
		return string(proposal.Status), nil
	}
	if status == 0 {
		proposal.Status = ProposalFailed
		proposal.FailureCode = "execution_reverted"
		proposal.UpdatedAt = service.now().UTC()
		if err := service.proposals.UpdateProposal(ctx, proposal); err != nil {
			return string(proposal.Status), err
		}
		return string(proposal.Status), nil
	}
	return string(proposal.Status), nil
}

// ListProposals returns the pending and recent proposals of one Safe.
func (service *Service) ListProposals(ctx context.Context, safeAccountID string, chainID uint64, limit int) ([]*Proposal, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	return service.proposals.ListProposals(ctx, safeAccountID, chainID, limit)
}

// GetProposal loads one proposal by ID.
func (service *Service) GetProposal(ctx context.Context, proposalID string) (*Proposal, error) {
	return service.proposals.GetProposal(ctx, proposalID)
}

// SafeSummary is the live on-chain state of one Safe account.
type SafeSummary struct {
	Address   common.Address
	ChainID   uint64
	Owners    []common.Address
	Threshold uint64
	Nonce     *big.Int
	Deployed  bool
	HasCode   bool
}

// SummarizeSafe reads the live on-chain state of a Safe account.
func (service *Service) SummarizeSafe(ctx context.Context, safeAccountID string, chainID uint64) (*SafeSummary, error) {
	account, err := service.accounts.GetAccount(ctx, safeAccountID)
	if err != nil {
		return nil, err
	}
	if account.SignerKind != wallet.SignerKindMultisig {
		return nil, fmt.Errorf("safe summary: account is not a Safe")
	}
	safeAddress := common.HexToAddress(account.Address)
	code, err := service.rpc.CodeAt(ctx, safeAddress)
	if err != nil {
		return nil, err
	}
	deployed := len(code) > 0
	summary := &SafeSummary{Address: safeAddress, ChainID: chainID, Deployed: deployed, HasCode: deployed}
	if !deployed {
		return summary, nil
	}
	owners, err := service.rpc.Owners(ctx, safeAddress)
	if err != nil {
		return nil, err
	}
	threshold, err := service.rpc.Threshold(ctx, safeAddress)
	if err != nil {
		return nil, err
	}
	nonce, err := service.rpc.Nonce(ctx, safeAddress)
	if err != nil {
		return nil, err
	}
	summary.Owners = owners
	summary.Threshold = threshold
	summary.Nonce = nonce
	return summary, nil
}

func sameOwnerSet(live []common.Address, frozen []OwnerSnapshot) bool {
	if len(live) != len(frozen) {
		return false
	}
	liveSet := make(map[common.Address]struct{}, len(live))
	for _, owner := range live {
		liveSet[owner] = struct{}{}
	}
	for _, owner := range frozen {
		if _, exists := liveSet[owner.Address]; !exists {
			return false
		}
	}
	return true
}

func safeTransactionFromIntent(intent signer.SafeTransaction) SafeTransaction {
	return SafeTransaction{
		To: intent.To, Value: new(big.Int).Set(intent.Value), Data: append([]byte(nil), intent.Data...),
		Operation: intent.Operation, SafeTxGas: new(big.Int).Set(intent.SafeTxGas),
		BaseGas: new(big.Int).Set(intent.BaseGas), GasPrice: new(big.Int).Set(intent.GasPrice),
		GasToken: intent.GasToken, RefundReceiver: intent.RefundReceiver, Nonce: new(big.Int).Set(intent.Nonce),
	}
}

func snapshotToProposal(snapshot []signer.SafeOwnerSnapshot) []OwnerSnapshot {
	owners := make([]OwnerSnapshot, 0, len(snapshot))
	for _, owner := range snapshot {
		owners = append(owners, OwnerSnapshot{Address: owner.Address, Kind: uint8(owner.Kind)})
	}
	return owners
}

func intentFromProposal(proposal *Proposal) signer.SafeTransactionIntent {
	owners := make([]signer.SafeOwnerSnapshot, 0, len(proposal.Owners))
	for _, owner := range proposal.Owners {
		owners = append(owners, signer.SafeOwnerSnapshot{Address: owner.Address, Kind: signer.SafeOwnerKind(owner.Kind)})
	}
	return signer.SafeTransactionIntent{
		SafeAddress: proposal.SafeAddress, ChainID: proposal.ChainID, Nonce: new(big.Int).Set(proposal.Nonce),
		Owners: owners, Threshold: proposal.Threshold,
		Transaction: signer.SafeTransaction{
			To: proposal.Transaction.To, Value: new(big.Int).Set(proposal.Transaction.Value),
			Data: append([]byte(nil), proposal.Transaction.Data...), Operation: proposal.Transaction.Operation,
			SafeTxGas: new(big.Int).Set(proposal.Transaction.SafeTxGas), BaseGas: new(big.Int).Set(proposal.Transaction.BaseGas),
			GasPrice: new(big.Int).Set(proposal.Transaction.GasPrice), GasToken: proposal.Transaction.GasToken,
			RefundReceiver: proposal.Transaction.RefundReceiver, Nonce: new(big.Int).Set(proposal.Transaction.Nonce),
		},
		Digest: proposal.Digest, Commitment: proposal.Commitment,
	}
}

func normalizeOwnerSignature(coordinator *signer.SafeCoordinator, owner common.Address, digest, commitment [32]byte, signature []byte) ([]byte, error) {
	normalized := append([]byte(nil), signature...)
	switch normalized[64] {
	case 27, 28:
		normalized[64] -= 27
	}
	r := new(big.Int).SetBytes(normalized[:32])
	s := new(big.Int).SetBytes(normalized[32:64])
	if !crypto.ValidateSignatureValues(normalized[64], r, s, true) {
		return nil, fmt.Errorf("safe proposal: invalid owner signature values")
	}
	publicKey, err := crypto.SigToPub(digest[:], normalized)
	if err != nil {
		return nil, fmt.Errorf("safe proposal: owner signature recovery: %w", err)
	}
	if crypto.PubkeyToAddress(*publicKey) != owner {
		return nil, fmt.Errorf("safe proposal: owner signature mismatch")
	}
	if err := coordinator.AddEOASignature(owner, digest, commitment, normalized); err != nil {
		return nil, err
	}
	normalized[64] += 27
	return normalized, nil
}

func randomID() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	encoded := fmt.Sprintf("%x-%x-%x-%x-%x", value[0:4], value[4:6], value[6:8], value[8:10], value[10:16])
	return encoded, nil
}
