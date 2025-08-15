package wallet

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/keystore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// TestImportWalletFromKeystoreV3WithUniversalKDF tests keystore import with Universal KDF support
func TestImportWalletFromKeystoreV3WithUniversalKDF(t *testing.T) {
	// Initialize crypto service for mnemonic encryption with mock config
	mockConfig := CreateMockConfig()
	InitCryptoService(mockConfig)

	// Create temporary directory for test
	tempDir, err := ioutil.TempDir("", "keystore_test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Create mock repository
	mockRepo := new(MockWalletRepository)
	mockRepo.On("FindBySourceHash", mock.AnythingOfType("string")).Return(nil, nil)
	mockRepo.On("AddWallet", mock.AnythingOfType("*wallet.Wallet")).Return(nil)

	// Create keystore
	ks := keystore.NewKeyStore(tempDir, keystore.StandardScryptN, keystore.StandardScryptP)

	// Create wallet service
	ws := NewWalletService(mockRepo, ks)

	tests := []struct {
		name           string
		keystoreFile   string
		password       string
		expectError    bool
		expectedKDF    string
		expectedSecurity string
	}{
		{
			name:           "Standard Scrypt KeyStore",
			keystoreFile:   "real_keystore_v3_standard.json",
			password:       "testpassword",
			expectError:    false,
			expectedKDF:    "scrypt",
			expectedSecurity: "Medium",
		},
		{
			name:           "Light Scrypt KeyStore",
			keystoreFile:   "real_keystore_v3_light.json",
			password:       "testpassword",
			expectError:    false,
			expectedKDF:    "scrypt",
			expectedSecurity: "Low",
		},
		{
			name:           "Complex Password KeyStore",
			keystoreFile:   "real_keystore_v3_complex_password.json",
			password:       "P@$$w0rd!123#ComplexPassword",
			expectError:    false,
			expectedKDF:    "scrypt",
			expectedSecurity: "Medium",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Get test keystore path
			keystorePath := filepath.Join("testdata", "keystores", tt.keystoreFile)
			
			// Skip test if file doesn't exist
			if _, err := os.Stat(keystorePath); os.IsNotExist(err) {
				t.Skipf("Test keystore file not found: %s", keystorePath)
				return
			}

			// Import wallet
			walletDetails, err := ws.ImportWalletFromKeystoreV3("test-wallet", keystorePath, tt.password)

			if tt.expectError {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, walletDetails)

			// Verify basic wallet properties
			assert.NotNil(t, walletDetails.Wallet)
			assert.NotNil(t, walletDetails.PrivateKey)
			assert.NotNil(t, walletDetails.PublicKey)
			assert.NotEmpty(t, walletDetails.Mnemonic)

			// Verify import method
			assert.Equal(t, ImportMethodKeystore, walletDetails.ImportMethod)
			assert.True(t, walletDetails.HasMnemonic)

			// Verify KDF information
			require.NotNil(t, walletDetails.KDFInfo)
			assert.Equal(t, tt.expectedKDF, walletDetails.KDFInfo.NormalizedType)
			assert.Equal(t, tt.expectedSecurity, walletDetails.KDFInfo.SecurityLevel)
			assert.NotEmpty(t, walletDetails.KDFInfo.Parameters)

			// Verify address consistency
			assert.NotEmpty(t, walletDetails.Wallet.Address)
			assert.True(t, len(walletDetails.Wallet.Address) == 42) // 0x + 40 hex chars
		})
	}
}

// TestUniversalKDFCompatibilityAnalysis tests the compatibility analysis functionality
func TestUniversalKDFCompatibilityAnalysis(t *testing.T) {
	analyzer := NewKDFCompatibilityAnalyzer()

	tests := []struct {
		name           string
		keystoreData   map[string]interface{}
		expectCompatible bool
		expectedKDF    string
		expectedSecurity string
	}{
		{
			name: "Valid Scrypt KeyStore",
			keystoreData: map[string]interface{}{
				"version": float64(3),
				"crypto": map[string]interface{}{
					"kdf": "scrypt",
					"kdfparams": map[string]interface{}{
						"n":     262144,
						"r":     8,
						"p":     1,
						"dklen": 32,
						"salt":  "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
					},
				},
			},
			expectCompatible: true,
			expectedKDF:     "scrypt",
			expectedSecurity: "Medium",
		},
		{
			name: "Valid PBKDF2 KeyStore",
			keystoreData: map[string]interface{}{
				"version": float64(3),
				"crypto": map[string]interface{}{
					"kdf": "pbkdf2",
					"kdfparams": map[string]interface{}{
						"c":     262144,
						"dklen": 32,
						"prf":   "hmac-sha256",
						"salt":  "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
					},
				},
			},
			expectCompatible: true,
			expectedKDF:     "pbkdf2",
			expectedSecurity: "Medium",
		},
		{
			name: "Case Insensitive KDF",
			keystoreData: map[string]interface{}{
				"version": float64(3),
				"crypto": map[string]interface{}{
					"kdf": "SCRYPT",
					"kdfparams": map[string]interface{}{
						"N":     262144,
						"R":     8,
						"P":     1,
						"dklen": 32,
						"Salt":  "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
					},
				},
			},
			expectCompatible: true,
			expectedKDF:     "scrypt",
			expectedSecurity: "Medium",
		},
		{
			name: "Unsupported KDF",
			keystoreData: map[string]interface{}{
				"version": float64(3),
				"crypto": map[string]interface{}{
					"kdf": "argon2",
					"kdfparams": map[string]interface{}{
						"memory": 65536,
						"time":   3,
						"threads": 4,
					},
				},
			},
			expectCompatible: false,
			expectedKDF:     "argon2",
		},
		{
			name: "Missing Crypto Section",
			keystoreData: map[string]interface{}{
				"version": float64(3),
			},
			expectCompatible: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report := analyzer.AnalyzeKeyStoreCompatibility(tt.keystoreData)

			assert.Equal(t, tt.expectCompatible, report.Compatible)
			
			if tt.expectCompatible {
				assert.Equal(t, tt.expectedKDF, report.NormalizedKDF)
				assert.Equal(t, tt.expectedSecurity, report.SecurityLevel)
				assert.NotEmpty(t, report.Parameters)
			} else {
				assert.NotEmpty(t, report.Issues)
			}
		})
	}
}

// TestKDFParameterConversion tests parameter conversion for different JSON types
func TestKDFParameterConversion(t *testing.T) {
	service := NewUniversalKDFService()

	tests := []struct {
		name        string
		cryptoParams *CryptoParams
		password    string
		expectError bool
	}{
		{
			name: "Integer Parameters",
			cryptoParams: &CryptoParams{
				KDF: "scrypt",
				KDFParams: map[string]interface{}{
					"n":     262144,
					"r":     8,
					"p":     1,
					"dklen": 32,
					"salt":  "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
				},
			},
			password:    "testpassword",
			expectError: false,
		},
		{
			name: "Float64 Parameters",
			cryptoParams: &CryptoParams{
				KDF: "scrypt",
				KDFParams: map[string]interface{}{
					"n":     float64(262144),
					"r":     float64(8),
					"p":     float64(1),
					"dklen": float64(32),
					"salt":  "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
				},
			},
			password:    "testpassword",
			expectError: false,
		},
		{
			name: "String Parameters",
			cryptoParams: &CryptoParams{
				KDF: "scrypt",
				KDFParams: map[string]interface{}{
					"n":     "262144",
					"r":     "8",
					"p":     "1",
					"dklen": "32",
					"salt":  "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
				},
			},
			password:    "testpassword",
			expectError: false,
		},
		{
			name: "Mixed Parameter Types",
			cryptoParams: &CryptoParams{
				KDF: "scrypt",
				KDFParams: map[string]interface{}{
					"n":     262144,
					"r":     float64(8),
					"p":     "1",
					"dklen": float64(32),
					"salt":  "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
				},
			},
			password:    "testpassword",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			derivedKey, err := service.DeriveKey(tt.password, tt.cryptoParams)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, derivedKey)
				assert.Equal(t, 32, len(derivedKey)) // Standard derived key length
			}
		})
	}
}

// TestKDFSecurityAnalysis tests security level analysis for different parameter sets
func TestKDFSecurityAnalysis(t *testing.T) {
	analyzer := NewKDFCompatibilityAnalyzer()

	tests := []struct {
		name           string
		kdf            string
		params         map[string]interface{}
		expectedLevel  string
	}{
		{
			name: "Low Security Scrypt",
			kdf:  "scrypt",
			params: map[string]interface{}{
				"n": 1024,
				"r": 1,
				"p": 1,
			},
			expectedLevel: "Low",
		},
		{
			name: "Medium Security Scrypt",
			kdf:  "scrypt",
			params: map[string]interface{}{
				"n": 262144,
				"r": 8,
				"p": 1,
			},
			expectedLevel: "Medium",
		},
		{
			name: "High Security Scrypt",
			kdf:  "scrypt",
			params: map[string]interface{}{
				"n": 1048576,
				"r": 8,
				"p": 2,
			},
			expectedLevel: "High",
		},
		{
			name: "Low Security PBKDF2",
			kdf:  "pbkdf2",
			params: map[string]interface{}{
				"c": 10000,
			},
			expectedLevel: "Low",
		},
		{
			name: "Medium Security PBKDF2",
			kdf:  "pbkdf2",
			params: map[string]interface{}{
				"c": 262144,
			},
			expectedLevel: "Medium",
		},
		{
			name: "High Security PBKDF2",
			kdf:  "pbkdf2",
			params: map[string]interface{}{
				"c": 1000000,
			},
			expectedLevel: "High",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			analysis := analyzer.analyzeParameterSecurity(tt.kdf, tt.params)
			assert.Equal(t, tt.expectedLevel, analysis.Level)
			assert.NotEmpty(t, analysis.Suggestions)
		})
	}
}

// TestKeystoreImportErrorMessages tests that error messages include KDF context
func TestKeystoreImportErrorMessages(t *testing.T) {
	// Create temporary directory for test
	tempDir, err := ioutil.TempDir("", "keystore_error_test")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Create mock repository
	mockRepo := new(MockWalletRepository)
	mockRepo.On("FindBySourceHash", mock.AnythingOfType("string")).Return(nil, nil)

	// Create keystore
	ks := keystore.NewKeyStore(tempDir, keystore.StandardScryptN, keystore.StandardScryptP)

	// Create wallet service
	ws := NewWalletService(mockRepo, ks)

	// Test with invalid keystore file
	invalidKeystorePath := filepath.Join(tempDir, "invalid.json")
	err = ioutil.WriteFile(invalidKeystorePath, []byte(`{"invalid": "json"}`), 0644)
	require.NoError(t, err)

	_, err = ws.ImportWalletFromKeystoreV3("test-wallet", invalidKeystorePath, "password")
	assert.Error(t, err)

	// Verify error contains useful information
	assert.Contains(t, err.Error(), "crypto")
}

// Add FindBySourceHash method to existing MockWalletRepository
func (m *MockWalletRepository) FindBySourceHash(sourceHash string) (*Wallet, error) {
	args := m.Called(sourceHash)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*Wallet), args.Error(1)
}