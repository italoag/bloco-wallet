package signer

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rlp"
)

const (
	ledgerTransactionAPDUDataLimit  = 255
	ledgerMaxTransactionRLPSize     = 1 << 20
	ledgerMaxTransactionIntegerBits = 256
)

var (
	// ErrLedgerTransactionIntent identifies an invalid or unsupported structured
	// transaction request. The Ledger is never contacted for these failures.
	ErrLedgerTransactionIntent = errors.New("ledger signer: invalid transaction intent")
	// ErrLedgerTransactionDigest indicates that the caller-provided digest does
	// not match the canonical signing hash of the unsigned transaction.
	ErrLedgerTransactionDigest = errors.New("ledger signer: transaction digest mismatch")
)

// LedgerTransactionIntent binds all data needed to display and sign an Ethereum
// transaction. UnsignedTransaction must not contain v/r/s values. Digest is the
// canonical EIP-155 or EIP-1559 signing hash, not an independently signable
// opaque digest.
type LedgerTransactionIntent struct {
	UnsignedTransaction *types.Transaction
	ChainID             *big.Int
	DerivationPath      string
	Digest              [32]byte
	ExpectedAddress     common.Address
}

// SignTransaction streams a canonical unsigned EIP-155 or EIP-1559
// transaction to the Ledger Ethereum application using INS 0x04. It returns a
// canonical Ethereum signature in R || S || parity form only after checking the
// supplied digest and recovering ExpectedAddress from the device signature.
func (device *LedgerDevice) SignTransaction(ctx context.Context, intent LedgerTransactionIntent) ([]byte, error) {
	if device == nil || device.transport == nil {
		return nil, ErrLedgerTransport
	}
	device.mu.Lock()
	defer device.mu.Unlock()
	if ctx == nil {
		return nil, fmt.Errorf("%w: context required", ErrLedgerTransactionIntent)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if intent.ExpectedAddress == (common.Address{}) {
		return nil, fmt.Errorf("%w: expected address required", ErrLedgerTransactionIntent)
	}

	path, err := derivationPathToNumbers(intent.DerivationPath)
	if err != nil {
		return nil, fmt.Errorf("%w: derivation path: %v", ErrLedgerTransactionIntent, err)
	}
	transactionRLP, transactionSigner, chainID, digest, err := prepareLedgerTransaction(intent)
	if err != nil {
		return nil, err
	}

	pathData := make([]byte, 1+4*len(path))
	pathData[0] = byte(len(path))
	for index, component := range path {
		binary.BigEndian.PutUint32(pathData[1+4*index:], component)
	}
	payload := make([]byte, 0, len(pathData)+len(transactionRLP))
	payload = append(payload, pathData...)
	payload = append(payload, transactionRLP...)

	legacySuffixLength := 0
	if intent.UnsignedTransaction.Type() == types.LegacyTxType {
		legacySuffixLength = ledgerEIP155SuffixLength(chainID)
	}
	response, err := device.streamLedgerTransaction(ctx, payload, legacySuffixLength)
	if err != nil {
		return nil, err
	}
	candidates, err := decodeLedgerTransactionSignatures(response, intent.UnsignedTransaction.Type(), chainID)
	if err != nil {
		return nil, err
	}
	for _, signature := range candidates {
		if err := verifyECDSASignature(intent.ExpectedAddress, digest, signature); err == nil {
			// WithSignature also checks the transaction-specific signature shape.
			if _, err := intent.UnsignedTransaction.WithSignature(transactionSigner, signature); err == nil {
				return append([]byte(nil), signature...), nil
			}
		}
	}
	return nil, ErrLedgerSignature
}

func prepareLedgerTransaction(intent LedgerTransactionIntent) ([]byte, types.Signer, *big.Int, [32]byte, error) {
	if intent.UnsignedTransaction == nil {
		return nil, nil, nil, [32]byte{}, fmt.Errorf("%w: unsigned transaction required", ErrLedgerTransactionIntent)
	}
	if intent.ChainID == nil || intent.ChainID.Sign() <= 0 || !intent.ChainID.IsUint64() {
		return nil, nil, nil, [32]byte{}, fmt.Errorf("%w: chain ID must fit in a positive uint64", ErrLedgerTransactionIntent)
	}
	chainID := new(big.Int).Set(intent.ChainID)
	transaction := intent.UnsignedTransaction
	v, r, s := transaction.RawSignatureValues()
	if v.Sign() != 0 || r.Sign() != 0 || s.Sign() != 0 {
		return nil, nil, nil, [32]byte{}, fmt.Errorf("%w: transaction already has signature values", ErrLedgerTransactionIntent)
	}
	if len(transaction.Data()) > ledgerMaxTransactionRLPSize {
		return nil, nil, nil, [32]byte{}, fmt.Errorf("%w: transaction data exceeds %d bytes", ErrLedgerTransactionIntent, ledgerMaxTransactionRLPSize)
	}
	if err := validateLedgerTransactionInteger("value", transaction.Value()); err != nil {
		return nil, nil, nil, [32]byte{}, err
	}

	var (
		encoded           []byte
		transactionSigner types.Signer
		err               error
	)
	switch transaction.Type() {
	case types.LegacyTxType:
		if err := validateLedgerTransactionInteger("gas price", transaction.GasPrice()); err != nil {
			return nil, nil, nil, [32]byte{}, err
		}
		encoded, err = rlp.EncodeToBytes([]any{
			transaction.Nonce(), transaction.GasPrice(), transaction.Gas(), transaction.To(),
			transaction.Value(), transaction.Data(), chainID, new(big.Int), new(big.Int),
		})
		transactionSigner = types.NewEIP155Signer(chainID)
	case types.DynamicFeeTxType:
		if transaction.ChainId().Cmp(chainID) != 0 {
			return nil, nil, nil, [32]byte{}, fmt.Errorf("%w: EIP-1559 chain ID mismatch", ErrLedgerTransactionIntent)
		}
		if err := validateLedgerTransactionInteger("max priority fee per gas", transaction.GasTipCap()); err != nil {
			return nil, nil, nil, [32]byte{}, err
		}
		if err := validateLedgerTransactionInteger("max fee per gas", transaction.GasFeeCap()); err != nil {
			return nil, nil, nil, [32]byte{}, err
		}
		if transaction.GasFeeCap().Cmp(transaction.GasTipCap()) < 0 {
			return nil, nil, nil, [32]byte{}, fmt.Errorf("%w: max fee per gas is below priority fee", ErrLedgerTransactionIntent)
		}
		encoded, err = rlp.EncodeToBytes([]any{
			chainID, transaction.Nonce(), transaction.GasTipCap(), transaction.GasFeeCap(),
			transaction.Gas(), transaction.To(), transaction.Value(), transaction.Data(),
			transaction.AccessList(),
		})
		if err == nil {
			encoded = append([]byte{types.DynamicFeeTxType}, encoded...)
		}
		transactionSigner = types.NewLondonSigner(chainID)
	default:
		return nil, nil, nil, [32]byte{}, fmt.Errorf("%w: unsupported transaction type %#x", ErrLedgerTransactionIntent, transaction.Type())
	}
	if err != nil {
		return nil, nil, nil, [32]byte{}, fmt.Errorf("%w: canonical transaction encoding: %v", ErrLedgerTransactionIntent, err)
	}
	if len(encoded) == 0 || len(encoded) > ledgerMaxTransactionRLPSize {
		return nil, nil, nil, [32]byte{}, fmt.Errorf("%w: canonical transaction size", ErrLedgerTransactionIntent)
	}

	encodedHash := crypto.Keccak256Hash(encoded)
	signerHash := transactionSigner.Hash(transaction)
	if encodedHash != signerHash {
		return nil, nil, nil, [32]byte{}, fmt.Errorf("%w: canonical signing payload invariant", ErrLedgerTransactionIntent)
	}
	digest := [32]byte(encodedHash)
	if intent.Digest != digest {
		return nil, nil, nil, [32]byte{}, ErrLedgerTransactionDigest
	}
	return encoded, transactionSigner, chainID, digest, nil
}

func validateLedgerTransactionInteger(name string, value *big.Int) error {
	if value == nil || value.Sign() < 0 || value.BitLen() > ledgerMaxTransactionIntegerBits {
		return fmt.Errorf("%w: %s is outside uint256", ErrLedgerTransactionIntent, name)
	}
	return nil
}

func (device *LedgerDevice) streamLedgerTransaction(ctx context.Context, payload []byte, legacySuffixLength int) ([]byte, error) {
	chunkLengths := ledgerTransactionChunkLengths(len(payload), legacySuffixLength)
	offset := 0
	var finalResponse []byte
	for index, chunkLength := range chunkLengths {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		p1 := byte(0x00)
		if index != 0 {
			p1 = 0x80
		}
		chunk := append([]byte(nil), payload[offset:offset+chunkLength]...)
		response, status, err := device.transport.Exchange(ctx, ledgerCLA, ledgerINSSign, p1, 0x00, chunk)
		if err != nil {
			return nil, fmt.Errorf("%w: sign transaction chunk %d: %w", ErrLedgerTransport, index, err)
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if status != ledgerSWOK {
			return nil, mapLedgerStatus(status)
		}
		offset += chunkLength
		if index != len(chunkLengths)-1 {
			if len(response) != 0 {
				return nil, fmt.Errorf("%w: premature transaction signature response", ErrLedgerTransport)
			}
			continue
		}
		finalResponse = append([]byte(nil), response...)
	}
	return finalResponse, nil
}

func ledgerTransactionChunkLengths(payloadLength, legacySuffixLength int) []int {
	if payloadLength <= 0 {
		return nil
	}
	lengths := make([]int, 0, (payloadLength+ledgerTransactionAPDUDataLimit-1)/ledgerTransactionAPDUDataLimit)
	remaining := payloadLength
	for remaining > ledgerTransactionAPDUDataLimit {
		lengths = append(lengths, ledgerTransactionAPDUDataLimit)
		remaining -= ledgerTransactionAPDUDataLimit
	}
	lengths = append(lengths, remaining)

	// Older Ethereum app versions may finish parsing a six-field legacy
	// transaction if an APDU ends immediately before the EIP-155 chain ID. Keep
	// the complete chainID, 0, 0 suffix plus one preceding byte in the last APDU.
	last := len(lengths) - 1
	if last > 0 && legacySuffixLength > 0 && lengths[last] <= legacySuffixLength {
		borrow := legacySuffixLength + 1 - lengths[last]
		lengths[last-1] -= borrow
		lengths[last] += borrow
	}
	return lengths
}

func ledgerEIP155SuffixLength(chainID *big.Int) int {
	chainBytes := chainID.Bytes()
	chainEncodingLength := len(chainBytes)
	if len(chainBytes) != 1 || chainBytes[0] >= 0x80 {
		chainEncodingLength++
	}
	return chainEncodingLength + 2 // canonical RLP chain ID followed by 0 and 0
}

func decodeLedgerTransactionSignatures(response []byte, transactionType byte, chainID *big.Int) ([][]byte, error) {
	if transactionType != types.LegacyTxType {
		signature, err := decodeLedgerSignature(response)
		if err != nil {
			return nil, err
		}
		return [][]byte{signature}, nil
	}
	if len(response) != crypto.SignatureLength {
		_, err := decodeLedgerSignature(response)
		return nil, err
	}

	var (
		candidates [][]byte
		seen       [2]bool
	)
	addCandidate := func(v byte) {
		normalized := append([]byte(nil), response...)
		normalized[0] = v
		signature, err := decodeLedgerSignature(normalized)
		if err != nil || seen[signature[64]] {
			return
		}
		seen[signature[64]] = true
		candidates = append(candidates, signature)
	}

	// The official app returns EIP-155 v for legacy transactions in one byte.
	// For chain IDs longer than four bytes it historically derives that byte
	// from the first four big-endian bytes; match that wire behavior here.
	base := ledgerLegacyVBase(chainID)
	if response[0] == base {
		addCandidate(0)
	}
	if response[0] == base+1 {
		addCandidate(1)
	}
	// Some older firmware and test transports return parity or 27/28 directly.
	if direct, err := decodeLedgerSignature(response); err == nil && !seen[direct[64]] {
		seen[direct[64]] = true
		candidates = append(candidates, direct)
	}
	if len(candidates) == 0 {
		return nil, ErrLedgerTransport
	}
	return candidates, nil
}

func ledgerLegacyVBase(chainID *big.Int) byte {
	encoded := chainID.Bytes()
	if len(encoded) > 4 {
		encoded = encoded[:4]
	}
	var truncated uint32
	for _, current := range encoded {
		truncated = truncated<<8 | uint32(current)
	}
	return byte(truncated*2 + 35)
}
