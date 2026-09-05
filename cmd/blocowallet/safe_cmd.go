package main

import (
	"context"
	"fmt"
	"math/big"
	"os"
	"strconv"
	"strings"
	"time"

	"blocowallet/internal/blockchain"
	"blocowallet/internal/safe"
	"blocowallet/internal/storage"
	"blocowallet/internal/wallet"
	"blocowallet/pkg/config"

	"github.com/ethereum/go-ethereum/common"
)

// runSafeCommand handles the `safe` subcommand family without starting the
// TUI: import, propose, list, sign, execute.
func runSafeCommand(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: blocowallet safe <import|propose|list|sign|execute>")
	}
	command := args[0]
	rest := args[1:]

	configManager := config.NewConfigurationManager()
	cfg, err := configManager.LoadConfiguration()
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}
	repo, err := storage.NewVaultRepository(cfg)
	if err != nil {
		return fmt.Errorf("open repository: %w", err)
	}
	defer func() { _ = repo.Close() }()
	codec, err := wallet.NewSecretEnvelopeCodec(wallet.ProductionArgon2idPolicy())
	if err != nil {
		return err
	}
	identityKey, err := loadOrCreateSourceIdentityKey(cfg.AppDir)
	if err != nil {
		return err
	}
	defer clear(identityKey)
	vault, err := wallet.NewWalletVault(repo, codec, wallet.VaultOptions{SourceIdentityKey: identityKey})
	if err != nil {
		return err
	}
	defer vault.Close()

	softwareSigner, err := wallet.NewSoftwareSignerWithApprovalVerifier(vault, repo)
	if err != nil {
		return err
	}
	signingBackends, err := configureExternalSigners(softwareSigner, repo, blockchain.NewRPCGateway(blockchain.RPCGatewayOptions{AllowedLocalTargets: nil}))
	if err != nil {
		return err
	}
	defer signingBackends.close()
	softwareAuthorizer, err := wallet.NewTransactionAuthorizer(vault, wallet.TransactionAuthorizationMode(cfg.Security.TransactionAuthorizationMode))
	if err != nil {
		return err
	}
	transactionAuthorizer, err := wallet.NewSigningAuthorizer(softwareAuthorizer, repo)
	if err != nil {
		softwareAuthorizer.Close()
		return err
	}
	defer transactionAuthorizer.Close()

	networkKey, chainID, err := resolveSafeNetwork(cfg, rest)
	if err != nil {
		return err
	}
	network, exists := cfg.Networks[networkKey]
	if !exists || !network.IsActive || network.ChainID <= 0 {
		return fmt.Errorf("network %q is not active", networkKey)
	}
	gateway := blockchain.NewRPCGateway(blockchain.RPCGatewayOptions{AllowedLocalTargets: nil})
	endpoint, err := network.ResolveRPCEndpoint(config.EnvironmentCredentialProvider{})
	if err != nil {
		return err
	}
	session, err := gateway.ValidateChain(context.Background(), endpoint, network.ChainID)
	if err != nil {
		return fmt.Errorf("validate chain: %w", err)
	}
	rpc, err := safe.NewRPCAdapter(gateway, session)
	if err != nil {
		return err
	}
	gasPayer, err := safe.NewGasPayerSignerAdapter(signingBackends.structured)
	if err != nil {
		return err
	}
	service, err := safe.NewWithGasPayer(repo, repo, &safeAccountImporter{repo: repo}, repo, signingBackends.structured, gasPayer, rpc, safe.Options{ApprovalTTL: 30 * time.Minute})
	if err != nil {
		return err
	}
	authorize := func(accountID string, operation func(wallet.CapabilityHandle) error) error {
		password, err := readPassword("Storage password for owner account: ")
		if err != nil {
			return err
		}
		defer clear(password)
		return transactionAuthorizer.Authorize(context.Background(), accountID, password, func(handle wallet.CapabilityHandle, _ uint64) error {
			return operation(handle)
		})
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	switch command {
	case "import":
		if len(rest) < 3 {
			return fmt.Errorf("usage: blocowallet safe import <network> <name> <safe-address>")
		}
		account, err := service.ImportSafe(ctx, safe.SafeImportRequest{Name: rest[1], Address: rest[2], ChainID: uint64(chainID)})
		if err != nil {
			return err
		}
		fmt.Printf("imported safe %s account=%s owners=%d threshold=%d\n", account.Address.Hex(), account.AccountID, len(account.Owners), account.Threshold)
		return nil
	case "propose":
		if len(rest) < 4 {
			return fmt.Errorf("usage: blocowallet safe propose <network> <safe-name> <to> <value> [data]")
		}
		to, value, data, err := parseProposalArgs(rest[1:])
		if err != nil {
			return err
		}
		safeAccount, err := resolveSafeAccount(ctx, repo, rest[0])
		if err != nil {
			return err
		}
		proposal, err := service.Propose(ctx, safe.SafeProposalRequest{
			SafeAccountID: safeAccount.AccountID, ChainID: uint64(chainID), To: to, Value: value, Data: data,
		})
		if err != nil {
			return err
		}
		fmt.Printf("proposed %s digest=0x%x nonce=%s signatures=0/%d\n", proposal.ProposalID, proposal.Digest, proposal.Nonce.String(), proposal.Threshold)
		return nil
	case "list":
		if len(rest) < 2 {
			return fmt.Errorf("usage: blocowallet safe list <network> <safe-name>")
		}
		safeAccount, err := resolveSafeAccount(ctx, repo, rest[1])
		if err != nil {
			return err
		}
		proposals, err := service.ListProposals(ctx, safeAccount.AccountID, uint64(chainID), 20)
		if err != nil {
			return err
		}
		for _, proposal := range proposals {
			fmt.Printf("%s %s to=%s value=%s signatures=%d/%d status=%s\n",
				proposal.ProposalID, proposal.Status, proposal.Transaction.To.Hex(), proposal.Transaction.Value.String(),
				len(proposal.Signatures), proposal.Threshold, proposal.TxHash.Hex())
		}
		return nil
	case "sign":
		if len(rest) < 3 {
			return fmt.Errorf("usage: blocowallet safe sign <network> <proposal-id> <owner-account-id>")
		}
		updated, err := service.Sign(ctx, safe.SafeSignRequest{ProposalID: rest[1], OwnerAccountID: rest[2], ChainID: uint64(chainID)}, authorize)
		if err != nil {
			return err
		}
		fmt.Printf("signed %s signatures=%d/%d\n", updated.ProposalID, len(updated.Signatures), updated.Threshold)
		return nil
	case "create":
		if len(rest) < 5 {
			return fmt.Errorf("usage: blocowallet safe create <network> <name> <owners-csv> <threshold> <deployer-account-id> [salt]")
		}
		owners, err := parseOwnerList(rest[2])
		if err != nil {
			return err
		}
		threshold, err := strconv.ParseUint(rest[3], 10, 64)
		if err != nil || threshold == 0 || threshold > uint64(len(owners)) {
			return fmt.Errorf("threshold must be between 1 and the owner count")
		}
		salt := uint64(1)
		if len(rest) > 5 {
			salt, err = strconv.ParseUint(rest[5], 10, 64)
			if err != nil {
				return fmt.Errorf("salt must be a non-negative integer")
			}
		}
		deployment, err := service.PrepareDeploy(ctx, safe.DeployRequest{
			ChainID: chainID, Name: rest[1], Owners: owners, Threshold: threshold, SaltNonce: salt,
		})
		if err != nil {
			return err
		}
		fmt.Printf("predicted Safe address: %s\n", deployment.SafeAddress.Hex())
		fmt.Printf("fund that address with the network fee (the proxy pays for setup), then press enter to deploy\n")
		if _, err := readSecretLine(); err != nil {
			return err
		}
		hash, err := service.BroadcastDeploy(ctx, deployment, rest[4], authorize)
		if err != nil {
			return err
		}
		imported, err := service.ImportSafe(ctx, safe.SafeImportRequest{
			Name: rest[1], Address: deployment.SafeAddress.Hex(), ChainID: uint64(chainID),
		})
		if err != nil {
			fmt.Printf("deployed %s tx=%s (import manually with safe import)\n", deployment.SafeAddress.Hex(), hash.Hex())
			return nil
		}
		fmt.Printf("deployed %s account=%s tx=%s\n", deployment.SafeAddress.Hex(), imported.AccountID, hash.Hex())
		return nil
	case "add-owner":
		if len(rest) < 4 {
			return fmt.Errorf("usage: blocowallet safe add-owner <network> <safe-name> <new-owner> <threshold>")
		}
		if !common.IsHexAddress(rest[2]) || common.HexToAddress(rest[2]).Hex() != rest[2] {
			return fmt.Errorf("new owner must be a checksummed address")
		}
		threshold, err := strconv.ParseUint(rest[3], 10, 64)
		if err != nil || threshold == 0 {
			return fmt.Errorf("threshold must be a positive integer")
		}
		safeAccount, err := resolveSafeAccount(ctx, repo, rest[1])
		if err != nil {
			return err
		}
		proposal, err := service.ProposeOwnerChange(ctx, safe.OwnerProposalRequest{
			SafeAccountID: safeAccount.AccountID, ChainID: uint64(chainID),
			Action: safe.OwnerActionAdd, Owner: common.HexToAddress(rest[2]), Threshold: threshold,
		})
		if err != nil {
			return err
		}
		fmt.Printf("proposed add owner %s proposal=%s\n", rest[2], proposal.ProposalID)
		return nil
	case "remove-owner":
		if len(rest) < 3 {
			return fmt.Errorf("usage: blocowallet safe remove-owner <network> <safe-name> <owner> [threshold]")
		}
		if !common.IsHexAddress(rest[2]) || common.HexToAddress(rest[2]).Hex() != rest[2] {
			return fmt.Errorf("owner must be a checksummed address")
		}
		var threshold uint64
		if len(rest) > 3 {
			threshold, err = strconv.ParseUint(rest[3], 10, 64)
			if err != nil {
				return fmt.Errorf("threshold must be a positive integer")
			}
		}
		safeAccount, err := resolveSafeAccount(ctx, repo, rest[1])
		if err != nil {
			return err
		}
		proposal, err := service.ProposeOwnerChange(ctx, safe.OwnerProposalRequest{
			SafeAccountID: safeAccount.AccountID, ChainID: uint64(chainID),
			Action: safe.OwnerActionRemove, Owner: common.HexToAddress(rest[2]), Threshold: threshold,
		})
		if err != nil {
			return err
		}
		fmt.Printf("proposed remove owner %s proposal=%s\n", rest[2], proposal.ProposalID)
		return nil
	case "change-threshold":
		if len(rest) < 3 {
			return fmt.Errorf("usage: blocowallet safe change-threshold <network> <safe-name> <threshold>")
		}
		threshold, err := strconv.ParseUint(rest[2], 10, 64)
		if err != nil || threshold == 0 {
			return fmt.Errorf("threshold must be a positive integer")
		}
		safeAccount, err := resolveSafeAccount(ctx, repo, rest[1])
		if err != nil {
			return err
		}
		proposal, err := service.ProposeOwnerChange(ctx, safe.OwnerProposalRequest{
			SafeAccountID: safeAccount.AccountID, ChainID: uint64(chainID),
			Action: safe.OwnerActionChangeThreshold, Threshold: threshold,
		})
		if err != nil {
			return err
		}
		fmt.Printf("proposed change threshold to %d proposal=%s\n", threshold, proposal.ProposalID)
		return nil
	case "sign-message":
		if len(rest) < 3 {
			return fmt.Errorf("usage: blocowallet safe sign-message <network> <safe-name> <owner-account-id> <message>")
		}
		safeAccount, err := resolveSafeAccount(ctx, repo, rest[1])
		if err != nil {
			return err
		}
		message := []byte(rest[3])
		proposed, err := service.ProposeMessage(ctx, safe.SafeMessageProposalRequest{
			SafeAccountID: safeAccount.AccountID, ChainID: uint64(chainID), Message: message,
		})
		if err != nil {
			return err
		}
		if err := service.SignMessage(ctx, proposed, rest[2], authorize); err != nil {
			return err
		}
		fmt.Printf("signed message %s digest=0x%x signatures=%d/%d\n", proposed.MessageID, proposed.Digest, len(proposed.Signatures), proposed.Threshold)
		return nil
	case "verify-message":
		if len(rest) < 3 {
			return fmt.Errorf("usage: blocowallet safe verify-message <network> <safe-name> <message>")
		}
		safeAccount, err := resolveSafeAccount(ctx, repo, rest[1])
		if err != nil {
			return err
		}
		proposed, err := service.ProposeMessage(ctx, safe.SafeMessageProposalRequest{
			SafeAccountID: safeAccount.AccountID, ChainID: uint64(chainID), Message: []byte(rest[2]),
		})
		if err != nil {
			return err
		}
		// Re-sign with all owners so the aggregate can be produced; the
		// message itself is verifiable off-chain via the EIP-1271 payload.
		aggregate, err := service.VerifyMessage(proposed)
		if err != nil {
			return fmt.Errorf("message not ready: %w", err)
		}
		fmt.Printf("message %s digest=0x%x aggregate=0x%x (threshold %d)\n", proposed.MessageID, proposed.Digest, aggregate, proposed.Threshold)
		return nil
	case "execute":
		if len(rest) < 3 {
			return fmt.Errorf("usage: blocowallet safe execute <network> <proposal-id> <gas-payer-account-id>")
		}
		hash, err := service.Execute(ctx, rest[1], rest[2], authorize)
		if err != nil {
			return err
		}
		fmt.Printf("executed %s tx=%s\n", rest[1], hash.Hex())
		return nil
	default:
		return fmt.Errorf("unknown safe command %q", command)
	}
}

func parseOwnerList(encoded string) ([]common.Address, error) {
	parts := strings.Split(encoded, ",")
	owners := make([]common.Address, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if !common.IsHexAddress(trimmed) || common.HexToAddress(trimmed).Hex() != trimmed {
			return nil, fmt.Errorf("owner %q must be a checksummed address", trimmed)
		}
		owners = append(owners, common.HexToAddress(trimmed))
	}
	return owners, nil
}

type safeAccountImporter struct {
	repo *storage.GORMRepository
}

func (importer *safeAccountImporter) ImportSafeAccount(ctx context.Context, request wallet.ExternalSignerImportRequest) (*wallet.Account, error) {
	return wallet.ImportExternalSignerAccount(ctx, importer.repo, request)
}

func resolveSafeNetwork(cfg *config.Config, args []string) (string, int64, error) {
	if len(args) < 1 {
		return "", 0, fmt.Errorf("network argument is required")
	}
	networkKey := args[0]
	network, exists := cfg.Networks[networkKey]
	if !exists || !network.IsActive || network.ChainID <= 0 {
		return "", 0, fmt.Errorf("network %q is not active", networkKey)
	}
	return networkKey, network.ChainID, nil
}

func resolveSafeAccount(ctx context.Context, repo *storage.GORMRepository, nameOrAddress string) (*wallet.Account, error) {
	accounts, err := repo.ListAccounts(ctx)
	if err != nil {
		return nil, err
	}
	for index := range accounts {
		account := &accounts[index]
		if account.SignerKind != wallet.SignerKindMultisig {
			continue
		}
		if account.Name == nameOrAddress || strings.EqualFold(account.Address, nameOrAddress) || account.AccountID == nameOrAddress {
			return account, nil
		}
	}
	return nil, fmt.Errorf("no Safe account matches %q", nameOrAddress)
}

func parseProposalArgs(args []string) (common.Address, *big.Int, []byte, error) {
	if len(args) < 3 {
		return common.Address{}, nil, nil, fmt.Errorf("usage: blocowallet safe propose <network> <safe-name> <to> <value> [data]")
	}
	if !common.IsHexAddress(args[0]) {
		return common.Address{}, nil, nil, fmt.Errorf("recipient address is invalid")
	}
	value, ok := new(big.Int).SetString(args[1], 10)
	if !ok || value.Sign() < 0 {
		return common.Address{}, nil, nil, fmt.Errorf("value must be a non-negative decimal integer")
	}
	var data []byte
	if len(args) > 2 {
		decoded, err := decodeHexData(args[2])
		if err != nil {
			return common.Address{}, nil, nil, err
		}
		data = decoded
	}
	return common.HexToAddress(args[0]), value, data, nil
}

func decodeHexData(encoded string) ([]byte, error) {
	trimmed := strings.TrimPrefix(strings.TrimSpace(encoded), "0x")
	if len(trimmed)%2 != 0 {
		return nil, fmt.Errorf("calldata must be hex with even length")
	}
	return common.FromHex(trimmed), nil
}

func readPassword(prompt string) ([]byte, error) {
	fmt.Fprint(os.Stderr, prompt)
	password, err := readSecretLine()
	if err != nil {
		return nil, err
	}
	if len(password) == 0 {
		return nil, fmt.Errorf("password is required")
	}
	return password, nil
}

func readSecretLine() ([]byte, error) {
	var buffer []byte
	var current [1]byte
	for {
		count, err := os.Stdin.Read(current[:])
		if count > 0 {
			if current[0] == '\n' {
				break
			}
			if current[0] != '\r' {
				buffer = append(buffer, current[0])
			}
		}
		if err != nil {
			return buffer, err
		}
	}
	return buffer, nil
}
