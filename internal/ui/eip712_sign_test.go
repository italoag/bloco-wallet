package ui

import (
	"context"
	"strings"
	"testing"

	"blocowallet/internal/constants"
	"blocowallet/internal/wallet"
	"blocowallet/pkg/config"
	"blocowallet/pkg/localization"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

const eip712SignUITestFixture = `{
	"types": {
		"EIP712Domain": [
			{"name": "name", "type": "string"},
			{"name": "version", "type": "string"},
			{"name": "chainId", "type": "uint256"},
			{"name": "verifyingContract", "type": "address"}
		],
		"Person": [{"name": "name", "type": "string"}, {"name": "wallet", "type": "address"}],
		"Mail": [
			{"name": "from", "type": "Person"},
			{"name": "to", "type": "Person"},
			{"name": "contents", "type": "string"}
		]
	},
	"primaryType": "Mail",
	"domain": {
		"name": "Ether Mail",
		"version": "1",
		"chainId": 1,
		"verifyingContract": "0xCcCCccccCCCCcCCCCCCcCcCccCcCCCcCcccccccC"
	},
	"message": {
		"from": {"name": "Cow", "wallet": "0xCD2a3d9F938E13CD947Ec05AbC7FE734Df8DD826"},
		"to": {"name": "Bob", "wallet": "0xbBbBBBBbbBBBbbbBbbBbbbbBBbBbbbbBbBbbBBbB"},
		"contents": "Hello, Bob!"
	}
}`

func TestEIP712SignFlowSelectsChainPreviewsFieldsAndSigns(t *testing.T) {
	previousLabels := localization.Labels
	localization.Labels = map[string]string{"wallet_details_title": "Wallet Details", "select_wallet_prompt": "Select wallet"}
	t.Cleanup(func() { localization.Labels = previousLabels })
	key, err := crypto.HexToECDSA("4646464646464646464646464646464646464646464646464646464646464646")
	if err != nil {
		t.Fatal(err)
	}
	signerAddress := crypto.PubkeyToAddress(key.PublicKey)
	cfg := &config.Config{Networks: map[string]config.Network{"mainnet": {Name: "Mainnet", ChainID: 1, IsActive: true}}}
	model := &CLIModel{
		width: 120, height: 30, styles: createStyles(), currentConfig: cfg,
		transactionAuthorizer: personalSignAuthorizerStub{},
		selectedAccount: &wallet.AccountSummary{
			AccountID: "11111111-1111-4111-8111-111111111111", Name: "Signer",
			Address: signerAddress.Hex(), SignerKind: wallet.SignerKindSoftware, State: wallet.AccountStateActive,
			Capabilities: wallet.CapabilitySignMessage,
		},
	}
	model.ConfigureMessageSigningFactory(func(context.Context) (MessageSigningService, error) {
		return &personalSignServiceStub{signer: signerAddress}, nil
	})
	model.initWalletDetailsComponents()
	model.updateWalletDetailsKeyAvailability()
	if !model.walletDetailsKeys.SignTypedData.Enabled() {
		t.Fatal("typed-data signing was not enabled")
	}
	_, command := model.updateWalletDetails(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})
	if command != nil || model.currentView != constants.EIP712SignView {
		t.Fatal("typed-data key did not open EIP-712 view")
	}
	_, _ = model.updateEIP712Sign(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	if model.eip712Sign.phase != eip712SignEntry {
		t.Fatal("chain selection did not advance to typed data entry")
	}
	model.eip712Sign.typedData.SetValue(eip712SignUITestFixture)
	_, _ = model.updateEIP712Sign(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	view := model.viewEIP712Sign()
	for _, expected := range []string{"Ether Mail", "0xCcCCccccCCCCcCCCCCCcCcCccCcCCCcCcccccccC", "Cow", "Hello, Bob!", "Digest: 0xbe609aee343fb3c4b28e1df9e632fca64fcfaede20f02e86244efddf30957bd2"} {
		if !strings.Contains(view, expected) {
			t.Fatalf("EIP-712 preview omitted %q: %q", expected, view)
		}
	}
	_, _ = model.updateEIP712Sign(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	model.eip712Sign.password.SetValue("Strong message password 1!")
	_, submit := model.updateEIP712Sign(tea.KeyMsg{Type: tea.KeyEnter})
	if submit == nil {
		t.Fatal("EIP-712 sign did not start submission")
	}
	_, _ = model.Update(submit())
	view = model.viewEIP712Sign()
	if model.eip712Sign.phase != eip712SignComplete || !strings.Contains(view, "Signature") {
		t.Fatalf("EIP-712 sign did not complete: %q", view)
	}
	_, _ = model.updateEIP712Sign(tea.KeyMsg{Type: tea.KeyEsc})
	if model.currentView != constants.WalletDetailsView || model.eip712Sign != nil {
		t.Fatal("EIP-712 back navigation did not restore wallet details")
	}
}

func TestEIP712SignRejectsChainMismatchWithClearError(t *testing.T) {
	previousLabels := localization.Labels
	localization.Labels = map[string]string{"wallet_details_title": "Wallet Details", "select_wallet_prompt": "Select wallet"}
	t.Cleanup(func() { localization.Labels = previousLabels })
	cfg := &config.Config{Networks: map[string]config.Network{"testnet": {Name: "Testnet", ChainID: 5, IsActive: true}}}
	model := &CLIModel{width: 120, height: 30, styles: createStyles(), currentConfig: cfg, transactionAuthorizer: personalSignAuthorizerStub{}, selectedAccount: &wallet.AccountSummary{
		AccountID: "11111111-1111-4111-8111-111111111111", Address: "0x9d8A62f656a8d1615C1294fd71e9CFb3E4855A4F", State: wallet.AccountStateActive,
	}}
	service := &personalSignServiceStub{signer: common.HexToAddress(model.selectedAccount.Address)}
	model.initEIP712Sign(service)
	_, _ = model.updateEIP712Sign(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	model.eip712Sign.typedData.SetValue(eip712SignUITestFixture)
	_, _ = model.updateEIP712Sign(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	if model.eip712Sign.phase != eip712SignEntry || model.eip712Sign.err == "" {
		t.Fatalf("chain mismatch was not rejected: phase=%s err=%q", model.eip712Sign.phase, model.eip712Sign.err)
	}
}
