package wallet

import (
	"blocowallet/pkg/config"
	"blocowallet/pkg/localization"
	"encoding/base64"
	"os"
	"strings"
	"testing"
)

// setupTestConfig cria uma configuração de teste com parâmetros seguros
func setupTestConfig(t *testing.T) *config.Config {
	// Criar uma configuração de teste
	cfg := &config.Config{
		Security: config.SecurityConfig{
			Argon2Time:    1,
			Argon2Memory:  64 * 1024, // 64MB
			Argon2Threads: 4,
			Argon2KeyLen:  32,
			SaltLength:    16,
		},
	}

	// Retornar a configuração
	return cfg
}

func TestArgon2IDParameters(t *testing.T) {
	// Obter a configuração de teste
	cfg := setupTestConfig(t)

	// Testar se os parâmetros do Argon2ID são razoáveis
	if cfg.Security.Argon2Time < 1 {
		t.Fatal("Argon2ID time parameter should be at least 1")
	}

	if cfg.Security.Argon2Memory < 64*1024 {
		t.Fatal("Argon2ID memory parameter should be at least 64KB")
	}

	if cfg.Security.Argon2Threads < 1 {
		t.Fatal("Argon2ID threads parameter should be at least 1")
	}

	if cfg.Security.Argon2KeyLen < 32 {
		t.Fatal("Argon2ID key length should be at least 32 bytes")
	}
}

func TestSaltGeneration(t *testing.T) {
	// Obter a configuração de teste
	cfg := setupTestConfig(t)

	// Inicializar o serviço de criptografia para testes
	cryptoService := NewCryptoService(cfg)

	// Inicializar as mensagens para testes
	localization.InitCryptoMessagesForTesting()

	// We can't test the internal generateSalt function directly since it's not exported
	// Instead, we'll test that encryption generates different results each time
	mnemonic := "test mnemonic"
	password := "password"

	encrypted1, err := cryptoService.EncryptMnemonic(mnemonic, password)
	if err != nil {
		t.Fatalf("Failed to encrypt first time: %v", err)
	}

	encrypted2, err := cryptoService.EncryptMnemonic(mnemonic, password)
	if err != nil {
		t.Fatalf("Failed to encrypt second time: %v", err)
	}

	// Different encryptions should produce different results due to random salt
	if encrypted1 == encrypted2 {
		t.Fatal("Encrypted results should be different due to random salt")
	}
}

// TestPasswordSecurity tests various password scenarios
func TestPasswordSecurity(t *testing.T) {
	// Obter a configuração de teste
	cfg := setupTestConfig(t)

	// Inicializar o serviço de criptografia para testes
	cryptoService := NewCryptoService(cfg)

	// Inicializar as mensagens para testes
	localization.InitCryptoMessagesForTesting()

	testCases := []struct {
		name        string
		password    string
		shouldError bool
	}{
		{"Empty password", "", true},
		{"Single character", "a", false},
		{"Normal password", "password123", false},
		{"Long password", strings.Repeat("x", 1000), false},
		{"Unicode password", "пароль123", false},
		{"Special characters", "!@#$%^&*()", false},
		{"Spaces in password", "pass word 123", false},
		{"Only numbers", "123456789", false},
		{"Mixed case", "PassWord123", false},
	}

	mnemonic := "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			encrypted, err := cryptoService.EncryptMnemonic(mnemonic, tc.password)
			if tc.shouldError {
				if err == nil {
					t.Fatal("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			// Test decryption
			decrypted, err := cryptoService.DecryptMnemonic(encrypted, tc.password)
			if err != nil {
				t.Fatalf("Failed to decrypt: %v", err)
			}

			if decrypted != mnemonic {
				t.Fatalf("Decrypted mnemonic doesn't match. Expected %q, got %q", mnemonic, decrypted)
			}
		})
	}
}

// TestMnemonicSecurity tests various mnemonic scenarios
func TestMnemonicSecurity(t *testing.T) {
	// Obter a configuração de teste
	cfg := setupTestConfig(t)

	// Inicializar o serviço de criptografia para testes
	cryptoService := NewCryptoService(cfg)

	// Inicializar as mensagens para testes
	localization.InitCryptoMessagesForTesting()

	testCases := []struct {
		name     string
		mnemonic string
	}{
		{"Empty mnemonic", ""},
		{"Standard mnemonic", "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"},
		{"Short mnemonic", "test"},
		{"Long mnemonic", strings.Repeat("word ", 100)},
		{"Unicode mnemonic", "слово слово слово слово слово слово слово слово слово слово слово слово"},
		{"Special characters", "!@#$ %^&* ()_+"},
		{"Numbers only", "1 2 3 4 5 6 7 8 9 10 11 12"},
		{"Mixed content", "word1 word2 word3 word4 word5 word6 word7 word8 word9 word10 word11 word12"},
	}

	password := "secure_password"

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			encrypted, err := cryptoService.EncryptMnemonic(tc.mnemonic, password)
			if err != nil {
				t.Fatalf("Failed to encrypt: %v", err)
			}

			decrypted, err := cryptoService.DecryptMnemonic(encrypted, password)
			if err != nil {
				t.Fatalf("Failed to decrypt: %v", err)
			}

			if decrypted != tc.mnemonic {
				t.Fatalf("Decrypted mnemonic doesn't match. Expected %q, got %q", tc.mnemonic, decrypted)
			}
		})
	}
}

// TestIncorrectPassword tests decryption with incorrect password
func TestIncorrectPassword(t *testing.T) {
	// Obter a configuração de teste
	cfg := setupTestConfig(t)

	// Inicializar o serviço de criptografia para testes
	cryptoService := NewCryptoService(cfg)

	// Inicializar as mensagens para testes
	localization.InitCryptoMessagesForTesting()

	mnemonic := "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"
	correctPassword := "correct_password"
	wrongPassword := "wrong_password"

	encrypted, err := cryptoService.EncryptMnemonic(mnemonic, correctPassword)
	if err != nil {
		t.Fatalf("Failed to encrypt: %v", err)
	}

	// Trying to decrypt with wrong password should fail
	_, err = cryptoService.DecryptMnemonic(encrypted, wrongPassword)
	if err == nil {
		t.Fatal("Expected error with incorrect password but got none")
	}

	// Verify with correct password should succeed
	if !cryptoService.VerifyMnemonicPassword(encrypted, correctPassword) {
		t.Fatal("Verification with correct password failed but should succeed")
	}

	// Verify with wrong password should fail
	if cryptoService.VerifyMnemonicPassword(encrypted, wrongPassword) {
		t.Fatal("Verification with wrong password succeeded but should fail")
	}
}

// TestInvalidEncryptedData tests decryption with invalid data
func TestInvalidEncryptedData(t *testing.T) {
	// Obter a configuração de teste
	cfg := setupTestConfig(t)

	// Inicializar o serviço de criptografia para testes
	cryptoService := NewCryptoService(cfg)

	// Inicializar as mensagens para testes
	localization.InitCryptoMessagesForTesting()

	testCases := []struct {
		name          string
		encryptedData string
	}{
		{"Empty string", ""},
		{"Invalid base64", "not-base64-data"},
		{"Too short after decode", base64.StdEncoding.EncodeToString([]byte("tooshort"))},
		{"Random bytes", base64.StdEncoding.EncodeToString([]byte(strings.Repeat("x", 100)))},
	}

	password := "password"

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := cryptoService.DecryptMnemonic(tc.encryptedData, password)
			if err == nil {
				t.Fatal("Expected error with invalid data but got none")
			}
		})
	}
}

// TestConfigParamsEffect testa se alterações nos parâmetros de configuração afetam o resultado da criptografia
func TestConfigParamsEffect(t *testing.T) {
	// Criar duas configurações com parâmetros diferentes
	cfg1 := &config.Config{
		Security: config.SecurityConfig{
			Argon2Time:    1,
			Argon2Memory:  64 * 1024,
			Argon2Threads: 4,
			Argon2KeyLen:  32,
			SaltLength:    16,
		},
	}

	cfg2 := &config.Config{
		Security: config.SecurityConfig{
			Argon2Time:    2,          // Alterado
			Argon2Memory:  128 * 1024, // Alterado
			Argon2Threads: 4,
			Argon2KeyLen:  32,
			SaltLength:    16,
		},
	}

	// Inicializar as mensagens para testes
	localization.InitCryptoMessagesForTesting()

	// Criar serviços de criptografia com as diferentes configurações
	cryptoService1 := NewCryptoService(cfg1)
	cryptoService2 := NewCryptoService(cfg2)

	// Dados de teste
	mnemonic := "test mnemonic"
	password := "password"

	// Criptografar com os dois serviços
	encrypted1, err := cryptoService1.EncryptMnemonic(mnemonic, password)
	if err != nil {
		t.Fatalf("Failed to encrypt with first config: %v", err)
	}

	encrypted2, err := cryptoService2.EncryptMnemonic(mnemonic, password)
	if err != nil {
		t.Fatalf("Failed to encrypt with second config: %v", err)
	}

	// Tentar descriptografar usando o serviço com parâmetros diferentes
	// A descriptografia deve falhar se os parâmetros tiverem efeito real
	_, err1 := cryptoService2.DecryptMnemonic(encrypted1, password)
	_, err2 := cryptoService1.DecryptMnemonic(encrypted2, password)

	// Pelo menos uma das tentativas deve falhar se os parâmetros realmente afetam a derivação da chave
	if err1 == nil && err2 == nil {
		t.Fatal("Expected at least one decryption to fail with different parameters")
	}
}

func TestCryptoHelpersNeverInitializeFromHome(t *testing.T) {
	previous := defaultCryptoService
	defaultCryptoService = nil
	t.Cleanup(func() {
		defaultCryptoService = previous
	})
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	_, encryptErr := EncryptMnemonic("mnemonic", "password")
	_, decryptErr := DecryptMnemonic("ciphertext", "password")

	if encryptErr == nil || decryptErr == nil {
		t.Fatal("uninitialized crypto helpers must fail closed")
	}
	entries, err := os.ReadDir(homeDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatal("crypto helpers wrote to the user home")
	}
}

func FuzzDecryptMnemonic(f *testing.F) {
	localization.InitCryptoMessagesForTesting()
	service := NewCryptoService(&config.Config{Security: config.SecurityConfig{
		Argon2Time:    1,
		Argon2Memory:  8,
		Argon2Threads: 1,
		Argon2KeyLen:  32,
		SaltLength:    16,
	}})
	f.Add("", "")
	f.Add("not-base64", "password")
	f.Add(base64.StdEncoding.EncodeToString(make([]byte, 48)), "password")

	f.Fuzz(func(t *testing.T, encrypted, password string) {
		if len(encrypted) > 4096 || len(password) > 1024 {
			return
		}
		defer func() {
			if recovered := recover(); recovered != nil {
				t.Fatalf("decrypt panicked: %v", recovered)
			}
		}()
		_, _ = service.DecryptMnemonic(encrypted, password)
	})
}
