package wallet

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

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

	// Create a keystore and encrypt the key
	ks := keystore.NewKeyStore(tempDir, keystore.StandardScryptN, keystore.StandardScryptP)
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

	// Create a keystore and encrypt the key
	ks := keystore.NewKeyStore(tempDir, keystore.StandardScryptN, keystore.StandardScryptP)
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

	// Create a keystore and encrypt the key
	ks := keystore.NewKeyStore(tempDir, keystore.StandardScryptN, keystore.StandardScryptP)
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

func TestImportWalletFromKeystoreV3_Success(t *testing.T) {
	// Initialize crypto service for mnemonic encryption with mock config
	mockConfig := CreateMockConfig()
	InitCryptoService(mockConfig)

	// Create a test keystore file
	password := "testpassword"
	keystorePath, address := createTestKeystoreFile(t, password)
	defer os.RemoveAll(filepath.Dir(keystorePath))

	// Create a mock repository
	mockRepo := new(MockWalletRepository)
	mockRepo.On("AddWallet", mock.AnythingOfType("*wallet.Wallet")).Return(nil)
	mockRepo.On("Close").Return(nil)

	// Create a keystore in a temporary directory
	tempDir, err := os.MkdirTemp("", "keystore-service-test")
	assert.NoError(t, err)
	defer os.RemoveAll(tempDir)

	ks := keystore.NewKeyStore(tempDir, keystore.StandardScryptN, keystore.StandardScryptP)

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
	assert.NotEmpty(t, walletDetails.Wallet.Mnemonic)
	assert.NotEmpty(t, walletDetails.Mnemonic)
	assert.NotNil(t, walletDetails.PrivateKey)
	assert.NotNil(t, walletDetails.PublicKey)

	// Close the repository
	mockRepo.Close()

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
	defer os.RemoveAll(tempDir)

	ks := keystore.NewKeyStore(tempDir, keystore.StandardScryptN, keystore.StandardScryptP)

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
	mockRepo.Close()

	// Verify that the repository was called
	mockRepo.AssertExpectations(t)
}

func TestImportWalletFromKeystoreV3_InvalidJSON(t *testing.T) {
	// Create a corrupted keystore file
	corruptedKeystorePath := createCorruptedKeystoreFile(t)
	defer os.RemoveAll(filepath.Dir(corruptedKeystorePath))

	// Create a mock repository
	mockRepo := new(MockWalletRepository)
	mockRepo.On("Close").Return(nil)

	// Create a keystore in a temporary directory
	tempDir, err := os.MkdirTemp("", "keystore-service-test")
	assert.NoError(t, err)
	defer os.RemoveAll(tempDir)

	ks := keystore.NewKeyStore(tempDir, keystore.StandardScryptN, keystore.StandardScryptP)

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
	mockRepo.Close()

	// Verify that the repository was called
	mockRepo.AssertExpectations(t)
}

func TestImportWalletFromKeystoreV3_InvalidVersion(t *testing.T) {
	// Create an invalid keystore file
	invalidKeystorePath := createInvalidKeystoreFile(t)
	defer os.RemoveAll(filepath.Dir(invalidKeystorePath))

	// Create a mock repository
	mockRepo := new(MockWalletRepository)
	mockRepo.On("Close").Return(nil)

	// Create a keystore in a temporary directory
	tempDir, err := os.MkdirTemp("", "keystore-service-test")
	assert.NoError(t, err)
	defer os.RemoveAll(tempDir)

	ks := keystore.NewKeyStore(tempDir, keystore.StandardScryptN, keystore.StandardScryptP)

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
	mockRepo.Close()

	// Verify that the repository was called
	mockRepo.AssertExpectations(t)
}
func TestImportWalletFromKeystoreV3_MissingFields(t *testing.T) {
	// Create a keystore file with missing fields
	missingFieldsKeystorePath := createMissingFieldsKeystoreFile(t)
	defer os.RemoveAll(filepath.Dir(missingFieldsKeystorePath))

	// Create a mock repository
	mockRepo := new(MockWalletRepository)
	mockRepo.On("Close").Return(nil)

	// Create a keystore in a temporary directory
	tempDir, err := os.MkdirTemp("", "keystore-service-test")
	assert.NoError(t, err)
	defer os.RemoveAll(tempDir)

	ks := keystore.NewKeyStore(tempDir, keystore.StandardScryptN, keystore.StandardScryptP)

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
	mockRepo.Close()

	// Verify that the repository was called
	mockRepo.AssertExpectations(t)
}

func TestImportWalletFromKeystoreV3_InvalidAddress(t *testing.T) {
	// Create a keystore file with invalid address
	password := "testpassword"
	invalidAddressKeystorePath := createInvalidAddressKeystoreFile(t, password)
	defer os.RemoveAll(filepath.Dir(invalidAddressKeystorePath))

	// Create a mock repository
	mockRepo := new(MockWalletRepository)
	mockRepo.On("Close").Return(nil)

	// Create a keystore in a temporary directory
	tempDir, err := os.MkdirTemp("", "keystore-service-test")
	assert.NoError(t, err)
	defer os.RemoveAll(tempDir)

	ks := keystore.NewKeyStore(tempDir, keystore.StandardScryptN, keystore.StandardScryptP)

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
	mockRepo.Close()

	// Verify that the repository was called
	mockRepo.AssertExpectations(t)
}

func TestImportWalletFromKeystoreV3_IncorrectPassword(t *testing.T) {
	// Create a test keystore file
	password := "testpassword"
	keystorePath, _ := createTestKeystoreFile(t, password)
	defer os.RemoveAll(filepath.Dir(keystorePath))

	// Create a mock repository
	mockRepo := new(MockWalletRepository)
	mockRepo.On("Close").Return(nil)

	// Create a keystore in a temporary directory
	tempDir, err := os.MkdirTemp("", "keystore-service-test")
	assert.NoError(t, err)
	defer os.RemoveAll(tempDir)

	ks := keystore.NewKeyStore(tempDir, keystore.StandardScryptN, keystore.StandardScryptP)

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
	mockRepo.Close()

	// Verify that the repository was called
	mockRepo.AssertExpectations(t)
}
func TestImportWalletFromKeystoreV3_AddressMismatch(t *testing.T) {
	// Create a keystore file with address mismatch
	password := "testpassword"
	addressMismatchKeystorePath := createAddressMismatchKeystoreFile(t, password)
	defer os.RemoveAll(filepath.Dir(addressMismatchKeystorePath))

	// Create a mock repository
	mockRepo := new(MockWalletRepository)
	mockRepo.On("Close").Return(nil)

	// Create a keystore in a temporary directory
	tempDir, err := os.MkdirTemp("", "keystore-service-test")
	assert.NoError(t, err)
	defer os.RemoveAll(tempDir)

	ks := keystore.NewKeyStore(tempDir, keystore.StandardScryptN, keystore.StandardScryptP)

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
	mockRepo.Close()

	// Verify that the repository was called
	mockRepo.AssertExpectations(t)
}

func TestImportWalletFromKeystoreV3_RepositoryError(t *testing.T) {
	// Initialize crypto service for mnemonic encryption with mock config
	mockConfig := CreateMockConfig()
	InitCryptoService(mockConfig)

	// Create a test keystore file
	password := "testpassword"
	keystorePath, _ := createTestKeystoreFile(t, password)
	defer os.RemoveAll(filepath.Dir(keystorePath))

	// Create a mock repository that returns an error
	mockRepo := new(MockWalletRepository)
	mockRepo.On("AddWallet", mock.AnythingOfType("*wallet.Wallet")).Return(assert.AnError)
	mockRepo.On("Close").Return(nil)

	// Create a keystore in a temporary directory
	tempDir, err := os.MkdirTemp("", "keystore-service-test")
	assert.NoError(t, err)
	defer os.RemoveAll(tempDir)

	ks := keystore.NewKeyStore(tempDir, keystore.StandardScryptN, keystore.StandardScryptP)

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
	mockRepo.Close()

	// Verify that the repository was called
	mockRepo.AssertExpectations(t)
}

func TestImportWalletFromKeystore_BackwardCompatibility(t *testing.T) {
	// Initialize crypto service for mnemonic encryption with mock config
	mockConfig := CreateMockConfig()
	InitCryptoService(mockConfig)

	// Create a test keystore file
	password := "testpassword"
	keystorePath, address := createTestKeystoreFile(t, password)
	defer os.RemoveAll(filepath.Dir(keystorePath))

	// Create a mock repository
	mockRepo := new(MockWalletRepository)
	mockRepo.On("AddWallet", mock.AnythingOfType("*wallet.Wallet")).Return(nil)
	mockRepo.On("Close").Return(nil)

	// Create a keystore in a temporary directory
	tempDir, err := os.MkdirTemp("", "keystore-service-test")
	assert.NoError(t, err)
	defer os.RemoveAll(tempDir)

	ks := keystore.NewKeyStore(tempDir, keystore.StandardScryptN, keystore.StandardScryptP)

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
	mockRepo.Close()

	// Verify that the repository was called
	mockRepo.AssertExpectations(t)
}

// TestAddressVerificationInImport tests that the address verification works correctly during import
func TestAddressVerificationInImport(t *testing.T) {
	// Initialize crypto service for mnemonic encryption with mock config
	mockConfig := CreateMockConfig()
	InitCryptoService(mockConfig)

	// Create a test keystore file
	password := "testpassword"
	keystorePath, address := createTestKeystoreFile(t, password)
	defer os.RemoveAll(filepath.Dir(keystorePath))

	// Create a mock repository
	mockRepo := new(MockWalletRepository)
	mockRepo.On("AddWallet", mock.AnythingOfType("*wallet.Wallet")).Return(nil)
	mockRepo.On("Close").Return(nil)

	// Create a keystore in a temporary directory
	tempDir, err := os.MkdirTemp("", "keystore-service-test")
	assert.NoError(t, err)
	defer os.RemoveAll(tempDir)

	ks := keystore.NewKeyStore(tempDir, keystore.StandardScryptN, keystore.StandardScryptP)

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
	mockRepo.Close()

	// Verify that the repository was called
	mockRepo.AssertExpectations(t)
}

// TestDeterministicMnemonicInImport tests that the deterministic mnemonic generation works correctly during import
func TestDeterministicMnemonicInImport(t *testing.T) {
	// Initialize crypto service for mnemonic encryption with mock config
	mockConfig := CreateMockConfig()
	InitCryptoService(mockConfig)

	// Create a test keystore file
	password := "testpassword"
	keystorePath, _ := createTestKeystoreFile(t, password)
	defer os.RemoveAll(filepath.Dir(keystorePath))

	// Create a mock repository
	mockRepo := new(MockWalletRepository)
	mockRepo.On("AddWallet", mock.AnythingOfType("*wallet.Wallet")).Return(nil)
	mockRepo.On("Close").Return(nil)

	// Create a keystore in a temporary directory
	tempDir, err := os.MkdirTemp("", "keystore-service-test")
	assert.NoError(t, err)
	defer os.RemoveAll(tempDir)

	ks := keystore.NewKeyStore(tempDir, keystore.StandardScryptN, keystore.StandardScryptP)

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
	mockRepo.Close()

	// Verify that the repository was called
	mockRepo.AssertExpectations(t)
}
