package wallet

import (
	"crypto/hmac"
	"crypto/sha512"
	"encoding/binary"
	"fmt"

	"github.com/decred/dcrd/dcrec/secp256k1/v4"
)

// BIP32 hierarchical deterministic key derivation
// (https://github.com/bitcoin/bips/blob/master/bip-0032.mediawiki).
//
// This is a minimal implementation of CKDpriv (master generation and
// hardened/non-hardened child derivation) covering exactly what EVM BIP-44
// derivation needs. It replaces the unmaintained
// github.com/tyler-smith/go-bip32 dependency and is verified against the
// official BIP32 test vectors in bip32_test.go.

// firstHardenedChild is the first index of the hardened BIP32 child range.
const firstHardenedChild uint32 = 0x80000000

// bip32MinSeedSize is the minimum BIP32 seed length (128 bits).
const bip32MinSeedSize = 16

// bip32MaxSeedSize is the maximum BIP32 seed length (512 bits).
const bip32MaxSeedSize = 64

// hdKey is a BIP32 extended private key: a 32-byte private key plus the
// 32-byte chain code used to derive children.
type hdKey struct {
	key       []byte // 32 bytes, in range [1, n-1]
	chainCode []byte // 32 bytes
}

// newMasterKey generates the master extended key from a BIP32 seed
// (128 to 512 bits, as produced by BIP39).
func newMasterKey(seed []byte) (*hdKey, error) {
	if len(seed) < bip32MinSeedSize || len(seed) > bip32MaxSeedSize {
		return nil, fmt.Errorf("bip32: seed size must be between %d and %d bytes", bip32MinSeedSize, bip32MaxSeedSize)
	}
	intermediate, err := bip32HMAC([]byte("Bitcoin seed"), seed)
	if err != nil {
		return nil, err
	}
	il, chainCode := intermediate[:32], intermediate[32:]
	keyBytes, err := parseBIP32PrivateKey(il)
	if err != nil {
		return nil, fmt.Errorf("bip32: master key: %w", err)
	}
	return &hdKey{key: keyBytes, chainCode: chainCode}, nil
}

// newChildKey derives the child key at index using BIP32 CKDpriv. Indices
// greater than or equal to firstHardenedChild derive hardened children.
func (key *hdKey) newChildKey(index uint32) (*hdKey, error) {
	if key == nil || len(key.key) != 32 || len(key.chainCode) != 32 {
		return nil, fmt.Errorf("bip32: invalid parent key")
	}
	data := make([]byte, 0, 37)
	if index >= firstHardenedChild {
		// Hardened: 0x00 || ser256(kpar) || ser32(i)
		data = append(data, 0x00)
		data = append(data, key.key...)
	} else {
		// Normal: serP(point(kpar)) || ser32(i)
		publicKey := secp256k1.PrivKeyFromBytes(key.key).PubKey()
		data = append(data, publicKey.SerializeCompressed()...)
	}
	var indexBytes [4]byte
	binary.BigEndian.PutUint32(indexBytes[:], index)
	data = append(data, indexBytes[:]...)

	intermediate, err := bip32HMAC(key.chainCode, data)
	if err != nil {
		return nil, err
	}
	il, chainCode := intermediate[:32], intermediate[32:]
	// ki = (IL + kpar) mod n
	var ilScalar, parentScalar secp256k1.ModNScalar
	if ilScalar.SetByteSlice(il) {
		return nil, fmt.Errorf("bip32: derived IL is out of range")
	}
	if parentScalar.SetByteSlice(key.key) {
		return nil, fmt.Errorf("bip32: parent key is out of range")
	}
	var childScalar secp256k1.ModNScalar
	childScalar.Add2(&ilScalar, &parentScalar)
	if childScalar.IsZero() {
		return nil, fmt.Errorf("bip32: derived key is zero")
	}
	childBytes := childScalar.Bytes()
	return &hdKey{key: childBytes[:], chainCode: chainCode}, nil
}

// bip32HMAC computes I = HMAC-SHA512(key, data).
func bip32HMAC(key, data []byte) ([]byte, error) {
	mac := hmac.New(sha512.New, key)
	if _, err := mac.Write(data); err != nil {
		return nil, fmt.Errorf("bip32: hmac: %w", err)
	}
	return mac.Sum(nil), nil
}

// parseBIP32PrivateKey validates a 32-byte candidate private key against the
// secp256k1 group order (must be in [1, n-1]) and returns a private copy.
func parseBIP32PrivateKey(candidate []byte) ([]byte, error) {
	var scalar secp256k1.ModNScalar
	if scalar.SetByteSlice(candidate) {
		return nil, fmt.Errorf("derived key is out of range")
	}
	if scalar.IsZero() {
		return nil, fmt.Errorf("derived key is zero")
	}
	keyBytes := scalar.Bytes()
	return keyBytes[:], nil
}
