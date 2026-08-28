package fido2_test

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"blocowallet/internal/fido2"
)

type memoryRepository struct {
	credentials map[string]*fido2.Credential
	challenges  map[string]*fido2.Challenge
	now         func() time.Time
}

func newMemoryRepository() *memoryRepository {
	return &memoryRepository{
		credentials: make(map[string]*fido2.Credential),
		challenges:  make(map[string]*fido2.Challenge),
		now:         time.Now,
	}
}

func (repository *memoryRepository) SaveCredential(_ context.Context, credential *fido2.Credential) error {
	repository.credentials[string(credential.CredentialID)] = credential
	return nil
}
func (repository *memoryRepository) GetCredential(_ context.Context, credentialID []byte) (*fido2.Credential, error) {
	credential, exists := repository.credentials[string(credentialID)]
	if !exists {
		return nil, &fido2TestError{"credential not found"}
	}
	return credential, nil
}
func (repository *memoryRepository) ListCredentials(_ context.Context, rpID string) ([]fido2.Credential, error) {
	var result []fido2.Credential
	for _, credential := range repository.credentials {
		if credential.RPID == rpID {
			result = append(result, *credential)
		}
	}
	return result, nil
}
func (repository *memoryRepository) UpdateSignCount(_ context.Context, credentialID []byte, signCount uint32, usedAt int64) error {
	credential, exists := repository.credentials[string(credentialID)]
	if !exists || credential.SignCount > signCount {
		return &fido2TestError{"counter regression"}
	}
	credential.SignCount = signCount
	used := usedAt
	credential.LastUsedAt = &used
	return nil
}
func (repository *memoryRepository) SaveChallenge(_ context.Context, challenge *fido2.Challenge) error {
	repository.challenges[challenge.ChallengeID] = challenge
	return nil
}
func (repository *memoryRepository) GetChallenge(_ context.Context, challengeID string) (*fido2.Challenge, error) {
	challenge, exists := repository.challenges[challengeID]
	if !exists {
		return nil, &fido2TestError{"challenge not found"}
	}
	return challenge, nil
}
func (repository *memoryRepository) ConsumeChallenge(_ context.Context, challengeID string, usedAt int64) error {
	challenge, exists := repository.challenges[challengeID]
	if !exists || challenge.Used {
		return &fido2TestError{"challenge not consumable"}
	}
	challenge.Used = true
	return nil
}

type fido2TestError struct{ message string }

func (err *fido2TestError) Error() string { return err.message }

func newTestService(now func() time.Time) (*fido2.Service, *memoryRepository) {
	repository := newMemoryRepository()
	service, err := fido2.NewService(repository, fido2.Options{
		Now: now,
		NewID: func() (string, error) {
			return "11111111-1111-4111-8111-111111111111", nil
		},
		ChallengeTTL: fido2.DefaultChallengeTTL,
	})
	if err != nil {
		panic(err)
	}
	return service, repository
}

func TestFIDO2RegistrationAndAuthenticationEndToEnd(t *testing.T) {
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	service, repository := newTestService(func() time.Time { return now })
	authenticator, err := fido2.NewSoftwareAuthenticator([]byte("user-handle"))
	if err != nil {
		t.Fatal(err)
	}
	const rpID = "bloco.local"
	const origin = "http://127.0.0.1:18080"

	register, err := service.BeginRegistration(context.Background(), rpID, origin, "11111111-1111-4111-8111-111111111111", []byte("user-handle"))
	if err != nil {
		t.Fatal(err)
	}
	challengeText := base64.RawURLEncoding.EncodeToString(register.Challenge)
	registration, err := authenticator.RegistrationResponse(challengeText, rpID, origin, false)
	if err != nil {
		t.Fatal(err)
	}
	credential, err := service.FinishRegistration(context.Background(), register.ChallengeID, registration, false)
	if err != nil {
		t.Fatal(err)
	}
	if credential.Algorithm != fido2.AlgorithmES256 || credential.RPID != rpID {
		t.Fatalf("unexpected credential: %+v", credential)
	}
	if credential.SignCount == 0 {
		t.Fatal("registration did not capture the authenticator counter")
	}
	if _, exists := repository.credentials[string(credential.CredentialID)]; !exists {
		t.Fatal("credential was not persisted")
	}

	authenticate, err := service.BeginAuthentication(context.Background(), rpID, origin, credential.CredentialID)
	if err != nil {
		t.Fatal(err)
	}
	registrationCounter := credential.SignCount
	challengeText = base64.RawURLEncoding.EncodeToString(authenticate.Challenge)
	assertion, err := authenticator.AssertionResponse(challengeText, rpID, origin, false)
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.FinishAuthentication(context.Background(), authenticate.ChallengeID, assertion, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.SignCount <= registrationCounter {
		t.Fatalf("assertion did not advance the counter: %d -> %d", registrationCounter, result.SignCount)
	}

	// A replay of the same challenge is rejected (single-use).
	if _, err := service.FinishAuthentication(context.Background(), authenticate.ChallengeID, assertion, false); err == nil || !strings.Contains(err.Error(), "already used") {
		t.Fatalf("challenge replay was accepted: %v", err)
	}
	// A counter regression is rejected as a cloned authenticator.
	stored := repository.credentials[string(credential.CredentialID)]
	stored.SignCount = 100
	authenticate, err = service.BeginAuthentication(context.Background(), rpID, origin, credential.CredentialID)
	if err != nil {
		t.Fatal(err)
	}
	challengeText = base64.RawURLEncoding.EncodeToString(authenticate.Challenge)
	assertion, err = authenticator.AssertionResponse(challengeText, rpID, origin, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.FinishAuthentication(context.Background(), authenticate.ChallengeID, assertion, false); err == nil || !strings.Contains(err.Error(), "counter regression") {
		t.Fatalf("counter regression was accepted: %v", err)
	}
}

func TestFIDO2RejectsBindingViolations(t *testing.T) {
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	service, repository := newTestService(func() time.Time { return now })
	authenticator, err := fido2.NewSoftwareAuthenticator([]byte("user-handle"))
	if err != nil {
		t.Fatal(err)
	}
	const rpID = "bloco.local"
	const origin = "http://127.0.0.1:18080"

	cases := []struct {
		name   string
		mutate func(challengeText string) (fido2.RegistrationResponse, error)
	}{
		{
			name: "wrong rp id",
			mutate: func(challengeText string) (fido2.RegistrationResponse, error) {
				return authenticator.RegistrationResponse(challengeText, "attacker.local", origin, false)
			},
		},
		{
			name: "wrong origin",
			mutate: func(challengeText string) (fido2.RegistrationResponse, error) {
				return authenticator.RegistrationResponse(challengeText, rpID, "http://evil.example", false)
			},
		},
		{
			name: "wrong challenge",
			mutate: func(_ string) (fido2.RegistrationResponse, error) {
				return authenticator.RegistrationResponse(base64.RawURLEncoding.EncodeToString([]byte("stale")), rpID, origin, false)
			},
		},
		{
			name: "missing user presence",
			mutate: func(challengeText string) (fido2.RegistrationResponse, error) {
				return authenticator.RegistrationResponseFlags(challengeText, rpID, origin, fido2.FlagAT)
			},
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			register, err := service.BeginRegistration(context.Background(), rpID, origin, "11111111-1111-4111-8111-111111111111", []byte("user-handle"))
			if err != nil {
				t.Fatal(err)
			}
			response, err := test.mutate(base64.RawURLEncoding.EncodeToString(register.Challenge))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := service.FinishRegistration(context.Background(), register.ChallengeID, response, false); err == nil {
				t.Fatalf("%s was accepted", test.name)
			}
		})
	}

	// User verification is enforced when required.
	register, err := service.BeginRegistration(context.Background(), rpID, origin, "11111111-1111-4111-8111-111111111111", []byte("user-handle"))
	if err != nil {
		t.Fatal(err)
	}
	response, err := authenticator.RegistrationResponse(base64.RawURLEncoding.EncodeToString(register.Challenge), rpID, origin, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.FinishRegistration(context.Background(), register.ChallengeID, response, true); err == nil || !strings.Contains(err.Error(), "user verification") {
		t.Fatalf("missing UV flag was accepted under strict policy: %v", err)
	}

	// Expired challenges are rejected.
	register, err = service.BeginRegistration(context.Background(), rpID, origin, "11111111-1111-4111-8111-111111111111", []byte("user-handle"))
	if err != nil {
		t.Fatal(err)
	}
	response, err = authenticator.RegistrationResponse(base64.RawURLEncoding.EncodeToString(register.Challenge), rpID, origin, false)
	if err != nil {
		t.Fatal(err)
	}
	expiredService, err := fido2.NewService(repository, fido2.Options{
		Now: func() time.Time { return now.Add(fido2.DefaultChallengeTTL + time.Minute) },
		NewID: func() (string, error) {
			return "11111111-1111-4111-8111-111111111111", nil
		},
		ChallengeTTL: fido2.DefaultChallengeTTL,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := expiredService.FinishRegistration(context.Background(), register.ChallengeID, response, false); err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("expired challenge was accepted: %v", err)
	}
}
