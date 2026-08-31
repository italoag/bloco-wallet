package signer

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
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
	ledgerSWOK   = 0x9000
	ledgerSWDeny = 0x6985
)

var (
	// ErrLedgerDenied is returned when the user rejects on the device.
	ErrLedgerDenied = errors.New("ledger signer: request denied on device")
	// ErrLedgerTransport is returned for protocol-level failures.
	ErrLedgerTransport = errors.New("ledger signer: transport failure")
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
}

// NewLedgerDevice creates a device over the given transport.
func NewLedgerDevice(transport APDUTransport) (*LedgerDevice, error) {
	if transport == nil {
		return nil, fmt.Errorf("ledger signer: transport required")
	}
	return &LedgerDevice{transport: transport}, nil
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
	// Response: uncompressed pubkey (65) || address length (1) || address.
	if len(response) < 65+1+20 {
		return PublicKey{}, ErrLedgerTransport
	}
	pubKey := append([]byte(nil), response[:65]...)
	addressLength := int(response[65])
	if addressLength != 20 || len(response) < 66+addressLength {
		return PublicKey{}, ErrLedgerTransport
	}
	return PublicKey{
		Address:   common.BytesToAddress(response[66 : 66+addressLength]),
		PublicKey: pubKey,
	}, nil
}

// SignTypedMessage signs an EIP-712 typed data hash (INS_SIGN_MSG).
// The device requests confirmation on its screen.
func (device *LedgerDevice) SignTypedMessage(ctx context.Context, derivationPath string, domainSeparatorHash, messageHash [32]byte) ([]byte, error) {
	if device == nil || device.transport == nil {
		return nil, ErrLedgerTransport
	}
	path, err := derivationPathToNumbers(derivationPath)
	if err != nil {
		return nil, err
	}
	request := make([]byte, 0, 1+4*len(path)+1+64)
	request = append(request, byte(len(path)))
	for _, component := range path {
		var encoded [4]byte
		binary.BigEndian.PutUint32(encoded[:], component)
		request = append(request, encoded[:]...)
	}
	request = append(request, 0x00) // metamask_v4_compat = false
	request = append(request, domainSeparatorHash[:]...)
	request = append(request, messageHash[:]...)
	response, status, err := device.transport.Exchange(ctx, ledgerCLA, ledgerINSSignEIP712, 0x00, 0x00, request)
	if err != nil {
		return nil, fmt.Errorf("ledger signer: sign typed message: %w", err)
	}
	if status != ledgerSWOK {
		return nil, mapLedgerStatus(status)
	}
	if len(response) != 65 {
		return nil, ErrLedgerTransport
	}
	return append([]byte(nil), response...), nil
}

// SignPersonalMessage signs a raw message with the EIP-191 personal scheme
// (INS_SIGN_PERSONAL_MESSAGE); the device applies the prefix itself.
func (device *LedgerDevice) SignPersonalMessage(ctx context.Context, derivationPath string, message []byte) ([]byte, error) {
	if device == nil || device.transport == nil {
		return nil, ErrLedgerTransport
	}
	path, err := derivationPathToNumbers(derivationPath)
	if err != nil {
		return nil, err
	}
	if len(message) == 0 || len(message) > 65535 {
		return nil, fmt.Errorf("ledger signer: message size")
	}
	request := make([]byte, 0, 1+4*len(path)+2+len(message))
	request = append(request, byte(len(path)))
	for _, component := range path {
		var encoded [4]byte
		binary.BigEndian.PutUint32(encoded[:], component)
		request = append(request, encoded[:]...)
	}
	request = append(request, byte(len(message)>>8), byte(len(message)))
	request = append(request, message...)
	response, status, err := device.transport.Exchange(ctx, ledgerCLA, ledgerINSSignPersonalMessage, 0x00, 0x00, request)
	if err != nil {
		return nil, fmt.Errorf("ledger signer: sign personal message: %w", err)
	}
	if status != ledgerSWOK {
		return nil, mapLedgerStatus(status)
	}
	if len(response) != 65 {
		return nil, ErrLedgerTransport
	}
	return append([]byte(nil), response...), nil
}

func mapLedgerStatus(status uint16) error {
	if status == ledgerSWDeny {
		return ErrLedgerDenied
	}
	return fmt.Errorf("%w: status %#x", ErrLedgerTransport, status)
}
