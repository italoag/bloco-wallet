package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"

	"blocowallet/internal/blockchain"
	"blocowallet/internal/daemon"
	"blocowallet/internal/safe"
	"blocowallet/internal/storage"
	"blocowallet/internal/wallet"
	"blocowallet/pkg/config"

	"github.com/ethereum/go-ethereum/common"
)

// registerSafeDaemonMethods exposes read-only Safe state and proposal
// creation over the local IPC daemon. Signing and execution stay in the TUI
// and CLI because they require interactive password authorization.
func registerSafeDaemonMethods(server *daemon.Server, cfg *config.Config, repo *storage.GORMRepository) {
	services := make(map[uint64]*safe.Service)
	names := make(map[uint64]string)
	gateway := blockchain.NewRPCGateway(blockchain.RPCGatewayOptions{AllowedLocalTargets: nil})
	structured := newDaemonStructuredSigner()
	for key, network := range cfg.Networks {
		if !network.IsActive || network.ChainID <= 0 {
			continue
		}
		service, buildErr := buildSafeServiceForNetwork(network, repo, gateway, structured)
		if buildErr != nil {
			continue
		}
		services[uint64(network.ChainID)] = service
		names[uint64(network.ChainID)] = key
	}
	if len(services) == 0 {
		return
	}

	server.RegisterMethod("safe.list", func(ctx context.Context, _ json.RawMessage) (any, error) {
		accounts, err := repo.ListAccounts(ctx)
		if err != nil {
			return nil, err
		}
		type safeSummary struct {
			AccountID string `json:"account_id"`
			Name      string `json:"name"`
			Address   string `json:"address"`
		}
		var summary []safeSummary
		for _, account := range accounts {
			if account.SignerKind != wallet.SignerKindMultisig {
				continue
			}
			summary = append(summary, safeSummary{
				AccountID: account.AccountID, Name: sanitizeDaemonString(account.Name), Address: account.Address,
			})
		}
		return map[string]any{"safes": summary, "networks": names}, nil
	})

	server.RegisterMethod("safe.summary", func(ctx context.Context, request json.RawMessage) (any, error) {
		var params struct {
			AccountID string `json:"account_id"`
			ChainID   uint64 `json:"chain_id"`
		}
		if err := json.Unmarshal(request, &params); err != nil || params.AccountID == "" || params.ChainID == 0 {
			return nil, fmt.Errorf("account_id and chain_id are required")
		}
		service, exists := services[params.ChainID]
		if !exists {
			return nil, fmt.Errorf("no Safe service for chain %d", params.ChainID)
		}
		summary, err := service.SummarizeSafe(ctx, params.AccountID, params.ChainID)
		if err != nil {
			return nil, err
		}
		owners := make([]string, 0, len(summary.Owners))
		for _, owner := range summary.Owners {
			owners = append(owners, owner.Hex())
		}
		return map[string]any{
			"address": summary.Address.Hex(), "deployed": summary.Deployed,
			"owners": owners, "threshold": summary.Threshold,
			"nonce": summary.Nonce.String(),
		}, nil
	})

	server.RegisterMethod("safe.proposals", func(ctx context.Context, request json.RawMessage) (any, error) {
		var params struct {
			AccountID string `json:"account_id"`
			ChainID   uint64 `json:"chain_id"`
			Limit     int    `json:"limit"`
		}
		if err := json.Unmarshal(request, &params); err != nil || params.AccountID == "" || params.ChainID == 0 {
			return nil, fmt.Errorf("account_id and chain_id are required")
		}
		service, exists := services[params.ChainID]
		if !exists {
			return nil, fmt.Errorf("no Safe service for chain %d", params.ChainID)
		}
		proposals, err := service.ListProposals(ctx, params.AccountID, params.ChainID, params.Limit)
		if err != nil {
			return nil, err
		}
		type proposalSummary struct {
			ProposalID string `json:"proposal_id"`
			To         string `json:"to"`
			Value      string `json:"value"`
			Signatures int    `json:"signatures"`
			Threshold  uint64 `json:"threshold"`
			Status     string `json:"status"`
		}
		summary := make([]proposalSummary, 0, len(proposals))
		for _, proposal := range proposals {
			summary = append(summary, proposalSummary{
				ProposalID: proposal.ProposalID, To: proposal.Transaction.To.Hex(),
				Value: proposal.Transaction.Value.String(), Signatures: len(proposal.Signatures),
				Threshold: proposal.Threshold, Status: string(proposal.Status),
			})
		}
		return map[string]any{"proposals": summary}, nil
	})

	server.RegisterMethod("safe.propose", func(ctx context.Context, request json.RawMessage) (any, error) {
		var params struct {
			AccountID string `json:"account_id"`
			ChainID   uint64 `json:"chain_id"`
			To        string `json:"to"`
			Value     string `json:"value"`
			Data      string `json:"data,omitempty"`
		}
		if err := json.Unmarshal(request, &params); err != nil || params.AccountID == "" || params.ChainID == 0 {
			return nil, fmt.Errorf("account_id, chain_id, to, and value are required")
		}
		if !common.IsHexAddress(params.To) || common.HexToAddress(params.To).Hex() != params.To {
			return nil, fmt.Errorf("to must be a checksummed address")
		}
		value, ok := new(big.Int).SetString(params.Value, 10)
		if !ok || value.Sign() < 0 {
			return nil, fmt.Errorf("value must be a non-negative decimal integer")
		}
		var data []byte
		if params.Data != "" {
			trimmed := trimHexPrefix(params.Data)
			if len(trimmed)%2 != 0 {
				return nil, fmt.Errorf("data must be even-length hex")
			}
			data = common.FromHex(trimmed)
		}
		service, exists := services[params.ChainID]
		if !exists {
			return nil, fmt.Errorf("no Safe service for chain %d", params.ChainID)
		}
		proposal, err := service.Propose(ctx, safe.SafeProposalRequest{
			SafeAccountID: params.AccountID, ChainID: params.ChainID,
			To: common.HexToAddress(params.To), Value: value, Data: data,
		})
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"proposal_id": proposal.ProposalID, "digest": fmt.Sprintf("0x%x", proposal.Digest),
			"signatures": 0, "threshold": proposal.Threshold,
		}, nil
	})
}

func trimHexPrefix(value string) string {
	if len(value) >= 2 && value[0] == '0' && (value[1] == 'x' || value[1] == 'X') {
		return value[2:]
	}
	return value
}
