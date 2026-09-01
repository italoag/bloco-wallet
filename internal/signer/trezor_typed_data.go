package signer

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"reflect"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/ethereum/go-ethereum/common"
	gethmath "github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/signer/core/apitypes"
)

const (
	trezorMessageEthereumSignTypedData          = 464
	trezorMessageEthereumTypedDataStructRequest = 465
	trezorMessageEthereumTypedDataStructAck     = 466
	trezorMessageEthereumTypedDataValueRequest  = 467
	trezorMessageEthereumTypedDataValueAck      = 468
)

const (
	trezorTypedDataMaxJSONBytes       = 64 << 10
	trezorTypedDataMaxTypes           = 32
	trezorTypedDataMaxMembers         = 32
	trezorTypedDataMaxTotalMembers    = 256
	trezorTypedDataMaxDepth           = 16
	trezorTypedDataMaxArrayLength     = 64
	trezorTypedDataMaxValueRequests   = 4096
	trezorTypedDataMaxEncodedBytes    = 64 << 10
	trezorTypedDataMaxDynamicBytes    = 64 << 10
	trezorTypedDataMaxJSONNodes       = 8192
	trezorTypedDataMaxButtonRequests  = 64
	trezorTypedDataMaxIdentifierBytes = 64
)

const (
	trezorEthereumDataTypeUint    uint32 = 1
	trezorEthereumDataTypeInt     uint32 = 2
	trezorEthereumDataTypeBytes   uint32 = 3
	trezorEthereumDataTypeString  uint32 = 4
	trezorEthereumDataTypeBool    uint32 = 5
	trezorEthereumDataTypeAddress uint32 = 6
	trezorEthereumDataTypeArray   uint32 = 7
	trezorEthereumDataTypeStruct  uint32 = 8
)

var (
	// ErrTrezorTypedDataInvalid indicates malformed or unsupported EIP-712 input.
	ErrTrezorTypedDataInvalid = errors.New("trezor signer: invalid typed data")
	// ErrTrezorTypedDataProtocol indicates a malformed typed-data request from the device.
	ErrTrezorTypedDataProtocol = errors.New("trezor signer: invalid typed-data protocol")
)

type trezorTypedDataDocument struct {
	types       apitypes.Types
	primaryType string
	domain      map[string]any
	message     map[string]any
}

type trezorTypedDataFieldType struct {
	dataType   uint32
	size       *uint32
	entryType  *trezorTypedDataFieldType
	structName string
}

type trezorTypedDataMember struct {
	name      string
	fieldType *trezorTypedDataFieldType
}

type trezorTypedDataSchema struct {
	members map[string][]trezorTypedDataMember
}

type trezorTypedDataPlan struct {
	primaryType   string
	structAcks    map[string][]byte
	reachable     map[string]struct{}
	encodedValues map[string][]byte
}

type trezorTypedDataPlanBuilder struct {
	plan         *trezorTypedDataPlan
	schema       *trezorTypedDataSchema
	valueCount   int
	encodedBytes int
	nodeCount    int
}

// EthereumSignTypedData signs complete EIP-712 data with the Core typed-data
// protocol. Input may be canonical JSON ([]byte, json.RawMessage, or string),
// apitypes.TypedData, or *apitypes.TypedData. MetaMask v4 compatibility defaults
// to true and may be explicitly selected with the optional argument.
func (device *UDPDevice) EthereumSignTypedData(
	ctx context.Context,
	derivationPath string,
	input any,
	metamaskV4Compat ...bool,
) (signature []byte, err error) {
	if device == nil {
		return nil, fmt.Errorf("%w: transport closed", ErrTrezorTypedDataInvalid)
	}
	device.conversationMu.Lock()
	defer device.conversationMu.Unlock()
	if ctx == nil {
		return nil, fmt.Errorf("%w: nil context", ErrTrezorTypedDataInvalid)
	}
	compatibility := true
	if len(metamaskV4Compat) > 1 {
		return nil, fmt.Errorf("%w: metamask compatibility option", ErrTrezorTypedDataInvalid)
	}
	if len(metamaskV4Compat) == 1 {
		compatibility = metamaskV4Compat[0]
	}
	path, err := derivationPathToNumbers(derivationPath)
	if err != nil {
		return nil, err
	}
	plan, err := prepareTrezorTypedData(input)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	request := encodeTrezorSignTypedData(path, plan.primaryType, compatibility)
	sessionActive := false
	defer func() {
		if err != nil && sessionActive && !errors.Is(err, ErrTrezorDeviceFailure) {
			device.cancelTypedDataSession()
		}
	}()
	if err = device.writeMessage(ctx, trezorMessageEthereumSignTypedData, request); err != nil {
		return nil, err
	}
	sessionActive = true

	requestedStructs := make(map[string]struct{}, len(plan.reachable))
	requestedValues := make(map[string]struct{}, len(plan.encodedValues))
	valuePhase := false
	buttonRequests := 0
	responseBudget := len(plan.reachable) + len(plan.encodedValues) + trezorTypedDataMaxButtonRequests + 1

	for responses := 0; responses < responseBudget; responses++ {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		var responseType int
		var payload []byte
		responseType, payload, err = device.readMessage(ctx)
		if err != nil {
			return nil, err
		}

		switch responseType {
		case trezorMessageButtonRequest:
			buttonRequests++
			if buttonRequests > trezorTypedDataMaxButtonRequests {
				return nil, fmt.Errorf("%w: button request budget", ErrTrezorTypedDataProtocol)
			}
			if device.buttonHandler == nil {
				return nil, ErrTrezorInteractionRequired
			}
			if err = device.writeMessage(ctx, trezorMessageButtonAck, nil); err != nil {
				return nil, err
			}
			if err = device.buttonHandler(ctx); err != nil {
				return nil, fmt.Errorf("trezor signer: device confirmation: %w", err)
			}

		case trezorMessageFailure:
			sessionActive = false
			return nil, ErrTrezorDeviceFailure

		case trezorMessagePinMatrixRequest, trezorMessagePassphraseRequest:
			return nil, ErrTrezorLocked

		case trezorMessageEthereumTypedDataStructRequest:
			if valuePhase {
				return nil, fmt.Errorf("%w: struct request after value request", ErrTrezorTypedDataProtocol)
			}
			name, parseErr := parseTrezorTypedDataStructRequest(payload)
			if parseErr != nil {
				return nil, parseErr
			}
			ack, known := plan.structAcks[name]
			if _, reachable := plan.reachable[name]; !known || !reachable {
				return nil, fmt.Errorf("%w: unknown struct request", ErrTrezorTypedDataProtocol)
			}
			if _, duplicate := requestedStructs[name]; duplicate {
				return nil, fmt.Errorf("%w: duplicate struct request", ErrTrezorTypedDataProtocol)
			}
			requestedStructs[name] = struct{}{}
			if len(requestedStructs) == len(plan.reachable) && len(plan.encodedValues) == 0 && device.buttonHandler == nil {
				var response []byte
				response, err = device.call(ctx, trezorMessageEthereumTypedDataStructAck, trezorMessageEthereumTypedDataSignature, ack)
				if err != nil {
					if errors.Is(err, ErrTrezorInteractionRequired) || errors.Is(err, ErrTrezorLocked) || errors.Is(err, ErrTrezorDeviceFailure) {
						sessionActive = false
					}
					return nil, fmt.Errorf("trezor signer: typed-data final response: %w", err)
				}
				sessionActive = false
				return parseSignatureField(response, 1)
			}
			if err = device.writeMessage(ctx, trezorMessageEthereumTypedDataStructAck, ack); err != nil {
				return nil, err
			}

		case trezorMessageEthereumTypedDataValueRequest:
			valuePhase = true
			if len(requestedStructs) != len(plan.reachable) {
				return nil, fmt.Errorf("%w: value requested before all types", ErrTrezorTypedDataProtocol)
			}
			memberPath, parseErr := parseTrezorTypedDataValueRequest(payload)
			if parseErr != nil {
				return nil, parseErr
			}
			pathKey := trezorTypedDataPathKey(memberPath)
			value, known := plan.encodedValues[pathKey]
			if !known {
				return nil, fmt.Errorf("%w: invalid member path", ErrTrezorTypedDataProtocol)
			}
			if _, duplicate := requestedValues[pathKey]; duplicate {
				return nil, fmt.Errorf("%w: duplicate member path", ErrTrezorTypedDataProtocol)
			}
			requestedValues[pathKey] = struct{}{}
			ack := appendBytesField(nil, 1, value)
			if len(requestedValues) == len(plan.encodedValues) && device.buttonHandler == nil {
				var response []byte
				response, err = device.call(ctx, trezorMessageEthereumTypedDataValueAck, trezorMessageEthereumTypedDataSignature, ack)
				if err != nil {
					if errors.Is(err, ErrTrezorInteractionRequired) || errors.Is(err, ErrTrezorLocked) || errors.Is(err, ErrTrezorDeviceFailure) {
						sessionActive = false
					}
					return nil, fmt.Errorf("trezor signer: typed-data final response: %w", err)
				}
				sessionActive = false
				return parseSignatureField(response, 1)
			}
			if err = device.writeMessage(ctx, trezorMessageEthereumTypedDataValueAck, ack); err != nil {
				return nil, err
			}

		case trezorMessageEthereumTypedDataSignature:
			if len(requestedStructs) != len(plan.reachable) || len(requestedValues) != len(plan.encodedValues) {
				return nil, fmt.Errorf("%w: premature signature", ErrTrezorTypedDataProtocol)
			}
			sessionActive = false
			return parseSignatureField(payload, 1)

		default:
			return nil, fmt.Errorf("%w: unexpected response type %d", ErrTrezorTypedDataProtocol, responseType)
		}
	}

	return nil, fmt.Errorf("%w: response budget", ErrTrezorTypedDataProtocol)
}

func (device *UDPDevice) cancelTypedDataSession() {
	_ = device.cancelAndDrain(context.Background())
}

func encodeTrezorSignTypedData(path []uint32, primaryType string, metamaskV4Compat bool) []byte {
	request := encodeAddressPath(path)
	request = appendBytesField(request, 2, []byte(primaryType))
	request = appendVarint(request, 3<<3)
	if metamaskV4Compat {
		return appendVarint(request, 1)
	}
	return appendVarint(request, 0)
}

func prepareTrezorTypedData(input any) (*trezorTypedDataPlan, error) {
	document, err := decodeTrezorTypedDataInput(input)
	if err != nil {
		return nil, err
	}
	schema, reachable, err := buildTrezorTypedDataSchema(document)
	if err != nil {
		return nil, err
	}
	plan := &trezorTypedDataPlan{
		primaryType:   document.primaryType,
		structAcks:    make(map[string][]byte, len(reachable)),
		reachable:     reachable,
		encodedValues: make(map[string][]byte),
	}
	for name := range reachable {
		plan.structAcks[name] = encodeTrezorTypedDataStructAck(schema.members[name])
	}
	builder := &trezorTypedDataPlanBuilder{plan: plan, schema: schema}
	if err := builder.addStruct("EIP712Domain", document.domain, []uint32{0}, 1); err != nil {
		return nil, err
	}
	if document.primaryType == "EIP712Domain" {
		if len(document.message) != 0 {
			return nil, fmt.Errorf("%w: message must be empty for EIP712Domain primary type", ErrTrezorTypedDataInvalid)
		}
		return plan, nil
	}
	if err := builder.addStruct(document.primaryType, document.message, []uint32{1}, 1); err != nil {
		return nil, err
	}
	return plan, nil
}

func decodeTrezorTypedDataInput(input any) (*trezorTypedDataDocument, error) {
	switch typed := input.(type) {
	case apitypes.TypedData:
		return documentFromAPITypedData(&typed)
	case *apitypes.TypedData:
		if typed == nil {
			return nil, fmt.Errorf("%w: nil typed data", ErrTrezorTypedDataInvalid)
		}
		return documentFromAPITypedData(typed)
	case []byte:
		return documentFromTrezorTypedDataJSON(typed)
	case json.RawMessage:
		return documentFromTrezorTypedDataJSON([]byte(typed))
	case string:
		return documentFromTrezorTypedDataJSON([]byte(typed))
	default:
		return nil, fmt.Errorf("%w: unsupported input type", ErrTrezorTypedDataInvalid)
	}
}

func documentFromAPITypedData(typedData *apitypes.TypedData) (*trezorTypedDataDocument, error) {
	typesCopy := make(apitypes.Types, len(typedData.Types))
	for name, members := range typedData.Types {
		typesCopy[name] = append([]apitypes.Type(nil), members...)
	}
	domain := make(map[string]any, 5)
	if typedData.Domain.Name != "" {
		domain["name"] = typedData.Domain.Name
	}
	if typedData.Domain.Version != "" {
		domain["version"] = typedData.Domain.Version
	}
	if typedData.Domain.ChainId != nil {
		domain["chainId"] = new(big.Int).Set((*big.Int)(typedData.Domain.ChainId))
	}
	if typedData.Domain.VerifyingContract != "" {
		domain["verifyingContract"] = typedData.Domain.VerifyingContract
	}
	if typedData.Domain.Salt != "" {
		domain["salt"] = typedData.Domain.Salt
	}
	for _, member := range typesCopy["EIP712Domain"] {
		switch member.Name {
		case "name":
			domain[member.Name] = typedData.Domain.Name
		case "version":
			domain[member.Name] = typedData.Domain.Version
		case "chainId":
			if typedData.Domain.ChainId == nil {
				return nil, fmt.Errorf("%w: missing domain chainId", ErrTrezorTypedDataInvalid)
			}
			domain[member.Name] = new(big.Int).Set((*big.Int)(typedData.Domain.ChainId))
		case "verifyingContract":
			domain[member.Name] = typedData.Domain.VerifyingContract
		case "salt":
			domain[member.Name] = typedData.Domain.Salt
		default:
			return nil, fmt.Errorf("%w: apitypes domain extension is unavailable", ErrTrezorTypedDataInvalid)
		}
	}
	message := map[string]any(typedData.Message)
	if message == nil {
		message = make(map[string]any)
	}
	return &trezorTypedDataDocument{
		types:       typesCopy,
		primaryType: typedData.PrimaryType,
		domain:      domain,
		message:     message,
	}, nil
}

func documentFromTrezorTypedDataJSON(data []byte) (*trezorTypedDataDocument, error) {
	if len(data) == 0 || len(data) > trezorTypedDataMaxJSONBytes || !utf8.Valid(data) {
		return nil, fmt.Errorf("%w: JSON size or encoding", ErrTrezorTypedDataInvalid)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	nodes := 0
	decoded, err := decodeTrezorStrictJSONValue(decoder, 1, &nodes)
	if err != nil {
		return nil, err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%w: trailing JSON data", ErrTrezorTypedDataInvalid)
	}
	canonical, err := json.Marshal(decoded)
	if err != nil || len(canonical) > trezorTypedDataMaxJSONBytes {
		return nil, fmt.Errorf("%w: canonical JSON", ErrTrezorTypedDataInvalid)
	}
	root, ok := decoded.(map[string]any)
	if !ok || len(root) != 4 {
		return nil, fmt.Errorf("%w: EIP-712 root object", ErrTrezorTypedDataInvalid)
	}
	for _, key := range []string{"types", "primaryType", "domain", "message"} {
		if _, present := root[key]; !present {
			return nil, fmt.Errorf("%w: missing root field", ErrTrezorTypedDataInvalid)
		}
	}
	types, err := decodeTrezorTypedDataTypes(root["types"])
	if err != nil {
		return nil, err
	}
	primaryType, ok := root["primaryType"].(string)
	if !ok {
		return nil, fmt.Errorf("%w: primaryType", ErrTrezorTypedDataInvalid)
	}
	domain, ok := root["domain"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%w: domain object", ErrTrezorTypedDataInvalid)
	}
	message, ok := root["message"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%w: message object", ErrTrezorTypedDataInvalid)
	}
	return &trezorTypedDataDocument{types: types, primaryType: primaryType, domain: domain, message: message}, nil
}

func decodeTrezorStrictJSONValue(decoder *json.Decoder, depth int, nodes *int) (any, error) {
	if depth > trezorTypedDataMaxDepth {
		return nil, fmt.Errorf("%w: JSON depth", ErrTrezorTypedDataInvalid)
	}
	(*nodes)++
	if *nodes > trezorTypedDataMaxJSONNodes {
		return nil, fmt.Errorf("%w: JSON node budget", ErrTrezorTypedDataInvalid)
	}
	token, err := decoder.Token()
	if err != nil {
		return nil, fmt.Errorf("%w: JSON syntax", ErrTrezorTypedDataInvalid)
	}
	delimiter, isDelimiter := token.(json.Delim)
	if !isDelimiter {
		return token, nil
	}
	switch delimiter {
	case '{':
		object := make(map[string]any)
		for decoder.More() {
			keyToken, keyErr := decoder.Token()
			if keyErr != nil {
				return nil, fmt.Errorf("%w: JSON object key", ErrTrezorTypedDataInvalid)
			}
			key, ok := keyToken.(string)
			if !ok {
				return nil, fmt.Errorf("%w: JSON object key", ErrTrezorTypedDataInvalid)
			}
			if _, duplicate := object[key]; duplicate {
				return nil, fmt.Errorf("%w: duplicate JSON key", ErrTrezorTypedDataInvalid)
			}
			value, valueErr := decodeTrezorStrictJSONValue(decoder, depth+1, nodes)
			if valueErr != nil {
				return nil, valueErr
			}
			object[key] = value
		}
		closing, closeErr := decoder.Token()
		if closeErr != nil || closing != json.Delim('}') {
			return nil, fmt.Errorf("%w: JSON object", ErrTrezorTypedDataInvalid)
		}
		return object, nil
	case '[':
		array := make([]any, 0)
		for decoder.More() {
			if len(array) >= trezorTypedDataMaxArrayLength {
				return nil, fmt.Errorf("%w: JSON array length", ErrTrezorTypedDataInvalid)
			}
			value, valueErr := decodeTrezorStrictJSONValue(decoder, depth+1, nodes)
			if valueErr != nil {
				return nil, valueErr
			}
			array = append(array, value)
		}
		closing, closeErr := decoder.Token()
		if closeErr != nil || closing != json.Delim(']') {
			return nil, fmt.Errorf("%w: JSON array", ErrTrezorTypedDataInvalid)
		}
		return array, nil
	default:
		return nil, fmt.Errorf("%w: JSON delimiter", ErrTrezorTypedDataInvalid)
	}
}

func decodeTrezorTypedDataTypes(value any) (apitypes.Types, error) {
	typeObject, ok := value.(map[string]any)
	if !ok || len(typeObject) == 0 || len(typeObject) > trezorTypedDataMaxTypes {
		return nil, fmt.Errorf("%w: type budget", ErrTrezorTypedDataInvalid)
	}
	types := make(apitypes.Types, len(typeObject))
	for name, rawMembers := range typeObject {
		memberArray, ok := rawMembers.([]any)
		if !ok || len(memberArray) > trezorTypedDataMaxMembers {
			return nil, fmt.Errorf("%w: member list", ErrTrezorTypedDataInvalid)
		}
		members := make([]apitypes.Type, 0, len(memberArray))
		for _, rawMember := range memberArray {
			memberObject, ok := rawMember.(map[string]any)
			if !ok || len(memberObject) != 2 {
				return nil, fmt.Errorf("%w: member object", ErrTrezorTypedDataInvalid)
			}
			memberName, nameOK := memberObject["name"].(string)
			memberType, typeOK := memberObject["type"].(string)
			if !nameOK || !typeOK {
				return nil, fmt.Errorf("%w: member identity", ErrTrezorTypedDataInvalid)
			}
			members = append(members, apitypes.Type{Name: memberName, Type: memberType})
		}
		types[name] = members
	}
	return types, nil
}

func buildTrezorTypedDataSchema(document *trezorTypedDataDocument) (*trezorTypedDataSchema, map[string]struct{}, error) {
	if document == nil || len(document.types) == 0 || len(document.types) > trezorTypedDataMaxTypes {
		return nil, nil, fmt.Errorf("%w: type budget", ErrTrezorTypedDataInvalid)
	}
	if !isTrezorTypedDataIdentifier(document.primaryType) {
		return nil, nil, fmt.Errorf("%w: primary type", ErrTrezorTypedDataInvalid)
	}
	if _, exists := document.types["EIP712Domain"]; !exists {
		return nil, nil, fmt.Errorf("%w: EIP712Domain type", ErrTrezorTypedDataInvalid)
	}
	if _, exists := document.types[document.primaryType]; !exists {
		return nil, nil, fmt.Errorf("%w: primary type definition", ErrTrezorTypedDataInvalid)
	}
	for name := range document.types {
		if !isTrezorTypedDataIdentifier(name) {
			return nil, nil, fmt.Errorf("%w: type name", ErrTrezorTypedDataInvalid)
		}
	}

	schema := &trezorTypedDataSchema{members: make(map[string][]trezorTypedDataMember, len(document.types))}
	totalMembers := 0
	for name, definitions := range document.types {
		if len(definitions) > trezorTypedDataMaxMembers {
			return nil, nil, fmt.Errorf("%w: members per type", ErrTrezorTypedDataInvalid)
		}
		totalMembers += len(definitions)
		if totalMembers > trezorTypedDataMaxTotalMembers {
			return nil, nil, fmt.Errorf("%w: total member budget", ErrTrezorTypedDataInvalid)
		}
		seen := make(map[string]struct{}, len(definitions))
		members := make([]trezorTypedDataMember, 0, len(definitions))
		for _, definition := range definitions {
			if !isTrezorTypedDataIdentifier(definition.Name) {
				return nil, nil, fmt.Errorf("%w: member name", ErrTrezorTypedDataInvalid)
			}
			if _, duplicate := seen[definition.Name]; duplicate {
				return nil, nil, fmt.Errorf("%w: duplicate member", ErrTrezorTypedDataInvalid)
			}
			seen[definition.Name] = struct{}{}
			fieldType, err := parseTrezorTypedDataFieldType(definition.Type, document.types)
			if err != nil {
				return nil, nil, err
			}
			if name == "EIP712Domain" && (fieldType.dataType == trezorEthereumDataTypeArray || fieldType.dataType == trezorEthereumDataTypeStruct) {
				return nil, nil, fmt.Errorf("%w: composite EIP712Domain member", ErrTrezorTypedDataInvalid)
			}
			members = append(members, trezorTypedDataMember{name: definition.Name, fieldType: fieldType})
		}
		schema.members[name] = members
	}
	if err := validateTrezorTypedDataTypeGraph(schema); err != nil {
		return nil, nil, err
	}
	reachable := make(map[string]struct{})
	collectTrezorTypedDataTypes(schema, "EIP712Domain", reachable)
	collectTrezorTypedDataTypes(schema, document.primaryType, reachable)
	return schema, reachable, nil
}

func isTrezorTypedDataIdentifier(value string) bool {
	if value == "" || len(value) > trezorTypedDataMaxIdentifierBytes {
		return false
	}
	for index := 0; index < len(value); index++ {
		character := value[index]
		if index == 0 {
			if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') || character == '_' || character == '$' {
				continue
			}
			return false
		}
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') || character == '_' || character == '$' {
			continue
		}
		return false
	}
	return true
}

func parseTrezorTypedDataFieldType(typeName string, types apitypes.Types) (*trezorTypedDataFieldType, error) {
	if typeName == "" || len(typeName) > trezorTypedDataMaxIdentifierBytes+8 {
		return nil, fmt.Errorf("%w: field type", ErrTrezorTypedDataInvalid)
	}
	if strings.HasSuffix(typeName, "]") {
		arrayStart := strings.LastIndexByte(typeName, '[')
		if arrayStart <= 0 || strings.ContainsAny(typeName[:arrayStart], "[]") {
			return nil, fmt.Errorf("%w: nested or malformed array", ErrTrezorTypedDataInvalid)
		}
		entryType, err := parseTrezorTypedDataNonArrayType(typeName[:arrayStart], types)
		if err != nil {
			return nil, err
		}
		lengthText := typeName[arrayStart+1 : len(typeName)-1]
		var size *uint32
		if lengthText != "" {
			if len(lengthText) > 1 && lengthText[0] == '0' {
				return nil, fmt.Errorf("%w: fixed array length", ErrTrezorTypedDataInvalid)
			}
			length, parseErr := strconv.ParseUint(lengthText, 10, 16)
			if parseErr != nil || length == 0 || length > trezorTypedDataMaxArrayLength {
				return nil, fmt.Errorf("%w: fixed array length", ErrTrezorTypedDataInvalid)
			}
			converted := uint32(length)
			size = &converted
		}
		return &trezorTypedDataFieldType{dataType: trezorEthereumDataTypeArray, size: size, entryType: entryType}, nil
	}
	if strings.ContainsAny(typeName, "[]") {
		return nil, fmt.Errorf("%w: malformed array", ErrTrezorTypedDataInvalid)
	}
	return parseTrezorTypedDataNonArrayType(typeName, types)
}

func parseTrezorTypedDataNonArrayType(typeName string, types apitypes.Types) (*trezorTypedDataFieldType, error) {
	switch typeName {
	case "bytes":
		return &trezorTypedDataFieldType{dataType: trezorEthereumDataTypeBytes}, nil
	case "string":
		return &trezorTypedDataFieldType{dataType: trezorEthereumDataTypeString}, nil
	case "bool":
		return &trezorTypedDataFieldType{dataType: trezorEthereumDataTypeBool}, nil
	case "address":
		return &trezorTypedDataFieldType{dataType: trezorEthereumDataTypeAddress}, nil
	}
	if strings.HasPrefix(typeName, "bytes") {
		size, ok := parseTrezorTypedDataBitSize(strings.TrimPrefix(typeName, "bytes"), 1, 32, 1)
		if !ok {
			return nil, fmt.Errorf("%w: bytes size", ErrTrezorTypedDataInvalid)
		}
		converted := uint32(size)
		return &trezorTypedDataFieldType{dataType: trezorEthereumDataTypeBytes, size: &converted}, nil
	}
	if strings.HasPrefix(typeName, "uint") {
		bits, ok := parseTrezorTypedDataBitSize(strings.TrimPrefix(typeName, "uint"), 8, 256, 8)
		if !ok {
			return nil, fmt.Errorf("%w: uint size", ErrTrezorTypedDataInvalid)
		}
		converted := uint32(bits / 8)
		return &trezorTypedDataFieldType{dataType: trezorEthereumDataTypeUint, size: &converted}, nil
	}
	if strings.HasPrefix(typeName, "int") {
		bits, ok := parseTrezorTypedDataBitSize(strings.TrimPrefix(typeName, "int"), 8, 256, 8)
		if !ok {
			return nil, fmt.Errorf("%w: int size", ErrTrezorTypedDataInvalid)
		}
		converted := uint32(bits / 8)
		return &trezorTypedDataFieldType{dataType: trezorEthereumDataTypeInt, size: &converted}, nil
	}
	definitions, exists := types[typeName]
	if !exists || !isTrezorTypedDataIdentifier(typeName) {
		return nil, fmt.Errorf("%w: unsupported field type", ErrTrezorTypedDataInvalid)
	}
	size := uint32(len(definitions))
	return &trezorTypedDataFieldType{dataType: trezorEthereumDataTypeStruct, size: &size, structName: typeName}, nil
}

func parseTrezorTypedDataBitSize(text string, minimum, maximum, multiple int) (int, bool) {
	if text == "" || (len(text) > 1 && text[0] == '0') {
		return 0, false
	}
	value, err := strconv.Atoi(text)
	if err != nil || value < minimum || value > maximum || value%multiple != 0 {
		return 0, false
	}
	return value, true
}

func validateTrezorTypedDataTypeGraph(schema *trezorTypedDataSchema) error {
	state := make(map[string]uint8, len(schema.members))
	var detectCycle func(string) error
	detectCycle = func(name string) error {
		switch state[name] {
		case 1:
			return fmt.Errorf("%w: cyclic type graph", ErrTrezorTypedDataInvalid)
		case 2:
			return nil
		}
		state[name] = 1
		for _, member := range schema.members[name] {
			if dependency := trezorTypedDataStructDependency(member.fieldType); dependency != "" {
				if err := detectCycle(dependency); err != nil {
					return err
				}
			}
		}
		state[name] = 2
		return nil
	}
	for name := range schema.members {
		if err := detectCycle(name); err != nil {
			return err
		}
	}

	depthMemo := make(map[string]int, len(schema.members))
	var graphDepth func(string) int
	graphDepth = func(name string) int {
		if depth := depthMemo[name]; depth != 0 {
			return depth
		}
		maximum := 1
		for _, member := range schema.members[name] {
			if dependency := trezorTypedDataStructDependency(member.fieldType); dependency != "" {
				if depth := 1 + graphDepth(dependency); depth > maximum {
					maximum = depth
				}
			}
		}
		depthMemo[name] = maximum
		return maximum
	}
	for name := range schema.members {
		if graphDepth(name) > trezorTypedDataMaxDepth {
			return fmt.Errorf("%w: type depth", ErrTrezorTypedDataInvalid)
		}
	}
	return nil
}

func trezorTypedDataStructDependency(fieldType *trezorTypedDataFieldType) string {
	if fieldType == nil {
		return ""
	}
	if fieldType.dataType == trezorEthereumDataTypeArray {
		fieldType = fieldType.entryType
	}
	if fieldType != nil && fieldType.dataType == trezorEthereumDataTypeStruct {
		return fieldType.structName
	}
	return ""
}

func collectTrezorTypedDataTypes(schema *trezorTypedDataSchema, name string, collected map[string]struct{}) {
	if _, exists := collected[name]; exists {
		return
	}
	collected[name] = struct{}{}
	for _, member := range schema.members[name] {
		if dependency := trezorTypedDataStructDependency(member.fieldType); dependency != "" {
			collectTrezorTypedDataTypes(schema, dependency, collected)
		}
	}
}

func encodeTrezorTypedDataStructAck(members []trezorTypedDataMember) []byte {
	var ack []byte
	for _, member := range members {
		var encodedMember []byte
		encodedMember = appendBytesField(encodedMember, 1, encodeTrezorTypedDataFieldType(member.fieldType))
		encodedMember = appendBytesField(encodedMember, 2, []byte(member.name))
		ack = appendBytesField(ack, 1, encodedMember)
	}
	return ack
}

func encodeTrezorTypedDataFieldType(fieldType *trezorTypedDataFieldType) []byte {
	var encoded []byte
	encoded = appendVarint(encoded, 1<<3)
	encoded = appendVarint(encoded, uint64(fieldType.dataType))
	if fieldType.size != nil {
		encoded = appendVarint(encoded, 2<<3)
		encoded = appendVarint(encoded, uint64(*fieldType.size))
	}
	if fieldType.entryType != nil {
		encoded = appendBytesField(encoded, 3, encodeTrezorTypedDataFieldType(fieldType.entryType))
	}
	if fieldType.structName != "" {
		encoded = appendBytesField(encoded, 4, []byte(fieldType.structName))
	}
	return encoded
}

func (builder *trezorTypedDataPlanBuilder) addStruct(typeName string, value any, path []uint32, depth int) error {
	if depth > trezorTypedDataMaxDepth {
		return fmt.Errorf("%w: value depth", ErrTrezorTypedDataInvalid)
	}
	builder.nodeCount++
	if builder.nodeCount > trezorTypedDataMaxJSONNodes {
		return fmt.Errorf("%w: value node budget", ErrTrezorTypedDataInvalid)
	}
	object, ok := trezorTypedDataStringMap(value)
	if !ok {
		return fmt.Errorf("%w: struct value", ErrTrezorTypedDataInvalid)
	}
	members, exists := builder.schema.members[typeName]
	if !exists || len(object) != len(members) {
		return fmt.Errorf("%w: struct fields", ErrTrezorTypedDataInvalid)
	}
	memberNames := make(map[string]struct{}, len(members))
	for _, member := range members {
		memberNames[member.name] = struct{}{}
	}
	for name := range object {
		if _, exists := memberNames[name]; !exists {
			return fmt.Errorf("%w: extra struct field", ErrTrezorTypedDataInvalid)
		}
	}
	for index, member := range members {
		memberValue, present := object[member.name]
		if !present {
			return fmt.Errorf("%w: missing struct field", ErrTrezorTypedDataInvalid)
		}
		memberPath := trezorTypedDataAppendPath(path, uint32(index))
		fieldType := member.fieldType
		switch fieldType.dataType {
		case trezorEthereumDataTypeStruct:
			if err := builder.addStruct(fieldType.structName, memberValue, memberPath, depth+1); err != nil {
				return err
			}
		case trezorEthereumDataTypeArray:
			if err := builder.addArray(fieldType, memberValue, memberPath, depth+1); err != nil {
				return err
			}
		default:
			encoded, err := encodeTrezorTypedDataAtomic(fieldType, memberValue)
			if err != nil {
				return err
			}
			if err := builder.addValue(memberPath, encoded); err != nil {
				return err
			}
		}
	}
	return nil
}

func (builder *trezorTypedDataPlanBuilder) addArray(fieldType *trezorTypedDataFieldType, value any, path []uint32, depth int) error {
	if depth > trezorTypedDataMaxDepth || fieldType.entryType == nil || fieldType.entryType.dataType == trezorEthereumDataTypeArray {
		return fmt.Errorf("%w: array depth or entry type", ErrTrezorTypedDataInvalid)
	}
	array, ok := trezorTypedDataArray(value)
	if !ok || array.Len() > trezorTypedDataMaxArrayLength {
		return fmt.Errorf("%w: array value", ErrTrezorTypedDataInvalid)
	}
	if fieldType.size != nil && array.Len() != int(*fieldType.size) {
		return fmt.Errorf("%w: fixed array length", ErrTrezorTypedDataInvalid)
	}
	if fieldType.size == nil {
		var encodedLength [2]byte
		binary.BigEndian.PutUint16(encodedLength[:], uint16(array.Len()))
		if err := builder.addValue(path, encodedLength[:]); err != nil {
			return err
		}
	}
	for index := 0; index < array.Len(); index++ {
		builder.nodeCount++
		if builder.nodeCount > trezorTypedDataMaxJSONNodes {
			return fmt.Errorf("%w: value node budget", ErrTrezorTypedDataInvalid)
		}
		entryValue := array.Index(index).Interface()
		entryPath := trezorTypedDataAppendPath(path, uint32(index))
		if fieldType.entryType.dataType == trezorEthereumDataTypeStruct {
			if err := builder.addStruct(fieldType.entryType.structName, entryValue, entryPath, depth+1); err != nil {
				return err
			}
			continue
		}
		encoded, err := encodeTrezorTypedDataAtomic(fieldType.entryType, entryValue)
		if err != nil {
			return err
		}
		if err := builder.addValue(entryPath, encoded); err != nil {
			return err
		}
	}
	return nil
}

func (builder *trezorTypedDataPlanBuilder) addValue(path []uint32, encoded []byte) error {
	if len(path) == 0 || len(path) > trezorTypedDataMaxDepth+1 {
		return fmt.Errorf("%w: member path depth", ErrTrezorTypedDataInvalid)
	}
	builder.valueCount++
	builder.encodedBytes += len(encoded)
	if builder.valueCount > trezorTypedDataMaxValueRequests || builder.encodedBytes > trezorTypedDataMaxEncodedBytes {
		return fmt.Errorf("%w: encoded value budget", ErrTrezorTypedDataInvalid)
	}
	key := trezorTypedDataPathKey(path)
	if _, duplicate := builder.plan.encodedValues[key]; duplicate {
		return fmt.Errorf("%w: duplicate value path", ErrTrezorTypedDataInvalid)
	}
	builder.plan.encodedValues[key] = append([]byte(nil), encoded...)
	return nil
}

func trezorTypedDataStringMap(value any) (map[string]any, bool) {
	if value == nil {
		return nil, false
	}
	if object, ok := value.(map[string]any); ok {
		return object, object != nil
	}
	reflected := reflect.ValueOf(value)
	if reflected.Kind() != reflect.Map || reflected.Type().Key().Kind() != reflect.String || reflected.IsNil() {
		return nil, false
	}
	object := make(map[string]any, reflected.Len())
	iterator := reflected.MapRange()
	for iterator.Next() {
		object[iterator.Key().String()] = iterator.Value().Interface()
	}
	return object, true
}

func trezorTypedDataArray(value any) (reflect.Value, bool) {
	if value == nil {
		return reflect.Value{}, false
	}
	array := reflect.ValueOf(value)
	if array.Kind() != reflect.Array && array.Kind() != reflect.Slice {
		return reflect.Value{}, false
	}
	if array.Kind() == reflect.Slice && array.IsNil() {
		return reflect.Value{}, false
	}
	return array, true
}

func trezorTypedDataAppendPath(path []uint32, value uint32) []uint32 {
	appended := make([]uint32, len(path)+1)
	copy(appended, path)
	appended[len(path)] = value
	return appended
}

func trezorTypedDataPathKey(path []uint32) string {
	encoded := make([]byte, len(path)*4)
	for index, component := range path {
		binary.BigEndian.PutUint32(encoded[index*4:], component)
	}
	return string(encoded)
}

func encodeTrezorTypedDataAtomic(fieldType *trezorTypedDataFieldType, value any) ([]byte, error) {
	switch fieldType.dataType {
	case trezorEthereumDataTypeUint:
		return encodeTrezorTypedDataInteger(value, int(*fieldType.size), false)
	case trezorEthereumDataTypeInt:
		return encodeTrezorTypedDataInteger(value, int(*fieldType.size), true)
	case trezorEthereumDataTypeBytes:
		encoded, err := decodeTrezorTypedDataBytes(value)
		if err != nil {
			return nil, err
		}
		if fieldType.size != nil && len(encoded) != int(*fieldType.size) {
			return nil, fmt.Errorf("%w: fixed bytes length", ErrTrezorTypedDataInvalid)
		}
		if fieldType.size == nil && len(encoded) > trezorTypedDataMaxDynamicBytes {
			return nil, fmt.Errorf("%w: dynamic bytes length", ErrTrezorTypedDataInvalid)
		}
		return encoded, nil
	case trezorEthereumDataTypeString:
		text, ok := value.(string)
		if !ok || !utf8.ValidString(text) || len(text) > trezorTypedDataMaxDynamicBytes {
			return nil, fmt.Errorf("%w: string value", ErrTrezorTypedDataInvalid)
		}
		return []byte(text), nil
	case trezorEthereumDataTypeBool:
		boolean, ok := value.(bool)
		if !ok {
			return nil, fmt.Errorf("%w: bool value", ErrTrezorTypedDataInvalid)
		}
		if boolean {
			return []byte{1}, nil
		}
		return []byte{0}, nil
	case trezorEthereumDataTypeAddress:
		address, err := decodeTrezorTypedDataAddress(value)
		if err != nil {
			return nil, err
		}
		return address, nil
	default:
		return nil, fmt.Errorf("%w: non-atomic value", ErrTrezorTypedDataInvalid)
	}
}

func encodeTrezorTypedDataInteger(value any, size int, signed bool) ([]byte, error) {
	integer, err := parseTrezorTypedDataInteger(value)
	if err != nil {
		return nil, err
	}
	bits := uint(size * 8)
	modulus := new(big.Int).Lsh(big.NewInt(1), bits)
	encodedInteger := new(big.Int).Set(integer)
	if signed {
		minimum := new(big.Int).Neg(new(big.Int).Rsh(new(big.Int).Set(modulus), 1))
		maximum := new(big.Int).Sub(new(big.Int).Rsh(new(big.Int).Set(modulus), 1), big.NewInt(1))
		if integer.Cmp(minimum) < 0 || integer.Cmp(maximum) > 0 {
			return nil, fmt.Errorf("%w: signed integer range", ErrTrezorTypedDataInvalid)
		}
		if encodedInteger.Sign() < 0 {
			encodedInteger.Add(encodedInteger, modulus)
		}
	} else if integer.Sign() < 0 || integer.Cmp(modulus) >= 0 {
		return nil, fmt.Errorf("%w: unsigned integer range", ErrTrezorTypedDataInvalid)
	}
	encoded := make([]byte, size)
	encodedInteger.FillBytes(encoded)
	return encoded, nil
}

func parseTrezorTypedDataInteger(value any) (*big.Int, error) {
	switch typed := value.(type) {
	case *big.Int:
		if typed != nil {
			return new(big.Int).Set(typed), nil
		}
	case big.Int:
		return new(big.Int).Set(&typed), nil
	case *gethmath.HexOrDecimal256:
		if typed != nil {
			return new(big.Int).Set((*big.Int)(typed)), nil
		}
	case gethmath.HexOrDecimal256:
		converted := big.Int(typed)
		return new(big.Int).Set(&converted), nil
	case json.Number:
		return parseTrezorTypedDataIntegerText(typed.String(), false)
	case string:
		return parseTrezorTypedDataIntegerText(typed, true)
	case int:
		return big.NewInt(int64(typed)), nil
	case int8:
		return big.NewInt(int64(typed)), nil
	case int16:
		return big.NewInt(int64(typed)), nil
	case int32:
		return big.NewInt(int64(typed)), nil
	case int64:
		return big.NewInt(typed), nil
	case uint:
		return new(big.Int).SetUint64(uint64(typed)), nil
	case uint8:
		return new(big.Int).SetUint64(uint64(typed)), nil
	case uint16:
		return new(big.Int).SetUint64(uint64(typed)), nil
	case uint32:
		return new(big.Int).SetUint64(uint64(typed)), nil
	case uint64:
		return new(big.Int).SetUint64(typed), nil
	case float32:
		return parseTrezorTypedDataFloat(float64(typed))
	case float64:
		return parseTrezorTypedDataFloat(typed)
	}
	return nil, fmt.Errorf("%w: integer value", ErrTrezorTypedDataInvalid)
}

func parseTrezorTypedDataIntegerText(text string, allowHex bool) (*big.Int, error) {
	if text == "" || strings.TrimSpace(text) != text {
		return nil, fmt.Errorf("%w: integer text", ErrTrezorTypedDataInvalid)
	}
	sign := ""
	magnitude := text
	if magnitude[0] == '-' || magnitude[0] == '+' {
		sign = magnitude[:1]
		magnitude = magnitude[1:]
	}
	base := 10
	if allowHex && (strings.HasPrefix(magnitude, "0x") || strings.HasPrefix(magnitude, "0X")) {
		base = 16
		magnitude = magnitude[2:]
	}
	if magnitude == "" {
		return nil, fmt.Errorf("%w: integer text", ErrTrezorTypedDataInvalid)
	}
	for _, character := range magnitude {
		valid := character >= '0' && character <= '9'
		if base == 16 {
			valid = valid || (character >= 'a' && character <= 'f') || (character >= 'A' && character <= 'F')
		}
		if !valid {
			return nil, fmt.Errorf("%w: integer text", ErrTrezorTypedDataInvalid)
		}
	}
	integer, ok := new(big.Int).SetString(sign+magnitude, base)
	if !ok {
		return nil, fmt.Errorf("%w: integer text", ErrTrezorTypedDataInvalid)
	}
	return integer, nil
}

func parseTrezorTypedDataFloat(value float64) (*big.Int, error) {
	if math.IsNaN(value) || math.IsInf(value, 0) || math.Trunc(value) != value {
		return nil, fmt.Errorf("%w: integer float", ErrTrezorTypedDataInvalid)
	}
	integer, accuracy := new(big.Float).SetFloat64(value).Int(nil)
	if accuracy != big.Exact {
		return nil, fmt.Errorf("%w: integer float", ErrTrezorTypedDataInvalid)
	}
	return integer, nil
}

func decodeTrezorTypedDataBytes(value any) ([]byte, error) {
	if text, ok := value.(string); ok {
		if strings.HasPrefix(text, "0x") || strings.HasPrefix(text, "0X") {
			text = text[2:]
		}
		if len(text)%2 != 0 || len(text)/2 > trezorTypedDataMaxDynamicBytes {
			return nil, fmt.Errorf("%w: hex bytes", ErrTrezorTypedDataInvalid)
		}
		decoded, err := hex.DecodeString(text)
		if err != nil {
			return nil, fmt.Errorf("%w: hex bytes", ErrTrezorTypedDataInvalid)
		}
		return decoded, nil
	}
	if value == nil {
		return nil, fmt.Errorf("%w: bytes value", ErrTrezorTypedDataInvalid)
	}
	reflected := reflect.ValueOf(value)
	if (reflected.Kind() != reflect.Array && reflected.Kind() != reflect.Slice) || reflected.Type().Elem().Kind() != reflect.Uint8 {
		return nil, fmt.Errorf("%w: bytes value", ErrTrezorTypedDataInvalid)
	}
	if reflected.Kind() == reflect.Slice && reflected.IsNil() {
		return nil, fmt.Errorf("%w: bytes value", ErrTrezorTypedDataInvalid)
	}
	if reflected.Len() > trezorTypedDataMaxDynamicBytes {
		return nil, fmt.Errorf("%w: bytes length", ErrTrezorTypedDataInvalid)
	}
	decoded := make([]byte, reflected.Len())
	for index := range decoded {
		decoded[index] = byte(reflected.Index(index).Uint())
	}
	return decoded, nil
}

func decodeTrezorTypedDataAddress(value any) ([]byte, error) {
	if address, ok := value.(common.Address); ok {
		return append([]byte(nil), address[:]...), nil
	}
	decoded, err := decodeTrezorTypedDataBytes(value)
	if err != nil || len(decoded) != common.AddressLength {
		return nil, fmt.Errorf("%w: address value", ErrTrezorTypedDataInvalid)
	}
	return decoded, nil
}

func parseTrezorTypedDataStructRequest(payload []byte) (string, error) {
	var name string
	found := false
	for len(payload) > 0 {
		tag, value, rest, err := decodeField(payload)
		if err != nil {
			return "", fmt.Errorf("%w: struct request: %v", ErrTrezorTypedDataProtocol, err)
		}
		payload = rest
		if tag>>3 != 1 {
			continue
		}
		if tag&7 != 2 || found || len(value) == 0 || len(value) > trezorTypedDataMaxIdentifierBytes || !utf8.Valid(value) {
			return "", fmt.Errorf("%w: struct name", ErrTrezorTypedDataProtocol)
		}
		name = string(value)
		found = true
	}
	if !found {
		return "", fmt.Errorf("%w: missing struct name", ErrTrezorTypedDataProtocol)
	}
	return name, nil
}

func parseTrezorTypedDataValueRequest(payload []byte) ([]uint32, error) {
	path := make([]uint32, 0, 4)
	for len(payload) > 0 {
		tag, value, rest, err := decodeField(payload)
		if err != nil {
			return nil, fmt.Errorf("%w: value request: %v", ErrTrezorTypedDataProtocol, err)
		}
		payload = rest
		if tag>>3 != 1 {
			continue
		}
		switch tag & 7 {
		case 0:
			component, size, valid := decodeVarint(value)
			if !valid || component > math.MaxUint32 || size != len(value) || !isCanonicalTrezorTypedDataUint32Varint(component, size) {
				return nil, fmt.Errorf("%w: member path component", ErrTrezorTypedDataProtocol)
			}
			path = append(path, uint32(component))
		case 2:
			for len(value) > 0 {
				component, size, valid := decodeVarint(value)
				if !valid || component > math.MaxUint32 || !isCanonicalTrezorTypedDataUint32Varint(component, size) {
					return nil, fmt.Errorf("%w: packed member path", ErrTrezorTypedDataProtocol)
				}
				path = append(path, uint32(component))
				value = value[size:]
			}
		default:
			return nil, fmt.Errorf("%w: member path wire type", ErrTrezorTypedDataProtocol)
		}
		if len(path) > trezorTypedDataMaxDepth+1 {
			return nil, fmt.Errorf("%w: member path depth", ErrTrezorTypedDataProtocol)
		}
	}
	if len(path) == 0 || (path[0] != 0 && path[0] != 1) {
		return nil, fmt.Errorf("%w: member path root", ErrTrezorTypedDataProtocol)
	}
	return path, nil
}

func isCanonicalTrezorTypedDataUint32Varint(value uint64, size int) bool {
	if value > math.MaxUint32 || size < 1 || size > 5 {
		return false
	}
	expected := 1
	for remaining := value; remaining >= 0x80; remaining >>= 7 {
		expected++
	}
	return size == expected
}
