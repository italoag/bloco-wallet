package wallet

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdsa"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"

	"github.com/ethereum/go-ethereum/crypto"
)

// EnhancedKeyStoreService versão melhorada com suporte universal a KDF
type EnhancedKeyStoreService struct {
	kdfService *UniversalKDFService
	logger     KDFLogger
}

// NewEnhancedKeyStoreService cria serviço melhorado
func NewEnhancedKeyStoreService() *EnhancedKeyStoreService {
	return &EnhancedKeyStoreService{
		kdfService: NewUniversalKDFService(),
		logger:     &SimpleKDFLogger{},
	}
}

// ReadKeyStore versão melhorada que suporta qualquer KDF
func (eks *EnhancedKeyStoreService) ReadKeyStore(filePath, password string) (*EnhancedWalletDetails, error) {
	// Lê o arquivo JSON
	data, err := ioutil.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("erro ao ler arquivo: %w", err)
	}

	// Deserializa o JSON
	var keystore KeystoreV3
	if err := json.Unmarshal(data, &keystore); err != nil {
		return nil, fmt.Errorf("erro ao deserializar JSON: %w", err)
	}

	// Valida a versão
	if keystore.Version != 3 {
		return nil, errors.New("versão de KeyStore não suportada")
	}

	// NOVO: Análise de compatibilidade antes de processar
	compatReport := eks.analyzeCompatibility(map[string]interface{}{
		"crypto":  keystore.Crypto,
		"version": keystore.Version,
	})

	if !compatReport.Compatible {
		return nil, fmt.Errorf("KeyStore incompatível: %v", compatReport.Issues)
	}

	// Log da análise
	if kdfParams, ok := keystore.Crypto.KDFParams.(map[string]interface{}); ok {
		eks.logger.LogKDFAttempt(compatReport.KDFType, kdfParams)
	}
	if compatReport.SecurityLevel == "Low" {
		fmt.Printf("⚠️ Aviso: Parâmetros de segurança baixa detectados\n")
	}

	// MELHORADO: Deriva a chave usando serviço universal
	cryptoParams := &CryptoParams{
		KDF:          keystore.Crypto.KDF,
		KDFParams:    keystore.Crypto.KDFParams.(map[string]interface{}),
		Cipher:       keystore.Crypto.Cipher,
		CipherText:   keystore.Crypto.CipherText,
		CipherParams: map[string]interface{}{"iv": keystore.Crypto.CipherParams.IV},
		MAC:          keystore.Crypto.MAC,
	}

	derivedKey, err := eks.kdfService.DeriveKey(password, cryptoParams)
	if err != nil {
		return nil, fmt.Errorf("erro ao derivar chave: %w", err)
	}

	// Verifica a integridade usando MAC
	if err := eks.verifyMAC(derivedKey, cryptoParams); err != nil {
		return nil, fmt.Errorf("senha incorreta ou arquivo corrompido: %w", err)
	}

	// Descriptografa a chave privada
	privateKeyBytes, err := eks.decryptPrivateKey(derivedKey, cryptoParams)
	if err != nil {
		return nil, fmt.Errorf("erro ao descriptografar chave privada: %w", err)
	}

	// Gera informações da carteira
	walletDetails, err := eks.generateWalletInfo(privateKeyBytes)
	if err != nil {
		return nil, fmt.Errorf("erro ao gerar informações da carteira: %w", err)
	}

	// NOVO: Adiciona informações de compatibilidade ao resultado
	walletDetails.KDFInfo = &KDFInfo{
		Type:          compatReport.KDFType,
		NormalizedType: compatReport.NormalizedKDF,
		SecurityLevel: compatReport.SecurityLevel,
		Parameters:    compatReport.Parameters,
	}

	return walletDetails, nil
}

// analyzeCompatibility analisa compatibilidade do KeyStore
func (eks *EnhancedKeyStoreService) analyzeCompatibility(keystoreData map[string]interface{}) *CompatibilityReport {
	analyzer := NewKDFCompatibilityAnalyzer()
	return analyzer.AnalyzeKeyStoreCompatibility(keystoreData)
}

// verifyMAC versão melhorada da verificação MAC
func (eks *EnhancedKeyStoreService) verifyMAC(derivedKey []byte, cryptoParams *CryptoParams) error {
	// Usa os últimos 16 bytes da chave derivada para MAC
	macKey := derivedKey[16:32]
	
	cipherText, err := hex.DecodeString(cryptoParams.CipherText)
	if err != nil {
		return fmt.Errorf("erro ao decodificar ciphertext: %w", err)
	}

	// Calcula MAC usando Keccak256 (padrão Ethereum)
	hash := crypto.Keccak256Hash(macKey, cipherText)
	calculatedMAC := hex.EncodeToString(hash.Bytes())

	if calculatedMAC != cryptoParams.MAC {
		return errors.New("MAC inválido - senha incorreta ou arquivo corrompido")
	}

	return nil
}

// decryptPrivateKey descriptografa a chave privada
func (eks *EnhancedKeyStoreService) decryptPrivateKey(derivedKey []byte, cryptoParams *CryptoParams) ([]byte, error) {
	// Suporta diferentes algoritmos de cifra
	switch cryptoParams.Cipher {
	case "aes-128-ctr":
		return eks.decryptAESCTR(derivedKey, cryptoParams)
	case "aes-128-cbc":
		return eks.decryptAESCBC(derivedKey, cryptoParams)
	default:
		return nil, fmt.Errorf("algoritmo de cifra não suportado: %s", cryptoParams.Cipher)
	}
}

// decryptAESCTR descriptografa usando AES-128-CTR
func (eks *EnhancedKeyStoreService) decryptAESCTR(derivedKey []byte, cryptoParams *CryptoParams) ([]byte, error) {
	// Usa os primeiros 16 bytes da chave derivada para descriptografia
	key := derivedKey[:16]
	
	ivInterface, exists := cryptoParams.CipherParams["iv"]
	if !exists {
		return nil, fmt.Errorf("IV não encontrado nos parâmetros de cifra")
	}
	
	ivStr, ok := ivInterface.(string)
	if !ok {
		return nil, fmt.Errorf("IV deve ser uma string")
	}

	iv, err := hex.DecodeString(ivStr)
	if err != nil {
		return nil, fmt.Errorf("erro ao decodificar IV: %w", err)
	}

	cipherText, err := hex.DecodeString(cryptoParams.CipherText)
	if err != nil {
		return nil, fmt.Errorf("erro ao decodificar ciphertext: %w", err)
	}

	// Cria cipher AES-CTR
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("erro ao criar cipher AES: %w", err)
	}

	stream := cipher.NewCTR(block, iv)
	
	// Descriptografa
	plaintext := make([]byte, len(cipherText))
	stream.XORKeyStream(plaintext, cipherText)

	return plaintext, nil
}

// decryptAESCBC descriptografa usando AES-128-CBC (para compatibilidade)
func (eks *EnhancedKeyStoreService) decryptAESCBC(derivedKey []byte, cryptoParams *CryptoParams) ([]byte, error) {
	// Implementação básica de AES-CBC
	key := derivedKey[:16]
	
	ivInterface, exists := cryptoParams.CipherParams["iv"]
	if !exists {
		return nil, fmt.Errorf("IV não encontrado nos parâmetros de cifra")
	}
	
	ivStr, ok := ivInterface.(string)
	if !ok {
		return nil, fmt.Errorf("IV deve ser uma string")
	}

	iv, err := hex.DecodeString(ivStr)
	if err != nil {
		return nil, fmt.Errorf("erro ao decodificar IV: %w", err)
	}

	cipherText, err := hex.DecodeString(cryptoParams.CipherText)
	if err != nil {
		return nil, fmt.Errorf("erro ao decodificar ciphertext: %w", err)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("erro ao criar cipher AES: %w", err)
	}

	if len(cipherText)%aes.BlockSize != 0 {
		return nil, errors.New("ciphertext não é múltiplo do tamanho do bloco")
	}

	mode := cipher.NewCBCDecrypter(block, iv)
	plaintext := make([]byte, len(cipherText))
	mode.CryptBlocks(plaintext, cipherText)

	// Remove padding PKCS7
	return removePKCS7Padding(plaintext)
}

// removePKCS7Padding remove padding PKCS7
func removePKCS7Padding(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, errors.New("dados vazios")
	}
	
	padding := int(data[len(data)-1])
	if padding > len(data) || padding == 0 {
		return nil, errors.New("padding inválido")
	}
	
	for i := len(data) - padding; i < len(data); i++ {
		if int(data[i]) != padding {
			return nil, errors.New("padding PKCS7 inválido")
		}
	}
	
	return data[:len(data)-padding], nil
}

// generateWalletInfo gera informações da carteira
func (eks *EnhancedKeyStoreService) generateWalletInfo(privateKeyBytes []byte) (*EnhancedWalletDetails, error) {
	// Converte para chave privada ECDSA
	privateKey, err := crypto.ToECDSA(privateKeyBytes)
	if err != nil {
		return nil, fmt.Errorf("erro ao converter chave privada: %w", err)
	}

	// Gera chave pública
	publicKey := privateKey.Public().(*ecdsa.PublicKey)

	// Gera endereço Ethereum
	address := crypto.PubkeyToAddress(*publicKey)

	// Validações de segurança
	if err := eks.validateEthereumKey(privateKey); err != nil {
		return nil, fmt.Errorf("chave inválida para Ethereum: %w", err)
	}

	// Cria wallet temporário para os detalhes
	wallet := &EnhancedWallet{
		Address: address.Hex(),
	}

	return &EnhancedWalletDetails{
		Wallet:     wallet,
		PrivateKey: privateKey,
		PublicKey:  publicKey,
		// KDFInfo será preenchido pelo método ReadKeyStore
	}, nil
}

// validateEthereumKey valida chave para Ethereum
func (eks *EnhancedKeyStoreService) validateEthereumKey(privateKey *ecdsa.PrivateKey) error {
	// Verifica se a chave privada não é zero
	if privateKey.D.Sign() == 0 {
		return errors.New("chave privada não pode ser zero")
	}

	// Verifica se a chave privada está no range válido
	if privateKey.D.Cmp(privateKey.Curve.Params().N) >= 0 {
		return errors.New("chave privada fora do range válido")
	}

	return nil
}