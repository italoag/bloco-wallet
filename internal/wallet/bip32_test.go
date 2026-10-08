package wallet

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/big"
	"testing"
)

// Official BIP32 test vectors
// (https://github.com/bitcoin/bips/blob/master/bip-0032.mediawiki#test-vectors).
// Each expected xprv is decoded and its private key and chain code compared
// against the derivation result at every step of the chain.

type bip32VectorChain struct {
	path []uint32
	xprv string
}

type bip32Vector struct {
	name   string
	seed   string
	chains []bip32VectorChain
}

var officialBIP32Vectors = []bip32Vector{
	{
		name: "vector 1",
		seed: "000102030405060708090a0b0c0d0e0f",
		chains: []bip32VectorChain{
			{path: nil, xprv: "xprv9s21ZrQH143K3QTDL4LXw2F7HEK3wJUD2nW2nRk4stbPy6cq3jPPqjiChkVvvNKmPGJxWUtg6LnF5kejMRNNU3TGtRBeJgk33yuGBxrMPHi"},
			{path: []uint32{firstHardenedChild + 0}, xprv: "xprv9uHRZZhk6KAJC1avXpDAp4MDc3sQKNxDiPvvkX8Br5ngLNv1TxvUxt4cV1rGL5hj6KCesnDYUhd7oWgT11eZG7XnxHrnYeSvkzY7d2bhkJ7"},
			{path: []uint32{firstHardenedChild + 0, 1}, xprv: "xprv9wTYmMFdV23N2TdNG573QoEsfRrWKQgWeibmLntzniatZvR9BmLnvSxqu53Kw1UmYPxLgboyZQaXwTCg8MSY3H2EU4pWcQDnRnrVA1xe8fs"},
			{path: []uint32{firstHardenedChild + 0, 1, firstHardenedChild + 2}, xprv: "xprv9z4pot5VBttmtdRTWfWQmoH1taj2axGVzFqSb8C9xaxKymcFzXBDptWmT7FwuEzG3ryjH4ktypQSAewRiNMjANTtpgP4mLTj34bhnZX7UiM"},
			{path: []uint32{firstHardenedChild + 0, 1, firstHardenedChild + 2, 2}, xprv: "xprvA2JDeKCSNNZky6uBCviVfJSKyQ1mDYahRjijr5idH2WwLsEd4Hsb2Tyh8RfQMuPh7f7RtyzTtdrbdqqsunu5Mm3wDvUAKRHSC34sJ7in334"},
			{path: []uint32{firstHardenedChild + 0, 1, firstHardenedChild + 2, 2, 1000000000}, xprv: "xprvA41z7zogVVwxVSgdKUHDy1SKmdb533PjDz7J6N6mV6uS3ze1ai8FHa8kmHScGpWmj4WggLyQjgPie1rFSruoUihUZREPSL39UNdE3BBDu76"},
		},
	},
	{
		name: "vector 2",
		seed: "fffcf9f6f3f0edeae7e4e1dedbd8d5d2cfccc9c6c3c0bdbab7b4b1aeaba8a5a29f9c999693908d8a8784817e7b7875726f6c696663605d5a5754514e4b484542",
		chains: []bip32VectorChain{
			{path: nil, xprv: "xprv9s21ZrQH143K31xYSDQpPDxsXRTUcvj2iNHm5NUtrGiGG5e2DtALGdso3pGz6ssrdK4PFmM8NSpSBHNqPqm55Qn3LqFtT2emdEXVYsCzC2U"},
			{path: []uint32{0}, xprv: "xprv9vHkqa6EV4sPZHYqZznhT2NPtPCjKuDKGY38FBWLvgaDx45zo9WQRUT3dKYnjwih2yJD9mkrocEZXo1ex8G81dwSM1fwqWpWkeS3v86pgKt"},
			{path: []uint32{0, firstHardenedChild + 2147483647}, xprv: "xprv9wSp6B7kry3Vj9m1zSnLvN3xH8RdsPP1Mh7fAaR7aRLcQMKTR2vidYEeEg2mUCTAwCd6vnxVrcjfy2kRgVsFawNzmjuHc2YmYRmagcEPdU9"},
			{path: []uint32{0, firstHardenedChild + 2147483647, 1}, xprv: "xprv9zFnWC6h2cLgpmSA46vutJzBcfJ8yaJGg8cX1e5StJh45BBciYTRXSd25UEPVuesF9yog62tGAQtHjXajPPdbRCHuWS6T8XA2ECKADdw4Ef"},
			{path: []uint32{0, firstHardenedChild + 2147483647, 1, firstHardenedChild + 2147483646}, xprv: "xprvA1RpRA33e1JQ7ifknakTFpgNXPmW2YvmhqLQYMmrj4xJXXWYpDPS3xz7iAxn8L39njGVyuoseXzU6rcxFLJ8HFsTjSyQbLYnMpCqE2VbFWc"},
			{path: []uint32{0, firstHardenedChild + 2147483647, 1, firstHardenedChild + 2147483646, 2}, xprv: "xprvA2nrNbFZABcdryreWet9Ea4LvTJcGsqrMzxHx98MMrotbir7yrKCEXw7nadnHM8Dq38EGfSh6dqA9QWTyefMLEcBYJUuekgW4BYPJcr9E7j"},
		},
	},
	{
		name: "vector 3 (leading zeros)",
		seed: "4b381541583be4423346c643850da4b320e46a87ae3d2a4e6da11eba819cd4acba45d239319ac14f863b8d5ab5a0d0c64d2e8a1e7d1457df2e5a3c51c73235be",
		chains: []bip32VectorChain{
			{path: nil, xprv: "xprv9s21ZrQH143K25QhxbucbDDuQ4naNntJRi4KUfWT7xo4EKsHt2QJDu7KXp1A3u7Bi1j8ph3EGsZ9Xvz9dGuVrtHHs7pXeTzjuxBrCmmhgC6"},
			{path: []uint32{firstHardenedChild + 0}, xprv: "xprv9uPDJpEQgRQfDcW7BkF7eTya6RPxXeJCqCJGHuCJ4GiRVLzkTXBAJMu2qaMWPrS7AANYqdq6vcBcBUdJCVVFceUvJFjaPdGZ2y9WACViL4L"},
		},
	},
	{
		name: "vector 4 (leading zeros)",
		seed: "3ddd5602285899a946114506157c7997e5444528f3003f6134712147db19b678",
		chains: []bip32VectorChain{
			{path: nil, xprv: "xprv9s21ZrQH143K48vGoLGRPxgo2JNkJ3J3fqkirQC2zVdk5Dgd5w14S7fRDyHH4dWNHUgkvsvNDCkvAwcSHNAQwhwgNMgZhLtQC63zxwhQmRv"},
			{path: []uint32{firstHardenedChild + 0}, xprv: "xprv9vB7xEWwNp9kh1wQRfCCQMnZUEG21LpbR9NPCNN1dwhiZkjjeGRnaALmPXCX7SgjFTiCTT6bXes17boXtjq3xLpcDjzEuGLQBM5ohqkao9G"},
			{path: []uint32{firstHardenedChild + 0, firstHardenedChild + 1}, xprv: "xprv9xJocDuwtYCMNAo3Zw76WENQeAS6WGXQ55RCy7tDJ8oALr4FWkuVoHJeHVAcAqiZLE7Je3vZJHxspZdFHfnBEjHqU5hG1Jaj32dVoS6XLT1"},
		},
	},
}

func TestBIP32OfficialVectors(t *testing.T) {
	for _, vector := range officialBIP32Vectors {
		seed, err := hex.DecodeString(vector.seed)
		if err != nil {
			t.Fatalf("%s: decode seed: %v", vector.name, err)
		}
		for _, chain := range vector.chains {
			key, err := newMasterKey(seed)
			if err != nil {
				t.Fatalf("%s: master key: %v", vector.name, err)
			}
			for _, index := range chain.path {
				if key, err = key.newChildKey(index); err != nil {
					t.Fatalf("%s: derive index %d: %v", vector.name, index, err)
				}
			}
			expected, err := decodeXPrv(chain.xprv)
			if err != nil {
				t.Fatalf("%s: decode xprv: %v", vector.name, err)
			}
			if !bytes.Equal(key.key, expected.privateKey) {
				t.Fatalf("%s: chain %v private key mismatch\n got %x\nwant %x", vector.name, chain.path, key.key, expected.privateKey)
			}
			if !bytes.Equal(key.chainCode, expected.chainCode) {
				t.Fatalf("%s: chain %v chain code mismatch\n got %x\nwant %x", vector.name, chain.path, key.chainCode, expected.chainCode)
			}
		}
	}
}

func TestBIP32RejectsInvalidSeeds(t *testing.T) {
	if _, err := newMasterKey(make([]byte, bip32MinSeedSize-1)); err == nil {
		t.Fatal("undersized seed was accepted")
	}
	if _, err := newMasterKey(make([]byte, bip32MaxSeedSize+1)); err == nil {
		t.Fatal("oversized seed was accepted")
	}
	var zeroKey *hdKey
	if _, err := zeroKey.newChildKey(0); err == nil {
		t.Fatal("nil parent key was accepted")
	}
}

// decodedXPrv holds the fields recovered from a base58check xprv string.
type decodedXPrv struct {
	privateKey []byte
	chainCode  []byte
}

// decodeXPrv decodes an extended private key: Base58Check over
// version(4) || depth(1) || parent fingerprint(4) || child number(4) ||
// chain code(32) || 0x00 || private key(32) = 78-byte payload plus a
// 4-byte double-SHA256 checksum (82 bytes decoded).
func decodeXPrv(encoded string) (*decodedXPrv, error) {
	decoded, err := decodeBase58(encoded)
	if err != nil {
		return nil, err
	}
	if len(decoded) != 82 {
		return nil, fmt.Errorf("invalid xprv length %d", len(decoded))
	}
	payload, checksum := decoded[:78], decoded[78:]
	firstHash := sha256.Sum256(payload)
	secondHash := sha256.Sum256(firstHash[:])
	if !bytes.Equal(secondHash[:4], checksum) {
		return nil, fmt.Errorf("xprv checksum mismatch")
	}
	// version = payload[0:4], depth = payload[4], parent fingerprint =
	// payload[5:9], child number = payload[9:13], chain code = payload[13:45],
	// key data = payload[45:78] (0x00 || 32-byte private key).
	keyData := payload[45:]
	if keyData[0] != 0x00 {
		return nil, fmt.Errorf("xprv key prefix is not 0x00")
	}
	return &decodedXPrv{
		privateKey: append([]byte(nil), keyData[1:33]...),
		chainCode:  append([]byte(nil), payload[13:45]...),
	}, nil
}

const base58Alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"

// decodeBase58 decodes a Base58-encoded string. Leading '1' characters map to
// leading zero bytes.
func decodeBase58(encoded string) ([]byte, error) {
	value := new(big.Int)
	radix := big.NewInt(58)
	for _, char := range encoded {
		index := bytes.IndexByte([]byte(base58Alphabet), byte(char))
		if index < 0 {
			return nil, fmt.Errorf("invalid base58 character %q", char)
		}
		value.Mul(value, radix)
		value.Add(value, big.NewInt(int64(index)))
	}
	magnitude := value.Bytes()
	leadingZeros := 0
	for leadingZeros < len(encoded) && encoded[leadingZeros] == '1' {
		leadingZeros++
	}
	decoded := make([]byte, leadingZeros+len(magnitude))
	copy(decoded[leadingZeros:], magnitude)
	return decoded, nil
}
