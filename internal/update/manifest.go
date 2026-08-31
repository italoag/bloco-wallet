// Package update implements signed update manifests. A manifest pins the
// release version, artifact hashes, and a minimum compatible app version,
// and is signed with the release ECDSA key. Clients verify the canonical
// manifest bytes before trusting any artifact URL.
package update

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"strings"
	"time"
)

const (
	// MaxManifestBytes bounds a manifest payload.
	MaxManifestBytes = 64 << 10
	// MaxArtifacts bounds the artifact list.
	MaxArtifacts = 32
	// MaxArtifactText bounds artifact identifiers.
	MaxArtifactText = 512
	// MaxSignatureBytes bounds the detached signature.
	MaxSignatureBytes = 256
	// MaxManifestAge bounds how old a signed manifest may be.
	MaxManifestAge = 180 * 24 * time.Hour
)

// Artifact pins one release file.
type Artifact struct {
	Name   string `json:"name"`
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

// Manifest is the signed update description.
type Manifest struct {
	Version           string     `json:"version"`
	MinimumAppVersion string     `json:"minimum_app_version"`
	PublishedAt       string     `json:"published_at"`
	Artifacts         []Artifact `json:"artifacts"`
	// Signature is hex of the ECDSA signature over the canonical payload.
	Signature string `json:"signature"`
}

// CanonicalPayload returns the bytes that are signed: version, minimum app
// version, published timestamp, and the artifact pins in stable order.
func (manifest *Manifest) CanonicalPayload() ([]byte, error) {
	if manifest == nil {
		return nil, fmt.Errorf("update: nil manifest")
	}
	if manifest.Version == "" || manifest.MinimumAppVersion == "" || manifest.PublishedAt == "" {
		return nil, fmt.Errorf("update: incomplete manifest")
	}
	if len(manifest.Artifacts) == 0 || len(manifest.Artifacts) > MaxArtifacts {
		return nil, fmt.Errorf("update: artifact budget")
	}
	payload := make([]byte, 0, 1024)
	payload = append(payload, []byte("bloco-wallet update manifest v1\n")...)
	payload = append(payload, []byte("version: "+manifest.Version+"\n")...)
	payload = append(payload, []byte("minimum_app_version: "+manifest.MinimumAppVersion+"\n")...)
	payload = append(payload, []byte("published_at: "+manifest.PublishedAt+"\n")...)
	for _, artifact := range manifest.Artifacts {
		if artifact.Name == "" || artifact.URL == "" || artifact.SHA256 == "" || len(artifact.Name) > MaxArtifactText || len(artifact.URL) > MaxArtifactText || len(artifact.SHA256) != 64 || artifact.Size <= 0 {
			return nil, fmt.Errorf("update: invalid artifact pin")
		}
		if !strings.HasPrefix(artifact.URL, "https://") {
			return nil, fmt.Errorf("update: artifact url must be https")
		}
		if _, err := hex.DecodeString(artifact.SHA256); err != nil {
			return nil, fmt.Errorf("update: invalid artifact hash")
		}
		payload = append(payload, []byte("artifact: "+artifact.Name+" "+artifact.SHA256+" "+fmt.Sprint(artifact.Size)+" "+artifact.URL+"\n")...)
	}
	if len(payload) > MaxManifestBytes {
		return nil, fmt.Errorf("update: manifest too large")
	}
	return payload, nil
}

// Verify checks the signature, shape, and version ordering.
func Verify(manifest *Manifest, publicKey *ecdsa.PublicKey, currentVersion string, now time.Time) error {
	if publicKey == nil {
		return fmt.Errorf("update: release key required")
	}
	if manifest == nil {
		return fmt.Errorf("update: nil manifest")
	}
	if manifest.Signature == "" || len(manifest.Signature) > MaxSignatureBytes {
		return fmt.Errorf("update: signature missing")
	}
	signature, err := hex.DecodeString(manifest.Signature)
	if err != nil || len(signature) == 0 || len(signature) > 72 {
		return fmt.Errorf("update: invalid signature encoding")
	}
	payload, err := manifest.CanonicalPayload()
	if err != nil {
		return err
	}
	hash := sha256.Sum256(payload)
	if !ecdsa.VerifyASN1(publicKey, hash[:], signature) {
		return fmt.Errorf("update: signature verification failed")
	}
	published, err := time.Parse(time.RFC3339, manifest.PublishedAt)
	if err != nil {
		return fmt.Errorf("update: invalid published timestamp")
	}
	if published.After(now.Add(time.Hour)) {
		return fmt.Errorf("update: manifest timestamp in the future")
	}
	if now.Sub(published) > MaxManifestAge {
		return fmt.Errorf("update: manifest is stale")
	}
	if manifest.Version == currentVersion {
		return fmt.Errorf("update: manifest version matches the installed version")
	}
	if compareVersions(manifest.Version, currentVersion) <= 0 {
		return fmt.Errorf("update: manifest is not newer than the installed version")
	}
	if compareVersions(manifest.MinimumAppVersion, currentVersion) > 0 {
		return fmt.Errorf("update: installed version is below the minimum")
	}
	return nil
}

// compareVersions compares dotted numeric versions; malformed versions sort
// as zero.
func compareVersions(left, right string) int {
	leftParts := parseVersion(left)
	rightParts := parseVersion(right)
	for index := 0; index < 3; index++ {
		if leftParts[index] != rightParts[index] {
			if leftParts[index] < rightParts[index] {
				return -1
			}
			return 1
		}
	}
	return 0
}

func parseVersion(value string) [3]int {
	var parts [3]int
	fields := strings.Split(strings.TrimPrefix(value, "v"), ".")
	for index := 0; index < len(fields) && index < 3; index++ {
		number := 0
		for _, character := range fields[index] {
			if character < '0' || character > '9' {
				number = -1
				break
			}
			number = number*10 + int(character-'0')
		}
		if number >= 0 {
			parts[index] = number
		}
	}
	return parts
}

// ParsePublicKey decodes an embedded PEM release key.
func ParsePublicKey(pemBytes []byte) (*ecdsa.PublicKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, fmt.Errorf("update: release key decode failed")
	}
	key, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("update: release key parse: %w", err)
	}
	publicKey, ok := key.(*ecdsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("update: release key is not ecdsa")
	}
	if publicKey.Curve != elliptic.P256() {
		return nil, fmt.Errorf("update: release key must use P-256")
	}
	return publicKey, nil
}

// CompareVersionsForTest exposes numeric version ordering for tests.
func CompareVersionsForTest(left, right string) int {
	return compareVersions(left, right)
}
