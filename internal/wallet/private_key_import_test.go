package wallet

import (
	"encoding/hex"
	"os"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/keystore"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// TestImportWalletFromPrivateKey_InvalidFormat ensures invalid private key format is rejected early
func TestImportWalletFromPrivateKey_InvalidFormat(t *testing.T) {
	cfg := CreateMockConfig()
	InitCryptoService(cfg)

	mockRepo := new(MockWalletRepository)
	mockRepo.On("Close").Return(nil).Maybe()

	tempDir, err := os.MkdirTemp("", "pk-import-test")
	assert.NoError(t, err)
	defer os.RemoveAll(tempDir)
	ks := keystore.NewKeyStore(tempDir, keystore.StandardScryptN, keystore.StandardScryptP)

	ws := NewWalletService(mockRepo, ks)
	_, err = ws.ImportWalletFromPrivateKey("BadPK", "1234", "pwd")
	if assert.Error(t, err) {
		inv, ok := err.(*InvalidImportDataError)
		assert.True(t, ok, "expected InvalidImportDataError")
		if ok {
			assert.Equal(t, string(ImportMethodPrivateKey), inv.Type)
		}
	}
}

// TestImportWalletFromPrivateKey_Duplicate ensures duplicate detection is based on source hash
func TestImportWalletFromPrivateKey_Duplicate(t *testing.T) {
	cfg := CreateMockConfig()
	InitCryptoService(cfg)

	// Generate a valid private key hex
	key, err := crypto.GenerateKey()
	assert.NoError(t, err)
	pkHex := hex.EncodeToString(crypto.FromECDSA(key))
	hash := (&SourceHashGenerator{}).GenerateFromPrivateKey(pkHex)

	mockRepo := new(MockWalletRepository)
	mockRepo.On("FindBySourceHash", hash).Return(&Wallet{Address: "0xDUP"}, nil)
	mockRepo.On("AddWallet", mock.Anything).Return(nil).Maybe()
	mockRepo.On("Close").Return(nil).Maybe()

	tempDir, err := os.MkdirTemp("", "pk-import-test")
	assert.NoError(t, err)
	defer os.RemoveAll(tempDir)
	ks := keystore.NewKeyStore(tempDir, keystore.StandardScryptN, keystore.StandardScryptP)

	ws := NewWalletService(mockRepo, ks)
	_, err = ws.ImportWalletFromPrivateKey("Dup", pkHex, "pwd")
	if assert.Error(t, err) {
		dup, ok := err.(*DuplicateWalletError)
		assert.True(t, ok, "expected DuplicateWalletError")
		if ok {
			assert.Equal(t, string(ImportMethodPrivateKey), dup.Type)
		}
	}
}

// TestImportWalletFromPrivateKey_Success ensures success path does not create a mnemonic
func TestImportWalletFromPrivateKey_Success(t *testing.T) {
	cfg := CreateMockConfig()
	InitCryptoService(cfg)

	key, err := crypto.GenerateKey()
	assert.NoError(t, err)
	pkHex := hex.EncodeToString(crypto.FromECDSA(key))
	hash := (&SourceHashGenerator{}).GenerateFromPrivateKey(pkHex)

	mockRepo := new(MockWalletRepository)
	// No duplicate
	mockRepo.On("FindBySourceHash", hash).Return(nil, nil)
	// Expect AddWallet with no mnemonic and correct fields
	mockRepo.On("AddWallet", mock.MatchedBy(func(w *Wallet) bool {
		return w.ImportMethod == string(ImportMethodPrivateKey) && w.SourceHash == hash && w.Mnemonic == nil && w.Address != "" && w.KeyStorePath != ""
	})).Return(nil)
	mockRepo.On("Close").Return(nil).Maybe()

	tempDir, err := os.MkdirTemp("", "pk-import-test")
	assert.NoError(t, err)
	defer os.RemoveAll(tempDir)
	ks := keystore.NewKeyStore(tempDir, keystore.StandardScryptN, keystore.StandardScryptP)

	ws := NewWalletService(mockRepo, ks)
	details, err := ws.ImportWalletFromPrivateKey("PK Wallet", pkHex, "pwd")
	assert.NoError(t, err)
	if assert.NotNil(t, details) {
		assert.Equal(t, ImportMethodPrivateKey, details.ImportMethod)
		assert.False(t, details.HasMnemonic)
		assert.Nil(t, details.Mnemonic)
		assert.NotNil(t, details.PrivateKey)
		assert.NotNil(t, details.PublicKey)
	}

	mockRepo.AssertExpectations(t)
}
