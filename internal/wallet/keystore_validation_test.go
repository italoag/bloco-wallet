package wallet

import (
	"crypto/rand"
	"encoding/json"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/keystore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateKeystoreV3(t *testing.T) {
	validator := &KeystoreValidator{}

	tests := []struct {
		name          string
		json          string
		expectedError KeystoreErrorType
	}{
		{
			name: "Valid keystore",
			json: `{
				"version": 3,
				"id": "f06e0f8e-7d91-4b09-8f5a-3c2c1a9b2b88",
				"address": "0x00112233445566778899aabbccddeeff7a6b5c4d",
				"crypto": {
					"cipher": "aes-128-ctr",
					"ciphertext": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
					"cipherparams": {
						"iv": "00112233445566778899aabbccddeeff"
					},
					"kdf": "scrypt",
					"kdfparams": {
						"dklen": 32,
						"n": 262144,
						"p": 1,
						"r": 8,
						"salt": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
					},
					"mac": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
				}
			}`,
			expectedError: -1, // No error expected
		}, {

			name: "Invalid JSON",
			json: `{
				"version": 3,
				"id": "f06e0f8e-7d91-4b09-8f5a-3c2c1a9b2b88",
				"address": "0x00112233445566778899aabbccddeeff7a6b5c4d",
				"crypto": {
					"cipher": "aes-128-ctr",
					"ciphertext": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
					"cipherparams": {
						"iv": "00112233445566778899aabbccddeeff"
					},
					"kdf": "scrypt",
					"kdfparams": {
						"dklen": 32,
						"n": 262144,
						"p": 1,
						"r": 8,
						"salt": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
					},
					"mac": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
				}
			`,
			expectedError: ErrorInvalidJSON,
		},
		{
			name: "Invalid version",
			json: `{
				"version": 2,
				"id": "f06e0f8e-7d91-4b09-8f5a-3c2c1a9b2b88",
				"address": "0x00112233445566778899aabbccddeeff7a6b5c4d",
				"crypto": {
					"cipher": "aes-128-ctr",
					"ciphertext": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
					"cipherparams": {
						"iv": "00112233445566778899aabbccddeeff"
					},
					"kdf": "scrypt",
					"kdfparams": {
						"dklen": 32,
						"n": 262144,
						"p": 1,
						"r": 8,
						"salt": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
					},
					"mac": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
				}
			}`,
			expectedError: ErrorInvalidVersion,
		}, {

			name: "Missing address",
			json: `{
				"version": 3,
				"id": "f06e0f8e-7d91-4b09-8f5a-3c2c1a9b2b88",
				"crypto": {
					"cipher": "aes-128-ctr",
					"ciphertext": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
					"cipherparams": {
						"iv": "00112233445566778899aabbccddeeff"
					},
					"kdf": "scrypt",
					"kdfparams": {
						"dklen": 32,
						"n": 262144,
						"p": 1,
						"r": 8,
						"salt": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
					},
					"mac": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
				}
			}`,
			expectedError: ErrorMissingRequiredFields,
		},
		{
			name: "Invalid address format",
			json: `{
				"version": 3,
				"id": "f06e0f8e-7d91-4b09-8f5a-3c2c1a9b2b88",
				"address": "not-a-valid-address",
				"crypto": {
					"cipher": "aes-128-ctr",
					"ciphertext": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
					"cipherparams": {
						"iv": "00112233445566778899aabbccddeeff"
					},
					"kdf": "scrypt",
					"kdfparams": {
						"dklen": 32,
						"n": 262144,
						"p": 1,
						"r": 8,
						"salt": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
					},
					"mac": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
				}
			}`,
			expectedError: ErrorInvalidAddress,
		}, {

			name: "Missing crypto.cipher field",
			json: `{
				"version": 3,
				"id": "f06e0f8e-7d91-4b09-8f5a-3c2c1a9b2b88",
				"address": "0x00112233445566778899aabbccddeeff7a6b5c4d",
				"crypto": {
					"ciphertext": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
					"cipherparams": {
						"iv": "00112233445566778899aabbccddeeff"
					},
					"kdf": "scrypt",
					"kdfparams": {
						"dklen": 32,
						"n": 262144,
						"p": 1,
						"r": 8,
						"salt": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
					},
					"mac": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
				}
			}`,
			expectedError: ErrorMissingRequiredFields,
		},
		{
			name: "Missing crypto.ciphertext field",
			json: `{
				"version": 3,
				"id": "f06e0f8e-7d91-4b09-8f5a-3c2c1a9b2b88",
				"address": "0x00112233445566778899aabbccddeeff7a6b5c4d",
				"crypto": {
					"cipher": "aes-128-ctr",
					"cipherparams": {
						"iv": "00112233445566778899aabbccddeeff"
					},
					"kdf": "scrypt",
					"kdfparams": {
						"dklen": 32,
						"n": 262144,
						"p": 1,
						"r": 8,
						"salt": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
					},
					"mac": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
				}
			}`,
			expectedError: ErrorMissingRequiredFields,
		},
		{
			name: "Missing crypto.cipherparams.iv field",
			json: `{
				"version": 3,
				"id": "f06e0f8e-7d91-4b09-8f5a-3c2c1a9b2b88",
				"address": "0x00112233445566778899aabbccddeeff7a6b5c4d",
				"crypto": {
					"cipher": "aes-128-ctr",
					"ciphertext": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
					"cipherparams": {},
					"kdf": "scrypt",
					"kdfparams": {
						"dklen": 32,
						"n": 262144,
						"p": 1,
						"r": 8,
						"salt": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
					},
					"mac": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
				}
			}`,
			expectedError: ErrorMissingRequiredFields,
		},
		{
			name: "Missing crypto.kdf field",
			json: `{
				"version": 3,
				"id": "f06e0f8e-7d91-4b09-8f5a-3c2c1a9b2b88",
				"address": "0x00112233445566778899aabbccddeeff7a6b5c4d",
				"crypto": {
					"cipher": "aes-128-ctr",
					"ciphertext": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
					"cipherparams": {
						"iv": "00112233445566778899aabbccddeeff"
					},
					"kdfparams": {
						"dklen": 32,
						"n": 262144,
						"p": 1,
						"r": 8,
						"salt": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
					},
					"mac": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
				}
			}`,
			expectedError: ErrorMissingRequiredFields,
		},
		{
			name: "Missing crypto.kdfparams field",
			json: `{
				"version": 3,
				"id": "f06e0f8e-7d91-4b09-8f5a-3c2c1a9b2b88",
				"address": "0x00112233445566778899aabbccddeeff7a6b5c4d",
				"crypto": {
					"cipher": "aes-128-ctr",
					"ciphertext": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
					"cipherparams": {
						"iv": "00112233445566778899aabbccddeeff"
					},
					"kdf": "scrypt",
					"mac": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
				}
			}`,
			expectedError: ErrorMissingRequiredFields,
		},
		{
			name: "Missing crypto.mac field",
			json: `{
				"version": 3,
				"id": "f06e0f8e-7d91-4b09-8f5a-3c2c1a9b2b88",
				"address": "0x00112233445566778899aabbccddeeff7a6b5c4d",
				"crypto": {
					"cipher": "aes-128-ctr",
					"ciphertext": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
					"cipherparams": {
						"iv": "00112233445566778899aabbccddeeff"
					},
					"kdf": "scrypt",
					"kdfparams": {
						"dklen": 32,
						"n": 262144,
						"p": 1,
						"r": 8,
						"salt": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
					}
				}
			}`,
			expectedError: ErrorMissingRequiredFields,
		}, {

			name: "PBKDF2 KDF",
			json: `{
				"version": 3,
				"id": "f06e0f8e-7d91-4b09-8f5a-3c2c1a9b2b88",
				"address": "0x00112233445566778899aabbccddeeff7a6b5c4d",
				"crypto": {
					"cipher": "aes-128-ctr",
					"ciphertext": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
					"cipherparams": {
						"iv": "00112233445566778899aabbccddeeff"
					},
					"kdf": "pbkdf2",
					"kdfparams": {
						"dklen": 32,
						"c": 10240,
						"prf": "hmac-sha256",
						"salt": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
					},
					"mac": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
				}
			}`,
			expectedError: -1, // No error expected
		},
		{
			name: "Missing PBKDF2 required field - dklen",
			json: `{
				"version": 3,
				"id": "f06e0f8e-7d91-4b09-8f5a-3c2c1a9b2b88",
				"address": "0x00112233445566778899aabbccddeeff7a6b5c4d",
				"crypto": {
					"cipher": "aes-128-ctr",
					"ciphertext": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
					"cipherparams": {
						"iv": "00112233445566778899aabbccddeeff"
					},
					"kdf": "pbkdf2",
					"kdfparams": {
						"c": 10240,
						"prf": "hmac-sha256",
						"salt": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
					},
					"mac": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
				}
			}`,
			expectedError: ErrorMissingRequiredFields,
		}, {

			name: "Missing Scrypt required field - dklen",
			json: `{
				"version": 3,
				"id": "f06e0f8e-7d91-4b09-8f5a-3c2c1a9b2b88",
				"address": "0x00112233445566778899aabbccddeeff7a6b5c4d",
				"crypto": {
					"cipher": "aes-128-ctr",
					"ciphertext": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
					"cipherparams": {
						"iv": "00112233445566778899aabbccddeeff"
					},
					"kdf": "scrypt",
					"kdfparams": {
						"n": 262144,
						"p": 1,
						"r": 8,
						"salt": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
					},
					"mac": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
				}
			}`,
			expectedError: ErrorMissingRequiredFields,
		},
		{
			name: "Unsupported KDF",
			json: `{
				"version": 3,
				"id": "f06e0f8e-7d91-4b09-8f5a-3c2c1a9b2b88",
				"address": "0x00112233445566778899aabbccddeeff7a6b5c4d",
				"crypto": {
					"cipher": "aes-128-ctr",
					"ciphertext": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
					"cipherparams": {
						"iv": "00112233445566778899aabbccddeeff"
					},
					"kdf": "unsupported-kdf",
					"kdfparams": {
						"dklen": 32,
						"n": 262144,
						"p": 1,
						"r": 8,
						"salt": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
					},
					"mac": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
				}
			}`,
			expectedError: ErrorInvalidKeystore,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := validator.ValidateKeystoreV3([]byte(tt.json))

			if tt.expectedError == -1 {
				if err != nil {
					t.Errorf("Expected no error, got %v", err)
				}
				return
			}

			if err == nil {
				t.Errorf("Expected error of type %v, got nil", tt.expectedError)
				return
			}

			keystoreErr, ok := err.(*KeystoreImportError)
			if !ok {
				t.Errorf("Expected KeystoreImportError, got %T", err)
				return
			}

			if keystoreErr.Type != tt.expectedError {
				t.Errorf("Expected error type %v, got %v", tt.expectedError, keystoreErr.Type)
			}
		})
	}
}

func TestStrictKeystoreValidationRejectsUnsafeParameters(t *testing.T) {
	key := keystore.NewKeyForDirectICAP(rand.Reader)
	keyJSON, err := keystore.EncryptKey(key, "password", TestScryptN, TestScryptP)
	require.NoError(t, err)

	tests := []struct {
		name      string
		transform func(map[string]interface{})
	}{
		{"dklen below 32", func(data map[string]interface{}) {
			data["crypto"].(map[string]interface{})["kdfparams"].(map[string]interface{})["dklen"] = float64(16)
		}},
		{"numeric string", func(data map[string]interface{}) {
			data["crypto"].(map[string]interface{})["kdfparams"].(map[string]interface{})["n"] = "4096"
		}},
		{"short iv", func(data map[string]interface{}) {
			data["crypto"].(map[string]interface{})["cipherparams"].(map[string]interface{})["iv"] = "00"
		}},
		{"short mac", func(data map[string]interface{}) { data["crypto"].(map[string]interface{})["mac"] = "00" }},
		{"non canonical cipher", func(data map[string]interface{}) { data["crypto"].(map[string]interface{})["cipher"] = "aes-128-cbc" }},
		{"missing id", func(data map[string]interface{}) { delete(data, "id") }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var data map[string]interface{}
			require.NoError(t, json.Unmarshal(keyJSON, &data))
			tt.transform(data)
			modified, marshalErr := json.Marshal(data)
			require.NoError(t, marshalErr)
			_, validationErr := (&KeystoreValidator{}).ValidateKeystoreV3(modified)
			assert.Error(t, validationErr)
		})
	}
}

func TestValidateAddress(t *testing.T) {
	validator := &KeystoreValidator{}

	tests := []struct {
		name          string
		address       string
		expectedError KeystoreErrorType
	}{
		{
			name:          "Valid address with 0x prefix",
			address:       "0x00112233445566778899aabbccddeeff7a6b5c4d",
			expectedError: -1, // No error expected
		},
		{
			name:          "Valid address without 0x prefix",
			address:       "00112233445566778899aabbccddeeff7a6b5c4d",
			expectedError: -1, // No error expected
		},
		{
			name:          "Empty address",
			address:       "",
			expectedError: ErrorMissingRequiredFields,
		},
		{
			name:          "Invalid address - too short",
			address:       "0x00112233445566778899aabbccddeeff",
			expectedError: ErrorInvalidAddress,
		},
		{
			name:          "Invalid address - too long",
			address:       "0x00112233445566778899aabbccddeeff7a6b5c4d3e2f",
			expectedError: ErrorInvalidAddress,
		},
		{
			name:          "Invalid address - non-hex characters",
			address:       "0x00112233445566778899aabbccddeeff7a6b5c4z",
			expectedError: ErrorInvalidAddress,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.ValidateAddress(tt.address)

			if tt.expectedError == -1 {
				if err != nil {
					t.Errorf("Expected no error, got %v", err)
				}
				return
			}

			if err == nil {
				t.Errorf("Expected error of type %v, got nil", tt.expectedError)
				return
			}

			keystoreErr, ok := err.(*KeystoreImportError)
			if !ok {
				t.Errorf("Expected KeystoreImportError, got %T", err)
				return
			}

			if keystoreErr.Type != tt.expectedError {
				t.Errorf("Expected error type %v, got %v", tt.expectedError, keystoreErr.Type)
			}
		})
	}
}

// TestKeystoreImportErrorMethods tests the methods of KeystoreImportError
func TestKeystoreImportErrorMethods(t *testing.T) {
	// Test Error() method
	err1 := NewKeystoreImportError(ErrorInvalidJSON, "O arquivo não contém um JSON válido", nil)
	assert.Equal(t, "O arquivo não contém um JSON válido", err1.Error())

	err2 := NewKeystoreImportError(ErrorInvalidJSON, "O arquivo não contém um JSON válido", assert.AnError)
	assert.Equal(t, "O arquivo não contém um JSON válido: assert.AnError general error for testing", err2.Error())

	// Test Unwrap() method
	assert.Nil(t, err1.Unwrap())
	assert.Equal(t, assert.AnError, err2.Unwrap())

	// Test GetLocalizedMessage() method
	assert.Equal(t, "keystore_invalid_json", err1.GetLocalizedMessage())

	// Test GetLocalizedMessageWithField() method
	err3 := NewKeystoreImportErrorWithField(ErrorMissingRequiredFields, "Missing field", "address", nil)
	assert.Equal(t, "keystore_missing_fields:address", err3.GetLocalizedMessageWithField())
}

// TestKeystoreErrorTypeString tests the String() method of KeystoreErrorType
func TestKeystoreErrorTypeString(t *testing.T) {
	tests := []struct {
		errorType KeystoreErrorType
		expected  string
	}{
		{ErrorFileNotFound, "FILE_NOT_FOUND"},
		{ErrorInvalidJSON, "INVALID_JSON"},
		{ErrorInvalidKeystore, "INVALID_KEYSTORE"},
		{ErrorInvalidVersion, "INVALID_VERSION"},
		{ErrorIncorrectPassword, "INCORRECT_PASSWORD"},
		{ErrorCorruptedFile, "CORRUPTED_FILE"},
		{ErrorAddressMismatch, "ADDRESS_MISMATCH"},
		{ErrorMissingRequiredFields, "MISSING_REQUIRED_FIELDS"},
		{ErrorInvalidAddress, "INVALID_ADDRESS"},
		{KeystoreErrorType(999), "UNKNOWN_ERROR"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.errorType.String())
		})
	}
}

func FuzzValidateKeystoreV3(f *testing.F) {
	f.Add([]byte(`{"version":3}`))
	f.Add([]byte(`not-json`))
	f.Add([]byte{})

	validator := &KeystoreValidator{}
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<20 {
			return
		}
		defer func() {
			if recovered := recover(); recovered != nil {
				t.Fatalf("validator panicked: %v", recovered)
			}
		}()
		_, _ = validator.ValidateKeystoreV3(data)
	})
}

func FuzzDecryptKeySafely(f *testing.F) {
	f.Add([]byte(`{"version":3}`), "password")
	f.Add([]byte(`{"version":3,"id":"00000000-0000-4000-8000-000000000000","address":"0000000000000000000000000000000000000000","crypto":{"cipher":"aes-128-ctr","ciphertext":"00","cipherparams":{"iv":"00"},"kdf":"scrypt","kdfparams":{"dklen":"32","n":"262144","r":"8","p":"1","salt":"00"},"mac":"00"}}`), "password")

	f.Fuzz(func(t *testing.T, data []byte, password string) {
		if len(data) > 1<<20 || len(password) > 1024 {
			return
		}
		defer func() {
			if recovered := recover(); recovered != nil {
				t.Fatalf("safe decrypt panicked: %v", recovered)
			}
		}()
		_, _ = decryptKeySafely(data, password)
	})
}

// TestKeystoreErrorTypeGetLocalizationKey tests the GetLocalizationKey() method of KeystoreErrorType
func TestKeystoreErrorTypeGetLocalizationKey(t *testing.T) {
	tests := []struct {
		errorType KeystoreErrorType
		expected  string
	}{
		{ErrorFileNotFound, "keystore_file_not_found"},
		{ErrorInvalidJSON, "keystore_invalid_json"},
		{ErrorInvalidKeystore, "keystore_invalid_structure"},
		{ErrorInvalidVersion, "keystore_invalid_version"},
		{ErrorIncorrectPassword, "keystore_incorrect_password"},
		{ErrorCorruptedFile, "keystore_corrupted_file"},
		{ErrorAddressMismatch, "keystore_address_mismatch"},
		{ErrorMissingRequiredFields, "keystore_missing_fields"},
		{ErrorInvalidAddress, "keystore_invalid_address"},
		{KeystoreErrorType(999), "unknown_error"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.errorType.GetLocalizationKey())
		})
	}
}
