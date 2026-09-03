package storage

import (
	"context"
	"fmt"
	"time"

	"blocowallet/internal/safe"

	"github.com/ethereum/go-ethereum/common"
	"gorm.io/gorm"
)

// safeProposalRow is the durable storage encoding of one Safe proposal.
type safeProposalRow struct {
	ProposalID  string    `gorm:"column:proposal_id;primaryKey;size:36"`
	AccountID   string    `gorm:"column:account_id;size:36;not null;index"`
	SafeAddress string    `gorm:"column:safe_address;size:42;not null;index"`
	ChainID     int64     `gorm:"column:chain_id;not null;index"`
	Status      string    `gorm:"column:status;size:16;not null;index"`
	Encoding    []byte    `gorm:"column:encoding;type:blob;not null"`
	Revision    int64     `gorm:"column:revision;not null;default:1"`
	CreatedAt   time.Time `gorm:"column:created_at_ms;not null"`
	UpdatedAt   time.Time `gorm:"column:updated_at_ms;not null"`
}

func (safeProposalRow) TableName() string { return "safe_proposals" }

var _ safe.ProposalRepository = (*GORMRepository)(nil)

// CreateProposal persists a new Safe proposal.
func (repository *GORMRepository) CreateProposal(ctx context.Context, proposal *safe.Proposal) error {
	if proposal == nil || proposal.ProposalID == "" || proposal.SafeAddress == (common.Address{}) {
		return fmt.Errorf("safe proposal is invalid")
	}
	encoded, err := safe.EncodeProposal(proposal)
	if err != nil {
		return err
	}
	row := safeProposalRow{
		ProposalID: proposal.ProposalID, AccountID: proposal.AccountID, SafeAddress: proposal.SafeAddress.Hex(),
		ChainID: int64(proposal.ChainID), Status: string(proposal.Status), Encoding: encoded,
		Revision: 1, CreatedAt: proposal.CreatedAt, UpdatedAt: proposal.UpdatedAt,
	}
	if err := repository.db.WithContext(ctx).Create(&row).Error; err != nil {
		return fmt.Errorf("create safe proposal: %w", err)
	}
	return nil
}

// GetProposal loads one Safe proposal by ID.
func (repository *GORMRepository) GetProposal(ctx context.Context, proposalID string) (*safe.Proposal, error) {
	var row safeProposalRow
	if err := repository.db.WithContext(ctx).Where("proposal_id = ?", proposalID).First(&row).Error; err != nil {
		return nil, fmt.Errorf("load safe proposal: %w", err)
	}
	return safe.DecodeProposal(row.Encoding)
}

// ListProposals returns proposals of one Safe account, most recent first.
func (repository *GORMRepository) ListProposals(ctx context.Context, accountID string, chainID uint64, limit int) ([]*safe.Proposal, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	var rows []safeProposalRow
	err := repository.db.WithContext(ctx).
		Where("account_id = ? AND chain_id = ?", accountID, int64(chainID)).
		Order("created_at_ms DESC, proposal_id ASC").
		Limit(limit).
		Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("list safe proposals: %w", err)
	}
	proposals := make([]*safe.Proposal, 0, len(rows))
	for _, row := range rows {
		proposal, err := safe.DecodeProposal(row.Encoding)
		if err != nil {
			return nil, err
		}
		proposals = append(proposals, proposal)
	}
	return proposals, nil
}

// UpdateProposal persists proposal state changes with optimistic locking.
func (repository *GORMRepository) UpdateProposal(ctx context.Context, proposal *safe.Proposal) error {
	if proposal == nil || proposal.ProposalID == "" {
		return fmt.Errorf("safe proposal is invalid")
	}
	encoded, err := safe.EncodeProposal(proposal)
	if err != nil {
		return err
	}
	result := repository.db.WithContext(ctx).Model(&safeProposalRow{}).
		Where("proposal_id = ?", proposal.ProposalID).
		Updates(map[string]any{
			"status": string(proposal.Status), "encoding": encoded, "updated_at_ms": proposal.UpdatedAt,
			"revision": gorm.Expr("revision + 1"),
		})
	if result.Error != nil {
		return fmt.Errorf("update safe proposal: %w", result.Error)
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("safe proposal conflict")
	}
	return nil
}
