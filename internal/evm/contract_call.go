package evm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"reflect"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

const (
	MaxCallABIBytes     = 256 << 10
	MaxCallABIMethods   = 64
	MaxCallABIInputs    = 16
	MaxCallArgsJSON     = 64 << 10
	MaxCallCalldata     = 32 << 10
	MaxCallOutputBytes  = 4 << 10
	maxCallMethodText   = 512
	maxCallPrimitiveLen = 4096
)

type ABISourceKind string

const (
	ABISourceBuiltin ABISourceKind = "builtin"
	ABISourceLocal   ABISourceKind = "local"
)

type CallABI struct {
	Handle  abi.ABI
	Kind    ABISourceKind
	Hash    common.Hash
	Methods map[string]abi.Method
}

func (handle *CallABI) ABIHash() common.Hash {
	if handle == nil {
		return common.Hash{}
	}
	return handle.Hash
}

func ParseCallABI(data []byte, kind ABISourceKind) (*CallABI, error) {
	if err := validateStrictJSON(data, MaxCallABIBytes, MaxStrictJSONDepth, MaxStrictJSONArrayLen); err != nil {
		return nil, err
	}
	parsed, err := abi.JSON(bytes.NewReader(data))
	if err != nil {
		return nil, &EngineError{Code: ErrorInvalidIntent, Field: "contract ABI", Cause: err}
	}
	if len(parsed.Methods) == 0 || len(parsed.Methods) > MaxCallABIMethods {
		return nil, invalidIntent("contract ABI methods")
	}
	if kind != ABISourceBuiltin && kind != ABISourceLocal {
		return nil, invalidIntent("contract ABI source")
	}
	selectors := make(map[string]string, len(parsed.Methods))
	for name, method := range parsed.Methods {
		if name == "" || len(name) > 128 || len(method.Inputs) > MaxCallABIInputs || len(method.Outputs) > MaxCallABIInputs {
			return nil, invalidIntent("contract ABI method")
		}
		selectorKey := string(method.ID)
		if owner, duplicate := selectors[selectorKey]; duplicate && owner != name {
			return nil, invalidIntent("contract ABI selector collision")
		}
		selectors[selectorKey] = name
		for _, input := range method.Inputs {
			if input.Name == "" || len(input.Name) > 128 || !supportedCallABIType(input.Type) {
				return nil, invalidIntent("contract ABI input")
			}
		}
		for _, output := range method.Outputs {
			if !supportedCallABIType(output.Type) {
				return nil, invalidIntent("contract ABI output")
			}
		}
	}
	canonical, err := json.Marshal(parsed)
	if err != nil {
		return nil, &EngineError{Code: ErrorInvalidIntent, Field: "contract ABI canonical", Cause: err}
	}
	return &CallABI{
		Handle:  parsed,
		Kind:    kind,
		Hash:    crypto.Keccak256Hash(canonical),
		Methods: parsed.Methods,
	}, nil
}

func supportedCallABIType(argumentType abi.Type) bool {
	switch argumentType.T {
	case abi.AddressTy, abi.BoolTy, abi.StringTy, abi.BytesTy, abi.FixedBytesTy, abi.UintTy, abi.IntTy:
		return true
	case abi.SliceTy, abi.ArrayTy:
		if argumentType.Elem == nil {
			return false
		}
		switch argumentType.Elem.T {
		case abi.AddressTy, abi.BoolTy, abi.StringTy, abi.BytesTy, abi.FixedBytesTy, abi.UintTy, abi.IntTy:
			return argumentType.Elem.Elem == nil
		default:
			return false
		}
	default:
		return false
	}
}

func decodeCallABIArgs(data []byte, method abi.Method) ([]any, error) {
	if len(data) > MaxCallArgsJSON {
		return nil, invalidIntent("contract call args size")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var raw []any
	if err := decoder.Decode(&raw); err != nil {
		return nil, &EngineError{Code: ErrorInvalidIntent, Field: "contract call args", Cause: err}
	}
	if err := rejectStrictJSONTrailing(decoder); err != nil {
		return nil, err
	}
	if len(raw) != len(method.Inputs) {
		return nil, invalidIntent("contract call argument count")
	}
	args := make([]any, 0, len(raw))
	for index, input := range method.Inputs {
		converted, err := convertCallABIValue(raw[index], input.Type)
		if err != nil {
			return nil, err
		}
		args = append(args, converted)
	}
	return args, nil
}

func convertCallABIValue(value any, argumentType abi.Type) (any, error) {
	if argumentType.T == abi.SliceTy || argumentType.T == abi.ArrayTy {
		raw, ok := value.([]any)
		if !ok || len(raw) > 256 {
			return nil, invalidIntent("contract call array argument")
		}
		if argumentType.Elem == nil {
			return nil, invalidIntent("contract call array element")
		}
		if argumentType.T == abi.ArrayTy && len(raw) != argumentType.Size {
			return nil, invalidIntent("contract call fixed array size")
		}
		elemType := argumentType.Elem.GetType()
		converted := reflect.MakeSlice(reflect.SliceOf(elemType), len(raw), len(raw))
		for index, item := range raw {
			element, err := convertCallABIValue(item, *argumentType.Elem)
			if err != nil {
				return nil, err
			}
			elementValue := reflect.ValueOf(element)
			if elementValue.Type() != elemType {
				if !elementValue.Type().ConvertibleTo(elemType) {
					return nil, invalidIntent("contract call array element type")
				}
				elementValue = elementValue.Convert(elemType)
			}
			converted.Index(index).Set(elementValue)
		}
		return converted.Interface(), nil
	}
	switch argumentType.T {
	case abi.AddressTy:
		text, ok := value.(string)
		if !ok || !common.IsHexAddress(text) || common.HexToAddress(text).Hex() != text {
			return nil, invalidIntent("contract call address argument")
		}
		return common.HexToAddress(text), nil
	case abi.BoolTy:
		flag, ok := value.(bool)
		if !ok {
			return nil, invalidIntent("contract call boolean argument")
		}
		return flag, nil
	case abi.StringTy:
		text, ok := value.(string)
		if !ok || len(text) > maxCallPrimitiveLen {
			return nil, invalidIntent("contract call string argument")
		}
		return text, nil
	case abi.BytesTy:
		text, ok := value.(string)
		if !ok || !strings.HasPrefix(text, "0x") || len(text) > maxCallPrimitiveLen*2+2 {
			return nil, invalidIntent("contract call bytes argument")
		}
		decoded := common.FromHex(text)
		if len(decoded) == 0 && text != "0x" {
			return nil, invalidIntent("contract call bytes argument")
		}
		return decoded, nil
	case abi.FixedBytesTy:
		text, ok := value.(string)
		if !ok || !strings.HasPrefix(text, "0x") || len(text) > maxCallPrimitiveLen*2+2 {
			return nil, invalidIntent("contract call fixed bytes argument")
		}
		decoded := common.FromHex(text)
		if len(decoded) == 0 && text != "0x" {
			return nil, invalidIntent("contract call fixed bytes argument")
		}
		if len(decoded) != argumentType.Size {
			return nil, invalidIntent("contract call fixed bytes argument")
		}
		fixed := reflect.New(reflect.ArrayOf(argumentType.Size, reflect.TypeOf(byte(0)))).Elem()
		reflect.Copy(fixed, reflect.ValueOf(decoded))
		return fixed.Interface(), nil
	case abi.UintTy, abi.IntTy:
		integer, err := callABIInteger(value)
		if err != nil {
			return nil, err
		}
		return convertCallABIInteger(integer, argumentType)
	default:
		return nil, invalidIntent("contract call argument type")
	}
}

func convertCallABIInteger(integer *big.Int, argumentType abi.Type) (any, error) {
	if argumentType.Size > 64 {
		return integer, nil
	}
	if argumentType.T == abi.UintTy {
		bound := new(big.Int).Lsh(big.NewInt(1), uint(argumentType.Size))
		if integer.Sign() < 0 || integer.Cmp(new(big.Int).Sub(bound, big.NewInt(1))) > 0 {
			return nil, invalidIntent("contract call unsigned argument bounds")
		}
		return boundedUnsignedInteger(integer.Uint64(), argumentType.Size), nil
	}
	bound := new(big.Int).Lsh(big.NewInt(1), uint(argumentType.Size-1))
	if integer.Cmp(new(big.Int).Sub(bound, big.NewInt(1))) > 0 || integer.Cmp(new(big.Int).Neg(bound)) < 0 {
		return nil, invalidIntent("contract call signed argument bounds")
	}
	return boundedSignedInteger(integer.Int64(), argumentType.Size), nil
}

func boundedUnsignedInteger(value uint64, size int) any {
	switch size {
	case 8:
		return uint8(value)
	case 16:
		return uint16(value)
	case 32:
		return uint32(value)
	default:
		return uint64(value)
	}
}

func boundedSignedInteger(value int64, size int) any {
	switch size {
	case 8:
		return int8(value)
	case 16:
		return int16(value)
	case 32:
		return int32(value)
	default:
		return int64(value)
	}
}

func callABIInteger(value any) (*big.Int, error) {
	switch typed := value.(type) {
	case json.Number:
		integer := new(big.Int)
		if _, ok := integer.SetString(typed.String(), 10); !ok || integer.BitLen() > 256 {
			return nil, invalidIntent("contract call integer argument")
		}
		return integer, nil
	case string:
		integer := new(big.Int)
		text := strings.TrimPrefix(typed, "0x")
		base := 10
		if strings.HasPrefix(typed, "0x") {
			base = 16
		}
		if _, ok := integer.SetString(text, base); !ok || integer.BitLen() > 256 {
			return nil, invalidIntent("contract call integer argument")
		}
		return integer, nil
	default:
		return nil, invalidIntent("contract call integer argument")
	}
}

type PrepareContractCallRequest struct {
	OperationID    string
	PlanGeneration uint64
	AccountID      string
	ChainID        uint64
	From           common.Address
	Contract       common.Address
	Value          *big.Int
	ABI            []byte
	ABISource      ABISourceKind
	Method         string
	Args           []byte
}

type ContractCallPreview struct {
	Contract  common.Address
	Method    string
	Selector  [4]byte
	Calldata  []byte
	Value     *big.Int
	ABISource ABISourceKind
	ABIHash   common.Hash
	Output    string
}

type ContractCallIntent struct {
	accountID string
	chainID   uint64
	from      common.Address
	contract  common.Address
	value     *big.Int
	abiHash   common.Hash
	abiSource ABISourceKind
	method    string
	calldata  []byte
}

func (intent ContractCallIntent) AccountID() string { return intent.accountID }
func (intent ContractCallIntent) ChainID() uint64   { return intent.chainID }
func (intent ContractCallIntent) From() common.Address {
	return intent.from
}
func (intent ContractCallIntent) Contract() common.Address {
	return intent.contract
}
func (intent ContractCallIntent) Value() *big.Int {
	return new(big.Int).Set(intent.value)
}
func (intent ContractCallIntent) ABISource() ABISourceKind { return intent.abiSource }
func (intent ContractCallIntent) ABIHash() common.Hash     { return intent.abiHash }
func (intent ContractCallIntent) Method() string           { return intent.method }
func (intent ContractCallIntent) Calldata() []byte {
	return append([]byte(nil), intent.calldata...)
}

func (engine *Engine) PrepareContractCall(ctx context.Context, request PrepareContractCallRequest) (*PreparedNativeTransfer, error) {
	if engine == nil || engine.repository == nil || engine.rpc == nil {
		return nil, &EngineError{Code: ErrorInvalidIntent, Field: "engine"}
	}
	if !accountIDPattern.MatchString(request.AccountID) || request.ChainID == 0 || request.From == (common.Address{}) || request.Contract == (common.Address{}) || request.Value == nil || request.Value.Sign() < 0 || request.Value.BitLen() > 256 {
		return nil, invalidIntent("contract call identity")
	}
	if !accountIDPattern.MatchString(request.OperationID) || request.PlanGeneration == 0 {
		return nil, invalidIntent("operation identity")
	}
	if request.Method == "" || len(request.Method) > maxCallMethodText {
		return nil, invalidIntent("contract call method")
	}
	callABI, err := ParseCallABI(request.ABI, request.ABISource)
	if err != nil {
		return nil, err
	}
	method, exists := callABI.Methods[request.Method]
	if !exists {
		return nil, invalidIntent("contract call method lookup")
	}
	args, err := decodeCallABIArgs(request.Args, method)
	if err != nil {
		return nil, err
	}
	calldata, err := method.Inputs.PackValues(args)
	if err != nil {
		return nil, &EngineError{Code: ErrorInvalidIntent, Field: "contract call encode", Cause: err}
	}
	encoded := append(append([]byte(nil), method.ID...), calldata...)
	if len(encoded) > MaxCallCalldata {
		return nil, invalidIntent("contract call calldata size")
	}
	if resolved, err := callABI.Handle.MethodById(encoded[:4]); err != nil || resolved.Name != method.Name {
		return nil, invalidIntent("contract call round-trip")
	}
	if engine.rpc.ChainID() != request.ChainID || engine.rpc.ProviderBinding() == (ProviderBinding{}) {
		return nil, &EngineError{Code: ErrorProviderUnavailable, Field: "provider binding"}
	}
	header, err := engine.rpc.LatestHeader(ctx)
	if err != nil {
		return nil, &EngineError{Code: ErrorProviderUnavailable, Field: "latest header", Cause: err}
	}
	code, err := engine.rpc.CodeAt(ctx, request.Contract, header.BlockIdentity)
	if err != nil {
		return nil, &EngineError{Code: ErrorProviderUnavailable, Field: "contract code", Cause: err}
	}
	if len(code) == 0 {
		return nil, &EngineError{Code: ErrorPolicyDenied, Field: "contract has no code"}
	}
	pendingNonce, err := engine.rpc.PendingNonceAt(ctx, request.From)
	if err != nil {
		return nil, &EngineError{Code: ErrorProviderUnavailable, Field: "pending nonce", Cause: err}
	}
	reservationID, err := engine.options.NewID()
	if err != nil {
		return nil, &EngineError{Code: ErrorNonceConflict, Field: "reservation ID", Cause: err}
	}
	now := engine.options.Now().UTC()
	reservation, err := engine.repository.ReserveNonce(ctx, ReserveNonceRequest{
		ReservationID: reservationID, OperationID: request.OperationID, AccountID: request.AccountID,
		Sender: request.From, ChainID: request.ChainID, PendingNonce: pendingNonce,
		PlanGeneration: request.PlanGeneration, ReservedAt: now, ExpiresAt: now.Add(engine.options.ReservationTTL),
	})
	if err != nil {
		return nil, err
	}
	invalidate := func() {
		_ = engine.repository.InvalidateUnsignedReservation(context.Background(), InvalidateReservationRequest{
			ReservationID: reservation.ReservationID, AccountID: reservation.AccountID,
			PlanGeneration: reservation.PlanGeneration, InvalidatedAt: engine.options.Now().UTC(), Reason: "plan_stale",
		})
	}
	fees, err := engine.feeOracle.Suggest(ctx, engine.rpc, header)
	if err != nil {
		invalidate()
		return nil, err
	}
	call := TransactionCall{
		From: request.From, To: request.Contract, Value: new(big.Int).Set(request.Value), Input: encoded,
	}
	applyFeeSuggestion(&call, fees)
	simulation, err := engine.simulator.Simulate(ctx, engine.rpc, call, header.BlockIdentity)
	if err != nil {
		invalidate()
		return nil, err
	}
	output, err := validateCallSimulationOutput(method, simulation.ReturnData)
	if err != nil {
		invalidate()
		return nil, err
	}
	if err := validateEconomics(engine.options.EconomicPolicy, fees, simulation, header, request.Value); err != nil {
		invalidate()
		return nil, err
	}
	findings, err := NewRiskPolicy().Evaluate(OperationContractCall, request.Contract, request.Value, simulation)
	if err != nil {
		invalidate()
		return nil, err
	}
	commitment, err := simulationPolicyCommitment(simulation, findings)
	if err != nil {
		invalidate()
		return nil, &EngineError{Code: ErrorPolicyDenied, Field: "simulation policy commitment", Cause: err}
	}
	var plan *FrozenPlan
	switch fees.Model {
	case FeeLegacy:
		plan, err = engine.planner.PlanContractCall(ContractCallIntent{
			accountID: request.AccountID, chainID: request.ChainID, from: request.From, contract: request.Contract,
			value: new(big.Int).Set(request.Value), abiHash: callABI.Hash, abiSource: callABI.Kind,
			method: method.Name, calldata: encoded,
		}, NativePlanInput{
			ProviderBinding: engine.rpc.ProviderBinding(), Nonce: reservation.Nonce, GasLimit: simulation.GasLimit,
			LegacyGasPrice: fees.GasPrice, SimulationBlockNumber: header.Number, SimulationBlockHash: header.Hash,
			SimulationResultHash: commitment,
		})
	case FeeDynamic:
		plan, err = engine.planner.PlanContractCallDynamicFee(ContractCallIntent{
			accountID: request.AccountID, chainID: request.ChainID, from: request.From, contract: request.Contract,
			value: new(big.Int).Set(request.Value), abiHash: callABI.Hash, abiSource: callABI.Kind,
			method: method.Name, calldata: encoded,
		}, DynamicFeePlanInput{
			ProviderBinding: engine.rpc.ProviderBinding(), Nonce: reservation.Nonce, GasLimit: simulation.GasLimit,
			GasFeeCap: fees.GasFeeCap, GasTipCap: fees.GasTipCap,
			SimulationBlockNumber: header.Number, SimulationBlockHash: header.Hash,
			SimulationResultHash: commitment,
		})
	default:
		err = invalidIntent("contract call fee model")
	}
	if err != nil {
		invalidate()
		return nil, err
	}
	var selector [4]byte
	copy(selector[:], encoded[:4])
	plan.contractCallPreview = ContractCallPreview{
		Contract: request.Contract, Method: method.Name, Selector: selector, Calldata: append([]byte(nil), encoded...),
		Value: new(big.Int).Set(request.Value), ABISource: callABI.Kind, ABIHash: callABI.Hash, Output: output,
	}
	return &PreparedNativeTransfer{plan: plan, reservation: reservation, simulation: simulation, fees: fees, findings: append([]RiskFinding(nil), findings...)}, nil
}

func validateCallSimulationOutput(method abi.Method, returnData []byte) (string, error) {
	if len(method.Outputs) == 0 {
		if len(returnData) != 0 {
			return "", &EngineError{Code: ErrorSimulationFailed, Field: "contract call unexpected return data"}
		}
		return "no return value", nil
	}
	if len(returnData) > MaxCallOutputBytes {
		return "", &EngineError{Code: ErrorSimulationFailed, Field: "contract call return data size"}
	}
	decoded, err := method.Outputs.Unpack(returnData)
	if err != nil {
		return "", &EngineError{Code: ErrorSimulationFailed, Field: "contract call return decode", Cause: err}
	}
	return renderCallOutput(decoded), nil
}

func renderCallOutput(values []any) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, safeCallOutputString(value))
	}
	return strings.Join(parts, ", ")
}

func safeCallOutputString(value any) string {
	switch typed := value.(type) {
	case string:
		if len(typed) > maxCallPrimitiveLen {
			return typed[:maxCallPrimitiveLen] + "..."
		}
		return typed
	case common.Address:
		return typed.Hex()
	case []byte:
		return "0x" + common.Bytes2Hex(typed)
	case *big.Int:
		return typed.String()
	case bool:
		if typed {
			return "true"
		}
		return "false"
	case []common.Address:
		parts := make([]string, 0, len(typed))
		for _, address := range typed {
			parts = append(parts, address.Hex())
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case []*big.Int:
		parts := make([]string, 0, len(typed))
		for _, integer := range typed {
			parts = append(parts, integer.String())
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case []string:
		return "[" + strings.Join(typed, ", ") + "]"
	case [][]byte:
		parts := make([]string, 0, len(typed))
		for _, data := range typed {
			parts = append(parts, "0x"+common.Bytes2Hex(data))
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case []bool:
		parts := make([]string, 0, len(typed))
		for _, flag := range typed {
			parts = append(parts, fmt.Sprintf("%t", flag))
		}
		return "[" + strings.Join(parts, ", ") + "]"
	default:
		return fmt.Sprintf("%v", typed)
	}
}
