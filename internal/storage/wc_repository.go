package storage

import (
	"context"
	"encoding/json"
	"fmt"

	"blocowallet/internal/walletconnect"

	"gorm.io/gorm"
)

var _ walletconnect.SessionStore = (*GORMRepository)(nil)

type wcSessionRow struct {
	Topic        string `gorm:"column:topic;primaryKey"`
	PeerName     string `gorm:"column:peer_name"`
	PeerMetadata string `gorm:"column:peer_metadata"`
	AccountID    string `gorm:"column:account_id"`
	Namespaces   string `gorm:"column:namespaces"`
	ExpiresAtMS  int64  `gorm:"column:expires_at_ms"`
	Revoked      bool   `gorm:"column:revoked"`
	CreatedAtMS  int64  `gorm:"column:created_at_ms"`
	LastUsedAtMS *int64 `gorm:"column:last_used_at_ms"`
}

func (wcSessionRow) TableName() string { return "wc_sessions" }

func (repository *GORMRepository) SaveSession(ctx context.Context, session *walletconnect.Session) error {
	if session == nil || session.Topic == "" || session.AccountID == "" || session.Namespaces == nil {
		return fmt.Errorf("walletconnect session is incomplete")
	}
	metadata, err := json.Marshal(session.PeerMetadata)
	if err != nil {
		return fmt.Errorf("encode walletconnect peer metadata: %w", err)
	}
	namespaces, err := json.Marshal(session.Namespaces)
	if err != nil {
		return fmt.Errorf("encode walletconnect namespaces: %w", err)
	}
	row := wcSessionRow{
		Topic: session.Topic, PeerName: session.PeerName, PeerMetadata: string(metadata),
		AccountID: session.AccountID, Namespaces: string(namespaces),
		ExpiresAtMS: session.ExpiresAt, Revoked: session.Revoked, CreatedAtMS: session.CreatedAt,
	}
	if session.LastUsedAt != nil {
		usedAt := *session.LastUsedAt
		row.LastUsedAtMS = &usedAt
	}
	if err := repository.db.WithContext(ctx).Create(&row).Error; err != nil {
		return fmt.Errorf("save walletconnect session: %w", err)
	}
	return nil
}

func (repository *GORMRepository) GetSession(ctx context.Context, topic string) (*walletconnect.Session, error) {
	var row wcSessionRow
	if err := repository.db.WithContext(ctx).Where("topic = ?", topic).First(&row).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("walletconnect session not found")
		}
		return nil, fmt.Errorf("load walletconnect session: %w", err)
	}
	session, err := wcSessionFromRow(row)
	if err != nil {
		return nil, err
	}
	return session, nil
}

func (repository *GORMRepository) ListSessions(ctx context.Context, accountID string, includeRevoked bool) ([]walletconnect.Session, error) {
	query := repository.db.WithContext(ctx).Where("account_id = ?", accountID)
	if !includeRevoked {
		query = query.Where("revoked = 0")
	}
	var rows []wcSessionRow
	if err := query.Order("created_at_ms ASC").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list walletconnect sessions: %w", err)
	}
	sessions := make([]walletconnect.Session, 0, len(rows))
	for _, row := range rows {
		session, err := wcSessionFromRow(row)
		if err != nil {
			return nil, err
		}
		sessions = append(sessions, *session)
	}
	return sessions, nil
}

func (repository *GORMRepository) RevokeSession(ctx context.Context, topic string, revokedAt int64) error {
	update := repository.db.WithContext(ctx).Model(&wcSessionRow{}).Where(
		"topic = ? AND revoked = 0", topic,
	).Updates(map[string]any{"revoked": true, "last_used_at_ms": revokedAt})
	if update.Error != nil {
		return fmt.Errorf("revoke walletconnect session: %w", update.Error)
	}
	if update.RowsAffected != 1 {
		return fmt.Errorf("walletconnect session not revocable")
	}
	return nil
}

func (repository *GORMRepository) TouchSession(ctx context.Context, topic string, usedAt int64) error {
	update := repository.db.WithContext(ctx).Model(&wcSessionRow{}).Where(
		"topic = ? AND revoked = 0", topic,
	).Update("last_used_at_ms", usedAt)
	if update.Error != nil {
		return fmt.Errorf("touch walletconnect session: %w", update.Error)
	}
	if update.RowsAffected != 1 {
		return fmt.Errorf("walletconnect session not active")
	}
	return nil
}

func wcSessionFromRow(row wcSessionRow) (*walletconnect.Session, error) {
	var metadata map[string]any
	if err := json.Unmarshal([]byte(row.PeerMetadata), &metadata); err != nil {
		return nil, fmt.Errorf("decode walletconnect peer metadata: %w", err)
	}
	var namespaces walletconnect.Namespaces
	if err := json.Unmarshal([]byte(row.Namespaces), &namespaces); err != nil {
		return nil, fmt.Errorf("decode walletconnect namespaces: %w", err)
	}
	session := &walletconnect.Session{
		Topic: row.Topic, PeerName: row.PeerName, PeerMetadata: metadata,
		AccountID: row.AccountID, Namespaces: namespaces,
		ExpiresAt: row.ExpiresAtMS, Revoked: row.Revoked, CreatedAt: row.CreatedAtMS,
	}
	if row.LastUsedAtMS != nil {
		usedAt := *row.LastUsedAtMS
		session.LastUsedAt = &usedAt
	}
	return session, nil
}
