package storage

import (
	"fmt"
	"time"

	"blocowallet/internal/wallet"

	"gorm.io/gorm"
)

type schemaMigration struct {
	Version   uint      `gorm:"primaryKey"`
	AppliedAt time.Time `gorm:"not null"`
}

func (schemaMigration) TableName() string {
	return "schema_migrations"
}

func configureSQLite(database *gorm.DB, disk bool) error {
	sqlDatabase, err := database.DB()
	if err != nil {
		return err
	}
	sqlDatabase.SetMaxOpenConns(1)
	sqlDatabase.SetMaxIdleConns(1)
	pragmas := []string{
		"PRAGMA foreign_keys = ON",
		"PRAGMA busy_timeout = 5000",
		"PRAGMA synchronous = FULL",
	}
	if disk {
		pragmas = append(pragmas, "PRAGMA journal_mode = WAL")
	}
	for _, pragma := range pragmas {
		if err := database.Exec(pragma).Error; err != nil {
			return fmt.Errorf("apply %s: %w", pragma, err)
		}
	}
	return nil
}

func runMigrations(database *gorm.DB, includeLegacy bool) error {
	if err := database.AutoMigrate(&schemaMigration{}); err != nil {
		return fmt.Errorf("create schema migration table: %w", err)
	}
	return database.Transaction(func(transaction *gorm.DB) error {
		var current uint
		if err := transaction.Model(&schemaMigration{}).Select("COALESCE(MAX(version), 0)").Scan(&current).Error; err != nil {
			return err
		}
		if current > 2 {
			return fmt.Errorf("database schema version %d is newer than supported version 2", current)
		}
		migrations := []struct {
			version uint
			apply   func(*gorm.DB) error
		}{
			{version: 1, apply: func(tx *gorm.DB) error {
				if !includeLegacy {
					return nil
				}
				return tx.AutoMigrate(&wallet.Wallet{})
			}},
			{version: 2, apply: func(tx *gorm.DB) error { return tx.AutoMigrate(&wallet.Account{}) }},
		}
		for _, migration := range migrations {
			if migration.version <= current {
				continue
			}
			if err := migration.apply(transaction); err != nil {
				return fmt.Errorf("apply schema migration %d: %w", migration.version, err)
			}
			if err := transaction.Create(&schemaMigration{Version: migration.version, AppliedAt: time.Now().UTC()}).Error; err != nil {
				return fmt.Errorf("record schema migration %d: %w", migration.version, err)
			}
		}
		return nil
	})
}
