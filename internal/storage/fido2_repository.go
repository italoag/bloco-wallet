package storage

import (
	"context"
	"encoding/json"
	"fmt"

	"blocowallet/internal/fido2"

	"gorm.io/gorm"
)

var _ fido2.Repository = (*GORMRepository)(nil)

type fido2CredentialRow struct {
	CredentialID []byte `gorm:"column:credential_id;primaryKey"`
	RPID         string `gorm:"column:rp_id"`
	UserHandle   []byte `gorm:"column:user_handle"`
	PublicKey    []byte `gorm:"column:public_key"`
	Algorithm    int64  `gorm:"column:algorithm"`
	SignCount    uint32 `gorm:"column:sign_count"`
	Transports   string `gorm:"column:transports"`
	CreatedAtMS  int64  `gorm:"column:created_at_ms"`
	LastUsedAtMS *int64 `gorm:"column:last_used_at_ms"`
}

func (fido2CredentialRow) TableName() string { return "fido2_credentials" }

type fido2ChallengeRow struct {
	ChallengeID string  `gorm:"column:challenge_id;primaryKey"`
	Kind        string  `gorm:"column:kind"`
	RPID        string  `gorm:"column:rp_id"`
	Origin      string  `gorm:"column:origin"`
	AccountID   *string `gorm:"column:account_id"`
	UserHandle  []byte  `gorm:"column:user_handle"`
	Challenge   []byte  `gorm:"column:challenge"`
	ExpiresAtMS int64   `gorm:"column:expires_at_ms"`
	Used        bool    `gorm:"column:used"`
	CreatedAtMS int64   `gorm:"column:created_at_ms"`
}

func (fido2ChallengeRow) TableName() string { return "fido2_challenges" }

func (repository *GORMRepository) SaveCredential(ctx context.Context, credential *fido2.Credential) error {
	if credential == nil || len(credential.CredentialID) == 0 || credential.PublicKey == nil {
		return fmt.Errorf("fido2 credential is incomplete")
	}
	transports, err := json.Marshal(credential.Transports)
	if err != nil {
		return fmt.Errorf("encode fido2 transports: %w", err)
	}
	row := fido2CredentialRow{
		CredentialID: append([]byte(nil), credential.CredentialID...),
		RPID:         credential.RPID,
		UserHandle:   append([]byte(nil), credential.UserHandle...),
		PublicKey:    append([]byte(nil), credential.PublicKey...),
		Algorithm:    credential.Algorithm,
		SignCount:    credential.SignCount,
		Transports:   string(transports),
		CreatedAtMS:  credential.CreatedAt,
	}
	if err := repository.db.WithContext(ctx).Create(&row).Error; err != nil {
		return fmt.Errorf("save fido2 credential: %w", err)
	}
	return nil
}

func (repository *GORMRepository) GetCredential(ctx context.Context, credentialID []byte) (*fido2.Credential, error) {
	var row fido2CredentialRow
	if err := repository.db.WithContext(ctx).Where("credential_id = ?", credentialID).First(&row).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("fido2 credential not found")
		}
		return nil, fmt.Errorf("load fido2 credential: %w", err)
	}
	credential := fido2CredentialFromRow(row)
	return &credential, nil
}

func (repository *GORMRepository) ListCredentials(ctx context.Context, rpID string) ([]fido2.Credential, error) {
	var rows []fido2CredentialRow
	if err := repository.db.WithContext(ctx).Where("rp_id = ?", rpID).Order("created_at_ms ASC").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list fido2 credentials: %w", err)
	}
	credentials := make([]fido2.Credential, 0, len(rows))
	for _, row := range rows {
		credentials = append(credentials, fido2CredentialFromRow(row))
	}
	return credentials, nil
}

func (repository *GORMRepository) UpdateSignCount(ctx context.Context, credentialID []byte, signCount uint32, usedAt int64) error {
	update := repository.db.WithContext(ctx).Model(&fido2CredentialRow{}).Where(
		"credential_id = ? AND sign_count <= ?", credentialID, signCount,
	).Updates(map[string]any{"sign_count": signCount, "last_used_at_ms": usedAt})
	if update.Error != nil {
		return fmt.Errorf("update fido2 sign count: %w", update.Error)
	}
	if update.RowsAffected != 1 {
		return fmt.Errorf("fido2 sign count regressed")
	}
	return nil
}

func (repository *GORMRepository) SaveChallenge(ctx context.Context, challenge *fido2.Challenge) error {
	if challenge == nil || len(challenge.Challenge) != 32 {
		return fmt.Errorf("fido2 challenge is incomplete")
	}
	var nullableAccountID *string
	if challenge.AccountID != "" {
		nullableAccountID = &challenge.AccountID
	}
	row := fido2ChallengeRow{
		ChallengeID: challenge.ChallengeID, Kind: string(challenge.Kind), RPID: challenge.RPID,
		Origin: challenge.Origin, AccountID: nullableAccountID, UserHandle: append([]byte(nil), challenge.UserHandle...),
		Challenge:   append([]byte(nil), challenge.Challenge...),
		ExpiresAtMS: challenge.ExpiresAt, Used: challenge.Used,
		CreatedAtMS: challenge.CreatedAt,
	}
	if err := repository.db.WithContext(ctx).Create(&row).Error; err != nil {
		return fmt.Errorf("save fido2 challenge: %w", err)
	}
	return nil
}

func (repository *GORMRepository) GetChallenge(ctx context.Context, challengeID string) (*fido2.Challenge, error) {
	var row fido2ChallengeRow
	if err := repository.db.WithContext(ctx).Where("challenge_id = ?", challengeID).First(&row).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("fido2 challenge not found")
		}
		return nil, fmt.Errorf("load fido2 challenge: %w", err)
	}
	accountID := ""
	if row.AccountID != nil {
		accountID = *row.AccountID
	}
	return &fido2.Challenge{
		ChallengeID: row.ChallengeID, Kind: fido2.ChallengeKind(row.Kind), RPID: row.RPID,
		Origin: row.Origin, AccountID: accountID, UserHandle: append([]byte(nil), row.UserHandle...),
		Challenge: append([]byte(nil), row.Challenge...),
		ExpiresAt: row.ExpiresAtMS, CreatedAt: row.CreatedAtMS, Used: row.Used,
	}, nil
}

func (repository *GORMRepository) ConsumeChallenge(ctx context.Context, challengeID string, usedAt int64) error {
	update := repository.db.WithContext(ctx).Model(&fido2ChallengeRow{}).Where(
		"challenge_id = ? AND used = 0 AND expires_at_ms > ?", challengeID, usedAt,
	).Update("used", true)
	if update.Error != nil {
		return fmt.Errorf("consume fido2 challenge: %w", update.Error)
	}
	if update.RowsAffected != 1 {
		return fmt.Errorf("fido2 challenge not consumable")
	}
	return nil
}

func fido2CredentialFromRow(row fido2CredentialRow) fido2.Credential {
	var transports []string
	_ = json.Unmarshal([]byte(row.Transports), &transports)
	if transports == nil {
		transports = []string{}
	}
	credential := fido2.Credential{
		CredentialID: append([]byte(nil), row.CredentialID...),
		RPID:         row.RPID,
		UserHandle:   append([]byte(nil), row.UserHandle...),
		PublicKey:    append([]byte(nil), row.PublicKey...),
		Algorithm:    row.Algorithm,
		SignCount:    row.SignCount,
		Transports:   transports,
		CreatedAt:    row.CreatedAtMS,
	}
	if row.LastUsedAtMS != nil {
		usedAt := *row.LastUsedAtMS
		credential.LastUsedAt = &usedAt
	}
	return credential
}
