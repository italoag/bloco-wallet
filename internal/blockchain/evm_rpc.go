package blockchain

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strings"

	"blocowallet/internal/evm"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
)

type EVMRPC struct {
	gateway *RPCGateway
	session *ValidatedRPCSession
}

var _ evm.RPC = (*EVMRPC)(nil)

func NewEVMRPC(gateway *RPCGateway, session *ValidatedRPCSession) (*EVMRPC, error) {
	if gateway == nil || session == nil || session.gateway != gateway || session.chainID <= 0 {
		return nil, fmt.Errorf("validated RPC session is required")
	}
	return &EVMRPC{gateway: gateway, session: session}, nil
}

func (client *EVMRPC) ProviderBinding() evm.ProviderBinding {
	if client == nil || client.session == nil {
		return evm.ProviderBinding{}
	}
	return evm.ProviderBinding(client.session.ID())
}

func (client *EVMRPC) ChainID() uint64 {
	if client == nil || client.session == nil || client.session.chainID <= 0 {
		return 0
	}
	return uint64(client.session.chainID)
}

func (client *EVMRPC) TransactionReceipt(ctx context.Context, transactionHash common.Hash) (evm.Receipt, bool, error) {
	if client == nil || client.gateway == nil || client.session == nil || transactionHash == (common.Hash{}) {
		return evm.Receipt{}, false, fmt.Errorf("EVM transaction hash is required")
	}
	var response struct {
		TransactionHash   string `json:"transactionHash"`
		BlockNumber       string `json:"blockNumber"`
		BlockHash         string `json:"blockHash"`
		TransactionIndex  string `json:"transactionIndex"`
		Status            string `json:"status"`
		GasUsed           string `json:"gasUsed"`
		EffectiveGasPrice string `json:"effectiveGasPrice"`
		Logs              []struct {
			Address string   `json:"address"`
			Topics  []string `json:"topics"`
			Data    string   `json:"data"`
		} `json:"logs"`
	}
	found, err := client.gateway.callValidatedNullable(ctx, client.session, "eth_getTransactionReceipt", []any{transactionHash.Hex()}, &response)
	if err != nil || !found {
		return evm.Receipt{}, found, err
	}
	parsedHash, hashErr := decodeRPCData32(response.TransactionHash)
	blockHash, blockHashErr := decodeRPCData32(response.BlockHash)
	blockNumber, numberErr := parseRPCUint64(response.BlockNumber)
	transactionIndex, indexErr := parseRPCUint64(response.TransactionIndex)
	status, statusErr := parseRPCUint64(response.Status)
	gasUsed, gasErr := parseRPCUint64(response.GasUsed)
	effectiveGasPrice, priceErr := parseRPCQuantity(response.EffectiveGasPrice, 256)
	if hashErr != nil || blockHashErr != nil || numberErr != nil || indexErr != nil || statusErr != nil || gasErr != nil || priceErr != nil || parsedHash != transactionHash || blockNumber == 0 || (status != 0 && status != 1) || gasUsed == 0 || effectiveGasPrice.Sign() <= 0 || len(response.Logs) > 1024 {
		return evm.Receipt{}, false, fmt.Errorf("EVM transaction receipt is invalid")
	}
	logs := make([]evm.ReceiptLog, 0, len(response.Logs))
	for _, rawLog := range response.Logs {
		if !common.IsHexAddress(rawLog.Address) || len(rawLog.Address) != 42 || len(rawLog.Topics) > 4 {
			return evm.Receipt{}, false, fmt.Errorf("EVM receipt log is invalid")
		}
		topics := make([]common.Hash, 0, len(rawLog.Topics))
		for _, encodedTopic := range rawLog.Topics {
			topic, err := decodeRPCData32(encodedTopic)
			if err != nil {
				return evm.Receipt{}, false, fmt.Errorf("EVM receipt log topic is invalid")
			}
			topics = append(topics, topic)
		}
		data, err := decodeRPCData(rawLog.Data, 8<<10)
		if err != nil {
			return evm.Receipt{}, false, fmt.Errorf("EVM receipt log data is invalid")
		}
		logs = append(logs, evm.ReceiptLog{Address: common.HexToAddress(rawLog.Address), Topics: topics, Data: data})
	}
	return evm.Receipt{
		TransactionHash:   parsedHash,
		Block:             evm.BlockIdentity{Number: blockNumber, Hash: blockHash},
		TransactionIndex:  transactionIndex,
		Status:            status,
		GasUsed:           gasUsed,
		EffectiveGasPrice: new(big.Int).Set(effectiveGasPrice),
		Logs:              logs,
	}, true, nil
}

func (client *EVMRPC) HeaderByNumber(ctx context.Context, number uint64) (evm.BlockHeader, bool, error) {
	if client == nil || client.gateway == nil || client.session == nil || number == 0 {
		return evm.BlockHeader{}, false, fmt.Errorf("EVM block number is required")
	}
	var response struct {
		Number        string `json:"number"`
		Hash          string `json:"hash"`
		ParentHash    string `json:"parentHash"`
		GasLimit      string `json:"gasLimit"`
		BaseFeePerGas string `json:"baseFeePerGas"`
	}
	found, err := client.gateway.callValidatedNullable(ctx, client.session, "eth_getBlockByNumber", []any{hexutil.EncodeUint64(number), false}, &response)
	if err != nil || !found {
		return evm.BlockHeader{}, found, err
	}
	header, err := decodeBlockHeader(response.Number, response.Hash, response.ParentHash, response.GasLimit, response.BaseFeePerGas)
	if err != nil || header.Number != number {
		return evm.BlockHeader{}, false, fmt.Errorf("EVM block header is invalid")
	}
	return header, true, nil
}

func (client *EVMRPC) LatestHeader(ctx context.Context) (evm.BlockHeader, error) {
	if client == nil || client.gateway == nil || client.session == nil {
		return evm.BlockHeader{}, fmt.Errorf("EVM RPC client is required")
	}
	var response struct {
		Number        string `json:"number"`
		Hash          string `json:"hash"`
		ParentHash    string `json:"parentHash"`
		GasLimit      string `json:"gasLimit"`
		BaseFeePerGas string `json:"baseFeePerGas"`
	}
	if err := client.gateway.Call(ctx, client.session, "eth_getBlockByNumber", []any{"latest", false}, &response); err != nil {
		return evm.BlockHeader{}, fmt.Errorf("get latest EVM header: %w", err)
	}
	header, err := decodeBlockHeader(response.Number, response.Hash, response.ParentHash, response.GasLimit, response.BaseFeePerGas)
	if err != nil {
		return evm.BlockHeader{}, fmt.Errorf("latest EVM header is invalid")
	}
	return header, nil
}

func (client *EVMRPC) PendingNonceAt(ctx context.Context, address common.Address) (uint64, error) {
	if client == nil || client.gateway == nil || client.session == nil {
		return 0, fmt.Errorf("EVM RPC client is required")
	}
	if address == (common.Address{}) {
		return 0, fmt.Errorf("EVM sender address is required")
	}
	var encoded string
	if err := client.gateway.Call(ctx, client.session, "eth_getTransactionCount", []any{address.Hex(), "pending"}, &encoded); err != nil {
		return 0, fmt.Errorf("get pending EVM nonce: %w", err)
	}
	nonce, err := parseRPCUint64(encoded)
	if err != nil {
		return 0, fmt.Errorf("pending EVM nonce is invalid")
	}
	return nonce, nil
}

func (client *EVMRPC) CodeAt(ctx context.Context, address common.Address, block evm.BlockIdentity) ([]byte, error) {
	if client == nil || client.gateway == nil || client.session == nil {
		return nil, fmt.Errorf("EVM RPC client is required")
	}
	if address == (common.Address{}) || block.Number == 0 || block.Hash == (common.Hash{}) {
		return nil, fmt.Errorf("contract address and canonical block are required")
	}
	var encoded string
	selector := map[string]any{"blockHash": block.Hash.Hex(), "requireCanonical": true}
	if err := client.gateway.Call(ctx, client.session, "eth_getCode", []any{address.Hex(), selector}, &encoded); err != nil {
		return nil, fmt.Errorf("get EVM contract code: %w", err)
	}
	code, err := decodeRPCData(encoded, 128<<10)
	if err != nil {
		return nil, fmt.Errorf("EVM contract code is invalid")
	}
	return code, nil
}

func (client *EVMRPC) SendRawTransaction(ctx context.Context, raw []byte) (common.Hash, error) {
	if client == nil || client.gateway == nil || client.session == nil {
		return common.Hash{}, fmt.Errorf("EVM RPC client is required")
	}
	if len(raw) == 0 || len(raw) > 128<<10 {
		return common.Hash{}, fmt.Errorf("signed EVM payload is outside policy")
	}
	var transaction types.Transaction
	if err := transaction.UnmarshalBinary(raw); err != nil || !transaction.Protected() || transaction.ChainId() == nil || !transaction.ChainId().IsInt64() || transaction.ChainId().Int64() != client.session.chainID {
		return common.Hash{}, &evm.BroadcastError{Kind: evm.BroadcastFailureRejected, Cause: fmt.Errorf("signed EVM payload chain binding is invalid")}
	}
	if _, err := types.Sender(types.LatestSignerForChainID(transaction.ChainId()), &transaction); err != nil {
		return common.Hash{}, &evm.BroadcastError{Kind: evm.BroadcastFailureRejected, Cause: fmt.Errorf("signed EVM payload signature is invalid")}
	}
	localHash := transaction.Hash()
	var encoded string
	sent, callErr := client.gateway.callSideEffect(ctx, client.session, "eth_sendRawTransaction", []any{hexutil.Encode(raw)}, &encoded)
	if callErr != nil {
		var remoteError *RPCRemoteError
		if errors.As(callErr, &remoteError) {
			switch remoteError.Kind {
			case RPCErrorAlreadyKnown:
				return localHash, nil
			case RPCErrorNonceTooLow:
				return common.Hash{}, &evm.BroadcastError{Kind: evm.BroadcastFailureNonceLow, Cause: callErr}
			default:
				return common.Hash{}, &evm.BroadcastError{Kind: evm.BroadcastFailureRejected, Cause: callErr}
			}
		}
		if sent {
			return common.Hash{}, &evm.BroadcastError{Kind: evm.BroadcastFailureAmbiguous, Cause: callErr}
		}
		return common.Hash{}, &evm.BroadcastError{Kind: evm.BroadcastFailureRejected, Cause: callErr}
	}
	remoteHash, err := decodeRPCData32(encoded)
	if err != nil || remoteHash != localHash {
		return common.Hash{}, &evm.BroadcastError{Kind: evm.BroadcastFailureAmbiguous, Cause: fmt.Errorf("broadcast EVM transaction hash mismatch")}
	}
	return remoteHash, nil
}

func (client *EVMRPC) SuggestGasPrice(ctx context.Context) (*big.Int, error) {
	return client.feeQuantity(ctx, "eth_gasPrice", "legacy gas price")
}

func (client *EVMRPC) SuggestGasTipCap(ctx context.Context) (*big.Int, error) {
	return client.feeQuantity(ctx, "eth_maxPriorityFeePerGas", "priority fee")
}

func (client *EVMRPC) feeQuantity(ctx context.Context, method, label string) (*big.Int, error) {
	if client == nil || client.gateway == nil || client.session == nil {
		return nil, fmt.Errorf("EVM RPC client is required")
	}
	var encoded string
	if err := client.gateway.Call(ctx, client.session, method, []any{}, &encoded); err != nil {
		return nil, fmt.Errorf("get EVM %s: %w", label, err)
	}
	value, err := parseRPCQuantity(encoded, 256)
	if err != nil || value.Sign() <= 0 {
		return nil, fmt.Errorf("EVM %s is invalid", label)
	}
	return new(big.Int).Set(value), nil
}

func (client *EVMRPC) CallContract(ctx context.Context, call evm.TransactionCall, block evm.BlockIdentity) ([]byte, error) {
	callObject, blockSelector, err := encodeTransactionCall(call, block)
	if err != nil {
		return nil, err
	}
	var encoded string
	if err := client.gateway.Call(ctx, client.session, "eth_call", []any{callObject, blockSelector}, &encoded); err != nil {
		var remoteError *RPCRemoteError
		if errors.As(err, &remoteError) && remoteError.Kind == RPCErrorExecutionReverted {
			return nil, &evm.RevertError{Kind: evm.RevertUnknown, Data: append([]byte(nil), remoteError.Data...), Cause: err}
		}
		return nil, fmt.Errorf("simulate EVM call: %w", err)
	}
	result, err := decodeRPCData(encoded, 64<<10)
	if err != nil {
		return nil, fmt.Errorf("EVM call result is invalid")
	}
	return result, nil
}

func (client *EVMRPC) EstimateGas(ctx context.Context, call evm.TransactionCall, block evm.BlockIdentity) (uint64, error) {
	callObject, blockSelector, err := encodeTransactionCall(call, block)
	if err != nil {
		return 0, err
	}
	var encoded string
	if err := client.gateway.Call(ctx, client.session, "eth_estimateGas", []any{callObject, blockSelector}, &encoded); err != nil {
		return 0, fmt.Errorf("estimate EVM gas: %w", err)
	}
	gas, err := parseRPCUint64(encoded)
	if err != nil || gas == 0 || gas > 30_000_000 {
		return 0, fmt.Errorf("EVM gas estimate is outside policy")
	}
	return gas, nil
}

func encodeTransactionCall(call evm.TransactionCall, block evm.BlockIdentity) (map[string]any, map[string]any, error) {
	if call.From == (common.Address{}) || call.To == (common.Address{}) {
		return nil, nil, fmt.Errorf("EVM call addresses are required")
	}
	if call.Value == nil || call.Value.Sign() < 0 || call.Value.BitLen() > 256 || len(call.Input) > 128<<10 {
		return nil, nil, fmt.Errorf("EVM call value or input is outside policy")
	}
	if block.Number == 0 || block.Hash == (common.Hash{}) {
		return nil, nil, fmt.Errorf("canonical EVM block is required")
	}
	object := map[string]any{
		"from":  call.From.Hex(),
		"to":    call.To.Hex(),
		"value": hexutil.EncodeBig(call.Value),
		"data":  hexutil.Encode(call.Input),
	}
	if call.Gas != 0 {
		object["gas"] = hexutil.EncodeUint64(call.Gas)
	}
	if call.GasPrice != nil {
		if call.GasPrice.Sign() <= 0 || call.GasPrice.BitLen() > 256 || call.MaxFeePerGas != nil || call.MaxPriorityFeePerGas != nil {
			return nil, nil, fmt.Errorf("legacy EVM fee fields are outside policy")
		}
		object["gasPrice"] = hexutil.EncodeBig(call.GasPrice)
	}
	if call.MaxFeePerGas != nil || call.MaxPriorityFeePerGas != nil {
		if call.MaxFeePerGas == nil || call.MaxPriorityFeePerGas == nil || call.MaxFeePerGas.Sign() <= 0 || call.MaxPriorityFeePerGas.Sign() < 0 || call.MaxFeePerGas.BitLen() > 256 || call.MaxPriorityFeePerGas.BitLen() > 256 || call.MaxPriorityFeePerGas.Cmp(call.MaxFeePerGas) > 0 || call.GasPrice != nil {
			return nil, nil, fmt.Errorf("dynamic EVM fee fields are outside policy")
		}
		object["maxFeePerGas"] = hexutil.EncodeBig(call.MaxFeePerGas)
		object["maxPriorityFeePerGas"] = hexutil.EncodeBig(call.MaxPriorityFeePerGas)
	}
	selector := map[string]any{"blockHash": block.Hash.Hex(), "requireCanonical": true}
	return object, selector, nil
}

func decodeRPCData(value string, maxBytes int) ([]byte, error) {
	if maxBytes <= 0 || len(value) < 2 || len(value) > 2+(maxBytes*2) || !strings.HasPrefix(value, "0x") || len(value)%2 != 0 {
		return nil, fmt.Errorf("invalid RPC data")
	}
	decoded, err := hex.DecodeString(value[2:])
	if err != nil || len(decoded) > maxBytes {
		return nil, fmt.Errorf("invalid RPC data")
	}
	return decoded, nil
}

func decodeBlockHeader(numberValue, hashValue, parentHashValue, gasLimitValue, baseFeeValue string) (evm.BlockHeader, error) {
	number, err := parseRPCUint64(numberValue)
	if err != nil || number == 0 {
		return evm.BlockHeader{}, fmt.Errorf("invalid block number")
	}
	gasLimit, err := parseRPCUint64(gasLimitValue)
	if err != nil || gasLimit == 0 || gasLimit > 100_000_000 {
		return evm.BlockHeader{}, fmt.Errorf("invalid block gas limit")
	}
	hash, err := decodeRPCData32(hashValue)
	if err != nil {
		return evm.BlockHeader{}, err
	}
	parentHash, err := decodeRPCData32(parentHashValue)
	if err != nil {
		return evm.BlockHeader{}, err
	}
	var baseFee *big.Int
	if baseFeeValue != "" {
		baseFee, err = parseRPCQuantity(baseFeeValue, 256)
		if err != nil {
			return evm.BlockHeader{}, err
		}
	}
	return evm.BlockHeader{
		BlockIdentity: evm.BlockIdentity{Number: number, Hash: hash},
		ParentHash:    parentHash, GasLimit: gasLimit, BaseFeePerGas: baseFee,
	}, nil
}

func parseRPCUint64(value string) (uint64, error) {
	quantity, err := parseRPCQuantity(value, 64)
	if err != nil || !quantity.IsUint64() {
		return 0, fmt.Errorf("invalid RPC uint64 quantity")
	}
	return quantity.Uint64(), nil
}

func decodeRPCData32(value string) (common.Hash, error) {
	if len(value) != 66 || !strings.HasPrefix(value, "0x") {
		return common.Hash{}, fmt.Errorf("invalid RPC data length")
	}
	decoded, err := hex.DecodeString(value[2:])
	if err != nil || len(decoded) != common.HashLength {
		return common.Hash{}, fmt.Errorf("invalid RPC data")
	}
	return common.BytesToHash(decoded), nil
}
