package evm

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"math/big"
	"time"

	"blocowallet/internal/wallet"

	"github.com/ethereum/go-ethereum/common"
)

type EconomicPolicy struct {
	MaxGasPrice          *big.Int
	MaxFeePerGas         *big.Int
	MaxPriorityFeePerGas *big.Int
	MaxGasLimit          uint64
	MaxGasCost           *big.Int
	MaxTotalNativeDebit  *big.Int
}

type EngineOptions struct {
	Now            func() time.Time
	NewID          func() (string, error)
	ReservationTTL time.Duration
	ApprovalTTL    time.Duration
	EconomicPolicy EconomicPolicy
}

type PrepareNativeRequest struct {
	OperationID    string
	PlanGeneration uint64
	AccountID      string
	ChainID        uint64
	From           common.Address
	To             common.Address
	Amount         *big.Int
}

type PrepareERC20TransferRequest struct {
	OperationID    string
	PlanGeneration uint64
	AccountID      string
	ChainID        uint64
	From           common.Address
	Contract       common.Address
	To             common.Address
	Amount         *big.Int
}

type PrepareERC20ApproveRequest struct {
	OperationID    string
	PlanGeneration uint64
	AccountID      string
	ChainID        uint64
	From           common.Address
	Contract       common.Address
	Spender        common.Address
	Amount         *big.Int
}

type PrepareERC721SafeTransferRequest struct {
	OperationID    string
	PlanGeneration uint64
	AccountID      string
	ChainID        uint64
	From           common.Address
	Contract       common.Address
	To             common.Address
	TokenID        *big.Int
}

type PrepareERC1155SafeTransferRequest struct {
	OperationID    string
	PlanGeneration uint64
	AccountID      string
	ChainID        uint64
	From           common.Address
	Contract       common.Address
	To             common.Address
	TokenID        *big.Int
	Amount         *big.Int
}

type PrepareERC1155BatchTransferRequest struct {
	OperationID    string
	PlanGeneration uint64
	AccountID      string
	ChainID        uint64
	From           common.Address
	Contract       common.Address
	To             common.Address
	Effects        []EffectEntry
}

type PreparedNativeTransfer struct {
	plan        *FrozenPlan
	reservation NonceReservation
	simulation  SimulationResult
	fees        FeeSuggestion
	findings    []RiskFinding
}

type ApprovalRequest struct {
	AuthorizationEpoch uint64
	RiskLevel          RiskLevel
	ConfirmationLevel  ConfirmationLevel
	ConfirmationTarget uint64
}

type ExecutionResult struct {
	TransactionID string
	Hash          common.Hash
	Raw           []byte
}

type Engine struct {
	repository TransactionRepository
	rpc        RPC
	planner    *Planner
	simulator  *Simulator
	feeOracle  *FeeOracle
	signer     *SigningAdapter
	options    EngineOptions
}

func NewEngine(repository TransactionRepository, rpc RPC, signer ApprovedDigestSigner, options EngineOptions) (*Engine, error) {
	if repository == nil || rpc == nil || signer == nil {
		return nil, fmt.Errorf("EVM engine dependencies are required")
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.NewID == nil {
		options.NewID = secureEngineUUID
	}
	if options.ReservationTTL <= 0 || options.ReservationTTL > 10*time.Minute {
		return nil, fmt.Errorf("EVM reservation TTL is outside policy")
	}
	if options.ApprovalTTL <= 0 || options.ApprovalTTL > 5*time.Minute {
		return nil, fmt.Errorf("EVM approval TTL is outside policy")
	}
	policy, err := normalizeEconomicPolicy(options.EconomicPolicy)
	if err != nil {
		return nil, err
	}
	options.EconomicPolicy = policy
	return &Engine{
		repository: repository,
		rpc:        rpc,
		planner:    NewPlanner(),
		simulator:  NewSimulator(),
		feeOracle:  NewFeeOracle(),
		signer:     NewSigningAdapter(signer),
		options:    options,
	}, nil
}

func (engine *Engine) PrepareERC20Approve(ctx context.Context, request PrepareERC20ApproveRequest) (*PreparedNativeTransfer, error) {
	if engine == nil || engine.repository == nil || engine.rpc == nil {
		return nil, &EngineError{Code: ErrorInvalidIntent, Field: "engine"}
	}
	intent, err := NewERC20ApproveIntent(request.AccountID, request.ChainID, request.From, request.Contract, request.Spender, request.Amount)
	if err != nil {
		return nil, err
	}
	if !accountIDPattern.MatchString(request.OperationID) || request.PlanGeneration == 0 {
		return nil, invalidIntent("operation identity")
	}
	if engine.rpc.ChainID() != request.ChainID || engine.rpc.ProviderBinding() == (ProviderBinding{}) {
		return nil, &EngineError{Code: ErrorProviderUnavailable, Field: "provider binding"}
	}
	header, err := engine.rpc.LatestHeader(ctx)
	if err != nil {
		return nil, &EngineError{Code: ErrorProviderUnavailable, Field: "latest header", Cause: err}
	}
	metadata, err := NewTokenMetadataResolver().Resolve(ctx, engine.rpc, request.From, request.Contract, header.BlockIdentity)
	if err != nil {
		return nil, err
	}
	currentAllowance, err := currentERC20Allowance(ctx, engine.rpc, intent.from, intent.contract, intent.spender, header.BlockIdentity)
	if err != nil {
		return nil, err
	}
	if currentAllowance.Sign() > 0 && intent.amount.Sign() > 0 {
		return nil, &EngineError{Code: ErrorPolicyDenied, Field: "revoke existing allowance before setting a new non-zero value"}
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
	call := TransactionCall{From: intent.from, To: intent.contract, Value: new(big.Int), Input: encodeERC20Method("approve(address,uint256)", intent.spender, intent.amount)}
	applyFeeSuggestion(&call, fees)
	simulation, err := engine.simulator.Simulate(ctx, engine.rpc, call, header.BlockIdentity)
	if err != nil {
		invalidate()
		return nil, err
	}
	if err := validateERC20SimulationResult(simulation.ReturnData); err != nil {
		invalidate()
		return nil, err
	}
	if err := validateEconomics(engine.options.EconomicPolicy, fees, simulation, header, nil); err != nil {
		invalidate()
		return nil, err
	}
	findings, err := NewRiskPolicy().Evaluate(OperationERC20Approve, intent.spender, intent.amount, simulation)
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
		plan, err = engine.planner.PlanERC20Approve(intent, ERC20PlanInput{
			NativePlanInput: NativePlanInput{
				ProviderBinding: engine.rpc.ProviderBinding(), Nonce: reservation.Nonce, GasLimit: simulation.GasLimit,
				LegacyGasPrice: fees.GasPrice, SimulationBlockNumber: header.Number, SimulationBlockHash: header.Hash,
				SimulationResultHash: commitment,
			},
			Metadata: metadata,
		})
	case FeeDynamic:
		plan, err = engine.planner.PlanERC20ApproveDynamicFee(intent, ERC20DynamicPlanInput{
			DynamicFeePlanInput: DynamicFeePlanInput{
				ProviderBinding: engine.rpc.ProviderBinding(), Nonce: reservation.Nonce, GasLimit: simulation.GasLimit,
				GasFeeCap: fees.GasFeeCap, GasTipCap: fees.GasTipCap,
				SimulationBlockNumber: header.Number, SimulationBlockHash: header.Hash,
				SimulationResultHash: commitment,
			},
			Metadata: metadata,
		})
	default:
		err = invalidIntent("ERC-20 approval fee model")
	}
	if err != nil {
		invalidate()
		return nil, err
	}
	return &PreparedNativeTransfer{plan: plan, reservation: reservation, simulation: simulation, fees: fees, findings: append([]RiskFinding(nil), findings...)}, nil
}

func (engine *Engine) PrepareERC20Transfer(ctx context.Context, request PrepareERC20TransferRequest) (*PreparedNativeTransfer, error) {
	if engine == nil || engine.repository == nil || engine.rpc == nil {
		return nil, &EngineError{Code: ErrorInvalidIntent, Field: "engine"}
	}
	intent, err := NewERC20TransferIntent(request.AccountID, request.ChainID, request.From, request.Contract, request.To, request.Amount)
	if err != nil {
		return nil, err
	}
	if !accountIDPattern.MatchString(request.OperationID) || request.PlanGeneration == 0 {
		return nil, invalidIntent("operation identity")
	}
	if engine.rpc.ChainID() != request.ChainID || engine.rpc.ProviderBinding() == (ProviderBinding{}) {
		return nil, &EngineError{Code: ErrorProviderUnavailable, Field: "provider binding"}
	}
	header, err := engine.rpc.LatestHeader(ctx)
	if err != nil {
		return nil, &EngineError{Code: ErrorProviderUnavailable, Field: "latest header", Cause: err}
	}
	metadata, err := NewTokenMetadataResolver().Resolve(ctx, engine.rpc, request.From, request.Contract, header.BlockIdentity)
	if err != nil {
		return nil, err
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
		From: intent.from, To: intent.contract, Value: new(big.Int),
		Input: encodeERC20Method("transfer(address,uint256)", intent.to, intent.amount),
	}
	applyFeeSuggestion(&call, fees)
	simulation, err := engine.simulator.Simulate(ctx, engine.rpc, call, header.BlockIdentity)
	if err != nil {
		invalidate()
		return nil, err
	}
	if err := validateERC20SimulationResult(simulation.ReturnData); err != nil {
		invalidate()
		return nil, err
	}
	if err := validateEconomics(engine.options.EconomicPolicy, fees, simulation, header, nil); err != nil {
		invalidate()
		return nil, err
	}
	findings, err := NewRiskPolicy().Evaluate(OperationERC20Transfer, intent.to, intent.amount, simulation)
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
		plan, err = engine.planner.PlanERC20Transfer(intent, ERC20PlanInput{
			NativePlanInput: NativePlanInput{
				ProviderBinding: engine.rpc.ProviderBinding(), Nonce: reservation.Nonce, GasLimit: simulation.GasLimit,
				LegacyGasPrice: fees.GasPrice, SimulationBlockNumber: header.Number, SimulationBlockHash: header.Hash,
				SimulationResultHash: commitment,
			},
			Metadata: metadata,
		})
	case FeeDynamic:
		plan, err = engine.planner.PlanERC20TransferDynamicFee(intent, ERC20DynamicPlanInput{
			DynamicFeePlanInput: DynamicFeePlanInput{
				ProviderBinding: engine.rpc.ProviderBinding(), Nonce: reservation.Nonce, GasLimit: simulation.GasLimit,
				GasFeeCap: fees.GasFeeCap, GasTipCap: fees.GasTipCap,
				SimulationBlockNumber: header.Number, SimulationBlockHash: header.Hash,
				SimulationResultHash: commitment,
			},
			Metadata: metadata,
		})
	default:
		err = invalidIntent("ERC-20 fee model")
	}
	if err != nil {
		invalidate()
		return nil, err
	}
	return &PreparedNativeTransfer{plan: plan, reservation: reservation, simulation: simulation, fees: fees, findings: append([]RiskFinding(nil), findings...)}, nil
}

func (engine *Engine) PrepareERC721SafeTransfer(ctx context.Context, request PrepareERC721SafeTransferRequest) (*PreparedNativeTransfer, error) {
	if engine == nil || engine.repository == nil || engine.rpc == nil {
		return nil, &EngineError{Code: ErrorInvalidIntent, Field: "engine"}
	}
	intent, err := NewERC721SafeTransferIntent(request.AccountID, request.ChainID, request.From, request.Contract, request.To, request.TokenID)
	if err != nil {
		return nil, err
	}
	if !accountIDPattern.MatchString(request.OperationID) || request.PlanGeneration == 0 {
		return nil, invalidIntent("operation identity")
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
		return nil, &EngineError{Code: ErrorProviderUnavailable, Field: "token contract code", Cause: err}
	}
	if len(code) == 0 {
		return nil, &EngineError{Code: ErrorPolicyDenied, Field: "token contract has no code"}
	}
	ownerData := make([]byte, 0, 36)
	ownerData = append(ownerData, common.FromHex("0x6352211e")...)
	ownerData = append(ownerData, common.LeftPadBytes(intent.tokenID.Bytes(), 32)...)
	ownerResult, err := engine.rpc.CallContract(ctx, TransactionCall{From: request.From, To: request.Contract, Value: new(big.Int), Input: ownerData}, header.BlockIdentity)
	if err != nil {
		return nil, &EngineError{Code: ErrorProviderUnavailable, Field: "token owner query", Cause: err}
	}
	if len(ownerResult) != 32 || common.BytesToAddress(ownerResult) != request.From {
		return nil, &EngineError{Code: ErrorPolicyDenied, Field: "token owner mismatch"}
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
		From: intent.from, To: intent.contract, Value: new(big.Int),
		Input: encodeERC721SafeTransferMethod(intent.from, intent.to, intent.tokenID),
	}
	applyFeeSuggestion(&call, fees)
	simulation, err := engine.simulator.Simulate(ctx, engine.rpc, call, header.BlockIdentity)
	if err != nil {
		invalidate()
		return nil, err
	}
	if err := validateERC721SimulationResult(simulation.ReturnData); err != nil {
		invalidate()
		return nil, err
	}
	if err := validateEconomics(engine.options.EconomicPolicy, fees, simulation, header, nil); err != nil {
		invalidate()
		return nil, err
	}
	findings, err := NewRiskPolicy().Evaluate(OperationERC721SafeTransfer, intent.to, intent.tokenID, simulation)
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
		plan, err = engine.planner.PlanERC721SafeTransfer(intent, ERC721PlanInput{
			NativePlanInput: NativePlanInput{
				ProviderBinding: engine.rpc.ProviderBinding(), Nonce: reservation.Nonce, GasLimit: simulation.GasLimit,
				LegacyGasPrice: fees.GasPrice, SimulationBlockNumber: header.Number, SimulationBlockHash: header.Hash,
				SimulationResultHash: commitment,
			},
			TokenID: intent.tokenID,
		})
	case FeeDynamic:
		plan, err = engine.planner.PlanERC721SafeTransferDynamicFee(intent, ERC721DynamicPlanInput{
			DynamicFeePlanInput: DynamicFeePlanInput{
				ProviderBinding: engine.rpc.ProviderBinding(), Nonce: reservation.Nonce, GasLimit: simulation.GasLimit,
				GasFeeCap: fees.GasFeeCap, GasTipCap: fees.GasTipCap,
				SimulationBlockNumber: header.Number, SimulationBlockHash: header.Hash,
				SimulationResultHash: commitment,
			},
			TokenID: intent.tokenID,
		})
	default:
		err = invalidIntent("ERC-721 fee model")
	}
	if err != nil {
		invalidate()
		return nil, err
	}
	return &PreparedNativeTransfer{plan: plan, reservation: reservation, simulation: simulation, fees: fees, findings: append([]RiskFinding(nil), findings...)}, nil
}

func validateERC721SimulationResult(returnData []byte) error {
	if len(returnData) != 0 {
		return &EngineError{Code: ErrorSimulationFailed, Field: "ERC-721 return data"}
	}
	return nil
}

func (engine *Engine) PrepareERC1155SafeTransfer(ctx context.Context, request PrepareERC1155SafeTransferRequest) (*PreparedNativeTransfer, error) {
	if engine == nil || engine.repository == nil || engine.rpc == nil {
		return nil, &EngineError{Code: ErrorInvalidIntent, Field: "engine"}
	}
	intent, err := NewERC1155SafeTransferIntent(request.AccountID, request.ChainID, request.From, request.Contract, request.To, request.TokenID, request.Amount)
	if err != nil {
		return nil, err
	}
	if !accountIDPattern.MatchString(request.OperationID) || request.PlanGeneration == 0 {
		return nil, invalidIntent("operation identity")
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
		return nil, &EngineError{Code: ErrorProviderUnavailable, Field: "token contract code", Cause: err}
	}
	if len(code) == 0 {
		return nil, &EngineError{Code: ErrorPolicyDenied, Field: "token contract has no code"}
	}
	balanceData := make([]byte, 0, 68)
	balanceData = append(balanceData, common.FromHex("0x00fdd58e")...)
	balanceData = append(balanceData, common.LeftPadBytes(request.From.Bytes(), 32)...)
	balanceData = append(balanceData, common.LeftPadBytes(intent.tokenID.Bytes(), 32)...)
	balanceResult, err := engine.rpc.CallContract(ctx, TransactionCall{From: request.From, To: request.Contract, Value: new(big.Int), Input: balanceData}, header.BlockIdentity)
	if err != nil {
		return nil, &EngineError{Code: ErrorProviderUnavailable, Field: "token balance query", Cause: err}
	}
	if len(balanceResult) != 32 || new(big.Int).SetBytes(balanceResult).Cmp(intent.amount) < 0 {
		return nil, &EngineError{Code: ErrorPolicyDenied, Field: "token balance mismatch"}
	}
	return engine.prepareERC1155Single(ctx, intent, header, request.OperationID, request.PlanGeneration)
}

func (engine *Engine) prepareERC1155Single(ctx context.Context, intent ERC1155SafeTransferIntent, header BlockHeader, operationID string, planGeneration uint64) (*PreparedNativeTransfer, error) {
	pendingNonce, err := engine.rpc.PendingNonceAt(ctx, intent.from)
	if err != nil {
		return nil, &EngineError{Code: ErrorProviderUnavailable, Field: "pending nonce", Cause: err}
	}
	reservationID, err := engine.options.NewID()
	if err != nil {
		return nil, &EngineError{Code: ErrorNonceConflict, Field: "reservation ID", Cause: err}
	}
	now := engine.options.Now().UTC()
	reservation, err := engine.repository.ReserveNonce(ctx, ReserveNonceRequest{
		ReservationID: reservationID, OperationID: operationID, AccountID: intent.accountID,
		Sender: intent.from, ChainID: intent.chainID, PendingNonce: pendingNonce,
		PlanGeneration: planGeneration, ReservedAt: now, ExpiresAt: now.Add(engine.options.ReservationTTL),
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
		From: intent.from, To: intent.contract, Value: new(big.Int),
		Input: encodeERC1155SafeTransferMethod(intent.from, intent.to, intent.tokenID, intent.amount),
	}
	applyFeeSuggestion(&call, fees)
	simulation, err := engine.simulator.Simulate(ctx, engine.rpc, call, header.BlockIdentity)
	if err != nil {
		invalidate()
		return nil, err
	}
	if err := validateERC721SimulationResult(simulation.ReturnData); err != nil {
		invalidate()
		return nil, err
	}
	if err := validateEconomics(engine.options.EconomicPolicy, fees, simulation, header, nil); err != nil {
		invalidate()
		return nil, err
	}
	findings, err := NewRiskPolicy().Evaluate(OperationERC1155SafeTransfer, intent.to, intent.amount, simulation)
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
		plan, err = engine.planner.PlanERC1155SafeTransfer(intent, ERC721PlanInput{
			NativePlanInput: NativePlanInput{
				ProviderBinding: engine.rpc.ProviderBinding(), Nonce: reservation.Nonce, GasLimit: simulation.GasLimit,
				LegacyGasPrice: fees.GasPrice, SimulationBlockNumber: header.Number, SimulationBlockHash: header.Hash,
				SimulationResultHash: commitment,
			},
			TokenID: intent.tokenID,
		})
	case FeeDynamic:
		plan, err = engine.planner.PlanERC1155SafeTransferDynamicFee(intent, ERC721DynamicPlanInput{
			DynamicFeePlanInput: DynamicFeePlanInput{
				ProviderBinding: engine.rpc.ProviderBinding(), Nonce: reservation.Nonce, GasLimit: simulation.GasLimit,
				GasFeeCap: fees.GasFeeCap, GasTipCap: fees.GasTipCap,
				SimulationBlockNumber: header.Number, SimulationBlockHash: header.Hash,
				SimulationResultHash: commitment,
			},
			TokenID: intent.tokenID,
		})
	default:
		err = invalidIntent("ERC-1155 fee model")
	}
	if err != nil {
		invalidate()
		return nil, err
	}
	return &PreparedNativeTransfer{plan: plan, reservation: reservation, simulation: simulation, fees: fees, findings: append([]RiskFinding(nil), findings...)}, nil
}

func (engine *Engine) PrepareERC1155BatchTransfer(ctx context.Context, request PrepareERC1155BatchTransferRequest) (*PreparedNativeTransfer, error) {
	if engine == nil || engine.repository == nil || engine.rpc == nil {
		return nil, &EngineError{Code: ErrorInvalidIntent, Field: "engine"}
	}
	intent, err := NewERC1155BatchTransferIntent(request.AccountID, request.ChainID, request.From, request.Contract, request.To, request.Effects)
	if err != nil {
		return nil, err
	}
	if !accountIDPattern.MatchString(request.OperationID) || request.PlanGeneration == 0 {
		return nil, invalidIntent("operation identity")
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
		return nil, &EngineError{Code: ErrorProviderUnavailable, Field: "token contract code", Cause: err}
	}
	if len(code) == 0 {
		return nil, &EngineError{Code: ErrorPolicyDenied, Field: "token contract has no code"}
	}
	return engine.prepareERC1155Batch(ctx, intent, header, request.OperationID, request.PlanGeneration)
}

func (engine *Engine) prepareERC1155Batch(ctx context.Context, intent ERC1155BatchTransferIntent, header BlockHeader, operationID string, planGeneration uint64) (*PreparedNativeTransfer, error) {
	pendingNonce, err := engine.rpc.PendingNonceAt(ctx, intent.from)
	if err != nil {
		return nil, &EngineError{Code: ErrorProviderUnavailable, Field: "pending nonce", Cause: err}
	}
	reservationID, err := engine.options.NewID()
	if err != nil {
		return nil, &EngineError{Code: ErrorNonceConflict, Field: "reservation ID", Cause: err}
	}
	now := engine.options.Now().UTC()
	reservation, err := engine.repository.ReserveNonce(ctx, ReserveNonceRequest{
		ReservationID: reservationID, OperationID: operationID, AccountID: intent.accountID,
		Sender: intent.from, ChainID: intent.chainID, PendingNonce: pendingNonce,
		PlanGeneration: planGeneration, ReservedAt: now, ExpiresAt: now.Add(engine.options.ReservationTTL),
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
		From: intent.from, To: intent.contract, Value: new(big.Int),
		Input: encodeERC1155BatchTransferMethod(intent.from, intent.to, intent.effects),
	}
	applyFeeSuggestion(&call, fees)
	simulation, err := engine.simulator.Simulate(ctx, engine.rpc, call, header.BlockIdentity)
	if err != nil {
		invalidate()
		return nil, err
	}
	if err := validateERC721SimulationResult(simulation.ReturnData); err != nil {
		invalidate()
		return nil, err
	}
	if err := validateEconomics(engine.options.EconomicPolicy, fees, simulation, header, nil); err != nil {
		invalidate()
		return nil, err
	}
	findings, err := NewRiskPolicy().Evaluate(OperationERC1155BatchTransfer, intent.to, new(big.Int), simulation)
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
		plan, err = engine.planner.PlanERC1155BatchTransfer(intent, ERC721PlanInput{
			NativePlanInput: NativePlanInput{
				ProviderBinding: engine.rpc.ProviderBinding(), Nonce: reservation.Nonce, GasLimit: simulation.GasLimit,
				LegacyGasPrice: fees.GasPrice, SimulationBlockNumber: header.Number, SimulationBlockHash: header.Hash,
				SimulationResultHash: commitment,
			},
		})
	case FeeDynamic:
		plan, err = engine.planner.PlanERC1155BatchTransferDynamicFee(intent, ERC721DynamicPlanInput{
			DynamicFeePlanInput: DynamicFeePlanInput{
				ProviderBinding: engine.rpc.ProviderBinding(), Nonce: reservation.Nonce, GasLimit: simulation.GasLimit,
				GasFeeCap: fees.GasFeeCap, GasTipCap: fees.GasTipCap,
				SimulationBlockNumber: header.Number, SimulationBlockHash: header.Hash,
				SimulationResultHash: commitment,
			},
		})
	default:
		err = invalidIntent("ERC-1155 batch fee model")
	}
	if err != nil {
		invalidate()
		return nil, err
	}
	return &PreparedNativeTransfer{plan: plan, reservation: reservation, simulation: simulation, fees: fees, findings: append([]RiskFinding(nil), findings...)}, nil
}

func (engine *Engine) PrepareNative(ctx context.Context, request PrepareNativeRequest) (*PreparedNativeTransfer, error) {
	if engine == nil || engine.repository == nil || engine.rpc == nil {
		return nil, &EngineError{Code: ErrorInvalidIntent, Field: "engine"}
	}
	intent, err := NewNativeTransferIntent(request.AccountID, request.ChainID, request.From, request.To, request.Amount)
	if err != nil {
		return nil, err
	}
	if !accountIDPattern.MatchString(request.OperationID) || request.PlanGeneration == 0 {
		return nil, invalidIntent("operation identity")
	}
	if engine.rpc.ChainID() != request.ChainID || engine.rpc.ProviderBinding() == (ProviderBinding{}) {
		return nil, &EngineError{Code: ErrorProviderUnavailable, Field: "provider binding"}
	}
	header, err := engine.rpc.LatestHeader(ctx)
	if err != nil {
		return nil, &EngineError{Code: ErrorProviderUnavailable, Field: "latest header", Cause: err}
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
	invalidate := func(reason string) {
		_ = engine.repository.InvalidateUnsignedReservation(context.Background(), InvalidateReservationRequest{
			ReservationID: reservation.ReservationID, AccountID: reservation.AccountID,
			PlanGeneration: reservation.PlanGeneration, InvalidatedAt: engine.options.Now().UTC(), Reason: reason,
		})
	}
	fees, err := engine.feeOracle.Suggest(ctx, engine.rpc, header)
	if err != nil {
		invalidate("plan_stale")
		return nil, err
	}
	call := TransactionCall{From: intent.from, To: intent.to, Value: new(big.Int).Set(intent.amount)}
	applyFeeSuggestion(&call, fees)
	simulation, err := engine.simulator.Simulate(ctx, engine.rpc, call, header.BlockIdentity)
	if err != nil {
		invalidate("plan_stale")
		return nil, err
	}
	if err := validateEconomics(engine.options.EconomicPolicy, fees, simulation, header, intent.amount); err != nil {
		invalidate("plan_stale")
		return nil, err
	}
	findings, err := NewRiskPolicy().Evaluate(OperationNativeTransfer, intent.to, intent.amount, simulation)
	if err != nil {
		invalidate("plan_stale")
		return nil, err
	}
	commitment, err := simulationPolicyCommitment(simulation, findings)
	if err != nil {
		invalidate("plan_stale")
		return nil, &EngineError{Code: ErrorPolicyDenied, Field: "simulation policy commitment", Cause: err}
	}
	var plan *FrozenPlan
	switch fees.Model {
	case FeeLegacy:
		plan, err = engine.planner.PlanNative(intent, NativePlanInput{
			ProviderBinding: engine.rpc.ProviderBinding(), Nonce: reservation.Nonce, GasLimit: simulation.GasLimit,
			LegacyGasPrice: fees.GasPrice, SimulationBlockNumber: header.Number, SimulationBlockHash: header.Hash,
			SimulationResultHash: commitment,
		})
	case FeeDynamic:
		plan, err = engine.planner.PlanNativeDynamicFee(intent, DynamicFeePlanInput{
			ProviderBinding: engine.rpc.ProviderBinding(), Nonce: reservation.Nonce, GasLimit: simulation.GasLimit,
			GasFeeCap: fees.GasFeeCap, GasTipCap: fees.GasTipCap,
			SimulationBlockNumber: header.Number, SimulationBlockHash: header.Hash,
			SimulationResultHash: commitment,
		})
	default:
		err = invalidIntent("fee model")
	}
	if err != nil {
		invalidate("plan_stale")
		return nil, err
	}
	return &PreparedNativeTransfer{plan: plan, reservation: reservation, simulation: simulation, fees: fees, findings: append([]RiskFinding(nil), findings...)}, nil
}

func currentERC20Allowance(ctx context.Context, rpc RPC, owner, contract, spender common.Address, block BlockIdentity) (*big.Int, error) {
	data := make([]byte, 0, 68)
	data = append(data, common.FromHex("0xdd62ed3e")...)
	data = append(data, common.LeftPadBytes(owner.Bytes(), 32)...)
	data = append(data, common.LeftPadBytes(spender.Bytes(), 32)...)
	result, err := rpc.CallContract(ctx, TransactionCall{From: owner, To: contract, Value: new(big.Int), Input: data}, block)
	if err != nil {
		return nil, &EngineError{Code: ErrorProviderUnavailable, Field: "current token allowance", Cause: err}
	}
	if len(result) != 32 {
		return nil, &EngineError{Code: ErrorPolicyDenied, Field: "token allowance response"}
	}
	return new(big.Int).SetBytes(result), nil
}

func validateERC20SimulationResult(returnData []byte) error {
	if len(returnData) != 32 {
		return &EngineError{Code: ErrorSimulationFailed, Field: "ERC-20 return data"}
	}
	value := new(big.Int).SetBytes(returnData)
	if value.Cmp(big.NewInt(1)) != 0 {
		return &EngineError{Code: ErrorSimulationFailed, Field: "ERC-20 returned false or non-canonical bool"}
	}
	return nil
}

func applyFeeSuggestion(call *TransactionCall, fees FeeSuggestion) {
	if fees.Model == FeeLegacy {
		call.GasPrice = new(big.Int).Set(fees.GasPrice)
		return
	}
	call.MaxFeePerGas = new(big.Int).Set(fees.GasFeeCap)
	call.MaxPriorityFeePerGas = new(big.Int).Set(fees.GasTipCap)
}

func (engine *Engine) ApproveSignAndBroadcast(ctx context.Context, handle wallet.CapabilityHandle, prepared *PreparedNativeTransfer, request ApprovalRequest) (ExecutionResult, error) {
	if engine == nil || prepared == nil || prepared.plan == nil {
		return ExecutionResult{}, invalidIntent("prepared transfer")
	}
	if request.AuthorizationEpoch == 0 {
		return ExecutionResult{}, invalidIntent("authorization epoch")
	}
	confirmationTarget := request.ConfirmationTarget
	if confirmationTarget == 0 {
		confirmationTarget = 12
	}
	if confirmationTarget > 10_000 {
		return ExecutionResult{}, invalidIntent("confirmation target")
	}
	riskLevel := RiskNormal
	for _, finding := range prepared.findings {
		if finding.Severity == RiskSeverityCritical {
			riskLevel = RiskCritical
			break
		}
		if finding.Severity == RiskSeverityWarning {
			riskLevel = RiskWarning
		}
	}
	if riskLevel == RiskCritical && request.ConfirmationLevel != ConfirmationReinforced {
		return ExecutionResult{}, &EngineError{Code: ErrorPolicyDenied, Field: "critical risk confirmation"}
	}
	if prepared.plan.ProviderBinding() != engine.rpc.ProviderBinding() || prepared.plan.ChainID().Uint64() != engine.rpc.ChainID() {
		return ExecutionResult{}, &EngineError{Code: ErrorPlanStale, Field: "provider binding"}
	}
	now := engine.options.Now().UTC()
	if !now.Before(prepared.reservation.ExpiresAt) {
		_ = engine.CancelPrepared(context.Background(), prepared, "plan_stale")
		return ExecutionResult{}, &EngineError{Code: ErrorPlanStale, Field: "reservation expiry"}
	}
	blockNumber, blockHash := prepared.plan.SimulationBlock()
	canonicalHeader, found, err := engine.rpc.HeaderByNumber(ctx, blockNumber)
	if err != nil {
		return ExecutionResult{}, &EngineError{Code: ErrorProviderUnavailable, Field: "simulation block revalidation", Cause: err}
	}
	if !found || canonicalHeader.Hash != blockHash {
		_ = engine.CancelPrepared(context.Background(), prepared, "plan_stale")
		return ExecutionResult{}, &EngineError{Code: ErrorPlanStale, Field: "simulation block"}
	}
	if err := engine.revalidatePreparedExecution(ctx, prepared); err != nil {
		_ = engine.CancelPrepared(context.Background(), prepared, "plan_stale")
		return ExecutionResult{}, err
	}
	pendingNonce, err := engine.rpc.PendingNonceAt(ctx, prepared.plan.From())
	if err != nil {
		return ExecutionResult{}, &EngineError{Code: ErrorProviderUnavailable, Field: "nonce revalidation", Cause: err}
	}
	if pendingNonce > prepared.reservation.Nonce {
		_ = engine.CancelPrepared(context.Background(), prepared, "plan_stale")
		return ExecutionResult{}, &EngineError{Code: ErrorPlanStale, Field: "pending nonce"}
	}
	approvalID, err := engine.options.NewID()
	if err != nil {
		return ExecutionResult{}, &EngineError{Code: ErrorPolicyDenied, Field: "approval ID", Cause: err}
	}
	transactionID, err := engine.options.NewID()
	if err != nil {
		return ExecutionResult{}, &EngineError{Code: ErrorPolicyDenied, Field: "transaction ID", Cause: err}
	}
	approval := SigningApproval{
		ApprovalID: approvalID, ReservationID: prepared.reservation.ReservationID,
		AccountID: prepared.plan.AccountID(), Sender: prepared.plan.From(), ChainID: prepared.plan.ChainID().Uint64(),
		Nonce: prepared.reservation.Nonce, AuthorizationEpoch: request.AuthorizationEpoch,
		PlanHash: prepared.plan.PlanHash(), TransactionDigest: prepared.plan.TransactionDigest(),
		RiskLevel: riskLevel, ConfirmationLevel: request.ConfirmationLevel, ConfirmationTarget: confirmationTarget,
		CreatedAt: now, ConfirmedAt: now, ExpiresAt: now.Add(engine.options.ApprovalTTL),
	}
	if err := ValidateSigningApproval(approval); err != nil {
		return ExecutionResult{}, err
	}
	if err := engine.repository.IssueApproval(ctx, approval); err != nil {
		return ExecutionResult{}, err
	}
	assetAmount := prepared.plan.Amount()
	var tokenID *big.Int
	var effects []EffectEntry
	switch prepared.plan.Operation() {
	case OperationERC721SafeTransfer:
		assetAmount = prepared.plan.TokenID()
	case OperationERC1155SafeTransfer:
		tokenID = prepared.plan.TokenID()
	case OperationERC1155BatchTransfer:
		assetAmount = new(big.Int)
		effects = prepared.plan.Effects()
	}
	_, err = engine.repository.AuthorizeSigning(ctx, AuthorizeSigningRequest{
		TransactionID: transactionID, ApprovalID: approvalID, ReservationID: approval.ReservationID,
		AccountID: approval.AccountID, Sender: approval.Sender, ChainID: approval.ChainID, Nonce: approval.Nonce,
		AuthorizationEpoch: approval.AuthorizationEpoch, PlanHash: approval.PlanHash,
		TransactionDigest: approval.TransactionDigest, Operation: prepared.plan.Operation(), Counterparty: prepared.plan.Counterparty(),
		AssetContract: prepared.plan.Asset().Contract, AssetAmount: assetAmount, TokenID: tokenID, Effects: effects, ConfirmationTarget: confirmationTarget, AuthorizedAt: now,
	})
	if err != nil {
		return ExecutionResult{}, err
	}
	signed, err := engine.signer.Sign(ctx, handle, prepared.plan, approval)
	if err != nil {
		_ = engine.repository.RecordSigningFailure(context.Background(), SigningFailureRequest{
			TransactionID: transactionID, FailedAt: engine.options.Now().UTC(), ResultCode: "signer_rejected",
		})
		return ExecutionResult{}, err
	}
	attempt, err := engine.repository.BeginFirstBroadcast(ctx, FirstBroadcastRequest{
		TransactionID: transactionID, SignedPayload: signed.Raw(), StartedAt: engine.options.Now().UTC(),
	})
	if err != nil {
		_ = engine.repository.RecordSigningFailure(context.Background(), SigningFailureRequest{
			TransactionID: transactionID, FailedAt: engine.options.Now().UTC(), ResultCode: "persistence_failed",
		})
		return ExecutionResult{}, err
	}
	remoteHash, err := engine.rpc.SendRawTransaction(ctx, attempt.SignedPayload)
	if err != nil {
		resultCode := "remote_rejected"
		var broadcastError *BroadcastError
		if errors.As(err, &broadcastError) && (broadcastError.Kind == BroadcastFailureAmbiguous || broadcastError.Kind == BroadcastFailureNonceLow) {
			resultCode = "transport_unknown"
		}
		_ = engine.repository.RecordBroadcastResult(context.Background(), BroadcastResult{
			TransactionID: transactionID, Hash: attempt.Hash, ResultCode: resultCode, CompletedAt: engine.options.Now().UTC(),
		})
		result := ExecutionResult{TransactionID: transactionID, Hash: attempt.Hash, Raw: append([]byte(nil), attempt.SignedPayload...)}
		return result, &EngineError{Code: ErrorBroadcastRejected, Field: resultCode, Cause: err}
	}
	if remoteHash != attempt.Hash || remoteHash != signed.Hash() {
		_ = engine.repository.RecordBroadcastResult(context.Background(), BroadcastResult{
			TransactionID: transactionID, Hash: attempt.Hash, ResultCode: "remote_rejected", CompletedAt: engine.options.Now().UTC(),
		})
		result := ExecutionResult{TransactionID: transactionID, Hash: attempt.Hash, Raw: append([]byte(nil), attempt.SignedPayload...)}
		return result, &EngineError{Code: ErrorBroadcastRejected, Field: "transaction hash"}
	}
	result := ExecutionResult{TransactionID: transactionID, Hash: remoteHash, Raw: signed.Raw()}
	if err := engine.repository.RecordBroadcastResult(ctx, BroadcastResult{
		TransactionID: transactionID, Hash: remoteHash, Accepted: true, ResultCode: "accepted", CompletedAt: engine.options.Now().UTC(),
	}); err != nil {
		return result, err
	}
	return result, nil
}

func (engine *Engine) revalidatePreparedExecution(ctx context.Context, prepared *PreparedNativeTransfer) error {
	header, err := engine.rpc.LatestHeader(ctx)
	if err != nil {
		return &EngineError{Code: ErrorProviderUnavailable, Field: "latest execution head", Cause: err}
	}
	transaction := prepared.plan.Transaction()
	if transaction == nil || transaction.To() == nil {
		return &EngineError{Code: ErrorPlanStale, Field: "prepared transaction"}
	}
	call := TransactionCall{
		From: prepared.plan.From(), To: *transaction.To(), Value: transaction.Value(),
		Input: transaction.Data(), Gas: transaction.Gas(),
	}
	if transaction.Type() == 2 {
		call.MaxFeePerGas = transaction.GasFeeCap()
		call.MaxPriorityFeePerGas = transaction.GasTipCap()
		if header.BaseFeePerGas != nil && header.BaseFeePerGas.Cmp(call.MaxFeePerGas) > 0 {
			return &EngineError{Code: ErrorPlanStale, Field: "base fee exceeded cap"}
		}
	} else {
		call.GasPrice = transaction.GasPrice()
	}
	returnData, err := engine.rpc.CallContract(ctx, call, header.BlockIdentity)
	if err != nil {
		return &EngineError{Code: ErrorPlanStale, Field: "fresh execution simulation", Cause: err}
	}
	if prepared.plan.Operation() == OperationERC20Transfer || prepared.plan.Operation() == OperationERC20Approve {
		if err := validateERC20SimulationResult(returnData); err != nil {
			return &EngineError{Code: ErrorPlanStale, Field: "fresh ERC-20 simulation", Cause: err}
		}
	}
	if prepared.plan.Operation() == OperationERC721SafeTransfer {
		if err := validateERC721SimulationResult(returnData); err != nil {
			return &EngineError{Code: ErrorPlanStale, Field: "fresh ERC-721 simulation", Cause: err}
		}
	}
	return nil
}

func (engine *Engine) CancelPrepared(ctx context.Context, prepared *PreparedNativeTransfer, reason string) error {
	if engine == nil || engine.repository == nil || prepared == nil || prepared.plan == nil {
		return invalidIntent("prepared cancellation")
	}
	if reason != "user_cancelled" && reason != "plan_stale" {
		return invalidIntent("prepared cancellation reason")
	}
	return engine.repository.InvalidateUnsignedReservation(ctx, InvalidateReservationRequest{
		ReservationID:  prepared.reservation.ReservationID,
		AccountID:      prepared.reservation.AccountID,
		PlanGeneration: prepared.reservation.PlanGeneration,
		InvalidatedAt:  engine.options.Now().UTC(),
		Reason:         reason,
	})
}

func (engine *Engine) Rebroadcast(ctx context.Context, transactionID string) (ExecutionResult, error) {
	if engine == nil || engine.repository == nil || engine.rpc == nil {
		return ExecutionResult{}, invalidIntent("rebroadcast engine")
	}
	attempt, err := engine.repository.BeginRebroadcast(ctx, transactionID, engine.options.Now().UTC())
	if err != nil {
		return ExecutionResult{}, err
	}
	if attempt.ChainID == 0 || attempt.ChainID != engine.rpc.ChainID() {
		return ExecutionResult{}, &EngineError{Code: ErrorPlanStale, Field: "rebroadcast chain identity"}
	}
	remoteHash, err := engine.rpc.SendRawTransaction(ctx, attempt.SignedPayload)
	if err != nil || remoteHash != attempt.Hash {
		resultCode := "remote_rejected"
		var broadcastError *BroadcastError
		if errors.As(err, &broadcastError) && (broadcastError.Kind == BroadcastFailureAmbiguous || broadcastError.Kind == BroadcastFailureNonceLow) {
			resultCode = "transport_unknown"
		}
		_ = engine.repository.RecordBroadcastResult(context.Background(), BroadcastResult{
			TransactionID: transactionID, Hash: attempt.Hash, ResultCode: resultCode, CompletedAt: engine.options.Now().UTC(),
		})
		result := ExecutionResult{TransactionID: transactionID, Hash: attempt.Hash, Raw: append([]byte(nil), attempt.SignedPayload...)}
		return result, &EngineError{Code: ErrorBroadcastRejected, Field: "rebroadcast " + resultCode, Cause: err}
	}
	result := ExecutionResult{TransactionID: transactionID, Hash: remoteHash, Raw: append([]byte(nil), attempt.SignedPayload...)}
	if err := engine.repository.RecordBroadcastResult(ctx, BroadcastResult{
		TransactionID: transactionID, Hash: remoteHash, Accepted: true, ResultCode: "accepted", CompletedAt: engine.options.Now().UTC(),
	}); err != nil {
		return result, err
	}
	return result, nil
}

func (engine *Engine) TrackTransaction(ctx context.Context, transactionID string, confirmationTarget uint64, observedAt time.Time) (TrackingResult, error) {
	if engine == nil || engine.repository == nil || engine.rpc == nil {
		return TrackingResult{}, invalidIntent("receipt tracker")
	}
	return NewReceiptTracker(engine.repository, engine.rpc).TrackOnce(ctx, transactionID, confirmationTarget, observedAt)
}

func (prepared *PreparedNativeTransfer) Plan() *FrozenPlan {
	if prepared == nil {
		return nil
	}
	return prepared.plan
}

func (prepared *PreparedNativeTransfer) Reservation() NonceReservation {
	if prepared == nil {
		return NonceReservation{}
	}
	return prepared.reservation
}

func (prepared *PreparedNativeTransfer) Simulation() SimulationResult {
	if prepared == nil {
		return SimulationResult{}
	}
	result := prepared.simulation
	result.ReturnData = append([]byte(nil), prepared.simulation.ReturnData...)
	return result
}

func (prepared *PreparedNativeTransfer) Findings() []RiskFinding {
	if prepared == nil {
		return nil
	}
	return append([]RiskFinding(nil), prepared.findings...)
}

func (prepared *PreparedNativeTransfer) Fees() FeeSuggestion {
	if prepared == nil {
		return FeeSuggestion{}
	}
	return cloneFeeSuggestion(prepared.fees)
}

func cloneFeeSuggestion(source FeeSuggestion) FeeSuggestion {
	result := FeeSuggestion{Model: source.Model}
	if source.GasPrice != nil {
		result.GasPrice = new(big.Int).Set(source.GasPrice)
	}
	if source.GasFeeCap != nil {
		result.GasFeeCap = new(big.Int).Set(source.GasFeeCap)
	}
	if source.GasTipCap != nil {
		result.GasTipCap = new(big.Int).Set(source.GasTipCap)
	}
	if source.BaseFee != nil {
		result.BaseFee = new(big.Int).Set(source.BaseFee)
	}
	return result
}

func normalizeEconomicPolicy(policy EconomicPolicy) (EconomicPolicy, error) {
	defaults := EconomicPolicy{
		MaxGasPrice: big.NewInt(10_000_000_000_000), MaxFeePerGas: big.NewInt(10_000_000_000_000),
		MaxPriorityFeePerGas: big.NewInt(1_000_000_000_000), MaxGasLimit: 10_000_000,
		MaxGasCost:          new(big.Int).Exp(big.NewInt(10), big.NewInt(17), nil),
		MaxTotalNativeDebit: new(big.Int).Exp(big.NewInt(10), big.NewInt(24), nil),
	}
	if policy.MaxGasPrice == nil {
		policy.MaxGasPrice = defaults.MaxGasPrice
	}
	if policy.MaxFeePerGas == nil {
		policy.MaxFeePerGas = defaults.MaxFeePerGas
	}
	if policy.MaxPriorityFeePerGas == nil {
		policy.MaxPriorityFeePerGas = defaults.MaxPriorityFeePerGas
	}
	if policy.MaxGasLimit == 0 {
		policy.MaxGasLimit = defaults.MaxGasLimit
	}
	if policy.MaxGasCost == nil {
		policy.MaxGasCost = defaults.MaxGasCost
	}
	if policy.MaxTotalNativeDebit == nil {
		policy.MaxTotalNativeDebit = defaults.MaxTotalNativeDebit
	}
	for _, value := range []*big.Int{policy.MaxGasPrice, policy.MaxFeePerGas, policy.MaxPriorityFeePerGas, policy.MaxGasCost, policy.MaxTotalNativeDebit} {
		if value.Sign() <= 0 || value.BitLen() > 256 {
			return EconomicPolicy{}, fmt.Errorf("EVM economic policy is outside bounds")
		}
	}
	if policy.MaxGasLimit < 21_000 || policy.MaxGasLimit > 30_000_000 || policy.MaxPriorityFeePerGas.Cmp(policy.MaxFeePerGas) > 0 {
		return EconomicPolicy{}, fmt.Errorf("EVM economic policy is inconsistent")
	}
	policy.MaxGasPrice = new(big.Int).Set(policy.MaxGasPrice)
	policy.MaxFeePerGas = new(big.Int).Set(policy.MaxFeePerGas)
	policy.MaxPriorityFeePerGas = new(big.Int).Set(policy.MaxPriorityFeePerGas)
	policy.MaxGasCost = new(big.Int).Set(policy.MaxGasCost)
	policy.MaxTotalNativeDebit = new(big.Int).Set(policy.MaxTotalNativeDebit)
	return policy, nil
}

func validateEconomics(policy EconomicPolicy, fees FeeSuggestion, simulation SimulationResult, header BlockHeader, nativeValue *big.Int) error {
	if simulation.GasLimit > policy.MaxGasLimit || simulation.GasLimit > header.GasLimit {
		return &EngineError{Code: ErrorPolicyDenied, Field: "gas limit"}
	}
	var perGas *big.Int
	switch fees.Model {
	case FeeLegacy:
		if fees.GasPrice == nil || fees.GasPrice.Cmp(policy.MaxGasPrice) > 0 {
			return &EngineError{Code: ErrorPolicyDenied, Field: "gas price"}
		}
		perGas = fees.GasPrice
	case FeeDynamic:
		if fees.GasFeeCap == nil || fees.GasTipCap == nil || fees.GasFeeCap.Cmp(policy.MaxFeePerGas) > 0 || fees.GasTipCap.Cmp(policy.MaxPriorityFeePerGas) > 0 {
			return &EngineError{Code: ErrorPolicyDenied, Field: "dynamic fee"}
		}
		perGas = fees.GasFeeCap
	default:
		return invalidIntent("fee model")
	}
	gasCost := new(big.Int).Mul(new(big.Int).SetUint64(simulation.GasLimit), perGas)
	if gasCost.Cmp(policy.MaxGasCost) > 0 {
		return &EngineError{Code: ErrorPolicyDenied, Field: "maximum gas cost"}
	}
	if nativeValue != nil {
		total := new(big.Int).Add(new(big.Int).Set(nativeValue), gasCost)
		if total.Cmp(policy.MaxTotalNativeDebit) > 0 {
			return &EngineError{Code: ErrorPolicyDenied, Field: "maximum native debit"}
		}
	}
	return nil
}

func NewOperationID() (string, error) {
	return secureEngineUUID()
}

func secureEngineUUID() (string, error) {
	value := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, value); err != nil {
		return "", err
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		value[0:4], value[4:6], value[6:8], value[8:10], value[10:16]), nil
}
