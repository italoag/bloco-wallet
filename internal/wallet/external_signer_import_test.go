package wallet_test

import (
	"context"
	"testing"

	"blocowallet/internal/wallet"

	"github.com/ethereum/go-ethereum/common"
)

type importAccountRepository struct {
	created *wallet.Account
}

func (repository *importAccountRepository) CreateAccount(_ context.Context, account *wallet.Account) error {
	repository.created = account
	return nil
}
func (repository *importAccountRepository) GetAccount(context.Context, string) (*wallet.Account, error) {
	return repository.created, nil
}
func (repository *importAccountRepository) FindAccountBySourceIdentity(context.Context, string) (*wallet.Account, error) {
	return nil, &importError{"not found"}
}
func (repository *importAccountRepository) FindAccountsByAddress(context.Context, string) ([]wallet.Account, error) {
	return nil, nil
}
func (repository *importAccountRepository) ListAccounts(context.Context) ([]wallet.Account, error) {
	return nil, nil
}
func (repository *importAccountRepository) GetVaultMetadata(context.Context, string) (string, error) {
	return "", nil
}
func (repository *importAccountRepository) PutVaultMetadata(context.Context, string, string) error {
	return nil
}
func (repository *importAccountRepository) UpdateAccount(context.Context, *wallet.Account) error {
	return nil
}
func (repository *importAccountRepository) DeletePendingAccount(context.Context, string, uint64) error {
	return nil
}
func (repository *importAccountRepository) WithAccountTransaction(_ context.Context, operation func(wallet.AccountRepository) error) error {
	return operation(repository)
}

type importError struct{ message string }

func (err *importError) Error() string { return err.message }

func TestImportExternalSignerAccountBindsCustodyFreeAccounts(t *testing.T) {
	repository := &importAccountRepository{}
	cloud, err := wallet.ImportExternalSignerAccount(context.Background(), repository, wallet.ExternalSignerImportRequest{
		Name: "Vault", Address: "0x9d8A62f656a8d1615C1294fd71e9CFb3E4855A4F",
		SignerKind: wallet.SignerKindCloud, Reference: "cloud:v1:https://vault.example/sign",
		Capabilities: wallet.CapabilitySignTransaction | wallet.CapabilitySignMessage, AuthorizationEpoch: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if cloud.SignerKind != wallet.SignerKindCloud || cloud.Capabilities != wallet.CapabilitySignTransaction|wallet.CapabilitySignMessage || cloud.SecretType != "" || len(cloud.SecretEnvelope) != 0 || cloud.DerivationScheme != "" {
		t.Fatalf("cloud account leaked custody material: %+v", cloud)
	}
	if cloud.SignerReference != "cloud:v1:https://vault.example/sign" || cloud.SourceIdentity == "" {
		t.Fatalf("cloud account lost its reference: %+v", cloud)
	}
	multisig, err := wallet.ImportExternalSignerAccount(context.Background(), repository, wallet.ExternalSignerImportRequest{
		Name: "Safe", Address: "0xCcCCccccCCCCcCCCCCCcCcCccCcCCCcCcccccccC",
		SignerKind: wallet.SignerKindMultisig, Reference: "safe:v1:0xCcCCccccCCCCcCCCCCCcCcCccCcCCCcCcccccccC",
		AuthorizationEpoch: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if multisig.SignerKind != wallet.SignerKindMultisig || multisig.Capabilities != 0 || multisig.SecretType != "" {
		t.Fatalf("multisig account leaked custody material: %+v", multisig)
	}
}

func TestImportExternalSignerAccountRejectsInvalidBindings(t *testing.T) {
	repository := &importAccountRepository{}
	base := wallet.ExternalSignerImportRequest{
		Name: "Vault", Address: "0x9d8A62f656a8d1615C1294fd71e9CFb3E4855A4F",
		SignerKind: wallet.SignerKindCloud, Reference: "cloud:v1:https://vault.example/sign",
		Capabilities: wallet.CapabilitySignTransaction | wallet.CapabilitySignMessage, AuthorizationEpoch: 1,
	}
	cases := []struct {
		name   string
		mutate func(*wallet.ExternalSignerImportRequest)
	}{
		{"software kind", func(request *wallet.ExternalSignerImportRequest) { request.SignerKind = wallet.SignerKindSoftware }},
		{"watch-only kind", func(request *wallet.ExternalSignerImportRequest) { request.SignerKind = wallet.SignerKindWatchOnly }},
		{"unverified cloud capabilities", func(request *wallet.ExternalSignerImportRequest) { request.Capabilities = 0 }},
		{"export capability", func(request *wallet.ExternalSignerImportRequest) {
			request.Capabilities = wallet.CapabilityExportSecret
		}},
		{"multisig EOA capabilities", func(request *wallet.ExternalSignerImportRequest) { request.SignerKind = wallet.SignerKindMultisig }},
		{"lowercase address", func(request *wallet.ExternalSignerImportRequest) {
			request.Address = "0x9d8a62f656a8d1615c1294fd71e9cfb3e4855a4f"
		}},
		{"zero address", func(request *wallet.ExternalSignerImportRequest) {
			request.Address = "0x0000000000000000000000000000000000000000"
		}},
		{"empty reference", func(request *wallet.ExternalSignerImportRequest) { request.Reference = "" }},
		{"newline reference", func(request *wallet.ExternalSignerImportRequest) { request.Reference = "cloud:v1:\nhttps://evil" }},
		{"remote plaintext reference", func(request *wallet.ExternalSignerImportRequest) { request.Reference = "cloud:v1:http://vault.example" }},
		{"credential query reference", func(request *wallet.ExternalSignerImportRequest) {
			request.Reference = "cloud:v1:https://vault.example?token=secret"
		}},
		{"noncanonical reference", func(request *wallet.ExternalSignerImportRequest) { request.Reference += "/" }},
		{"long name", func(request *wallet.ExternalSignerImportRequest) { request.Name = string(make([]byte, 65)) }},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			request := base
			test.mutate(&request)
			if _, err := wallet.ImportExternalSignerAccount(context.Background(), repository, request); err == nil {
				t.Fatal("invalid external signer import was accepted")
			}
		})
	}
}

var _ = common.Hash{}
