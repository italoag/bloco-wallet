package signer

import (
	"bytes"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"sync"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

const (
	safeStaticSignatureLength = 65
	safeMaximumSignatureBytes = 4 << 10
	safeMaximumOwnerSnapshot  = 1024
)

var (
	// ErrSafeInvalidIntent means the transaction or owner snapshot no longer
	// matches its digest and commitment.
	ErrSafeInvalidIntent = errors.New("safe coordinator: invalid intent")
	// ErrSafeSignatureBinding means a response belongs to another transaction
	// digest or owner-snapshot commitment.
	ErrSafeSignatureBinding = errors.New("safe coordinator: signature binding mismatch")
	// ErrSafeUnknownOwner means the submitted address is not in the frozen
	// owner snapshot.
	ErrSafeUnknownOwner = errors.New("safe coordinator: unknown owner")
	// ErrSafeDuplicateSignature means an owner has already contributed a
	// signature to this coordinator.
	ErrSafeDuplicateSignature = errors.New("safe coordinator: duplicate owner signature")
	// ErrSafeInvalidSignature means an EOA signature is malformed, malleable,
	// cannot recover the declared owner, or has the wrong owner kind.
	ErrSafeInvalidSignature = errors.New("safe coordinator: invalid owner signature")
	// ErrSafeInsufficientSignatures prevents producing executable calldata
	// before the frozen threshold has been reached.
	ErrSafeInsufficientSignatures = errors.New("safe coordinator: insufficient signatures")
)

// SafeOwnerKind records how Safe.checkNSignatures must validate an owner.
type SafeOwnerKind uint8

const (
	// SafeOwnerEOA contributes a canonical secp256k1 signature with v=27/28.
	SafeOwnerEOA SafeOwnerKind = iota + 1
	// SafeOwnerContract contributes an EIP-1271 payload through Safe's v=0
	// dynamic-signature encoding.
	SafeOwnerContract
)

// SafeOwnerSnapshot freezes an owner address and its validation mechanism.
// Contract owners are deliberately distinct from EOAs: the coordinator never
// attempts to recover an EOA signature for a contract (including a nested
// Safe).
type SafeOwnerSnapshot struct {
	Address common.Address
	Kind    SafeOwnerKind
}

// SafeTransactionIntent freezes both the complete transaction and the Safe
// owner configuration under which signatures are gathered. Digest is the
// on-chain Safe transaction hash. Commitment additionally binds the owner set
// and threshold, which are intentionally absent from the Safe transaction
// hash.
type SafeTransactionIntent struct {
	SafeAddress common.Address
	ChainID     uint64
	Nonce       *big.Int
	Owners      []SafeOwnerSnapshot
	Threshold   uint64
	Transaction SafeTransaction
	Digest      [32]byte
	Commitment  [32]byte
}

// SafeOwnerSignature is a response bound to one immutable intent. Signature is
// r||s||v for an EOA and the raw EIP-1271 payload for a contract owner.
type SafeOwnerSignature struct {
	Owner      common.Address
	Kind       SafeOwnerKind
	Digest     [32]byte
	Commitment [32]byte
	Signature  []byte
}

type safeCollectedSignature struct {
	owner     common.Address
	kind      SafeOwnerKind
	signature []byte
}

// SafeCoordinator validates and deterministically combines owner signatures.
// It is safe for concurrent submissions and never owns or invokes an EOA key.
type SafeCoordinator struct {
	mu         sync.RWMutex
	intent     SafeTransactionIntent
	owners     map[common.Address]SafeOwnerSnapshot
	signatures map[common.Address]safeCollectedSignature
}

// NewSafeTransactionIntent creates an isolated owner/transaction snapshot and
// computes both the Safe digest and the application-level commitment.
func NewSafeTransactionIntent(safeAddress common.Address, chainID uint64, owners []SafeOwnerSnapshot, threshold uint64, transaction SafeTransaction) (SafeTransactionIntent, error) {
	intent := SafeTransactionIntent{
		SafeAddress: safeAddress,
		ChainID:     chainID,
		Nonce:       cloneBigInt(transaction.Nonce),
		Owners:      append([]SafeOwnerSnapshot(nil), owners...),
		Threshold:   threshold,
		Transaction: cloneSafeTransaction(transaction),
	}
	if err := validateSafeIntentBase(intent); err != nil {
		return SafeTransactionIntent{}, err
	}
	digest, err := SafeTransactionDigest(intent.SafeAddress, intent.ChainID, intent.Transaction)
	if err != nil {
		return SafeTransactionIntent{}, fmt.Errorf("%w: digest: %v", ErrSafeInvalidIntent, err)
	}
	intent.Digest = digest
	commitment, err := safeTransactionIntentCommitment(intent)
	if err != nil {
		return SafeTransactionIntent{}, err
	}
	intent.Commitment = commitment
	return intent.clone(), nil
}

// Validate recomputes every binding in the intent, including the owner-set
// commitment. It does not trust the duplicated nonce or caller-supplied hash.
func (intent SafeTransactionIntent) Validate() error {
	if err := validateSafeIntentBase(intent); err != nil {
		return err
	}
	digest, err := SafeTransactionDigest(intent.SafeAddress, intent.ChainID, intent.Transaction)
	if err != nil {
		return fmt.Errorf("%w: digest: %v", ErrSafeInvalidIntent, err)
	}
	if digest != intent.Digest {
		return fmt.Errorf("%w: transaction digest mismatch", ErrSafeInvalidIntent)
	}
	commitment, err := safeTransactionIntentCommitment(intent)
	if err != nil {
		return err
	}
	if commitment != intent.Commitment {
		return fmt.Errorf("%w: owner snapshot commitment mismatch", ErrSafeInvalidIntent)
	}
	return nil
}

// NewSafeCoordinator validates and privately clones an intent so subsequent
// mutations of caller-owned slices or big integers cannot change what is
// signed or executed.
func NewSafeCoordinator(intent SafeTransactionIntent) (*SafeCoordinator, error) {
	if err := intent.Validate(); err != nil {
		return nil, err
	}
	frozen := intent.clone()
	owners := make(map[common.Address]SafeOwnerSnapshot, len(frozen.Owners))
	for _, owner := range frozen.Owners {
		owners[owner.Address] = owner
	}
	return &SafeCoordinator{
		intent:     frozen,
		owners:     owners,
		signatures: make(map[common.Address]safeCollectedSignature, int(frozen.Threshold)),
	}, nil
}

// Intent returns a deep copy of the frozen intent.
func (coordinator *SafeCoordinator) Intent() SafeTransactionIntent {
	if coordinator == nil {
		return SafeTransactionIntent{}
	}
	return coordinator.intent.clone()
}

// AddEOASignature validates and adds an EOA response. Both digest and
// commitment must be echoed by the signing flow, preventing cross-intent and
// stale-owner-snapshot responses.
func (coordinator *SafeCoordinator) AddEOASignature(owner common.Address, digest, commitment [32]byte, signature []byte) error {
	return coordinator.AddSignature(SafeOwnerSignature{
		Owner: owner, Kind: SafeOwnerEOA, Digest: digest,
		Commitment: commitment, Signature: signature,
	})
}

// AddContractSignature adds a raw EIP-1271 payload for a snapshotted contract
// owner. The contract performs payload validation when Safe executes; this
// method validates its owner kind, bounds, and immutable intent bindings.
func (coordinator *SafeCoordinator) AddContractSignature(owner common.Address, digest, commitment [32]byte, signature []byte) error {
	return coordinator.AddSignature(SafeOwnerSignature{
		Owner: owner, Kind: SafeOwnerContract, Digest: digest,
		Commitment: commitment, Signature: signature,
	})
}

// AddSignature validates one bound owner response and atomically records it.
func (coordinator *SafeCoordinator) AddSignature(submission SafeOwnerSignature) error {
	if coordinator == nil {
		return fmt.Errorf("%w: nil coordinator", ErrSafeInvalidIntent)
	}
	if submission.Digest != coordinator.intent.Digest || submission.Commitment != coordinator.intent.Commitment {
		return ErrSafeSignatureBinding
	}
	owner, known := coordinator.owners[submission.Owner]
	if !known {
		return ErrSafeUnknownOwner
	}
	if submission.Owner == coordinator.intent.SafeAddress || submission.Kind != owner.Kind {
		return fmt.Errorf("%w: owner kind mismatch", ErrSafeInvalidSignature)
	}

	var normalized []byte
	switch owner.Kind {
	case SafeOwnerEOA:
		var err error
		normalized, err = normalizeSafeEOASignature(owner.Address, coordinator.intent.Digest, submission.Signature)
		if err != nil {
			return err
		}
	case SafeOwnerContract:
		// Reuse the single-owner encoder for its payload/address bounds. The
		// offset is intentionally discarded and rebuilt once every collected
		// static part is known.
		if _, err := ComposeSafeContractSignature(owner.Address, submission.Signature); err != nil {
			return fmt.Errorf("%w: contract payload: %v", ErrSafeInvalidSignature, err)
		}
		normalized = append([]byte(nil), submission.Signature...)
	default:
		return fmt.Errorf("%w: owner kind", ErrSafeInvalidSignature)
	}

	coordinator.mu.Lock()
	defer coordinator.mu.Unlock()
	if _, duplicate := coordinator.signatures[owner.Address]; duplicate {
		return ErrSafeDuplicateSignature
	}
	candidate := safeCollectedSignature{owner: owner.Address, kind: owner.Kind, signature: normalized}
	if safeSignatureSetLength(coordinator.signatures, candidate) > safeMaximumSignatureBytes {
		return fmt.Errorf("%w: aggregate signature bounds", ErrSafeInvalidSignature)
	}
	coordinator.signatures[owner.Address] = candidate
	return nil
}

// SignatureCount returns the number of distinct owners collected.
func (coordinator *SafeCoordinator) SignatureCount() int {
	if coordinator == nil {
		return 0
	}
	coordinator.mu.RLock()
	defer coordinator.mu.RUnlock()
	return len(coordinator.signatures)
}

// Ready reports whether the frozen owner threshold has been reached.
func (coordinator *SafeCoordinator) Ready() bool {
	if coordinator == nil {
		return false
	}
	coordinator.mu.RLock()
	defer coordinator.mu.RUnlock()
	return uint64(len(coordinator.signatures)) >= coordinator.intent.Threshold
}

// Signatures returns Safe.checkNSignatures-compatible bytes only after the
// threshold is met. Static parts are sorted by owner address; all contract
// offsets are recalculated relative to the complete aggregate byte string.
func (coordinator *SafeCoordinator) Signatures() ([]byte, error) {
	if coordinator == nil {
		return nil, fmt.Errorf("%w: nil coordinator", ErrSafeInvalidIntent)
	}
	coordinator.mu.RLock()
	if uint64(len(coordinator.signatures)) < coordinator.intent.Threshold {
		coordinator.mu.RUnlock()
		return nil, ErrSafeInsufficientSignatures
	}
	collected := make([]safeCollectedSignature, 0, len(coordinator.signatures))
	for _, signature := range coordinator.signatures {
		copyOfSignature := signature
		copyOfSignature.signature = append([]byte(nil), signature.signature...)
		collected = append(collected, copyOfSignature)
	}
	coordinator.mu.RUnlock()

	sort.Slice(collected, func(i, j int) bool {
		return bytes.Compare(collected[i].owner[:], collected[j].owner[:]) < 0
	})
	staticLength := len(collected) * safeStaticSignatureLength
	staticParts := make([][]byte, 0, len(collected))
	dynamicParts := make([]byte, 0)
	for _, signature := range collected {
		switch signature.kind {
		case SafeOwnerEOA:
			staticParts = append(staticParts, append([]byte(nil), signature.signature...))
		case SafeOwnerContract:
			composed, err := ComposeSafeContractSignature(signature.owner, signature.signature)
			if err != nil {
				return nil, fmt.Errorf("%w: contract composition: %v", ErrSafeInvalidSignature, err)
			}
			staticPart := append([]byte(nil), composed[:safeStaticSignatureLength]...)
			offset := staticLength + len(dynamicParts)
			copy(staticPart[32:64], safeUint256Word(new(big.Int).SetUint64(uint64(offset))))
			staticParts = append(staticParts, staticPart)
			dynamicParts = append(dynamicParts, composed[safeStaticSignatureLength:]...)
		default:
			return nil, fmt.Errorf("%w: collected owner kind", ErrSafeInvalidSignature)
		}
	}
	if staticLength+len(dynamicParts) > safeMaximumSignatureBytes {
		return nil, fmt.Errorf("%w: aggregate signature bounds", ErrSafeInvalidSignature)
	}
	aggregate := make([]byte, 0, staticLength+len(dynamicParts))
	for _, staticPart := range staticParts {
		aggregate = append(aggregate, staticPart...)
	}
	aggregate = append(aggregate, dynamicParts...)
	return aggregate, nil
}

// ExecTransaction returns ABI calldata for the complete frozen transaction.
// No calldata is returned before the threshold is reached.
func (coordinator *SafeCoordinator) ExecTransaction() ([]byte, error) {
	if coordinator == nil {
		return nil, fmt.Errorf("%w: nil coordinator", ErrSafeInvalidIntent)
	}
	signatures, err := coordinator.Signatures()
	if err != nil {
		return nil, err
	}
	return EncodeSafeExecTransaction(coordinator.intent.Transaction, signatures)
}

func validateSafeIntentBase(intent SafeTransactionIntent) error {
	if intent.SafeAddress == (common.Address{}) || intent.ChainID == 0 || intent.Nonce == nil {
		return fmt.Errorf("%w: Safe, chain, and nonce are required", ErrSafeInvalidIntent)
	}
	if intent.Transaction.Nonce == nil || intent.Nonce.Cmp(intent.Transaction.Nonce) != 0 {
		return fmt.Errorf("%w: nonce snapshot mismatch", ErrSafeInvalidIntent)
	}
	if len(intent.Owners) == 0 || len(intent.Owners) > safeMaximumOwnerSnapshot {
		return fmt.Errorf("%w: owner snapshot bounds", ErrSafeInvalidIntent)
	}
	if intent.Threshold == 0 || intent.Threshold > uint64(len(intent.Owners)) || intent.Threshold > uint64(safeMaximumSignatureBytes/safeStaticSignatureLength) {
		return fmt.Errorf("%w: threshold", ErrSafeInvalidIntent)
	}
	seen := make(map[common.Address]struct{}, len(intent.Owners))
	for _, owner := range intent.Owners {
		if owner.Address == (common.Address{}) || owner.Address == intent.SafeAddress {
			return fmt.Errorf("%w: zero or self owner", ErrSafeInvalidIntent)
		}
		if owner.Kind != SafeOwnerEOA && owner.Kind != SafeOwnerContract {
			return fmt.Errorf("%w: owner kind", ErrSafeInvalidIntent)
		}
		if _, duplicate := seen[owner.Address]; duplicate {
			return fmt.Errorf("%w: duplicate owner", ErrSafeInvalidIntent)
		}
		seen[owner.Address] = struct{}{}
	}
	return nil
}

func safeTransactionIntentCommitment(intent SafeTransactionIntent) ([32]byte, error) {
	if err := validateSafeIntentBase(intent); err != nil {
		return [32]byte{}, err
	}
	owners := append([]SafeOwnerSnapshot(nil), intent.Owners...)
	sort.Slice(owners, func(i, j int) bool {
		return bytes.Compare(owners[i].Address[:], owners[j].Address[:]) < 0
	})
	typeHash := crypto.Keccak256Hash([]byte("bloco-wallet/SafeTransactionIntent/v1"))
	encoded := make([]byte, 0, (7+2*len(owners))*32)
	encoded = append(encoded, typeHash[:]...)
	encoded = append(encoded, safeAddressWord(intent.SafeAddress)...)
	encoded = append(encoded, safeUint256Word(new(big.Int).SetUint64(intent.ChainID))...)
	encoded = append(encoded, safeUint256Word(intent.Nonce)...)
	encoded = append(encoded, safeUint256Word(new(big.Int).SetUint64(intent.Threshold))...)
	encoded = append(encoded, safeUint256Word(new(big.Int).SetUint64(uint64(len(owners))))...)
	encoded = append(encoded, intent.Digest[:]...)
	for _, owner := range owners {
		encoded = append(encoded, safeAddressWord(owner.Address)...)
		encoded = append(encoded, safeUint256Word(new(big.Int).SetUint64(uint64(owner.Kind)))...)
	}
	commitment := crypto.Keccak256Hash(encoded)
	return [32]byte(commitment), nil
}

func normalizeSafeEOASignature(owner common.Address, digest [32]byte, signature []byte) ([]byte, error) {
	if len(signature) != safeStaticSignatureLength {
		return nil, fmt.Errorf("%w: EOA signature size", ErrSafeInvalidSignature)
	}
	normalized := append([]byte(nil), signature...)
	var recoveryID byte
	switch normalized[64] {
	case 0, 1:
		recoveryID = normalized[64]
	case 27, 28:
		recoveryID = normalized[64] - 27
	default:
		return nil, fmt.Errorf("%w: EOA recovery id", ErrSafeInvalidSignature)
	}
	r := new(big.Int).SetBytes(normalized[:32])
	s := new(big.Int).SetBytes(normalized[32:64])
	if !crypto.ValidateSignatureValues(recoveryID, r, s, true) {
		return nil, fmt.Errorf("%w: EOA values are invalid or malleable", ErrSafeInvalidSignature)
	}
	recoverySignature := append([]byte(nil), normalized...)
	recoverySignature[64] = recoveryID
	publicKey, err := crypto.SigToPub(digest[:], recoverySignature)
	if err != nil {
		return nil, fmt.Errorf("%w: EOA recovery: %v", ErrSafeInvalidSignature, err)
	}
	if crypto.PubkeyToAddress(*publicKey) != owner {
		return nil, fmt.Errorf("%w: recovered EOA owner mismatch", ErrSafeInvalidSignature)
	}
	normalized[64] = recoveryID + 27
	return normalized, nil
}

func safeSignatureSetLength(existing map[common.Address]safeCollectedSignature, candidate safeCollectedSignature) int {
	total := safeCollectedSignatureLength(candidate)
	for _, signature := range existing {
		total += safeCollectedSignatureLength(signature)
	}
	return total
}

func safeCollectedSignatureLength(signature safeCollectedSignature) int {
	if signature.kind == SafeOwnerContract {
		return safeStaticSignatureLength + 32 + ((len(signature.signature)+31)/32)*32
	}
	return safeStaticSignatureLength
}

func (intent SafeTransactionIntent) clone() SafeTransactionIntent {
	intent.Nonce = cloneBigInt(intent.Nonce)
	intent.Owners = append([]SafeOwnerSnapshot(nil), intent.Owners...)
	intent.Transaction = cloneSafeTransaction(intent.Transaction)
	return intent
}

func cloneSafeTransaction(transaction SafeTransaction) SafeTransaction {
	transaction.Value = cloneBigInt(transaction.Value)
	transaction.Data = append([]byte(nil), transaction.Data...)
	transaction.SafeTxGas = cloneBigInt(transaction.SafeTxGas)
	transaction.BaseGas = cloneBigInt(transaction.BaseGas)
	transaction.GasPrice = cloneBigInt(transaction.GasPrice)
	transaction.Nonce = cloneBigInt(transaction.Nonce)
	return transaction
}

func cloneBigInt(value *big.Int) *big.Int {
	if value == nil {
		return nil
	}
	return new(big.Int).Set(value)
}
