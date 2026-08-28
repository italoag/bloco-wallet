package wallet

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/keystore"
	"github.com/ethereum/go-ethereum/crypto"
)

func TestWalletVaultImportsWatchOnlyWithoutCustodyMaterial(t *testing.T) {
	vault, repository, _ := newTestVault(t)
	preview, err := PreviewWatchOnlyImport(WatchOnlyImportRequest{Address: "f39fd6e51aad88f6f4ce6ab8827279cfffb92266"})
	if err != nil {
		t.Fatal(err)
	}
	if preview.Address != "0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266" || preview.SignerKind != SignerKindWatchOnly || preview.SourceFormat != "watch_only_address" {
		t.Fatalf("unexpected watch-only preview: %+v", preview)
	}
	summary, err := vault.ImportWatchOnly(context.Background(), WatchOnlyImportRequest{Name: "Observer", Address: preview.Address})
	if err != nil {
		t.Fatal(err)
	}
	if summary.Address != preview.Address || summary.SignerKind != SignerKindWatchOnly || summary.Capabilities != 0 || summary.State != AccountStateActive {
		t.Fatalf("unexpected watch-only summary: %+v", summary)
	}
	stored, err := repository.GetAccount(context.Background(), summary.AccountID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.SecretType != "" || len(stored.SecretEnvelope) != 0 || stored.DerivationScheme != "" || stored.DerivationPath != "" || stored.BIP39Language != "" || stored.HasBIP39Passphrase {
		t.Fatalf("watch-only account persisted custody material: %+v", stored)
	}
	if _, err := vault.Unlock(context.Background(), summary.AccountID, []byte("irrelevant")); !errors.Is(err, ErrCapabilityDenied) {
		t.Fatalf("watch-only account unlock returned %v", err)
	}
	if err := vault.RotatePassword(context.Background(), summary.AccountID, []byte("Old watch-only password 1!"), []byte("New watch-only password 2!")); !errors.Is(err, ErrCapabilityDenied) {
		t.Fatalf("watch-only password rotation returned %v", err)
	}
	if err := vault.LockAccount(context.Background(), summary.AccountID); !errors.Is(err, ErrCapabilityDenied) {
		t.Fatalf("watch-only account lock returned %v", err)
	}
	if _, err := vault.ImportWatchOnly(context.Background(), WatchOnlyImportRequest{Name: "Duplicate", Address: strings.ToLower(preview.Address)}); !errors.Is(err, ErrAccountConflict) {
		t.Fatalf("duplicate watch-only import returned %v", err)
	}
	restarted, err := NewWalletVault(repository, vault.codec, vault.options)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	accounts, err := restarted.ListAccounts(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(accounts) != 1 || accounts[0].AccountID != summary.AccountID || accounts[0].SignerKind != SignerKindWatchOnly {
		t.Fatalf("watch-only account did not survive restart: %+v", accounts)
	}
}

func TestWatchOnlyImportValidatesAddressAndLinksSameAddressAccounts(t *testing.T) {
	for _, value := range []string{"", " 0x0000000000000000000000000000000000000001", "0x0000000000000000000000000000000000000000", "0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92267", "0x1234"} {
		if _, err := PreviewWatchOnlyImport(WatchOnlyImportRequest{Address: value}); err == nil {
			t.Fatalf("invalid watch-only address was accepted: %q", value)
		}
	}
	vault, _, _ := newTestVault(t)
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	privateKey := hex.EncodeToString(crypto.FromECDSA(key))
	address := crypto.PubkeyToAddress(key.PublicKey).Hex()
	key.D.SetInt64(0)
	password := []byte("Strong storage password 1!")
	software, err := vault.ImportPrivateKey(context.Background(), PrivateKeyImportRequest{
		Name:                   "Custodial",
		PrivateKey:             privateKey,
		StoragePassword:        password,
		ConfirmStoragePassword: append([]byte(nil), password...),
	})
	if err != nil {
		t.Fatal(err)
	}
	observed, err := vault.ImportWatchOnly(context.Background(), WatchOnlyImportRequest{Name: "Observed alias", Address: address})
	if err != nil {
		t.Fatal(err)
	}
	if observed.RelatedAccountID != software.AccountID {
		t.Fatalf("same-address watch-only account was not linked: %+v", observed)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := vault.ImportWatchOnly(cancelled, WatchOnlyImportRequest{Name: "Cancelled", Address: "0x0000000000000000000000000000000000000001"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled watch-only import returned %v", err)
	}
}

func TestCanonicalImportValidationAndFailurePaths(t *testing.T) {
	if _, err := PreviewMnemonicImport(MnemonicImportRequest{Mnemonic: "invalid"}); err == nil {
		t.Fatal("invalid mnemonic preview was accepted")
	}
	if _, err := PreviewMnemonicImport(MnemonicImportRequest{
		Mnemonic:       "test test test test test test test test test test test junk",
		BIP39Language:  BIP39English,
		DerivationPath: "invalid",
	}); err == nil {
		t.Fatal("invalid mnemonic path was accepted")
	}
	for _, value := range []string{"", " 01", "zz", strings.Repeat("0", 64)} {
		if _, err := PreviewPrivateKeyImport(PrivateKeyImportRequest{PrivateKey: value}); err == nil {
			t.Fatalf("invalid private key preview was accepted: %q", value)
		}
	}
	if _, err := PreviewKeystoreImport(nil, nil); err == nil {
		t.Fatal("empty keystore preview was accepted")
	}
	if _, err := metadataForCanonicalSecret("account", "0x0000000000000000000000000000000000000001", canonicalSecretV1{
		Kind:           SecretTypeMnemonic,
		DerivationPath: "invalid",
	}, 1); err == nil {
		t.Fatal("invalid canonical metadata path was accepted")
	}

	codec, err := NewSecretEnvelopeCodec(testEnvelopePolicy())
	if err != nil {
		t.Fatal(err)
	}
	identityKey := bytes.Repeat([]byte{0x42}, 32)
	password := []byte("Strong storage password 1!")
	request := MnemonicImportRequest{
		Name:                   "Failure path",
		Mnemonic:               "test test test test test test test test test test test junk",
		StoragePassword:        password,
		ConfirmStoragePassword: append([]byte(nil), password...),
	}
	closed, err := NewWalletVault(newMemoryAccountRepository(), codec, VaultOptions{SourceIdentityKey: identityKey})
	if err != nil {
		t.Fatal(err)
	}
	closed.Close()
	if _, err := closed.ImportMnemonic(context.Background(), request); !errors.Is(err, ErrVaultClosed) {
		t.Fatalf("closed vault import returned %v", err)
	}
	cancelledContext, cancel := context.WithCancel(context.Background())
	cancel()
	vault, err := NewWalletVault(newMemoryAccountRepository(), codec, VaultOptions{SourceIdentityKey: identityKey})
	if err != nil {
		t.Fatal(err)
	}
	invalidMnemonicRequest := request
	invalidMnemonicRequest.Mnemonic = "invalid"
	if _, err := vault.ImportMnemonic(context.Background(), invalidMnemonicRequest); err == nil {
		t.Fatal("invalid mnemonic import was accepted")
	}
	if _, err := vault.ImportMnemonic(cancelledContext, request); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled import returned %v", err)
	}
	emptyName := request
	emptyName.Name = " "
	if _, err := vault.ImportMnemonic(context.Background(), emptyName); err == nil {
		t.Fatal("empty import name was accepted")
	}
	shortPassword := request
	shortPassword.StoragePassword = []byte("short")
	shortPassword.ConfirmStoragePassword = []byte("short")
	if _, err := vault.ImportMnemonic(context.Background(), shortPassword); err == nil {
		t.Fatal("short storage password was accepted")
	}
	randomFault := errors.New("entropy fault")
	randomVault, err := NewWalletVault(newMemoryAccountRepository(), codec, VaultOptions{
		SourceIdentityKey: identityKey,
		Random:            errorReader{err: randomFault},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := randomVault.ImportMnemonic(context.Background(), request); !errors.Is(err, randomFault) {
		t.Fatalf("import entropy error was lost: %v", err)
	}
	sealFault := errors.New("seal fault")
	sealVault, err := NewWalletVault(newMemoryAccountRepository(), &faultEnvelope{base: codec, sealErr: sealFault}, VaultOptions{SourceIdentityKey: identityKey})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sealVault.ImportMnemonic(context.Background(), request); !errors.Is(err, sealFault) {
		t.Fatalf("import seal error was lost: %v", err)
	}
	openRepository := newMemoryAccountRepository()
	openVault, err := NewWalletVault(openRepository, &faultEnvelope{base: codec, openErr: sealFault}, VaultOptions{SourceIdentityKey: identityKey})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := openVault.ImportMnemonic(context.Background(), request); !errors.Is(err, sealFault) {
		t.Fatalf("import verification error was lost: %v", err)
	}
	if accounts, _ := openRepository.ListAccounts(context.Background()); len(accounts) != 0 {
		t.Fatal("failed import verification committed account")
	}
	repositoryFault := errors.New("repository fault")
	for _, repository := range []*faultAccountRepository{
		{base: newMemoryAccountRepository(), txErr: repositoryFault},
		{base: newMemoryAccountRepository(), createErr: repositoryFault},
	} {
		faultVault, err := NewWalletVault(repository, codec, VaultOptions{SourceIdentityKey: identityKey})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := faultVault.ImportMnemonic(context.Background(), request); !errors.Is(err, repositoryFault) {
			t.Fatalf("repository import error was lost: %v", err)
		}
	}
}

func TestCanonicalImportRequiresStableIdentityKey(t *testing.T) {
	codec, err := NewSecretEnvelopeCodec(testEnvelopePolicy())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewWalletVault(newMemoryAccountRepository(), codec, VaultOptions{SourceIdentityKey: []byte("short")}); err == nil {
		t.Fatal("short source identity key was accepted")
	}
	repository := newMemoryAccountRepository()
	bound, err := NewWalletVault(repository, codec, VaultOptions{SourceIdentityKey: bytes.Repeat([]byte{0x42}, 32)})
	if err != nil {
		t.Fatal(err)
	}
	defer bound.Close()
	if _, err := NewWalletVault(repository, codec, VaultOptions{SourceIdentityKey: bytes.Repeat([]byte{0x43}, 32)}); err == nil {
		t.Fatal("mismatched source identity key was accepted")
	}
	vault, err := NewWalletVault(newMemoryAccountRepository(), codec, VaultOptions{})
	if err != nil {
		t.Fatal(err)
	}
	password := []byte("Strong storage password 1!")
	if _, err := vault.ImportMnemonic(context.Background(), MnemonicImportRequest{
		Name:                   "No identity key",
		Mnemonic:               "test test test test test test test test test test test junk",
		StoragePassword:        password,
		ConfirmStoragePassword: append([]byte(nil), password...),
	}); !errors.Is(err, ErrSourceIdentityKeyUnavailable) {
		t.Fatalf("import without identity key returned %v", err)
	}
}

func TestCreatedAndImportedIdentitiesConflictOrRelate(t *testing.T) {
	codec, err := NewSecretEnvelopeCodec(testEnvelopePolicy())
	if err != nil {
		t.Fatal(err)
	}
	repository := newMemoryAccountRepository()
	mnemonic := "test test test test test test test test test test test junk"
	vault, err := NewWalletVault(repository, codec, VaultOptions{
		SourceIdentityKey: bytes.Repeat([]byte{0x42}, 32),
		MnemonicGenerator: func() (string, error) { return mnemonic, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	password := []byte("Strong storage password 1!")
	created, challenge, err := vault.Create(context.Background(), CreateAccountRequest{Name: "Created", Password: password})
	if err != nil {
		t.Fatal(err)
	}
	answers := make(map[int]string, len(challenge.RequiredWordIndices))
	for _, index := range challenge.RequiredWordIndices {
		answers[index] = challenge.Words[index]
	}
	if _, err := vault.ConfirmBackup(context.Background(), challenge.ChallengeID, answers); err != nil {
		t.Fatal(err)
	}
	if _, err := vault.ImportMnemonic(context.Background(), MnemonicImportRequest{
		Name:                   "Duplicate mnemonic",
		Mnemonic:               mnemonic,
		StoragePassword:        password,
		ConfirmStoragePassword: append([]byte(nil), password...),
	}); !errors.Is(err, ErrAccountConflict) {
		t.Fatalf("created mnemonic was reimported: %v", err)
	}
	path, _ := ParseDerivationPath("m/44'/60'/0'/0/0")
	privateKey, _, err := deriveEVMAccount(mnemonic, "", BIP39English, path)
	if err != nil {
		t.Fatal(err)
	}
	privateKeyHex := hex.EncodeToString(privateKey)
	clear(privateKey)
	related, err := vault.ImportPrivateKey(context.Background(), PrivateKeyImportRequest{
		Name:                   "Related raw key",
		PrivateKey:             privateKeyHex,
		StoragePassword:        password,
		ConfirmStoragePassword: append([]byte(nil), password...),
	})
	if err != nil {
		t.Fatal(err)
	}
	if related.RelatedAccountID != created.AccountID {
		t.Fatal("same-address import was not linked to existing account")
	}
}

func TestWalletVaultImportsMnemonicCanonically(t *testing.T) {
	vault, repository, _ := newTestVault(t)
	storagePassword := []byte("Strong storage password 1!")
	request := MnemonicImportRequest{
		Name:                   "Imported mnemonic",
		Mnemonic:               "test test test test test test test test test test test junk",
		BIP39Passphrase:        "pássphrase",
		BIP39Language:          BIP39English,
		DerivationPath:         "m/44'/60'/7'/0/3",
		StoragePassword:        storagePassword,
		ConfirmStoragePassword: append([]byte(nil), storagePassword...),
	}
	preview, err := PreviewMnemonicImport(request)
	if err != nil {
		t.Fatal(err)
	}
	if !preview.HasBIP39Passphrase || preview.DerivationPath != request.DerivationPath {
		t.Fatal("mnemonic preview omitted canonical metadata")
	}
	summary, err := vault.ImportMnemonic(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Address != preview.Address || summary.State != AccountStateActive {
		t.Fatal("imported mnemonic summary differs from preview")
	}
	stored, err := repository.GetAccount(context.Background(), summary.AccountID)
	if err != nil {
		t.Fatal(err)
	}
	if !stored.HasBIP39Passphrase || stored.DerivationPath != request.DerivationPath || stored.BIP39Language != string(BIP39English) || stored.AccountIndex != 7 || stored.AddressIndex != 3 {
		t.Fatal("canonical mnemonic metadata was not persisted")
	}
	if bytes.Contains(stored.SecretEnvelope, []byte(request.Mnemonic)) || bytes.Contains(stored.SecretEnvelope, []byte(request.BIP39Passphrase)) {
		t.Fatal("mnemonic import persisted plaintext secret")
	}
	handle, err := vault.Unlock(context.Background(), summary.AccountID, storagePassword)
	if err != nil {
		t.Fatal(err)
	}
	if err := vault.Lock(handle); err != nil {
		t.Fatal(err)
	}
	if _, err := vault.ImportMnemonic(context.Background(), request); !errors.Is(err, ErrAccountConflict) {
		t.Fatalf("duplicate mnemonic import was accepted: %v", err)
	}

	restarted, err := NewWalletVault(repository, vault.codec, VaultOptions{SourceIdentityKey: bytes.Repeat([]byte{0x42}, 32)})
	if err != nil {
		t.Fatal(err)
	}
	normalizedDuplicate := request
	normalizedDuplicate.BIP39Passphrase = "pa\u0301ssphrase"
	if _, err := restarted.ImportMnemonic(context.Background(), normalizedDuplicate); !errors.Is(err, ErrAccountConflict) {
		t.Fatalf("NFKD-equivalent duplicate was accepted: %v", err)
	}
	differentPath := request
	differentPath.Name = "Second account"
	differentPath.DerivationPath = "m/44'/60'/8'/0/3"
	if _, err := restarted.ImportMnemonic(context.Background(), differentPath); err != nil {
		t.Fatalf("different derivation path was rejected: %v", err)
	}
}

func TestCanonicalImportAcceptsPrintableUnicodeStoragePassword(t *testing.T) {
	vault, _, _ := newTestVault(t)
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	privateKey := hex.EncodeToString(crypto.FromECDSA(key))
	key.D.SetInt64(0)
	password := []byte("密碼密碼密碼密碼密碼密碼密碼密碼")
	if _, err := vault.ImportPrivateKey(context.Background(), PrivateKeyImportRequest{
		Name:                   "Unicode password",
		PrivateKey:             privateKey,
		StoragePassword:        password,
		ConfirmStoragePassword: append([]byte(nil), password...),
	}); err != nil {
		t.Fatalf("printable Unicode storage password was rejected: %v", err)
	}
}

func TestWalletVaultImportsPrivateKeyAndRejectsConfirmationErrors(t *testing.T) {
	vault, repository, _ := newTestVault(t)
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	privateKey := hex.EncodeToString(crypto.FromECDSA(key))
	key.D.SetInt64(0)
	storagePassword := []byte("Strong storage password 1!")
	request := PrivateKeyImportRequest{
		Name:                   "Imported key",
		PrivateKey:             privateKey,
		StoragePassword:        storagePassword,
		ConfirmStoragePassword: []byte("Different storage password 2!"),
	}
	if _, err := vault.ImportPrivateKey(context.Background(), request); !errors.Is(err, ErrStoragePasswordConfirmation) {
		t.Fatalf("mismatched storage password was accepted: %v", err)
	}
	accounts, err := repository.ListAccounts(context.Background())
	if err != nil || len(accounts) != 0 {
		t.Fatal("failed private key import persisted an account")
	}
	request.ConfirmStoragePassword = append([]byte(nil), storagePassword...)
	preview, err := PreviewPrivateKeyImport(request)
	if err != nil {
		t.Fatal(err)
	}
	summary, err := vault.ImportPrivateKey(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Address != preview.Address || summary.State != AccountStateActive {
		t.Fatal("private key import differs from preview")
	}
	if _, err := vault.ImportPrivateKey(context.Background(), request); !errors.Is(err, ErrAccountConflict) {
		t.Fatalf("duplicate private key was accepted: %v", err)
	}
	if _, err := PreviewPrivateKeyImport(PrivateKeyImportRequest{PrivateKey: " 0x" + privateKey}); err == nil {
		t.Fatal("private key with surrounding whitespace was accepted")
	}
}

func TestKeystoreImportAllowsEmptySourcePassword(t *testing.T) {
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	address := crypto.PubkeyToAddress(key.PublicKey).Hex()
	encoded, err := keystore.EncryptKey(&keystore.Key{
		Address:    crypto.PubkeyToAddress(key.PublicKey),
		PrivateKey: key,
	}, "", keystore.LightScryptN, keystore.LightScryptP)
	key.D.SetInt64(0)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := PreviewKeystoreImport(encoded, nil)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Address != address {
		t.Fatal("empty source password changed imported address")
	}
}

func TestExternalEthersParityAndWeb3jKeystoreVectors(t *testing.T) {
	type vector struct {
		Source     string `json:"source"`
		File       string `json:"file"`
		Password   string `json:"password"`
		Address    string `json:"address"`
		PrivateKey string `json:"private_key"`
	}
	manifest, err := os.ReadFile("testdata/external-keystore-vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var vectors []vector
	if err := json.Unmarshal(manifest, &vectors); err != nil {
		t.Fatal(err)
	}
	for _, testVector := range vectors {
		t.Run(testVector.Source, func(t *testing.T) {
			fixture, err := os.ReadFile(filepath.Join("testdata", testVector.File))
			if err != nil {
				t.Fatal(err)
			}
			preview, err := PreviewKeystoreImport(fixture, []byte(testVector.Password))
			if err != nil {
				t.Fatal(err)
			}
			if testVector.Address != "" && !addressesEqual(preview.Address, testVector.Address) {
				t.Fatalf("external fixture address mismatch: %s", preview.Address)
			}
			secret, _, err := canonicalPrivateKeyFromKeystore(fixture, []byte(testVector.Password))
			if err != nil {
				t.Fatal(err)
			}
			defer clear(secret.PrivateKey)
			if hex.EncodeToString(secret.PrivateKey) != testVector.PrivateKey {
				t.Fatal("external fixture private key mismatch")
			}
		})
	}
}

func TestOfficialWeb3SecretStoragePBKDF2Vector(t *testing.T) {
	fixture, err := os.ReadFile("testdata/web3-secret-storage-pbkdf2.json")
	if err != nil {
		t.Fatal(err)
	}
	expectedSecret, err := os.ReadFile("testdata/web3-secret-storage-pbkdf2.secret")
	if err != nil {
		t.Fatal(err)
	}
	defer clear(expectedSecret)
	keystoreKDFSemaphore <- struct{}{}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := PreviewKeystoreImportContext(cancelled, fixture, []byte("testpassword")); !errors.Is(err, context.Canceled) {
		<-keystoreKDFSemaphore
		t.Fatalf("cancelled KDF wait returned %v", err)
	}
	<-keystoreKDFSemaphore
	preview, err := PreviewKeystoreImport(fixture, []byte("testpassword"))
	if err != nil {
		t.Fatal(err)
	}
	if preview.Address != "0x008AeEda4D805471dF9b2A5B0f38A0C3bCBA786b" {
		t.Fatalf("official fixture address mismatch: %s", preview.Address)
	}
	var extendedArtifact map[string]any
	if err := json.Unmarshal(fixture, &extendedArtifact); err != nil {
		t.Fatal(err)
	}
	extendedArtifact["minorversion"] = 1
	extendedArtifact["label"] = "ethers-compatible"
	extendedArtifact["x-ethers"] = map[string]any{"client": "ethers"}
	extended, err := json.Marshal(extendedArtifact)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := PreviewKeystoreImport(extended, []byte("testpassword")); err != nil {
		t.Fatalf("known Keystore extensions were rejected: %v", err)
	}
	duplicateVersion := bytes.Replace(fixture, []byte(`"version": 3`), []byte(`"version": 3, "Version": 3`), 1)
	if _, err := PreviewKeystoreImport(duplicateVersion, []byte("testpassword")); err == nil {
		t.Fatal("duplicate Keystore JSON key was accepted")
	}
	secret, _, err := canonicalPrivateKeyFromKeystore(fixture, []byte("testpassword"))
	if err != nil {
		t.Fatal(err)
	}
	defer clear(secret.PrivateKey)
	if hex.EncodeToString(secret.PrivateKey) != strings.TrimSpace(string(expectedSecret)) {
		t.Fatal("official fixture private key mismatch")
	}
	var tamperedArtifact map[string]any
	if err := json.Unmarshal(fixture, &tamperedArtifact); err != nil {
		t.Fatal(err)
	}
	tamperedArtifact["address"] = "0000000000000000000000000000000000000001"
	tamperedAddress, err := json.Marshal(tamperedArtifact)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := canonicalPrivateKeyFromKeystore(tamperedAddress, []byte("testpassword")); err == nil {
		t.Fatal("keystore with mismatched declared address was accepted")
	}
}

func TestWalletVaultImportsKeystoreWithSeparatePasswords(t *testing.T) {
	vault, repository, _ := newTestVault(t)
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	privateKeyBytes := crypto.FromECDSA(key)
	defer clear(privateKeyBytes)
	keystoreKey := &keystore.Key{
		Id:         [16]byte{1, 2, 3, 4, 5, 6, 0x47, 8, 0x89, 10, 11, 12, 13, 14, 15, 16},
		Address:    crypto.PubkeyToAddress(key.PublicKey),
		PrivateKey: key,
	}
	sourcePassword := []byte("External keystore password")
	encoded, err := keystore.EncryptKey(keystoreKey, string(sourcePassword), keystore.LightScryptN, keystore.LightScryptP)
	key.D.SetInt64(0)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := PreviewKeystoreImport(encoded, sourcePassword)
	if err != nil {
		t.Fatal(err)
	}
	storagePassword := []byte("Different storage password 1!")
	request := KeystoreImportRequest{
		Name:                   "Imported keystore",
		KeystoreJSON:           encoded,
		SourcePassword:         sourcePassword,
		StoragePassword:        storagePassword,
		ConfirmStoragePassword: append([]byte(nil), storagePassword...),
	}
	summary, err := vault.ImportKeystore(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Address != preview.Address {
		t.Fatal("keystore import differs from preview")
	}
	stored, err := repository.GetAccount(context.Background(), summary.AccountID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.SecretType != SecretTypePrivateKey || bytes.Contains(stored.SecretEnvelope, privateKeyBytes) {
		t.Fatal("keystore was not converted to canonical encrypted storage")
	}
	wrongSource := request
	wrongSource.Name = "Wrong source"
	wrongSource.SourcePassword = []byte("wrong external password")
	if _, err := vault.ImportKeystore(context.Background(), wrongSource); err == nil {
		t.Fatal("wrong source password was accepted")
	}
	if accounts, err := repository.ListAccounts(context.Background()); err != nil || len(accounts) != 1 {
		t.Fatal("failed keystore import changed account count")
	}
	if _, err := PreviewKeystoreImport(make([]byte, maxKeystoreImportSize+1), sourcePassword); err == nil {
		t.Fatal("oversized keystore was accepted")
	}
}
