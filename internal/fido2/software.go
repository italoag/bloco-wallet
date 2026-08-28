package fido2

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"

	"github.com/fxamacker/cbor/v2"
)

// SoftwareAuthenticator is a test-only WebAuthn authenticator that produces
// structurally valid registration and assertion responses signed with a real
// P-256 key. It is used to exercise the verifier end to end without hardware.
type SoftwareAuthenticator struct {
	credentialID []byte
	privateKey   *ecdsa.PrivateKey
	signCount    uint32
	userHandle   []byte
}

// NewSoftwareAuthenticator creates an authenticator with a fresh key.
func NewSoftwareAuthenticator(userHandle []byte) (*SoftwareAuthenticator, error) {
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	credentialID := make([]byte, 32)
	if _, err := rand.Read(credentialID); err != nil {
		return nil, err
	}
	if len(userHandle) == 0 || len(userHandle) > MaxUserHandleBytes {
		return nil, fmt.Errorf("fido2: invalid user handle")
	}
	return &SoftwareAuthenticator{credentialID: credentialID, privateKey: privateKey, userHandle: userHandle}, nil
}

// CredentialID returns the authenticator credential identifier.
func (authenticator *SoftwareAuthenticator) CredentialID() []byte {
	return append([]byte(nil), authenticator.credentialID...)
}

// COSEPublicKey encodes the P-256 public key in COSE form.
func (authenticator *SoftwareAuthenticator) COSEPublicKey() ([]byte, error) {
	key := map[int]any{
		1:  int64(coseKeyTypeEC2),
		3:  int64(AlgorithmES256),
		-1: int64(coseCurveP256),
		-2: authenticator.privateKey.X.FillBytes(make([]byte, 32)),
		-3: authenticator.privateKey.Y.FillBytes(make([]byte, 32)),
	}
	return cbor.Marshal(key)
}

// RegistrationResponse builds a packed self-attestation registration
// response for the given challenge, rpID, and origin.
func (authenticator *SoftwareAuthenticator) RegistrationResponse(challenge, rpID, origin string, requireUV bool) (RegistrationResponse, error) {
	flags := FlagUP | FlagAT
	if requireUV {
		flags |= FlagUV
	}
	return authenticator.RegistrationResponseFlags(challenge, rpID, origin, flags)
}

// RegistrationResponseFlags builds a registration response with explicit
// authenticator flags, allowing tests to exercise missing presence.
func (authenticator *SoftwareAuthenticator) RegistrationResponseFlags(challenge, rpID, origin string, flags byte) (RegistrationResponse, error) {
	var response RegistrationResponse
	clientDataJSON, err := authenticator.clientData("webauthn.create", challenge, origin)
	if err != nil {
		return response, err
	}
	publicKey, err := authenticator.COSEPublicKey()
	if err != nil {
		return response, err
	}
	authData := authenticator.authData(rpID, flags)
	attested := append([]byte(nil), make([]byte, 16)...) // aaguid
	attested = append(attested, byte(len(authenticator.credentialID)>>8), byte(len(authenticator.credentialID)))
	attested = append(attested, authenticator.credentialID...)
	attested = append(attested, publicKey...)
	authData = append(authData, attested...)
	clientDataHash := sha256.Sum256(clientDataJSON)
	signature, err := authenticator.sign(append(append([]byte(nil), authData...), clientDataHash[:]...))
	if err != nil {
		return response, err
	}
	attestationObject, err := cbor.Marshal(map[string]any{
		"fmt":      "packed",
		"attStmt":  map[int]any{-1: signature, -2: [][]byte{authenticator.credentialID}, -3: int64(AlgorithmES256)},
		"authData": authData,
	})
	if err != nil {
		return response, err
	}
	response.ID = base64.RawURLEncoding.EncodeToString(authenticator.credentialID)
	response.RawID = append([]byte(nil), authenticator.credentialID...)
	response.Type = "public-key"
	response.Response.ClientDataJSON = clientDataJSON
	response.Response.AttestationObject = attestationObject
	return response, nil
}

// AssertionResponse builds an assertion response for the given challenge.
func (authenticator *SoftwareAuthenticator) AssertionResponse(challenge, rpID, origin string, requireUV bool) (AssertionResponse, error) {
	flags := FlagUP
	if requireUV {
		flags |= FlagUV
	}
	return authenticator.AssertionResponseFlags(challenge, rpID, origin, flags)
}

// AssertionResponseFlags builds an assertion with explicit flags.
func (authenticator *SoftwareAuthenticator) AssertionResponseFlags(challenge, rpID, origin string, flags byte) (AssertionResponse, error) {
	var response AssertionResponse
	clientDataJSON, err := authenticator.clientData("webauthn.get", challenge, origin)
	if err != nil {
		return response, err
	}
	authData := authenticator.authData(rpID, flags)
	clientDataHash := sha256.Sum256(clientDataJSON)
	signature, err := authenticator.sign(append(append([]byte(nil), authData...), clientDataHash[:]...))
	if err != nil {
		return response, err
	}
	response.ID = base64.RawURLEncoding.EncodeToString(authenticator.credentialID)
	response.RawID = append([]byte(nil), authenticator.credentialID...)
	response.Type = "public-key"
	response.Response.ClientDataJSON = clientDataJSON
	response.Response.AuthenticatorData = authData
	response.Response.Signature = signature
	response.Response.UserHandle = append([]byte(nil), authenticator.userHandle...)
	return response, nil
}

func (authenticator *SoftwareAuthenticator) clientData(kind, challenge, origin string) ([]byte, error) {
	return json.Marshal(map[string]any{
		"type":      kind,
		"challenge": challenge,
		"origin":    origin,
	})
}

func (authenticator *SoftwareAuthenticator) authData(rpID string, flags byte) []byte {
	rpIDHash := sha256.Sum256([]byte(rpID))
	authenticator.signCount++
	data := append([]byte(nil), rpIDHash[:]...)
	data = append(data, flags)
	var counter [4]byte
	binary.BigEndian.PutUint32(counter[:], authenticator.signCount)
	data = append(data, counter[:]...)
	return data
}

func (authenticator *SoftwareAuthenticator) sign(message []byte) ([]byte, error) {
	hash := sha256.Sum256(message)
	r, s, err := ecdsa.Sign(rand.Reader, authenticator.privateKey, hash[:])
	if err != nil {
		return nil, err
	}
	signature := make([]byte, 64)
	r.FillBytes(signature[:32])
	s.FillBytes(signature[32:])
	return signature, nil
}

var _ = bytes.Equal
