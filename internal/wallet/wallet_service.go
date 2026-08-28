package wallet

import (
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/ethereum/go-ethereum/accounts/keystore"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/google/uuid"
)

type WalletDetails struct {
	Wallet       *Wallet
	Mnemonic     *string      // Nullable for non-mnemonic imports
	ImportMethod ImportMethod // Track import method
	HasMnemonic  bool         // Helper field for UI
	KDFInfo      *KDFInfo     // KDF analysis information
}

type WalletService struct {
	Repo        WalletRepository
	KeyStore    *keystore.KeyStore
	KeyStoreDir string
}

var ErrWalletDeletionDisabled = errors.New("wallet deletion is disabled until transactional deletion is available")

func NewWalletService(repo WalletRepository, ks *keystore.KeyStore, keyStoreDir ...string) *WalletService {
	// Verify that CryptoService is initialized
	if defaultCryptoService == nil {
		panic("CryptoService must be initialized before creating WalletService. Call wallet.InitCryptoService(cfg) first.")
	}

	dir := defaultCryptoService.config.WalletsDir
	if len(keyStoreDir) > 0 {
		dir = keyStoreDir[0]
	}
	if dir != "" {
		dir = filepath.Clean(dir)
	}

	return &WalletService{
		Repo:        repo,
		KeyStore:    ks,
		KeyStoreDir: dir,
	}
}

func (ws *WalletService) CreateWallet(name, password string) (*WalletDetails, error) {
	mnemonic, err := GenerateMnemonic()
	if err != nil {
		return nil, err
	}
	return ws.CreateWalletFromMnemonic(name, mnemonic, password)
}

func (ws *WalletService) CreateWalletFromMnemonic(name, mnemonic, password string) (*WalletDetails, error) {
	if _, err := DetectBIP39Language(mnemonic); err != nil {
		return nil, NewInvalidImportDataError(string(ImportMethodMnemonic), "Invalid mnemonic phrase")
	}
	sourceHash := (&SourceHashGenerator{}).GenerateFromMnemonic(mnemonic)
	if existingWallet, err := ws.Repo.FindBySourceHash(sourceHash); err != nil {
		return nil, err
	} else if existingWallet != nil {
		return nil, NewDuplicateWalletError(string(ImportMethodMnemonic), existingWallet.Address, "A wallet with this mnemonic phrase already exists")
	}

	privateKeyHex, err := derivePrivateKeyLegacy(mnemonic)
	if err != nil {
		return nil, err
	}

	privKey, err := hexToECDSALegacy(privateKeyHex)
	if err != nil {
		return nil, err
	}

	account, err := ws.KeyStore.ImportECDSA(privKey, password)
	if err != nil {
		return nil, err
	}
	keepKeyStore := false
	defer func() {
		if !keepKeyStore {
			_ = os.Remove(account.URL.Path)
		}
	}()

	// Encrypt the mnemonic before storing
	encryptedMnemonic, err := EncryptMnemonic(mnemonic, password)
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt mnemonic: %w", err)
	}

	wallet := &Wallet{
		Name:         name,
		Address:      account.Address.Hex(),
		KeyStorePath: account.URL.Path,
		Mnemonic:     &encryptedMnemonic, // Store the encrypted mnemonic
		ImportMethod: string(ImportMethodMnemonic),
		SourceHash:   sourceHash,
	}

	if err = ws.Repo.AddWallet(wallet); err != nil {
		return nil, err
	}
	keepKeyStore = true

	walletDetails := &WalletDetails{
		Wallet:       wallet,
		Mnemonic:     &mnemonic,
		ImportMethod: ImportMethodMnemonic,
		HasMnemonic:  true,
	}

	return walletDetails, nil
}

func (ws *WalletService) ImportWallet(name, mnemonic, password string) (*WalletDetails, error) {
	// 5.2 Validate mnemonic before any processing
	if _, err := DetectBIP39Language(mnemonic); err != nil {
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

	privateKeyHex, err := derivePrivateKeyLegacy(mnemonic)
	if err != nil {
		return nil, err
	}

	privKey, err := hexToECDSALegacy(privateKeyHex)
	if err != nil {
		return nil, err
	}

	account, err := ws.KeyStore.ImportECDSA(privKey, password)
	if err != nil {
		return nil, err
	}
	keepKeyStore := false
	defer func() {
		if !keepKeyStore {
			_ = os.Remove(account.URL.Path)
		}
	}()

	// Encrypt the mnemonic before storing
	encryptedMnemonic, err := EncryptMnemonic(mnemonic, password)
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt mnemonic: %w", err)
	}

	wallet := &Wallet{
		Name:         name,
		Address:      account.Address.Hex(),
		KeyStorePath: account.URL.Path,
		Mnemonic:     &encryptedMnemonic, // Store the encrypted mnemonic
		ImportMethod: string(ImportMethodMnemonic),
		SourceHash:   sourceHash,
	}

	if err = ws.Repo.AddWallet(wallet); err != nil {
		return nil, err
	}
	keepKeyStore = true

	walletDetails := &WalletDetails{
		Wallet:       wallet,
		Mnemonic:     &mnemonic,
		ImportMethod: ImportMethodMnemonic,
		HasMnemonic:  true,
	}

	return walletDetails, nil
}

func (ws *WalletService) ImportWalletFromPrivateKey(name, privateKeyHex, password string) (*WalletDetails, error) {
	// Normalize: remove 0x prefix if present
	if len(privateKeyHex) > 2 && (privateKeyHex[:2] == "0x" || privateKeyHex[:2] == "0X") {
		privateKeyHex = privateKeyHex[2:]
	}

	// 6.3 Validate private key format before processing
	if len(privateKeyHex) != 64 {
		return nil, NewInvalidImportDataError(string(ImportMethodPrivateKey), "Invalid private key format")
	}
	// ensure hex characters decode
	if _, err := hex.DecodeString(privateKeyHex); err != nil {
		return nil, NewInvalidImportDataError(string(ImportMethodPrivateKey), "Invalid private key format")
	}

	// 6.2 Duplicate detection by source hash (private key)
	hashGen := &SourceHashGenerator{}
	sourceHash := hashGen.GenerateFromPrivateKey(privateKeyHex)
	if existingWallet, err := ws.Repo.FindBySourceHash(sourceHash); err == nil && existingWallet != nil {
		return nil, NewDuplicateWalletError(string(ImportMethodPrivateKey), existingWallet.Address, "A wallet with this private key already exists")
	} else if err != nil {
		return nil, err
	}

	// Convert hex to ECDSA private key
	privKey, err := hexToECDSALegacy(privateKeyHex)
	if err != nil {
		return nil, NewInvalidImportDataError(string(ImportMethodPrivateKey), "Invalid private key format")
	}

	// Import the private key to keystore
	defer privKey.D.SetInt64(0)
	keyID, err := uuid.NewRandom()
	if err != nil {
		return nil, fmt.Errorf("generate keystore identity: %w", err)
	}
	n, p := GetTestKeystoreParams()
	address := crypto.PubkeyToAddress(privKey.PublicKey)
	encryptedKey, err := keystore.EncryptKey(&keystore.Key{Id: keyID, Address: address, PrivateKey: privKey}, password, n, p)
	if err != nil {
		return nil, fmt.Errorf("encrypt private key import: %w", err)
	}
	if err := os.MkdirAll(ws.KeyStoreDir, 0o700); err != nil {
		return nil, fmt.Errorf("prepare keystore directory: %w", err)
	}
	destination, err := os.CreateTemp(ws.KeyStoreDir, address.Hex()+"-*.json")
	if err != nil {
		return nil, fmt.Errorf("create private key keystore: %w", err)
	}
	newPath := destination.Name()
	if err := destination.Chmod(0o600); err != nil {
		_ = destination.Close()
		_ = os.Remove(newPath)
		return nil, fmt.Errorf("protect private key keystore: %w", err)
	}
	if _, err := destination.Write(encryptedKey); err != nil {
		_ = destination.Close()
		_ = os.Remove(newPath)
		return nil, fmt.Errorf("write private key keystore: %w", err)
	}
	if err := destination.Sync(); err != nil {
		_ = destination.Close()
		_ = os.Remove(newPath)
		return nil, fmt.Errorf("sync private key keystore: %w", err)
	}
	if err := destination.Close(); err != nil {
		_ = os.Remove(newPath)
		return nil, fmt.Errorf("close private key keystore: %w", err)
	}

	// Rename the keystore file to match Ethereum address
	keepKeyStore := false
	defer func() {
		if !keepKeyStore {
			_ = os.Remove(newPath)
		}
	}()

	// 6.1 Mnemonic must be unavailable for private key imports
	var nilMnemonic *string = nil

	// Create the wallet entry without mnemonic
	wallet := &Wallet{
		Name:         name,
		Address:      address.Hex(),
		KeyStorePath: newPath,
		Mnemonic:     nilMnemonic, // No mnemonic stored for private key imports
		ImportMethod: string(ImportMethodPrivateKey),
		SourceHash:   sourceHash,
	}

	// Add wallet to repository
	if err = ws.Repo.AddWallet(wallet); err != nil {
		return nil, err
	}
	keepKeyStore = true

	// Return wallet details without mnemonic
	walletDetails := &WalletDetails{
		Wallet:       wallet,
		Mnemonic:     nil,
		ImportMethod: ImportMethodPrivateKey,
		HasMnemonic:  false,
	}

	return walletDetails, nil
}

// ImportWalletFromKeystoreV3 imports a wallet from a keystore v3 file with Universal KDF support
func (ws *WalletService) ImportWalletFromKeystoreV3(name, keystorePath, password string) (*WalletDetails, error) {
	return ws.ImportWalletFromKeystoreV3WithProgress(name, keystorePath, password, nil)
}

// ImportWalletFromKeystoreV3WithProgress imports a wallet from a keystore v3 file with progress tracking
func (ws *WalletService) ImportWalletFromKeystoreV3WithProgress(name, keystorePath, password string, progressChan chan<- ImportProgress) (*WalletDetails, error) {
	return ws.ImportWalletFromKeystoreV3WithContext(context.Background(), name, keystorePath, password, progressChan)
}

func (ws *WalletService) ImportWalletFromKeystoreV3WithContext(ctx context.Context, name, keystorePath, password string, progressChan chan<- ImportProgress) (*WalletDetails, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, NewKeystoreImportError(ErrorImportInterrupted, "Import cancelled", err)
	}

	// Send initial progress update
	ws.sendProgressUpdate(progressChan, ImportProgress{
		CurrentFile:     keystorePath,
		TotalFiles:      1,
		ProcessedFiles:  0,
		Percentage:      0.0,
		Errors:          []ImportError{},
		PendingPassword: false,
		PendingFile:     "",
		StartTime:       time.Now(),
		ElapsedTime:     0,
	})

	// Step 1: Validate file existence
	ws.sendProgressUpdate(progressChan, ImportProgress{
		CurrentFile:     keystorePath,
		TotalFiles:      1,
		ProcessedFiles:  0,
		Percentage:      10.0,
		Errors:          []ImportError{},
		PendingPassword: false,
		PendingFile:     "",
		StartTime:       time.Now(),
		ElapsedTime:     0,
	})

	if err := ctx.Err(); err != nil {
		return nil, NewKeystoreImportError(ErrorImportInterrupted, "Import cancelled", err)
	}

	// Step 2: Read the keystore file
	ws.sendProgressUpdate(progressChan, ImportProgress{
		CurrentFile:     keystorePath,
		TotalFiles:      1,
		ProcessedFiles:  0,
		Percentage:      20.0,
		Errors:          []ImportError{},
		PendingPassword: false,
		PendingFile:     "",
		StartTime:       time.Now(),
		ElapsedTime:     0,
	})

	keyJSON, err := readRegularFile(keystorePath, 1024*1024)
	if err != nil {
		errorType := ErrorInvalidKeystore
		message := "Error reading the keystore file"
		if os.IsNotExist(err) {
			errorType = ErrorFileNotFound
			message = "Keystore file not found at specified path"
		}
		return nil, NewKeystoreImportError(
			errorType,
			message,
			err,
		)
	}
	if err := ctx.Err(); err != nil {
		return nil, NewKeystoreImportError(ErrorImportInterrupted, "Import cancelled", err)
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
	ws.sendProgressUpdate(progressChan, ImportProgress{
		CurrentFile:     keystorePath,
		TotalFiles:      1,
		ProcessedFiles:  0,
		Percentage:      30.0,
		Errors:          []ImportError{},
		PendingPassword: false,
		PendingFile:     "",
		StartTime:       time.Now(),
		ElapsedTime:     0,
	})

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
	ws.sendProgressUpdate(progressChan, ImportProgress{
		CurrentFile:     keystorePath,
		TotalFiles:      1,
		ProcessedFiles:  0,
		Percentage:      40.0,
		Errors:          []ImportError{},
		PendingPassword: false,
		PendingFile:     "",
		StartTime:       time.Now(),
		ElapsedTime:     0,
	})

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
	ws.sendProgressUpdate(progressChan, ImportProgress{
		CurrentFile:     keystorePath,
		TotalFiles:      1,
		ProcessedFiles:  0,
		Percentage:      50.0,
		Errors:          []ImportError{},
		PendingPassword: false,
		PendingFile:     "",
		StartTime:       time.Now(),
		ElapsedTime:     0,
	})

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
	defer clear(derivedKey)
	if err := ctx.Err(); err != nil {
		return nil, NewKeystoreImportError(ErrorImportInterrupted, "Import cancelled", err)
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
	ws.sendProgressUpdate(progressChan, ImportProgress{
		CurrentFile:     keystorePath,
		TotalFiles:      1,
		ProcessedFiles:  0,
		Percentage:      70.0,
		Errors:          []ImportError{},
		PendingPassword: false,
		PendingFile:     "",
		StartTime:       time.Now(),
		ElapsedTime:     0,
	})

	privateKeyBytes, err := enhancedService.decryptPrivateKey(derivedKey, cryptoParams)
	if err != nil {
		return nil, NewKeystoreImportError(
			ErrorCorruptedFile,
			"Failed to decrypt private key",
			err,
		)
	}
	defer clear(privateKeyBytes)

	// Step 13: Convert to ECDSA private key
	privateKey, err := crypto.ToECDSA(privateKeyBytes)
	if err != nil {
		return nil, NewKeystoreImportError(
			ErrorCorruptedFile,
			"Invalid private key format",
			err,
		)
	}
	defer privateKey.D.SetInt64(0)

	// Step 14: Verify address matches derived address
	derivedAddress := crypto.PubkeyToAddress(privateKey.PublicKey).Hex()
	normalizedKeystoreAddress := common.HexToAddress(keystoreData.Address).Hex()
	normalizedDerivedAddress := common.HexToAddress(derivedAddress).Hex()

	if keystoreData.Address != "" && normalizedKeystoreAddress != normalizedDerivedAddress {
		return nil, NewKeystoreImportError(
			ErrorAddressMismatch,
			fmt.Sprintf("Address mismatch: keystore address %s does not match derived address %s",
				normalizedKeystoreAddress, normalizedDerivedAddress),
			nil,
		)
	}

	// Step 15: No mnemonic generation for keystore imports
	// Keystore files contain only private keys, not original mnemonic phrases.
	// It's technically impossible to recover the original mnemonic from a private key.
	var nilMnemonic *string = nil

	// Step 16: Create destination path
	address := normalizedDerivedAddress
	keystoreDir := ws.KeyStoreDir
	if keystoreDir == "" {
		accounts := ws.KeyStore.Accounts()
		if len(accounts) > 0 {
			keystoreDir = filepath.Dir(accounts[0].URL.Path)
		}
	}
	if keystoreDir == "" {
		return nil, NewKeystoreImportError(
			ErrorFileNotFound,
			"Managed keystore directory is not configured",
			nil,
		)
	}
	if err := os.MkdirAll(keystoreDir, 0700); err != nil {
		return nil, NewKeystoreImportError(
			ErrorFileNotFound,
			"Error creating keystore directory",
			err,
		)
	}

	// Step 17: Copy keystore file to destination
	ws.sendProgressUpdate(progressChan, ImportProgress{
		CurrentFile:     keystorePath,
		TotalFiles:      1,
		ProcessedFiles:  0,
		Percentage:      80.0,
		Errors:          []ImportError{},
		PendingPassword: false,
		PendingFile:     "",
		StartTime:       time.Now(),
		ElapsedTime:     0,
	})

	if err := ctx.Err(); err != nil {
		return nil, NewKeystoreImportError(ErrorImportInterrupted, "Import cancelled", err)
	}
	destFile, err := os.CreateTemp(keystoreDir, address+"-*.json")
	if err != nil {
		return nil, NewKeystoreImportError(
			ErrorFileNotFound,
			"Error creating destination file",
			err,
		)
	}
	destPath := destFile.Name()
	keepDestination := false
	defer func() {
		if !keepDestination {
			_ = os.Remove(destPath)
		}
	}()

	if _, err = destFile.Write(keyJSON); err != nil {
		_ = destFile.Close()
		return nil, NewKeystoreImportError(
			ErrorFileNotFound,
			"Error writing to destination file",
			err,
		)
	}
	if err = destFile.Sync(); err != nil {
		_ = destFile.Close()
		return nil, NewKeystoreImportError(
			ErrorFileNotFound,
			"Error syncing destination file",
			err,
		)
	}
	if err = destFile.Close(); err != nil {
		// Avoid printing to terminal; write to file logger if available
		if svcLogger != nil {
			svcLogger.Warn("Error closing destination file: " + err.Error())
		}
		return nil, NewKeystoreImportError(
			ErrorFileNotFound,
			"Error closing destination file",
			err,
		)
	}
	storedJSON, err := os.ReadFile(destPath)
	if err != nil {
		return nil, NewKeystoreImportError(ErrorCorruptedFile, "Failed to verify stored keystore", err)
	}
	reloadedKey, err := decryptKeySafely(storedJSON, password)
	if err != nil {
		return nil, NewKeystoreImportError(ErrorInvalidKeystore, "Keystore is not supported for persistent use", err)
	}
	if reloadedKey.Address.Hex() != address || reloadedKey.PrivateKey.D.Cmp(privateKey.D) != 0 {
		return nil, NewKeystoreImportError(ErrorAddressMismatch, "Stored keystore identity mismatch", nil)
	}

	// Step 18: Create wallet entry with import method and source hash (no mnemonic)
	wallet := &Wallet{
		Name:         name,
		Address:      address,
		KeyStorePath: destPath,
		Mnemonic:     nilMnemonic, // No mnemonic for keystore imports
		ImportMethod: string(ImportMethodKeystore),
		SourceHash:   sourceHash,
	}

	// Step 19: Add wallet to repository
	ws.sendProgressUpdate(progressChan, ImportProgress{
		CurrentFile:     keystorePath,
		TotalFiles:      1,
		ProcessedFiles:  0,
		Percentage:      90.0,
		Errors:          []ImportError{},
		PendingPassword: false,
		PendingFile:     "",
		StartTime:       time.Now(),
		ElapsedTime:     0,
	})

	commit := func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		return ws.Repo.AddWallet(wallet)
	}
	if control := importControlFromContext(ctx); control != nil {
		err = control.commit(commit)
	} else {
		err = commit()
	}
	if err != nil {
		if ctx.Err() != nil {
			return nil, NewKeystoreImportError(ErrorImportInterrupted, "Import cancelled", ctx.Err())
		}
		return nil, NewKeystoreImportError(
			ErrorCorruptedFile,
			"Failed to add wallet to repository",
			err,
		)
	}
	keepDestination = true

	// Step 20: Create KDF information for wallet details
	kdfInfo := &KDFInfo{
		Type:           compatReport.KDFType,
		NormalizedType: compatReport.NormalizedKDF,
		SecurityLevel:  compatReport.SecurityLevel,
		Parameters:     compatReport.Parameters,
	}

	// Step 21: Send completion progress and return enhanced wallet details
	ws.sendProgressUpdate(progressChan, ImportProgress{
		CurrentFile:     keystorePath,
		TotalFiles:      1,
		ProcessedFiles:  1,
		Percentage:      100.0,
		Errors:          []ImportError{},
		PendingPassword: false,
		PendingFile:     "",
		StartTime:       time.Now(),
		ElapsedTime:     0,
	})

	walletDetails := &WalletDetails{
		Wallet:       wallet,
		Mnemonic:     nil, // No mnemonic available for keystore imports
		ImportMethod: ImportMethodKeystore,
		HasMnemonic:  false, // Keystore imports don't have mnemonics
		KDFInfo:      kdfInfo,
	}

	return walletDetails, nil
}

// sendProgressUpdate sends a progress update through the channel with timeout handling
func (ws *WalletService) sendProgressUpdate(progressChan chan<- ImportProgress, progress ImportProgress) {
	if progressChan == nil {
		return // No progress channel provided, skip update
	}

	// Use buffered send with longer timeout to ensure progress updates are delivered
	select {
	case progressChan <- progress:
		// Successfully sent progress update
	case <-time.After(500 * time.Millisecond):
		// Timeout after 500ms - this allows more time for UI to process
		// Avoid printing to terminal; write to file logger if available
		if svcLogger != nil {
			svcLogger.Warn("WalletService: progress update dropped (channel may be blocked)")
		}
	}
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
	key, err := decryptKeySafely(keyJSON, password)
	if err != nil {
		return nil, fmt.Errorf("incorrect password")
	}
	key.PrivateKey.D.SetInt64(0)

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
		ImportMethod: ImportMethod(wallet.ImportMethod),
		HasMnemonic:  wallet.Mnemonic != nil,
	}
	return walletDetails, nil
}

func (ws *WalletService) GetAllWallets() ([]Wallet, error) {
	return ws.Repo.GetAllWallets()
}

func (ws *WalletService) DeleteWallet(wallet *Wallet) error {
	return ErrWalletDeletionDisabled
}

// Helper functions

func readRegularFile(path string, maxBytes int64) (data []byte, err error) {
	linkInfo, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if linkInfo.Mode()&os.ModeSymlink != 0 || !linkInfo.Mode().IsRegular() {
		return nil, fmt.Errorf("path is not a regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxBytes {
		return nil, fmt.Errorf("file exceeds the allowed size or is not regular")
	}
	data, err = io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("file exceeds the allowed size")
	}
	return data, nil
}

var keystoreKDFSemaphore = make(chan struct{}, 1)

func decryptKeySafely(keyJSON []byte, password string) (*keystore.Key, error) {
	return decryptKeySafelyContext(context.Background(), keyJSON, password)
}

func decryptKeySafelyContext(ctx context.Context, keyJSON []byte, password string) (key *keystore.Key, err error) {
	if _, err := (&KeystoreValidator{}).ValidateKeystoreV3(keyJSON); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	select {
	case keystoreKDFSemaphore <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() { <-keystoreKDFSemaphore }()
	defer func() {
		if recovered := recover(); recovered != nil {
			key = nil
			err = fmt.Errorf("invalid keystore parameters")
		}
	}()
	key, err = keystore.DecryptKey(keyJSON, password)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		key.PrivateKey.D.SetInt64(0)
		return nil, err
	}
	return key, nil
}

func GenerateMnemonic() (string, error) {
	return generateMnemonicForLanguage(12, BIP39English)
}

func derivePrivateKeyLegacy(mnemonic string) (string, error) {
	language, err := DetectBIP39Language(mnemonic)
	if err != nil {
		return "", fmt.Errorf("invalid mnemonic phrase")
	}
	path, err := ParseDerivationPath("m/44'/60'/0'/0/0")
	if err != nil {
		return "", err
	}
	privateKey, _, err := deriveEVMAccount(mnemonic, "", language, path)
	if err != nil {
		return "", err
	}
	defer clear(privateKey)
	return hex.EncodeToString(privateKey), nil
}

func hexToECDSALegacy(hexkey string) (*ecdsa.PrivateKey, error) {
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
