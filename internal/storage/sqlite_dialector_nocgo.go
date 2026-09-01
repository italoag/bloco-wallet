//go:build !cgo

package storage

import (
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	_ "modernc.org/sqlite"
)

func createSQLiteDialector(dbPath string) gorm.Dialector {
	return sqlite.New(sqlite.Config{DriverName: "sqlite", DSN: dbPath})
}
