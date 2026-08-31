package update_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"testing"
	"time"

	"blocowallet/internal/update"
)

func signManifest(t *testing.T, key *ecdsa.PrivateKey, manifest *update.Manifest) {
	t.Helper()
	payload, err := manifest.CanonicalPayload()
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(payload)
	signature, err := ecdsa.SignASN1(rand.Reader, key, hash[:])
	if err != nil {
		t.Fatal(err)
	}
	manifest.Signature = hex.EncodeToString(signature)
}

func pemPublicKey(t *testing.T, key *ecdsa.PublicKey) []byte {
	t.Helper()
	der, err := x509.MarshalPKIXPublicKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})
}

func TestUpdateManifestVerifyAcceptsValidSignedRelease(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	manifest := &update.Manifest{
		Version: "v1.2.0", MinimumAppVersion: "v1.0.0",
		PublishedAt: now.Add(-time.Hour).Format(time.RFC3339),
		Artifacts: []update.Artifact{
			{Name: "bloco-wallet-darwin-amd64", URL: "https://releases.example/bloco-wallet-darwin-amd64", SHA256: hex.EncodeToString(make([]byte, 32)), Size: 1234},
		},
	}
	signManifest(t, key, manifest)
	publicKey, err := update.ParsePublicKey(pemPublicKey(t, &key.PublicKey))
	if err != nil {
		t.Fatal(err)
	}
	if err := update.Verify(manifest, publicKey, "v1.1.0", now); err != nil {
		t.Fatal(err)
	}
	// A wrong key must reject the manifest.
	otherKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if err := update.Verify(manifest, &otherKey.PublicKey, "v1.1.0", now); err == nil {
		t.Fatal("manifest signed by another key was accepted")
	}
	// Downgrades are rejected.
	if err := update.Verify(manifest, publicKey, "v1.2.0", now); err == nil {
		t.Fatal("same-version manifest was accepted")
	}
	if err := update.Verify(manifest, publicKey, "v1.3.0", now); err == nil {
		t.Fatal("downgrade manifest was accepted")
	}
	// Below-minimum app versions are rejected.
	if err := update.Verify(manifest, publicKey, "v0.9.0", now); err == nil {
		t.Fatal("below-minimum app version was accepted")
	}
	// Future timestamps are rejected.
	future := *manifest
	future.PublishedAt = now.Add(2 * time.Hour).Format(time.RFC3339)
	signManifest(t, key, &future)
	if err := update.Verify(&future, publicKey, "v1.1.0", now); err == nil {
		t.Fatal("future-dated manifest was accepted")
	}
	// Stale manifests are rejected.
	stale := *manifest
	stale.PublishedAt = now.Add(-365 * 24 * time.Hour).Format(time.RFC3339)
	signManifest(t, key, &stale)
	if err := update.Verify(&stale, publicKey, "v1.1.0", now); err == nil {
		t.Fatal("stale manifest was accepted")
	}
	// Non-P-256 release keys are rejected.
	otherCurveKey, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := update.ParsePublicKey(pemPublicKey(t, &otherCurveKey.PublicKey)); err == nil {
		t.Fatal("non-P-256 release key was accepted")
	}
	// Insecure artifact URLs are rejected before signature checks.
	httpArtifact := *manifest
	httpArtifact.Artifacts[0].URL = "http://releases.example/a"
	if err := update.Verify(&httpArtifact, publicKey, "v1.1.0", now); err == nil {
		t.Fatal("http artifact url was accepted")
	}
}

func TestUpdateManifestRejectsTampering(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	manifest := &update.Manifest{
		Version: "v1.2.0", MinimumAppVersion: "v1.0.0",
		PublishedAt: now.Add(-time.Hour).Format(time.RFC3339),
		Artifacts: []update.Artifact{
			{Name: "bloco-wallet", URL: "https://releases.example/a", SHA256: hex.EncodeToString(make([]byte, 32)), Size: 100},
		},
	}
	signManifest(t, key, manifest)
	publicKey, err := update.ParsePublicKey(pemPublicKey(t, &key.PublicKey))
	if err != nil {
		t.Fatal(err)
	}
	tampered := *manifest
	tampered.Artifacts[0].SHA256 = hex.EncodeToString(make([]byte, 32))
	tampered.Artifacts[0].SHA256 = hex.EncodeToString(append(make([]byte, 31), 1))
	if err := update.Verify(&tampered, publicKey, "v1.1.0", now); err == nil {
		t.Fatal("tampered artifact hash was accepted")
	}
	unsigned := *manifest
	unsigned.Signature = ""
	if err := update.Verify(&unsigned, publicKey, "v1.1.0", now); err == nil {
		t.Fatal("unsigned manifest was accepted")
	}
	// Malformed artifact pins are rejected before signature checks.
	malformed := *manifest
	malformed.Artifacts[0].Size = 0
	if err := update.Verify(&malformed, publicKey, "v1.1.0", now); err == nil {
		t.Fatal("malformed artifact pin was accepted")
	}
	// Version ordering is numeric, not lexical.
	if update.CompareVersionsForTest("v1.10.0", "v1.9.0") != 1 {
		t.Fatal("numeric version ordering failed")
	}
}
