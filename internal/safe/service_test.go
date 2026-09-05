package safe

import (
	"context"
	"crypto/ecdsa"
	"math/big"
	"testing"
	"time"

	"blocowallet/internal/evm"
	"blocowallet/internal/wallet"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

type fakeProposalRepo struct {
	proposals map[string]*Proposal
}

func newFakeProposalRepo() *fakeProposalRepo {
	return &fakeProposalRepo{proposals: make(map[string]*Proposal)}
}

func (repo *fakeProposalRepo) CreateProposal(_ context.Context, proposal *Proposal) error {
	repo.proposals[proposal.ProposalID] = proposal
	return nil
}

func (repo *fakeProposalRepo) GetProposal(_ context.Context, proposalID string) (*Proposal, error) {
	proposal, exists := repo.proposals[proposalID]
	if !exists {
		return nil, ErrNotFound
	}
	return proposal, nil
}

func (repo *fakeProposalRepo) ListProposals(_ context.Context, accountID string, _ uint64, _ int) ([]*Proposal, error) {
	var proposals []*Proposal
	for _, proposal := range repo.proposals {
		if proposal.AccountID == accountID {
			proposals = append(proposals, proposal)
		}
	}
	return proposals, nil
}

func (repo *fakeProposalRepo) UpdateProposal(_ context.Context, proposal *Proposal) error {
	repo.proposals[proposal.ProposalID] = proposal
	return nil
}

type fakeAccountRepo struct {
	accounts []*wallet.Account
}

func (repo *fakeAccountRepo) GetAccount(_ context.Context, accountID string) (*wallet.Account, error) {
	for _, account := range repo.accounts {
		if account.AccountID == accountID {
			return account, nil
		}
	}
	return nil, ErrNotFound
}

func (repo *fakeAccountRepo) FindAccountsByAddress(_ context.Context, address string) ([]wallet.Account, error) {
	var matches []wallet.Account
	for _, account := range repo.accounts {
		if account.Address == address {
			matches = append(matches, *account)
		}
	}
	return matches, nil
}

type fakeImporter struct {
	created []*wallet.Account
	repo    *fakeAccountRepo
}

func (importer *fakeImporter) ImportSafeAccount(_ context.Context, request wallet.ExternalSignerImportRequest) (*wallet.Account, error) {
	account := &wallet.Account{
		AccountID: "11111111-1111-4111-8111-111111111111", Name: request.Name, Address: request.Address,
		SignerKind: wallet.SignerKindMultisig, SignerReference: request.Reference,
		Capabilities: request.Capabilities, State: wallet.AccountStateActive, AuthorizationEpoch: 1,
	}
	importer.created = append(importer.created, account)
	importer.repo.accounts = append(importer.repo.accounts, account)
	return account, nil
}

type fakeMessages struct {
	approvals []evm.MessageApproval
	complete  bool
}

func (messages *fakeMessages) IssueMessageApproval(_ context.Context, approval evm.MessageApproval) error {
	messages.approvals = append(messages.approvals, approval)
	return nil
}

func (messages *fakeMessages) AuthorizeMessageSigning(_ context.Context, request evm.AuthorizeMessageSigningRequest) (evm.MessageSigningRecord, error) {
	return evm.MessageSigningRecord{
		SigningID: request.SigningID, ApprovalID: request.ApprovalID, AccountID: request.AccountID,
		Signer: request.Signer, Scheme: request.Scheme, ChainID: request.ChainID,
		Digest: request.Digest, IntentHash: request.IntentHash, State: evm.MessageSigningInProgress,
	}, nil
}

func (messages *fakeMessages) CompleteMessageSigning(_ context.Context, _ evm.CompleteMessageSigningRequest) error {
	messages.complete = true
	return nil
}

func (messages *fakeMessages) FailMessageSigning(_ context.Context, _ evm.FailMessageSigningRequest) error {
	return nil
}

type fakeRPC struct {
	owners        []common.Address
	threshold     uint64
	nonce         *big.Int
	code          []byte
	pendingNonce  uint64
	broadcast     []byte
	broadcastHash common.Hash
}

func (rpc *fakeRPC) Owners(context.Context, common.Address) ([]common.Address, error) {
	return rpc.owners, nil
}

func (rpc *fakeRPC) Threshold(context.Context, common.Address) (uint64, error) {
	return rpc.threshold, nil
}

func (rpc *fakeRPC) Nonce(context.Context, common.Address) (*big.Int, error) {
	return new(big.Int).Set(rpc.nonce), nil
}

func (rpc *fakeRPC) CodeAt(context.Context, common.Address) ([]byte, error) {
	return rpc.code, nil
}

func (rpc *fakeRPC) ProxyCreationCode(context.Context, common.Address) ([]byte, error) {
	return []byte{0x60, 0x80}, nil
}

func (rpc *fakeRPC) PendingNonce(context.Context, common.Address) (uint64, error) {
	return rpc.pendingNonce, nil
}

func (rpc *fakeRPC) SuggestGasPrice(context.Context) (*big.Int, error) {
	return big.NewInt(1), nil
}

func (rpc *fakeRPC) Broadcast(_ context.Context, raw []byte) (common.Hash, error) {
	rpc.broadcast = append([]byte(nil), raw...)
	return rpc.broadcastHash, nil
}

func (rpc *fakeRPC) TransactionStatus(_ context.Context, _ common.Hash) (uint64, bool, error) {
	return 1, true, nil
}

func TestSafeServiceFullLifecycle(t *testing.T) {
	ownerKey, err := crypto.HexToECDSA("4646464646464646464646464646464646464646464646464646464646464646")
	if err != nil {
		t.Fatal(err)
	}
	owner := crypto.PubkeyToAddress(ownerKey.PublicKey)
	safeAddress := common.HexToAddress("0x3C44CdDdB6a900fa2b585dd299e03d12FA4293BC")
	recipient := common.HexToAddress("0x2222222222222222222222222222222222222222")
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
		owners: []common.Address{owner}, threshold: 1, nonce: big.NewInt(3), code: []byte{1},
		broadcastHash: common.HexToHash("0xabcdef"),
	}
	signer := &ownerDigestTestSigner{}
	gasPayer := &gasPayerTestSigner{key: ownerKey}

	service, err := NewWithGasPayer(proposals, accounts, importer, messages, signer, gasPayer, rpc, Options{Now: func() time.Time { return time.Unix(1750000000, 0).UTC() }})
	if err != nil {
		t.Fatal(err)
	}

	imported, err := service.ImportSafe(context.Background(), SafeImportRequest{Name: "My Safe", Address: safeAddress.Hex(), ChainID: 31337})
	if err != nil {
		t.Fatal(err)
	}
	if imported.Threshold != 1 || len(imported.Owners) != 1 || imported.Address != safeAddress {
		t.Fatalf("unexpected import: %+v", imported)
	}
	if len(importer.created) != 1 || importer.created[0].Capabilities&wallet.CapabilitySignTransaction == 0 {
		t.Fatalf("safe account was not imported with capabilities: %+v", importer.created)
	}

	proposal, err := service.Propose(context.Background(), SafeProposalRequest{
		SafeAccountID: imported.AccountID, ChainID: 31337, To: recipient, Value: big.NewInt(12345),
	})
	if err != nil {
		t.Fatal(err)
	}
	if proposal.Nonce.Cmp(big.NewInt(3)) != 0 || proposal.Threshold != 1 || proposal.Digest == ([32]byte{}) {
		t.Fatalf("unexpected proposal: %+v", proposal)
	}
	if len(proposal.Owners) != 1 || proposal.Owners[0].Address != owner {
		t.Fatal("proposal owner snapshot mismatch")
	}

	updated, err := service.Sign(context.Background(), SafeSignRequest{
		ProposalID: proposal.ProposalID, OwnerAccountID: ownerAccount.AccountID, ChainID: 31337,
	}, func(accountID string, operation func(wallet.CapabilityHandle) error) error {
		if accountID != ownerAccount.AccountID {
			t.Fatalf("authorized wrong account %s", accountID)
		}
		return operation(wallet.CapabilityHandle{})
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.Signatures) != 1 || !messages.complete || len(messages.approvals) != 1 {
		t.Fatalf("owner signature was not recorded: %+v", updated)
	}
	if messages.approvals[0].Scheme != wallet.MessageSigningSafeOwner || messages.approvals[0].Digest != proposal.Digest || messages.approvals[0].IntentHash != proposal.Commitment {
		t.Fatalf("safe-owner approval binding mismatch: %+v", messages.approvals[0])
	}

	hash, err := service.Execute(context.Background(), proposal.ProposalID, ownerAccount.AccountID, func(accountID string, operation func(wallet.CapabilityHandle) error) error {
		if accountID != ownerAccount.AccountID {
			t.Fatalf("authorized wrong gas payer %s", accountID)
		}
		return operation(wallet.CapabilityHandle{})
	})
	if err != nil {
		t.Fatal(err)
	}
	if hash != rpc.broadcastHash {
		t.Fatalf("execute hash mismatch: %s", hash)
	}
	if len(rpc.broadcast) == 0 {
		t.Fatal("execTransaction payload was not broadcast")
	}
	stored, err := service.GetProposal(context.Background(), proposal.ProposalID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != ProposalExecuted || stored.TxHash != rpc.broadcastHash {
		t.Fatalf("proposal was not marked executed: %+v", stored)
	}
}

type gasPayerTestSigner struct {
	key *ecdsa.PrivateKey
}

func (signer *gasPayerTestSigner) SignGasPayer(_ context.Context, _ wallet.CapabilityHandle, request GasPayerSignRequest) ([]byte, error) {
	transaction := types.NewTx(&types.LegacyTx{
		Nonce: request.Nonce, GasPrice: request.GasPrice, Gas: request.GasLimit,
		To: &request.To, Value: big.NewInt(0), Data: request.Data,
	})
	transactionSigner := types.NewEIP155Signer(new(big.Int).SetUint64(request.ChainID))
	signed, err := types.SignTx(transaction, transactionSigner, signer.key)
	if err != nil {
		return nil, err
	}
	return signed.MarshalBinary()
}

type ownerDigestTestSigner struct {
	key *ecdsa.PrivateKey
}

func (signer *ownerDigestTestSigner) SignSafeOwnerDigest(_ context.Context, _ wallet.CapabilityHandle, request evm.SafeOwnerDigestRequest) (wallet.SoftwareSigningResult, error) {
	key := signer.key
	if key == nil {
		var err error
		key, err = crypto.HexToECDSA("4646464646464646464646464646464646464646464646464646464646464646")
		if err != nil {
			return wallet.SoftwareSigningResult{}, err
		}
	}
	signature, err := crypto.Sign(request.Digest[:], key)
	if err != nil {
		return wallet.SoftwareSigningResult{}, err
	}
	return wallet.SoftwareSigningResult{
		AccountID: request.AccountID, Purpose: wallet.SigningPurposeMessage,
		MessageScheme: wallet.MessageSigningSafeOwner, ChainID: request.ChainID,
		Digest: request.Digest, IntentHash: request.IntentHash, Signature: signature,
	}, nil
}
