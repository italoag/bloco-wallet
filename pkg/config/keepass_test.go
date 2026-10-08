package config

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const (
	keepassTestTarget = "11111111-1111-4111-8111-111111111111"
	keepassTestVault  = "22222222-2222-4222-8222-222222222222"
)

func writeKeePassConfig(t *testing.T, appDir, keepassBlock string) {
	t.Helper()
	defaults, err := defaultConfig.ReadFile("default_config.toml")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(appDir, "config.toml"), append(defaults, []byte(keepassBlock)...), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestKeePassConfigLoadConfigRoundTrip(t *testing.T) {
	appDir := t.TempDir()
	vaultPath := filepath.Join(t.TempDir(), "vault.kdbx")
	writeKeePassConfig(t, appDir, `
[keepass]
enabled = true
path = `+strconv.Quote(vaultPath)+`
target_id = "`+keepassTestTarget+`"
vault_id = "`+keepassTestVault+`"
`)
	cfg, err := LoadConfig(appDir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !cfg.KeePass.Enabled || cfg.KeePass.Path != vaultPath || cfg.KeePass.TargetID != keepassTestTarget || cfg.KeePass.VaultID != keepassTestVault {
		t.Fatalf("keepass binding mismatch: %+v", cfg.KeePass)
	}
}

func TestKeePassConfigLoadConfigExpandsHome(t *testing.T) {
	appDir := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	writeKeePassConfig(t, appDir, `
[keepass]
enabled = false
path = "~/vault.kdbx"
target_id = "`+keepassTestTarget+`"
vault_id = "`+keepassTestVault+`"
`)
	cfg, err := LoadConfig(appDir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.KeePass.Path != filepath.Join(home, "vault.kdbx") {
		t.Fatalf("path not expanded: %q", cfg.KeePass.Path)
	}
	if cfg.KeePass.Enabled {
		t.Fatal("disabled binding reported enabled")
	}
}

func TestKeePassConfigManagerRoundTrip(t *testing.T) {
	appDir := t.TempDir()
	t.Setenv("BLOCO_WALLET_APP_APP_DIR", appDir)
	vaultPath := filepath.Join(t.TempDir(), "vault.kdbx")
	writeKeePassConfig(t, appDir, `
[keepass]
enabled = true
path = `+strconv.Quote(vaultPath)+`
target_id = "`+keepassTestTarget+`"
vault_id = "`+keepassTestVault+`"
`)
	manager := NewConfigurationManager()
	cfg, err := manager.LoadConfiguration()
	if err != nil {
		t.Fatalf("manager load: %v", err)
	}
	if !cfg.KeePass.Enabled || cfg.KeePass.TargetID != keepassTestTarget || cfg.KeePass.VaultID != keepassTestVault || cfg.KeePass.Path != vaultPath {
		t.Fatalf("manager keepass binding mismatch: %+v", cfg.KeePass)
	}
	cfg.KeePass.Enabled = false
	if err := manager.SaveConfiguration(cfg); err != nil {
		t.Fatalf("save disabled binding: %v", err)
	}
	reloaded, err := manager.LoadConfiguration()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reloaded.KeePass.Enabled || reloaded.KeePass.Path != vaultPath || reloaded.KeePass.TargetID != keepassTestTarget {
		t.Fatalf("disabled binding not retained: %+v", reloaded.KeePass)
	}
	data, err := os.ReadFile(filepath.Join(appDir, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	section := text
	if index := strings.Index(text, "[keepass]"); index >= 0 {
		section = text[index:]
		if end := strings.Index(section[1:], "["); end >= 0 {
			section = section[:end+1]
		}
	}
	for _, banned := range []string{"password", "secret", "mnemonic", "private_key", "master"} {
		if strings.Contains(section, banned) {
			t.Fatalf("serialized keepass config contains %q", banned)
		}
	}
}

func TestKeePassConfigValidation(t *testing.T) {
	vaultPath := filepath.Join(t.TempDir(), "vault.kdbx")
	cases := []struct {
		name    string
		keepass KeePassConfig
		valid   bool
	}{
		{"zero disabled", KeePassConfig{}, true},
		{"enabled bound", KeePassConfig{Enabled: true, Path: vaultPath, TargetID: keepassTestTarget, VaultID: keepassTestVault}, true},
		{"disabled bound retained", KeePassConfig{Path: vaultPath, TargetID: keepassTestTarget, VaultID: keepassTestVault}, true},
		{"enabled unbound", KeePassConfig{Enabled: true}, false},
		{"partial binding", KeePassConfig{Path: vaultPath}, false},
		{"relative path", KeePassConfig{Enabled: true, Path: "vault.kdbx", TargetID: keepassTestTarget, VaultID: keepassTestVault}, false},
		{"wrong extension", KeePassConfig{Enabled: true, Path: filepath.Join(t.TempDir(), "vault.txt"), TargetID: keepassTestTarget, VaultID: keepassTestVault}, false},
		{"non canonical target", KeePassConfig{Enabled: true, Path: vaultPath, TargetID: "11111111111141118111111111111111", VaultID: keepassTestVault}, false},
		{"non v4 target", KeePassConfig{Enabled: true, Path: vaultPath, TargetID: "11111111-1111-1111-8111-111111111111", VaultID: keepassTestVault}, false},
		{"uppercase vault", KeePassConfig{Enabled: true, Path: vaultPath, TargetID: keepassTestTarget, VaultID: strings.ToUpper("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa")}, false},
	}
	for _, tc := range cases {
		cfg := &Config{KeePass: tc.keepass}
		_, err := marshalPersistentConfig(cfg)
		if tc.valid && err != nil {
			t.Fatalf("%s: unexpected rejection: %v", tc.name, err)
		}
		if !tc.valid && err == nil {
			t.Fatalf("%s: expected rejection", tc.name)
		}
	}
}

func TestKeePassConfigLoadRejectsInvalid(t *testing.T) {
	appDir := t.TempDir()
	writeKeePassConfig(t, appDir, `
[keepass]
enabled = true
path = "relative/vault.kdbx"
target_id = "`+keepassTestTarget+`"
vault_id = "`+keepassTestVault+`"
`)
	if _, err := LoadConfig(appDir); err == nil {
		t.Fatal("invalid keepass binding accepted")
	}
}
