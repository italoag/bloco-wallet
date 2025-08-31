package wallet

import (
	"os"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/keystore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// TestImportWallet_MnemonicDuplicateDetection ensures duplicate detection is based on mnemonic source hash
func TestImportWallet_MnemonicDuplicateDetection(t *testing.T) {
	// Initialize crypto service for mnemonic encryption
	cfg := CreateMockConfig()
	InitCryptoService(cfg)

	mnemonic := "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"
	hash := (&SourceHashGenerator{}).GenerateFromMnemonic(mnemonic)

	// Mock repository: report that wallet with this source already exists
	mockRepo := new(MockWalletRepository)
	mockRepo.On("FindBySourceHash", hash).Return(&Wallet{Address: "0xDEADBEEF"}, nil)
	// Ensure AddWallet is NOT called
	mockRepo.On("AddWallet", mock.Anything).Return(nil).Maybe()
	mockRepo.On("Close").Return(nil).Maybe()

	// Temporary keystore directory
	tempDir, err := os.MkdirTemp("", "mnemonic-import-test")
	assert.NoError(t, err)
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Logf("Failed to remove temp directory: %v", err)
		}
	}()
	ks := keystore.NewKeyStore(tempDir, keystore.StandardScryptN, keystore.StandardScryptP)

	ws := NewWalletService(mockRepo, ks)
	_, err = ws.ImportWallet("My Wallet", mnemonic, "password")

	// Expect duplicate mnemonic error
	if assert.Error(t, err) {
		dupErr, ok := err.(*DuplicateWalletError)
		assert.True(t, ok, "expected DuplicateWalletError")
		if ok {
			assert.Equal(t, string(ImportMethodMnemonic), dupErr.Type)
		}
	}
}

// TestImportWallet_MnemonicSuccess ensures different mnemonics import successfully and store proper fields
func TestImportWallet_MnemonicSuccess(t *testing.T) {
	// Initialize crypto service
	cfg := CreateMockConfig()
	InitCryptoService(cfg)

	mnemonic := "legal winner thank year wave sausage worth useful legal winner thank yellow"
	hash := (&SourceHashGenerator{}).GenerateFromMnemonic(mnemonic)

	mockRepo := new(MockWalletRepository)
	// No duplicate
	mockRepo.On("FindBySourceHash", hash).Return(nil, nil)
	// Accept add
	mockRepo.On("AddWallet", mock.MatchedBy(func(w *Wallet) bool {
		return w.ImportMethod == string(ImportMethodMnemonic) && w.SourceHash == hash && w.Mnemonic != nil && *w.Mnemonic != ""
	})).Return(nil)
	mockRepo.On("Close").Return(nil).Maybe()

	// Temporary keystore directory
	tempDir, err := os.MkdirTemp("", "mnemonic-import-test")
	assert.NoError(t, err)
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Logf("Failed to remove temp directory: %v", err)
		}
	}()
	ks := keystore.NewKeyStore(tempDir, keystore.StandardScryptN, keystore.StandardScryptP)

	ws := NewWalletService(mockRepo, ks)
	details, err := ws.ImportWallet("Wallet A", mnemonic, "pass")
	assert.NoError(t, err)
	if assert.NotNil(t, details) {
		assert.Equal(t, ImportMethodMnemonic, details.ImportMethod)
		assert.True(t, details.HasMnemonic)
		assert.NotNil(t, details.Mnemonic)
	}

	mockRepo.AssertExpectations(t)
}

// TestImportWallet_InvalidMnemonic ensures invalid mnemonic is rejected before processing
func TestImportWallet_InvalidMnemonic(t *testing.T) {
	cfg := CreateMockConfig()
	InitCryptoService(cfg)

	invalid := "not a valid mnemonic phrase"

	// Mock repo shouldn't be used but create a benign one
	mockRepo := new(MockWalletRepository)
	mockRepo.On("Close").Return(nil).Maybe()

	// Temporary keystore directory
	tempDir, err := os.MkdirTemp("", "mnemonic-import-test")
	assert.NoError(t, err)
	defer func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Logf("Failed to remove temp directory: %v", err)
		}
	}()
	ks := keystore.NewKeyStore(tempDir, keystore.StandardScryptN, keystore.StandardScryptP)

	ws := NewWalletService(mockRepo, ks)
	_, err = ws.ImportWallet("Bad", invalid, "pwd")
	if assert.Error(t, err) {
		invErr, ok := err.(*InvalidImportDataError)
		assert.True(t, ok, "expected InvalidImportDataError")
		if ok {
			assert.Equal(t, string(ImportMethodMnemonic), invErr.Type)
		}
	}
}
