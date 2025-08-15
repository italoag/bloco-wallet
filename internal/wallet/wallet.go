package wallet

import "time"

// Wallet representa uma carteira de criptomoeda
type Wallet struct {
	ID           int       `gorm:"primaryKey"`
	Name         string    `gorm:"not null"`
	Address      string    `gorm:"index;not null"`              // changed from uniqueIndex to regular index
	KeyStorePath string    `gorm:"not null"`
	Mnemonic     *string   `gorm:"type:text"`                   // nullable to support non-mnemonic imports
	ImportMethod string    `gorm:"not null"`                    // import method: mnemonic, private_key, keystore
	SourceHash   string    `gorm:"uniqueIndex;not null"`        // unique hash of source data
	CreatedAt    time.Time `gorm:"not null;autoCreateTime"`
}

// TableName define o nome da tabela no banco de dados
func (Wallet) TableName() string {
	return "wallets"
}
