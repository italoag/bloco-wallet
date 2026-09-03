package safe

import (
	"context"
	"fmt"
	"math/big"

	"blocowallet/internal/wallet"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// DeployRequest describes a new Safe proxy to deploy through the official
// SafeProxyFactory.
type DeployRequest struct {
	ChainID   int64
	Name      string
	Owners    []common.Address
	Threshold uint64
	SaltNonce uint64
}

// Deployment carries the predicted address and the unsigned deployment
// envelope so callers can fund the Safe before broadcasting.
type Deployment struct {
	ChainID     int64
	SafeAddress common.Address
	Factory     common.Address
	Singleton   common.Address
	SetupData   []byte
	ProxyCall   []byte
}

// PrepareDeploy derives the CREATE2 address and encodes the deployment
// transaction for the official Safe v1.5.0 contracts of the chain.
func (service *Service) PrepareDeploy(ctx context.Context, request DeployRequest) (*Deployment, error) {
	if request.ChainID <= 0 || request.Name == "" || len(request.Name) > 64 {
		return nil, fmt.Errorf("safe deploy: name and chain are required")
	}
	if len(request.Owners) == 0 || len(request.Owners) > 1024 {
		return nil, fmt.Errorf("safe deploy: owner list is required")
	}
	if request.Threshold == 0 || request.Threshold > uint64(len(request.Owners)) {
		return nil, fmt.Errorf("safe deploy: threshold must be between 1 and the owner count")
	}
	seen := make(map[common.Address]struct{}, len(request.Owners))
	for _, owner := range request.Owners {
		if owner == (common.Address{}) {
			return nil, fmt.Errorf("safe deploy: owner address is zero")
		}
		if _, duplicate := seen[owner]; duplicate {
			return nil, fmt.Errorf("safe deploy: duplicate owner %s", owner.Hex())
		}
		seen[owner] = struct{}{}
	}
	singleton, factory, err := requireKnownSafeContracts(request.ChainID)
	if err != nil {
		return nil, err
	}
	creationCode, err := service.rpc.ProxyCreationCode(ctx, factory)
	if err != nil {
		return nil, fmt.Errorf("safe deploy: proxy creation code: %w", err)
	}
	setupData, err := encodeSafeSetupData(request.Owners, request.Threshold)
	if err != nil {
		return nil, err
	}
	proxyCall, err := encodeCreateProxyWithNonceData(singleton, setupData, request.SaltNonce)
	if err != nil {
		return nil, err
	}
	safeAddress := deriveSafeProxyAddress(factory, singleton, creationCode, setupData, new(big.Int).SetUint64(request.SaltNonce))
	return &Deployment{
		ChainID: request.ChainID, SafeAddress: safeAddress, Factory: factory, Singleton: singleton,
		SetupData: setupData, ProxyCall: proxyCall,
	}, nil
}

// BroadcastDeploy signs the deployment envelope with the given deployer
// account and broadcasts it. The Safe must be funded with the network fee
// beforehand.
func (service *Service) BroadcastDeploy(ctx context.Context, deployment *Deployment, deployerAccountID string, authorize func(accountID string, operation func(wallet.CapabilityHandle) error) error) (common.Hash, error) {
	if service.gasPayer == nil {
		return common.Hash{}, fmt.Errorf("safe deploy: gas payer signer is unavailable")
	}
	if deployerAccountID == "" || authorize == nil {
		return common.Hash{}, fmt.Errorf("safe deploy: deployer account and authorization are required")
	}
	if deployment == nil || deployment.Factory == (common.Address{}) || len(deployment.ProxyCall) == 0 {
		return common.Hash{}, fmt.Errorf("safe deploy: prepared deployment is required")
	}
	deployerAccount, err := service.accounts.GetAccount(ctx, deployerAccountID)
	if err != nil {
		return common.Hash{}, err
	}
	if deployment.ChainID <= 0 {
		return common.Hash{}, fmt.Errorf("safe deploy: deployment chain is unknown")
	}
	var raw []byte
	var signingErr error
	deployErr := authorize(deployerAccountID, func(handle wallet.CapabilityHandle) error {
		nonce, nonceErr := service.rpc.PendingNonce(ctx, common.HexToAddress(deployerAccount.Address))
		if nonceErr != nil {
			return nonceErr
		}
		gasPrice, gasPriceErr := service.rpc.SuggestGasPrice(ctx)
		if gasPriceErr != nil {
			return gasPriceErr
		}
		raw, signingErr = service.gasPayer.SignGasPayer(ctx, handle, GasPayerSignRequest{
			AccountID: deployerAccount.AccountID, From: common.HexToAddress(deployerAccount.Address),
			ChainID: uint64(deployment.ChainID), To: deployment.Factory, Data: deployment.ProxyCall,
			Nonce: nonce, GasPrice: gasPrice, GasLimit: 500_000,
		})
		return signingErr
	})
	if deployErr != nil {
		return common.Hash{}, deployErr
	}
	if len(raw) == 0 {
		return common.Hash{}, fmt.Errorf("safe deploy: no signed payload produced")
	}
	return service.rpc.Broadcast(ctx, raw)
}

func encodeSafeSetupData(owners []common.Address, threshold uint64) ([]byte, error) {
	method := abi.NewMethod("setup", "setup", abi.Function, "nonpayable", false, false,
		abi.Arguments{
			{Name: "owners", Type: mustABIType("address[]")},
			{Name: "threshold", Type: mustABIType("uint256")},
			{Name: "to", Type: mustABIType("address")},
			{Name: "data", Type: mustABIType("bytes")},
			{Name: "fallbackHandler", Type: mustABIType("address")},
			{Name: "paymentToken", Type: mustABIType("address")},
			{Name: "payment", Type: mustABIType("uint256")},
			{Name: "paymentReceiver", Type: mustABIType("address")},
		},
		abi.Arguments{})
	packed, err := method.Inputs.Pack(owners, new(big.Int).SetUint64(threshold), common.Address{}, []byte{}, common.Address{}, common.Address{}, big.NewInt(0), common.Address{})
	if err != nil {
		return nil, err
	}
	return append(append([]byte(nil), method.ID...), packed...), nil
}

func encodeCreateProxyWithNonceData(singleton common.Address, initializer []byte, saltNonce uint64) ([]byte, error) {
	method := abi.NewMethod("createProxyWithNonce", "createProxyWithNonce", abi.Function, "nonpayable", false, false,
		abi.Arguments{
			{Name: "singleton", Type: mustABIType("address")},
			{Name: "initializer", Type: mustABIType("bytes")},
			{Name: "saltNonce", Type: mustABIType("uint256")},
		},
		abi.Arguments{})
	packed, err := method.Inputs.Pack(singleton, initializer, new(big.Int).SetUint64(saltNonce))
	if err != nil {
		return nil, err
	}
	return append(append([]byte(nil), method.ID...), packed...), nil
}

func mustABIType(kind string) abi.Type {
	parsed, err := abi.NewType(kind, "", nil)
	if err != nil {
		panic(err)
	}
	return parsed
}

// deriveSafeProxyAddress mirrors SafeProxyFactory.createProxyWithNonce:
// salt = keccak256(abi.encodePacked(keccak256(initializer), saltNonce)).
func deriveSafeProxyAddress(factory, singleton common.Address, creationCode, initializer []byte, saltNonce *big.Int) common.Address {
	initCode := append(append([]byte(nil), creationCode...), make([]byte, 12)...)
	initCode = append(initCode, singleton.Bytes()...)
	saltBinding := make([]byte, 0, 64)
	saltBinding = append(saltBinding, crypto.Keccak256(initializer)...)
	nonceWord := make([]byte, 32)
	saltNonce.FillBytes(nonceWord)
	saltBinding = append(saltBinding, nonceWord...)
	return create2(factory, crypto.Keccak256(saltBinding), initCode)
}

func create2(factory common.Address, salt []byte, initCode []byte) common.Address {
	initCodeHash := crypto.Keccak256(initCode)
	input := make([]byte, 0, 1+20+32+32)
	input = append(input, 0xff)
	input = append(input, factory.Bytes()...)
	input = append(input, salt...)
	input = append(input, initCodeHash...)
	hash := crypto.Keccak256(input)
	return common.BytesToAddress(hash[12:])
}
