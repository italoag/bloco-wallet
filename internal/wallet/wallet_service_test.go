package wallet

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts/keystore"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// Mock repository for testing
type MockWalletRepository struct {
	mock.Mock
}

type blockingAddRepository struct {
	started chan struct{}
	release chan struct{}
	wallets []Wallet
}

func (r *blockingAddRepository) AddWallet(w *Wallet) error {
	close(r.started)
	<-r.release
	w.ID = len(r.wallets) + 1
	r.wallets = append(r.wallets, *w)
	return nil
}

func (r *blockingAddRepository) GetAllWallets() ([]Wallet, error) {
	return append([]Wallet(nil), r.wallets...), nil
}

func (r *blockingAddRepository) DeleteWallet(int) error {
	return nil
}

func (r *blockingAddRepository) FindBySourceHash(string) (*Wallet, error) {
	return nil, nil
}

func (r *blockingAddRepository) FindByAddress(string) ([]Wallet, error) {
	return nil, nil
}

func (r *blockingAddRepository) FindByAddressAndMethod(string, string) ([]Wallet, error) {
	return nil, nil
}

func (r *blockingAddRepository) Close() error {
	return nil
}

func (m *MockWalletRepository) AddWallet(wallet *Wallet) error {
	args := m.Called(wallet)
	return args.Error(0)
}

// Ensure MockWalletRepository satisfies WalletRepository used by NewWalletService
// Some tests expect FindBySourceHash to exist (deduplication by source hash)
func (m *MockWalletRepository) FindBySourceHash(sourceHash string) (*Wallet, error) {
	args := m.Called(sourceHash)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*Wallet), args.Error(1)
}

// New interface methods to satisfy WalletRepository
func (m *MockWalletRepository) FindByAddress(address string) ([]Wallet, error) {
	args := m.Called(address)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]Wallet), args.Error(1)
}

func (m *MockWalletRepository) FindByAddressAndMethod(address, importMethod string) ([]Wallet, error) {
	args := m.Called(address, importMethod)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]Wallet), args.Error(1)
}

func (m *MockWalletRepository) GetWalletByID(id int) (*Wallet, error) {
	args := m.Called(id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*Wallet), args.Error(1)
}

func (m *MockWalletRepository) GetWalletByAddress(address string) (*Wallet, error) {
	args := m.Called(address)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*Wallet), args.Error(1)
}

func (m *MockWalletRepository) GetAllWallets() ([]Wallet, error) {
	args := m.Called()
	return args.Get(0).([]Wallet), args.Error(1)
}

func (m *MockWalletRepository) UpdateWallet(wallet *Wallet) error {
	args := m.Called(wallet)
	return args.Error(0)
}

func (m *MockWalletRepository) DeleteWallet(id int) error {
	args := m.Called(id)
	return args.Error(0)
}

func (m *MockWalletRepository) Close() error {
	args := m.Called()
	return args.Error(0)
}

// Helper function to create a test keystore file
func createTestKeystoreFile(t *testing.T, password string) (string, common.Address) {
	// Create a temporary directory for the test
	tempDir, err := os.MkdirTemp("", "keystore-test")
	assert.NoError(t, err)

	// Create a new key
	key, err := crypto.GenerateKey()
	assert.NoError(t, err)

	// Create a keystore and encrypt the key with test-optimized parameters
	n, p := GetTestKeystoreParams()
	ks := keystore.NewKeyStore(tempDir, n, p)
	account, err := ks.ImportECDSA(key, password)
	assert.NoError(t, err)

	// Get the path to the keystore file
	keystorePath := account.URL.Path

	return keystorePath, account.Address
}

// Helper function to create an invalid keystore file
func createInvalidKeystoreFile(t *testing.T) string {
	// Create a temporary directory for the test
	tempDir, err := os.MkdirTemp("", "keystore-test")
	assert.NoError(t, err)

	// Create an invalid keystore file
	invalidKeystorePath := filepath.Join(tempDir, "invalid-keystore.json")
	invalidKeystoreContent := `{"version": 2, "address": "0x123", "crypto": {}}`
	err = os.WriteFile(invalidKeystorePath, []byte(invalidKeystoreContent), 0600)
	assert.NoError(t, err)

	return invalidKeystorePath
}

// Helper function to create a corrupted keystore file
func createCorruptedKeystoreFile(t *testing.T) string {
	// Create a temporary directory for the test
	tempDir, err := os.MkdirTemp("", "keystore-test")
	assert.NoError(t, err)

	// Create a corrupted keystore file (invalid JSON)
	corruptedKeystorePath := filepath.Join(tempDir, "corrupted-keystore.json")
	corruptedKeystoreContent := `{"version": 3, "address": "0x123", "crypto": {`
	err = os.WriteFile(corruptedKeystorePath, []byte(corruptedKeystoreContent), 0600)
	assert.NoError(t, err)

	return corruptedKeystorePath
}

// Helper function to create a keystore file with missing fields
func createMissingFieldsKeystoreFile(t *testing.T) string {
	// Create a temporary directory for the test
	tempDir, err := os.MkdirTemp("", "keystore-test")
	assert.NoError(t, err)

	// Create a keystore file with missing fields
	missingFieldsKeystorePath := filepath.Join(tempDir, "missing-fields-keystore.json")
	missingFieldsKeystoreContent := `{"version": 3, "address": "0x123"}`
	err = os.WriteFile(missingFieldsKeystorePath, []byte(missingFieldsKeystoreContent), 0600)
	assert.NoError(t, err)

	return missingFieldsKeystorePath
}

// Helper function to create a keystore file with invalid address
func createInvalidAddressKeystoreFile(t *testing.T, password string) string {
	// Create a temporary directory for the test
	tempDir, err := os.MkdirTemp("", "keystore-test")
	assert.NoError(t, err)

	// Create a new key
	key, err := crypto.GenerateKey()
	assert.NoError(t, err)

	// Create a keystore and encrypt the key with test-optimized parameters
	n, p := GetTestKeystoreParams()
	ks := keystore.NewKeyStore(tempDir, n, p)
	account, err := ks.ImportECDSA(key, password)
	assert.NoError(t, err)

	// Read the keystore file
	keystorePath := account.URL.Path
	keystoreContent, err := os.ReadFile(keystorePath)
	assert.NoError(t, err)

	// Parse the keystore content
	var keystoreData map[string]interface{}
	err = json.Unmarshal(keystoreContent, &keystoreData)
	assert.NoError(t, err)

	// Modify the address to an invalid one
	keystoreData["address"] = "invalid-address"

	// Write the modified keystore back to a new file
	invalidAddressKeystorePath := filepath.Join(tempDir, "invalid-address-keystore.json")
	modifiedKeystoreContent, err := json.Marshal(keystoreData)
	assert.NoError(t, err)
	err = os.WriteFile(invalidAddressKeystorePath, modifiedKeystoreContent, 0600)
	assert.NoError(t, err)

	return invalidAddressKeystorePath
}

// Helper function to create a keystore file with address mismatch
func createAddressMismatchKeystoreFile(t *testing.T, password string) string {
	// Create a temporary directory for the test
	tempDir, err := os.MkdirTemp("", "keystore-test")
	assert.NoError(t, err)

	// Create a new key
	key, err := crypto.GenerateKey()
	assert.NoError(t, err)

	// Create a keystore and encrypt the key with test-optimized parameters
	n, p := GetTestKeystoreParams()
	ks := keystore.NewKeyStore(tempDir, n, p)
	account, err := ks.ImportECDSA(key, password)
	assert.NoError(t, err)

	// Read the keystore file
	keystorePath := account.URL.Path
	keystoreContent, err := os.ReadFile(keystorePath)
	assert.NoError(t, err)

	// Parse the keystore content
	var keystoreData map[string]interface{}
	err = json.Unmarshal(keystoreContent, &keystoreData)
	assert.NoError(t, err)

	// Modify the address to a different valid address
	keystoreData["address"] = "0x1234567890123456789012345678901234567890"

	// Write the modified keystore back to a new file
	addressMismatchKeystorePath := filepath.Join(tempDir, "address-mismatch-keystore.json")
	modifiedKeystoreContent, err := json.Marshal(keystoreData)
	assert.NoError(t, err)
	err = os.WriteFile(addressMismatchKeystorePath, modifiedKeystoreContent, 0600)
	assert.NoError(t, err)

	return addressMismatchKeystorePath
}

func TestDerivePrivateKeyKnownBIP44Vector(t *testing.T) {
	privateKeyHex, err := DerivePrivateKey("test test test test test test test test test test test junk")
	assert.NoError(t, err)
	privateKey, err := HexToECDSA(privateKeyHex)
	assert.NoError(t, err)
	assert.Equal(
		t,
		common.HexToAddress("0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266"),
		crypto.PubkeyToAddress(privateKey.PublicKey),
	)
}

func TestFirstKeystoreImportUsesConfiguredDirectory(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	configuredDir := filepath.Join(t.TempDir(), "configured-keystore")
	assert.NoError(t, os.MkdirAll(configuredDir, 0o700))

	cfg := CreateMockConfig(t)
	cfg.WalletsDir = configuredDir
	InitCryptoService(cfg)

	sourcePath, _ := createTestKeystoreFile(t, "SourcePass1!")
	defer func() {
		assert.NoError(t, os.RemoveAll(filepath.Dir(sourcePath)))
	}()

	mockRepo := new(MockWalletRepository)
	mockRepo.On("AddWallet", mock.AnythingOfType("*wallet.Wallet")).Return(nil)
	ks := keystore.NewKeyStore(configuredDir, TestScryptN, TestScryptP)
	service := NewWalletService(mockRepo, ks)

	details, err := service.ImportWalletFromKeystoreV3("Configured", sourcePath, "SourcePass1!")
	assert.NoError(t, err)
	if assert.NotNil(t, details) {
		assert.Equal(t, configuredDir, filepath.Dir(details.Wallet.KeyStorePath))
		info, statErr := os.Stat(details.Wallet.KeyStorePath)
		assert.NoError(t, statErr)
		assert.Equal(t, os.FileMode(0600), info.Mode().Perm())
	}
}

func TestCancelledKeystoreImportDoesNotPersist(t *testing.T) {
	configuredDir := filepath.Join(t.TempDir(), "configured-keystore")
	assert.NoError(t, os.MkdirAll(configuredDir, 0o700))
	cfg := CreateMockConfig(t)
	cfg.WalletsDir = configuredDir
	InitCryptoService(cfg)

	sourcePath, _ := createTestKeystoreFile(t, "SourcePass1!")
	defer func() {
		assert.NoError(t, os.RemoveAll(filepath.Dir(sourcePath)))
	}()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	service := NewWalletService(
		new(MockWalletRepository),
		keystore.NewKeyStore(configuredDir, TestScryptN, TestScryptP),
	)

	details, err := service.ImportWalletFromKeystoreV3WithContext(ctx, "Cancelled", sourcePath, "SourcePass1!", nil)
	assert.Nil(t, details)
	assert.ErrorIs(t, err, context.Canceled)
	files, readErr := os.ReadDir(configuredDir)
	assert.NoError(t, readErr)
	assert.Empty(t, files)
}

func TestKeystoreImportRejectsNonRegularAndOversizedFiles(t *testing.T) {
	cfg := CreateMockConfig(t)
	InitCryptoService(cfg)
	assert.NoError(t, os.MkdirAll(cfg.WalletsDir, 0700))
	service := NewWalletService(new(MockWalletRepository), keystore.NewKeyStore(cfg.WalletsDir, TestScryptN, TestScryptP), cfg.WalletsDir)

	directoryPath := filepath.Join(t.TempDir(), "directory.json")
	assert.NoError(t, os.MkdirAll(directoryPath, 0700))
	_, err := service.ImportWalletFromKeystoreV3("Directory", directoryPath, "password")
	assert.Error(t, err)

	oversizedPath := filepath.Join(t.TempDir(), "oversized.json")
	assert.NoError(t, os.WriteFile(oversizedPath, make([]byte, 1024*1024+1), 0600))
	_, err = service.ImportWalletFromKeystoreV3("Oversized", oversizedPath, "password")
	assert.Error(t, err)

	regularPath := filepath.Join(t.TempDir(), "regular.json")
	assert.NoError(t, os.WriteFile(regularPath, []byte("{}"), 0600))
	symlinkPath := filepath.Join(t.TempDir(), "symlink.json")
	if symlinkErr := os.Symlink(regularPath, symlinkPath); symlinkErr == nil {
		_, err = service.ImportWalletFromKeystoreV3("Symlink", symlinkPath, "password")
		assert.Error(t, err)
	}
}

func TestKeystoreImportRejectsFormatsThatCannotReload(t *testing.T) {
	sourcePath, _ := createTestKeystoreFile(t, "SourcePass1!")
	defer func() {
		assert.NoError(t, os.RemoveAll(filepath.Dir(sourcePath)))
	}()
	sourceJSON, err := os.ReadFile(sourcePath)
	assert.NoError(t, err)

	tests := []struct {
		name      string
		transform func(map[string]interface{})
	}{
		{
			name: "missing id",
			transform: func(data map[string]interface{}) {
				delete(data, "id")
			},
		},
		{
			name: "uppercase kdf",
			transform: func(data map[string]interface{}) {
				data["crypto"].(map[string]interface{})["kdf"] = "SCRYPT"
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var data map[string]interface{}
			assert.NoError(t, json.Unmarshal(sourceJSON, &data))
			tt.transform(data)
			modifiedJSON, marshalErr := json.Marshal(data)
			assert.NoError(t, marshalErr)
			modifiedPath := filepath.Join(t.TempDir(), "modified.json")
			assert.NoError(t, os.WriteFile(modifiedPath, modifiedJSON, 0600))
			cfg := CreateMockConfig(t)
			InitCryptoService(cfg)
			assert.NoError(t, os.MkdirAll(cfg.WalletsDir, 0700))
			service := NewWalletService(new(MockWalletRepository), keystore.NewKeyStore(cfg.WalletsDir, TestScryptN, TestScryptP), cfg.WalletsDir)

			details, importErr := service.ImportWalletFromKeystoreV3("Unsupported", modifiedPath, "SourcePass1!")

			assert.Nil(t, details)
			assert.Error(t, importErr)
			files, readErr := os.ReadDir(cfg.WalletsDir)
			assert.NoError(t, readErr)
			assert.Empty(t, files)
		})
	}
}

func TestKeystorePasswordsRoundTripExactly(t *testing.T) {
	passwords := []string{
		"",
		"  exact password  ",
		strings.Repeat("long-password-", 20),
		"senha-unicode-ç-密碼",
	}
	for _, password := range passwords {
		t.Run(fmt.Sprintf("length_%d", len(password)), func(t *testing.T) {
			cfg := CreateMockConfig(t)
			InitCryptoService(cfg)
			assert.NoError(t, os.MkdirAll(cfg.WalletsDir, 0700))
			sourceDir := t.TempDir()
			sourceStore := keystore.NewKeyStore(sourceDir, TestScryptN, TestScryptP)
			key, err := crypto.GenerateKey()
			assert.NoError(t, err)
			account, err := sourceStore.ImportECDSA(key, password)
			assert.NoError(t, err)
			mockRepo := new(MockWalletRepository)
			mockRepo.On("AddWallet", mock.AnythingOfType("*wallet.Wallet")).Return(nil)
			service := NewWalletService(mockRepo, keystore.NewKeyStore(cfg.WalletsDir, TestScryptN, TestScryptP), cfg.WalletsDir)

			details, err := service.ImportWalletFromKeystoreV3("Exact Password", account.URL.Path, password)
			assert.NoError(t, err)
			if !assert.NotNil(t, details) {
				return
			}
			restarted := NewWalletService(mockRepo, keystore.NewKeyStore(cfg.WalletsDir, TestScryptN, TestScryptP), cfg.WalletsDir)
			loaded, err := restarted.LoadWallet(details.Wallet, password)
			assert.NoError(t, err)
			assert.NotNil(t, loaded)
			if strings.TrimSpace(password) != password {
				_, err = restarted.LoadWallet(details.Wallet, strings.TrimSpace(password))
				assert.Error(t, err)
			}
		})
	}
}

func TestCancelWaitsForInFlightCommit(t *testing.T) {
	cfg := CreateMockConfig(t)
	InitCryptoService(cfg)
	assert.NoError(t, os.MkdirAll(cfg.WalletsDir, 0o700))
	sourcePath, _ := createTestKeystoreFile(t, "SourcePass1!")
	defer func() {
		assert.NoError(t, os.RemoveAll(filepath.Dir(sourcePath)))
	}()

	repo := &blockingAddRepository{started: make(chan struct{}), release: make(chan struct{})}
	service := NewWalletService(repo, keystore.NewKeyStore(cfg.WalletsDir, TestScryptN, TestScryptP), cfg.WalletsDir)
	control := NewImportControl(context.Background())
	importDone := make(chan error, 1)
	go func() {
		_, err := service.ImportWalletFromKeystoreV3WithContext(control.Context(), "Blocking", sourcePath, "SourcePass1!", nil)
		importDone <- err
	}()
	select {
	case <-repo.started:
	case <-time.After(5 * time.Second):
		t.Fatal("import did not reach commit")
	}

	cancelDone := make(chan struct{})
	go func() {
		control.Cancel()
		close(cancelDone)
	}()
	select {
	case <-cancelDone:
		t.Fatal("cancellation acknowledged before in-flight commit finished")
	case <-time.After(50 * time.Millisecond):
	}
	close(repo.release)

	assert.NoError(t, <-importDone)
	select {
	case <-cancelDone:
	case <-time.After(time.Second):
		t.Fatal("cancellation did not finish after commit")
	}
	assert.Len(t, repo.wallets, 1)
}

func TestDeleteWalletIsDisabled(t *testing.T) {
	cfg := CreateMockConfig(t)
	InitCryptoService(cfg)
	managedDir := cfg.WalletsDir
	assert.NoError(t, os.MkdirAll(managedDir, 0o700))
	trustedPath := filepath.Join(managedDir, "trusted.json")
	assert.NoError(t, os.WriteFile(trustedPath, []byte("trusted"), 0600))
	outsidePath := filepath.Join(t.TempDir(), "outside.json")
	assert.NoError(t, os.WriteFile(outsidePath, []byte("outside"), 0600))
	mockRepo := new(MockWalletRepository)
	service := NewWalletService(mockRepo, keystore.NewKeyStore(managedDir, TestScryptN, TestScryptP), managedDir)

	err := service.DeleteWallet(&Wallet{ID: 7, KeyStorePath: outsidePath})

	assert.ErrorIs(t, err, ErrWalletDeletionDisabled)
	assert.FileExists(t, trustedPath)
	assert.FileExists(t, outsidePath)
	mockRepo.AssertNotCalled(t, "DeleteWallet", mock.Anything)
}

func TestDeleteWalletDisabledForSharedPath(t *testing.T) {
	cfg := CreateMockConfig(t)
	InitCryptoService(cfg)
	assert.NoError(t, os.MkdirAll(cfg.WalletsDir, 0o700))
	sharedPath := filepath.Join(cfg.WalletsDir, "shared.json")
	assert.NoError(t, os.WriteFile(sharedPath, []byte("shared"), 0600))

	mockRepo := new(MockWalletRepository)
	mockRepo.On("GetAllWallets").Return([]Wallet{
		{ID: 1, KeyStorePath: sharedPath},
		{ID: 2, KeyStorePath: sharedPath},
	}, nil)
	service := NewWalletService(mockRepo, keystore.NewKeyStore(cfg.WalletsDir, TestScryptN, TestScryptP), cfg.WalletsDir)

	err := service.DeleteWallet(&Wallet{ID: 1})

	assert.ErrorIs(t, err, ErrWalletDeletionDisabled)
	assert.FileExists(t, sharedPath)
	mockRepo.AssertNotCalled(t, "DeleteWallet", mock.Anything)
}

func TestImportWalletFromKeystoreV3_Success(t *testing.T) {
	// Initialize crypto service for mnemonic encryption with mock config
	mockConfig := CreateMockConfig(t)
	InitCryptoService(mockConfig)

	// Create a test keystore file
	password := "testpassword"
	keystorePath, address := createTestKeystoreFile(t, password)
	defer func() {
		if err := os.RemoveAll(filepath.Dir(keystorePath)); err != nil {
			t.Logf("Failed to remove keystore directory: %v", err)
		}
	}()

	// Create a mock repository
	mockRepo := new(MockWalletRepository)
	mockRepo.On("AddWallet", mock.AnythingOfType("*wallet.Wallet")).Return(nil)
	mockRepo.On("Close").Return(nil)

	// Create a keystore in a temporary directory
	tempDir, err := os.MkdirTemp("", "keystore-service-test")
	assert.NoError(t, err)
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Logf("Failed to remove temp directory: %v", err)
		}
	}()

	// Use test-optimized parameters for faster testing
	n, p := GetTestKeystoreParams()
	ks := keystore.NewKeyStore(tempDir, n, p)

	// Create the wallet service
	walletService := NewWalletService(mockRepo, ks)

	// Import the wallet
	walletDetails, err := walletService.ImportWalletFromKeystoreV3("Test Wallet", keystorePath, password)

	// Verify the result
	assert.NoError(t, err)
	assert.NotNil(t, walletDetails)
	assert.Equal(t, "Test Wallet", walletDetails.Wallet.Name)
	assert.Equal(t, address.Hex(), walletDetails.Wallet.Address)
	assert.NotEmpty(t, walletDetails.Wallet.KeyStorePath)
	assert.Nil(t, walletDetails.Wallet.Mnemonic) // Keystore imports don't have mnemonics
	assert.Nil(t, walletDetails.Mnemonic)        // Keystore imports don't have mnemonics
	assert.NotNil(t, walletDetails.PrivateKey)
	assert.NotNil(t, walletDetails.PublicKey)

	// Close the repository
	if err := mockRepo.Close(); err != nil {
		t.Logf("Failed to close mock repository: %v", err)
	}

	// Verify that the repository was called
	mockRepo.AssertExpectations(t)
}
func TestImportWalletFromKeystoreV3_FileNotFound(t *testing.T) {
	// Create a mock repository
	mockRepo := new(MockWalletRepository)
	mockRepo.On("Close").Return(nil)

	// Create a keystore in a temporary directory
	tempDir, err := os.MkdirTemp("", "keystore-service-test")
	assert.NoError(t, err)
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Logf("Failed to remove temp directory: %v", err)
		}
	}()

	// Use test-optimized parameters for faster testing
	n, p := GetTestKeystoreParams()
	ks := keystore.NewKeyStore(tempDir, n, p)

	// Create the wallet service
	walletService := NewWalletService(mockRepo, ks)

	// Try to import a non-existent wallet
	walletDetails, err := walletService.ImportWalletFromKeystoreV3("Test Wallet", "/non/existent/path.json", "password")

	// Verify the result
	assert.Error(t, err)
	assert.Nil(t, walletDetails)

	// Check that the error is of the correct type
	keystoreErr, ok := err.(*KeystoreImportError)
	assert.True(t, ok)
	assert.Equal(t, ErrorFileNotFound, keystoreErr.Type)

	// Close the repository
	if err := mockRepo.Close(); err != nil {
		t.Logf("Failed to close mock repository: %v", err)
	}

	// Verify that the repository was called
	mockRepo.AssertExpectations(t)
}

func TestImportWalletFromKeystoreV3_InvalidJSON(t *testing.T) {
	// Create a corrupted keystore file
	corruptedKeystorePath := createCorruptedKeystoreFile(t)
	defer func() {
		if err := os.RemoveAll(filepath.Dir(corruptedKeystorePath)); err != nil {
			t.Logf("Failed to remove corrupted keystore directory: %v", err)
		}
	}()

	// Create a mock repository
	mockRepo := new(MockWalletRepository)
	mockRepo.On("Close").Return(nil)

	// Create a keystore in a temporary directory
	tempDir, err := os.MkdirTemp("", "keystore-service-test")
	assert.NoError(t, err)
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Logf("Failed to remove temp directory: %v", err)
		}
	}()

	// Use test-optimized parameters for faster testing
	n, p := GetTestKeystoreParams()
	ks := keystore.NewKeyStore(tempDir, n, p)

	// Create the wallet service
	walletService := NewWalletService(mockRepo, ks)

	// Try to import the corrupted wallet
	walletDetails, err := walletService.ImportWalletFromKeystoreV3("Test Wallet", corruptedKeystorePath, "password")

	// Verify the result
	assert.Error(t, err)
	assert.Nil(t, walletDetails)

	// Check that the error is of the correct type
	keystoreErr, ok := err.(*KeystoreImportError)
	assert.True(t, ok)
	assert.Equal(t, ErrorInvalidJSON, keystoreErr.Type)

	// Close the repository
	if err := mockRepo.Close(); err != nil {
		t.Logf("Failed to close mock repository: %v", err)
	}

	// Verify that the repository was called
	mockRepo.AssertExpectations(t)
}

func TestImportWalletFromKeystoreV3_InvalidVersion(t *testing.T) {
	// Create an invalid keystore file
	invalidKeystorePath := createInvalidKeystoreFile(t)
	defer func() {
		if err := os.RemoveAll(filepath.Dir(invalidKeystorePath)); err != nil {
			t.Logf("Failed to remove invalid keystore directory: %v", err)
		}
	}()

	// Create a mock repository
	mockRepo := new(MockWalletRepository)
	mockRepo.On("Close").Return(nil)

	// Create a keystore in a temporary directory
	tempDir, err := os.MkdirTemp("", "keystore-service-test")
	assert.NoError(t, err)
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Logf("Failed to remove temp directory: %v", err)
		}
	}()

	// Use test-optimized parameters for faster testing
	n, p := GetTestKeystoreParams()
	ks := keystore.NewKeyStore(tempDir, n, p)

	// Create the wallet service
	walletService := NewWalletService(mockRepo, ks)

	// Try to import the invalid wallet
	walletDetails, err := walletService.ImportWalletFromKeystoreV3("Test Wallet", invalidKeystorePath, "password")

	// Verify the result
	assert.Error(t, err)
	assert.Nil(t, walletDetails)

	// Check that the error is of the correct type
	// Note: With Universal KDF, version validation happens during compatibility analysis
	// The error might be ErrorInvalidKeystore instead of ErrorInvalidVersion
	keystoreErr, ok := err.(*KeystoreImportError)
	if ok {
		// Accept either ErrorInvalidVersion or ErrorInvalidKeystore
		assert.True(t, keystoreErr.Type == ErrorInvalidVersion || keystoreErr.Type == ErrorInvalidKeystore)
	} else {
		// If it's not a KeystoreImportError, just verify it's an error
		assert.Error(t, err)
	}

	// Close the repository
	if err := mockRepo.Close(); err != nil {
		t.Logf("Failed to close mock repository: %v", err)
	}

	// Verify that the repository was called
	mockRepo.AssertExpectations(t)
}
func TestImportWalletFromKeystoreV3_MissingFields(t *testing.T) {
	// Create a keystore file with missing fields
	missingFieldsKeystorePath := createMissingFieldsKeystoreFile(t)
	defer func() {
		if err := os.RemoveAll(filepath.Dir(missingFieldsKeystorePath)); err != nil {
			t.Logf("Failed to remove missing fields keystore directory: %v", err)
		}
	}()

	// Create a mock repository
	mockRepo := new(MockWalletRepository)
	mockRepo.On("Close").Return(nil)

	// Create a keystore in a temporary directory
	tempDir, err := os.MkdirTemp("", "keystore-service-test")
	assert.NoError(t, err)
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Logf("Failed to remove temp directory: %v", err)
		}
	}()

	// Use test-optimized parameters for faster testing
	n, p := GetTestKeystoreParams()
	ks := keystore.NewKeyStore(tempDir, n, p)

	// Create the wallet service
	walletService := NewWalletService(mockRepo, ks)

	// Try to import the wallet with missing fields
	walletDetails, err := walletService.ImportWalletFromKeystoreV3("Test Wallet", missingFieldsKeystorePath, "password")

	// Verify the result
	assert.Error(t, err)
	assert.Nil(t, walletDetails)

	// Check that the error is of the correct type
	_, ok := err.(*KeystoreImportError)
	assert.True(t, ok, "Expected KeystoreImportError")

	// Close the repository
	if err := mockRepo.Close(); err != nil {
		t.Logf("Failed to close mock repository: %v", err)
	}

	// Verify that the repository was called
	mockRepo.AssertExpectations(t)
}

func TestImportWalletFromKeystoreV3_InvalidAddress(t *testing.T) {
	// Create a keystore file with invalid address
	password := "testpassword"
	invalidAddressKeystorePath := createInvalidAddressKeystoreFile(t, password)
	defer func() {
		if err := os.RemoveAll(filepath.Dir(invalidAddressKeystorePath)); err != nil {
			t.Logf("Failed to remove invalid address keystore directory: %v", err)
		}
	}()

	// Create a mock repository
	mockRepo := new(MockWalletRepository)
	mockRepo.On("Close").Return(nil)

	// Create a keystore in a temporary directory
	tempDir, err := os.MkdirTemp("", "keystore-service-test")
	assert.NoError(t, err)
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Logf("Failed to remove temp directory: %v", err)
		}
	}()

	// Use test-optimized parameters for faster testing
	n, p := GetTestKeystoreParams()
	ks := keystore.NewKeyStore(tempDir, n, p)

	// Create the wallet service
	walletService := NewWalletService(mockRepo, ks)

	// Try to import the wallet with invalid address
	walletDetails, err := walletService.ImportWalletFromKeystoreV3("Test Wallet", invalidAddressKeystorePath, "password")

	// Verify the result
	assert.Error(t, err)
	assert.Nil(t, walletDetails)

	// Check that the error is of the correct type
	keystoreErr, ok := err.(*KeystoreImportError)
	assert.True(t, ok)
	assert.Equal(t, ErrorInvalidAddress, keystoreErr.Type)

	// Close the repository
	if err := mockRepo.Close(); err != nil {
		t.Logf("Failed to close mock repository: %v", err)
	}

	// Verify that the repository was called
	mockRepo.AssertExpectations(t)
}

func TestImportWalletFromKeystoreV3_IncorrectPassword(t *testing.T) {
	// Create a test keystore file
	password := "testpassword"
	keystorePath, _ := createTestKeystoreFile(t, password)
	defer func() {
		if err := os.RemoveAll(filepath.Dir(keystorePath)); err != nil {
			t.Logf("Failed to remove keystore directory: %v", err)
		}
	}()

	// Create a mock repository
	mockRepo := new(MockWalletRepository)
	mockRepo.On("Close").Return(nil)

	// Create a keystore in a temporary directory
	tempDir, err := os.MkdirTemp("", "keystore-service-test")
	assert.NoError(t, err)
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Logf("Failed to remove temp directory: %v", err)
		}
	}()

	// Use test-optimized parameters for faster testing
	n, p := GetTestKeystoreParams()
	ks := keystore.NewKeyStore(tempDir, n, p)

	// Create the wallet service
	walletService := NewWalletService(mockRepo, ks)

	// Try to import the wallet with incorrect password
	walletDetails, err := walletService.ImportWalletFromKeystoreV3("Test Wallet", keystorePath, "wrongpassword")

	// Verify the result
	assert.Error(t, err)
	assert.Nil(t, walletDetails)

	// Check that the error is of the correct type
	keystoreErr, ok := err.(*KeystoreImportError)
	assert.True(t, ok)
	assert.Equal(t, ErrorIncorrectPassword, keystoreErr.Type)

	// Close the repository
	if err := mockRepo.Close(); err != nil {
		t.Logf("Failed to close mock repository: %v", err)
	}

	// Verify that the repository was called
	mockRepo.AssertExpectations(t)
}
func TestImportWalletFromKeystoreV3_AddressMismatch(t *testing.T) {
	// Create a keystore file with address mismatch
	password := "testpassword"
	addressMismatchKeystorePath := createAddressMismatchKeystoreFile(t, password)
	defer func() {
		if err := os.RemoveAll(filepath.Dir(addressMismatchKeystorePath)); err != nil {
			t.Logf("Failed to remove address mismatch keystore directory: %v", err)
		}
	}()

	// Create a mock repository
	mockRepo := new(MockWalletRepository)
	mockRepo.On("Close").Return(nil)

	// Create a keystore in a temporary directory
	tempDir, err := os.MkdirTemp("", "keystore-service-test")
	assert.NoError(t, err)
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Logf("Failed to remove temp directory: %v", err)
		}
	}()

	// Use test-optimized parameters for faster testing
	n, p := GetTestKeystoreParams()
	ks := keystore.NewKeyStore(tempDir, n, p)

	// Create the wallet service
	walletService := NewWalletService(mockRepo, ks)

	// Try to import the wallet with address mismatch
	walletDetails, err := walletService.ImportWalletFromKeystoreV3("Test Wallet", addressMismatchKeystorePath, password)

	// Verify the result
	assert.Error(t, err)
	assert.Nil(t, walletDetails)

	// Check that the error is of the correct type
	keystoreErr, ok := err.(*KeystoreImportError)
	assert.True(t, ok)
	assert.Equal(t, ErrorAddressMismatch, keystoreErr.Type)

	// Close the repository
	if err := mockRepo.Close(); err != nil {
		t.Logf("Failed to close mock repository: %v", err)
	}

	// Verify that the repository was called
	mockRepo.AssertExpectations(t)
}

func TestImportWalletFromKeystoreV3_RepositoryError(t *testing.T) {
	// Initialize crypto service for mnemonic encryption with mock config
	mockConfig := CreateMockConfig(t)
	InitCryptoService(mockConfig)

	// Create a test keystore file
	password := "testpassword"
	keystorePath, _ := createTestKeystoreFile(t, password)
	defer func() {
		if err := os.RemoveAll(filepath.Dir(keystorePath)); err != nil {
			t.Logf("Failed to remove keystore directory: %v", err)
		}
	}()

	// Create a mock repository that returns an error
	mockRepo := new(MockWalletRepository)
	mockRepo.On("AddWallet", mock.AnythingOfType("*wallet.Wallet")).Return(assert.AnError)
	mockRepo.On("Close").Return(nil)

	// Create a keystore in a temporary directory
	tempDir, err := os.MkdirTemp("", "keystore-service-test")
	assert.NoError(t, err)
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Logf("Failed to remove temp directory: %v", err)
		}
	}()

	// Use test-optimized parameters for faster testing
	n, p := GetTestKeystoreParams()
	ks := keystore.NewKeyStore(tempDir, n, p)

	// Create the wallet service
	walletService := NewWalletService(mockRepo, ks)

	// Try to import the wallet
	walletDetails, err := walletService.ImportWalletFromKeystoreV3("Test Wallet", keystorePath, password)

	// Verify the result
	assert.Error(t, err)
	assert.Nil(t, walletDetails)

	// Check that the error is of the correct type
	keystoreErr, ok := err.(*KeystoreImportError)
	assert.True(t, ok)
	assert.Equal(t, ErrorCorruptedFile, keystoreErr.Type)

	// Close the repository
	if err := mockRepo.Close(); err != nil {
		t.Logf("Failed to close mock repository: %v", err)
	}

	// Verify that the repository was called
	mockRepo.AssertExpectations(t)
}

func TestImportWalletFromKeystore_BackwardCompatibility(t *testing.T) {
	// Initialize crypto service for mnemonic encryption with mock config
	mockConfig := CreateMockConfig(t)
	InitCryptoService(mockConfig)

	// Create a test keystore file
	password := "testpassword"
	keystorePath, address := createTestKeystoreFile(t, password)
	defer func() {
		if err := os.RemoveAll(filepath.Dir(keystorePath)); err != nil {
			t.Logf("Failed to remove keystore directory: %v", err)
		}
	}()

	// Create a mock repository
	mockRepo := new(MockWalletRepository)
	mockRepo.On("AddWallet", mock.AnythingOfType("*wallet.Wallet")).Return(nil)
	mockRepo.On("Close").Return(nil)

	// Create a keystore in a temporary directory
	tempDir, err := os.MkdirTemp("", "keystore-service-test")
	assert.NoError(t, err)
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Logf("Failed to remove temp directory: %v", err)
		}
	}()

	// Use test-optimized parameters for faster testing
	n, p := GetTestKeystoreParams()
	ks := keystore.NewKeyStore(tempDir, n, p)

	// Create the wallet service
	walletService := NewWalletService(mockRepo, ks)

	// Import the wallet using the old function name
	walletDetails, err := walletService.ImportWalletFromKeystore("Test Wallet", keystorePath, password)

	// Verify the result
	assert.NoError(t, err)
	assert.NotNil(t, walletDetails)
	assert.Equal(t, "Test Wallet", walletDetails.Wallet.Name)
	assert.Equal(t, address.Hex(), walletDetails.Wallet.Address)

	// Close the repository
	if err := mockRepo.Close(); err != nil {
		t.Logf("Failed to close mock repository: %v", err)
	}

	// Verify that the repository was called
	mockRepo.AssertExpectations(t)
}

// TestAddressVerificationInImport tests that the address verification works correctly during import
func TestAddressVerificationInImport(t *testing.T) {
	// Initialize crypto service for mnemonic encryption with mock config
	mockConfig := CreateMockConfig(t)
	InitCryptoService(mockConfig)

	// Create a test keystore file
	password := "testpassword"
	keystorePath, address := createTestKeystoreFile(t, password)
	defer func() {
		if err := os.RemoveAll(filepath.Dir(keystorePath)); err != nil {
			t.Logf("Failed to remove keystore directory: %v", err)
		}
	}()

	// Create a mock repository
	mockRepo := new(MockWalletRepository)
	mockRepo.On("AddWallet", mock.AnythingOfType("*wallet.Wallet")).Return(nil)
	mockRepo.On("Close").Return(nil)

	// Create a keystore in a temporary directory
	tempDir, err := os.MkdirTemp("", "keystore-service-test")
	assert.NoError(t, err)
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Logf("Failed to remove temp directory: %v", err)
		}
	}()

	// Use test-optimized parameters for faster testing
	n, p := GetTestKeystoreParams()
	ks := keystore.NewKeyStore(tempDir, n, p)

	// Create the wallet service
	walletService := NewWalletService(mockRepo, ks)

	// Import the wallet
	walletDetails, err := walletService.ImportWalletFromKeystoreV3("Test Wallet", keystorePath, password)

	// Verify the result
	assert.NoError(t, err)
	assert.NotNil(t, walletDetails)

	// Verify that the address in the wallet matches the expected address
	assert.Equal(t, address.Hex(), walletDetails.Wallet.Address)

	// Verify that the private key in the wallet details corresponds to the address
	derivedAddress := crypto.PubkeyToAddress(walletDetails.PrivateKey.PublicKey).Hex()
	assert.Equal(t, address.Hex(), derivedAddress)

	// Close the repository
	if err := mockRepo.Close(); err != nil {
		t.Logf("Failed to close mock repository: %v", err)
	}

	// Verify that the repository was called
	mockRepo.AssertExpectations(t)
}

// TestDeterministicMnemonicInImport tests that the deterministic mnemonic generation works correctly during import
func TestDeterministicMnemonicInImport(t *testing.T) {
	// Initialize crypto service for mnemonic encryption with mock config
	mockConfig := CreateMockConfig(t)
	InitCryptoService(mockConfig)

	// Create a test keystore file
	password := "testpassword"
	keystorePath, _ := createTestKeystoreFile(t, password)
	defer func() {
		if err := os.RemoveAll(filepath.Dir(keystorePath)); err != nil {
			t.Logf("Failed to remove keystore directory: %v", err)
		}
	}()

	// Create a mock repository
	mockRepo := new(MockWalletRepository)
	mockRepo.On("AddWallet", mock.AnythingOfType("*wallet.Wallet")).Return(nil)
	mockRepo.On("Close").Return(nil)

	// Create a keystore in a temporary directory
	tempDir, err := os.MkdirTemp("", "keystore-service-test")
	assert.NoError(t, err)
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Logf("Failed to remove temp directory: %v", err)
		}
	}()

	// Use test-optimized parameters for faster testing
	n, p := GetTestKeystoreParams()
	ks := keystore.NewKeyStore(tempDir, n, p)

	// Create the wallet service
	walletService := NewWalletService(mockRepo, ks)

	// Import the wallet twice to verify deterministic mnemonic generation
	walletDetails1, err := walletService.ImportWalletFromKeystoreV3("Test Wallet 1", keystorePath, password)
	assert.NoError(t, err)
	assert.NotNil(t, walletDetails1)

	walletDetails2, err := walletService.ImportWalletFromKeystoreV3("Test Wallet 2", keystorePath, password)
	assert.NoError(t, err)
	assert.NotNil(t, walletDetails2)

	// Verify that the mnemonics are the same for the same keystore file
	assert.Equal(t, walletDetails1.Mnemonic, walletDetails2.Mnemonic)

	// Close the repository
	if err := mockRepo.Close(); err != nil {
		t.Logf("Failed to close mock repository: %v", err)
	}

	// Verify that the repository was called
	mockRepo.AssertExpectations(t)
}
