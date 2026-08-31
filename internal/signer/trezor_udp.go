package signer

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"
)

// Trezor protocol message type ids (trezor-firmware common/protob).
const (
	trezorMessageInitialize                    = 0
	trezorMessageFeatures                      = 17
	trezorMessageEthereumGetPublicKey          = 201
	trezorMessageEthereumPublicKey             = 202
	trezorMessageEthereumSignTypedMessage      = 226
	trezorMessageEthereumTypedMessageSignature = 227
)

const (
	// trezorMaxDatagram bounds a single UDP bridge exchange.
	trezorMaxDatagram = 64 << 10
	// trezorBridgePort is the trezord emulator UDP port.
	trezorBridgePort = 21324
	// trezorSessionSize is the ASCII hex session id length.
	trezorSessionSize = 32
)

// ErrTrezorDeviceFailure is a firmware-reported failure.
var ErrTrezorDeviceFailure = errors.New("trezor signer: device failure")

// UDPDevice speaks the trezord emulator bridge protocol (UDP envelope +
// protobuf messages) against a running Trezor emulator. It implements the
// TrezorDevice contract with the real firmware.
type UDPDevice struct {
	address string
	conn    net.Conn
	session []byte
	timeout time.Duration
}

// NewUDPDevice connects to the trezord emulator bridge.
func NewUDPDevice(ctx context.Context, address string) (*UDPDevice, error) {
	if address == "" {
		address = fmt.Sprintf("127.0.0.1:%d", trezorBridgePort)
	}
	dialer := net.Dialer{Timeout: 5 * time.Second}
	conn, err := dialer.DialContext(ctx, "udp", address)
	if err != nil {
		return nil, fmt.Errorf("trezor signer: bridge dial: %w", err)
	}
	device := &UDPDevice{address: address, conn: conn, timeout: 20 * time.Second}
	// The first exchange establishes the session; retry for startup races.
	for attempt := 0; attempt < 3; attempt++ {
		if _, err := device.exchange(ctx, trezorMessageInitialize, trezorMessageFeatures, nil); err == nil {
			return device, nil
		}
		if attempt < 2 {
			time.Sleep(200 * time.Millisecond)
		}
	}
	_ = conn.Close()
	return nil, fmt.Errorf("trezor signer: bridge initialize failed")
}

// Initialize implements TrezorDevice.
func (device *UDPDevice) Initialize(ctx context.Context) (TrezorFeatures, error) {
	features, err := device.exchange(ctx, trezorMessageInitialize, trezorMessageFeatures, nil)
	if err != nil {
		return TrezorFeatures{}, err
	}
	return parseFeatures(features)
}

// EthereumGetPublicKey implements TrezorDevice.
func (device *UDPDevice) EthereumGetPublicKey(ctx context.Context, derivationPath string) ([]byte, error) {
	path, err := derivationPathToNumbers(derivationPath)
	if err != nil {
		return nil, err
	}
	request := encodeFields([][]byte{encodePackedVarints(path)})
	response, err := device.exchange(ctx, trezorMessageEthereumGetPublicKey, trezorMessageEthereumPublicKey, request)
	if err != nil {
		return nil, err
	}
	publicKey, err := parseEthereumPublicKey(response)
	if err != nil {
		return nil, err
	}
	return publicKey, nil
}

// EthereumSignTypedMessage implements TrezorDevice: fields are
// address_n(1), metamask_v4_compat(2), domain_separator_hash(3),
// message_hash(4).
func (device *UDPDevice) EthereumSignTypedMessage(ctx context.Context, derivationPath string, messageHash [32]byte) ([]byte, error) {
	path, err := derivationPathToNumbers(derivationPath)
	if err != nil {
		return nil, err
	}
	request := encodeFields([][]byte{
		encodePackedVarints(path),
		nil, // metamask_v4_compat absent
		nil, // domain_separator_hash absent
		messageHash[:],
	})
	response, err := device.exchange(ctx, trezorMessageEthereumSignTypedMessage, trezorMessageEthereumTypedMessageSignature, request)
	if err != nil {
		return nil, err
	}
	signature, err := parseEthereumTypedMessageSignature(response)
	if err != nil {
		return nil, err
	}
	return signature, nil
}

// Close releases the bridge socket.
func (device *UDPDevice) Close() error {
	if device == nil || device.conn == nil {
		return nil
	}
	return device.conn.Close()
}

// exchange performs one request/response round over the trezord bridge.
// Datagram: "##" session(32 hex) "##" length(4 BE) message(type 2 BE + payload).
func (device *UDPDevice) exchange(ctx context.Context, messageType, expectedResponseType int, payload []byte) ([]byte, error) {
	if device == nil || device.conn == nil {
		return nil, fmt.Errorf("trezor signer: bridge closed")
	}
	deadline := time.Now().Add(device.timeout)
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
		deadline = contextDeadline
	}
	_ = device.conn.SetDeadline(deadline)
	message := make([]byte, 0, 2+len(payload))
	message = append(message, byte(messageType>>8), byte(messageType))
	message = append(message, payload...)
	session := device.session
	if session == nil {
		session = make([]byte, trezorSessionSize) // zeros until assigned
	}
	datagram := make([]byte, 0, 40+len(message))
	datagram = append(datagram, []byte("##")...)
	datagram = append(datagram, session...)
	datagram = append(datagram, []byte("##")...)
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(message)))
	datagram = append(datagram, length[:]...)
	datagram = append(datagram, message...)
	if _, err := device.conn.Write(datagram); err != nil {
		return nil, fmt.Errorf("trezor signer: bridge write: %w", err)
	}
	buffer := make([]byte, trezorMaxDatagram)
	count, err := device.conn.Read(buffer)
	if err != nil {
		return nil, fmt.Errorf("trezor signer: bridge read: %w", err)
	}
	if count < 40 || string(buffer[:2]) != "##" || string(buffer[34:36]) != "##" {
		return nil, fmt.Errorf("trezor signer: malformed bridge response")
	}
	assignedSession := append([]byte(nil), buffer[2:34]...)
	if len(device.session) == 0 {
		device.session = assignedSession
	}
	messageLength := int(binary.BigEndian.Uint32(buffer[36:40]))
	if messageLength < 2 || messageLength > count-40 {
		return nil, fmt.Errorf("trezor signer: response length")
	}
	responseType := int(buffer[40])<<8 | int(buffer[41])
	if responseType == 3 { // Failure
		return nil, ErrTrezorDeviceFailure
	}
	if responseType != expectedResponseType {
		return nil, fmt.Errorf("trezor signer: unexpected response type %d", responseType)
	}
	return buffer[42 : 40+messageLength], nil
}

// parseFeatures decodes the Features message.
func parseFeatures(payload []byte) (TrezorFeatures, error) {
	features := TrezorFeatures{}
	var major, minor, patch uint32
	for len(payload) > 0 {
		tag, value, rest, err := decodeField(payload)
		if err != nil {
			return features, err
		}
		payload = rest
		field := tag >> 3
		wire := tag & 0x7
		switch field {
		case 7:
			features.Model = string(value)
		case 11, 14, 15:
			if wire == 0 {
				number, numberErr := decodeVarintValue(value)
				if numberErr != nil {
					return features, numberErr
				}
				switch field {
				case 11:
					features.Initialized = number != 0
				case 14:
					features.PinProtection = number != 0
				case 15:
					features.PassphraseProtection = number != 0
				}
			}
		case 2, 3, 4:
			if wire == 0 {
				number, numberErr := decodeVarintValue(value)
				if numberErr != nil {
					return features, numberErr
				}
				switch field {
				case 2:
					major = number
				case 3:
					minor = number
				case 4:
					patch = number
				}
			}
		}
	}
	features.Version = fmt.Sprintf("%d.%d.%d", major, minor, patch)
	return features, nil
}

// parseEthereumPublicKey extracts the compressed public key node.
func parseEthereumPublicKey(payload []byte) ([]byte, error) {
	for len(payload) > 0 {
		tag, value, rest, err := decodeField(payload)
		if err != nil {
			return nil, err
		}
		payload = rest
		if tag>>3 == 1 { // node (HDNodeType)
			return extractHDNodePublicKey(value)
		}
	}
	return nil, fmt.Errorf("trezor signer: public key missing")
}

// extractHDNodePublicKey walks the HDNodeType message for public_key.
func extractHDNodePublicKey(node []byte) ([]byte, error) {
	for len(node) > 0 {
		tag, value, rest, err := decodeField(node)
		if err != nil {
			return nil, err
		}
		node = rest
		if tag>>3 == 6 { // public_key
			if len(value) != 33 {
				return nil, fmt.Errorf("trezor signer: invalid public key size")
			}
			return value, nil
		}
	}
	return nil, fmt.Errorf("trezor signer: public key node missing")
}

// parseEthereumTypedMessageSignature extracts the signature bytes.
func parseEthereumTypedMessageSignature(payload []byte) ([]byte, error) {
	for len(payload) > 0 {
		tag, value, rest, err := decodeField(payload)
		if err != nil {
			return nil, err
		}
		payload = rest
		if tag>>3 == 2 && len(value) == 65 { // signature
			return value, nil
		}
	}
	return nil, fmt.Errorf("trezor signer: signature missing")
}

// decodeField reads one protobuf field (tag varint + value).
func decodeField(data []byte) (tag uint64, value, rest []byte, err error) {
	tag, consumed, ok := decodeVarint(data)
	if !ok || consumed == 0 {
		return 0, nil, nil, fmt.Errorf("trezor signer: malformed field tag")
	}
	rest = data[consumed:]
	switch tag & 0x7 {
	case 0: // varint
		_, size, valid := decodeVarint(rest)
		if !valid {
			return 0, nil, nil, fmt.Errorf("trezor signer: malformed varint")
		}
		return tag, rest[:size], rest[size:], nil
	case 2: // length-delimited
		length, size, valid := decodeVarint(rest)
		if !valid || length > uint64(len(rest)-size) {
			return 0, nil, nil, fmt.Errorf("trezor signer: malformed length")
		}
		return tag, rest[size : size+int(length)], rest[size+int(length):], nil
	default:
		return 0, nil, nil, fmt.Errorf("trezor signer: unsupported wire type")
	}
}

func decodeVarint(data []byte) (value uint64, size int, ok bool) {
	for index := 0; index < 10 && index < len(data); index++ {
		current := data[index]
		value |= uint64(current&0x7f) << (7 * index)
		if current&0x80 == 0 {
			return value, index + 1, true
		}
	}
	return 0, 0, false
}

// decodeVarintValue converts varint bytes to a uint32 value.
func decodeVarintValue(data []byte) (uint32, error) {
	value, _, ok := decodeVarint(data)
	if !ok || value > 0xffffffff {
		return 0, fmt.Errorf("trezor signer: malformed varint value")
	}
	return uint32(value), nil
}

// encodeFields emits fields 1..n in order with wire type 2; nil entries are
// skipped.
func encodeFields(fields [][]byte) []byte {
	var encoded []byte
	for index, field := range fields {
		if field == nil {
			continue
		}
		encoded = appendVarint(encoded, uint64(index+1)<<3|2)
		encoded = appendVarint(encoded, uint64(len(field)))
		encoded = append(encoded, field...)
	}
	return encoded
}

func appendVarint(buffer []byte, value uint64) []byte {
	for value >= 0x80 {
		buffer = append(buffer, byte(value)|0x80)
		value >>= 7
	}
	return append(buffer, byte(value))
}

// encodePackedVarints encodes a repeated uint32 field as packed varints.
func encodePackedVarints(values []uint32) []byte {
	var packed []byte
	for _, value := range values {
		packed = appendVarint(packed, uint64(value))
	}
	return packed
}

// derivationPathToNumbers converts "m/44'/60'/0'/0/0" to hardened numbers.
func derivationPathToNumbers(path string) ([]uint32, error) {
	if len(path) < 2 || path[:2] != "m/" {
		return nil, fmt.Errorf("trezor signer: invalid derivation path")
	}
	var numbers []uint32
	for _, segment := range strings.Split(path[2:], "/") {
		hardened := false
		if len(segment) > 0 && (segment[len(segment)-1] == '\'' || segment[len(segment)-1] == 'h') {
			hardened = true
			segment = segment[:len(segment)-1]
		}
		value, err := strconv.ParseUint(segment, 10, 32)
		if err != nil {
			return nil, fmt.Errorf("trezor signer: invalid derivation path")
		}
		number := uint32(value)
		if hardened {
			number |= 0x80000000
		}
		numbers = append(numbers, number)
	}
	if len(numbers) == 0 || len(numbers) > 10 {
		return nil, fmt.Errorf("trezor signer: derivation path depth")
	}
	return numbers, nil
}
