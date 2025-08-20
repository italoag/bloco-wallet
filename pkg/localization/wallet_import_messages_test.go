package localization

import "testing"

func TestAddWalletImportMessages(t *testing.T) {
	if Labels == nil {
		Labels = make(map[string]string)
	}

	testCases := []struct {
		name     string
		lang     string
		key      string
		expected string
	}{
		{"EN duplicate mnemonic", "en", "duplicate_mnemonic", "A wallet with this mnemonic phrase already exists"},
		{"PT duplicate mnemonic", "pt", "duplicate_mnemonic", "Uma carteira com esta frase mnemônica já existe"},
		{"ES duplicate mnemonic", "es", "duplicate_mnemonic", "Ya existe una cartera con esta frase mnemónica"},
		{"EN no mnemonic", "en", "no_mnemonic_available", "Mnemonic not available (imported via private key)"},
		{"PT no mnemonic", "pt", "no_mnemonic_available", "Mnemônica não disponível (importada via chave privada)"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			SetCurrentLanguage(tc.lang)
			for k := range Labels {
				delete(Labels, k)
			}
			AddWalletImportMessages()
			if Labels[tc.key] != tc.expected {
				t.Fatalf("expected %q, got %q for key %q in lang %q", tc.expected, Labels[tc.key], tc.key, tc.lang)
			}
		})
	}
}

func TestFormatDuplicateImportError(t *testing.T) {
	SetCurrentLanguage("en")
	Labels = make(map[string]string)
	AddWalletImportMessages()

	msg := FormatDuplicateImportError("mnemonic", "mnemonic", "0xABC")
	if msg == "" {
		t.Fatal("got empty message")
	}
	if wantSub := "A wallet with this mnemonic phrase already exists"; !contains(msg, wantSub) {
		t.Fatalf("expected base message to contain %q, got %q", wantSub, msg)
	}
	if wantSub := "[Method: Mnemonic]"; !contains(msg, wantSub) {
		t.Fatalf("expected method label, got %q", msg)
	}
	if wantSub := "(0xABC)"; !contains(msg, wantSub) {
		t.Fatalf("expected address context, got %q", msg)
	}
}

func TestGetNoMnemonicAvailableMessage(t *testing.T) {
	SetCurrentLanguage("pt")
	Labels = make(map[string]string)
	AddWalletImportMessages()

	msg := GetNoMnemonicAvailableMessage("private_key")
	if msg != "Mnemônica não disponível (importada via chave privada)" {
		t.Fatalf("unexpected message: %q", msg)
	}
}

// contains helper (no strings.Contains to keep it simple and explicit)
func contains(s, substr string) bool {
	// simple rune-wise search
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
