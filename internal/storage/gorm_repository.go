package storage

import (
	"blocowallet/internal/wallet"
	"blocowallet/pkg/config"
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// GORMRepository implementa a interface WalletRepository usando GORM
type GORMRepository struct {
	db *gorm.DB
}

// Garantimos que GORMRepository implementa a interface WalletRepository
var _ wallet.WalletRepository = &GORMRepository{}
var _ wallet.AccountRepository = &GORMRepository{}

// NewWalletRepository cria uma nova instância de GORMRepository com base na configuração
func NewWalletRepository(cfg *config.Config) (*GORMRepository, error) {
	return newRepository(cfg, true)
}

func NewVaultRepository(cfg *config.Config) (*GORMRepository, error) {
	return newRepository(cfg, false)
}

func newRepository(cfg *config.Config, includeLegacy bool) (*GORMRepository, error) {
	// Usar apenas SQLite para testes e desenvolvimento
	dbPath := cfg.DatabasePath
	if cfg.Database.DSN != "" {
		dbPath = cfg.Database.DSN
	}

	diskPath, isDisk, err := sqliteDiskPath(dbPath)
	if err != nil {
		return nil, fmt.Errorf("caminho SQLite inválido: %w", err)
	}
	// Garantir que o diretório existe
	if isDisk {
		dir := filepath.Dir(diskPath)
		if err := ensurePrivateDatabaseDir(dir, cfg.AppDir); err != nil {
			return nil, fmt.Errorf("falha ao proteger diretório para o banco de dados: %w", err)
		}
		file, err := os.OpenFile(diskPath, os.O_CREATE|os.O_RDWR, 0600)
		if err != nil {
			return nil, fmt.Errorf("falha ao criar arquivo do banco de dados: %w", err)
		}
		if err := file.Close(); err != nil {
			return nil, fmt.Errorf("falha ao fechar arquivo do banco de dados: %w", err)
		}
		if err := os.Chmod(diskPath, 0600); err != nil {
			return nil, fmt.Errorf("falha ao proteger arquivo do banco de dados: %w", err)
		}
	}

	// Usar o driver SQLite apropriado para o ambiente
	dialector := createSQLiteDialector(dbPath)

	db, err := gorm.Open(dialector, &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		return nil, fmt.Errorf("falha ao conectar ao banco de dados: %w", err)
	}
	keepOpen := false
	defer func() {
		if !keepOpen {
			if sqlDatabase, closeErr := db.DB(); closeErr == nil {
				_ = sqlDatabase.Close()
			}
		}
	}()

	if err := configureSQLite(db, isDisk); err != nil {
		return nil, fmt.Errorf("falha ao configurar SQLite: %w", err)
	}
	if err := runMigrations(db, includeLegacy); err != nil {
		return nil, fmt.Errorf("falha ao migrar banco de dados: %w", err)
	}
	if !includeLegacy && db.Migrator().HasTable(&wallet.Wallet{}) {
		var legacyCount int64
		if err := db.Model(&wallet.Wallet{}).Count(&legacyCount).Error; err != nil {
			return nil, fmt.Errorf("falha ao verificar dados legados: %w", err)
		}
		if legacyCount > 0 {
			return nil, fmt.Errorf("legacy wallet data requires explicit migration")
		}
	}
	if isDisk {
		for _, path := range []string{diskPath, diskPath + "-wal", diskPath + "-shm"} {
			if err := os.Chmod(path, 0600); err != nil && !os.IsNotExist(err) {
				return nil, fmt.Errorf("falha ao proteger arquivo do banco de dados: %w", err)
			}
		}
	}

	keepOpen = true
	return &GORMRepository{db: db}, nil
}

func sqliteDiskPath(dsn string) (string, bool, error) {
	if dsn == "" || dsn == ":memory:" {
		return "", false, nil
	}
	if strings.HasPrefix(dsn, "file:") {
		parsed, err := url.Parse(dsn)
		if err != nil {
			return "", false, err
		}
		path := parsed.Opaque
		if path == "" {
			path = parsed.Path
		}
		if path == ":memory:" || parsed.Query().Get("mode") == "memory" {
			return "", false, nil
		}
		if path == "" {
			return "", false, fmt.Errorf("URI SQLite sem caminho")
		}
		decoded, err := url.PathUnescape(path)
		if err != nil {
			return "", false, err
		}
		return filepath.Clean(decoded), true, nil
	}
	path := strings.SplitN(dsn, "?", 2)[0]
	if path == "" {
		return "", false, fmt.Errorf("DSN SQLite sem caminho")
	}
	return filepath.Clean(path), true, nil
}

func ensurePrivateDatabaseDir(dir, appDir string) error {
	if !filepath.IsAbs(dir) {
		return fmt.Errorf("database directory must be absolute")
	}
	info, err := os.Lstat(dir)
	if os.IsNotExist(err) {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return err
		}
		return os.Chmod(dir, 0700)
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("database directory must be a regular directory")
	}
	if info.Mode().Perm()&0077 == 0 {
		return nil
	}
	appRoot, err := filepath.Abs(appDir)
	if err != nil || appDir == "" {
		return fmt.Errorf("database directory is not private")
	}
	dirPath, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	relative, err := filepath.Rel(appRoot, dirPath)
	if err != nil || filepath.IsAbs(relative) || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return fmt.Errorf("external database directory is not private")
	}
	return os.Chmod(dirPath, 0700)
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

func (repo *GORMRepository) CreateAccount(ctx context.Context, account *wallet.Account) error {
	if err := account.Validate(); err != nil {
		return err
	}
	if account.Revision == 0 {
		account.Revision = 1
	}
	result := repo.db.WithContext(ctx).Create(account)
	return normalizeAccountError(result.Error)
}

func (repo *GORMRepository) GetAccount(ctx context.Context, accountID string) (*wallet.Account, error) {
	var account wallet.Account
	result := repo.db.WithContext(ctx).Where("account_id = ?", accountID).First(&account)
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return nil, wallet.ErrAccountNotFound
	}
	if result.Error != nil {
		return nil, result.Error
	}
	return &account, nil
}

func (repo *GORMRepository) FindAccountBySourceIdentity(ctx context.Context, sourceIdentity string) (*wallet.Account, error) {
	var account wallet.Account
	result := repo.db.WithContext(ctx).Where("source_identity = ?", sourceIdentity).First(&account)
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return nil, wallet.ErrAccountNotFound
	}
	if result.Error != nil {
		return nil, result.Error
	}
	return &account, nil
}

func (repo *GORMRepository) ListAccounts(ctx context.Context) ([]wallet.Account, error) {
	var accounts []wallet.Account
	result := repo.db.WithContext(ctx).Order("created_at ASC, account_id ASC").Find(&accounts)
	return accounts, result.Error
}

func (repo *GORMRepository) UpdateAccount(ctx context.Context, account *wallet.Account) error {
	if err := account.Validate(); err != nil {
		return err
	}
	if account.Revision == 0 {
		return wallet.ErrAccountRevisionConflict
	}
	nextRevision := account.Revision + 1
	result := repo.db.WithContext(ctx).
		Model(&wallet.Account{}).
		Where("account_id = ? AND revision = ?", account.AccountID, account.Revision).
		Updates(map[string]any{
			"name":                account.Name,
			"state":               account.State,
			"secret_envelope":     append([]byte(nil), account.SecretEnvelope...),
			"envelope_generation": account.EnvelopeGeneration,
			"authorization_epoch": account.AuthorizationEpoch,
			"backup_generation":   account.BackupGeneration,
			"capabilities":        account.Capabilities,
			"updated_at":          account.UpdatedAt,
			"revision":            nextRevision,
		})
	if result.Error != nil {
		return normalizeAccountError(result.Error)
	}
	if result.RowsAffected != 1 {
		return wallet.ErrAccountRevisionConflict
	}
	account.Revision = nextRevision
	return nil
}

func (repo *GORMRepository) DeletePendingAccount(ctx context.Context, accountID string, backupGeneration uint64) error {
	result := repo.db.WithContext(ctx).
		Where("account_id = ? AND state = ? AND backup_generation = ?", accountID, wallet.AccountStatePendingBackup, backupGeneration).
		Delete(&wallet.Account{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return wallet.ErrAccountNotFound
	}
	return nil
}

func (repo *GORMRepository) WithAccountTransaction(ctx context.Context, operation func(wallet.AccountRepository) error) error {
	if operation == nil {
		return fmt.Errorf("account transaction operation is required")
	}
	return repo.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return operation(&GORMRepository{db: tx})
	})
}

func normalizeAccountError(err error) error {
	if err == nil {
		return nil
	}
	message := strings.ToLower(err.Error())
	if errors.Is(err, gorm.ErrDuplicatedKey) || strings.Contains(message, "unique constraint") || strings.Contains(message, "duplicate") {
		return fmt.Errorf("%w: %v", wallet.ErrAccountConflict, err)
	}
	return err
}

// Close fecha a conexão com o banco de dados
func (repo *GORMRepository) Close() error {
	sqlDB, err := repo.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}
