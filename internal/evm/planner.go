package evm

import (
	"math/big"
	"unicode"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rlp"
)

const planDomain = "bloco-wallet/evm-plan/v1"

type Operation string

const (
	OperationNativeTransfer       Operation = "native_transfer"
	OperationERC20Transfer        Operation = "erc20_transfer"
	OperationERC20Approve         Operation = "erc20_approve"
	OperationERC721SafeTransfer   Operation = "erc721_safe_transfer"
	OperationERC1155SafeTransfer  Operation = "erc1155_safe_transfer"
	OperationERC1155BatchTransfer Operation = "erc1155_batch_transfer"
	OperationContractCall         Operation = "contract_call"
)

type NativePlanInput struct {
	ProviderBinding       ProviderBinding
	Nonce                 uint64
	GasLimit              uint64
	LegacyGasPrice        *big.Int
	SimulationBlockNumber uint64
	SimulationBlockHash   common.Hash
	SimulationResultHash  common.Hash
}

type DynamicFeePlanInput struct {
	ProviderBinding       ProviderBinding
	Nonce                 uint64
	GasLimit              uint64
	GasFeeCap             *big.Int
	GasTipCap             *big.Int
	SimulationBlockNumber uint64
	SimulationBlockHash   common.Hash
	SimulationResultHash  common.Hash
}

type TokenMetadata struct {
	Name        string
	Symbol      string
	Decimals    uint8
	BlockNumber uint64
}

type Asset struct {
	Name     string
	Symbol   string
	Decimals uint8
	Contract common.Address
}

type ERC20PlanInput struct {
	NativePlanInput
	Metadata TokenMetadata
}

type ERC20DynamicPlanInput struct {
	DynamicFeePlanInput
	Metadata TokenMetadata
}

type ERC721PlanInput struct {
	NativePlanInput
	TokenID *big.Int
}

type ERC721DynamicPlanInput struct {
	DynamicFeePlanInput
	TokenID *big.Int
}

type FrozenPlan struct {
	accountID             string
	chainID               *big.Int
	from                  common.Address
	providerBinding       ProviderBinding
	operation             Operation
	counterparty          common.Address
	amount                *big.Int
	tokenID               *big.Int
	effects               []EffectEntry
	transaction           *types.Transaction
	asset                 Asset
	contractCallPreview   ContractCallPreview
	planHash              [32]byte
	transactionDigest     [32]byte
	simulationBlockNumber uint64
	simulationBlockHash   common.Hash
	simulationResultHash  common.Hash
}

type Planner struct{}

func NewPlanner() *Planner { return &Planner{} }

func (planner *Planner) PlanNative(intent NativeTransferIntent, input NativePlanInput) (*FrozenPlan, error) {
	if intent.amount == nil {
		return nil, invalidIntent("native intent")
	}
	if input.ProviderBinding == (ProviderBinding{}) {
		return nil, invalidIntent("provider binding")
	}
	if input.GasLimit < 21_000 || input.GasLimit > 30_000_000 {
		return nil, invalidIntent("gas limit")
	}
	if input.LegacyGasPrice == nil || input.LegacyGasPrice.Sign() <= 0 || input.LegacyGasPrice.BitLen() > 256 {
		return nil, invalidIntent("gas price")
	}
	if input.SimulationBlockNumber == 0 || input.SimulationBlockHash == (common.Hash{}) {
		return nil, invalidIntent("simulation block")
	}
	chainID := new(big.Int).SetUint64(intent.chainID)
	transaction := types.NewTx(&types.LegacyTx{
		Nonce:    input.Nonce,
		GasPrice: new(big.Int).Set(input.LegacyGasPrice),
		Gas:      input.GasLimit,
		To:       addressPointer(intent.to),
		Value:    new(big.Int).Set(intent.amount),
	})
	digest := types.NewEIP155Signer(chainID).Hash(transaction)
	canonical, err := rlp.EncodeToBytes(struct {
		Domain                string
		FeeModel              string
		AccountID             string
		ChainID               *big.Int
		From                  common.Address
		ProviderBinding       ProviderBinding
		Nonce                 uint64
		GasPrice              *big.Int
		GasLimit              uint64
		To                    common.Address
		Value                 *big.Int
		Data                  []byte
		SimulationBlockNumber uint64
		SimulationBlockHash   common.Hash
		SimulationResultHash  common.Hash
	}{
		Domain:                planDomain,
		FeeModel:              "legacy",
		AccountID:             intent.accountID,
		ChainID:               chainID,
		From:                  intent.from,
		ProviderBinding:       input.ProviderBinding,
		Nonce:                 input.Nonce,
		GasPrice:              new(big.Int).Set(input.LegacyGasPrice),
		GasLimit:              input.GasLimit,
		To:                    intent.to,
		Value:                 new(big.Int).Set(intent.amount),
		SimulationBlockNumber: input.SimulationBlockNumber,
		SimulationBlockHash:   input.SimulationBlockHash,
		SimulationResultHash:  input.SimulationResultHash,
	})
	if err != nil {
		return nil, &EngineError{Code: ErrorInvalidIntent, Field: "canonical plan", Cause: err}
	}
	planHash := crypto.Keccak256Hash(canonical)
	return &FrozenPlan{
		accountID:             intent.accountID,
		chainID:               chainID,
		from:                  intent.from,
		providerBinding:       input.ProviderBinding,
		operation:             OperationNativeTransfer,
		counterparty:          intent.to,
		amount:                new(big.Int).Set(intent.amount),
		transaction:           transaction,
		planHash:              planHash,
		transactionDigest:     digest,
		simulationBlockNumber: input.SimulationBlockNumber,
		simulationBlockHash:   input.SimulationBlockHash,
		simulationResultHash:  input.SimulationResultHash,
	}, nil
}

func (planner *Planner) PlanERC20Transfer(intent ERC20TransferIntent, input ERC20PlanInput) (*FrozenPlan, error) {
	if intent.amount == nil {
		return nil, invalidIntent("ERC-20 intent")
	}
	if input.ProviderBinding == (ProviderBinding{}) {
		return nil, invalidIntent("provider binding")
	}
	if input.GasLimit < 21_000 || input.GasLimit > 30_000_000 {
		return nil, invalidIntent("gas limit")
	}
	if input.LegacyGasPrice == nil || input.LegacyGasPrice.Sign() <= 0 || input.LegacyGasPrice.BitLen() > 256 {
		return nil, invalidIntent("gas price")
	}
	if input.SimulationBlockNumber == 0 || input.SimulationBlockHash == (common.Hash{}) || input.Metadata.BlockNumber != input.SimulationBlockNumber {
		return nil, invalidIntent("simulation block")
	}
	if !validMetadataText(input.Metadata.Name, 64) || !validMetadataText(input.Metadata.Symbol, 16) || input.Metadata.Decimals > 36 {
		return nil, invalidIntent("token metadata")
	}
	data := encodeERC20Method("transfer(address,uint256)", intent.to, intent.amount)
	chainID := new(big.Int).SetUint64(intent.chainID)
	transaction := types.NewTx(&types.LegacyTx{
		Nonce:    input.Nonce,
		GasPrice: new(big.Int).Set(input.LegacyGasPrice),
		Gas:      input.GasLimit,
		To:       addressPointer(intent.contract),
		Value:    new(big.Int),
		Data:     append([]byte(nil), data...),
	})
	digest := types.NewEIP155Signer(chainID).Hash(transaction)
	canonical, err := rlp.EncodeToBytes(struct {
		Domain                string
		Operation             string
		FeeModel              string
		AccountID             string
		ChainID               *big.Int
		From                  common.Address
		ProviderBinding       ProviderBinding
		Contract              common.Address
		Recipient             common.Address
		Amount                *big.Int
		TokenName             string
		TokenSymbol           string
		TokenDecimals         uint8
		Nonce                 uint64
		GasPrice              *big.Int
		GasLimit              uint64
		Data                  []byte
		SimulationBlockNumber uint64
		SimulationBlockHash   common.Hash
		SimulationResultHash  common.Hash
	}{
		Domain:                planDomain,
		Operation:             "erc20_transfer",
		FeeModel:              "legacy",
		AccountID:             intent.accountID,
		ChainID:               chainID,
		From:                  intent.from,
		ProviderBinding:       input.ProviderBinding,
		Contract:              intent.contract,
		Recipient:             intent.to,
		Amount:                new(big.Int).Set(intent.amount),
		TokenName:             input.Metadata.Name,
		TokenSymbol:           input.Metadata.Symbol,
		TokenDecimals:         input.Metadata.Decimals,
		Nonce:                 input.Nonce,
		GasPrice:              new(big.Int).Set(input.LegacyGasPrice),
		GasLimit:              input.GasLimit,
		Data:                  append([]byte(nil), data...),
		SimulationBlockNumber: input.SimulationBlockNumber,
		SimulationBlockHash:   input.SimulationBlockHash,
		SimulationResultHash:  input.SimulationResultHash,
	})
	if err != nil {
		return nil, &EngineError{Code: ErrorInvalidIntent, Field: "canonical plan", Cause: err}
	}
	return &FrozenPlan{
		accountID:             intent.accountID,
		chainID:               chainID,
		from:                  intent.from,
		providerBinding:       input.ProviderBinding,
		operation:             OperationERC20Transfer,
		counterparty:          intent.to,
		amount:                new(big.Int).Set(intent.amount),
		transaction:           transaction,
		asset:                 Asset{Name: input.Metadata.Name, Symbol: input.Metadata.Symbol, Decimals: input.Metadata.Decimals, Contract: intent.contract},
		planHash:              crypto.Keccak256Hash(canonical),
		transactionDigest:     digest,
		simulationBlockNumber: input.SimulationBlockNumber,
		simulationBlockHash:   input.SimulationBlockHash,
		simulationResultHash:  input.SimulationResultHash,
	}, nil
}

func (planner *Planner) PlanERC20Approve(intent ERC20ApproveIntent, input ERC20PlanInput) (*FrozenPlan, error) {
	if intent.amount == nil || input.ProviderBinding == (ProviderBinding{}) {
		return nil, invalidIntent("ERC-20 approval intent")
	}
	if input.GasLimit < 21_000 || input.GasLimit > 30_000_000 || input.LegacyGasPrice == nil || input.LegacyGasPrice.Sign() <= 0 || input.LegacyGasPrice.BitLen() > 256 {
		return nil, invalidIntent("approval gas or fee")
	}
	if input.SimulationBlockNumber == 0 || input.SimulationBlockHash == (common.Hash{}) || input.Metadata.BlockNumber != input.SimulationBlockNumber || !validMetadataText(input.Metadata.Name, 64) || !validMetadataText(input.Metadata.Symbol, 16) || input.Metadata.Decimals > 36 {
		return nil, invalidIntent("approval metadata or simulation")
	}
	data := encodeERC20Method("approve(address,uint256)", intent.spender, intent.amount)
	chainID := new(big.Int).SetUint64(intent.chainID)
	transaction := types.NewTx(&types.LegacyTx{
		Nonce: input.Nonce, GasPrice: new(big.Int).Set(input.LegacyGasPrice), Gas: input.GasLimit,
		To: addressPointer(intent.contract), Value: new(big.Int), Data: append([]byte(nil), data...),
	})
	digest := types.NewEIP155Signer(chainID).Hash(transaction)
	canonical, err := rlp.EncodeToBytes(struct {
		Domain, Operation, FeeModel, AccountID string
		ChainID                                *big.Int
		From                                   common.Address
		ProviderBinding                        ProviderBinding
		Contract, Spender                      common.Address
		Amount                                 *big.Int
		TokenName, TokenSymbol                 string
		TokenDecimals                          uint8
		Nonce, GasLimit, SimulationBlockNumber uint64
		GasPrice                               *big.Int
		Data                                   []byte
		SimulationBlockHash                    common.Hash
		SimulationResultHash                   common.Hash
	}{
		Domain: planDomain, Operation: string(OperationERC20Approve), FeeModel: string(FeeLegacy), AccountID: intent.accountID,
		ChainID: chainID, From: intent.from, ProviderBinding: input.ProviderBinding,
		Contract: intent.contract, Spender: intent.spender, Amount: new(big.Int).Set(intent.amount),
		TokenName: input.Metadata.Name, TokenSymbol: input.Metadata.Symbol, TokenDecimals: input.Metadata.Decimals,
		Nonce: input.Nonce, GasLimit: input.GasLimit, GasPrice: new(big.Int).Set(input.LegacyGasPrice), Data: append([]byte(nil), data...),
		SimulationBlockNumber: input.SimulationBlockNumber, SimulationBlockHash: input.SimulationBlockHash, SimulationResultHash: input.SimulationResultHash,
	})
	if err != nil {
		return nil, &EngineError{Code: ErrorInvalidIntent, Field: "canonical approval plan", Cause: err}
	}
	return &FrozenPlan{
		accountID: intent.accountID, chainID: chainID, from: intent.from, providerBinding: input.ProviderBinding,
		operation: OperationERC20Approve, counterparty: intent.spender, amount: new(big.Int).Set(intent.amount), transaction: transaction,
		asset:    Asset{Name: input.Metadata.Name, Symbol: input.Metadata.Symbol, Decimals: input.Metadata.Decimals, Contract: intent.contract},
		planHash: crypto.Keccak256Hash(canonical), transactionDigest: digest,
		simulationBlockNumber: input.SimulationBlockNumber, simulationBlockHash: input.SimulationBlockHash, simulationResultHash: input.SimulationResultHash,
	}, nil
}

func (planner *Planner) PlanERC20ApproveDynamicFee(intent ERC20ApproveIntent, input ERC20DynamicPlanInput) (*FrozenPlan, error) {
	if intent.amount == nil || input.ProviderBinding == (ProviderBinding{}) {
		return nil, invalidIntent("ERC-20 approval intent")
	}
	if input.GasLimit < 21_000 || input.GasLimit > 30_000_000 || input.GasFeeCap == nil || input.GasFeeCap.Sign() <= 0 || input.GasFeeCap.BitLen() > 256 || input.GasTipCap == nil || input.GasTipCap.Sign() < 0 || input.GasTipCap.BitLen() > 256 || input.GasTipCap.Cmp(input.GasFeeCap) > 0 {
		return nil, invalidIntent("approval gas or fee")
	}
	if input.SimulationBlockNumber == 0 || input.SimulationBlockHash == (common.Hash{}) || input.Metadata.BlockNumber != input.SimulationBlockNumber || !validMetadataText(input.Metadata.Name, 64) || !validMetadataText(input.Metadata.Symbol, 16) || input.Metadata.Decimals > 36 {
		return nil, invalidIntent("approval metadata or simulation")
	}
	data := encodeERC20Method("approve(address,uint256)", intent.spender, intent.amount)
	chainID := new(big.Int).SetUint64(intent.chainID)
	transaction := types.NewTx(&types.DynamicFeeTx{
		ChainID: chainID, Nonce: input.Nonce,
		GasTipCap: new(big.Int).Set(input.GasTipCap), GasFeeCap: new(big.Int).Set(input.GasFeeCap),
		Gas: input.GasLimit, To: addressPointer(intent.contract), Value: new(big.Int), Data: append([]byte(nil), data...),
	})
	digest := types.NewLondonSigner(chainID).Hash(transaction)
	canonical, err := rlp.EncodeToBytes(struct {
		Domain, Operation, FeeModel, AccountID string
		ChainID                                *big.Int
		From                                   common.Address
		ProviderBinding                        ProviderBinding
		Contract, Spender                      common.Address
		Amount                                 *big.Int
		TokenName, TokenSymbol                 string
		TokenDecimals                          uint8
		Nonce, GasLimit, SimulationBlockNumber uint64
		GasFeeCap, GasTipCap                   *big.Int
		Data                                   []byte
		SimulationBlockHash                    common.Hash
		SimulationResultHash                   common.Hash
	}{
		Domain: planDomain, Operation: string(OperationERC20Approve), FeeModel: string(FeeDynamic), AccountID: intent.accountID,
		ChainID: chainID, From: intent.from, ProviderBinding: input.ProviderBinding,
		Contract: intent.contract, Spender: intent.spender, Amount: new(big.Int).Set(intent.amount),
		TokenName: input.Metadata.Name, TokenSymbol: input.Metadata.Symbol, TokenDecimals: input.Metadata.Decimals,
		Nonce: input.Nonce, GasLimit: input.GasLimit, GasFeeCap: new(big.Int).Set(input.GasFeeCap), GasTipCap: new(big.Int).Set(input.GasTipCap), Data: append([]byte(nil), data...),
		SimulationBlockNumber: input.SimulationBlockNumber, SimulationBlockHash: input.SimulationBlockHash, SimulationResultHash: input.SimulationResultHash,
	})
	if err != nil {
		return nil, &EngineError{Code: ErrorInvalidIntent, Field: "canonical approval plan", Cause: err}
	}
	return &FrozenPlan{
		accountID: intent.accountID, chainID: chainID, from: intent.from, providerBinding: input.ProviderBinding,
		operation: OperationERC20Approve, counterparty: intent.spender, amount: new(big.Int).Set(intent.amount), transaction: transaction,
		asset:    Asset{Name: input.Metadata.Name, Symbol: input.Metadata.Symbol, Decimals: input.Metadata.Decimals, Contract: intent.contract},
		planHash: crypto.Keccak256Hash(canonical), transactionDigest: digest,
		simulationBlockNumber: input.SimulationBlockNumber, simulationBlockHash: input.SimulationBlockHash, simulationResultHash: input.SimulationResultHash,
	}, nil
}

func (planner *Planner) PlanERC20TransferDynamicFee(intent ERC20TransferIntent, input ERC20DynamicPlanInput) (*FrozenPlan, error) {
	if intent.amount == nil {
		return nil, invalidIntent("ERC-20 intent")
	}
	if input.ProviderBinding == (ProviderBinding{}) {
		return nil, invalidIntent("provider binding")
	}
	if input.GasLimit < 21_000 || input.GasLimit > 30_000_000 {
		return nil, invalidIntent("gas limit")
	}
	if input.GasFeeCap == nil || input.GasFeeCap.Sign() <= 0 || input.GasFeeCap.BitLen() > 256 {
		return nil, invalidIntent("gas fee cap")
	}
	if input.GasTipCap == nil || input.GasTipCap.Sign() < 0 || input.GasTipCap.BitLen() > 256 || input.GasTipCap.Cmp(input.GasFeeCap) > 0 {
		return nil, invalidIntent("gas tip cap")
	}
	if input.SimulationBlockNumber == 0 || input.SimulationBlockHash == (common.Hash{}) || input.Metadata.BlockNumber != input.SimulationBlockNumber {
		return nil, invalidIntent("simulation block")
	}
	if !validMetadataText(input.Metadata.Name, 64) || !validMetadataText(input.Metadata.Symbol, 16) || input.Metadata.Decimals > 36 {
		return nil, invalidIntent("token metadata")
	}
	data := encodeERC20Method("transfer(address,uint256)", intent.to, intent.amount)
	chainID := new(big.Int).SetUint64(intent.chainID)
	transaction := types.NewTx(&types.DynamicFeeTx{
		ChainID: chainID, Nonce: input.Nonce,
		GasTipCap: new(big.Int).Set(input.GasTipCap), GasFeeCap: new(big.Int).Set(input.GasFeeCap),
		Gas: input.GasLimit, To: addressPointer(intent.contract), Value: new(big.Int), Data: append([]byte(nil), data...),
	})
	digest := types.NewLondonSigner(chainID).Hash(transaction)
	canonical, err := rlp.EncodeToBytes(struct {
		Domain                string
		Operation             string
		FeeModel              string
		AccountID             string
		ChainID               *big.Int
		From                  common.Address
		ProviderBinding       ProviderBinding
		Contract              common.Address
		Recipient             common.Address
		Amount                *big.Int
		TokenName             string
		TokenSymbol           string
		TokenDecimals         uint8
		Nonce                 uint64
		GasFeeCap             *big.Int
		GasTipCap             *big.Int
		GasLimit              uint64
		Data                  []byte
		SimulationBlockNumber uint64
		SimulationBlockHash   common.Hash
		SimulationResultHash  common.Hash
	}{
		Domain: planDomain, Operation: "erc20_transfer", FeeModel: "eip1559",
		AccountID: intent.accountID, ChainID: chainID, From: intent.from, ProviderBinding: input.ProviderBinding,
		Contract: intent.contract, Recipient: intent.to, Amount: new(big.Int).Set(intent.amount),
		TokenName: input.Metadata.Name, TokenSymbol: input.Metadata.Symbol, TokenDecimals: input.Metadata.Decimals,
		Nonce: input.Nonce, GasFeeCap: new(big.Int).Set(input.GasFeeCap), GasTipCap: new(big.Int).Set(input.GasTipCap),
		GasLimit: input.GasLimit, Data: append([]byte(nil), data...),
		SimulationBlockNumber: input.SimulationBlockNumber, SimulationBlockHash: input.SimulationBlockHash, SimulationResultHash: input.SimulationResultHash,
	})
	if err != nil {
		return nil, &EngineError{Code: ErrorInvalidIntent, Field: "canonical plan", Cause: err}
	}
	return &FrozenPlan{
		accountID: intent.accountID, chainID: chainID, from: intent.from, providerBinding: input.ProviderBinding,
		operation:    OperationERC20Transfer,
		counterparty: intent.to,
		amount:       new(big.Int).Set(intent.amount),
		transaction:  transaction,
		asset:        Asset{Name: input.Metadata.Name, Symbol: input.Metadata.Symbol, Decimals: input.Metadata.Decimals, Contract: intent.contract},
		planHash:     crypto.Keccak256Hash(canonical), transactionDigest: digest,
		simulationBlockNumber: input.SimulationBlockNumber, simulationBlockHash: input.SimulationBlockHash, simulationResultHash: input.SimulationResultHash,
	}, nil
}

func (planner *Planner) PlanERC721SafeTransfer(intent ERC721SafeTransferIntent, input ERC721PlanInput) (*FrozenPlan, error) {
	if intent.tokenID == nil {
		return nil, invalidIntent("ERC-721 intent")
	}
	if input.ProviderBinding == (ProviderBinding{}) {
		return nil, invalidIntent("provider binding")
	}
	if input.GasLimit < 21_000 || input.GasLimit > 30_000_000 {
		return nil, invalidIntent("gas limit")
	}
	if input.LegacyGasPrice == nil || input.LegacyGasPrice.Sign() <= 0 || input.LegacyGasPrice.BitLen() > 256 {
		return nil, invalidIntent("gas price")
	}
	if input.SimulationBlockNumber == 0 || input.SimulationBlockHash == (common.Hash{}) {
		return nil, invalidIntent("simulation block")
	}
	if input.TokenID == nil || input.TokenID.Sign() < 0 || input.TokenID.BitLen() > 256 || input.TokenID.Cmp(intent.tokenID) != 0 {
		return nil, invalidIntent("token ID")
	}
	data := encodeERC721SafeTransferMethod(intent.from, intent.to, input.TokenID)
	chainID := new(big.Int).SetUint64(intent.chainID)
	transaction := types.NewTx(&types.LegacyTx{
		Nonce: input.Nonce, GasPrice: new(big.Int).Set(input.LegacyGasPrice), Gas: input.GasLimit,
		To: addressPointer(intent.contract), Value: new(big.Int), Data: append([]byte(nil), data...),
	})
	digest := types.NewEIP155Signer(chainID).Hash(transaction)
	canonical, err := rlp.EncodeToBytes(struct {
		Domain                string
		Operation             string
		FeeModel              string
		AccountID             string
		ChainID               *big.Int
		From                  common.Address
		ProviderBinding       ProviderBinding
		Contract              common.Address
		Recipient             common.Address
		TokenID               *big.Int
		Nonce                 uint64
		GasPrice              *big.Int
		GasLimit              uint64
		Data                  []byte
		SimulationBlockNumber uint64
		SimulationBlockHash   common.Hash
		SimulationResultHash  common.Hash
	}{
		Domain: planDomain, Operation: string(OperationERC721SafeTransfer), FeeModel: string(FeeLegacy), AccountID: intent.accountID,
		ChainID: chainID, From: intent.from, ProviderBinding: input.ProviderBinding,
		Contract: intent.contract, Recipient: intent.to, TokenID: new(big.Int).Set(input.TokenID),
		Nonce: input.Nonce, GasPrice: new(big.Int).Set(input.LegacyGasPrice), GasLimit: input.GasLimit, Data: append([]byte(nil), data...),
		SimulationBlockNumber: input.SimulationBlockNumber, SimulationBlockHash: input.SimulationBlockHash, SimulationResultHash: input.SimulationResultHash,
	})
	if err != nil {
		return nil, &EngineError{Code: ErrorInvalidIntent, Field: "canonical ERC-721 plan", Cause: err}
	}
	return &FrozenPlan{
		accountID: intent.accountID, chainID: chainID, from: intent.from, providerBinding: input.ProviderBinding,
		operation: OperationERC721SafeTransfer, counterparty: intent.to, tokenID: new(big.Int).Set(input.TokenID), transaction: transaction,
		asset:    Asset{Contract: intent.contract},
		planHash: crypto.Keccak256Hash(canonical), transactionDigest: digest,
		simulationBlockNumber: input.SimulationBlockNumber, simulationBlockHash: input.SimulationBlockHash, simulationResultHash: input.SimulationResultHash,
	}, nil
}

func (planner *Planner) PlanERC721SafeTransferDynamicFee(intent ERC721SafeTransferIntent, input ERC721DynamicPlanInput) (*FrozenPlan, error) {
	if intent.tokenID == nil {
		return nil, invalidIntent("ERC-721 intent")
	}
	if input.ProviderBinding == (ProviderBinding{}) {
		return nil, invalidIntent("provider binding")
	}
	if input.GasLimit < 21_000 || input.GasLimit > 30_000_000 {
		return nil, invalidIntent("gas limit")
	}
	if input.GasFeeCap == nil || input.GasFeeCap.Sign() <= 0 || input.GasFeeCap.BitLen() > 256 {
		return nil, invalidIntent("gas fee cap")
	}
	if input.GasTipCap == nil || input.GasTipCap.Sign() < 0 || input.GasTipCap.BitLen() > 256 || input.GasTipCap.Cmp(input.GasFeeCap) > 0 {
		return nil, invalidIntent("gas tip cap")
	}
	if input.SimulationBlockNumber == 0 || input.SimulationBlockHash == (common.Hash{}) {
		return nil, invalidIntent("simulation block")
	}
	if input.TokenID == nil || input.TokenID.Sign() < 0 || input.TokenID.BitLen() > 256 || input.TokenID.Cmp(intent.tokenID) != 0 {
		return nil, invalidIntent("token ID")
	}
	data := encodeERC721SafeTransferMethod(intent.from, intent.to, input.TokenID)
	chainID := new(big.Int).SetUint64(intent.chainID)
	transaction := types.NewTx(&types.DynamicFeeTx{
		ChainID: chainID, Nonce: input.Nonce,
		GasTipCap: new(big.Int).Set(input.GasTipCap), GasFeeCap: new(big.Int).Set(input.GasFeeCap),
		Gas: input.GasLimit, To: addressPointer(intent.contract), Value: new(big.Int), Data: append([]byte(nil), data...),
	})
	digest := types.NewLondonSigner(chainID).Hash(transaction)
	canonical, err := rlp.EncodeToBytes(struct {
		Domain                string
		Operation             string
		FeeModel              string
		AccountID             string
		ChainID               *big.Int
		From                  common.Address
		ProviderBinding       ProviderBinding
		Contract              common.Address
		Recipient             common.Address
		TokenID               *big.Int
		Nonce                 uint64
		GasFeeCap             *big.Int
		GasTipCap             *big.Int
		GasLimit              uint64
		Data                  []byte
		SimulationBlockNumber uint64
		SimulationBlockHash   common.Hash
		SimulationResultHash  common.Hash
	}{
		Domain: planDomain, Operation: string(OperationERC721SafeTransfer), FeeModel: string(FeeDynamic), AccountID: intent.accountID,
		ChainID: chainID, From: intent.from, ProviderBinding: input.ProviderBinding,
		Contract: intent.contract, Recipient: intent.to, TokenID: new(big.Int).Set(input.TokenID),
		Nonce: input.Nonce, GasFeeCap: new(big.Int).Set(input.GasFeeCap), GasTipCap: new(big.Int).Set(input.GasTipCap),
		GasLimit: input.GasLimit, Data: append([]byte(nil), data...),
		SimulationBlockNumber: input.SimulationBlockNumber, SimulationBlockHash: input.SimulationBlockHash, SimulationResultHash: input.SimulationResultHash,
	})
	if err != nil {
		return nil, &EngineError{Code: ErrorInvalidIntent, Field: "canonical ERC-721 plan", Cause: err}
	}
	return &FrozenPlan{
		accountID: intent.accountID, chainID: chainID, from: intent.from, providerBinding: input.ProviderBinding,
		operation: OperationERC721SafeTransfer, counterparty: intent.to, tokenID: new(big.Int).Set(input.TokenID), transaction: transaction,
		asset:    Asset{Contract: intent.contract},
		planHash: crypto.Keccak256Hash(canonical), transactionDigest: digest,
		simulationBlockNumber: input.SimulationBlockNumber, simulationBlockHash: input.SimulationBlockHash, simulationResultHash: input.SimulationResultHash,
	}, nil
}

func (planner *Planner) PlanERC1155SafeTransfer(intent ERC1155SafeTransferIntent, input ERC721PlanInput) (*FrozenPlan, error) {
	if input.ProviderBinding == (ProviderBinding{}) || input.GasLimit < 21_000 || input.GasLimit > 30_000_000 || input.LegacyGasPrice == nil || input.LegacyGasPrice.Sign() <= 0 || input.LegacyGasPrice.BitLen() > 256 {
		return nil, invalidIntent("ERC-1155 gas or fee")
	}
	if input.SimulationBlockNumber == 0 || input.SimulationBlockHash == (common.Hash{}) || input.TokenID == nil || input.TokenID.Cmp(intent.tokenID) != 0 {
		return nil, invalidIntent("ERC-1155 token or simulation")
	}
	data := encodeERC1155SafeTransferMethod(intent.from, intent.to, intent.tokenID, intent.amount)
	chainID := new(big.Int).SetUint64(intent.chainID)
	transaction := types.NewTx(&types.LegacyTx{
		Nonce: input.Nonce, GasPrice: new(big.Int).Set(input.LegacyGasPrice), Gas: input.GasLimit,
		To: addressPointer(intent.contract), Value: new(big.Int), Data: append([]byte(nil), data...),
	})
	digest := types.NewEIP155Signer(chainID).Hash(transaction)
	canonical, err := rlp.EncodeToBytes(struct {
		Domain, Operation, FeeModel, AccountID string
		ChainID                                *big.Int
		From                                   common.Address
		ProviderBinding                        ProviderBinding
		Contract, Recipient                    common.Address
		TokenID, Amount                        *big.Int
		Nonce, GasLimit, SimulationBlockNumber uint64
		GasPrice                               *big.Int
		Data                                   []byte
		SimulationBlockHash                    common.Hash
		SimulationResultHash                   common.Hash
	}{
		Domain: planDomain, Operation: string(OperationERC1155SafeTransfer), FeeModel: string(FeeLegacy), AccountID: intent.accountID,
		ChainID: chainID, From: intent.from, ProviderBinding: input.ProviderBinding,
		Contract: intent.contract, Recipient: intent.to, TokenID: new(big.Int).Set(intent.tokenID), Amount: new(big.Int).Set(intent.amount),
		Nonce: input.Nonce, GasLimit: input.GasLimit, GasPrice: new(big.Int).Set(input.LegacyGasPrice), Data: append([]byte(nil), data...),
		SimulationBlockNumber: input.SimulationBlockNumber, SimulationBlockHash: input.SimulationBlockHash, SimulationResultHash: input.SimulationResultHash,
	})
	if err != nil {
		return nil, &EngineError{Code: ErrorInvalidIntent, Field: "canonical ERC-1155 plan", Cause: err}
	}
	return &FrozenPlan{
		accountID: intent.accountID, chainID: chainID, from: intent.from, providerBinding: input.ProviderBinding,
		operation: OperationERC1155SafeTransfer, counterparty: intent.to, amount: new(big.Int).Set(intent.amount), tokenID: new(big.Int).Set(intent.tokenID), transaction: transaction,
		asset:    Asset{Contract: intent.contract},
		planHash: crypto.Keccak256Hash(canonical), transactionDigest: digest,
		simulationBlockNumber: input.SimulationBlockNumber, simulationBlockHash: input.SimulationBlockHash, simulationResultHash: input.SimulationResultHash,
	}, nil
}

func (planner *Planner) PlanERC1155SafeTransferDynamicFee(intent ERC1155SafeTransferIntent, input ERC721DynamicPlanInput) (*FrozenPlan, error) {
	if input.ProviderBinding == (ProviderBinding{}) || input.GasLimit < 21_000 || input.GasLimit > 30_000_000 || input.GasFeeCap == nil || input.GasFeeCap.Sign() <= 0 || input.GasFeeCap.BitLen() > 256 || input.GasTipCap == nil || input.GasTipCap.Sign() < 0 || input.GasTipCap.BitLen() > 256 || input.GasTipCap.Cmp(input.GasFeeCap) > 0 {
		return nil, invalidIntent("ERC-1155 gas or fee")
	}
	if input.SimulationBlockNumber == 0 || input.SimulationBlockHash == (common.Hash{}) || input.TokenID == nil || input.TokenID.Cmp(intent.tokenID) != 0 {
		return nil, invalidIntent("ERC-1155 token or simulation")
	}
	data := encodeERC1155SafeTransferMethod(intent.from, intent.to, intent.tokenID, intent.amount)
	chainID := new(big.Int).SetUint64(intent.chainID)
	transaction := types.NewTx(&types.DynamicFeeTx{
		ChainID: chainID, Nonce: input.Nonce,
		GasTipCap: new(big.Int).Set(input.GasTipCap), GasFeeCap: new(big.Int).Set(input.GasFeeCap),
		Gas: input.GasLimit, To: addressPointer(intent.contract), Value: new(big.Int), Data: append([]byte(nil), data...),
	})
	digest := types.NewLondonSigner(chainID).Hash(transaction)
	canonical, err := rlp.EncodeToBytes(struct {
		Domain, Operation, FeeModel, AccountID string
		ChainID                                *big.Int
		From                                   common.Address
		ProviderBinding                        ProviderBinding
		Contract, Recipient                    common.Address
		TokenID, Amount                        *big.Int
		Nonce, GasLimit, SimulationBlockNumber uint64
		GasFeeCap, GasTipCap                   *big.Int
		Data                                   []byte
		SimulationBlockHash                    common.Hash
		SimulationResultHash                   common.Hash
	}{
		Domain: planDomain, Operation: string(OperationERC1155SafeTransfer), FeeModel: string(FeeDynamic), AccountID: intent.accountID,
		ChainID: chainID, From: intent.from, ProviderBinding: input.ProviderBinding,
		Contract: intent.contract, Recipient: intent.to, TokenID: new(big.Int).Set(intent.tokenID), Amount: new(big.Int).Set(intent.amount),
		Nonce: input.Nonce, GasLimit: input.GasLimit, GasFeeCap: new(big.Int).Set(input.GasFeeCap), GasTipCap: new(big.Int).Set(input.GasTipCap), Data: append([]byte(nil), data...),
		SimulationBlockNumber: input.SimulationBlockNumber, SimulationBlockHash: input.SimulationBlockHash, SimulationResultHash: input.SimulationResultHash,
	})
	if err != nil {
		return nil, &EngineError{Code: ErrorInvalidIntent, Field: "canonical ERC-1155 plan", Cause: err}
	}
	return &FrozenPlan{
		accountID: intent.accountID, chainID: chainID, from: intent.from, providerBinding: input.ProviderBinding,
		operation: OperationERC1155SafeTransfer, counterparty: intent.to, amount: new(big.Int).Set(intent.amount), tokenID: new(big.Int).Set(intent.tokenID), transaction: transaction,
		asset:    Asset{Contract: intent.contract},
		planHash: crypto.Keccak256Hash(canonical), transactionDigest: digest,
		simulationBlockNumber: input.SimulationBlockNumber, simulationBlockHash: input.SimulationBlockHash, simulationResultHash: input.SimulationResultHash,
	}, nil
}

func (planner *Planner) PlanERC1155BatchTransfer(intent ERC1155BatchTransferIntent, input ERC721PlanInput) (*FrozenPlan, error) {
	if input.ProviderBinding == (ProviderBinding{}) || input.GasLimit < 21_000 || input.GasLimit > 30_000_000 || input.LegacyGasPrice == nil || input.LegacyGasPrice.Sign() <= 0 || input.LegacyGasPrice.BitLen() > 256 {
		return nil, invalidIntent("ERC-1155 batch gas or fee")
	}
	if input.SimulationBlockNumber == 0 || input.SimulationBlockHash == (common.Hash{}) {
		return nil, invalidIntent("ERC-1155 batch simulation")
	}
	data := encodeERC1155BatchTransferMethod(intent.from, intent.to, intent.effects)
	chainID := new(big.Int).SetUint64(intent.chainID)
	transaction := types.NewTx(&types.LegacyTx{
		Nonce: input.Nonce, GasPrice: new(big.Int).Set(input.LegacyGasPrice), Gas: input.GasLimit,
		To: addressPointer(intent.contract), Value: new(big.Int), Data: append([]byte(nil), data...),
	})
	digest := types.NewEIP155Signer(chainID).Hash(transaction)
	canonical, err := rlp.EncodeToBytes(struct {
		Domain, Operation, FeeModel, AccountID string
		ChainID                                *big.Int
		From                                   common.Address
		ProviderBinding                        ProviderBinding
		Contract, Recipient                    common.Address
		Effects                                []EffectEntry
		Nonce, GasLimit, SimulationBlockNumber uint64
		GasPrice                               *big.Int
		Data                                   []byte
		SimulationBlockHash                    common.Hash
		SimulationResultHash                   common.Hash
	}{
		Domain: planDomain, Operation: string(OperationERC1155BatchTransfer), FeeModel: string(FeeLegacy), AccountID: intent.accountID,
		ChainID: chainID, From: intent.from, ProviderBinding: input.ProviderBinding,
		Contract: intent.contract, Recipient: intent.to, Effects: CloneEffectEntries(intent.effects),
		Nonce: input.Nonce, GasLimit: input.GasLimit, GasPrice: new(big.Int).Set(input.LegacyGasPrice), Data: append([]byte(nil), data...),
		SimulationBlockNumber: input.SimulationBlockNumber, SimulationBlockHash: input.SimulationBlockHash, SimulationResultHash: input.SimulationResultHash,
	})
	if err != nil {
		return nil, &EngineError{Code: ErrorInvalidIntent, Field: "canonical ERC-1155 batch plan", Cause: err}
	}
	return &FrozenPlan{
		accountID: intent.accountID, chainID: chainID, from: intent.from, providerBinding: input.ProviderBinding,
		operation: OperationERC1155BatchTransfer, counterparty: intent.to, effects: CloneEffectEntries(intent.effects), transaction: transaction,
		asset:    Asset{Contract: intent.contract},
		planHash: crypto.Keccak256Hash(canonical), transactionDigest: digest,
		simulationBlockNumber: input.SimulationBlockNumber, simulationBlockHash: input.SimulationBlockHash, simulationResultHash: input.SimulationResultHash,
	}, nil
}

func (planner *Planner) PlanERC1155BatchTransferDynamicFee(intent ERC1155BatchTransferIntent, input ERC721DynamicPlanInput) (*FrozenPlan, error) {
	if input.ProviderBinding == (ProviderBinding{}) || input.GasLimit < 21_000 || input.GasLimit > 30_000_000 || input.GasFeeCap == nil || input.GasFeeCap.Sign() <= 0 || input.GasFeeCap.BitLen() > 256 || input.GasTipCap == nil || input.GasTipCap.Sign() < 0 || input.GasTipCap.BitLen() > 256 || input.GasTipCap.Cmp(input.GasFeeCap) > 0 {
		return nil, invalidIntent("ERC-1155 batch gas or fee")
	}
	if input.SimulationBlockNumber == 0 || input.SimulationBlockHash == (common.Hash{}) {
		return nil, invalidIntent("ERC-1155 batch simulation")
	}
	data := encodeERC1155BatchTransferMethod(intent.from, intent.to, intent.effects)
	chainID := new(big.Int).SetUint64(intent.chainID)
	transaction := types.NewTx(&types.DynamicFeeTx{
		ChainID: chainID, Nonce: input.Nonce,
		GasTipCap: new(big.Int).Set(input.GasTipCap), GasFeeCap: new(big.Int).Set(input.GasFeeCap),
		Gas: input.GasLimit, To: addressPointer(intent.contract), Value: new(big.Int), Data: append([]byte(nil), data...),
	})
	digest := types.NewLondonSigner(chainID).Hash(transaction)
	canonical, err := rlp.EncodeToBytes(struct {
		Domain, Operation, FeeModel, AccountID string
		ChainID                                *big.Int
		From                                   common.Address
		ProviderBinding                        ProviderBinding
		Contract, Recipient                    common.Address
		Effects                                []EffectEntry
		Nonce, GasLimit, SimulationBlockNumber uint64
		GasFeeCap, GasTipCap                   *big.Int
		Data                                   []byte
		SimulationBlockHash                    common.Hash
		SimulationResultHash                   common.Hash
	}{
		Domain: planDomain, Operation: string(OperationERC1155BatchTransfer), FeeModel: string(FeeDynamic), AccountID: intent.accountID,
		ChainID: chainID, From: intent.from, ProviderBinding: input.ProviderBinding,
		Contract: intent.contract, Recipient: intent.to, Effects: CloneEffectEntries(intent.effects),
		Nonce: input.Nonce, GasLimit: input.GasLimit, GasFeeCap: new(big.Int).Set(input.GasFeeCap), GasTipCap: new(big.Int).Set(input.GasTipCap), Data: append([]byte(nil), data...),
		SimulationBlockNumber: input.SimulationBlockNumber, SimulationBlockHash: input.SimulationBlockHash, SimulationResultHash: input.SimulationResultHash,
	})
	if err != nil {
		return nil, &EngineError{Code: ErrorInvalidIntent, Field: "canonical ERC-1155 batch plan", Cause: err}
	}
	return &FrozenPlan{
		accountID: intent.accountID, chainID: chainID, from: intent.from, providerBinding: input.ProviderBinding,
		operation: OperationERC1155BatchTransfer, counterparty: intent.to, effects: CloneEffectEntries(intent.effects), transaction: transaction,
		asset:    Asset{Contract: intent.contract},
		planHash: crypto.Keccak256Hash(canonical), transactionDigest: digest,
		simulationBlockNumber: input.SimulationBlockNumber, simulationBlockHash: input.SimulationBlockHash, simulationResultHash: input.SimulationResultHash,
	}, nil
}

func encodeERC1155SafeTransferMethod(from, to common.Address, tokenID, amount *big.Int) []byte {
	selector := crypto.Keccak256([]byte("safeTransferFrom(address,address,uint256,uint256,bytes)"))[:4]
	data := make([]byte, 0, 164)
	data = append(data, selector...)
	data = append(data, common.LeftPadBytes(from.Bytes(), 32)...)
	data = append(data, common.LeftPadBytes(to.Bytes(), 32)...)
	data = append(data, common.LeftPadBytes(tokenID.Bytes(), 32)...)
	data = append(data, common.LeftPadBytes(amount.Bytes(), 32)...)
	data = append(data, common.LeftPadBytes(big.NewInt(0x80).Bytes(), 32)...)
	data = append(data, make([]byte, 32)...)
	return data
}

func encodeERC1155BatchTransferMethod(from, to common.Address, effects []EffectEntry) []byte {
	selector := crypto.Keccak256([]byte("safeBatchTransferFrom(address,address,uint256[],uint256[],bytes)"))[:4]
	count := uint64(len(effects))
	data := make([]byte, 0, 196+int(count)*64)
	data = append(data, selector...)
	data = append(data, common.LeftPadBytes(from.Bytes(), 32)...)
	data = append(data, common.LeftPadBytes(to.Bytes(), 32)...)
	data = append(data, common.LeftPadBytes(big.NewInt(0x80).Bytes(), 32)...)
	data = append(data, common.LeftPadBytes(big.NewInt(int64(0x80+32+count*32)).Bytes(), 32)...)
	data = append(data, common.LeftPadBytes(big.NewInt(int64(0x80+64+count*64)).Bytes(), 32)...)
	data = append(data, common.LeftPadBytes(big.NewInt(int64(count)).Bytes(), 32)...)
	for _, effect := range effects {
		data = append(data, common.LeftPadBytes(effect.TokenID.Bytes(), 32)...)
	}
	data = append(data, common.LeftPadBytes(big.NewInt(int64(count)).Bytes(), 32)...)
	for _, effect := range effects {
		data = append(data, common.LeftPadBytes(effect.Amount.Bytes(), 32)...)
	}
	data = append(data, make([]byte, 32)...)
	return data
}

func (planner *Planner) PlanContractCall(intent ContractCallIntent, input NativePlanInput) (*FrozenPlan, error) {
	if intent.calldata == nil || intent.value == nil {
		return nil, invalidIntent("contract call intent")
	}
	if input.ProviderBinding == (ProviderBinding{}) || input.GasLimit < 21_000 || input.GasLimit > 30_000_000 || input.LegacyGasPrice == nil || input.LegacyGasPrice.Sign() <= 0 || input.LegacyGasPrice.BitLen() > 256 {
		return nil, invalidIntent("contract call gas or fee")
	}
	if input.SimulationBlockNumber == 0 || input.SimulationBlockHash == (common.Hash{}) {
		return nil, invalidIntent("contract call simulation")
	}
	chainID := new(big.Int).SetUint64(intent.chainID)
	transaction := types.NewTx(&types.LegacyTx{
		Nonce: input.Nonce, GasPrice: new(big.Int).Set(input.LegacyGasPrice), Gas: input.GasLimit,
		To: addressPointer(intent.contract), Value: new(big.Int).Set(intent.value), Data: append([]byte(nil), intent.calldata...),
	})
	digest := types.NewEIP155Signer(chainID).Hash(transaction)
	canonical, err := rlp.EncodeToBytes(struct {
		Domain, Operation, FeeModel, AccountID string
		ChainID                                *big.Int
		From, Contract                         common.Address
		ProviderBinding                        ProviderBinding
		Value                                  *big.Int
		ABISource, ABIHash, Method             string
		Calldata                               []byte
		Nonce, GasLimit, SimulationBlockNumber uint64
		GasPrice                               *big.Int
		SimulationBlockHash                    common.Hash
		SimulationResultHash                   common.Hash
	}{
		Domain: planDomain, Operation: string(OperationContractCall), FeeModel: string(FeeLegacy), AccountID: intent.accountID,
		ChainID: chainID, From: intent.from, Contract: intent.contract, ProviderBinding: input.ProviderBinding,
		Value: new(big.Int).Set(intent.value), ABISource: string(intent.abiSource), ABIHash: intent.abiHash.Hex(), Method: intent.method,
		Calldata: append([]byte(nil), intent.calldata...),
		Nonce:    input.Nonce, GasLimit: input.GasLimit, GasPrice: new(big.Int).Set(input.LegacyGasPrice),
		SimulationBlockNumber: input.SimulationBlockNumber, SimulationBlockHash: input.SimulationBlockHash, SimulationResultHash: input.SimulationResultHash,
	})
	if err != nil {
		return nil, &EngineError{Code: ErrorInvalidIntent, Field: "canonical contract call plan", Cause: err}
	}
	return &FrozenPlan{
		accountID: intent.accountID, chainID: chainID, from: intent.from, providerBinding: input.ProviderBinding,
		operation: OperationContractCall, counterparty: intent.contract, amount: new(big.Int).Set(intent.value), transaction: transaction,
		asset:    Asset{Contract: intent.contract},
		planHash: crypto.Keccak256Hash(canonical), transactionDigest: digest,
		simulationBlockNumber: input.SimulationBlockNumber, simulationBlockHash: input.SimulationBlockHash, simulationResultHash: input.SimulationResultHash,
	}, nil
}

func (planner *Planner) PlanContractCallDynamicFee(intent ContractCallIntent, input DynamicFeePlanInput) (*FrozenPlan, error) {
	if intent.calldata == nil || intent.value == nil {
		return nil, invalidIntent("contract call intent")
	}
	if input.ProviderBinding == (ProviderBinding{}) || input.GasLimit < 21_000 || input.GasLimit > 30_000_000 || input.GasFeeCap == nil || input.GasFeeCap.Sign() <= 0 || input.GasFeeCap.BitLen() > 256 || input.GasTipCap == nil || input.GasTipCap.Sign() < 0 || input.GasTipCap.BitLen() > 256 || input.GasTipCap.Cmp(input.GasFeeCap) > 0 {
		return nil, invalidIntent("contract call gas or fee")
	}
	if input.SimulationBlockNumber == 0 || input.SimulationBlockHash == (common.Hash{}) {
		return nil, invalidIntent("contract call simulation")
	}
	chainID := new(big.Int).SetUint64(intent.chainID)
	transaction := types.NewTx(&types.DynamicFeeTx{
		ChainID: chainID, Nonce: input.Nonce,
		GasTipCap: new(big.Int).Set(input.GasTipCap), GasFeeCap: new(big.Int).Set(input.GasFeeCap),
		Gas: input.GasLimit, To: addressPointer(intent.contract), Value: new(big.Int).Set(intent.value), Data: append([]byte(nil), intent.calldata...),
	})
	digest := types.NewLondonSigner(chainID).Hash(transaction)
	canonical, err := rlp.EncodeToBytes(struct {
		Domain, Operation, FeeModel, AccountID string
		ChainID                                *big.Int
		From, Contract                         common.Address
		ProviderBinding                        ProviderBinding
		Value                                  *big.Int
		ABISource, ABIHash, Method             string
		Calldata                               []byte
		Nonce, GasLimit, SimulationBlockNumber uint64
		GasFeeCap, GasTipCap                   *big.Int
		SimulationBlockHash                    common.Hash
		SimulationResultHash                   common.Hash
	}{
		Domain: planDomain, Operation: string(OperationContractCall), FeeModel: string(FeeDynamic), AccountID: intent.accountID,
		ChainID: chainID, From: intent.from, Contract: intent.contract, ProviderBinding: input.ProviderBinding,
		Value: new(big.Int).Set(intent.value), ABISource: string(intent.abiSource), ABIHash: intent.abiHash.Hex(), Method: intent.method,
		Calldata: append([]byte(nil), intent.calldata...),
		Nonce:    input.Nonce, GasLimit: input.GasLimit, GasFeeCap: new(big.Int).Set(input.GasFeeCap), GasTipCap: new(big.Int).Set(input.GasTipCap),
		SimulationBlockNumber: input.SimulationBlockNumber, SimulationBlockHash: input.SimulationBlockHash, SimulationResultHash: input.SimulationResultHash,
	})
	if err != nil {
		return nil, &EngineError{Code: ErrorInvalidIntent, Field: "canonical contract call plan", Cause: err}
	}
	return &FrozenPlan{
		accountID: intent.accountID, chainID: chainID, from: intent.from, providerBinding: input.ProviderBinding,
		operation: OperationContractCall, counterparty: intent.contract, amount: new(big.Int).Set(intent.value), transaction: transaction,
		asset:    Asset{Contract: intent.contract},
		planHash: crypto.Keccak256Hash(canonical), transactionDigest: digest,
		simulationBlockNumber: input.SimulationBlockNumber, simulationBlockHash: input.SimulationBlockHash, simulationResultHash: input.SimulationResultHash,
	}, nil
}

func encodeERC721SafeTransferMethod(from, to common.Address, tokenID *big.Int) []byte {
	selector := crypto.Keccak256([]byte("safeTransferFrom(address,address,uint256)"))[:4]
	data := make([]byte, 0, 100)
	data = append(data, selector...)
	data = append(data, common.LeftPadBytes(from.Bytes(), 32)...)
	data = append(data, common.LeftPadBytes(to.Bytes(), 32)...)
	data = append(data, common.LeftPadBytes(tokenID.Bytes(), 32)...)
	return data
}

func encodeERC20Method(signature string, address common.Address, amount *big.Int) []byte {
	selector := crypto.Keccak256([]byte(signature))[:4]
	data := make([]byte, 0, 68)
	data = append(data, selector...)
	data = append(data, common.LeftPadBytes(address.Bytes(), 32)...)
	data = append(data, common.LeftPadBytes(amount.Bytes(), 32)...)
	return data
}

func validMetadataText(value string, maxRunes int) bool {
	runes := []rune(value)
	if len(runes) == 0 || len(runes) > maxRunes {
		return false
	}
	for _, character := range runes {
		if unicode.IsControl(character) || !unicode.IsPrint(character) {
			return false
		}
	}
	return true
}

func (planner *Planner) PlanNativeDynamicFee(intent NativeTransferIntent, input DynamicFeePlanInput) (*FrozenPlan, error) {
	if intent.amount == nil {
		return nil, invalidIntent("native intent")
	}
	if input.ProviderBinding == (ProviderBinding{}) {
		return nil, invalidIntent("provider binding")
	}
	if input.GasLimit < 21_000 || input.GasLimit > 30_000_000 {
		return nil, invalidIntent("gas limit")
	}
	if input.GasFeeCap == nil || input.GasFeeCap.Sign() <= 0 || input.GasFeeCap.BitLen() > 256 {
		return nil, invalidIntent("gas fee cap")
	}
	if input.GasTipCap == nil || input.GasTipCap.Sign() < 0 || input.GasTipCap.BitLen() > 256 || input.GasTipCap.Cmp(input.GasFeeCap) > 0 {
		return nil, invalidIntent("gas tip cap")
	}
	if input.SimulationBlockNumber == 0 || input.SimulationBlockHash == (common.Hash{}) {
		return nil, invalidIntent("simulation block")
	}
	chainID := new(big.Int).SetUint64(intent.chainID)
	transaction := types.NewTx(&types.DynamicFeeTx{
		ChainID:   chainID,
		Nonce:     input.Nonce,
		GasTipCap: new(big.Int).Set(input.GasTipCap),
		GasFeeCap: new(big.Int).Set(input.GasFeeCap),
		Gas:       input.GasLimit,
		To:        addressPointer(intent.to),
		Value:     new(big.Int).Set(intent.amount),
	})
	digest := types.NewLondonSigner(chainID).Hash(transaction)
	canonical, err := rlp.EncodeToBytes(struct {
		Domain                string
		FeeModel              string
		AccountID             string
		ChainID               *big.Int
		From                  common.Address
		ProviderBinding       ProviderBinding
		Nonce                 uint64
		GasFeeCap             *big.Int
		GasTipCap             *big.Int
		GasLimit              uint64
		To                    common.Address
		Value                 *big.Int
		Data                  []byte
		SimulationBlockNumber uint64
		SimulationBlockHash   common.Hash
		SimulationResultHash  common.Hash
	}{
		Domain:                planDomain,
		FeeModel:              "eip1559",
		AccountID:             intent.accountID,
		ChainID:               chainID,
		From:                  intent.from,
		ProviderBinding:       input.ProviderBinding,
		Nonce:                 input.Nonce,
		GasFeeCap:             new(big.Int).Set(input.GasFeeCap),
		GasTipCap:             new(big.Int).Set(input.GasTipCap),
		GasLimit:              input.GasLimit,
		To:                    intent.to,
		Value:                 new(big.Int).Set(intent.amount),
		SimulationBlockNumber: input.SimulationBlockNumber,
		SimulationBlockHash:   input.SimulationBlockHash,
		SimulationResultHash:  input.SimulationResultHash,
	})
	if err != nil {
		return nil, &EngineError{Code: ErrorInvalidIntent, Field: "canonical plan", Cause: err}
	}
	planHash := crypto.Keccak256Hash(canonical)
	return &FrozenPlan{
		accountID:             intent.accountID,
		chainID:               chainID,
		from:                  intent.from,
		providerBinding:       input.ProviderBinding,
		operation:             OperationNativeTransfer,
		counterparty:          intent.to,
		amount:                new(big.Int).Set(intent.amount),
		transaction:           transaction,
		planHash:              planHash,
		transactionDigest:     digest,
		simulationBlockNumber: input.SimulationBlockNumber,
		simulationBlockHash:   input.SimulationBlockHash,
		simulationResultHash:  input.SimulationResultHash,
	}, nil
}

func addressPointer(address common.Address) *common.Address {
	value := address
	return &value
}

func (plan *FrozenPlan) AccountID() string { return plan.accountID }
func (plan *FrozenPlan) ChainID() *big.Int {
	if plan == nil || plan.chainID == nil {
		return nil
	}
	return new(big.Int).Set(plan.chainID)
}
func (plan *FrozenPlan) From() common.Address { return plan.from }
func (plan *FrozenPlan) Operation() Operation {
	if plan == nil {
		return ""
	}
	return plan.operation
}
func (plan *FrozenPlan) Counterparty() common.Address {
	if plan == nil {
		return common.Address{}
	}
	return plan.counterparty
}
func (plan *FrozenPlan) Amount() *big.Int {
	if plan == nil || plan.amount == nil {
		return nil
	}
	return new(big.Int).Set(plan.amount)
}
func (plan *FrozenPlan) TokenID() *big.Int {
	if plan == nil || plan.tokenID == nil {
		return nil
	}
	return new(big.Int).Set(plan.tokenID)
}
func (plan *FrozenPlan) Effects() []EffectEntry {
	if plan == nil {
		return nil
	}
	return CloneEffectEntries(plan.effects)
}
func (plan *FrozenPlan) ContractCallPreview() ContractCallPreview {
	if plan == nil {
		return ContractCallPreview{}
	}
	return plan.contractCallPreview
}
func (plan *FrozenPlan) ProviderBinding() ProviderBinding {
	if plan == nil {
		return ProviderBinding{}
	}
	return plan.providerBinding
}
func (plan *FrozenPlan) Transaction() *types.Transaction {
	if plan == nil || plan.transaction == nil {
		return nil
	}
	encoded, err := plan.transaction.MarshalBinary()
	if err != nil {
		return nil
	}
	var transaction types.Transaction
	if err := transaction.UnmarshalBinary(encoded); err != nil {
		return nil
	}
	return &transaction
}
func (plan *FrozenPlan) Asset() Asset {
	if plan == nil {
		return Asset{}
	}
	return plan.asset
}
func (plan *FrozenPlan) PlanHash() [32]byte          { return plan.planHash }
func (plan *FrozenPlan) TransactionDigest() [32]byte { return plan.transactionDigest }
func (plan *FrozenPlan) SimulationBlock() (uint64, common.Hash) {
	return plan.simulationBlockNumber, plan.simulationBlockHash
}
func (plan *FrozenPlan) SimulationResultHash() common.Hash {
	if plan == nil {
		return common.Hash{}
	}
	return plan.simulationResultHash
}
func (plan *FrozenPlan) MaximumGasCost() *big.Int {
	if plan == nil || plan.transaction == nil {
		return nil
	}
	price := plan.transaction.GasPrice()
	if plan.transaction.Type() == types.DynamicFeeTxType {
		price = plan.transaction.GasFeeCap()
	}
	return new(big.Int).Mul(new(big.Int).SetUint64(plan.transaction.Gas()), price)
}
