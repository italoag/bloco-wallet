package wallet

import (
	"errors"
	"testing"
)

type mockRepo struct {
	ret    *Wallet
	retErr error
}

func (m *mockRepo) AddWallet(w *Wallet) error                   { return nil }
func (m *mockRepo) GetAllWallets() ([]Wallet, error)            { return nil, nil }
func (m *mockRepo) DeleteWallet(walletID int) error             { return nil }
func (m *mockRepo) FindBySourceHash(sourceHash string) (*Wallet, error) {
	return m.ret, m.retErr
}
func (m *mockRepo) Close() error { return nil }
func (m *mockRepo) FindByAddress(address string) ([]Wallet, error) { return nil, nil }
func (m *mockRepo) FindByAddressAndMethod(address, importMethod string) ([]Wallet, error) {
	return nil, nil
}

func TestValidateUniqueSourceHash_Empty(t *testing.T) {
	repo := &mockRepo{}
	err := ValidateUniqueSourceHash(repo, "  \t\n", ImportMethodMnemonic)
	if err == nil {
		t.Fatalf("expected error for empty source hash, got nil")
	}
	var inv *InvalidImportDataError
	if !errors.As(err, &inv) {
		t.Fatalf("expected InvalidImportDataError, got %T: %v", err, err)
	}
}

func TestValidateUniqueSourceHash_NoCollision(t *testing.T) {
	repo := &mockRepo{ret: nil, retErr: nil}
	err := ValidateUniqueSourceHash(repo, "abcd", ImportMethodKeystore)
	if err != nil {
		t.Fatalf("expected nil, got error: %v", err)
	}
}

func TestValidateUniqueSourceHash_Collision(t *testing.T) {
	addr := "0xABC"
	repo := &mockRepo{ret: &Wallet{Address: addr}, retErr: nil}
	err := ValidateUniqueSourceHash(repo, "ffff", ImportMethodPrivateKey)
	if err == nil {
		t.Fatalf("expected duplicate error, got nil")
	}
	var dup *DuplicateWalletError
	if !errors.As(err, &dup) {
		t.Fatalf("expected DuplicateWalletError, got %T: %v", err, err)
	}
	if dup.Type != string(ImportMethodPrivateKey) {
		t.Fatalf("unexpected dup.Type: %s", dup.Type)
	}
	if dup.Address != addr {
		t.Fatalf("unexpected dup.Address: %s", dup.Address)
	}
}
