package wallet

import (
	"crypto/ecdsa"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ethereum/go-ethereum/accounts/keystore"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/tyler-smith/go-bip32"
	"github.com/tyler-smith/go-bip39"
)

type WalletDetails struct {
	Wallet       *Wallet
	Mnemonic     *string        // Nullable for non-mnemonic imports
	PrivateKey   *ecdsa.PrivateKey
	PublicKey    *ecdsa.PublicKey
	ImportMethod ImportMethod   // Track import method
	HasMnemonic  bool           // Helper field for UI
	KDFInfo      *KDFInfo       // KDF analysis information
}

type WalletService struct {
	Repo     WalletRepository
	KeyStore *keystore.KeyStore
}

func NewWalletService(repo WalletRepository, ks *keystore.KeyStore) *WalletService {
	// Verify that CryptoService is initialized
	if defaultCryptoService == nil {
		panic("CryptoService must be initialized before creating WalletService. Call wallet.InitCryptoService(cfg) first.")
	}

	return &WalletService{
		Repo:     repo,
		KeyStore: ks,
	}
}

func (ws *WalletService) CreateWallet(name, password string) (*WalletDetails, error) {
	mnemonic, err := GenerateMnemonic()
	if err != nil {
		return nil, err
	}

	privateKeyHex, err := DerivePrivateKey(mnemonic)
	if err != nil {
		return nil, err
	}

	privKey, err := HexToECDSA(privateKeyHex)
	if err != nil {
		return nil, err
	}

	account, err := ws.KeyStore.ImportECDSA(privKey, password)
	if err != nil {
		return nil, err
	}

	originalPath := account.URL.Path
	newFilename := fmt.Sprintf("%s.json", account.Address.Hex())
	newPath := filepath.Join(filepath.Dir(originalPath), newFilename)
	err = os.Rename(originalPath, newPath)
	if err != nil {
		return nil, fmt.Errorf("error renaming the wallet file: %v", err)
	}

	// Encrypt the mnemonic before storing
	encryptedMnemonic, err := EncryptMnemonic(mnemonic, password)
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt mnemonic: %v", err)
	}

	wallet := &Wallet{
		Name:         name,
		Address:      account.Address.Hex(),
		KeyStorePath: newPath,
		Mnemonic:     &encryptedMnemonic, // Store the encrypted mnemonic
		ImportMethod: string(ImportMethodMnemonic),
		SourceHash:   (&SourceHashGenerator{}).GenerateFromMnemonic(mnemonic),
	}

	if err = ws.Repo.AddWallet(wallet); err != nil {
		return nil, err
	}

	walletDetails := &WalletDetails{
		Wallet:       wallet,
		Mnemonic:     &mnemonic,
		PrivateKey:   privKey,
		PublicKey:    &privKey.PublicKey,
		ImportMethod: ImportMethodMnemonic,
		HasMnemonic:  true,
	}

	return walletDetails, nil
}

func (ws *WalletService) ImportWallet(name, mnemonic, password string) (*WalletDetails, error) {
	// 5.2 Validate mnemonic before any processing
	if !bip39.IsMnemonicValid(mnemonic) {
		return nil, NewInvalidImportDataError(string(ImportMethodMnemonic), "Invalid mnemonic phrase")
	}

	// 5.1 Generate source hash and check duplicates by mnemonic-based source
	hashGen := &SourceHashGenerator{}
	sourceHash := hashGen.GenerateFromMnemonic(mnemonic)
	if existingWallet, err := ws.Repo.FindBySourceHash(sourceHash); err == nil && existingWallet != nil {
		return nil, NewDuplicateWalletError(string(ImportMethodMnemonic), existingWallet.Address, "A wallet with this mnemonic phrase already exists")
	} else if err != nil {
		return nil, err
	}

	privateKeyHex, err := DerivePrivateKey(mnemonic)
	if err != nil {
		return nil, err
	}

	privKey, err := HexToECDSA(privateKeyHex)
	if err != nil {
		return nil, err
	}

	account, err := ws.KeyStore.ImportECDSA(privKey, password)
	if err != nil {
		return nil, err
	}

	originalPath := account.URL.Path
	newFilename := fmt.Sprintf("%s.json", account.Address.Hex())
	newPath := filepath.Join(filepath.Dir(originalPath), newFilename)
	err = os.Rename(originalPath, newPath)
	if err != nil {
		return nil, fmt.Errorf("error renaming the wallet file: %v", err)
	}

	// Encrypt the mnemonic before storing
	encryptedMnemonic, err := EncryptMnemonic(mnemonic, password)
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt mnemonic: %v", err)
	}

 wallet := &Wallet{
		Name:         name,
		Address:      account.Address.Hex(),
		KeyStorePath: newPath,
		Mnemonic:     &encryptedMnemonic, // Store the encrypted mnemonic
		ImportMethod: string(ImportMethodMnemonic),
		SourceHash:   (&SourceHashGenerator{}).GenerateFromMnemonic(mnemonic),
	}

	if err = ws.Repo.AddWallet(wallet); err != nil {
		return nil, err
	}

	walletDetails := &WalletDetails{
		Wallet:       wallet,
		Mnemonic:     &mnemonic,
		PrivateKey:   privKey,
		PublicKey:    &privKey.PublicKey,
		ImportMethod: ImportMethodMnemonic,
		HasMnemonic:  true,
	}

	return walletDetails, nil
}

func (ws *WalletService) ImportWalletFromPrivateKey(name, privateKeyHex, password string) (*WalletDetails, error) {
	// Remove "0x" prefix if present
	if len(privateKeyHex) > 2 && privateKeyHex[:2] == "0x" {
		privateKeyHex = privateKeyHex[2:]
	}

	// Validate private key format
	if len(privateKeyHex) != 64 {
		return nil, fmt.Errorf("invalid private key format")
	}

	// Convert hex to ECDSA private key
	privKey, err := HexToECDSA(privateKeyHex)
	if err != nil {
		return nil, fmt.Errorf("invalid private key: %v", err)
	}

	// Generate a deterministic mnemonic from private key
	mnemonic, err := GenerateDeterministicMnemonic(privKey)
	if err != nil {
		return nil, fmt.Errorf("error generating deterministic mnemonic: %v", err)
	}

	// Import the private key to keystore
	account, err := ws.KeyStore.ImportECDSA(privKey, password)
	if err != nil {
		return nil, err
	}

	// Rename the keystore file to match Ethereum address
	originalPath := account.URL.Path
	newFilename := fmt.Sprintf("%s.json", account.Address.Hex())
	newPath := filepath.Join(filepath.Dir(originalPath), newFilename)
	err = os.Rename(originalPath, newPath)
	if err != nil {
		return nil, fmt.Errorf("error renaming the wallet file: %v", err)
	}

	// Encrypt the mnemonic before storing
	encryptedMnemonic, err := EncryptMnemonic(mnemonic, password)
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt mnemonic: %v", err)
	}

 // Create the wallet entry with the encrypted mnemonic
 wallet := &Wallet{
 	Name:         name,
 	Address:      account.Address.Hex(),
 	KeyStorePath: newPath,
 	Mnemonic:     &encryptedMnemonic, // Store the encrypted mnemonic
 	ImportMethod: string(ImportMethodPrivateKey),
 	SourceHash:   (&SourceHashGenerator{}).GenerateFromPrivateKey(privateKeyHex),
 }

 // Add wallet to repository
 if err = ws.Repo.AddWallet(wallet); err != nil {
 	return nil, err
 }

 // Return wallet details with the generated mnemonic
 walletDetails := &WalletDetails{
 	Wallet:       wallet,
 	Mnemonic:     &mnemonic,
 	PrivateKey:   privKey,
 	PublicKey:    &privKey.PublicKey,
 	ImportMethod: ImportMethodPrivateKey,
 	HasMnemonic:  true,
 }

 return walletDetails, nil
}

// ImportWalletFromKeystoreV3 imports a wallet from a keystore v3 file with Universal KDF support
func (ws *WalletService) ImportWalletFromKeystoreV3(name, keystorePath, password string) (*WalletDetails, error) {
	// Step 1: Validate file existence
	if _, err := os.Stat(keystorePath); os.IsNotExist(err) {
		return nil, NewKeystoreImportError(
			ErrorFileNotFound,
			"Keystore file not found at specified path",
			err,
		)
	}

	// Step 2: Read the keystore file
	keyJSON, err := os.ReadFile(keystorePath)
	if err != nil {
		return nil, NewKeystoreImportError(
			ErrorFileNotFound,
			"Error reading the keystore file",
			err,
		)
	}

	// Step 3: Generate source hash from keystore JSON content for duplicate detection
	hashGen := &SourceHashGenerator{}
	sourceHash := hashGen.GenerateFromKeystore(keyJSON)

	// Step 4: Check for duplicates based on source hash (when model is updated)
	// For now, we'll skip this check until the wallet model is updated with SourceHash field
	// TODO: Uncomment when wallet model includes SourceHash field
	_ = sourceHash // Prevent unused variable warning
	/*
		if existingWallet, err := ws.Repo.FindBySourceHash(sourceHash); err == nil && existingWallet != nil {
			return nil, NewDuplicateWalletError(
				string(ImportMethodKeystore),
				existingWallet.Address,
				"A wallet with this keystore already exists",
			)
		}
	*/

	// Step 5: Initialize Universal KDF Service for compatibility analysis
	kdfService := NewUniversalKDFService()
	compatAnalyzer := NewKDFCompatibilityAnalyzer()

	// Step 6: Parse keystore JSON for compatibility analysis
	var keystoreMap map[string]interface{}
	if err := json.Unmarshal(keyJSON, &keystoreMap); err != nil {
		return nil, NewKeystoreImportError(
			ErrorInvalidJSON,
			"Error parsing keystore JSON for compatibility analysis",
			err,
		)
	}

	// Step 7: Perform compatibility analysis before processing
	compatReport := compatAnalyzer.AnalyzeKeyStoreCompatibility(keystoreMap)
	if !compatReport.Compatible {
		return nil, NewKeystoreImportError(
			ErrorInvalidKeystore,
			fmt.Sprintf("Keystore incompatible: %v", compatReport.Issues),
			nil,
		)
	}

	// Step 7a: Security analysis (logging removed to avoid cluttering TUI)
	// Security warnings and info are now handled internally without console output
	// The security level is still available in walletDetails.KDFInfo for programmatic use

	// Step 8: Validate keystore structure using existing validator
	validator := &KeystoreValidator{}
	keystoreData, err := validator.ValidateKeystoreV3(keyJSON)
	if err != nil {
		return nil, err
	}

	// Step 9: Use Universal KDF Service to decrypt the keystore
	cryptoData, ok := keystoreMap["crypto"].(map[string]interface{})
	if !ok {
		return nil, NewKeystoreImportError(
			ErrorInvalidKeystore,
			"Invalid crypto section in keystore",
			nil,
		)
	}

	kdfParams, ok := cryptoData["kdfparams"].(map[string]interface{})
	if !ok {
		return nil, NewKeystoreImportError(
			ErrorInvalidKeystore,
			"Invalid KDF parameters in keystore",
			nil,
		)
	}

	cryptoParams := &CryptoParams{
		KDF:          keystoreData.Crypto.KDF,
		KDFParams:    kdfParams,
		Cipher:       keystoreData.Crypto.Cipher,
		CipherText:   keystoreData.Crypto.CipherText,
		CipherParams: map[string]interface{}{"iv": keystoreData.Crypto.CipherParams.IV},
		MAC:          keystoreData.Crypto.MAC,
	}

	// Step 10: Derive key using Universal KDF Service
	derivedKey, err := kdfService.DeriveKey(password, cryptoParams)
	if err != nil {
		// Provide KDF-specific error context
		kdfContext := fmt.Sprintf("KDF: %s (%s), Security Level: %s",
			compatReport.KDFType, compatReport.NormalizedKDF, compatReport.SecurityLevel)
		return nil, NewKeystoreImportError(
			ErrorIncorrectPassword,
			fmt.Sprintf("Failed to derive key using Universal KDF (%s): %v", kdfContext, err),
			err,
		)
	}

	// Step 11: Use Enhanced KeyStore Service for decryption
	enhancedService := NewEnhancedKeyStoreService()

	// Verify MAC using derived key
	if err := enhancedService.verifyMAC(derivedKey, cryptoParams); err != nil {
		return nil, NewKeystoreImportError(
			ErrorIncorrectPassword,
			"Incorrect password or corrupted keystore file",
			err,
		)
	}

	// Step 12: Decrypt private key
	privateKeyBytes, err := enhancedService.decryptPrivateKey(derivedKey, cryptoParams)
	if err != nil {
		return nil, NewKeystoreImportError(
			ErrorCorruptedFile,
			"Failed to decrypt private key",
			err,
		)
	}

	// Step 13: Convert to ECDSA private key
	privateKey, err := crypto.ToECDSA(privateKeyBytes)
	if err != nil {
		return nil, NewKeystoreImportError(
			ErrorCorruptedFile,
			"Invalid private key format",
			err,
		)
	}

	// Step 14: Verify address matches derived address
	derivedAddress := crypto.PubkeyToAddress(privateKey.PublicKey).Hex()
	normalizedKeystoreAddress := common.HexToAddress(keystoreData.Address).Hex()
	normalizedDerivedAddress := common.HexToAddress(derivedAddress).Hex()

	if normalizedKeystoreAddress != normalizedDerivedAddress {
		return nil, NewKeystoreImportError(
			ErrorAddressMismatch,
			fmt.Sprintf("Address mismatch: keystore address %s does not match derived address %s",
				normalizedKeystoreAddress, normalizedDerivedAddress),
			nil,
		)
	}

	// Step 15: Generate deterministic mnemonic from private key (preserving existing behavior)
	mnemonic, err := GenerateAndValidateDeterministicMnemonic(privateKey)
	if err != nil {
		return nil, NewKeystoreImportError(
			ErrorCorruptedFile,
			"Error generating deterministic mnemonic",
			err,
		)
	}

	// Step 16: Create destination path
	address := normalizedDerivedAddress
	destFilename := fmt.Sprintf("%s.json", address)

	var keystoreDir string
	accounts := ws.KeyStore.Accounts()
	if len(accounts) > 0 {
		keystoreDir = filepath.Dir(accounts[0].URL.Path)
	} else {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return nil, NewKeystoreImportError(
				ErrorFileNotFound,
				"Error getting user home directory",
				err,
			)
		}
		keystoreDir = filepath.Join(homeDir, ".wallets", "keystore")

		if err := os.MkdirAll(keystoreDir, 0700); err != nil {
			return nil, NewKeystoreImportError(
				ErrorFileNotFound,
				"Error creating keystore directory",
				err,
			)
		}
	}

	destPath := filepath.Join(keystoreDir, destFilename)

	// Step 17: Copy keystore file to destination
	destFile, err := os.Create(destPath)
	if err != nil {
		return nil, NewKeystoreImportError(
			ErrorFileNotFound,
			"Error creating destination file",
			err,
		)
	}
	defer destFile.Close()

	if _, err = destFile.Write(keyJSON); err != nil {
		return nil, NewKeystoreImportError(
			ErrorFileNotFound,
			"Error writing to destination file",
			err,
		)
	}

	// Step 18: Encrypt mnemonic before storing
	encryptedMnemonic, err := EncryptMnemonic(mnemonic, password)
	if err != nil {
		return nil, NewKeystoreImportError(
			ErrorCorruptedFile,
			"Failed to encrypt mnemonic",
			err,
		)
	}

 // Step 19: Create wallet entry with import method and source hash
 wallet := &Wallet{
 	Name:         name,
 	Address:      address,
 	KeyStorePath: destPath,
 	Mnemonic:     &encryptedMnemonic,
 	ImportMethod: string(ImportMethodKeystore),
 	SourceHash:   sourceHash,
 }

 // Step 20: Add wallet to repository
 if err = ws.Repo.AddWallet(wallet); err != nil {
 	return nil, NewKeystoreImportError(
 		ErrorCorruptedFile,
 		"Failed to add wallet to repository",
 		err,
 	)
 }

 // Step 21: Create KDF information for wallet details
 kdfInfo := &KDFInfo{
		Type:           compatReport.KDFType,
		NormalizedType: compatReport.NormalizedKDF,
		SecurityLevel:  compatReport.SecurityLevel,
		Parameters:     compatReport.Parameters,
	}

	// Step 22: Return enhanced wallet details with KDF information
 walletDetails := &WalletDetails{
		Wallet:       wallet,
		Mnemonic:     &mnemonic,
		PrivateKey:   privateKey,
		PublicKey:    &privateKey.PublicKey,
		ImportMethod: ImportMethodKeystore,
		HasMnemonic:  true, // Keystore imports preserve mnemonic generation
		KDFInfo:      kdfInfo,
	}

	return walletDetails, nil
}

// ImportWalletFromKeystore is kept for backward compatibility
// It calls the new ImportWalletFromKeystoreV3 function
func (ws *WalletService) ImportWalletFromKeystore(name, keystorePath, password string) (*WalletDetails, error) {
	return ws.ImportWalletFromKeystoreV3(name, keystorePath, password)
}

func (ws *WalletService) LoadWallet(wallet *Wallet, password string) (*WalletDetails, error) {
	keyJSON, err := os.ReadFile(wallet.KeyStorePath)
	if err != nil {
		return nil, fmt.Errorf("error reading the wallet file: %v", err)
	}
	key, err := keystore.DecryptKey(keyJSON, password)
	if err != nil {
		return nil, fmt.Errorf("incorrect password")
	}

 // Decrypt the mnemonic
 var mnemonicPtr *string
 if wallet.Mnemonic != nil {
 	decryptedMnemonic, err := DecryptMnemonic(*wallet.Mnemonic, password)
 	if err != nil {
 		return nil, fmt.Errorf("failed to decrypt mnemonic: %v", err)
 	}
 	mnemonicPtr = &decryptedMnemonic
 }

 walletDetails := &WalletDetails{
 	Wallet:       wallet,
 	Mnemonic:     mnemonicPtr,
 	PrivateKey:   key.PrivateKey,
 	PublicKey:    &key.PrivateKey.PublicKey,
 	ImportMethod: ImportMethod(wallet.ImportMethod),
 	HasMnemonic:  wallet.Mnemonic != nil,
 }
 return walletDetails, nil
}

func (ws *WalletService) GetAllWallets() ([]Wallet, error) {
	return ws.Repo.GetAllWallets()
}

func (ws *WalletService) DeleteWallet(wallet *Wallet) error {
	// Remove o arquivo keystore do sistema
	err := os.Remove(wallet.KeyStorePath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove keystore file: %v", err)
	}
	// Remove do banco de dados
	return ws.Repo.DeleteWallet(wallet.ID)
}

// Helper functions

func GenerateMnemonic() (string, error) {
	entropy, err := bip39.NewEntropy(128)
	if err != nil {
		return "", err
	}
	mnemonic, err := bip39.NewMnemonic(entropy)
	if err != nil {
		return "", err
	}
	return mnemonic, nil
}

func DerivePrivateKey(mnemonic string) (string, error) {
	if !bip39.IsMnemonicValid(mnemonic) {
		return "", fmt.Errorf("invalid mnemonic phrase")
	}
	seed := bip39.NewSeed(mnemonic, "")
	masterKey, err := bip32.NewMasterKey(seed)
	if err != nil {
		return "", err
	}
	purposeKey, err := masterKey.NewChildKey(bip32.FirstHardenedChild + 44)
	if err != nil {
		return "", err
	}
	coinTypeKey, err := purposeKey.NewChildKey(bip32.FirstHardenedChild + 60)
	if err != nil {
		return "", err
	}
	accountKey, err := coinTypeKey.NewChildKey(bip32.FirstHardenedChild + 0)
	if err != nil {
		return "", err
	}
	changeKey, err := accountKey.NewChildKey(0)
	if err != nil {
		return "", err
	}
	addressKey, err := changeKey.NewChildKey(0)
	if err != nil {
		return "", err
	}
	privateKeyBytes := addressKey.Key
	return hex.EncodeToString(privateKeyBytes), nil
}

func HexToECDSA(hexkey string) (*ecdsa.PrivateKey, error) {
	privateKeyBytes, err := hex.DecodeString(hexkey)
	if err != nil {
		return nil, err
	}
	privateKey, err := crypto.ToECDSA(privateKeyBytes)
	if err != nil {
		return nil, err
	}
	return privateKey, nil
}
