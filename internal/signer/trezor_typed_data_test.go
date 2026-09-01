package signer

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	gethmath "github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/signer/core/apitypes"
)

const trezorTypedDataJSONFixture = `{
  "types": {
    "EIP712Domain": [
      {"name":"name","type":"string"},
      {"name":"chainId","type":"uint256"},
      {"name":"verifyingContract","type":"address"},
      {"name":"salt","type":"bytes32"}
    ],
    "Person": [
      {"name":"name","type":"string"},
      {"name":"wallet","type":"address"}
    ],
    "Batch": [
      {"name":"sender","type":"Person"},
      {"name":"reviewers","type":"Person[]"},
      {"name":"amounts","type":"uint16[]"},
      {"name":"flags","type":"bool[2]"},
      {"name":"delta","type":"int16"},
      {"name":"blob","type":"bytes"},
      {"name":"tag","type":"bytes3"},
      {"name":"memo","type":"string"},
      {"name":"recipient","type":"address"},
      {"name":"approved","type":"bool"},
      {"name":"nonce","type":"uint256"}
    ]
  },
  "primaryType": "Batch",
  "domain": {
    "name":"Bloco",
    "chainId":1,
    "verifyingContract":"0xCcCCccccCCCCcCCCCCCcCcCccCcCCCcCcccccccC",
    "salt":"0xdeadbeef00000000000000000000000000000000000000000000000000000000"
  },
  "message": {
    "sender":{"name":"Alice","wallet":"0x1111111111111111111111111111111111111111"},
    "reviewers":[
      {"name":"Bob","wallet":"0x2222222222222222222222222222222222222222"},
      {"name":"Carol","wallet":"0x3333333333333333333333333333333333333333"}
    ],
    "amounts":[1,256,65535],
    "flags":[true,false],
    "delta":-2,
    "blob":"0x010203",
    "tag":"0xaabbcc",
    "memo":"hello",
    "recipient":"0x4444444444444444444444444444444444444444",
    "approved":true,
    "nonce":9007199254740993
  }
}`

type trezorTypedDataWireMessage struct {
	messageType int
	payload     []byte
}

type trezorTypedDataScriptTransport struct {
	responsePackets [][]byte
	writes          []trezorTypedDataWireMessage
	assembling      bool
	writeType       int
	writeLength     int
	writePayload    []byte
}

func (transport *trezorTypedDataScriptTransport) WritePacket(_ context.Context, packet []byte) error {
	if len(packet) != trezorPacketSize || packet[0] != '?' {
		return errors.New("bad packet")
	}
	if !transport.assembling {
		if string(packet[:3]) != "?##" {
			return errors.New("bad first packet")
		}
		transport.writeType = int(binary.BigEndian.Uint16(packet[3:5]))
		transport.writeLength = int(binary.BigEndian.Uint32(packet[5:9]))
		transport.writePayload = transport.writePayload[:0]
		remaining := transport.writeLength
		if remaining > len(packet[9:]) {
			remaining = len(packet[9:])
		}
		transport.writePayload = append(transport.writePayload, packet[9:9+remaining]...)
		transport.assembling = len(transport.writePayload) < transport.writeLength
	} else {
		remaining := transport.writeLength - len(transport.writePayload)
		if remaining > len(packet[1:]) {
			remaining = len(packet[1:])
		}
		transport.writePayload = append(transport.writePayload, packet[1:1+remaining]...)
		transport.assembling = len(transport.writePayload) < transport.writeLength
	}
	if !transport.assembling {
		transport.writes = append(transport.writes, trezorTypedDataWireMessage{
			messageType: transport.writeType,
			payload:     append([]byte(nil), transport.writePayload...),
		})
	}
	return nil
}

func (transport *trezorTypedDataScriptTransport) ReadPacket(context.Context) ([]byte, error) {
	if len(transport.responsePackets) == 0 {
		return nil, errors.New("script exhausted")
	}
	packet := transport.responsePackets[0]
	transport.responsePackets = transport.responsePackets[1:]
	return append([]byte(nil), packet...), nil
}

func (*trezorTypedDataScriptTransport) Close() error { return nil }

func newTrezorTypedDataScriptTransport(responses ...trezorTypedDataWireMessage) *trezorTypedDataScriptTransport {
	transport := &trezorTypedDataScriptTransport{}
	for _, response := range responses {
		transport.responsePackets = append(transport.responsePackets, trezorTypedDataPackets(response.messageType, response.payload)...)
	}
	return transport
}

func trezorTypedDataPackets(messageType int, payload []byte) [][]byte {
	message := make([]byte, 8+len(payload))
	message[0], message[1] = '#', '#'
	binary.BigEndian.PutUint16(message[2:4], uint16(messageType))
	binary.BigEndian.PutUint32(message[4:8], uint32(len(payload)))
	copy(message[8:], payload)
	packets := make([][]byte, 0, (len(message)+trezorPacketDataSize-1)/trezorPacketDataSize)
	for offset := 0; offset < len(message); {
		end := offset + trezorPacketDataSize
		if end > len(message) {
			end = len(message)
		}
		packet := make([]byte, trezorPacketSize)
		packet[0] = '?'
		copy(packet[1:], message[offset:end])
		packets = append(packets, packet)
		offset = end
	}
	return packets
}

func trezorTypedDataStructResponse(name string) trezorTypedDataWireMessage {
	return trezorTypedDataWireMessage{
		messageType: trezorMessageEthereumTypedDataStructRequest,
		payload:     appendBytesField(nil, 1, []byte(name)),
	}
}

func trezorTypedDataValueResponse(path []uint32) trezorTypedDataWireMessage {
	var payload []byte
	for _, component := range path {
		payload = appendVarint(payload, 1<<3)
		payload = appendVarint(payload, uint64(component))
	}
	return trezorTypedDataWireMessage{messageType: trezorMessageEthereumTypedDataValueRequest, payload: payload}
}

func trezorTypedDataSignatureResponse(signature []byte) trezorTypedDataWireMessage {
	payload := appendBytesField(nil, 1, signature)
	payload = appendBytesField(payload, 2, []byte("0x1111111111111111111111111111111111111111"))
	return trezorTypedDataWireMessage{messageType: trezorMessageEthereumTypedDataSignature, payload: payload}
}

func trezorTypedDataSortedPaths(plan *trezorTypedDataPlan) [][]uint32 {
	keys := make([]string, 0, len(plan.encodedValues))
	for key := range plan.encodedValues {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	paths := make([][]uint32, 0, len(keys))
	for _, key := range keys {
		encoded := []byte(key)
		path := make([]uint32, len(encoded)/4)
		for index := range path {
			path[index] = binary.BigEndian.Uint32(encoded[index*4:])
		}
		paths = append(paths, path)
	}
	return paths
}

func trezorTypedDataSuccessResponses(plan *trezorTypedDataPlan, typeOrder []string, signature []byte) []trezorTypedDataWireMessage {
	responses := make([]trezorTypedDataWireMessage, 0, len(typeOrder)+len(plan.encodedValues)+1)
	for _, name := range typeOrder {
		responses = append(responses, trezorTypedDataStructResponse(name))
	}
	for _, path := range trezorTypedDataSortedPaths(plan) {
		responses = append(responses, trezorTypedDataValueResponse(path))
	}
	responses = append(responses, trezorTypedDataSignatureResponse(signature))
	return responses
}

func TestTrezorEthereumSignTypedDataStateMachine(t *testing.T) {
	plan, err := prepareTrezorTypedData([]byte(trezorTypedDataJSONFixture))
	if err != nil {
		t.Fatal(err)
	}
	signature := bytes.Repeat([]byte{0x5a}, 65)
	paths := trezorTypedDataSortedPaths(plan)
	responses := trezorTypedDataSuccessResponses(plan, []string{"EIP712Domain", "Batch", "Person"}, signature)
	transport := newTrezorTypedDataScriptTransport(responses...)
	device := &UDPDevice{transport: transport}

	actual, err := device.EthereumSignTypedData(context.Background(), "m/44'/60'/0'/0/7", []byte(trezorTypedDataJSONFixture), false)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(actual, signature) {
		t.Fatalf("signature mismatch: %x", actual)
	}
	if len(transport.responsePackets) != 0 {
		t.Fatalf("unconsumed response packets: %d", len(transport.responsePackets))
	}
	if got, want := len(transport.writes), 1+3+len(paths); got != want {
		t.Fatalf("write count %d, want %d", got, want)
	}
	for _, write := range transport.writes {
		if write.messageType == trezorMessageEthereumSignTypedHash {
			t.Fatal("Core typed-data flow fell back to typed-hash signing")
		}
	}

	initial := decodeTrezorTypedDataInitial(t, transport.writes[0].payload)
	wantPath := []uint32{0x8000002c, 0x8000003c, 0x80000000, 0, 7}
	if !equalTrezorTypedDataPath(initial.path, wantPath) || initial.primaryType != "Batch" || initial.metamaskV4Compat {
		t.Fatalf("unexpected initial request: %+v", initial)
	}

	domainMembers := decodeTrezorTypedDataStructAck(t, transport.writes[1].payload)
	if domainMembers["chainId"].dataType != trezorEthereumDataTypeUint || valueOrZero(domainMembers["chainId"].size) != 32 {
		t.Fatalf("bad domain uint metadata: %+v", domainMembers["chainId"])
	}
	batchMembers := decodeTrezorTypedDataStructAck(t, transport.writes[2].payload)
	assertTrezorFieldType(t, batchMembers["sender"], trezorEthereumDataTypeStruct, 2, "Person")
	if reviewers := batchMembers["reviewers"]; reviewers.dataType != trezorEthereumDataTypeArray || reviewers.size != nil || reviewers.entryType == nil {
		t.Fatalf("bad dynamic struct array metadata: %+v", reviewers)
	} else {
		assertTrezorFieldType(t, *reviewers.entryType, trezorEthereumDataTypeStruct, 2, "Person")
	}
	if flags := batchMembers["flags"]; flags.dataType != trezorEthereumDataTypeArray || valueOrZero(flags.size) != 2 || flags.entryType == nil || flags.entryType.dataType != trezorEthereumDataTypeBool {
		t.Fatalf("bad fixed bool array metadata: %+v", flags)
	}
	assertTrezorFieldType(t, batchMembers["delta"], trezorEthereumDataTypeInt, 2, "")
	assertTrezorFieldType(t, batchMembers["tag"], trezorEthereumDataTypeBytes, 3, "")

	expectedValues := expectedTrezorTypedDataFixtureValues(t)
	for index, path := range paths {
		write := transport.writes[4+index]
		if write.messageType != trezorMessageEthereumTypedDataValueAck {
			t.Fatalf("path %v got write type %d", path, write.messageType)
		}
		actualValue := decodeTrezorSingleBytesField(t, write.payload, 1)
		expected, exists := expectedValues[trezorTypedDataPathKey(path)]
		if !exists {
			t.Fatalf("missing independent expectation for path %v", path)
		}
		if !bytes.Equal(actualValue, expected) {
			t.Fatalf("path %v value %x, want %x", path, actualValue, expected)
		}
	}
	if len(expectedValues) != len(paths) {
		t.Fatalf("expected value count %d, path count %d", len(expectedValues), len(paths))
	}
}

type decodedTrezorTypedDataInitial struct {
	path                 []uint32
	primaryType          string
	metamaskV4Compat     bool
	metamaskV4CompatSeen bool
}

func decodeTrezorTypedDataInitial(t *testing.T, payload []byte) decodedTrezorTypedDataInitial {
	t.Helper()
	var decoded decodedTrezorTypedDataInitial
	for len(payload) > 0 {
		tag, value, rest, err := decodeField(payload)
		if err != nil {
			t.Fatal(err)
		}
		payload = rest
		switch tag >> 3 {
		case 1:
			component, err := decodeVarintValue(value)
			if err != nil {
				t.Fatal(err)
			}
			decoded.path = append(decoded.path, component)
		case 2:
			decoded.primaryType = string(value)
		case 3:
			compatibility, err := decodeVarintValue(value)
			if err != nil {
				t.Fatal(err)
			}
			decoded.metamaskV4Compat = compatibility != 0
			decoded.metamaskV4CompatSeen = true
		}
	}
	if !decoded.metamaskV4CompatSeen {
		t.Fatal("metamask_v4_compat was not sent")
	}
	return decoded
}

type decodedTrezorFieldType struct {
	dataType   uint32
	size       *uint32
	entryType  *decodedTrezorFieldType
	structName string
}

func decodeTrezorTypedDataStructAck(t *testing.T, payload []byte) map[string]decodedTrezorFieldType {
	t.Helper()
	members := make(map[string]decodedTrezorFieldType)
	for len(payload) > 0 {
		tag, memberPayload, rest, err := decodeField(payload)
		if err != nil || tag != 1<<3|2 {
			t.Fatalf("bad struct ack: tag=%d err=%v", tag, err)
		}
		payload = rest
		var name string
		var fieldType decodedTrezorFieldType
		for len(memberPayload) > 0 {
			memberTag, value, memberRest, memberErr := decodeField(memberPayload)
			if memberErr != nil {
				t.Fatal(memberErr)
			}
			memberPayload = memberRest
			switch memberTag >> 3 {
			case 1:
				fieldType = decodeTrezorTypedDataFieldType(t, value)
			case 2:
				name = string(value)
			}
		}
		members[name] = fieldType
	}
	return members
}

func decodeTrezorTypedDataFieldType(t *testing.T, payload []byte) decodedTrezorFieldType {
	t.Helper()
	var fieldType decodedTrezorFieldType
	for len(payload) > 0 {
		tag, value, rest, err := decodeField(payload)
		if err != nil {
			t.Fatal(err)
		}
		payload = rest
		switch tag >> 3 {
		case 1:
			fieldType.dataType, err = decodeVarintValue(value)
		case 2:
			var size uint32
			size, err = decodeVarintValue(value)
			fieldType.size = &size
		case 3:
			entry := decodeTrezorTypedDataFieldType(t, value)
			fieldType.entryType = &entry
		case 4:
			fieldType.structName = string(value)
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	return fieldType
}

func assertTrezorFieldType(t *testing.T, fieldType decodedTrezorFieldType, dataType, size uint32, structName string) {
	t.Helper()
	if fieldType.dataType != dataType || valueOrZero(fieldType.size) != size || fieldType.structName != structName {
		t.Fatalf("field type %+v, want kind=%d size=%d struct=%q", fieldType, dataType, size, structName)
	}
}

func valueOrZero(value *uint32) uint32 {
	if value == nil {
		return 0
	}
	return *value
}

func expectedTrezorTypedDataFixtureValues(t *testing.T) map[string][]byte {
	t.Helper()
	expected := make(map[string][]byte)
	put := func(path []uint32, value []byte) { expected[trezorTypedDataPathKey(path)] = value }
	put([]uint32{0, 0}, []byte("Bloco"))
	put([]uint32{0, 1}, leftPadTrezorTestInteger(t, "1", 32))
	put([]uint32{0, 2}, mustDecodeTrezorTestHex(t, "cccccccccccccccccccccccccccccccccccccccc"))
	put([]uint32{0, 3}, mustDecodeTrezorTestHex(t, "deadbeef00000000000000000000000000000000000000000000000000000000"))
	put([]uint32{1, 0, 0}, []byte("Alice"))
	put([]uint32{1, 0, 1}, bytes.Repeat([]byte{0x11}, 20))
	put([]uint32{1, 1}, []byte{0, 2})
	put([]uint32{1, 1, 0, 0}, []byte("Bob"))
	put([]uint32{1, 1, 0, 1}, bytes.Repeat([]byte{0x22}, 20))
	put([]uint32{1, 1, 1, 0}, []byte("Carol"))
	put([]uint32{1, 1, 1, 1}, bytes.Repeat([]byte{0x33}, 20))
	put([]uint32{1, 2}, []byte{0, 3})
	put([]uint32{1, 2, 0}, []byte{0, 1})
	put([]uint32{1, 2, 1}, []byte{1, 0})
	put([]uint32{1, 2, 2}, []byte{0xff, 0xff})
	put([]uint32{1, 3, 0}, []byte{1})
	put([]uint32{1, 3, 1}, []byte{0})
	put([]uint32{1, 4}, []byte{0xff, 0xfe})
	put([]uint32{1, 5}, []byte{1, 2, 3})
	put([]uint32{1, 6}, []byte{0xaa, 0xbb, 0xcc})
	put([]uint32{1, 7}, []byte("hello"))
	put([]uint32{1, 8}, bytes.Repeat([]byte{0x44}, 20))
	put([]uint32{1, 9}, []byte{1})
	put([]uint32{1, 10}, leftPadTrezorTestInteger(t, "9007199254740993", 32))
	return expected
}

func leftPadTrezorTestInteger(t *testing.T, text string, size int) []byte {
	t.Helper()
	integer, ok := new(big.Int).SetString(text, 10)
	if !ok {
		t.Fatal("bad test integer")
	}
	return integer.FillBytes(make([]byte, size))
}

func mustDecodeTrezorTestHex(t *testing.T, text string) []byte {
	t.Helper()
	decoded, err := hex.DecodeString(text)
	if err != nil {
		t.Fatal(err)
	}
	return decoded
}

func decodeTrezorSingleBytesField(t *testing.T, payload []byte, field uint64) []byte {
	t.Helper()
	var result []byte
	found := false
	for len(payload) > 0 {
		tag, value, rest, err := decodeField(payload)
		if err != nil {
			t.Fatal(err)
		}
		payload = rest
		if tag>>3 == field {
			if found || tag&7 != 2 {
				t.Fatal("duplicate or wrongly encoded bytes field")
			}
			result = append([]byte(nil), value...)
			found = true
		}
	}
	if !found {
		t.Fatal("bytes field missing")
	}
	return result
}

func equalTrezorTypedDataPath(left, right []uint32) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func TestTrezorEthereumSignTypedDataAcceptsAPITypesAndHandlesButtons(t *testing.T) {
	typedData := apitypes.TypedData{
		Types: apitypes.Types{
			"EIP712Domain": {{Name: "chainId", Type: "uint256"}},
			"Record": {
				{Name: "count", Type: "uint8"},
				{Name: "debt", Type: "int8"},
				{Name: "raw", Type: "bytes"},
				{Name: "label", Type: "string"},
				{Name: "owner", Type: "address"},
				{Name: "ready", Type: "bool"},
			},
		},
		PrimaryType: "Record",
		Domain:      apitypes.TypedDataDomain{ChainId: gethmath.NewHexOrDecimal256(1)},
		Message: apitypes.TypedDataMessage{
			"count": uint8(255), "debt": int8(-128), "raw": []byte{9, 8, 7},
			"label": "api", "owner": common.HexToAddress("0x5555555555555555555555555555555555555555"), "ready": true,
		},
	}
	plan, err := prepareTrezorTypedData(&typedData)
	if err != nil {
		t.Fatal(err)
	}
	signature := bytes.Repeat([]byte{0x6b}, 65)
	responses := trezorTypedDataSuccessResponses(plan, []string{"EIP712Domain", "Record"}, signature)
	responses = append([]trezorTypedDataWireMessage{{messageType: trezorMessageButtonRequest}}, responses...)
	responses = append(responses[:len(responses)-1], append([]trezorTypedDataWireMessage{{messageType: trezorMessageButtonRequest}}, responses[len(responses)-1:]...)...)
	transport := newTrezorTypedDataScriptTransport(responses...)
	device := &UDPDevice{transport: transport}
	buttonCalls := 0
	device.SetButtonHandler(func(context.Context) error {
		buttonCalls++
		return nil
	})

	actual, err := device.EthereumSignTypedData(context.Background(), "m/44'/60'/0'/0/0", typedData)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(actual, signature) || buttonCalls != 2 {
		t.Fatalf("signature/buttons mismatch: signature=%x calls=%d", actual, buttonCalls)
	}
	initial := decodeTrezorTypedDataInitial(t, transport.writes[0].payload)
	if !initial.metamaskV4Compat || initial.primaryType != "Record" {
		t.Fatalf("default compatibility not encoded: %+v", initial)
	}
	if transport.writes[1].messageType != trezorMessageButtonAck || transport.writes[len(transport.writes)-1].messageType != trezorMessageButtonAck {
		t.Fatalf("button acknowledgements not ordered: first=%d last=%d", transport.writes[1].messageType, transport.writes[len(transport.writes)-1].messageType)
	}
	for _, write := range transport.writes {
		if write.messageType == trezorMessageEthereumSignTypedHash {
			t.Fatal("typed-hash fallback was sent")
		}
	}
}

func TestTrezorEthereumSignTypedDataRejectsMalformedDeviceRequestsAndCancels(t *testing.T) {
	plan, err := prepareTrezorTypedData([]byte(trezorTypedDataJSONFixture))
	if err != nil {
		t.Fatal(err)
	}
	structPrefix := []trezorTypedDataWireMessage{
		trezorTypedDataStructResponse("EIP712Domain"),
		trezorTypedDataStructResponse("Batch"),
		trezorTypedDataStructResponse("Person"),
	}
	validPaths := trezorTypedDataSortedPaths(plan)
	tooDeep := make([]uint32, trezorTypedDataMaxDepth+2)
	tooDeep[0] = 1
	malformedVarint := trezorTypedDataWireMessage{messageType: trezorMessageEthereumTypedDataValueRequest, payload: []byte{0x08, 0x80}}
	overlongVarint := trezorTypedDataWireMessage{messageType: trezorMessageEthereumTypedDataValueRequest, payload: []byte{0x08, 0x80, 0x00}}

	tests := []struct {
		name      string
		responses []trezorTypedDataWireMessage
	}{
		{name: "unknown struct", responses: []trezorTypedDataWireMessage{trezorTypedDataStructResponse("Unknown")}},
		{name: "missing path", responses: append(append([]trezorTypedDataWireMessage(nil), structPrefix...), trezorTypedDataWireMessage{messageType: trezorMessageEthereumTypedDataValueRequest})},
		{name: "bad root", responses: append(append([]trezorTypedDataWireMessage(nil), structPrefix...), trezorTypedDataValueResponse([]uint32{2, 0}))},
		{name: "out of range", responses: append(append([]trezorTypedDataWireMessage(nil), structPrefix...), trezorTypedDataValueResponse([]uint32{1, 99}))},
		{name: "struct endpoint", responses: append(append([]trezorTypedDataWireMessage(nil), structPrefix...), trezorTypedDataValueResponse([]uint32{1, 0}))},
		{name: "path depth", responses: append(append([]trezorTypedDataWireMessage(nil), structPrefix...), trezorTypedDataValueResponse(tooDeep))},
		{name: "malformed protobuf", responses: append(append([]trezorTypedDataWireMessage(nil), structPrefix...), malformedVarint)},
		{name: "overlong varint", responses: append(append([]trezorTypedDataWireMessage(nil), structPrefix...), overlongVarint)},
		{name: "duplicate path", responses: append(append(append([]trezorTypedDataWireMessage(nil), structPrefix...), trezorTypedDataValueResponse(validPaths[0])), trezorTypedDataValueResponse(validPaths[0]))},
		{name: "struct after value", responses: append(append(append([]trezorTypedDataWireMessage(nil), structPrefix...), trezorTypedDataValueResponse(validPaths[0])), trezorTypedDataStructResponse("Batch"))},
		{name: "premature signature", responses: []trezorTypedDataWireMessage{trezorTypedDataSignatureResponse(bytes.Repeat([]byte{1}, 65))}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			transport := newTrezorTypedDataScriptTransport(test.responses...)
			device := &UDPDevice{transport: transport}
			_, callErr := device.EthereumSignTypedData(context.Background(), "m/44'/60'/0'/0/0", []byte(trezorTypedDataJSONFixture))
			if !errors.Is(callErr, ErrTrezorTypedDataProtocol) {
				t.Fatalf("error %v is not a protocol error", callErr)
			}
			if len(transport.writes) < 2 || transport.writes[0].messageType != trezorMessageEthereumSignTypedData || transport.writes[len(transport.writes)-1].messageType != trezorMessageCancel {
				t.Fatalf("typed request/cancel sequence missing: %+v", transport.writes)
			}
			for _, write := range transport.writes {
				if write.messageType == trezorMessageEthereumSignTypedHash {
					t.Fatal("malformed Core request triggered hash-only fallback")
				}
			}
		})
	}
}

func TestTrezorEthereumSignTypedDataInteractionCancellation(t *testing.T) {
	t.Run("missing handler", func(t *testing.T) {
		transport := newTrezorTypedDataScriptTransport(trezorTypedDataWireMessage{messageType: trezorMessageButtonRequest})
		device := &UDPDevice{transport: transport}
		_, err := device.EthereumSignTypedData(context.Background(), "m/44'/60'/0'/0/0", []byte(trezorTypedDataJSONFixture))
		if !errors.Is(err, ErrTrezorInteractionRequired) {
			t.Fatalf("unexpected error: %v", err)
		}
		assertTrezorTypedDataWriteTypes(t, transport.writes, trezorMessageEthereumSignTypedData, trezorMessageCancel)
	})

	t.Run("handler cancellation", func(t *testing.T) {
		transport := newTrezorTypedDataScriptTransport(trezorTypedDataWireMessage{messageType: trezorMessageButtonRequest})
		device := &UDPDevice{transport: transport}
		device.SetButtonHandler(func(context.Context) error { return context.Canceled })
		_, err := device.EthereumSignTypedData(context.Background(), "m/44'/60'/0'/0/0", []byte(trezorTypedDataJSONFixture))
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("unexpected error: %v", err)
		}
		assertTrezorTypedDataWriteTypes(t, transport.writes, trezorMessageEthereumSignTypedData, trezorMessageButtonAck, trezorMessageCancel)
	})

	t.Run("final call cancels once", func(t *testing.T) {
		plan, err := prepareTrezorTypedData([]byte(trezorTypedDataJSONFixture))
		if err != nil {
			t.Fatal(err)
		}
		responses := trezorTypedDataSuccessResponses(plan, []string{"EIP712Domain", "Batch", "Person"}, bytes.Repeat([]byte{1}, 65))
		responses[len(responses)-1] = trezorTypedDataWireMessage{messageType: trezorMessageButtonRequest}
		transport := newTrezorTypedDataScriptTransport(responses...)
		device := &UDPDevice{transport: transport}
		_, err = device.EthereumSignTypedData(context.Background(), "m/44'/60'/0'/0/0", []byte(trezorTypedDataJSONFixture))
		if !errors.Is(err, ErrTrezorInteractionRequired) {
			t.Fatalf("unexpected error: %v", err)
		}
		cancelCount := 0
		for _, write := range transport.writes {
			if write.messageType == trezorMessageCancel {
				cancelCount++
			}
		}
		if cancelCount != 1 || transport.writes[len(transport.writes)-1].messageType != trezorMessageCancel {
			t.Fatalf("final interaction wrote %d cancels: %+v", cancelCount, transport.writes)
		}
	})
}

func assertTrezorTypedDataWriteTypes(t *testing.T, writes []trezorTypedDataWireMessage, expected ...int) {
	t.Helper()
	if len(writes) != len(expected) {
		t.Fatalf("write count %d, want %d: %+v", len(writes), len(expected), writes)
	}
	for index := range expected {
		if writes[index].messageType != expected[index] {
			t.Fatalf("write %d type %d, want %d", index, writes[index].messageType, expected[index])
		}
	}
}

func TestTrezorEthereumSignTypedDataEnforcesInputDepthAndBudgets(t *testing.T) {
	base := func() apitypes.TypedData {
		return apitypes.TypedData{
			Types: apitypes.Types{
				"EIP712Domain": {{Name: "chainId", Type: "uint256"}},
				"Root":         {{Name: "value", Type: "uint256"}},
			},
			PrimaryType: "Root",
			Domain:      apitypes.TypedDataDomain{ChainId: gethmath.NewHexOrDecimal256(1)},
			Message:     apitypes.TypedDataMessage{"value": 1},
		}
	}

	tests := []struct {
		name  string
		input func() any
	}{
		{name: "duplicate JSON key", input: func() any {
			return []byte(strings.Replace(trezorTypedDataJSONFixture, `"primaryType": "Batch"`, `"primaryType":"Batch","primaryType":"Batch"`, 1))
		}},
		{name: "trailing JSON", input: func() any { return []byte(trezorTypedDataJSONFixture + `{}`) }},
		{name: "JSON size", input: func() any { return bytes.Repeat([]byte{' '}, trezorTypedDataMaxJSONBytes+1) }},
		{name: "nested array", input: func() any {
			value := base()
			value.Types["Root"][0].Type = "uint256[][]"
			value.Message["value"] = [][]int{{1}}
			return value
		}},
		{name: "fixed array length", input: func() any {
			value := base()
			value.Types["Root"][0].Type = "uint256[2]"
			value.Message["value"] = []int{1}
			return value
		}},
		{name: "integer overflow", input: func() any {
			value := base()
			value.Types["Root"][0].Type = "uint8"
			value.Message["value"] = 256
			return value
		}},
		{name: "array length", input: func() any {
			value := base()
			value.Types["Root"][0].Type = "uint256[]"
			value.Message["value"] = make([]int, trezorTypedDataMaxArrayLength+1)
			return value
		}},
		{name: "type depth", input: func() any { return deepTrezorTypedData(trezorTypedDataMaxDepth + 1) }},
		{name: "type budget", input: func() any {
			value := base()
			for index := 0; len(value.Types) <= trezorTypedDataMaxTypes; index++ {
				value.Types[fmt.Sprintf("Unused%d", index)] = nil
			}
			return value
		}},
		{name: "member budget", input: func() any {
			value := base()
			members := make([]apitypes.Type, trezorTypedDataMaxMembers+1)
			message := make(apitypes.TypedDataMessage, len(members))
			for index := range members {
				name := fmt.Sprintf("v%d", index)
				members[index] = apitypes.Type{Name: name, Type: "uint8"}
				message[name] = 0
			}
			value.Types["Root"] = members
			value.Message = message
			return value
		}},
		{name: "value request budget", input: func() any { return wideTrezorTypedData() }},
		{name: "dynamic value budget", input: func() any {
			value := base()
			value.Types["Root"][0].Type = "string[]"
			large := strings.Repeat("x", trezorTypedDataMaxDynamicBytes)
			items := make([]string, trezorTypedDataMaxArrayLength)
			for index := range items {
				items[index] = large
			}
			value.Message["value"] = items
			return value
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			transport := newTrezorTypedDataScriptTransport()
			device := &UDPDevice{transport: transport}
			_, err := device.EthereumSignTypedData(context.Background(), "m/44'/60'/0'/0/0", test.input())
			if !errors.Is(err, ErrTrezorTypedDataInvalid) {
				t.Fatalf("error %v is not an input error", err)
			}
			if len(transport.writes) != 0 {
				t.Fatalf("invalid input reached device: %+v", transport.writes)
			}
		})
	}
}

func deepTrezorTypedData(depth int) apitypes.TypedData {
	types := apitypes.Types{"EIP712Domain": {{Name: "chainId", Type: "uint256"}}}
	for index := 0; index < depth; index++ {
		name := fmt.Sprintf("Node%d", index)
		if index == depth-1 {
			types[name] = []apitypes.Type{{Name: "value", Type: "uint8"}}
		} else {
			types[name] = []apitypes.Type{{Name: "next", Type: fmt.Sprintf("Node%d", index+1)}}
		}
	}
	return apitypes.TypedData{
		Types: types, PrimaryType: "Node0",
		Domain:  apitypes.TypedDataDomain{ChainId: gethmath.NewHexOrDecimal256(1)},
		Message: apitypes.TypedDataMessage{},
	}
}

func wideTrezorTypedData() apitypes.TypedData {
	rootMembers := make([]apitypes.Type, trezorTypedDataMaxMembers)
	leafMembers := make([]apitypes.Type, trezorTypedDataMaxMembers)
	leafValue := make(map[string]any, trezorTypedDataMaxMembers)
	for index := range leafMembers {
		name := fmt.Sprintf("v%d", index)
		leafMembers[index] = apitypes.Type{Name: name, Type: "uint8"}
		leafValue[name] = 0
	}
	message := make(apitypes.TypedDataMessage, trezorTypedDataMaxMembers)
	for index := range rootMembers {
		name := fmt.Sprintf("items%d", index)
		rootMembers[index] = apitypes.Type{Name: name, Type: "Leaf[]"}
		items := make([]any, trezorTypedDataMaxArrayLength)
		for itemIndex := range items {
			items[itemIndex] = leafValue
		}
		message[name] = items
	}
	return apitypes.TypedData{
		Types: apitypes.Types{
			"EIP712Domain": {{Name: "chainId", Type: "uint256"}},
			"Root":         rootMembers,
			"Leaf":         leafMembers,
		},
		PrimaryType: "Root",
		Domain:      apitypes.TypedDataDomain{ChainId: gethmath.NewHexOrDecimal256(1)},
		Message:     message,
	}
}
