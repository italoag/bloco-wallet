package evm

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"math/big"
	"time"
	"unicode/utf8"

	"blocowallet/internal/terminal"
	"blocowallet/internal/wallet"

	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

const (
	MaxPersonalSignMessageBytes = 64 << 10
	maxMessageOriginBytes       = 256
	personalSignIntentDomain    = "bloco-wallet/eip191-personal/v1"
)

type PreparePersonalSignRequest struct {
	AccountID string
	Signer    common.Address
	Message   []byte
	Origin    string
}

type PersonalSignPreview struct {
	AccountID     string
	Signer        common.Address
	Message       []byte
	MessageLength int
	Origin        string
	UTF8          bool
	Digest        common.Hash
	IntentHash    common.Hash
}

type PreparedPersonalSign struct {
	accountID  string
	signer     common.Address
	message    []byte
	origin     string
	digest     [32]byte
	intentHash [32]byte
}

func PreparePersonalSign(request PreparePersonalSignRequest) (*PreparedPersonalSign, error) {
	if !accountIDPattern.MatchString(request.AccountID) {
		return nil, invalidIntent("personal-sign account ID")
	}
	if request.Signer == (common.Address{}) {
		return nil, invalidIntent("personal-sign signer")
	}
	if len(request.Message) == 0 || len(request.Message) > MaxPersonalSignMessageBytes {
		return nil, invalidIntent("personal-sign message size")
	}
	if request.Origin == "" || len(request.Origin) > maxMessageOriginBytes || terminal.SanitizeInline(request.Origin, maxMessageOriginBytes) != request.Origin {
		return nil, invalidIntent("personal-sign origin")
	}
	digestBytes := accounts.TextHash(request.Message)
	var digest [32]byte
	copy(digest[:], digestBytes)
	intentHash := personalSignIntentHash(request.AccountID, request.Signer, request.Origin, request.Message, digest)
	return &PreparedPersonalSign{
		accountID: request.AccountID, signer: request.Signer, message: append([]byte(nil), request.Message...),
		origin: request.Origin, digest: digest, intentHash: intentHash,
	}, nil
}

func (prepared *PreparedPersonalSign) Preview() PersonalSignPreview {
	if prepared == nil {
		return PersonalSignPreview{}
	}
	return PersonalSignPreview{
		AccountID: prepared.accountID, Signer: prepared.signer, Message: append([]byte(nil), prepared.message...),
		MessageLength: len(prepared.message), Origin: prepared.origin, UTF8: utf8.Valid(prepared.message),
		Digest: prepared.digest, IntentHash: prepared.intentHash,
	}
}

func personalSignIntentHash(accountID string, signer common.Address, origin string, message []byte, digest [32]byte) [32]byte {
	canonical := make([]byte, 0, len(personalSignIntentDomain)+len(accountID)+len(origin)+len(message)+64)
	canonical = append(canonical, personalSignIntentDomain...)
	canonical = append(canonical, 0)
	canonical = append(canonical, accountID...)
	canonical = append(canonical, signer.Bytes()...)
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(origin)))
	canonical = append(canonical, length[:]...)
	canonical = append(canonical, origin...)
	binary.BigEndian.PutUint64(length[:], uint64(len(message)))
	canonical = append(canonical, length[:]...)
	canonical = append(canonical, message...)
	canonical = append(canonical, digest[:]...)
	return crypto.Keccak256Hash(canonical)
}

type MessageApprovalState string

const (
	MessageApprovalPending     MessageApprovalState = "pending"
	MessageApprovalConsumed    MessageApprovalState = "consumed"
	MessageApprovalInvalidated MessageApprovalState = "invalidated"
)

type MessageSigningState string

const (
	MessageSigningInProgress MessageSigningState = "signing"
	MessageSigningSigned     MessageSigningState = "signed"
	MessageSigningFailed     MessageSigningState = "signing_failed"
)

type MessageApproval struct {
	ApprovalID         string
	AccountID          string
	Signer             common.Address
	Scheme             wallet.MessageSigningScheme
	ChainID            uint64
	Digest             [32]byte
	IntentHash         [32]byte
	PayloadSize        uint64
	AuthorizationEpoch uint64
	ConfirmationLevel  ConfirmationLevel
	CreatedAt          time.Time
	ConfirmedAt        time.Time
	ExpiresAt          time.Time
	State              MessageApprovalState
	Revision           uint64
}

type AuthorizeMessageSigningRequest struct {
	SigningID          string
	ApprovalID         string
	AccountID          string
	Signer             common.Address
	Scheme             wallet.MessageSigningScheme
	ChainID            uint64
	Digest             [32]byte
	IntentHash         [32]byte
	AuthorizationEpoch uint64
	AuthorizedAt       time.Time
}

type MessageSigningRecord struct {
	SigningID      string
	ApprovalID     string
	AccountID      string
	Signer         common.Address
	Scheme         wallet.MessageSigningScheme
	ChainID        uint64
	Digest         [32]byte
	IntentHash     [32]byte
	State          MessageSigningState
	SignatureHash  common.Hash
	LastResultCode string
	CreatedAt      time.Time
	CompletedAt    time.Time
	Revision       uint64
}

type CompleteMessageSigningRequest struct {
	SigningID     string
	SignatureHash common.Hash
	CompletedAt   time.Time
}

type FailMessageSigningRequest struct {
	SigningID   string
	ResultCode  string
	CompletedAt time.Time
}

type MessageSigningRepository interface {
	IssueMessageApproval(context.Context, MessageApproval) error
	AuthorizeMessageSigning(context.Context, AuthorizeMessageSigningRequest) (MessageSigningRecord, error)
	CompleteMessageSigning(context.Context, CompleteMessageSigningRequest) error
	FailMessageSigning(context.Context, FailMessageSigningRequest) error
}

type MessageSigningOptions struct {
	Now         func() time.Time
	NewID       func() (string, error)
	ApprovalTTL time.Duration
}

type MessageSigningService struct {
	repository MessageSigningRepository
	signer     MessageIntentSigner
	options    MessageSigningOptions
}

type PersonalSignApprovalRequest struct {
	AuthorizationEpoch  uint64
	ConfirmedIntentHash [32]byte
	ConfirmationLevel   ConfirmationLevel
}

type PersonalSignResult struct {
	ApprovalID string
	SigningID  string
	AccountID  string
	Signer     common.Address
	Digest     [32]byte
	IntentHash [32]byte
	Signature  []byte
}

func NewMessageSigningService(repository MessageSigningRepository, signer ApprovedDigestSigner, options MessageSigningOptions) (*MessageSigningService, error) {
	adapter, err := NewDigestSignerAdapter(signer)
	if err != nil {
		return nil, err
	}
	return newMessageSigningService(repository, adapter, options)
}

// NewMessageSigningServiceWithStructuredSigner passes canonical message
// payloads to a structured signer.
func NewMessageSigningServiceWithStructuredSigner(repository MessageSigningRepository, signer MessageIntentSigner, options MessageSigningOptions) (*MessageSigningService, error) {
	return newMessageSigningService(repository, signer, options)
}

func newMessageSigningService(repository MessageSigningRepository, signer MessageIntentSigner, options MessageSigningOptions) (*MessageSigningService, error) {
	if repository == nil || signer == nil {
		return nil, fmt.Errorf("message signing dependencies are required")
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.NewID == nil {
		options.NewID = secureEngineUUID
	}
	if options.ApprovalTTL <= 0 || options.ApprovalTTL > 5*time.Minute {
		return nil, fmt.Errorf("message approval TTL is outside policy")
	}
	return &MessageSigningService{repository: repository, signer: signer, options: options}, nil
}

func (service *MessageSigningService) ApproveAndSignPersonal(ctx context.Context, handle wallet.CapabilityHandle, prepared *PreparedPersonalSign, request PersonalSignApprovalRequest) (PersonalSignResult, error) {
	if service == nil || prepared == nil || prepared.accountID == "" || prepared.signer == (common.Address{}) || prepared.digest == ([32]byte{}) || prepared.intentHash == ([32]byte{}) {
		return PersonalSignResult{}, invalidIntent("prepared personal-sign request")
	}
	return service.approveAndSignMessage(ctx, handle, messageApprovalIntent{
		accountID: prepared.accountID, signer: prepared.signer, scheme: wallet.MessageSigningEIP191Personal,
		digest: prepared.digest, intentHash: prepared.intentHash, payloadSize: uint64(len(prepared.message)),
		personalMessage: append([]byte(nil), prepared.message...), origin: prepared.origin,
	}, request, "personal-sign")
}

func (service *MessageSigningService) ApproveAndSignEIP712(ctx context.Context, handle wallet.CapabilityHandle, prepared *PreparedEIP712Sign, request PersonalSignApprovalRequest) (PersonalSignResult, error) {
	if service == nil || prepared == nil || prepared.accountID == "" || prepared.signer == (common.Address{}) || prepared.chainID == 0 || prepared.digest == (common.Hash{}) || prepared.intentHash == (common.Hash{}) {
		return PersonalSignResult{}, invalidIntent("prepared EIP-712 request")
	}
	return service.approveAndSignMessage(ctx, handle, messageApprovalIntent{
		accountID: prepared.accountID, signer: prepared.signer, scheme: wallet.MessageSigningEIP712,
		chainID: prepared.chainID, digest: prepared.digest, intentHash: prepared.intentHash,
		payloadSize: uint64(len(prepared.canonicalJSON)), origin: prepared.origin,
		canonicalJSON:       append([]byte(nil), prepared.canonicalJSON...),
		domainSeparatorHash: prepared.domainSeparatorHash, messageHash: prepared.messageHash,
	}, request, "EIP-712")
}

type messageApprovalIntent struct {
	accountID           string
	signer              common.Address
	scheme              wallet.MessageSigningScheme
	chainID             uint64
	digest              [32]byte
	intentHash          [32]byte
	payloadSize         uint64
	origin              string
	personalMessage     []byte
	canonicalJSON       []byte
	domainSeparatorHash [32]byte
	messageHash         [32]byte
}

func (service *MessageSigningService) approveAndSignMessage(ctx context.Context, handle wallet.CapabilityHandle, intent messageApprovalIntent, request PersonalSignApprovalRequest, field string) (PersonalSignResult, error) {
	if service == nil || service.repository == nil || service.signer == nil || intent.accountID == "" || intent.signer == (common.Address{}) || intent.scheme == "" || intent.digest == ([32]byte{}) || intent.intentHash == ([32]byte{}) {
		return PersonalSignResult{}, invalidIntent("prepared message signing request")
	}
	if request.AuthorizationEpoch == 0 || request.AuthorizationEpoch > math.MaxInt64 || request.ConfirmedIntentHash != intent.intentHash {
		return PersonalSignResult{}, &EngineError{Code: ErrorPolicyDenied, Field: field + " approval binding"}
	}
	if request.ConfirmationLevel != ConfirmationReinforced {
		return PersonalSignResult{}, &EngineError{Code: ErrorPolicyDenied, Field: field + " reinforced confirmation"}
	}
	now := service.options.Now().UTC()
	approvalID, err := service.options.NewID()
	if err != nil {
		return PersonalSignResult{}, &EngineError{Code: ErrorPolicyDenied, Field: "message approval ID", Cause: err}
	}
	signingID, err := service.options.NewID()
	if err != nil {
		return PersonalSignResult{}, &EngineError{Code: ErrorPolicyDenied, Field: "message signing ID", Cause: err}
	}
	if approvalID == signingID {
		return PersonalSignResult{}, &EngineError{Code: ErrorPolicyDenied, Field: "message signing identity"}
	}
	approval := MessageApproval{
		ApprovalID: approvalID, AccountID: intent.accountID, Signer: intent.signer,
		Scheme: intent.scheme, ChainID: intent.chainID, Digest: intent.digest, IntentHash: intent.intentHash,
		PayloadSize: intent.payloadSize, AuthorizationEpoch: request.AuthorizationEpoch,
		ConfirmationLevel: request.ConfirmationLevel, CreatedAt: now, ConfirmedAt: now,
		ExpiresAt: now.Add(service.options.ApprovalTTL), State: MessageApprovalPending, Revision: 1,
	}
	if err := ValidateMessageApproval(approval); err != nil {
		return PersonalSignResult{}, err
	}
	if err := service.repository.IssueMessageApproval(ctx, approval); err != nil {
		return PersonalSignResult{}, err
	}
	record, err := service.repository.AuthorizeMessageSigning(ctx, AuthorizeMessageSigningRequest{
		SigningID: signingID, ApprovalID: approvalID, AccountID: approval.AccountID, Signer: approval.Signer,
		Scheme: approval.Scheme, ChainID: approval.ChainID, Digest: approval.Digest, IntentHash: approval.IntentHash,
		AuthorizationEpoch: approval.AuthorizationEpoch, AuthorizedAt: now,
	})
	if err != nil {
		return PersonalSignResult{}, err
	}
	if record.SigningID != signingID || record.ApprovalID != approvalID || record.AccountID != approval.AccountID || record.Signer != approval.Signer || record.Scheme != approval.Scheme || record.ChainID != approval.ChainID || record.Digest != approval.Digest || record.IntentHash != approval.IntentHash || record.State != MessageSigningInProgress {
		service.failMessageSigning(signingID, "persistence_failed")
		return PersonalSignResult{}, &EngineError{Code: ErrorSigningFailed, Field: "message signing record binding"}
	}
	var signed wallet.SoftwareSigningResult
	switch approval.Scheme {
	case wallet.MessageSigningEIP191Personal:
		signed, err = service.signer.SignPersonalMessage(ctx, handle, PersonalMessageSigningIntent{
			AccountID: approval.AccountID, Signer: approval.Signer,
			Message: append([]byte(nil), intent.personalMessage...), Origin: intent.origin,
			Digest: approval.Digest, IntentHash: approval.IntentHash, ApprovalID: approval.ApprovalID,
		})
	case wallet.MessageSigningEIP712:
		signed, err = service.signer.SignEIP712(ctx, handle, EIP712SigningIntent{
			AccountID: approval.AccountID, Signer: approval.Signer, ChainID: approval.ChainID, Origin: intent.origin,
			CanonicalJSON:       append([]byte(nil), intent.canonicalJSON...),
			DomainSeparatorHash: intent.domainSeparatorHash, MessageHash: intent.messageHash,
			Digest: approval.Digest, IntentHash: approval.IntentHash, ApprovalID: approval.ApprovalID,
		})
	default:
		err = fmt.Errorf("unsupported structured message scheme")
	}
	if err != nil {
		service.failMessageSigning(signingID, "signer_rejected")
		return PersonalSignResult{}, &EngineError{Code: ErrorSigningFailed, Field: field + " signer", Cause: err}
	}
	if signed.AccountID != approval.AccountID || signed.Purpose != wallet.SigningPurposeMessage || signed.MessageScheme != approval.Scheme || signed.ChainID != approval.ChainID || signed.Digest != approval.Digest || signed.IntentHash != approval.IntentHash {
		service.failMessageSigning(signingID, "invalid_signature")
		return PersonalSignResult{}, &EngineError{Code: ErrorSigningFailed, Field: field + " result binding"}
	}
	if err := validateMessageSignature(signed.Signature, intent.signer, intent.digest); err != nil {
		service.failMessageSigning(signingID, "invalid_signature")
		return PersonalSignResult{}, err
	}
	signature := append([]byte(nil), signed.Signature...)
	signature[crypto.RecoveryIDOffset] += 27
	signatureHash := crypto.Keccak256Hash(signature)
	if err := service.repository.CompleteMessageSigning(ctx, CompleteMessageSigningRequest{SigningID: signingID, SignatureHash: signatureHash, CompletedAt: service.options.Now().UTC()}); err != nil {
		service.failMessageSigning(signingID, "persistence_failed")
		return PersonalSignResult{}, err
	}
	return PersonalSignResult{
		ApprovalID: approvalID, SigningID: signingID, AccountID: intent.accountID, Signer: intent.signer,
		Digest: intent.digest, IntentHash: intent.intentHash, Signature: signature,
	}, nil
}

func (service *MessageSigningService) failMessageSigning(signingID, resultCode string) {
	_ = service.repository.FailMessageSigning(context.Background(), FailMessageSigningRequest{
		SigningID: signingID, ResultCode: resultCode, CompletedAt: service.options.Now().UTC(),
	})
}

func ValidateMessageApproval(approval MessageApproval) error {
	if !accountIDPattern.MatchString(approval.ApprovalID) || !accountIDPattern.MatchString(approval.AccountID) || approval.Signer == (common.Address{}) {
		return invalidIntent("message approval identity")
	}
	if !validMessageSchemeBinding(approval.Scheme, approval.ChainID) || approval.Digest == ([32]byte{}) || approval.IntentHash == ([32]byte{}) {
		return invalidIntent("message approval digest")
	}
	if approval.PayloadSize > MaxPersonalSignMessageBytes || approval.AuthorizationEpoch == 0 || approval.AuthorizationEpoch > math.MaxInt64 || approval.ConfirmationLevel != ConfirmationReinforced {
		return invalidIntent("message approval policy")
	}
	if approval.CreatedAt.IsZero() || approval.ConfirmedAt.Before(approval.CreatedAt) || !approval.ExpiresAt.After(approval.ConfirmedAt) || approval.State != MessageApprovalPending || approval.Revision == 0 {
		return invalidIntent("message approval lifecycle")
	}
	return nil
}

func ValidateAuthorizeMessageSigningRequest(request AuthorizeMessageSigningRequest) error {
	if !accountIDPattern.MatchString(request.SigningID) || !accountIDPattern.MatchString(request.ApprovalID) || !accountIDPattern.MatchString(request.AccountID) || request.Signer == (common.Address{}) {
		return invalidIntent("message signing identity")
	}
	if !validMessageSchemeBinding(request.Scheme, request.ChainID) || request.Digest == ([32]byte{}) || request.IntentHash == ([32]byte{}) || request.AuthorizationEpoch == 0 || request.AuthorizationEpoch > math.MaxInt64 || request.AuthorizedAt.IsZero() {
		return invalidIntent("message signing binding")
	}
	return nil
}

func ValidateCompleteMessageSigningRequest(request CompleteMessageSigningRequest) error {
	if !accountIDPattern.MatchString(request.SigningID) || request.SignatureHash == (common.Hash{}) || request.CompletedAt.IsZero() {
		return invalidIntent("message signing completion")
	}
	return nil
}

func ValidateFailMessageSigningRequest(request FailMessageSigningRequest) error {
	if !accountIDPattern.MatchString(request.SigningID) || request.CompletedAt.IsZero() {
		return invalidIntent("message signing failure")
	}
	switch request.ResultCode {
	case "signer_rejected", "invalid_signature", "persistence_failed", "cancelled":
		return nil
	default:
		return invalidIntent("message signing failure code")
	}
}

func validMessageSchemeBinding(scheme wallet.MessageSigningScheme, chainID uint64) bool {
	switch scheme {
	case wallet.MessageSigningEIP191Personal:
		return chainID == 0
	case wallet.MessageSigningEIP712:
		return chainID > 0 && chainID <= math.MaxInt64
	default:
		return false
	}
}

func validateMessageSignature(signature []byte, expected common.Address, digest [32]byte) error {
	if len(signature) != crypto.SignatureLength || signature[crypto.RecoveryIDOffset] > 1 {
		return &EngineError{Code: ErrorSigningFailed, Field: "message signature encoding"}
	}
	r := new(big.Int).SetBytes(signature[:32])
	s := new(big.Int).SetBytes(signature[32:64])
	if !crypto.ValidateSignatureValues(signature[64], r, s, true) {
		return &EngineError{Code: ErrorSigningFailed, Field: "message signature values"}
	}
	publicKey, err := crypto.SigToPub(digest[:], signature)
	if err != nil {
		return &EngineError{Code: ErrorSigningFailed, Field: "message signature recovery", Cause: err}
	}
	if crypto.PubkeyToAddress(*publicKey) != expected {
		return &EngineError{Code: ErrorSigningFailed, Field: "message signature account"}
	}
	return nil
}
