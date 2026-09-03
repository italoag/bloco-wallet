package safe

import (
	"context"
	"math/big"
	"testing"
	"time"

	"blocowallet/internal/wallet"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

func TestPrepareDeployDerivesDeterministicAddress(t *testing.T) {
	ownerKey, err := crypto.HexToECDSA("4646464646464646464646464646464646464646464646464646464646464646")
	if err != nil {
		t.Fatal(err)
	}
	owner := crypto.PubkeyToAddress(ownerKey.PublicKey)
	ownerAccount := &wallet.Account{
		AccountID: "21111111-1111-4111-8111-111111111111", Name: "Owner", Address: owner.Hex(),
		SignerKind: wallet.SignerKindSoftware, State: wallet.AccountStateActive,
		Capabilities: wallet.CapabilitySignMessage, AuthorizationEpoch: 1,
	}
	proposals := newFakeProposalRepo()
	accounts := &fakeAccountRepo{accounts: []*wallet.Account{ownerAccount}}
	importer := &fakeImporter{repo: accounts}
	messages := &fakeMessages{}
	rpc := &fakeRPC{
		code: []byte{1}, nonce: big.NewInt(0), pendingNonce: 4,
		broadcastHash: common.HexToHash("0xdeploy"),
	}
	gasPayer := &gasPayerTestSigner{key: ownerKey}
	service, err := NewWithGasPayer(proposals, accounts, importer, messages, &ownerDigestTestSigner{key: ownerKey}, gasPayer, rpc, Options{Now: func() time.Time { return time.Unix(1750000000, 0).UTC() }})
	if err != nil {
		t.Fatal(err)
	}

	deployment, err := service.PrepareDeploy(context.Background(), DeployRequest{
		ChainID: 1, Name: "Team Safe", Owners: []common.Address{owner}, Threshold: 1, SaltNonce: 7,
	})
	if err != nil {
		t.Fatal(err)
	}
	if deployment.ChainID != 1 || deployment.SafeAddress == (common.Address{}) || len(deployment.ProxyCall) == 0 {
		t.Fatalf("unexpected deployment: %+v", deployment)
	}
	expected := deriveSafeProxyAddress(deployment.Factory, deployment.Singleton, []byte{0x60, 0x80}, deployment.SetupData, big.NewInt(7))
	if deployment.SafeAddress != expected {
		t.Fatalf("derived address mismatch: %s != %s", deployment.SafeAddress, expected)
	}

	hash, err := service.BroadcastDeploy(context.Background(), deployment, ownerAccount.AccountID, func(accountID string, operation func(wallet.CapabilityHandle) error) error {
		return operation(wallet.CapabilityHandle{})
	})
	if err != nil {
		t.Fatal(err)
	}
	if hash != rpc.broadcastHash {
		t.Fatalf("deploy hash mismatch: %s", hash)
	}
	if len(rpc.broadcast) == 0 {
		t.Fatal("deployment payload was not broadcast")
	}
	var transaction types.Transaction
	if err := transaction.UnmarshalBinary(rpc.broadcast); err != nil {
		t.Fatal(err)
	}
	if transaction.To() == nil || *transaction.To() != deployment.Factory {
		t.Fatalf("deployment envelope targets %v, want factory %s", transaction.To(), deployment.Factory)
	}
	sender, err := types.Sender(types.NewEIP155Signer(big.NewInt(1)), &transaction)
	if err != nil {
		t.Fatal(err)
	}
	if sender != owner {
		t.Fatalf("deployment signed by %s, want %s", sender, owner)
	}
}
