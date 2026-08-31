package backup

import (
	"bytes"
	"testing"
	"time"

	"blocowallet/internal/wallet"
)

func testAccounts() []wallet.Account {
	return []wallet.Account{
		{
			AccountID: "11111111-1111-4111-8111-111111111111", Name: "Alpha",
			Address:    "0x9d8A62f656a8d1615C1294fd71e9CFb3E4855A4F",
			SignerKind: wallet.SignerKindSoftware, SignerReference: "11111111-1111-4111-8111-111111111111",
			SecretType: wallet.SecretTypeMnemonic, SecretEnvelope: []byte("0123456789abcdef-envelope-a"),
			State: wallet.AccountStateActive, Capabilities: wallet.CapabilitySignTransaction | wallet.CapabilitySignMessage,
			SourceIdentity: "alpha-source", AuthorizationEpoch: 1, EnvelopeGeneration: 1, BackupGeneration: 1,
			DerivationScheme: "bip44", DerivationPath: "m/44'/60'/0'/0/0", BIP39Language: "english",
		},
		{
			AccountID: "22222222-2222-4222-8222-222222222222", Name: "Vault",
			Address:    "0xCcCCccccCCCCcCCCCCCcCcCccCcCCCcCcccccccC",
			SignerKind: wallet.SignerKindCloud, SignerReference: "cloud:v1:https://vault.example",
			State: wallet.AccountStateActive, SourceIdentity: "vault-source", AuthorizationEpoch: 1, EnvelopeGeneration: 1, BackupGeneration: 1,
		},
	}
}

func TestBackupRoundTripWithAuthentication(t *testing.T) {
	deriver := &Argon2idDeriver{Time: 1, Memory: 64 * 1024, Threads: 4}
	sealer, err := NewSealer(deriver)
	if err != nil {
		t.Fatal(err)
	}
	password := []byte("correct horse battery staple")
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC).UnixMilli()
	archive, err := sealer.Create(password, 14, testAccounts(), now)
	if err != nil {
		t.Fatal(err)
	}
	if len(archive) == 0 || VerifyHash(archive) == ([32]byte{}) {
		t.Fatal("archive was not materialized")
	}
	manifest, err := sealer.Open(password, archive)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Schema != 14 || manifest.CreatedAtMS != now || len(manifest.Accounts) != 2 {
		t.Fatalf("unexpected manifest: %+v", manifest)
	}
	if manifest.Accounts[0].SecretEnvelope == nil || !bytes.Equal(manifest.Accounts[0].SecretEnvelope, []byte("0123456789abcdef-envelope-a")) {
		t.Fatal("secret envelope did not survive the round trip")
	}
	if manifest.Accounts[1].SignerKind != string(wallet.SignerKindCloud) || len(manifest.Accounts[1].SecretEnvelope) != 0 {
		t.Fatal("custody-free account leaked an envelope")
	}
}

func TestBackupRejectsCorruptionAndWrongPassword(t *testing.T) {
	deriver := &Argon2idDeriver{Time: 1, Memory: 64 * 1024, Threads: 4}
	sealer, err := NewSealer(deriver)
	if err != nil {
		t.Fatal(err)
	}
	archive, err := sealer.Create([]byte("a sufficiently long password"), 14, testAccounts(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sealer.Open([]byte("wrong but long enough password"), archive); err == nil {
		t.Fatal("wrong password opened the archive")
	}
	tampered := append([]byte(nil), archive...)
	tampered[len(tampered)-1] ^= 0x01
	if _, err := sealer.Open([]byte("a sufficiently long password"), tampered); err == nil {
		t.Fatal("tampered archive was accepted")
	}
	truncated := archive[:len(archive)/2]
	if _, err := sealer.Open([]byte("a sufficiently long password"), truncated); err == nil {
		t.Fatal("truncated archive was accepted")
	}
	badHeader := append([]byte(nil), archive...)
	copy(badHeader[:4], []byte("XXXX"))
	if _, err := sealer.Open([]byte("a sufficiently long password"), badHeader); err == nil {
		t.Fatal("foreign header was accepted")
	}
	empty := sealerEmptyOpen(sealer)
	if _, err := sealer.Open([]byte("a sufficiently long password"), empty); err == nil {
		t.Fatal("empty archive was accepted")
	}
	if _, err := sealer.Create([]byte("short"), 14, testAccounts(), 1); err == nil {
		t.Fatal("short password was accepted")
	}
	if _, err := sealer.Create([]byte("not printable \x01"), 14, testAccounts(), 1); err == nil {
		t.Fatal("non-printable password was accepted")
	}
}

func sealerEmptyOpen(sealer *Sealer) []byte {
	return nil
}

func TestBackupRejectsEmptyPasswordAndAccountBudget(t *testing.T) {
	sealer, err := NewSealer(&Argon2idDeriver{Time: 1, Memory: 64 * 1024, Threads: 4})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sealer.Create(nil, 14, testAccounts(), 1); err == nil {
		t.Fatal("empty password was accepted")
	}
	if _, err := sealer.Create([]byte("password"), 0, testAccounts(), 1); err == nil {
		t.Fatal("zero schema version was accepted")
	}
	tooMany := make([]wallet.Account, MaxAccounts+1)
	if _, err := sealer.Create([]byte("password"), 14, tooMany, 1); err == nil {
		t.Fatal("account budget overflow was accepted")
	}
}
