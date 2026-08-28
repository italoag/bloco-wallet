package evm

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"strings"

	"blocowallet/internal/terminal"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/signer/core/apitypes"
)

const (
	MaxEIP712TypedDataBytes   = 64 << 10
	MaxEIP712Types            = 32
	MaxEIP712FieldsPerType    = 32
	MaxEIP712TotalFields      = 256
	MaxEIP712Depth            = 16
	MaxEIP712ArrayLength      = 64
	MaxEIP712RenderValueBytes = 512
	MaxEIP712RenderArrayItems = 8
	eip712IntentDomain        = "bloco-wallet/eip712/v1"
)

type PrepareEIP712SignRequest struct {
	AccountID string
	Signer    common.Address
	ChainID   uint64
	TypedData []byte
	Origin    string
}

type EIP712Preview struct {
	AccountID         string
	Signer            common.Address
	ChainID           uint64
	PrimaryType       string
	DomainName        string
	DomainVersion     string
	DomainChainID     uint64
	VerifyingContract common.Address
	Digest            common.Hash
	IntentHash        common.Hash
	CanonicalJSON     []byte
	Rendered          string
}

type PreparedEIP712Sign struct {
	accountID     string
	signer        common.Address
	chainID       uint64
	typedData     *apitypes.TypedData
	canonicalJSON []byte
	digest        common.Hash
	intentHash    common.Hash
	rendered      string
}

func PrepareEIP712Sign(request PrepareEIP712SignRequest) (*PreparedEIP712Sign, error) {
	if !accountIDPattern.MatchString(request.AccountID) {
		return nil, invalidIntent("EIP-712 account ID")
	}
	if request.Signer == (common.Address{}) || request.ChainID == 0 || request.ChainID > math.MaxInt64 {
		return nil, invalidIntent("EIP-712 signer chain")
	}
	if len(request.TypedData) == 0 || len(request.TypedData) > MaxEIP712TypedDataBytes {
		return nil, invalidIntent("EIP-712 typed data size")
	}
	if request.Origin == "" || len(request.Origin) > maxMessageOriginBytes || terminal.SanitizeInline(request.Origin, maxMessageOriginBytes) != request.Origin {
		return nil, invalidIntent("EIP-712 origin")
	}
	canonical, err := canonicalEIP712JSON(request.TypedData)
	if err != nil {
		return nil, err
	}
	typedData, err := validateEIP712TypedData(canonical)
	if err != nil {
		return nil, err
	}
	domainChainID, err := eip712DomainChainID(typedData)
	if err != nil {
		return nil, err
	}
	if domainChainID != request.ChainID {
		return nil, &EngineError{Code: ErrorPolicyDenied, Field: "EIP-712 chain binding"}
	}
	verifyingContract, err := eip712VerifyingContract(typedData)
	if err != nil {
		return nil, err
	}
	if verifyingContract == (common.Address{}) {
		return nil, &EngineError{Code: ErrorPolicyDenied, Field: "EIP-712 verifying contract"}
	}
	digestBytes, _, err := apitypes.TypedDataAndHash(*typedData)
	if err != nil {
		return nil, &EngineError{Code: ErrorInvalidIntent, Field: "EIP-712 digest", Cause: err}
	}
	var digest common.Hash
	copy(digest[:], digestBytes)
	intentHash := eip712IntentHash(request.AccountID, request.Signer, request.ChainID, request.Origin, digest)
	rendered, err := renderEIP712Message(typedData, digest)
	if err != nil {
		return nil, err
	}
	return &PreparedEIP712Sign{
		accountID: request.AccountID, signer: request.Signer, chainID: request.ChainID,
		typedData: typedData, canonicalJSON: canonical, digest: digest, intentHash: intentHash, rendered: rendered,
	}, nil
}

func (prepared *PreparedEIP712Sign) Preview() EIP712Preview {
	if prepared == nil || prepared.typedData == nil {
		return EIP712Preview{}
	}
	domainChainID, _ := eip712DomainChainID(prepared.typedData)
	verifyingContract, _ := eip712VerifyingContract(prepared.typedData)
	domainName := domainStringField(prepared.typedData, "name")
	domainVersion := domainStringField(prepared.typedData, "version")
	return EIP712Preview{
		AccountID: prepared.accountID, Signer: prepared.signer, ChainID: prepared.chainID,
		PrimaryType: prepared.typedData.PrimaryType, DomainName: domainName, DomainVersion: domainVersion,
		DomainChainID: domainChainID, VerifyingContract: verifyingContract,
		Digest: prepared.digest, IntentHash: prepared.intentHash,
		CanonicalJSON: append([]byte(nil), prepared.canonicalJSON...), Rendered: prepared.rendered,
	}
}

func canonicalEIP712JSON(data []byte) ([]byte, error) {
	if err := validateEIP712StrictJSON(data); err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var decoded map[string]any
	if err := decoder.Decode(&decoded); err != nil {
		return nil, &EngineError{Code: ErrorInvalidIntent, Field: "EIP-712 strict JSON", Cause: err}
	}
	if err := rejectStrictJSONTrailing(decoder); err != nil {
		return nil, err
	}
	canonical, err := json.Marshal(decoded)
	if err != nil {
		return nil, &EngineError{Code: ErrorInvalidIntent, Field: "EIP-712 canonical JSON", Cause: err}
	}
	if len(canonical) > MaxEIP712TypedDataBytes {
		return nil, invalidIntent("EIP-712 canonical size")
	}
	return canonical, nil
}

func validateEIP712StrictJSON(data []byte) error {
	return validateStrictJSON(data, MaxEIP712TypedDataBytes, MaxEIP712Depth, MaxEIP712ArrayLength)
}

func validateEIP712TypedData(canonical []byte) (*apitypes.TypedData, error) {
	decoder := json.NewDecoder(bytes.NewReader(canonical))
	decoder.UseNumber()
	var typedData apitypes.TypedData
	if err := decoder.Decode(&typedData); err != nil {
		return nil, &EngineError{Code: ErrorInvalidIntent, Field: "EIP-712 typed data", Cause: err}
	}
	if err := rejectStrictJSONTrailing(decoder); err != nil {
		return nil, err
	}
	normalized := normalizeEIP712MessageNumbers(typedData.Message)
	if message, ok := normalized.(map[string]any); ok {
		typedData.Message = apitypes.TypedDataMessage(message)
	}
	if typedData.PrimaryType == "" || len(typedData.Types) == 0 || len(typedData.Types) > MaxEIP712Types {
		return nil, invalidIntent("EIP-712 type graph")
	}
	if _, exists := typedData.Types[typedData.PrimaryType]; !exists {
		return nil, invalidIntent("EIP-712 primary type")
	}
	if _, exists := typedData.Types["EIP712Domain"]; !exists {
		return nil, invalidIntent("EIP-712 domain type")
	}
	totalFields := 0
	for typeName, fields := range typedData.Types {
		if len(fields) == 0 || len(fields) > MaxEIP712FieldsPerType {
			return nil, invalidIntent("EIP-712 field budget")
		}
		totalFields += len(fields)
		if totalFields > MaxEIP712TotalFields {
			return nil, invalidIntent("EIP-712 total field budget")
		}
		seen := make(map[string]struct{}, len(fields))
		for _, field := range fields {
			if field.Name == "" || field.Type == "" {
				return nil, invalidIntent("EIP-712 field identity")
			}
			if _, duplicate := seen[field.Name]; duplicate {
				return nil, invalidIntent("EIP-712 duplicate field")
			}
			seen[field.Name] = struct{}{}
			if err := validateEIP712FieldType(field.Type, typeName, typedData.Types); err != nil {
				return nil, err
			}
		}
	}
	if err := validateEIP712TypeGraph(typedData.PrimaryType, typedData.Types); err != nil {
		return nil, err
	}
	return &typedData, nil
}

func normalizeEIP712MessageNumbers(value any) any {
	switch typed := value.(type) {
	case json.Number:
		integer := new(big.Int)
		if _, ok := integer.SetString(typed.String(), 10); !ok {
			return typed
		}
		return integer
	case map[string]any:
		for key, item := range typed {
			typed[key] = normalizeEIP712MessageNumbers(item)
		}
		return typed
	case []any:
		for index, item := range typed {
			typed[index] = normalizeEIP712MessageNumbers(item)
		}
		return typed
	default:
		return typed
	}
}

func validateEIP712FieldType(fieldType, owner string, types map[string][]apitypes.Type) error {
	baseType := strings.TrimSuffix(fieldType, "[]")
	baseType = strings.TrimSuffix(baseType, "]")
	for {
		trimmed := strings.TrimSuffix(baseType, "[]")
		if trimmed == baseType {
			break
		}
		baseType = trimmed
	}
	if baseType == "" || len(baseType) > 64 {
		return invalidIntent("EIP-712 field type")
	}
	arrayCount := strings.Count(fieldType, "[]")
	if arrayCount > 2 {
		return invalidIntent("EIP-712 array nesting")
	}
	if _, isPrimitive := eip712PrimitiveTypes[baseType]; isPrimitive {
		return nil
	}
	if _, isKnown := types[baseType]; !isKnown || baseType == owner {
		return invalidIntent("EIP-712 type reference")
	}
	return nil
}

var eip712PrimitiveTypes = map[string]struct{}{
	"address": {}, "bool": {}, "string": {}, "bytes": {},
	"uint8": {}, "uint16": {}, "uint24": {}, "uint32": {}, "uint40": {}, "uint48": {}, "uint56": {}, "uint64": {},
	"uint72": {}, "uint80": {}, "uint88": {}, "uint96": {}, "uint104": {}, "uint112": {}, "uint120": {}, "uint128": {},
	"uint136": {}, "uint144": {}, "uint152": {}, "uint160": {}, "uint168": {}, "uint176": {}, "uint184": {}, "uint192": {},
	"uint200": {}, "uint208": {}, "uint216": {}, "uint224": {}, "uint232": {}, "uint240": {}, "uint248": {}, "uint256": {},
	"int8": {}, "int16": {}, "int24": {}, "int32": {}, "int40": {}, "int48": {}, "int56": {}, "int64": {},
	"int72": {}, "int80": {}, "int88": {}, "int96": {}, "int104": {}, "int112": {}, "int120": {}, "int128": {},
	"int136": {}, "int144": {}, "int152": {}, "int160": {}, "int168": {}, "int176": {}, "int184": {}, "int192": {},
	"int200": {}, "int208": {}, "int216": {}, "int224": {}, "int232": {}, "int240": {}, "int248": {}, "int256": {},
	"bytes1": {}, "bytes2": {}, "bytes3": {}, "bytes4": {}, "bytes5": {}, "bytes6": {}, "bytes7": {}, "bytes8": {},
	"bytes9": {}, "bytes10": {}, "bytes11": {}, "bytes12": {}, "bytes13": {}, "bytes14": {}, "bytes15": {}, "bytes16": {},
	"bytes17": {}, "bytes18": {}, "bytes19": {}, "bytes20": {}, "bytes21": {}, "bytes22": {}, "bytes23": {}, "bytes24": {},
	"bytes25": {}, "bytes26": {}, "bytes27": {}, "bytes28": {}, "bytes29": {}, "bytes30": {}, "bytes31": {}, "bytes32": {},
}

func validateEIP712TypeGraph(primaryType string, types map[string][]apitypes.Type) error {
	state := make(map[string]uint8, len(types))
	var visit func(string, int) error
	visit = func(typeName string, currentDepth int) error {
		if currentDepth > MaxEIP712Depth {
			return invalidIntent("EIP-712 type depth")
		}
		switch state[typeName] {
		case 1:
			return invalidIntent("EIP-712 type cycle")
		case 2:
			return nil
		}
		state[typeName] = 1
		for _, field := range types[typeName] {
			baseType := strings.TrimSuffix(field.Type, "[]")
			if _, isPrimitive := eip712PrimitiveTypes[baseType]; isPrimitive {
				continue
			}
			if err := visit(baseType, currentDepth+1); err != nil {
				return err
			}
		}
		state[typeName] = 2
		return nil
	}
	return visit(primaryType, 0)
}

func eip712DomainChainID(typedData *apitypes.TypedData) (uint64, error) {
	if typedData.Domain.ChainId == nil {
		return 0, invalidIntent("EIP-712 domain chain ID")
	}
	integer := (*big.Int)(typedData.Domain.ChainId)
	if integer == nil || !integer.IsUint64() || integer.Uint64() == 0 {
		return 0, invalidIntent("EIP-712 domain chain ID value")
	}
	return integer.Uint64(), nil
}

func eip712VerifyingContract(typedData *apitypes.TypedData) (common.Address, error) {
	text := typedData.Domain.VerifyingContract
	if text == "" {
		return common.Address{}, nil
	}
	mixedcase, err := common.NewMixedcaseAddressFromString(text)
	if err != nil || !common.IsHexAddress(text) || !mixedcase.ValidChecksum() {
		return common.Address{}, invalidIntent("EIP-712 verifying contract")
	}
	return common.HexToAddress(text), nil
}

func domainStringField(typedData *apitypes.TypedData, key string) string {
	var value string
	switch key {
	case "name":
		value = typedData.Domain.Name
	case "version":
		value = typedData.Domain.Version
	default:
		return ""
	}
	if len(value) > 128 {
		return ""
	}
	return value
}

func eip712IntentHash(accountID string, signer common.Address, chainID uint64, origin string, digest common.Hash) common.Hash {
	canonical := make([]byte, 0, len(eip712IntentDomain)+len(accountID)+len(origin)+64)
	canonical = append(canonical, eip712IntentDomain...)
	canonical = append(canonical, 0)
	canonical = append(canonical, accountID...)
	canonical = append(canonical, signer.Bytes()...)
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], chainID)
	canonical = append(canonical, length[:]...)
	binary.BigEndian.PutUint64(length[:], uint64(len(origin)))
	canonical = append(canonical, length[:]...)
	canonical = append(canonical, origin...)
	canonical = append(canonical, digest[:]...)
	return crypto.Keccak256Hash(canonical)
}

func renderEIP712Message(typedData *apitypes.TypedData, digest common.Hash) (string, error) {
	var rendered strings.Builder
	rendered.WriteString("Primary type: " + safeRenderValue(typedData.PrimaryType) + "\n")
	rendered.WriteString("Domain name: " + safeRenderValue(domainStringField(typedData, "name")) + "\n")
	rendered.WriteString("Domain version: " + safeRenderValue(domainStringField(typedData, "version")) + "\n")
	domainChainID, err := eip712DomainChainID(typedData)
	if err != nil {
		return "", err
	}
	_, _ = fmt.Fprintf(&rendered, "Chain ID: %d\n", domainChainID)
	verifyingContract, err := eip712VerifyingContract(typedData)
	if err != nil {
		return "", err
	}
	rendered.WriteString("Verifying contract: " + verifyingContract.Hex() + "\n")
	rendered.WriteString("Message:\n")
	if err := renderEIP712Fields(&rendered, typedData.PrimaryType, typedData.Message, typedData.Types, 1); err != nil {
		return "", err
	}
	rendered.WriteString("\nDigest: " + digest.Hex())
	return rendered.String(), nil
}

func renderEIP712Fields(rendered *strings.Builder, typeName string, message map[string]any, types map[string][]apitypes.Type, depth int) error {
	if depth > MaxEIP712Depth {
		return invalidIntent("EIP-712 render depth")
	}
	fields, exists := types[typeName]
	if !exists {
		return invalidIntent("EIP-712 render type")
	}
	for _, field := range fields {
		fieldName := safeRenderValue(field.Name)
		value, present := message[field.Name]
		if !present {
			rendered.WriteString(strings.Repeat("  ", depth) + fieldName + ": <missing>\n")
			continue
		}
		baseType := strings.TrimSuffix(field.Type, "[]")
		if _, isPrimitive := eip712PrimitiveTypes[baseType]; isPrimitive && !strings.HasSuffix(field.Type, "[]") {
			rendered.WriteString(strings.Repeat("  ", depth) + fieldName + ": " + safeRenderValue(eip712PrimitiveString(value)) + "\n")
			continue
		}
		if strings.HasSuffix(field.Type, "[]") {
			array, ok := value.([]any)
			if !ok || len(array) > MaxEIP712ArrayLength {
				return invalidIntent("EIP-712 render array")
			}
			_, _ = fmt.Fprintf(rendered, "%s%s: array[%d]", strings.Repeat("  ", depth), fieldName, len(array))
			if len(array) > 0 {
				rendered.WriteString(" = [")
				preview := array
				if len(preview) > MaxEIP712RenderArrayItems {
					preview = preview[:MaxEIP712RenderArrayItems]
				}
				for index, item := range preview {
					if index > 0 {
						rendered.WriteString(", ")
					}
					rendered.WriteString(safeRenderValue(eip712PrimitiveString(item)))
				}
				if len(array) > MaxEIP712RenderArrayItems {
					rendered.WriteString(", ...")
				}
				rendered.WriteString("]")
			}
			rendered.WriteString("\n")
			continue
		}
		nested, ok := value.(map[string]any)
		if !ok {
			return invalidIntent("EIP-712 render nested")
		}
		rendered.WriteString(strings.Repeat("  ", depth) + fieldName + ":\n")
		if err := renderEIP712Fields(rendered, baseType, nested, types, depth+1); err != nil {
			return err
		}
	}
	return nil
}

func eip712PrimitiveString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case bool:
		if typed {
			return "true"
		}
		return "false"
	case float64:
		return new(big.Rat).SetFloat64(typed).FloatString(0)
	case json.Number:
		return typed.String()
	case *big.Int:
		return typed.String()
	case []byte:
		return "0x" + common.Bytes2Hex(typed)
	case nil:
		return "null"
	default:
		return fmt.Sprintf("%v", typed)
	}
}

func safeRenderValue(value string) string {
	return terminal.SanitizeInline(value, MaxEIP712RenderValueBytes)
}
