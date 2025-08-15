package wallet

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/keystore"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/scrypt"
)

// TestKeystorePasswordDebug verifica se a senha está correta para o keystore
func TestKeystorePasswordDebug(t *testing.T) {
	keystorePath := "testdata/keystores/real_keystore_v3_complex_password.json"

	// Tenta diferentes senhas possíveis
	passwords := []string{
		"ComplexPassword123!@#",
		"password123",
		"test123",
		"123456",
		"",
	}

	// Lê o arquivo keystore
	keyJSON, err := os.ReadFile(keystorePath)
	require.NoError(t, err)

	fmt.Printf("=== Keystore Password Debug ===\n")
	fmt.Printf("File: %s\n", keystorePath)
	fmt.Printf("Size: %d bytes\n", len(keyJSON))

	// Parse JSON
	var keystoreData map[string]interface{}
	err = json.Unmarshal(keyJSON, &keystoreData)
	require.NoError(t, err)

	// Extrai dados crypto
	cryptoData := keystoreData["crypto"].(map[string]interface{})
	kdfParams := cryptoData["kdfparams"].(map[string]interface{})

	// Extrai parâmetros
	salt, _ := hex.DecodeString(kdfParams["salt"].(string))
	n := int(kdfParams["n"].(float64))
	r := int(kdfParams["r"].(float64))
	p := int(kdfParams["p"].(float64))
	dklen := int(kdfParams["dklen"].(float64))

	ciphertext, _ := hex.DecodeString(cryptoData["ciphertext"].(string))
	expectedMAC, _ := hex.DecodeString(cryptoData["mac"].(string))

	fmt.Printf("KDF Parameters:\n")
	fmt.Printf("  Salt: %x\n", salt)
	fmt.Printf("  N: %d\n", n)
	fmt.Printf("  R: %d\n", r)
	fmt.Printf("  P: %d\n", p)
	fmt.Printf("  DKLen: %d\n", dklen)
	fmt.Printf("Expected MAC: %x\n", expectedMAC)

	// Testa cada senha
	for i, password := range passwords {
		fmt.Printf("\n--- Testing password %d: '%s' ---\n", i+1, password)

		// Tenta com keystore padrão do Ethereum
		key, err := keystore.DecryptKey(keyJSON, password)
		if err == nil {
			fmt.Printf("✅ SUCCESS with standard keystore decryption!\n")
			fmt.Printf("   Address: %s\n", key.Address.Hex())
			return
		} else {
			fmt.Printf("❌ Standard keystore failed: %v\n", err)
		}

		// Tenta derivação manual
		derivedKey, err := scrypt.Key([]byte(password), salt, n, r, p, dklen)
		if err != nil {
			fmt.Printf("❌ Scrypt derivation failed: %v\n", err)
			continue
		}

		// Calcula MAC manualmente
		macData := append(derivedKey[16:32], ciphertext...)
		calculatedMAC := crypto.Keccak256(macData)

		fmt.Printf("   Derived key: %x\n", derivedKey)
		fmt.Printf("   MAC data: %x\n", macData)
		fmt.Printf("   Calculated MAC: %x\n", calculatedMAC)
		fmt.Printf("   Expected MAC:   %x\n", expectedMAC)

		if hex.EncodeToString(calculatedMAC) == hex.EncodeToString(expectedMAC) {
			fmt.Printf("✅ MAC matches! Correct password: '%s'\n", password)
			return
		} else {
			fmt.Printf("❌ MAC doesn't match\n")
		}
	}

	fmt.Printf("\n❌ None of the tested passwords worked\n")
	t.Fatal("Could not find correct password")
}

// TestGenerateNewKeystoreForTesting gera um novo keystore para testes
func TestGenerateNewKeystoreForTesting(t *testing.T) {
	// t.Skip("Only run when needed to generate new test keystore")

	tempDir := t.TempDir()
	ks := keystore.NewKeyStore(tempDir, keystore.StandardScryptN, keystore.StandardScryptP)

	password := "ComplexPassword123!@#"

	// Cria uma nova conta
	account, err := ks.NewAccount(password)
	require.NoError(t, err)

	fmt.Printf("Generated new keystore:\n")
	fmt.Printf("Address: %s\n", account.Address.Hex())
	fmt.Printf("Path: %s\n", account.URL.Path)

	// Lê o conteúdo do arquivo
	keyJSON, err := os.ReadFile(account.URL.Path)
	require.NoError(t, err)

	fmt.Printf("Keystore content:\n%s\n", string(keyJSON))

	// Testa se consegue descriptografar
	key, err := keystore.DecryptKey(keyJSON, password)
	require.NoError(t, err)

	fmt.Printf("✅ Successfully decrypted with password: %s\n", password)
	fmt.Printf("Address matches: %v\n", key.Address == account.Address)
}
