package safe

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

// SafeTransaction mirrors the Safe execTransaction parameters persisted with
// each proposal. It is JSON-serialized so the proposal can be reconstructed
// without depending on the coordinator's in-memory cloning rules.
type SafeTransaction struct {
	To             common.Address `json:"to"`
	Value          *big.Int       `json:"value"`
	Data           []byte         `json:"data"`
	Operation      uint8          `json:"operation"`
	SafeTxGas      *big.Int       `json:"safeTxGas"`
	BaseGas        *big.Int       `json:"baseGas"`
	GasPrice       *big.Int       `json:"gasPrice"`
	GasToken       common.Address `json:"gasToken"`
	RefundReceiver common.Address `json:"refundReceiver"`
	Nonce          *big.Int       `json:"nonce"`
}

// OwnerSnapshot is the frozen owner set of one proposal.
type OwnerSnapshot struct {
	Address common.Address
	Kind    uint8
}

// OwnerSignature is one collected owner approval.
type OwnerSignature struct {
	Owner     common.Address
	Kind      uint8
	Signature []byte
}

// ProposalStatus tracks the lifecycle of one Safe transaction proposal.
type ProposalStatus string

const (
	ProposalPending  ProposalStatus = "pending"
	ProposalExecuted ProposalStatus = "executed"
	ProposalFailed   ProposalStatus = "failed"
)

// Proposal is a durable Safe transaction proposal.
type Proposal struct {
	ProposalID  string
	SafeAddress common.Address
	AccountID   string
	ChainID     uint64
	Nonce       *big.Int
	Transaction SafeTransaction
	Owners      []OwnerSnapshot
	Threshold   uint64
	Digest      [32]byte
	Commitment  [32]byte
	Signatures  []OwnerSignature
	Status      ProposalStatus
	TxHash      common.Hash
	FailureCode string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	SigningID   string
	ApprovalID  string
	Signer      common.Address
}

// ProposalRepository persists Safe proposals and their signatures.
type ProposalRepository interface {
	CreateProposal(context.Context, *Proposal) error
	GetProposal(context.Context, string) (*Proposal, error)
	ListProposals(context.Context, string, uint64, int) ([]*Proposal, error)
	UpdateProposal(context.Context, *Proposal) error
}

// ProposalJSON is the storage encoding of a proposal row.
type ProposalJSON struct {
	ProposalID  string           `json:"proposalId"`
	SafeAddress string           `json:"safeAddress"`
	AccountID   string           `json:"accountId"`
	ChainID     uint64           `json:"chainId"`
	Nonce       string           `json:"nonce"`
	Transaction SafeTransaction  `json:"transaction"`
	Owners      []OwnerSnapshot  `json:"owners"`
	Threshold   uint64           `json:"threshold"`
	Digest      string           `json:"digest"`
	Commitment  string           `json:"commitment"`
	Signatures  []OwnerSignature `json:"signatures,omitempty"`
	Status      ProposalStatus   `json:"status"`
	TxHash      string           `json:"txHash,omitempty"`
	FailureCode string           `json:"failureCode,omitempty"`
	CreatedAt   time.Time        `json:"createdAt"`
	UpdatedAt   time.Time        `json:"updatedAt"`
	SigningID   string           `json:"signingId,omitempty"`
	ApprovalID  string           `json:"approvalId,omitempty"`
	Signer      string           `json:"signer,omitempty"`
}

// EncodeProposal serializes a proposal for durable storage.
func EncodeProposal(proposal *Proposal) ([]byte, error) {
	if proposal == nil {
		return nil, fmt.Errorf("safe proposal is required")
	}
	return json.Marshal(proposalJSONFromProposal(proposal))
}

// DecodeProposal restores a proposal from its durable encoding.
func DecodeProposal(encoded []byte) (*Proposal, error) {
	var row ProposalJSON
	if err := json.Unmarshal(encoded, &row); err != nil {
		return nil, err
	}
	return proposalFromJSON(row)
}

func proposalJSONFromProposal(proposal *Proposal) ProposalJSON {
	return ProposalJSON{
		ProposalID:  proposal.ProposalID,
		SafeAddress: proposal.SafeAddress.Hex(),
		AccountID:   proposal.AccountID,
		ChainID:     proposal.ChainID,
		Nonce:       proposal.Nonce.Text(16),
		Transaction: proposal.Transaction,
		Owners:      proposal.Owners,
		Threshold:   proposal.Threshold,
		Digest:      common.Bytes2Hex(proposal.Digest[:]),
		Commitment:  common.Bytes2Hex(proposal.Commitment[:]),
		Signatures:  proposal.Signatures,
		Status:      proposal.Status,
		TxHash:      proposal.TxHash.Hex(),
		FailureCode: proposal.FailureCode,
		CreatedAt:   proposal.CreatedAt,
		UpdatedAt:   proposal.UpdatedAt,
		SigningID:   proposal.SigningID,
		ApprovalID:  proposal.ApprovalID,
		Signer:      proposal.Signer.Hex(),
	}
}

func proposalFromJSON(row ProposalJSON) (*Proposal, error) {
	nonce, ok := new(big.Int).SetString(row.Nonce, 16)
	if !ok {
		return nil, fmt.Errorf("safe proposal nonce is invalid")
	}
	proposal := &Proposal{
		ProposalID:  row.ProposalID,
		SafeAddress: common.HexToAddress(row.SafeAddress),
		AccountID:   row.AccountID,
		ChainID:     row.ChainID,
		Nonce:       nonce,
		Transaction: row.Transaction,
		Owners:      row.Owners,
		Threshold:   row.Threshold,
		Signatures:  row.Signatures,
		Status:      row.Status,
		TxHash:      common.HexToHash(row.TxHash),
		FailureCode: row.FailureCode,
		CreatedAt:   row.CreatedAt,
		UpdatedAt:   row.UpdatedAt,
		SigningID:   row.SigningID,
		ApprovalID:  row.ApprovalID,
		Signer:      common.HexToAddress(row.Signer),
	}
	digest, err := decodeHash(row.Digest)
	if err != nil {
		return nil, err
	}
	commitment, err := decodeHash(row.Commitment)
	if err != nil {
		return nil, err
	}
	proposal.Digest = digest
	proposal.Commitment = commitment
	return proposal, nil
}

func decodeHash(encoded string) ([32]byte, error) {
	var hash [32]byte
	decoded, err := hex.DecodeString(strings.TrimPrefix(encoded, "0x"))
	if err != nil || len(decoded) != 32 {
		return hash, fmt.Errorf("safe proposal hash is invalid")
	}
	copy(hash[:], decoded)
	return hash, nil
}
