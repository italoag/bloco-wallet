package signer

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"

	"blocowallet/internal/wallet"

	gethaccounts "github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// Ledger Ethereum app APDU constants (LedgerHQ/app-ethereum).
const (
	ledgerCLA = 0xE0
	// INS values of the Ethereum application.
	ledgerINSGetPublicKey        = 0x02
	ledgerINSSign                = 0x04
	ledgerINSGetAppConfiguration = 0x06
	ledgerINSSignPersonalMessage = 0x08
	ledgerINSSignEIP712          = 0x0c
	// Status words.
	ledgerSWOK       = 0x9000
	ledgerSWDeny     = 0x6985
	ledgerSWCanceled = 0x6982
)

var (
	// ErrLedgerDenied is returned when the user rejects on the device.
	ErrLedgerDenied = errors.New("ledger signer: request denied on device")
	// ErrLedgerTransport is returned for protocol-level failures.
	ErrLedgerTransport = errors.New("ledger signer: transport failure")
	// ErrLedgerSignature is returned when the device signs with another key.
	ErrLedgerSignature = errors.New("ledger signer: signature verification failed")
	// ErrLedgerInsecureApp rejects Ethereum app versions before 1.22.3.
	ErrLedgerInsecureApp = errors.New("ledger signer: Ethereum app version is below the secure baseline")
)

// APDUTransport exchanges raw APDUs with the device (HID, TCP proxy, or
// Speculos HTTP API).
type APDUTransport interface {
	// Exchange sends one APDU and returns the response data and status word.
	Exchange(ctx context.Context, cla, ins, p1, p2 byte, data []byte) ([]byte, uint16, error)
}

// LedgerDevice speaks the Ledger Ethereum application protocol.
type LedgerDevice struct {
	transport APDUTransport
	mu        sync.Mutex
}

// NewLedgerDevice creates a device over the given transport.
func NewLedgerDevice(transport APDUTransport) (*LedgerDevice, error) {
	if transport == nil {
		return nil, fmt.Errorf("ledger signer: transport required")
	}
	return &LedgerDevice{transport: transport}, nil
}

// LedgerAppConfiguration is returned by Ethereum INS 0x06.
type LedgerAppConfiguration struct {
	Flags byte
	Major byte
	Minor byte
	Patch byte
}

// Secure reports whether the app includes the 1.22.3 APDU review hardening.
func (configuration LedgerAppConfiguration) Secure() bool {
	if configuration.Major != 1 {
		return configuration.Major > 1
	}
	if configuration.Minor != 22 {
		return configuration.Minor > 22
	}
	return configuration.Patch >= 3
}

// GetAppConfiguration reads and validates the exact four-byte app response.
func (device *LedgerDevice) GetAppConfiguration(ctx context.Context) (LedgerAppConfiguration, error) {
	if device == nil || device.transport == nil {
		return LedgerAppConfiguration{}, ErrLedgerTransport
	}
	device.mu.Lock()
	defer device.mu.Unlock()
	response, status, err := device.transport.Exchange(ctx, ledgerCLA, ledgerINSGetAppConfiguration, 0, 0, nil)
	if err != nil {
		return LedgerAppConfiguration{}, fmt.Errorf("ledger signer: app configuration: %w", err)
	}
	if status != ledgerSWOK {
		return LedgerAppConfiguration{}, mapLedgerStatus(status)
	}
	if len(response) != 4 {
		return LedgerAppConfiguration{}, ErrLedgerTransport
	}
	return LedgerAppConfiguration{Flags: response[0], Major: response[1], Minor: response[2], Patch: response[3]}, nil
}

// PublicKey is the result of GetPublicKey.
type PublicKey struct {
	Address   common.Address
	PublicKey []byte
}

// GetPublicKey requests the address and public key for a derivation path.
func (device *LedgerDevice) GetPublicKey(ctx context.Context, derivationPath string) (PublicKey, error) {
	if device == nil || device.transport == nil {
		return PublicKey{}, ErrLedgerTransport
	}
	device.mu.Lock()
	defer device.mu.Unlock()
	path, err := derivationPathToNumbers(derivationPath)
	if err != nil {
		return PublicKey{}, err
	}
	request := make([]byte, 0, 1+4*len(path))
	request = append(request, byte(len(path)))
	for _, component := range path {
		var encoded [4]byte
		binary.BigEndian.PutUint32(encoded[:], component)
		request = append(request, encoded[:]...)
	}
	response, status, err := device.transport.Exchange(ctx, ledgerCLA, ledgerINSGetPublicKey, 0x00, 0x00, request)
	if err != nil {
		return PublicKey{}, fmt.Errorf("ledger signer: get public key: %w", err)
	}
	if status != ledgerSWOK {
		return PublicKey{}, mapLedgerStatus(status)
	}
	// Response: public key length (1) || uncompressed pubkey (65) ||
	// address length (1) || address ASCII hex (40 bytes).
	if len(response) != 1+65+1+40 || response[0] != 65 || response[1] != 0x04 {
		return PublicKey{}, ErrLedgerTransport
	}
	pubKey := append([]byte(nil), response[1:66]...)
	addressLength := int(response[66])
	if addressLength != 40 {
		return PublicKey{}, ErrLedgerTransport
	}
	addressBytes, err := hex.DecodeString(string(response[67 : 67+addressLength]))
	if err != nil || len(addressBytes) != common.AddressLength {
		return PublicKey{}, ErrLedgerTransport
	}
	parsedPublicKey, err := crypto.UnmarshalPubkey(pubKey)
	if err != nil {
		return PublicKey{}, ErrLedgerTransport
	}
	address := common.BytesToAddress(addressBytes)
	if crypto.PubkeyToAddress(*parsedPublicKey) != address {
		return PublicKey{}, ErrLedgerTransport
	}
	return PublicKey{Address: address, PublicKey: pubKey}, nil
}

// SignTypedMessage signs an EIP-712 typed data hash (INS 0x0C, P2=0 v0).
// The device requests confirmation on its screen.
func (device *LedgerDevice) SignTypedMessage(ctx context.Context, derivationPath string, domainSeparatorHash, messageHash [32]byte) ([]byte, error) {
	if device == nil || device.transport == nil {
		return nil, ErrLedgerTransport
	}
	device.mu.Lock()
	defer device.mu.Unlock()
	path, err := derivationPathToNumbers(derivationPath)
	if err != nil {
		return nil, err
	}
	request := make([]byte, 0, 1+4*len(path)+64)
	request = append(request, byte(len(path)))
	for _, component := range path {
		var encoded [4]byte
		binary.BigEndian.PutUint32(encoded[:], component)
		request = append(request, encoded[:]...)
	}
	request = append(request, domainSeparatorHash[:]...)
	request = append(request, messageHash[:]...)
	response, status, err := device.transport.Exchange(ctx, ledgerCLA, ledgerINSSignEIP712, 0x00, 0x00, request)
	if err != nil {
		return nil, fmt.Errorf("ledger signer: sign typed message: %w", err)
	}
	if status != ledgerSWOK {
		return nil, mapLedgerStatus(status)
	}
	return decodeLedgerSignature(response)
}

// SignPersonalMessage signs a raw message with the EIP-191 personal scheme
// (INS_SIGN_PERSONAL_MESSAGE); the device applies the prefix itself.
func (device *LedgerDevice) SignPersonalMessage(ctx context.Context, derivationPath string, message []byte) ([]byte, error) {
	if device == nil || device.transport == nil {
		return nil, ErrLedgerTransport
	}
	device.mu.Lock()
	defer device.mu.Unlock()
	path, err := derivationPathToNumbers(derivationPath)
	if err != nil {
		return nil, err
	}
	if len(message) == 0 || len(message) > 65535 {
		return nil, fmt.Errorf("ledger signer: message size")
	}
	header := make([]byte, 0, 1+4*len(path)+4)
	header = append(header, byte(len(path)))
	for _, component := range path {
		var encoded [4]byte
		binary.BigEndian.PutUint32(encoded[:], component)
		header = append(header, encoded[:]...)
	}
	var messageLength [4]byte
	binary.BigEndian.PutUint32(messageLength[:], uint32(len(message)))
	header = append(header, messageLength[:]...)
	firstChunkSize := 255 - len(header)
	if firstChunkSize > len(message) {
		firstChunkSize = len(message)
	}
	firstRequest := append(append([]byte(nil), header...), message[:firstChunkSize]...)
	response, status, err := device.transport.Exchange(ctx, ledgerCLA, ledgerINSSignPersonalMessage, 0x00, 0x00, firstRequest)
	if err != nil {
		return nil, fmt.Errorf("ledger signer: sign personal message: %w", err)
	}
	if status != ledgerSWOK {
		return nil, mapLedgerStatus(status)
	}
	for offset := firstChunkSize; offset < len(message); {
		end := offset + 255
		if end > len(message) {
			end = len(message)
		}
		response, status, err = device.transport.Exchange(ctx, ledgerCLA, ledgerINSSignPersonalMessage, 0x80, 0x00, message[offset:end])
		if err != nil {
			return nil, fmt.Errorf("ledger signer: sign personal message continuation: %w", err)
		}
		if status != ledgerSWOK {
			return nil, mapLedgerStatus(status)
		}
		offset = end
	}
	return decodeLedgerSignature(response)
}

func decodeLedgerSignature(response []byte) ([]byte, error) {
	if len(response) != 65 {
		return nil, ErrLedgerTransport
	}
	v := response[0]
	switch v {
	case 27, 28:
		v -= 27
	case 0, 1:
	default:
		return nil, ErrLedgerTransport
	}
	signature := make([]byte, 65)
	copy(signature[:64], response[1:])
	signature[64] = v
	return signature, nil
}

// LedgerTypedHashRequest carries the structured EIP-712 intent required by
// the device and approval verifier.
type LedgerTypedHashRequest struct {
	AccountID           string
	ChainID             uint64
	DomainSeparatorHash [32]byte
	MessageHash         [32]byte
	IntentHash          [32]byte
	ApprovalID          string
}

// LedgerPersonalMessageRequest carries the raw EIP-191 message.
type LedgerPersonalMessageRequest struct {
	AccountID  string
	ChainID    uint64
	Message    []byte
	IntentHash [32]byte
	ApprovalID string
}

// LedgerSigner applies wallet approvals and verifies device signatures.
type LedgerSigner struct {
	device                  *LedgerDevice
	accounts                AccountLookup
	messageApprovalVerifier wallet.MessageApprovalVerifier
}

// NewLedgerSigner creates an approved Ledger message signer.
func NewLedgerSigner(device *LedgerDevice, accounts AccountLookup, verifier wallet.MessageApprovalVerifier) (*LedgerSigner, error) {
	if device == nil || accounts == nil || verifier == nil {
		return nil, fmt.Errorf("ledger signer: device, accounts, and verifier are required")
	}
	return &LedgerSigner{device: device, accounts: accounts, messageApprovalVerifier: verifier}, nil
}

// Sign rejects opaque digests because Ledger requires a structured intent.
func (signer *LedgerSigner) Sign(context.Context, wallet.CapabilityHandle, wallet.SoftwareSigningRequest) (wallet.SoftwareSigningResult, error) {
	return wallet.SoftwareSigningResult{}, ErrHardwareIntentRequired
}

// SignTypedHash signs an approved EIP-712 domain/message hash pair.
func (signer *LedgerSigner) SignTypedHash(ctx context.Context, request LedgerTypedHashRequest) (wallet.SoftwareSigningResult, error) {
	if request.ChainID == 0 || request.ApprovalID == "" || request.IntentHash == ([32]byte{}) {
		return wallet.SoftwareSigningResult{}, fmt.Errorf("ledger signer: incomplete typed-data binding")
	}
	account, derivationPath, expectedAddress, err := signer.resolveAccount(ctx, request.AccountID)
	if err != nil {
		return wallet.SoftwareSigningResult{}, err
	}
	digestHash := crypto.Keccak256Hash([]byte{0x19, 0x01}, request.DomainSeparatorHash[:], request.MessageHash[:])
	var digest [32]byte
	copy(digest[:], digestHash[:])
	if err := signer.messageApprovalVerifier.VerifyMessageApproval(ctx, wallet.MessageApprovalBinding{
		AccountID: account.AccountID, Scheme: wallet.MessageSigningEIP712, ChainID: request.ChainID,
		Digest: digest, IntentHash: request.IntentHash, ApprovalID: request.ApprovalID,
	}); err != nil {
		return wallet.SoftwareSigningResult{}, err
	}
	if err := signer.ensureSecureApp(ctx); err != nil {
		return wallet.SoftwareSigningResult{}, err
	}
	signature, err := signer.device.SignTypedMessage(ctx, derivationPath, request.DomainSeparatorHash, request.MessageHash)
	if err != nil {
		return wallet.SoftwareSigningResult{}, err
	}
	if err := verifyECDSASignature(expectedAddress, digest, signature); err != nil {
		return wallet.SoftwareSigningResult{}, ErrLedgerSignature
	}
	return wallet.SoftwareSigningResult{
		AccountID: account.AccountID, Purpose: wallet.SigningPurposeMessage,
		MessageScheme: wallet.MessageSigningEIP712, ChainID: request.ChainID,
		Digest: digest, IntentHash: request.IntentHash, Signature: signature,
	}, nil
}

// SignPersonalMessage signs an approved raw EIP-191 message.
func (signer *LedgerSigner) SignPersonalMessage(ctx context.Context, request LedgerPersonalMessageRequest) (wallet.SoftwareSigningResult, error) {
	if len(request.Message) == 0 || len(request.Message) > 64<<10 {
		return wallet.SoftwareSigningResult{}, fmt.Errorf("ledger signer: message size")
	}
	if request.ChainID != 0 || request.ApprovalID == "" || request.IntentHash == ([32]byte{}) {
		return wallet.SoftwareSigningResult{}, fmt.Errorf("ledger signer: incomplete personal-sign binding")
	}
	account, derivationPath, expectedAddress, err := signer.resolveAccount(ctx, request.AccountID)
	if err != nil {
		return wallet.SoftwareSigningResult{}, err
	}
	var digest [32]byte
	copy(digest[:], gethaccounts.TextHash(request.Message))
	if err := signer.messageApprovalVerifier.VerifyMessageApproval(ctx, wallet.MessageApprovalBinding{
		AccountID: account.AccountID, Scheme: wallet.MessageSigningEIP191Personal, ChainID: request.ChainID,
		Digest: digest, IntentHash: request.IntentHash, ApprovalID: request.ApprovalID,
	}); err != nil {
		return wallet.SoftwareSigningResult{}, err
	}
	if err := signer.ensureSecureApp(ctx); err != nil {
		return wallet.SoftwareSigningResult{}, err
	}
	signature, err := signer.device.SignPersonalMessage(ctx, derivationPath, request.Message)
	if err != nil {
		return wallet.SoftwareSigningResult{}, err
	}
	if err := verifyECDSASignature(expectedAddress, digest, signature); err != nil {
		return wallet.SoftwareSigningResult{}, ErrLedgerSignature
	}
	return wallet.SoftwareSigningResult{
		AccountID: account.AccountID, Purpose: wallet.SigningPurposeMessage,
		MessageScheme: wallet.MessageSigningEIP191Personal, ChainID: request.ChainID,
		Digest: digest, IntentHash: request.IntentHash, Signature: signature,
	}, nil
}

func (signer *LedgerSigner) ensureSecureApp(ctx context.Context) error {
	configuration, err := signer.device.GetAppConfiguration(ctx)
	if err != nil {
		return err
	}
	if !configuration.Secure() {
		return ErrLedgerInsecureApp
	}
	return nil
}

func (signer *LedgerSigner) resolveAccount(ctx context.Context, accountID string) (*wallet.Account, string, common.Address, error) {
	if signer == nil {
		return nil, "", common.Address{}, fmt.Errorf("ledger signer: nil signer")
	}
	account, err := signer.accounts.GetAccount(ctx, accountID)
	if err != nil {
		return nil, "", common.Address{}, fmt.Errorf("ledger signer: account: %w", err)
	}
	if account == nil || account.AccountID != accountID || account.State != wallet.AccountStateActive {
		return nil, "", common.Address{}, fmt.Errorf("ledger signer: account binding mismatch or inactive state")
	}
	if account.SignerKind != wallet.SignerKindHardware {
		return nil, "", common.Address{}, fmt.Errorf("ledger signer: account is not hardware-backed")
	}
	const prefix = "ledger:v1:"
	if !strings.HasPrefix(account.SignerReference, prefix) {
		return nil, "", common.Address{}, fmt.Errorf("ledger signer: invalid signer reference")
	}
	derivationPath := strings.TrimPrefix(account.SignerReference, prefix)
	if _, err := wallet.ParseDerivationPath(derivationPath); err != nil {
		return nil, "", common.Address{}, fmt.Errorf("ledger signer: invalid derivation path: %w", err)
	}
	expectedAddress := common.HexToAddress(account.Address)
	if expectedAddress == (common.Address{}) {
		return nil, "", common.Address{}, fmt.Errorf("ledger signer: invalid account address")
	}
	return account, derivationPath, expectedAddress, nil
}

func mapLedgerStatus(status uint16) error {
	if status == ledgerSWDeny || status == ledgerSWCanceled {
		return ErrLedgerDenied
	}
	return fmt.Errorf("%w: status %#x", ErrLedgerTransport, status)
}
