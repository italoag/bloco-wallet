package storage

import (
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// createSQLiteDialector cria o dialector SQLite apropriado para o ambiente
func createSQLiteDialector(dbPath string) gorm.Dialector {
	return sqlite.Open(dbPath)
}