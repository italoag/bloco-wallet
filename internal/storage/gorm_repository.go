package storage

import (
	"blocowallet/internal/wallet"
	"blocowallet/pkg/config"
	"fmt"
	"os"
	"path/filepath"

	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// GORMRepository implementa a interface WalletRepository usando GORM
type GORMRepository struct {
	db *gorm.DB
}

// Garantimos que GORMRepository implementa a interface WalletRepository
var _ wallet.WalletRepository = &GORMRepository{}

// NewWalletRepository cria uma nova instância de GORMRepository com base na configuração
func NewWalletRepository(cfg *config.Config) (*GORMRepository, error) {
	// Usar apenas SQLite para testes e desenvolvimento
	dbPath := cfg.DatabasePath
	if cfg.Database.DSN != "" {
		dbPath = cfg.Database.DSN
	}

	// Garantir que o diretório existe
	dir := filepath.Dir(dbPath)
	if err := ensureDir(dir); err != nil {
		return nil, fmt.Errorf("falha ao criar diretório para o banco de dados: %w", err)
	}

	// Usar o driver SQLite apropriado para o ambiente
	dialector := createSQLiteDialector(dbPath)

	db, err := gorm.Open(dialector, &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		return nil, fmt.Errorf("falha ao conectar ao banco de dados: %w", err)
	}

	// Auto Migrate cria a tabela se não existir
	err = db.AutoMigrate(&wallet.Wallet{})
	if err != nil {
		return nil, fmt.Errorf("falha ao migrar tabela de carteiras: %w", err)
	}

	return &GORMRepository{db: db}, nil
}

// ensureDir garante que o diretório existe
func ensureDir(dir string) error {
	return os.MkdirAll(dir, os.ModePerm)
}

// AddWallet adiciona uma nova carteira ao banco de dados
func (repo *GORMRepository) AddWallet(wallet *wallet.Wallet) error {
	return repo.db.Create(wallet).Error
}

// GetAllWallets retorna todas as carteiras salvas
func (repo *GORMRepository) GetAllWallets() ([]wallet.Wallet, error) {
	var wallets []wallet.Wallet
	result := repo.db.Find(&wallets)
	return wallets, result.Error
}

// DeleteWallet remove uma carteira pelo ID
func (repo *GORMRepository) DeleteWallet(walletID int) error {
	return repo.db.Delete(&wallet.Wallet{}, walletID).Error
}

// FindBySourceHash finds a wallet by its source hash
func (repo *GORMRepository) FindBySourceHash(sourceHash string) (*wallet.Wallet, error) {
	var w wallet.Wallet
	result := repo.db.Where("source_hash = ?", sourceHash).First(&w)
	if result.Error != nil {
		if result.Error == gorm.ErrRecordNotFound {
			return nil, nil // Return nil if not found, not an error
		}
		return nil, result.Error
	}
	return &w, nil
}

// FindByAddress returns all wallets that match the given address (may be multiple)
func (repo *GORMRepository) FindByAddress(address string) ([]wallet.Wallet, error) {
	var wallets []wallet.Wallet
	result := repo.db.Where("address = ?", address).Find(&wallets)
	return wallets, result.Error
}

// FindByAddressAndMethod returns wallets filtered by address and import method
func (repo *GORMRepository) FindByAddressAndMethod(address, importMethod string) ([]wallet.Wallet, error) {
	var wallets []wallet.Wallet
	result := repo.db.Where("address = ? AND import_method = ?", address, importMethod).Find(&wallets)
	return wallets, result.Error
}

// Close fecha a conexão com o banco de dados
func (repo *GORMRepository) Close() error {
	sqlDB, err := repo.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}
