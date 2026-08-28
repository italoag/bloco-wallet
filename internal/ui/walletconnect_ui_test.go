package ui

import (
	"context"
	"testing"
	"time"

	"blocowallet/internal/walletconnect"

	tea "github.com/charmbracelet/bubbletea"
)

type testWalletConnectService struct {
	sessions []walletconnect.Session
	proposal *walletconnect.Proposal
	approved int64
	rejected int64
	revoked  []string
}

func (service *testWalletConnectService) ListSessions(context.Context, string, bool) ([]walletconnect.Session, error) {
	return service.sessions, nil
}
func (service *testWalletConnectService) RevokeSession(_ context.Context, topic string, _ int64) error {
	service.revoked = append(service.revoked, topic)
	for index := range service.sessions {
		if service.sessions[index].Topic == topic {
			service.sessions = append(service.sessions[:index], service.sessions[index+1:]...)
			break
		}
	}
	return nil
}
func (service *testWalletConnectService) ApproveProposal(_ context.Context, id int64, accountID, address string) (*walletconnect.Session, error) {
	service.approved = id
	now := time.Now().UnixMilli()
	session := &walletconnect.Session{
		Topic:    "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		PeerName: "Approved Dapp", AccountID: accountID,
		Namespaces: walletconnect.Namespaces{"eip155": {Chains: []string{"eip155:1"}, Methods: []string{"personal_sign"}, Accounts: []string{"eip155:1:" + address}}},
		ExpiresAt:  now + 3600000, CreatedAt: now,
	}
	service.sessions = append(service.sessions, *session)
	return session, nil
}
func (service *testWalletConnectService) RejectProposal(id int64) { service.rejected = id }
func (service *testWalletConnectService) PendingProposal(id int64) (*walletconnect.Proposal, bool) {
	if service.proposal != nil && service.proposal.ID == id {
		return service.proposal, true
	}
	return nil, false
}

func TestWalletConnectUIListApproveAndRevoke(t *testing.T) {
	model := eligibleNativeTransferModel()
	service := &testWalletConnectService{}
	model.ConfigureWalletConnect(service, service)
	model.initWalletConnect()
	if model.walletConnect == nil || model.walletConnect.phase != walletConnectList {
		t.Fatal("WalletConnect view did not initialize")
	}
	state := model.walletConnect
	if len(state.sessions) != 0 {
		t.Fatal("session list was not empty")
	}

	// Incoming proposal surfaces in the TUI and can be approved.
	service.proposal = &walletconnect.Proposal{
		ID: 42, PairingTopic: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		Expiry: time.Now().Add(time.Minute).UnixMilli(),
		RequiredNamespaces: walletconnect.RequiredNamespaces{
			"eip155": {Chains: []string{"eip155:1"}, Methods: []string{"personal_sign"}, Events: []string{"chainChanged"}},
		},
	}
	model.walletConnectHandleProposal(service.proposal)
	if model.walletConnect.phase != walletConnectProposal {
		t.Fatal("proposal did not surface in the TUI")
	}
	updated, _ := model.updateWalletConnect(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	updatedModel, ok := updated.(*CLIModel)
	if !ok {
		t.Fatal("WalletConnect update returned the wrong model")
	}
	if service.approved != 42 || updatedModel.walletConnect.phase != walletConnectList || len(updatedModel.walletConnect.sessions) != 1 {
		t.Fatalf("proposal approval did not persist a session: %+v", updatedModel.walletConnect)
	}

	// Rejecting a second proposal drops it without a session.
	service.proposal = &walletconnect.Proposal{
		ID: 43, PairingTopic: "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
		Expiry: time.Now().Add(time.Minute).UnixMilli(),
		RequiredNamespaces: walletconnect.RequiredNamespaces{
			"eip155": {Chains: []string{"eip155:1"}, Methods: []string{"personal_sign"}},
		},
	}
	updatedModel.walletConnectHandleProposal(service.proposal)
	updated, _ = updatedModel.updateWalletConnect(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	updatedModel, _ = updated.(*CLIModel)
	if service.rejected != 43 || len(updatedModel.walletConnect.sessions) != 1 {
		t.Fatal("proposal rejection was not honored")
	}

	// Revoking the approved session removes it.
	updatedModel.walletConnect.selected = 0
	updated, _ = updatedModel.updateWalletConnect(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	updatedModel, _ = updated.(*CLIModel)
	if updatedModel.walletConnect.phase != walletConnectSession {
		t.Fatal("session detail did not open")
	}
	updated, _ = updatedModel.updateWalletConnect(tea.KeyMsg{Type: tea.KeyEnter})
	updatedModel, _ = updated.(*CLIModel)
	if len(service.revoked) != 1 || len(updatedModel.walletConnect.sessions) != 0 {
		t.Fatalf("session revocation failed: %+v", service.revoked)
	}
}
