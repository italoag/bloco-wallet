package evm_test

import (
	"bytes"
	"fmt"
	"testing"

	"blocowallet/internal/evm"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

const eip712MailFixture = `{
	"types": {
		"EIP712Domain": [
			{"name": "name", "type": "string"},
			{"name": "version", "type": "string"},
			{"name": "chainId", "type": "uint256"},
			{"name": "verifyingContract", "type": "address"}
		],
		"Person": [
			{"name": "name", "type": "string"},
			{"name": "wallet", "type": "address"}
		],
		"Mail": [
			{"name": "from", "type": "Person"},
			{"name": "to", "type": "Person"},
			{"name": "contents", "type": "string"}
		]
	},
	"primaryType": "Mail",
	"domain": {
		"name": "Ether Mail",
		"version": "1",
		"chainId": 1,
		"verifyingContract": "0xCcCCccccCCCCcCCCCCCcCcCccCcCCCcCcccccccC"
	},
	"message": {
		"from": {"name": "Cow", "wallet": "0xCD2a3d9F938E13CD947Ec05AbC7FE734Df8DD826"},
		"to": {"name": "Bob", "wallet": "0xbBbBBBBbbBBBbbbBbbBbbbbBBbBbbbbBbBbbBBbB"},
		"contents": "Hello, Bob!"
	}
}`

func TestPrepareEIP712MatchesOfficialSpecVector(t *testing.T) {
	prepared, err := evm.PrepareEIP712Sign(evm.PrepareEIP712SignRequest{
		AccountID: "11111111-1111-4111-8111-111111111111",
		Signer:    common.HexToAddress("0x1563915e194D8CfBA1943570603F7606A3115508"),
		ChainID:   1,
		TypedData: []byte(eip712MailFixture),
		Origin:    "local-user",
	})
	if err != nil {
		t.Fatal(err)
	}
	preview := prepared.Preview()
	if preview.PrimaryType != "Mail" || preview.DomainName != "Ether Mail" || preview.DomainVersion != "1" || preview.DomainChainID != 1 || preview.VerifyingContract.Hex() != "0xCcCCccccCCCCcCCCCCCcCcCccCcCCCcCcccccccC" {
		t.Fatalf("unexpected EIP-712 preview: %+v", preview)
	}
	if !bytes.Equal(preview.Digest[:], hexutil.MustDecode("0xbe609aee343fb3c4b28e1df9e632fca64fcfaede20f02e86244efddf30957bd2")) {
		t.Fatalf("EIP-712 digest does not match the official vector: %x", preview.Digest)
	}
	if preview.IntentHash == ([32]byte{}) || !bytes.Contains(preview.CanonicalJSON, []byte(`"contents":"Hello, Bob!"`)) {
		t.Fatal("EIP-712 preview omitted canonical binding material")
	}
	if preview.Rendered == "" || !bytes.Contains([]byte(preview.Rendered), []byte("Cow")) || !bytes.Contains([]byte(preview.Rendered), []byte("Hello, Bob!")) {
		t.Fatal("EIP-712 preview omitted every typed field")
	}
}

func TestPrepareEIP712PreservesLargeIntegerPrecisionInDigest(t *testing.T) {
	fixture := `{
		"types": {
			"EIP712Domain": [{"name": "name", "type": "string"}, {"name": "version", "type": "string"}, {"name": "chainId", "type": "uint256"}, {"name": "verifyingContract", "type": "address"}],
			"Transfer": [{"name": "value", "type": "uint256"}]
		},
		"primaryType": "Transfer",
		"domain": {"name": "Precision", "version": "1", "chainId": 1, "verifyingContract": "0xCcCCccccCCCCcCCCCCCcCcCccCcCCCcCcccccccC"},
		"message": {"value": %s}
	}`
	prepare := func(value string) ([32]byte, error) {
		prepared, err := evm.PrepareEIP712Sign(evm.PrepareEIP712SignRequest{
			AccountID: "11111111-1111-4111-8111-111111111111",
			Signer:    common.HexToAddress("0x1563915e194D8CfBA1943570603F7606A3115508"),
			ChainID:   1,
			TypedData: []byte(fmt.Sprintf(fixture, value)),
			Origin:    "local-user",
		})
		if err != nil {
			return [32]byte{}, err
		}
		return prepared.Preview().Digest, nil
	}
	aboveFloat53, err := prepare("9007199254740993")
	if err != nil {
		t.Fatal(err)
	}
	belowFloat53, err := prepare("9007199254740992")
	if err != nil {
		t.Fatal(err)
	}
	if aboveFloat53 == belowFloat53 {
		t.Fatal("EIP-712 digest truncated a 2^53+1 integer to float64 precision")
	}
	maximumUint256, err := prepare("115792089237316195423570985008687907853269984665640564039457584007913129639935")
	if err != nil {
		t.Fatalf("EIP-712 rejected the full uint256 range: %v", err)
	}
	if maximumUint256 == belowFloat53 || maximumUint256 == aboveFloat53 {
		t.Fatal("EIP-712 digest collapsed distinct uint256 values")
	}
}

func TestPrepareEIP712RejectsTrailingGarbageAndSanitizesPreview(t *testing.T) {
	base := evm.PrepareEIP712SignRequest{
		AccountID: "11111111-1111-4111-8111-111111111111",
		Signer:    common.HexToAddress("0x1563915e194D8CfBA1943570603F7606A3115508"),
		ChainID:   1,
		TypedData: []byte(eip712MailFixture),
		Origin:    "local-user",
	}
	garbage := append(append([]byte(nil), eip712MailFixture...), []byte(" xyz")...)
	if _, err := evm.PrepareEIP712Sign(evm.PrepareEIP712SignRequest{
		AccountID: base.AccountID, Signer: base.Signer, ChainID: base.ChainID,
		TypedData: garbage, Origin: base.Origin,
	}); err == nil {
		t.Fatal("trailing garbage after JSON was accepted")
	}
	escaped := bytes.Replace([]byte(eip712MailFixture), []byte(`"contents": "Hello, Bob!"`), []byte(`"contents": "ok\u001b]0;x\u0007bye"`), 1)
	prepared, err := evm.PrepareEIP712Sign(evm.PrepareEIP712SignRequest{
		AccountID: base.AccountID, Signer: base.Signer, ChainID: base.ChainID,
		TypedData: escaped, Origin: base.Origin,
	})
	if err != nil {
		t.Fatal(err)
	}
	rendered := prepared.Preview().Rendered
	if bytes.Contains([]byte(rendered), []byte{0x1b}) {
		t.Fatal("EIP-712 preview leaked terminal escape sequences")
	}
	if !bytes.Contains([]byte(rendered), []byte("bye")) {
		t.Fatal("EIP-712 preview dropped sanitized content")
	}
}

func TestPrepareEIP712RejectsStrictJSONAndPolicyViolations(t *testing.T) {
	base := evm.PrepareEIP712SignRequest{
		AccountID: "11111111-1111-4111-8111-111111111111",
		Signer:    common.HexToAddress("0x1563915e194D8CfBA1943570603F7606A3115508"),
		ChainID:   1,
		TypedData: []byte(eip712MailFixture),
		Origin:    "local-user",
	}
	duplicateKeys := bytes.Replace([]byte(eip712MailFixture), []byte(`"primaryType": "Mail"`), []byte(`"primaryType": "Mail", "primaryType": "Mail"`), 1)
	trailing := append(append([]byte(nil), eip712MailFixture...), []byte(" {}\n")...)
	badChecksum := bytes.Replace([]byte(eip712MailFixture), []byte(`"verifyingContract": "0xCcCCccccCCCCcCCCCCCcCcCccCcCCCcCcccccccC"`), []byte(`"verifyingContract": "0xcccccccccccccccccccccccccccccccccccccccc"`), 1)
	mismatchedChain := bytes.Replace([]byte(eip712MailFixture), []byte(`"chainId": 1`), []byte(`"chainId": 5`), 1)
	unsafeFieldName := bytes.ReplaceAll([]byte(eip712MailFixture), []byte(`"contents"`), []byte(`"con\u001btents"`))
	wrongDomainType := bytes.Replace([]byte(eip712MailFixture), []byte(`{"name": "chainId", "type": "uint256"}`), []byte(`{"name": "chainId", "type": "bytes32"}`), 1)
	for _, value := range [][]byte{duplicateKeys, trailing, badChecksum, mismatchedChain, unsafeFieldName, wrongDomainType} {
		request := base
		request.TypedData = value
		if _, err := evm.PrepareEIP712Sign(request); err == nil {
			t.Fatal("strict EIP-712 JSON violation was accepted")
		}
	}
	missingChain := bytes.Replace([]byte(eip712MailFixture), []byte(`{"name": "chainId", "type": "uint256"},`), nil, 1)
	missingChain = bytes.Replace(missingChain, []byte(`"chainId": 1,`), nil, 1)
	request := base
	request.TypedData = missingChain
	if _, err := evm.PrepareEIP712Sign(request); err == nil {
		t.Fatal("EIP-712 without domain chain ID was accepted")
	}
	oversized := bytes.Replace([]byte(eip712MailFixture), []byte("Hello, Bob!"), []byte(`{"a":["aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"]}`), 1)
	request.TypedData = oversized
	if _, err := evm.PrepareEIP712Sign(request); err == nil {
		t.Fatal("EIP-712 with nested fields beyond the budget was accepted")
	}
	cyclic := `{
		"types": {
			"EIP712Domain": [{"name": "chainId", "type": "uint256"}],
			"A": [{"name": "b", "type": "B"}],
			"B": [{"name": "a", "type": "A"}]
		},
		"primaryType": "A",
		"domain": {"chainId": 1},
		"message": {}
	}`
	request.TypedData = []byte(cyclic)
	if _, err := evm.PrepareEIP712Sign(request); err == nil {
		t.Fatal("EIP-712 indirect type cycle was accepted")
	}
	noDomainType := bytes.Replace([]byte(cyclic), []byte(`"EIP712Domain": [{"name": "chainId", "type": "uint256"}],`), nil, 1)
	request.TypedData = noDomainType
	if _, err := evm.PrepareEIP712Sign(request); err == nil {
		t.Fatal("EIP-712 without EIP712Domain type was accepted")
	}
}
