// Package fido2 implements the wallet-side WebAuthn verification for FIDO2
// credentials: strict registration parsing, challenge binding, assertion
// verification (RP ID hash, user presence, optional user verification,
// signature, origin, expiry, and monotonic counter), and single-use
// challenges. Attestation formats are limited to "none" and "packed" with
// self-attestation; anything else is rejected.
package fido2

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"

	"github.com/fxamacker/cbor/v2"
)

const (
	// MaxCredentialIDBytes bounds stored credential IDs.
	MaxCredentialIDBytes = 512
	// MinCredentialIDBytes matches the WebAuthn floor for credential IDs.
	MinCredentialIDBytes = 16
	// MaxUserHandleBytes bounds the user handle.
	MaxUserHandleBytes = 64
	// MaxPublicKeyBytes bounds the stored COSE public key.
	MaxPublicKeyBytes = 2048
	// MaxTransports bounds the transports list.
	MaxTransports = 16
	// MaxClientDataJSONBytes bounds clientDataJSON payloads.
	MaxClientDataJSONBytes = 64 << 10
	// MaxAttestationObjectBytes bounds registration attestation objects.
	MaxAttestationObjectBytes = 64 << 10
	// MaxExtensionBytes bounds authenticator extensions.
	MaxExtensionBytes = 16 << 10
)

// User presence and verification flags from authenticator data.
const (
	FlagUP byte = 1 << 0
	FlagUV byte = 1 << 2
	FlagAT byte = 1 << 6
	FlagED byte = 1 << 7
)

// COSE algorithm identifiers supported by the verifier.
const (
	AlgorithmES256 = -7
	AlgorithmEdDSA = -8
)

// Credential is a stored FIDO2 credential.
type Credential struct {
	CredentialID []byte
	RPID         string
	UserHandle   []byte
	PublicKey    []byte // canonical COSE key (CBOR)
	Algorithm    int64
	SignCount    uint32
	Transports   []string
	CreatedAt    int64
	LastUsedAt   *int64
}

// ChallengeKind distinguishes registration from authentication.
type ChallengeKind string

const (
	ChallengeRegister     ChallengeKind = "register"
	ChallengeAuthenticate ChallengeKind = "authenticate"
)

// Challenge is a single-use, expiring challenge.
type Challenge struct {
	ChallengeID string
	Kind        ChallengeKind
	RPID        string
	Origin      string
	AccountID   string
	UserHandle  []byte
	Challenge   []byte
	ExpiresAt   int64
	CreatedAt   int64
	Used        bool
}

// RegisterChallenge is returned to the caller for credential creation.
type RegisterChallenge struct {
	ChallengeID string
	RPID        string
	Origin      string
	Challenge   []byte
	ExpiresAt   int64
	UserHandle  []byte
}

// AuthenticateChallenge is returned for assertion ceremonies.
type AuthenticateChallenge struct {
	ChallengeID  string
	RPID         string
	Origin       string
	Challenge    []byte
	ExpiresAt    int64
	CredentialID []byte
}

// AssertionResult reports a verified assertion.
type AssertionResult struct {
	CredentialID []byte
	UserHandle   []byte
	SignCount    uint32
	VerifiedAt   int64
}

// RegistrationResponse is the parsed WebAuthn registration response.
type RegistrationResponse struct {
	ID       string `json:"id"`
	RawID    []byte `json:"rawId"`
	Type     string `json:"type"`
	Response struct {
		ClientDataJSON    []byte `json:"clientDataJSON"`
		AttestationObject []byte `json:"attestationObject"`
	} `json:"response"`
}

// AssertionResponse is the parsed WebAuthn assertion response.
type AssertionResponse struct {
	ID       string `json:"id"`
	RawID    []byte `json:"rawId"`
	Type     string `json:"type"`
	Response struct {
		ClientDataJSON    []byte `json:"clientDataJSON"`
		AuthenticatorData []byte `json:"authenticatorData"`
		Signature         []byte `json:"signature"`
		UserHandle        []byte `json:"userHandle"`
	} `json:"response"`
}

// clientData is the verified clientDataJSON payload.
type clientData struct {
	Type        string `json:"type"`
	Challenge   string `json:"challenge"`
	Origin      string `json:"origin"`
	CrossOrigin bool   `json:"crossOrigin,omitempty"`
}

// authenticatorData is the parsed authenticator data prefix.
type authenticatorData struct {
	RPIDHash   [32]byte
	Flags      byte
	SignCount  uint32
	Extensions []byte
}

// attestationObject is the CBOR registration payload.
type attestationObject struct {
	Format    string      `cbor:"fmt"`
	AuthData  []byte      `cbor:"authData"`
	Statement map[int]any `cbor:"attStmt"`
}

const (
	coseKeyTypeEC2   = 2
	coseKeyTypeOKP   = 1
	coseCurveP256    = 1
	coseCurveEd25519 = 6
)

// ParseRegistration verifies a registration response against the challenge
// and returns the derived credential.
func (service *Service) ParseRegistration(response RegistrationResponse, challenge *Challenge, requireUserVerification bool) (*Credential, error) {
	if challenge == nil || challenge.Kind != ChallengeRegister {
		return nil, fmt.Errorf("fido2: registration challenge mismatch")
	}
	if response.Type != "public-key" || len(response.RawID) == 0 {
		return nil, fmt.Errorf("fido2: invalid registration type")
	}
	if len(response.RawID) < MinCredentialIDBytes || len(response.RawID) > MaxCredentialIDBytes {
		return nil, fmt.Errorf("fido2: credential id size")
	}
	if response.ID != base64.RawURLEncoding.EncodeToString(response.RawID) {
		return nil, fmt.Errorf("fido2: credential id mismatch")
	}
	if len(response.Response.AttestationObject) == 0 || len(response.Response.AttestationObject) > MaxAttestationObjectBytes {
		return nil, fmt.Errorf("fido2: attestation object size")
	}
	if len(response.Response.ClientDataJSON) == 0 || len(response.Response.ClientDataJSON) > MaxClientDataJSONBytes {
		return nil, fmt.Errorf("fido2: client data size")
	}
	clientDataHash := sha256.Sum256(response.Response.ClientDataJSON)
	clientData, err := parseClientData(response.Response.ClientDataJSON)
	if err != nil {
		return nil, err
	}
	if clientData.Type != "webauthn.create" {
		return nil, fmt.Errorf("fido2: client data type")
	}
	if err := verifyClientData(clientData, challenge); err != nil {
		return nil, err
	}
	var attestation attestationObject
	if err := cbor.Unmarshal(response.Response.AttestationObject, &attestation); err != nil {
		return nil, fmt.Errorf("fido2: attestation decode: %w", err)
	}
	if attestation.Format != "none" && attestation.Format != "packed" {
		return nil, fmt.Errorf("fido2: unsupported attestation format %q", attestation.Format)
	}
	if len(attestation.AuthData) < 37 {
		return nil, fmt.Errorf("fido2: authenticator data too short")
	}
	authData, err := parseAuthenticatorData(attestation.AuthData)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(authData.RPIDHash[:], rpIDHash(challenge.RPID)) {
		return nil, fmt.Errorf("fido2: rp id hash mismatch")
	}
	if authData.Flags&FlagAT == 0 {
		return nil, fmt.Errorf("fido2: attested credential data missing")
	}
	if authData.Flags&FlagUP == 0 {
		return nil, fmt.Errorf("fido2: user presence required")
	}
	if requireUserVerification && authData.Flags&FlagUV == 0 {
		return nil, fmt.Errorf("fido2: user verification required")
	}
	attested, err := parseAttestedCredentialData(attestation.AuthData[37:])
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(attested.credentialID, response.RawID) {
		return nil, fmt.Errorf("fido2: attested credential id mismatch")
	}
	credential := &Credential{
		CredentialID: append([]byte(nil), attested.credentialID...),
		RPID:         challenge.RPID,
		PublicKey:    attested.publicKey,
		Algorithm:    attested.algorithm,
		SignCount:    authData.SignCount,
		CreatedAt:    service.now().UnixMilli(),
	}
	if attestation.Format == "packed" {
		if err := verifyPackedAttestation(attestation, attestation.AuthData, clientDataHash, credential); err != nil {
			return nil, err
		}
	}
	return credential, nil
}

// ParseAssertion verifies an assertion response and returns the result.
// A non-zero authenticator counter that does not increase is rejected as a
// cloned authenticator.
func (service *Service) ParseAssertion(response AssertionResponse, challenge *Challenge, credential *Credential, requireUserVerification bool) (*AssertionResult, error) {
	if challenge == nil || challenge.Kind != ChallengeAuthenticate {
		return nil, fmt.Errorf("fido2: assertion challenge mismatch")
	}
	if credential == nil {
		return nil, fmt.Errorf("fido2: missing credential")
	}
	if response.Type != "public-key" {
		return nil, fmt.Errorf("fido2: invalid assertion type")
	}
	if !bytes.Equal(response.RawID, credential.CredentialID) {
		return nil, fmt.Errorf("fido2: credential id mismatch")
	}
	if len(response.Response.ClientDataJSON) == 0 || len(response.Response.ClientDataJSON) > MaxClientDataJSONBytes {
		return nil, fmt.Errorf("fido2: client data size")
	}
	if len(response.Response.AuthenticatorData) < 37 {
		return nil, fmt.Errorf("fido2: authenticator data too short")
	}
	if len(response.Response.Signature) == 0 || len(response.Response.Signature) > 512 {
		return nil, fmt.Errorf("fido2: signature size")
	}
	clientDataHash := sha256.Sum256(response.Response.ClientDataJSON)
	clientData, err := parseClientData(response.Response.ClientDataJSON)
	if err != nil {
		return nil, err
	}
	if clientData.Type != "webauthn.get" {
		return nil, fmt.Errorf("fido2: client data type")
	}
	if err := verifyClientData(clientData, challenge); err != nil {
		return nil, err
	}
	authData, err := parseAuthenticatorData(response.Response.AuthenticatorData)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(authData.RPIDHash[:], rpIDHash(challenge.RPID)) {
		return nil, fmt.Errorf("fido2: rp id hash mismatch")
	}
	if authData.Flags&FlagUP == 0 {
		return nil, fmt.Errorf("fido2: user presence required")
	}
	if requireUserVerification && authData.Flags&FlagUV == 0 {
		return nil, fmt.Errorf("fido2: user verification required")
	}
	if authData.SignCount > 0 && credential.SignCount > 0 && authData.SignCount <= credential.SignCount {
		return nil, fmt.Errorf("fido2: authenticator counter regression")
	}
	signed := append(append([]byte(nil), response.Response.AuthenticatorData...), clientDataHash[:]...)
	if err := verifySignature(credential, signed, response.Response.Signature); err != nil {
		return nil, err
	}
	return &AssertionResult{
		CredentialID: append([]byte(nil), credential.CredentialID...),
		UserHandle:   append([]byte(nil), response.Response.UserHandle...),
		SignCount:    authData.SignCount,
		VerifiedAt:   service.now().UnixMilli(),
	}, nil
}

func parseClientData(raw []byte) (*clientData, error) {
	var data clientData
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if err := decoder.Decode(&data); err != nil {
		return nil, fmt.Errorf("fido2: client data decode: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err == nil {
		return nil, fmt.Errorf("fido2: client data trailing")
	}
	if data.Challenge == "" || data.Origin == "" {
		return nil, fmt.Errorf("fido2: client data incomplete")
	}
	return &data, nil
}

func verifyClientData(data *clientData, challenge *Challenge) error {
	challengeEncoded := base64.RawURLEncoding.EncodeToString(challenge.Challenge)
	if subtle.ConstantTimeCompare([]byte(data.Challenge), []byte(challengeEncoded)) != 1 {
		return fmt.Errorf("fido2: challenge mismatch")
	}
	if data.Origin != challenge.Origin {
		return fmt.Errorf("fido2: origin mismatch")
	}
	if data.CrossOrigin {
		return fmt.Errorf("fido2: cross-origin ceremony not allowed")
	}
	return nil
}

func rpIDHash(rpID string) []byte {
	hash := sha256.Sum256([]byte(rpID))
	return hash[:]
}

func parseAuthenticatorData(data []byte) (*authenticatorData, error) {
	if len(data) < 37 {
		return nil, fmt.Errorf("fido2: authenticator data length")
	}
	authData := &authenticatorData{}
	copy(authData.RPIDHash[:], data[:32])
	authData.Flags = data[32]
	authData.SignCount = uint32(data[33])<<24 | uint32(data[34])<<16 | uint32(data[35])<<8 | uint32(data[36])
	if authData.Flags&FlagED != 0 {
		if len(data) > 37+MaxExtensionBytes {
			return nil, fmt.Errorf("fido2: extensions too large")
		}
		authData.Extensions = append([]byte(nil), data[37:]...)
	}
	return authData, nil
}

type attestedCredential struct {
	credentialID []byte
	publicKey    []byte
	algorithm    int64
}

func parseAttestedCredentialData(data []byte) (*attestedCredential, error) {
	if len(data) < 18 {
		return nil, fmt.Errorf("fido2: attested credential data length")
	}
	idLength := int(data[16])<<8 | int(data[17])
	if idLength < MinCredentialIDBytes || idLength > MaxCredentialIDBytes || len(data) < 18+idLength {
		return nil, fmt.Errorf("fido2: attested credential id length")
	}
	credentialID := data[18 : 18+idLength]
	keyData := data[18+idLength:]
	if len(keyData) == 0 || len(keyData) > MaxPublicKeyBytes {
		return nil, fmt.Errorf("fido2: attested public key size")
	}
	algorithm, err := coseAlgorithm(keyData)
	if err != nil {
		return nil, err
	}
	return &attestedCredential{
		credentialID: append([]byte(nil), credentialID...),
		publicKey:    append([]byte(nil), keyData...),
		algorithm:    algorithm,
	}, nil
}

func coseAlgorithm(keyData []byte) (int64, error) {
	algorithm, _, _, err := parseCOSEKey(keyData)
	return algorithm, err
}

// parseCOSEKey decodes a canonical COSE key into algorithm and curve
// parameters.
func parseCOSEKey(keyData []byte) (algorithm int64, keyType int64, params map[int]any, err error) {
	var fields map[int]any
	if err := cbor.Unmarshal(keyData, &fields); err != nil {
		return 0, 0, nil, fmt.Errorf("fido2: cose key decode: %w", err)
	}
	algorithm, hasAlgorithm := coseInteger(fields[3])
	keyType, hasType := coseInteger(fields[1])
	if !hasAlgorithm || !hasType {
		return 0, 0, nil, fmt.Errorf("fido2: cose key incomplete")
	}
	if algorithm != AlgorithmES256 && algorithm != AlgorithmEdDSA {
		return 0, 0, nil, fmt.Errorf("fido2: unsupported cose algorithm %d", algorithm)
	}
	return algorithm, keyType, fields, nil
}

// coseInteger normalizes CBOR integer values, which the decoder may produce
// as int, int64, uint64, or smaller integer types depending on magnitude.
func coseInteger(value any) (int64, bool) {
	switch typed := value.(type) {
	case int:
		return int64(typed), true
	case int8:
		return int64(typed), true
	case int16:
		return int64(typed), true
	case int32:
		return int64(typed), true
	case int64:
		return typed, true
	case uint:
		return int64(typed), true
	case uint8:
		return int64(typed), true
	case uint16:
		return int64(typed), true
	case uint32:
		return int64(typed), true
	case uint64:
		if typed <= uint64(^uint64(0)>>1) {
			return int64(typed), true
		}
	}
	return 0, false
}

func coseBytes(value any) ([]byte, bool) {
	decoded, ok := value.([]byte)
	return decoded, ok
}

func verifyPackedAttestation(attestation attestationObject, authData []byte, clientDataHash [32]byte, credential *Credential) error {
	signature, hasSignature := attestation.Statement[-1].([]byte)
	if !hasSignature || len(signature) == 0 {
		return fmt.Errorf("fido2: packed attestation without signature")
	}
	signed := append(append([]byte(nil), authData...), clientDataHash[:]...)
	if err := verifySignature(credential, signed, signature); err != nil {
		return fmt.Errorf("fido2: packed self-attestation verification: %w", err)
	}
	return nil
}

func verifySignature(credential *Credential, message, signature []byte) error {
	algorithm, keyType, fields, err := parseCOSEKey(credential.PublicKey)
	if err != nil {
		return err
	}
	switch algorithm {
	case AlgorithmES256:
		curve, hasCurve := coseInteger(fields[-1])
		xBytes, hasX := coseBytes(fields[-2])
		yBytes, hasY := coseBytes(fields[-3])
		if keyType != coseKeyTypeEC2 || !hasCurve || curve != coseCurveP256 || !hasX || len(xBytes) != 32 || !hasY || len(yBytes) != 32 {
			return fmt.Errorf("fido2: unsupported ec2 key parameters")
		}
		pub := &ecdsa.PublicKey{
			Curve: elliptic.P256(),
			X:     new(big.Int).SetBytes(xBytes),
			Y:     new(big.Int).SetBytes(yBytes),
		}
		if len(signature) != 64 {
			return fmt.Errorf("fido2: es256 signature size")
		}
		r := new(big.Int).SetBytes(signature[:32])
		s := new(big.Int).SetBytes(signature[32:])
		hash := sha256.Sum256(message)
		if !ecdsa.Verify(pub, hash[:], r, s) {
			return fmt.Errorf("fido2: es256 signature invalid")
		}
		return nil
	case AlgorithmEdDSA:
		curve, hasCurve := coseInteger(fields[-1])
		okp, hasOKP := coseBytes(fields[-2])
		if keyType != coseKeyTypeOKP || !hasCurve || curve != coseCurveEd25519 || !hasOKP || len(okp) != ed25519.PublicKeySize {
			return fmt.Errorf("fido2: unsupported okp key parameters")
		}
		pub := ed25519.PublicKey(append([]byte(nil), okp...))
		if !ed25519.Verify(pub, message, signature) {
			return fmt.Errorf("fido2: eddsa signature invalid")
		}
		return nil
	default:
		return fmt.Errorf("fido2: unsupported signature algorithm")
	}
}
