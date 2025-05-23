package usecases

import (
	"blocowallet/domain"
	"crypto/ecdsa"
	"encoding/hex"
	"fmt"
	"github.com/ethereum/go-ethereum/accounts/keystore"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/tyler-smith/go-bip32"
	"github.com/tyler-smith/go-bip39"
	"os"
	"path/filepath"
)

type WalletDetails struct {
	Wallet     *domain.Wallet
	Mnemonic   string
	PrivateKey *ecdsa.PrivateKey
	PublicKey  *ecdsa.PublicKey
}

type WalletService struct {
	Repo     domain.WalletRepository
	KeyStore *keystore.KeyStore
}

func NewWalletService(repo domain.WalletRepository, ks *keystore.KeyStore) *WalletService {
	return &WalletService{
		Repo:     repo,
		KeyStore: ks,
	}
}

func (ws *WalletService) CreateWallet(password string) (*WalletDetails, error) {
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

	wallet := &domain.Wallet{
		Address:      account.Address.Hex(),
		KeyStorePath: newPath,
		Mnemonic:     mnemonic, // Store the mnemonic
	}

	err = ws.Repo.AddWallet(wallet)
	if err != nil {
		return nil, err
	}

	walletDetails := &WalletDetails{
		Wallet:     wallet,
		Mnemonic:   mnemonic,
		PrivateKey: privKey,
		PublicKey:  &privKey.PublicKey,
	}

	return walletDetails, nil
}

func (ws *WalletService) ImportWallet(mnemonic, password string) (*WalletDetails, error) {
	if !bip39.IsMnemonicValid(mnemonic) {
		return nil, fmt.Errorf("invalid mnemonic phrase")
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

	wallet := &domain.Wallet{
		Address:      account.Address.Hex(),
		KeyStorePath: newPath,
		Mnemonic:     mnemonic, // Store the mnemonic
	}

	err = ws.Repo.AddWallet(wallet)
	if err != nil {
		return nil, err
	}

	walletDetails := &WalletDetails{
		Wallet:     wallet,
		Mnemonic:   mnemonic,
		PrivateKey: privKey,
		PublicKey:  &privKey.PublicKey,
	}

	return walletDetails, nil
}

func (ws *WalletService) ImportWalletFromPrivateKey(privateKeyHex, password string) (*WalletDetails, error) {
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

	// Generate a mnemonic from private key
	privateKeyBytes, _ := hex.DecodeString(privateKeyHex)
	entropy := crypto.Keccak256(privateKeyBytes)[:16] // Use first 16 bytes for BIP39 entropy
	mnemonic, err := bip39.NewMnemonic(entropy)
	if err != nil {
		return nil, fmt.Errorf("error generating mnemonic: %v", err)
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

	// Create the wallet entry with the generated mnemonic
	wallet := &domain.Wallet{
		Address:      account.Address.Hex(),
		KeyStorePath: newPath,
		Mnemonic:     mnemonic, // Store the generated mnemonic
	}

	// Add wallet to repository
	err = ws.Repo.AddWallet(wallet)
	if err != nil {
		return nil, err
	}

	// Return wallet details with the generated mnemonic
	walletDetails := &WalletDetails{
		Wallet:     wallet,
		Mnemonic:   mnemonic,
		PrivateKey: privKey,
		PublicKey:  &privKey.PublicKey,
	}

	return walletDetails, nil
}

func (ws *WalletService) LoadWallet(wallet *domain.Wallet, password string) (*WalletDetails, error) {
	keyJSON, err := os.ReadFile(wallet.KeyStorePath)
	if err != nil {
		return nil, fmt.Errorf("error reading the wallet file: %v", err)
	}
	key, err := keystore.DecryptKey(keyJSON, password)
	if err != nil {
		return nil, fmt.Errorf("incorrect password")
	}

	walletDetails := &WalletDetails{
		Wallet:     wallet,
		Mnemonic:   wallet.Mnemonic,
		PrivateKey: key.PrivateKey,
		PublicKey:  &key.PrivateKey.PublicKey,
	}
	return walletDetails, nil
}

func (ws *WalletService) GetAllWallets() ([]domain.Wallet, error) {
	return ws.Repo.GetAllWallets()
}

func (ws *WalletService) DeleteWallet(wallet *domain.Wallet) error {
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
