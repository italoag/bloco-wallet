package safe

import (
	"fmt"

	"github.com/ethereum/go-ethereum/common"
)

// Safe v1.5.0 official contract addresses per chain
// (https://github.com/safe-global/safe-deployments).
var safeSingletonByChain = map[int64]common.Address{
	1:        common.HexToAddress("0xd9Db270c1B5E3Bd161E8c8503c55cEABeE709552"),
	5:        common.HexToAddress("0x69f4D1788e39c87893C980c06EdF4b7f686e2938"),
	10:       common.HexToAddress("0x69f4D1788e39c87893C980c06EdF4b7f686e2938"),
	56:       common.HexToAddress("0xd9Db270c1B5E3Bd161E8c8503c55cEABeE709552"),
	100:      common.HexToAddress("0x69f4D1788e39c87893C980c06EdF4b7f686e2938"),
	137:      common.HexToAddress("0xd9Db270c1B5E3Bd161E8c8503c55cEABeE709552"),
	8453:     common.HexToAddress("0x69f4D1788e39c87893C980c06EdF4b7f686e2938"),
	11155111: common.HexToAddress("0x69f4D1788e39c87893C980c06EdF4b7f686e2938"),
	31337:    common.HexToAddress("0x69f4D1788e39c87893C980c06EdF4b7f686e2938"),
}

var safeFactoryByChain = map[int64]common.Address{
	1:        common.HexToAddress("0xa6B71E26C5e0845f74c812102Ca7114b6a896AB2"),
	5:        common.HexToAddress("0xC22834581EbC8527d974F8a1c97B1bEA752EF400"),
	10:       common.HexToAddress("0xC22834581EbC8527d974F8a1c97B1bEA752EF400"),
	56:       common.HexToAddress("0xa6B71E26C5e0845f74c812102Ca7114b6a896AB2"),
	100:      common.HexToAddress("0xC22834581EbC8527d974F8a1c97B1bEA752EF400"),
	137:      common.HexToAddress("0xa6B71E26C5e0845f74c812102Ca7114b6a896AB2"),
	8453:     common.HexToAddress("0xC22834581EbC8527d974F8a1c97B1bEA752EF400"),
	11155111: common.HexToAddress("0xC22834581EbC8527d974F8a1c97B1bEA752EF400"),
}

// KnownSafeContracts resolves the official Safe v1.5.0 singleton and proxy
// factory for a chain.
func KnownSafeContracts(chainID int64) (singleton, factory common.Address, ok bool) {
	singleton, singletonOK := safeSingletonByChain[chainID]
	factory, factoryOK := safeFactoryByChain[chainID]
	return singleton, factory, singletonOK && factoryOK
}

// RegisterTestContracts overrides the known contracts of a chain. Intended
// for integration tests that deploy the official artifacts locally.
func RegisterTestContracts(chainID int64, singleton, factory common.Address) {
	safeSingletonByChain[chainID] = singleton
	safeFactoryByChain[chainID] = factory
}

func requireKnownSafeContracts(chainID int64) (common.Address, common.Address, error) {
	singleton, factory, ok := KnownSafeContracts(chainID)
	if !ok {
		return common.Address{}, common.Address{}, fmt.Errorf("no official Safe v1.5.0 contracts known for chain %d", chainID)
	}
	return singleton, factory, nil
}
