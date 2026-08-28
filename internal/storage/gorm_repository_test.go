package storage

import (
	"blocowallet/internal/wallet"
	"blocowallet/pkg/config"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestConfig(t *testing.T) *config.Config {
	// Criando um diretório temporário para o teste
	tempDir, err := os.MkdirTemp("", "wallet_test")
	require.NoError(t, err)

	// Limpeza após o teste
	t.Cleanup(func() {
		err := os.RemoveAll(tempDir)
		if err != nil {
			return
		}
	})

	// Configuração para teste com SQLite em memória
	return &config.Config{
		AppDir:       tempDir,
		DatabasePath: tempDir + "/test.db",
		Database: config.DatabaseConfig{
			Type: "sqlite",
			DSN:  ":memory:", // SQLite em memória
		},
	}
}

func TestRepositoryResolvesDSNCredentialReference(t *testing.T) {
	t.Setenv("BLOCO_TEST_DATABASE_DSN", "file::memory:?cache=shared")
	repository, err := NewVaultRepository(&config.Config{
		DatabasePath: filepath.Join(t.TempDir(), "fallback.db"),
		Database:     config.DatabaseConfig{Type: "sqlite", DSNRef: "env:BLOCO_TEST_DATABASE_DSN"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = repository.Close() }()
	if _, err := NewVaultRepository(&config.Config{
		DatabasePath: filepath.Join(t.TempDir(), "fallback.db"),
		Database:     config.DatabaseConfig{Type: "sqlite", DSNRef: "env:BLOCO_MISSING_DATABASE_DSN"},
	}); err == nil {
		t.Fatal("missing DSN reference silently fell back to database path")
	}
}

func TestNewWalletRepository(t *testing.T) {
	cfg := setupTestConfig(t)

	repo, err := NewWalletRepository(cfg)
	require.NoError(t, err)
	require.NotNil(t, repo)

	// Testando a conexão
	err = repo.Close()
	require.NoError(t, err)
}

func TestGORMRepository_AddWallet(t *testing.T) {
	cfg := setupTestConfig(t)

	repo, err := NewWalletRepository(cfg)
	require.NoError(t, err)
	defer func(repo *GORMRepository) {
		err := repo.Close()
		if err != nil {
			t.Errorf("Erro ao fechar o repositório: %v", err)
		}
	}(repo)

	// Criando uma carteira para teste
	mn := "test mnemonic"
	testWallet := &wallet.Wallet{
		Address:      "0x123456",
		KeyStorePath: "/path/to/keystore",
		Mnemonic:     &mn,
		ImportMethod: string(wallet.ImportMethodMnemonic),
		SourceHash:   (&wallet.SourceHashGenerator{}).GenerateFromMnemonic(mn),
	}

	// Adicionando a carteira
	err = repo.AddWallet(testWallet)
	assert.NoError(t, err)
	assert.NotZero(t, testWallet.ID, "ID da carteira deveria ser definido após a inserção")

	// Verificando se a carteira foi salva recuperando todas as carteiras
	wallets, err := repo.GetAllWallets()
	assert.NoError(t, err)
	assert.Len(t, wallets, 1)
	assert.Equal(t, testWallet.Address, wallets[0].Address)
	assert.Equal(t, testWallet.KeyStorePath, wallets[0].KeyStorePath)
	assert.Equal(t, testWallet.Mnemonic, wallets[0].Mnemonic)
}

func TestGORMRepository_GetAllWallets(t *testing.T) {
	cfg := setupTestConfig(t)

	repo, err := NewWalletRepository(cfg)
	require.NoError(t, err)
	defer func(repo *GORMRepository) {
		err := repo.Close()
		if err != nil {
			t.Errorf("Erro ao fechar o repositório: %v", err)
		}
	}(repo)

	// Inicialmente não deve haver carteiras
	wallets, err := repo.GetAllWallets()
	assert.NoError(t, err)
	assert.Empty(t, wallets)

	// Adicionando algumas carteiras para teste
	mn1 := "test mnemonic 1"
	mn2 := "test mnemonic 2"
	testWallets := []*wallet.Wallet{
		{
			Address:      "0x111111",
			KeyStorePath: "/path/to/keystore1",
			Mnemonic:     &mn1,
			ImportMethod: string(wallet.ImportMethodMnemonic),
			SourceHash:   (&wallet.SourceHashGenerator{}).GenerateFromMnemonic(mn1),
		},
		{
			Address:      "0x222222",
			KeyStorePath: "/path/to/keystore2",
			Mnemonic:     &mn2,
			ImportMethod: string(wallet.ImportMethodMnemonic),
			SourceHash:   (&wallet.SourceHashGenerator{}).GenerateFromMnemonic(mn2),
		},
	}

	for _, w := range testWallets {
		err = repo.AddWallet(w)
		require.NoError(t, err)
	}

	// Verificando se todas as carteiras foram recuperadas
	wallets, err = repo.GetAllWallets()
	assert.NoError(t, err)
	assert.Len(t, wallets, 2)
}

func TestGORMRepository_DeleteWallet(t *testing.T) {
	cfg := setupTestConfig(t)

	repo, err := NewWalletRepository(cfg)
	require.NoError(t, err)
	defer func(repo *GORMRepository) {
		err := repo.Close()
		if err != nil {
			t.Errorf("Erro ao fechar o repositório: %v", err)
		}
	}(repo)

	// Adicionando uma carteira para teste
	mnDel := "test mnemonic"
	testWallet := &wallet.Wallet{
		Address:      "0x123456",
		KeyStorePath: "/path/to/keystore",
		Mnemonic:     &mnDel,
		ImportMethod: string(wallet.ImportMethodMnemonic),
		SourceHash:   (&wallet.SourceHashGenerator{}).GenerateFromMnemonic(mnDel),
	}

	err = repo.AddWallet(testWallet)
	require.NoError(t, err)
	require.NotZero(t, testWallet.ID)

	// Deletando a carteira
	err = repo.DeleteWallet(testWallet.ID)
	assert.NoError(t, err)

	// Verificando se a carteira foi removida
	wallets, err := repo.GetAllWallets()
	assert.NoError(t, err)
	assert.Empty(t, wallets)
}

// Teste para verificar o comportamento com diferentes configurações SQLite
func TestGORMRepository_SQLiteConfigurations(t *testing.T) {
	testCases := []struct {
		name    string
		dsn     string
		wantErr bool
	}{
		{
			name:    "SQLite em memória",
			dsn:     ":memory:",
			wantErr: false,
		},
		{
			name:    "SQLite em arquivo",
			dsn:     "", // Usar DatabasePath
			wantErr: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tempDir, err := os.MkdirTemp("", "wallet_test")
			require.NoError(t, err)
			defer func(path string) {
				err := os.RemoveAll(path)
				if err != nil {
					t.Errorf("Erro ao remover diretório temporário: %v", err)
				}
			}(tempDir)

			cfg := &config.Config{
				AppDir:       tempDir,
				DatabasePath: tempDir + "/test.db",
				Database: config.DatabaseConfig{
					Type: "sqlite",
					DSN:  tc.dsn,
				},
			}

			repo, err := NewWalletRepository(cfg)
			if tc.wantErr {
				assert.Error(t, err)
				assert.Nil(t, repo)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, repo)
				// Fechar a conexão se não for nil
				if repo != nil {
					err := repo.Close()
					assert.NoError(t, err)
				}
				if tc.dsn == "" && runtime.GOOS != "windows" {
					info, statErr := os.Stat(cfg.DatabasePath)
					require.NoError(t, statErr)
					assert.Equal(t, os.FileMode(0600), info.Mode().Perm())
				}
			}
		})
	}
}

func TestGORMRepository_FileURIPermissions(t *testing.T) {
	tempDir := t.TempDir()
	if runtime.GOOS != "windows" {
		require.NoError(t, os.Chmod(tempDir, 0755))
	}
	databasePath := filepath.Join(tempDir, "uri.db")
	dsn := (&url.URL{Scheme: "file", Path: databasePath}).String()
	cfg := &config.Config{
		AppDir:       tempDir,
		DatabasePath: databasePath,
		Database:     config.DatabaseConfig{Type: "sqlite", DSN: dsn},
	}

	repo, err := NewWalletRepository(cfg)
	require.NoError(t, err)
	require.NoError(t, repo.Close())

	assert.FileExists(t, databasePath)
	if runtime.GOOS != "windows" {
		dirInfo, statErr := os.Stat(tempDir)
		require.NoError(t, statErr)
		assert.Equal(t, os.FileMode(0700), dirInfo.Mode().Perm())
		fileInfo, statErr := os.Stat(databasePath)
		require.NoError(t, statErr)
		assert.Equal(t, os.FileMode(0600), fileInfo.Mode().Perm())
	}
}

func TestGORMRepository_RejectsInsecureExternalDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission semantics")
	}
	appDir := t.TempDir()
	externalDir := t.TempDir()
	require.NoError(t, os.Chmod(externalDir, 0755))
	databasePath := filepath.Join(externalDir, "external.db")
	cfg := &config.Config{
		AppDir:       appDir,
		DatabasePath: databasePath,
		Database: config.DatabaseConfig{
			Type: "sqlite",
			DSN:  (&url.URL{Scheme: "file", Path: databasePath}).String(),
		},
	}

	repo, err := NewWalletRepository(cfg)

	assert.Error(t, err)
	assert.Nil(t, repo)
	info, statErr := os.Stat(externalDir)
	require.NoError(t, statErr)
	assert.Equal(t, os.FileMode(0755), info.Mode().Perm())
	assert.NoFileExists(t, databasePath)
}

func TestGORMRepository_FindBySourceHash_And_AddressQueries(t *testing.T) {
	cfg := setupTestConfig(t)

	repo, err := NewWalletRepository(cfg)
	require.NoError(t, err)
	defer func(repo *GORMRepository) {
		_ = repo.Close()
	}(repo)

	addr := "0xABCDEF"
	mnA := "alpha phrase"
	mnB := "beta phrase"
	hashA := (&wallet.SourceHashGenerator{}).GenerateFromMnemonic(mnA)
	hashB := (&wallet.SourceHashGenerator{}).GenerateFromMnemonic(mnB)

	wA := &wallet.Wallet{Address: addr, KeyStorePath: "/tmp/a.json", Mnemonic: &mnA, ImportMethod: string(wallet.ImportMethodMnemonic), SourceHash: hashA}
	wB := &wallet.Wallet{Address: addr, KeyStorePath: "/tmp/b.json", Mnemonic: &mnB, ImportMethod: string(wallet.ImportMethodMnemonic), SourceHash: hashB}
	wC := &wallet.Wallet{Address: addr, KeyStorePath: "/tmp/c.json", Mnemonic: nil, ImportMethod: string(wallet.ImportMethodPrivateKey), SourceHash: (&wallet.SourceHashGenerator{}).GenerateFromPrivateKey("deadbeef")}

	for _, w := range []*wallet.Wallet{wA, wB, wC} {
		require.NoError(t, repo.AddWallet(w))
	}

	// FindBySourceHash
	gotA, err := repo.FindBySourceHash(hashA)
	assert.NoError(t, err)
	require.NotNil(t, gotA)
	assert.Equal(t, wA.ID, gotA.ID)

	gotNil, err := repo.FindBySourceHash("nonexistent")
	assert.NoError(t, err)
	assert.Nil(t, gotNil)

	// FindByAddress should return 3 wallets (same address, different sources/methods)
	list, err := repo.FindByAddress(addr)
	assert.NoError(t, err)
	assert.Len(t, list, 3)

	// FindByAddressAndMethod should filter by method
	listMnemonic, err := repo.FindByAddressAndMethod(addr, string(wallet.ImportMethodMnemonic))
	assert.NoError(t, err)
	assert.Len(t, listMnemonic, 2)

	listPriv, err := repo.FindByAddressAndMethod(addr, string(wallet.ImportMethodPrivateKey))
	assert.NoError(t, err)
	assert.Len(t, listPriv, 1)
}
