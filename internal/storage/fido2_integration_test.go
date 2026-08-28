package storage

import (
	"context"
	"encoding/base64"
	"path/filepath"
	"testing"
	"time"

	"blocowallet/internal/fido2"
	"blocowallet/pkg/config"

	"github.com/ethereum/go-ethereum/common"
)

func TestFIDO2ServiceDurableFlowWithRealVault(t *testing.T) {
	root := t.TempDir()
	cfg := &config.Config{
		AppDir:       root,
		DatabasePath: filepath.Join(root, "fido2.db"),
		Database:     config.DatabaseConfig{Type: "sqlite"},
	}
	repository, err := NewVaultRepository(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = repository.Close() }()
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	service, err := fido2.NewService(repository, fido2.Options{
		Now: func() time.Time { return now },
		NewID: func() (string, error) {
			return "11111111-1111-4111-8111-111111111111", nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	authenticator, err := fido2.NewSoftwareAuthenticator(common.FromHex("0x1111111111111111111111111111111111111111"))
	if err != nil {
		t.Fatal(err)
	}
	const rpID = "bloco.local"
	const origin = "http://127.0.0.1:18080"

	register, err := service.BeginRegistration(context.Background(), rpID, origin, "11111111-1111-4111-8111-111111111111", []byte("user-handle"))
	if err != nil {
		t.Fatal(err)
	}
	registration, err := authenticator.RegistrationResponse(base64.RawURLEncoding.EncodeToString(register.Challenge), rpID, origin, false)
	if err != nil {
		t.Fatal(err)
	}
	credential, err := service.FinishRegistration(context.Background(), register.ChallengeID, registration, false)
	if err != nil {
		t.Fatal(err)
	}

	// Restart the repository: credential and counter must survive.
	if err := repository.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewVaultRepository(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reopened.Close() }()
	service, err = fido2.NewService(reopened, fido2.Options{
		Now: func() time.Time { return now.Add(time.Minute) },
		NewID: func() (string, error) {
			return "22222222-2222-4222-8222-222222222222", nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := reopened.GetCredential(context.Background(), credential.CredentialID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.SignCount != credential.SignCount || loaded.RPID != rpID {
		t.Fatalf("credential did not survive restart: %+v", loaded)
	}
	authenticate, err := service.BeginAuthentication(context.Background(), rpID, origin, credential.CredentialID)
	if err != nil {
		t.Fatal(err)
	}
	assertion, err := authenticator.AssertionResponse(base64.RawURLEncoding.EncodeToString(authenticate.Challenge), rpID, origin, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.FinishAuthentication(context.Background(), authenticate.ChallengeID, assertion, false); err != nil {
		t.Fatal(err)
	}
	advanced, err := reopened.GetCredential(context.Background(), credential.CredentialID)
	if err != nil {
		t.Fatal(err)
	}
	if advanced.SignCount <= loaded.SignCount {
		t.Fatalf("counter did not persist after restart: %d -> %d", loaded.SignCount, advanced.SignCount)
	}
	// Immutability: the credential identity cannot be mutated.
	if err := reopened.db.Model(&fido2CredentialRow{}).Where("credential_id = ?", credential.CredentialID).Update("rp_id", "attacker.local").Error; err == nil {
		t.Fatal("fido2 credential identity was mutable")
	}
	if err := reopened.db.Model(&fido2ChallengeRow{}).Where("challenge_id = ?", authenticate.ChallengeID).Update("challenge", make([]byte, 32)).Error; err == nil {
		t.Fatal("fido2 challenge binding was mutable")
	}
}
