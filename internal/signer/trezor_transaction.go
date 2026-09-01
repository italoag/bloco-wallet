package signer

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

const (
	trezorMessageEthereumSignTx        = 58
	trezorMessageEthereumTxRequest     = 59
	trezorMessageEthereumTxAck         = 60
	trezorMessageEthereumSignTxEIP1559 = 452
	trezorTransactionInitialChunkBytes = 1024
	trezorTransactionMaximumDataBytes  = 1 << 20
)

var (
	ErrTrezorTransactionIntent   = errors.New("trezor signer: invalid transaction intent")
	ErrTrezorTransactionProtocol = errors.New("trezor signer: invalid transaction protocol")
)

// TrezorTransactionIntent binds an unsigned transaction to its chain, digest,
// derivation path, and expected device address.
type TrezorTransactionIntent struct {
	UnsignedTransaction *types.Transaction
	ChainID             *big.Int
	DerivationPath      string
	Digest              [32]byte
	ExpectedAddress     common.Address
}

type trezorTransactionResponse struct {
	dataLength uint32
	hasData    bool
	signatureV uint32
	hasV       bool
	signatureR []byte
	signatureS []byte
}

// SignTransaction drives EthereumSignTx/EthereumSignTxEIP1559 and its bounded
// data-chunk request loop.
func (device *UDPDevice) SignTransaction(ctx context.Context, intent TrezorTransactionIntent) ([]byte, error) {
	if device == nil || device.transport == nil || ctx == nil {
		return nil, ErrTrezorTransactionIntent
	}
	device.conversationMu.Lock()
	defer device.conversationMu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	path, err := derivationPathToNumbers(intent.DerivationPath)
	if err != nil {
		return nil, fmt.Errorf("%w: derivation path: %v", ErrTrezorTransactionIntent, err)
	}
	messageType, initialPayload, remainingData, signer, digest, err := prepareTrezorTransaction(path, intent)
	if err != nil {
		return nil, err
	}
	responsePayload, err := device.call(ctx, messageType, trezorMessageEthereumTxRequest, initialPayload)
	if err != nil {
		return nil, err
	}
	sessionActive := true
	defer func() {
		if sessionActive {
			_ = device.cancelAndDrain(ctx)
		}
	}()
	response, err := parseTrezorTransactionResponse(responsePayload)
	if err != nil {
		return nil, err
	}
	for rounds := 0; response.hasData; rounds++ {
		if rounds > (trezorTransactionMaximumDataBytes/trezorTransactionInitialChunkBytes)+1 {
			return nil, fmt.Errorf("%w: chunk request budget", ErrTrezorTransactionProtocol)
		}
		if response.hasV || len(response.signatureR) != 0 || len(response.signatureS) != 0 || response.dataLength == 0 || response.dataLength > trezorTransactionInitialChunkBytes || int(response.dataLength) > len(remainingData) {
			return nil, fmt.Errorf("%w: invalid data request", ErrTrezorTransactionProtocol)
		}
		requested := int(response.dataLength)
		ack := appendBytesField(nil, 1, remainingData[:requested])
		remainingData = remainingData[requested:]
		responsePayload, err = device.call(ctx, trezorMessageEthereumTxAck, trezorMessageEthereumTxRequest, ack)
		if err != nil {
			sessionActive = false
			return nil, err
		}
		response, err = parseTrezorTransactionResponse(responsePayload)
		if err != nil {
			return nil, err
		}
	}
	if response.hasV || len(response.signatureR) != 0 || len(response.signatureS) != 0 {
		sessionActive = false
	}
	if len(remainingData) != 0 || !response.hasV || len(response.signatureR) != 32 || len(response.signatureS) != 32 {
		return nil, fmt.Errorf("%w: premature or malformed signature", ErrTrezorTransactionProtocol)
	}
	signature, err := normalizeTrezorTransactionSignature(response, intent.UnsignedTransaction.Type(), intent.ChainID)
	if err != nil {
		return nil, err
	}
	if err := verifyECDSASignature(intent.ExpectedAddress, digest, signature); err != nil {
		return nil, ErrTrezorSignature
	}
	if _, err := intent.UnsignedTransaction.WithSignature(signer, signature); err != nil {
		return nil, fmt.Errorf("%w: apply signature: %v", ErrTrezorTransactionProtocol, err)
	}
	return signature, nil
}

func prepareTrezorTransaction(path []uint32, intent TrezorTransactionIntent) (int, []byte, []byte, types.Signer, [32]byte, error) {
	if intent.UnsignedTransaction == nil || intent.ChainID == nil || intent.ChainID.Sign() <= 0 || !intent.ChainID.IsUint64() || intent.ExpectedAddress == (common.Address{}) {
		return 0, nil, nil, nil, [32]byte{}, ErrTrezorTransactionIntent
	}
	transaction := intent.UnsignedTransaction
	v, r, s := transaction.RawSignatureValues()
	if v.Sign() != 0 || r.Sign() != 0 || s.Sign() != 0 || len(transaction.Data()) > trezorTransactionMaximumDataBytes {
		return 0, nil, nil, nil, [32]byte{}, ErrTrezorTransactionIntent
	}
	for _, value := range []*big.Int{transaction.Value(), transaction.GasPrice(), transaction.GasTipCap(), transaction.GasFeeCap()} {
		if value == nil || value.Sign() < 0 || value.BitLen() > 256 {
			return 0, nil, nil, nil, [32]byte{}, ErrTrezorTransactionIntent
		}
	}
	chainID := intent.ChainID.Uint64()
	data := transaction.Data()
	initialLength := len(data)
	if initialLength > trezorTransactionInitialChunkBytes {
		initialLength = trezorTransactionInitialChunkBytes
	}
	initial := data[:initialLength]
	remaining := append([]byte(nil), data[initialLength:]...)
	to := ""
	if transaction.To() != nil {
		to = transaction.To().Hex()
	}
	request := encodeAddressPath(path)
	var signer types.Signer
	var messageType int
	switch transaction.Type() {
	case types.LegacyTxType:
		messageType = trezorMessageEthereumSignTx
		signer = types.NewEIP155Signer(new(big.Int).Set(intent.ChainID))
		request = appendTrezorBytes(request, 2, new(big.Int).SetUint64(transaction.Nonce()))
		request = appendBytesField(request, 3, transaction.GasPrice().Bytes())
		request = appendTrezorBytes(request, 4, new(big.Int).SetUint64(transaction.Gas()))
		request = appendTrezorBytes(request, 6, transaction.Value())
		if len(initial) != 0 {
			request = appendBytesField(request, 7, initial)
		}
		request = appendTrezorVarint(request, 8, uint64(len(data)))
		request = appendTrezorVarint(request, 9, chainID)
		if to != "" {
			request = appendBytesField(request, 11, []byte(to))
		}
	case types.DynamicFeeTxType:
		if transaction.ChainId().Cmp(intent.ChainID) != 0 || transaction.GasFeeCap().Cmp(transaction.GasTipCap()) < 0 {
			return 0, nil, nil, nil, [32]byte{}, ErrTrezorTransactionIntent
		}
		messageType = trezorMessageEthereumSignTxEIP1559
		signer = types.NewLondonSigner(new(big.Int).Set(intent.ChainID))
		request = appendTrezorBytes(request, 2, new(big.Int).SetUint64(transaction.Nonce()))
		request = appendBytesField(request, 3, transaction.GasFeeCap().Bytes())
		request = appendBytesField(request, 4, transaction.GasTipCap().Bytes())
		request = appendTrezorBytes(request, 5, new(big.Int).SetUint64(transaction.Gas()))
		if to != "" {
			request = appendBytesField(request, 6, []byte(to))
		}
		request = appendBytesField(request, 7, transaction.Value().Bytes())
		if len(initial) != 0 {
			request = appendBytesField(request, 8, initial)
		}
		request = appendTrezorVarint(request, 9, uint64(len(data)))
		request = appendTrezorVarint(request, 10, chainID)
		for _, tuple := range transaction.AccessList() {
			entry := appendBytesField(nil, 1, []byte(tuple.Address.Hex()))
			for _, key := range tuple.StorageKeys {
				entry = appendBytesField(entry, 2, key[:])
			}
			request = appendBytesField(request, 11, entry)
		}
	default:
		return 0, nil, nil, nil, [32]byte{}, ErrTrezorTransactionIntent
	}
	digestHash := signer.Hash(transaction)
	digest := [32]byte(digestHash)
	if digest != intent.Digest {
		return 0, nil, nil, nil, [32]byte{}, ErrTrezorTransactionIntent
	}
	return messageType, request, remaining, signer, digest, nil
}

func appendTrezorBytes(buffer []byte, field uint64, value *big.Int) []byte {
	return appendBytesField(buffer, field, value.Bytes())
}

func appendTrezorVarint(buffer []byte, field, value uint64) []byte {
	buffer = appendVarint(buffer, field<<3)
	return appendVarint(buffer, value)
}

func parseTrezorTransactionResponse(payload []byte) (trezorTransactionResponse, error) {
	var response trezorTransactionResponse
	for len(payload) > 0 {
		tag, value, rest, err := decodeField(payload)
		if err != nil {
			return response, fmt.Errorf("%w: %v", ErrTrezorTransactionProtocol, err)
		}
		payload = rest
		field := tag >> 3
		switch field {
		case 1:
			parsed, err := decodeVarintValue(value)
			if err != nil || response.hasData {
				return response, ErrTrezorTransactionProtocol
			}
			response.dataLength, response.hasData = parsed, true
		case 2:
			parsed, _, ok := decodeVarint(value)
			if !ok || parsed > math.MaxUint32 || response.hasV {
				return response, ErrTrezorTransactionProtocol
			}
			response.signatureV, response.hasV = uint32(parsed), true
		case 3:
			if len(response.signatureR) != 0 {
				return response, ErrTrezorTransactionProtocol
			}
			response.signatureR = append([]byte(nil), value...)
		case 4:
			if len(response.signatureS) != 0 {
				return response, ErrTrezorTransactionProtocol
			}
			response.signatureS = append([]byte(nil), value...)
		default:
			return response, ErrTrezorTransactionProtocol
		}
	}
	return response, nil
}

func normalizeTrezorTransactionSignature(response trezorTransactionResponse, transactionType byte, chainID *big.Int) ([]byte, error) {
	var parity uint32
	switch response.signatureV {
	case 0, 1:
		parity = response.signatureV
	case 27, 28:
		parity = response.signatureV - 27
	default:
		if transactionType != types.LegacyTxType {
			return nil, ErrTrezorTransactionProtocol
		}
		base := new(big.Int).Mul(chainID, big.NewInt(2))
		base.Add(base, big.NewInt(35))
		if !base.IsUint64() || base.Uint64() > math.MaxUint32 {
			return nil, ErrTrezorTransactionProtocol
		}
		value := uint64(response.signatureV)
		switch value {
		case base.Uint64():
			parity = 0
		case base.Uint64() + 1:
			parity = 1
		default:
			return nil, ErrTrezorTransactionProtocol
		}
	}
	signature := make([]byte, crypto.SignatureLength)
	copy(signature[:32], response.signatureR)
	copy(signature[32:64], response.signatureS)
	signature[64] = byte(parity)
	r := new(big.Int).SetBytes(signature[:32])
	s := new(big.Int).SetBytes(signature[32:64])
	if !crypto.ValidateSignatureValues(signature[64], r, s, true) {
		return nil, ErrTrezorTransactionProtocol
	}
	return signature, nil
}
