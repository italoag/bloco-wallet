package fido2

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"time"
)

const (
	// DefaultChallengeTTL bounds challenge validity.
	DefaultChallengeTTL = 5 * time.Minute
	// MaxChallengesPerAccount bounds pending challenges per account.
	MaxChallengesPerAccount = 16
)

// Repository persists credentials and single-use challenges.
type Repository interface {
	SaveCredential(context.Context, *Credential) error
	GetCredential(ctx context.Context, credentialID []byte) (*Credential, error)
	ListCredentials(ctx context.Context, rpID string) ([]Credential, error)
	UpdateSignCount(ctx context.Context, credentialID []byte, signCount uint32, usedAt int64) error
	SaveChallenge(context.Context, *Challenge) error
	GetChallenge(ctx context.Context, challengeID string) (*Challenge, error)
	ConsumeChallenge(ctx context.Context, challengeID string, usedAt int64) error
}

// Options configures the FIDO2 service.
type Options struct {
	Now          func() time.Time
	NewID        func() (string, error)
	ChallengeTTL time.Duration
}

// Service orchestrates registration and authentication ceremonies.
type Service struct {
	repository   Repository
	now          func() time.Time
	newID        func() (string, error)
	challengeTTL time.Duration
}

// NewService creates a FIDO2 service.
func NewService(repository Repository, options Options) (*Service, error) {
	if repository == nil {
		return nil, fmt.Errorf("fido2: repository is required")
	}
	service := &Service{repository: repository}
	service.now = options.Now
	if service.now == nil {
		service.now = time.Now
	}
	service.newID = options.NewID
	if service.newID == nil {
		service.newID = randomChallengeID
	}
	service.challengeTTL = options.ChallengeTTL
	if service.challengeTTL <= 0 {
		service.challengeTTL = DefaultChallengeTTL
	}
	return service, nil
}

// BeginRegistration creates a single-use registration challenge. The user
// handle is bound to the challenge and stored with the resulting credential.
func (service *Service) BeginRegistration(ctx context.Context, rpID, origin, accountID string, userHandle []byte) (*RegisterChallenge, error) {
	if err := validateRPIDOrigin(rpID, origin); err != nil {
		return nil, err
	}
	if len(userHandle) == 0 || len(userHandle) > MaxUserHandleBytes {
		return nil, fmt.Errorf("fido2: invalid user handle")
	}
	challenge, err := service.newChallenge(ctx, ChallengeRegister, rpID, origin, accountID)
	if err != nil {
		return nil, err
	}
	challenge.UserHandle = append([]byte(nil), userHandle...)
	if err := service.repository.SaveChallenge(ctx, challenge); err != nil {
		return nil, err
	}
	return &RegisterChallenge{
		ChallengeID: challenge.ChallengeID, RPID: rpID, Origin: origin,
		Challenge: append([]byte(nil), challenge.Challenge...), ExpiresAt: challenge.ExpiresAt,
		UserHandle: append([]byte(nil), userHandle...),
	}, nil
}

// FinishRegistration verifies and persists a registration response.
func (service *Service) FinishRegistration(ctx context.Context, challengeID string, response RegistrationResponse, requireUserVerification bool) (*Credential, error) {
	challenge, err := service.takeChallenge(ctx, challengeID, ChallengeRegister)
	if err != nil {
		return nil, err
	}
	credential, err := service.ParseRegistration(response, challenge, requireUserVerification)
	if err != nil {
		return nil, err
	}
	credential.UserHandle = append([]byte(nil), challenge.UserHandle...)
	if err := service.repository.SaveCredential(ctx, credential); err != nil {
		return nil, err
	}
	return credential, nil
}

// BeginAuthentication creates a single-use assertion challenge.
func (service *Service) BeginAuthentication(ctx context.Context, rpID, origin string, credentialID []byte) (*AuthenticateChallenge, error) {
	if err := validateRPIDOrigin(rpID, origin); err != nil {
		return nil, err
	}
	if len(credentialID) < MinCredentialIDBytes || len(credentialID) > MaxCredentialIDBytes {
		return nil, fmt.Errorf("fido2: credential id size")
	}
	credential, err := service.repository.GetCredential(ctx, credentialID)
	if err != nil {
		return nil, err
	}
	if credential.RPID != rpID {
		return nil, fmt.Errorf("fido2: credential rp id mismatch")
	}
	challenge, err := service.newChallenge(ctx, ChallengeAuthenticate, rpID, origin, "")
	if err != nil {
		return nil, err
	}
	if err := service.repository.SaveChallenge(ctx, challenge); err != nil {
		return nil, err
	}
	return &AuthenticateChallenge{
		ChallengeID: challenge.ChallengeID, RPID: rpID, Origin: origin,
		Challenge: append([]byte(nil), challenge.Challenge...), ExpiresAt: challenge.ExpiresAt,
		CredentialID: append([]byte(nil), credentialID...),
	}, nil
}

// FinishAuthentication verifies an assertion and advances the counter.
func (service *Service) FinishAuthentication(ctx context.Context, challengeID string, response AssertionResponse, requireUserVerification bool) (*AssertionResult, error) {
	challenge, err := service.takeChallenge(ctx, challengeID, ChallengeAuthenticate)
	if err != nil {
		return nil, err
	}
	credential, err := service.repository.GetCredential(ctx, response.RawID)
	if err != nil {
		return nil, err
	}
	result, err := service.ParseAssertion(response, challenge, credential, requireUserVerification)
	if err != nil {
		return nil, err
	}
	if err := service.repository.UpdateSignCount(ctx, credential.CredentialID, result.SignCount, result.VerifiedAt); err != nil {
		return nil, err
	}
	return result, nil
}

func (service *Service) newChallenge(ctx context.Context, kind ChallengeKind, rpID, origin, accountID string) (*Challenge, error) {
	if accountID != "" && len(accountID) != 36 {
		return nil, fmt.Errorf("fido2: invalid account id")
	}
	id, err := service.newID()
	if err != nil {
		return nil, fmt.Errorf("fido2: challenge id: %w", err)
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return nil, fmt.Errorf("fido2: challenge entropy: %w", err)
	}
	now := service.now()
	challenge := &Challenge{
		ChallengeID: id, Kind: kind, RPID: rpID, Origin: origin, AccountID: accountID,
		Challenge: raw, CreatedAt: now.UnixMilli(), ExpiresAt: now.Add(service.challengeTTL).UnixMilli(), Used: false,
	}
	return challenge, nil
}

// takeChallenge loads, validates, and single-use consumes a challenge.
func (service *Service) takeChallenge(ctx context.Context, challengeID string, kind ChallengeKind) (*Challenge, error) {
	if len(challengeID) != 36 {
		return nil, fmt.Errorf("fido2: invalid challenge id")
	}
	challenge, err := service.repository.GetChallenge(ctx, challengeID)
	if err != nil {
		return nil, err
	}
	if challenge.Kind != kind {
		return nil, fmt.Errorf("fido2: challenge kind mismatch")
	}
	if challenge.Used {
		return nil, fmt.Errorf("fido2: challenge already used")
	}
	if challenge.ExpiresAt <= service.now().UnixMilli() {
		return nil, fmt.Errorf("fido2: challenge expired")
	}
	if err := service.repository.ConsumeChallenge(ctx, challengeID, service.now().UnixMilli()); err != nil {
		return nil, err
	}
	return challenge, nil
}

func validateRPIDOrigin(rpID, origin string) error {
	if rpID == "" || len(rpID) > 128 {
		return fmt.Errorf("fido2: invalid rp id")
	}
	if origin == "" || len(origin) > 512 {
		return fmt.Errorf("fido2: invalid origin")
	}
	return nil
}

func randomChallengeID() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x-%x-%x-%x-%x", raw[0:4], raw[4:6], raw[6:8], raw[8:10], raw[10:16]), nil
}

var _ = sha256.Size
