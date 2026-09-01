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

const (
	trezorMessageInitialize                 = 0
	trezorMessageFeatures                   = 17
	trezorMessageFailure                    = 3
	trezorMessagePinMatrixRequest           = 18
	trezorMessageCancel                     = 20
	trezorMessageButtonRequest              = 26
	trezorMessageButtonAck                  = 27
	trezorMessagePassphraseRequest          = 41
	trezorMessageEthereumSignMessage        = 64
	trezorMessageEthereumMessageSignature   = 66
	trezorMessageEthereumGetPublicKey       = 450
	trezorMessageEthereumPublicKey          = 451
	trezorMessageEthereumSignTypedHash      = 470
	trezorMessageEthereumTypedDataSignature = 469
)

const (
	trezorPacketSize      = 64
	trezorPacketDataSize  = trezorPacketSize - 1
	trezorMaxMessageBytes = 1 << 20
	trezorBridgePort      = 21324
)

var ErrTrezorDeviceFailure = errors.New("trezor signer: device failure")

type trezorPacketTransport interface {
	WritePacket(context.Context, []byte) error
	ReadPacket(context.Context) ([]byte, error)
	Close() error
}

type udpPacketTransport struct {
	conn    net.Conn
	timeout time.Duration
}

type UDPDevice struct {
	address       string
	transport     trezorPacketTransport
	buttonHandler func(context.Context) error
}

func NewUDPDevice(ctx context.Context, address string) (*UDPDevice, error) {
	if address == "" {
		address = fmt.Sprintf("127.0.0.1:%d", trezorBridgePort)
	}
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fmt.Errorf("trezor signer: invalid emulator address")
	}
	ip := net.ParseIP(host)
	if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return nil, fmt.Errorf("trezor signer: emulator address must be loopback")
	}
	dialer := net.Dialer{Timeout: 5 * time.Second}
	conn, err := dialer.DialContext(ctx, "udp", address)
	if err != nil {
		return nil, fmt.Errorf("trezor signer: emulator dial: %w", err)
	}
	transport := &udpPacketTransport{conn: conn, timeout: 20 * time.Second}
	if err := transport.ping(ctx); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return &UDPDevice{address: address, transport: transport}, nil
}

func (device *UDPDevice) SetButtonHandler(handler func(context.Context) error) {
	device.buttonHandler = handler
}

func (device *UDPDevice) Initialize(ctx context.Context) (TrezorFeatures, error) {
	features, err := device.call(ctx, trezorMessageInitialize, trezorMessageFeatures, nil)
	if err != nil {
		return TrezorFeatures{}, err
	}
	return parseFeatures(features)
}

func (device *UDPDevice) EthereumGetPublicKey(ctx context.Context, derivationPath string) ([]byte, error) {
	path, err := derivationPathToNumbers(derivationPath)
	if err != nil {
		return nil, err
	}
	request := encodeAddressPath(path)
	response, err := device.call(ctx, trezorMessageEthereumGetPublicKey, trezorMessageEthereumPublicKey, request)
	if err != nil {
		return nil, err
	}
	return parseEthereumPublicKey(response)
}

func (device *UDPDevice) EthereumSignTypedHash(ctx context.Context, derivationPath string, domainSeparatorHash, messageHash [32]byte) ([]byte, error) {
	path, err := derivationPathToNumbers(derivationPath)
	if err != nil {
		return nil, err
	}
	request := encodeAddressPath(path)
	request = appendBytesField(request, 2, domainSeparatorHash[:])
	request = appendBytesField(request, 3, messageHash[:])
	response, err := device.call(ctx, trezorMessageEthereumSignTypedHash, trezorMessageEthereumTypedDataSignature, request)
	if err != nil {
		return nil, err
	}
	return parseSignatureField(response, 1)
}

func (device *UDPDevice) EthereumSignMessage(ctx context.Context, derivationPath string, message []byte) ([]byte, error) {
	if len(message) == 0 || len(message) > 64<<10 {
		return nil, fmt.Errorf("trezor signer: message size")
	}
	path, err := derivationPathToNumbers(derivationPath)
	if err != nil {
		return nil, err
	}
	request := encodeAddressPath(path)
	request = appendBytesField(request, 2, message)
	response, err := device.call(ctx, trezorMessageEthereumSignMessage, trezorMessageEthereumMessageSignature, request)
	if err != nil {
		return nil, err
	}
	return parseSignatureField(response, 2)
}

func (device *UDPDevice) Close() error {
	if device == nil || device.transport == nil {
		return nil
	}
	return device.transport.Close()
}

func (transport *udpPacketTransport) ping(ctx context.Context) error {
	if transport == nil || transport.conn == nil {
		return fmt.Errorf("trezor signer: emulator closed")
	}
	if err := transport.setDeadline(ctx); err != nil {
		return err
	}
	if _, err := transport.conn.Write([]byte("PINGPING")); err != nil {
		return fmt.Errorf("trezor signer: emulator ping: %w", err)
	}
	response := make([]byte, 8)
	count, err := transport.conn.Read(response)
	if err != nil {
		return fmt.Errorf("trezor signer: emulator ping: %w", err)
	}
	if count != 8 || string(response) != "PONGPONG" {
		return fmt.Errorf("trezor signer: invalid emulator ping response")
	}
	return nil
}

func (device *UDPDevice) call(ctx context.Context, messageType, expectedResponseType int, payload []byte) ([]byte, error) {
	responseType, responsePayload, err := device.exchange(ctx, messageType, payload)
	if err != nil {
		return nil, err
	}
	for responseType == trezorMessageButtonRequest {
		if device.buttonHandler == nil {
			_ = device.writeMessage(ctx, trezorMessageCancel, nil)
			return nil, ErrTrezorInteractionRequired
		}
		if err := device.writeMessage(ctx, trezorMessageButtonAck, nil); err != nil {
			return nil, err
		}
		if err := device.buttonHandler(ctx); err != nil {
			_ = device.writeMessage(ctx, trezorMessageCancel, nil)
			return nil, fmt.Errorf("trezor signer: device confirmation: %w", err)
		}
		responseType, responsePayload, err = device.readMessage(ctx)
		if err != nil {
			return nil, err
		}
	}
	switch responseType {
	case trezorMessageFailure:
		return nil, ErrTrezorDeviceFailure
	case trezorMessagePinMatrixRequest, trezorMessagePassphraseRequest:
		_ = device.writeMessage(ctx, trezorMessageCancel, nil)
		return nil, ErrTrezorLocked
	}
	if responseType != expectedResponseType {
		return nil, fmt.Errorf("trezor signer: unexpected response type %d", responseType)
	}
	return responsePayload, nil
}

func (device *UDPDevice) exchange(ctx context.Context, messageType int, payload []byte) (int, []byte, error) {
	if err := device.writeMessage(ctx, messageType, payload); err != nil {
		return 0, nil, err
	}
	return device.readMessage(ctx)
}

func (device *UDPDevice) writeMessage(ctx context.Context, messageType int, payload []byte) error {
	if device == nil || device.transport == nil {
		return fmt.Errorf("trezor signer: transport closed")
	}
	if len(payload) > trezorMaxMessageBytes {
		return fmt.Errorf("trezor signer: request too large")
	}
	buffer := make([]byte, 0, 2+2+4+len(payload))
	buffer = append(buffer, '#', '#')
	var header [6]byte
	binary.BigEndian.PutUint16(header[0:2], uint16(messageType))
	binary.BigEndian.PutUint32(header[2:6], uint32(len(payload)))
	buffer = append(buffer, header[:]...)
	buffer = append(buffer, payload...)
	for offset := 0; offset < len(buffer); {
		end := offset + trezorPacketDataSize
		if end > len(buffer) {
			end = len(buffer)
		}
		packet := make([]byte, trezorPacketSize)
		packet[0] = '?'
		copy(packet[1:], buffer[offset:end])
		if err := device.transport.WritePacket(ctx, packet); err != nil {
			return err
		}
		offset = end
	}
	return nil
}

func (device *UDPDevice) readMessage(ctx context.Context) (int, []byte, error) {
	if device == nil || device.transport == nil {
		return 0, nil, fmt.Errorf("trezor signer: transport closed")
	}
	first, err := device.transport.ReadPacket(ctx)
	if err != nil {
		return 0, nil, err
	}
	if len(first) != trezorPacketSize || string(first[:3]) != "?##" {
		return 0, nil, fmt.Errorf("trezor signer: malformed first packet")
	}
	responseType := int(binary.BigEndian.Uint16(first[3:5]))
	length := int(binary.BigEndian.Uint32(first[5:9]))
	if length < 0 || length > trezorMaxMessageBytes {
		return 0, nil, fmt.Errorf("trezor signer: response too large")
	}
	response := make([]byte, 0, length)
	remaining := length
	firstData := first[9:]
	if remaining < len(firstData) {
		firstData = firstData[:remaining]
	}
	response = append(response, firstData...)
	remaining -= len(firstData)
	for remaining > 0 {
		packet, err := device.transport.ReadPacket(ctx)
		if err != nil {
			return 0, nil, err
		}
		if len(packet) != trezorPacketSize || packet[0] != '?' {
			return 0, nil, fmt.Errorf("trezor signer: malformed continuation packet")
		}
		data := packet[1:]
		if remaining < len(data) {
			data = data[:remaining]
		}
		response = append(response, data...)
		remaining -= len(data)
	}
	return responseType, response, nil
}

func (transport *udpPacketTransport) WritePacket(ctx context.Context, packet []byte) error {
	if len(packet) != trezorPacketSize {
		return fmt.Errorf("trezor signer: invalid packet size")
	}
	if err := transport.setDeadline(ctx); err != nil {
		return err
	}
	if _, err := transport.conn.Write(packet); err != nil {
		return fmt.Errorf("trezor signer: emulator write: %w", err)
	}
	return nil
}

func (transport *udpPacketTransport) ReadPacket(ctx context.Context) ([]byte, error) {
	if err := transport.setDeadline(ctx); err != nil {
		return nil, err
	}
	packet := make([]byte, trezorPacketSize)
	count, err := transport.conn.Read(packet)
	if err != nil {
		return nil, fmt.Errorf("trezor signer: emulator read: %w", err)
	}
	if count != trezorPacketSize {
		return nil, fmt.Errorf("trezor signer: invalid packet size")
	}
	return packet, nil
}

func (transport *udpPacketTransport) Close() error {
	if transport == nil || transport.conn == nil {
		return nil
	}
	return transport.conn.Close()
}

func (transport *udpPacketTransport) setDeadline(ctx context.Context) error {
	deadline := time.Now().Add(transport.timeout)
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
		deadline = contextDeadline
	}
	if err := transport.conn.SetDeadline(deadline); err != nil {
		return fmt.Errorf("trezor signer: emulator deadline: %w", err)
	}
	return nil
}

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
		if wire == 0 {
			number, err := decodeVarintValue(value)
			if err != nil {
				return features, err
			}
			switch field {
			case 2:
				major = number
			case 3:
				minor = number
			case 4:
				patch = number
			case 7:
				features.PinProtection = number != 0
			case 8:
				features.PassphraseProtection = number != 0
			case 12:
				features.Initialized = number != 0
			}
		} else if field == 21 && wire == 2 {
			features.Model = string(value)
		}
	}
	features.Version = fmt.Sprintf("%d.%d.%d", major, minor, patch)
	return features, nil
}

func parseEthereumPublicKey(payload []byte) ([]byte, error) {
	for len(payload) > 0 {
		tag, value, rest, err := decodeField(payload)
		if err != nil {
			return nil, err
		}
		payload = rest
		if tag>>3 == 1 {
			return extractHDNodePublicKey(value)
		}
	}
	return nil, fmt.Errorf("trezor signer: public key missing")
}

func extractHDNodePublicKey(node []byte) ([]byte, error) {
	for len(node) > 0 {
		tag, value, rest, err := decodeField(node)
		if err != nil {
			return nil, err
		}
		node = rest
		if tag>>3 == 6 {
			if len(value) != 33 {
				return nil, fmt.Errorf("trezor signer: invalid public key size")
			}
			return append([]byte(nil), value...), nil
		}
	}
	return nil, fmt.Errorf("trezor signer: public key node missing")
}

func parseSignatureField(payload []byte, signatureField uint64) ([]byte, error) {
	for len(payload) > 0 {
		tag, value, rest, err := decodeField(payload)
		if err != nil {
			return nil, err
		}
		payload = rest
		if tag>>3 == signatureField {
			if len(value) != 65 {
				return nil, fmt.Errorf("trezor signer: invalid signature size")
			}
			return append([]byte(nil), value...), nil
		}
	}
	return nil, fmt.Errorf("trezor signer: signature missing")
}

func decodeField(data []byte) (tag uint64, value, rest []byte, err error) {
	tag, consumed, ok := decodeVarint(data)
	if !ok || consumed == 0 {
		return 0, nil, nil, fmt.Errorf("trezor signer: malformed field tag")
	}
	rest = data[consumed:]
	switch tag & 0x7 {
	case 0:
		_, size, valid := decodeVarint(rest)
		if !valid {
			return 0, nil, nil, fmt.Errorf("trezor signer: malformed varint")
		}
		return tag, rest[:size], rest[size:], nil
	case 1:
		if len(rest) < 8 {
			return 0, nil, nil, fmt.Errorf("trezor signer: truncated fixed64")
		}
		return tag, rest[:8], rest[8:], nil
	case 2:
		length, size, valid := decodeVarint(rest)
		if !valid || length > uint64(len(rest)-size) {
			return 0, nil, nil, fmt.Errorf("trezor signer: malformed length")
		}
		return tag, rest[size : size+int(length)], rest[size+int(length):], nil
	case 5:
		if len(rest) < 4 {
			return 0, nil, nil, fmt.Errorf("trezor signer: truncated fixed32")
		}
		return tag, rest[:4], rest[4:], nil
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

func decodeVarintValue(data []byte) (uint32, error) {
	value, _, ok := decodeVarint(data)
	if !ok || value > 0xffffffff {
		return 0, fmt.Errorf("trezor signer: malformed varint value")
	}
	return uint32(value), nil
}

func encodeAddressPath(values []uint32) []byte {
	var encoded []byte
	for _, value := range values {
		encoded = appendVarint(encoded, 1<<3)
		encoded = appendVarint(encoded, uint64(value))
	}
	return encoded
}

func appendBytesField(buffer []byte, field uint64, value []byte) []byte {
	buffer = appendVarint(buffer, field<<3|2)
	buffer = appendVarint(buffer, uint64(len(value)))
	return append(buffer, value...)
}

func appendVarint(buffer []byte, value uint64) []byte {
	for value >= 0x80 {
		buffer = append(buffer, byte(value)|0x80)
		value >>= 7
	}
	return append(buffer, byte(value))
}

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
		value, err := strconv.ParseUint(segment, 10, 31)
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
