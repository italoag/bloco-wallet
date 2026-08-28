package ui

import (
	"context"
	"strings"
	"testing"

	"blocowallet/internal/constants"
	"blocowallet/internal/evm"
	"blocowallet/internal/wallet"
	"blocowallet/pkg/localization"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

type personalSignServiceStub struct {
	signer common.Address
}

func (service *personalSignServiceStub) ApproveAndSignEIP712(_ context.Context, _ wallet.CapabilityHandle, prepared *evm.PreparedEIP712Sign, request evm.PersonalSignApprovalRequest) (evm.PersonalSignResult, error) {
	preview := prepared.Preview()
	key, err := crypto.HexToECDSA("4646464646464646464646464646464646464646464646464646464646464646")
	if err != nil {
		return evm.PersonalSignResult{}, err
	}
	defer key.D.SetInt64(0)
	signature, err := crypto.Sign(preview.Digest[:], key)
	if err != nil {
		return evm.PersonalSignResult{}, err
	}
	signature[crypto.RecoveryIDOffset] += 27
	return evm.PersonalSignResult{
		ApprovalID: "51111111-1111-4111-8111-111111111111", SigningID: "71111111-1111-4111-8111-111111111111",
		AccountID: preview.AccountID, Signer: service.signer, Digest: preview.Digest, IntentHash: preview.IntentHash, Signature: signature,
	}, nil
}

func (service *personalSignServiceStub) ApproveAndSignPersonal(_ context.Context, _ wallet.CapabilityHandle, prepared *evm.PreparedPersonalSign, request evm.PersonalSignApprovalRequest) (evm.PersonalSignResult, error) {
	key, err := crypto.HexToECDSA("4646464646464646464646464646464646464646464646464646464646464646")
	if err != nil {
		return evm.PersonalSignResult{}, err
	}
	defer key.D.SetInt64(0)
	preview := prepared.Preview()
	signature, err := crypto.Sign(preview.Digest[:], key)
	if err != nil {
		return evm.PersonalSignResult{}, err
	}
	signature[crypto.RecoveryIDOffset] += 27
	return evm.PersonalSignResult{
		ApprovalID: "51111111-1111-4111-8111-111111111111", SigningID: "71111111-1111-4111-8111-111111111111",
		AccountID: preview.AccountID, Signer: service.signer, Digest: preview.Digest, IntentHash: preview.IntentHash, Signature: signature,
	}, nil
}

type personalSignAuthorizerStub struct{}

func (personalSignAuthorizerStub) Authorize(_ context.Context, _ string, _ []byte, operation wallet.TransactionAuthorizationOperation) error {
	return operation(wallet.CapabilityHandle{}, 1)
}

func (personalSignAuthorizerStub) HasActiveSession(context.Context, string) bool { return false }

func TestPersonalSignFlowShowsPreviewSignatureAndKeepsViewPure(t *testing.T) {
	previousLabels := localization.Labels
	localization.Labels = map[string]string{"wallet_details_title": "Wallet Details", "select_wallet_prompt": "Select wallet"}
	t.Cleanup(func() { localization.Labels = previousLabels })
	key, err := crypto.HexToECDSA("4646464646464646464646464646464646464646464646464646464646464646")
	if err != nil {
		t.Fatal(err)
	}
	signerAddress := crypto.PubkeyToAddress(key.PublicKey)
	key.D.SetInt64(0)
	model := &CLIModel{
		width: 120, height: 30, styles: createStyles(),
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
	if !model.walletDetailsKeys.SignMessage.Enabled() {
		t.Fatalf("message signing was not enabled for software account: %+v", model.selectedAccount)
	}
	_, command := model.updateWalletDetails(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	if command != nil || model.currentView != constants.PersonalSignView {
		t.Fatal("sign message key did not open personal sign view")
	}
	model.personalSign.message.SetValue("Hello Joe")
	_, _ = model.updatePersonalSign(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	view := model.viewPersonalSign()
	if !strings.Contains(view, "personal_sign") || !strings.Contains(view, "no chain binding") || !strings.Contains(view, "Hello Joe") {
		t.Fatalf("personal sign preview is incomplete: %q", view)
	}
	_, _ = model.updatePersonalSign(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	model.personalSign.password.SetValue("Strong message password 1!")
	_, submit := model.updatePersonalSign(tea.KeyMsg{Type: tea.KeyEnter})
	if submit == nil {
		t.Fatal("personal sign did not start submission")
	}
	before := model.personalSignGeneration
	_, _ = model.Update(submit())
	view = model.viewPersonalSign()
	if model.personalSignGeneration != before || model.personalSign.phase != personalSignComplete || !strings.Contains(view, "Signature") || !strings.Contains(view, "0x") {
		t.Fatalf("personal sign did not complete: %q", view)
	}
	_, _ = model.updatePersonalSign(tea.KeyMsg{Type: tea.KeyEsc})
	if model.currentView != constants.WalletDetailsView || model.personalSign != nil {
		t.Fatal("personal sign back navigation did not restore wallet details")
	}
}

func TestPersonalSignStaleResultIsIgnoredAfterBack(t *testing.T) {
	previousLabels := localization.Labels
	localization.Labels = map[string]string{"wallet_details_title": "Wallet Details", "select_wallet_prompt": "Select wallet"}
	t.Cleanup(func() { localization.Labels = previousLabels })
	model := &CLIModel{width: 120, height: 30, styles: createStyles(), transactionAuthorizer: personalSignAuthorizerStub{}, selectedAccount: &wallet.AccountSummary{
		AccountID: "11111111-1111-4111-8111-111111111111", Address: "0x1563915e194D8CfBA1943570603F7606A3115508", State: wallet.AccountStateActive,
	}}
	service := &personalSignServiceStub{}
	model.initPersonalSign(service)
	model.personalSign.message.SetValue("stale")
	_, _ = model.updatePersonalSign(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	_, _ = model.updatePersonalSign(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	model.personalSign.password.SetValue("Strong message password 1!")
	_, submit := model.updatePersonalSign(tea.KeyMsg{Type: tea.KeyEnter})
	generation := model.personalSignGeneration
	_, _ = model.updatePersonalSign(tea.KeyMsg{Type: tea.KeyEsc})
	if model.currentView != constants.WalletDetailsView || model.personalSign != nil {
		t.Fatal("back navigation did not clear personal sign")
	}
	_, _ = model.Update(submit())
	if model.currentView != constants.WalletDetailsView || model.personalSign != nil {
		t.Fatal("stale personal sign result changed the restored view")
	}
	_ = generation
}
