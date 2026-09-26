package ui

import (
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"math/big"
	"strings"

	"blocowallet/internal/wallet"
)

var batchNameNouns = []string{"Block", "Trader", "Wallet", "Ledger", "Token", "Chain", "Vault", "Coin", "Key", "Bridge", "Node", "Orbit", "Cloud", "River", "Ocean", "Forest", "Mountain", "Valley", "Sky", "Star", "Moon", "Sun", "Comet", "Nova", "Falcon", "Eagle", "Hawk", "Raven", "Wolf", "Fox", "Lion", "Tiger", "Bear", "Panda", "Otter", "Horse", "Dolphin", "Whale", "Shark", "Turtle", "Dragon", "Phoenix", "Beacon", "Compass", "Anchor", "Harbor", "Sailor", "Pilot", "Ranger", "Scout", "Voyager", "Pioneer", "Rocket", "Planet", "Galaxy", "Crystal", "Stone", "Silver", "Gold", "Copper", "Cedar", "Maple", "Willow", "Oak"}
var batchNameAdjectives = []string{"White", "Black", "Blue", "Green", "Red", "Gold", "Silver", "Amber", "Bright", "Bold", "Brave", "Calm", "Clear", "Clever", "Cool", "Cosmic", "Crimson", "Crystal", "Daring", "Deep", "Emerald", "Fast", "Fierce", "Fine", "Free", "Fresh", "Gentle", "Grand", "Happy", "Honest", "Ivory", "Jade", "Keen", "Kind", "Light", "Lively", "Loyal", "Lucky", "Merry", "Mighty", "Nimble", "Noble", "Orange", "Peaceful", "Quiet", "Rapid", "Ready", "Royal", "Ruby", "Sage", "Sharp", "Silent", "Smart", "Snowy", "Solid", "Steady", "Strong", "Sunny", "Swift", "True", "Violet", "Warm", "Wise", "Young"}

func nextBatchWalletName(used map[string]struct{}, random io.Reader) (string, error) {
	total := len(batchNameNouns) * len(batchNameAdjectives)
	offset, err := rand.Int(random, big.NewInt(int64(total)))
	if err != nil {
		return "", err
	}
	for attempt := 0; attempt < total; attempt++ {
		index := (int(offset.Int64()) + attempt) % total
		name := batchNameNouns[index/len(batchNameAdjectives)] + "-" + batchNameAdjectives[index%len(batchNameAdjectives)]
		key := strings.ToLower(name)
		if _, exists := used[key]; !exists {
			used[key] = struct{}{}
			return name, nil
		}
	}
	return "", fmt.Errorf("no unique two-word wallet names are available")
}

func assignCanonicalBatchNames(ctx context.Context, vault *wallet.WalletVault, state *canonicalImportState) error {
	if !state.isBatch() {
		return nil
	}
	accounts, err := vault.ListAccounts(ctx)
	if err != nil {
		return err
	}
	used := make(map[string]struct{}, len(accounts))
	for _, account := range accounts {
		used[strings.ToLower(strings.TrimSpace(account.Name))] = struct{}{}
	}
	if state.method == canonicalBatchMethod {
		for index := range state.batchItems {
			name, err := nextBatchWalletName(used, rand.Reader)
			if err != nil {
				return err
			}
			state.batchItems[index].Name = name
		}
		return nil
	}
	for index := range state.mnemonicItems {
		name, err := nextBatchWalletName(used, rand.Reader)
		if err != nil {
			return err
		}
		state.mnemonicItems[index].Name = name
	}
	return nil
}
