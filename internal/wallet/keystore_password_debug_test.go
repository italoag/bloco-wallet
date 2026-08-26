package wallet

import (
	"encoding/hex"
	"encoding/json"
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
	correctPassword := "ComplexPassword123!@#"
	wrongPasswords := []string{"password123", "test123", "123456", ""}

	// Lê o arquivo keystore
	keyJSON, err := os.ReadFile(keystorePath)
	require.NoError(t, err)

	// Parse JSON
	var keystoreData map[string]interface{}
	err = json.Unmarshal(keyJSON, &keystoreData)
	require.NoError(t, err)

	// Extrai dados crypto
	cryptoData := keystoreData["crypto"].(map[string]interface{})
	kdfParams := cryptoData["kdfparams"].(map[string]interface{})

	// Extrai parâmetros
	salt, err := hex.DecodeString(kdfParams["salt"].(string))
	require.NoError(t, err)
	n := int(kdfParams["n"].(float64))
	r := int(kdfParams["r"].(float64))
	p := int(kdfParams["p"].(float64))
	dklen := int(kdfParams["dklen"].(float64))

	ciphertext, err := hex.DecodeString(cryptoData["ciphertext"].(string))
	require.NoError(t, err)
	expectedMAC, err := hex.DecodeString(cryptoData["mac"].(string))
	require.NoError(t, err)

	// Testa cada senha
	for _, password := range wrongPasswords {
		_, err := keystore.DecryptKey(keyJSON, password)
		require.Error(t, err)
	}

	key, err := keystore.DecryptKey(keyJSON, correctPassword)
	require.NoError(t, err)
	require.NotNil(t, key)

	// Tenta derivação manual
	derivedKey, err := scrypt.Key([]byte(correctPassword), salt, n, r, p, dklen)
	require.NoError(t, err)

	// Calcula MAC manualmente
	macData := append(append([]byte(nil), derivedKey[16:32]...), ciphertext...)
	calculatedMAC := crypto.Keccak256(macData)
	require.Equal(t, expectedMAC, calculatedMAC)
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

	// Lê o conteúdo do arquivo
	keyJSON, err := os.ReadFile(account.URL.Path)
	require.NoError(t, err)

	// Testa se consegue descriptografar
	key, err := keystore.DecryptKey(keyJSON, password)
	require.NoError(t, err)
	require.Equal(t, account.Address, key.Address)
}
