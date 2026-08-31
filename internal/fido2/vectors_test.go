package fido2

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"testing"
)

// TestParseAuthenticatorDataAgainstSpecConstruction pins the parser against
// an authenticator data structure built manually from the WebAuthn spec
// (rpIdHash = SHA-256(rpId), flags, counter) — not by the package's own
// authenticator.
func TestParseAuthenticatorDataAgainstSpecConstruction(t *testing.T) {
	rpIDHash := sha256.Sum256([]byte("example.com"))
	authData := make([]byte, 0, 37)
	authData = append(authData, rpIDHash[:]...)
	authData = append(authData, FlagUP|FlagUV)
	var counter [4]byte
	binary.BigEndian.PutUint32(counter[:], 42)
	authData = append(authData, counter[:]...)

	parsed, err := parseAuthenticatorData(authData)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Flags&FlagUP == 0 || parsed.Flags&FlagUV == 0 || parsed.Flags&FlagAT != 0 {
		t.Fatalf("unexpected flags: %#x", parsed.Flags)
	}
	if parsed.SignCount != 42 {
		t.Fatalf("unexpected counter: %d", parsed.SignCount)
	}
	if !bytes.Equal(parsed.RPIDHash[:], rpIDHash[:]) {
		t.Fatal("rpIdHash was not preserved")
	}
	// Truncated data and oversized extensions are rejected.
	if _, err := parseAuthenticatorData(authData[:36]); err == nil {
		t.Fatal("truncated authenticator data was accepted")
	}
	oversized := append(append([]byte(nil), authData...), bytes.Repeat([]byte{0xA0}, MaxExtensionBytes+1)...)
	oversized[32] |= FlagED
	if _, err := parseAuthenticatorData(oversized); err == nil {
		t.Fatal("oversized extension blob was accepted")
	}
}

// TestClientDataJSONFieldsAgainstSpec pins the clientDataJSON shape the
// WebAuthn spec requires: type, challenge, origin, crossOrigin.
func TestClientDataJSONFieldsAgainstSpec(t *testing.T) {
	clientData, err := json.Marshal(map[string]any{
		"type":        "webauthn.get",
		"challenge":   "AAECAwQFBgcICQoLDA0ODw",
		"origin":      "https://login.example.com",
		"crossOrigin": false,
	})
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := parseClientData(clientData)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Type != "webauthn.get" || parsed.Challenge != "AAECAwQFBgcICQoLDA0ODw" || parsed.Origin != "https://login.example.com" || parsed.CrossOrigin {
		t.Fatalf("client data fields mismatch: %+v", parsed)
	}
	// Trailing JSON is rejected by the strict decoder.
	if _, err := parseClientData(append(clientData, []byte(" {}")...)); err == nil {
		t.Fatal("client data with trailing JSON was accepted")
	}
	// crossOrigin ceremonies must be rejected by the verifier.
	crossOrigin, err := json.Marshal(map[string]any{
		"type": "webauthn.get", "challenge": "c", "origin": "https://a.example", "crossOrigin": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	parsed, err = parseClientData(crossOrigin)
	if err != nil {
		t.Fatal(err)
	}
	if !parsed.CrossOrigin {
		t.Fatal("crossOrigin flag was lost")
	}
}

// TestAttestedCredentialDataAgainstSpecConstruction builds attested
// credential data manually (aaguid + length + id + COSE key) and checks the
// parser, including the COSE algorithm discovery.
func TestAttestedCredentialDataAgainstSpecConstruction(t *testing.T) {
	credentialID := bytes.Repeat([]byte{0xAB}, 32)
	aaguid := bytes.Repeat([]byte{0x11}, 16)
	keyData := []byte{
		0xa5, 0x01, 0x02, 0x03, 0x26, 0x20, 0x01, 0x21, 0x58, 0x20,
	}
	keyData = append(keyData, bytes.Repeat([]byte{0x77}, 32)...)
	keyData = append(keyData, 0x22, 0x58, 0x20)
	keyData = append(keyData, bytes.Repeat([]byte{0x88}, 32)...)

	data := make([]byte, 0, 16+2+32+len(keyData))
	data = append(data, aaguid...)
	data = append(data, byte(len(credentialID)>>8), byte(len(credentialID)))
	data = append(data, credentialID...)
	data = append(data, keyData...)

	attested, err := parseAttestedCredentialData(data)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(attested.credentialID, credentialID) {
		t.Fatal("credential id mismatch")
	}
	if attested.algorithm != AlgorithmES256 {
		t.Fatalf("unexpected algorithm: %d", attested.algorithm)
	}
	if _, err := parseAttestedCredentialData(data[:16]); err == nil {
		t.Fatal("truncated attested data was accepted")
	}
	// A fixed-bytes key that is not COSE is rejected.
	if _, err := parseAttestedCredentialData(data[:18]); err == nil {
		t.Fatal("short attested data was accepted")
	}
}
