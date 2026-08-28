package evm

import (
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rlp"
)

type RiskFindingID string

type RiskSeverity string

const (
	RiskFindingNewRecipient        RiskFindingID = "new_recipient"
	RiskFindingTraceUnavailable    RiskFindingID = "trace_unavailable"
	RiskFindingTokenApproval       RiskFindingID = "token_spender_permission"
	RiskFindingContractCall        RiskFindingID = "contract_call"
	RiskFindingNativeValueTransfer RiskFindingID = "native_value_transfer"

	RiskSeverityWarning  RiskSeverity = "warning"
	RiskSeverityCritical RiskSeverity = "critical"
)

type RiskFinding struct {
	ID       RiskFindingID
	Severity RiskSeverity
	Subject  common.Address
}

type RiskPolicy struct{}

func NewRiskPolicy() *RiskPolicy { return &RiskPolicy{} }

func (policy *RiskPolicy) Evaluate(operation Operation, counterparty common.Address, amount *big.Int, simulation SimulationResult) ([]RiskFinding, error) {
	if counterparty == (common.Address{}) || amount == nil || amount.Sign() < 0 {
		return nil, invalidIntent("risk context")
	}
	findings := make([]RiskFinding, 0, 2)
	switch operation {
	case OperationNativeTransfer:
		findings = append(findings, RiskFinding{ID: RiskFindingNewRecipient, Severity: RiskSeverityWarning, Subject: counterparty})
	case OperationERC20Transfer, OperationERC721SafeTransfer, OperationERC1155SafeTransfer, OperationERC1155BatchTransfer:
		if simulation.Trace == nil {
			findings = append(findings, RiskFinding{ID: RiskFindingTraceUnavailable, Severity: RiskSeverityWarning, Subject: counterparty})
		}
	case OperationContractCall:
		findings = append(findings, RiskFinding{ID: RiskFindingContractCall, Severity: RiskSeverityWarning, Subject: counterparty})
		if simulation.Trace == nil {
			findings = append(findings, RiskFinding{ID: RiskFindingTraceUnavailable, Severity: RiskSeverityWarning, Subject: counterparty})
		}
		if amount != nil && amount.Sign() > 0 {
			findings = append(findings, RiskFinding{ID: RiskFindingNativeValueTransfer, Severity: RiskSeverityCritical, Subject: counterparty})
		}
	case OperationERC20Approve:
		findings = append(findings, RiskFinding{ID: RiskFindingTokenApproval, Severity: RiskSeverityCritical, Subject: counterparty})
		if simulation.Trace == nil {
			findings = append(findings, RiskFinding{ID: RiskFindingTraceUnavailable, Severity: RiskSeverityWarning, Subject: counterparty})
		}
	default:
		return nil, fmt.Errorf("unsupported risk operation")
	}
	return findings, nil
}

func simulationPolicyCommitment(simulation SimulationResult, findings []RiskFinding) (common.Hash, error) {
	encoded, err := rlp.EncodeToBytes(struct {
		Domain     string
		Block      BlockIdentity
		GasLimit   uint64
		ReturnHash common.Hash
		Findings   []RiskFinding
	}{
		Domain: "bloco-wallet/evm-simulation-policy/v1", Block: simulation.Block,
		GasLimit: simulation.GasLimit, ReturnHash: crypto.Keccak256Hash(simulation.ReturnData), Findings: findings,
	})
	if err != nil {
		return common.Hash{}, err
	}
	return crypto.Keccak256Hash(encoded), nil
}
