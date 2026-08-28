package storage

import (
	"context"
	"sync"
	"testing"
	"time"

	"blocowallet/internal/evm"
	"blocowallet/internal/wallet"

	"github.com/ethereum/go-ethereum/common"
)

func testMessageApproval(account *wallet.Account, now time.Time) evm.MessageApproval {
	return evm.MessageApproval{
		ApprovalID: "51111111-1111-4111-8111-111111111111", AccountID: account.AccountID,
		Signer: common.HexToAddress(account.Address), Scheme: wallet.MessageSigningEIP191Personal,
		Digest: [32]byte{1}, IntentHash: [32]byte{2}, PayloadSize: 7, AuthorizationEpoch: account.AuthorizationEpoch,
		ConfirmationLevel: evm.ConfirmationReinforced, CreatedAt: now, ConfirmedAt: now,
		ExpiresAt: now.Add(time.Minute), State: evm.MessageApprovalPending, Revision: 1,
	}
}

func TestMessageSigningRepositoryConsumesApprovalOnceAndBlocksReplay(t *testing.T) {
	repository := newAccountTestRepository(t)
	account := testAccount("11111111-1111-4111-8111-111111111111", "message-approval")
	account.State = wallet.AccountStateActive
	if err := repository.CreateAccount(context.Background(), account); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	approval := testMessageApproval(account, now)
	if err := repository.IssueMessageApproval(context.Background(), approval); err != nil {
		t.Fatal(err)
	}
	binding := wallet.MessageApprovalBinding{
		AccountID: account.AccountID, Scheme: approval.Scheme, Digest: approval.Digest,
		IntentHash: approval.IntentHash, ApprovalID: approval.ApprovalID,
	}
	if err := repository.VerifyMessageApproval(context.Background(), binding); err == nil {
		t.Fatal("pending message approval was treated as consumable")
	}
	authorize := evm.AuthorizeMessageSigningRequest{
		SigningID: "71111111-1111-4111-8111-111111111111", ApprovalID: approval.ApprovalID,
		AccountID: account.AccountID, Signer: approval.Signer, Scheme: approval.Scheme,
		Digest: approval.Digest, IntentHash: approval.IntentHash, AuthorizationEpoch: approval.AuthorizationEpoch, AuthorizedAt: now,
	}
	record, err := repository.AuthorizeMessageSigning(context.Background(), authorize)
	if err != nil {
		t.Fatal(err)
	}
	if record.State != evm.MessageSigningInProgress || record.SigningID != authorize.SigningID {
		t.Fatalf("unexpected message signing record: %+v", record)
	}
	if err := repository.VerifyMessageApproval(context.Background(), binding); err != nil {
		t.Fatalf("consumed message approval was not bound to signing record: %v", err)
	}
	wrong := binding
	wrong.IntentHash[0] ^= 0xff
	if err := repository.VerifyMessageApproval(context.Background(), wrong); err == nil {
		t.Fatal("message verifier accepted a different intent hash")
	}
	if _, err := repository.AuthorizeMessageSigning(context.Background(), authorize); err == nil {
		t.Fatal("message approval was consumed twice")
	}
	signatureHash := common.HexToHash("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if err := repository.CompleteMessageSigning(context.Background(), evm.CompleteMessageSigningRequest{SigningID: record.SigningID, SignatureHash: signatureHash, CompletedAt: now.Add(time.Second)}); err != nil {
		t.Fatal(err)
	}
	if err := repository.VerifyMessageApproval(context.Background(), binding); err == nil {
		t.Fatal("completed message approval remained replayable")
	}
	stale := testMessageApproval(account, now)
	stale.ApprovalID = "52222222-2222-4222-8222-222222222222"
	stale.CreatedAt = now.Add(-time.Minute)
	stale.ConfirmedAt = now.Add(-time.Minute)
	stale.ExpiresAt = now.Add(-time.Second)
	if err := repository.IssueMessageApproval(context.Background(), stale); err != nil {
		t.Fatal(err)
	}
	if err := repository.db.Model(&messageApprovalRow{}).Where("approval_id = ?", stale.ApprovalID).Updates(map[string]any{"state": string(evm.MessageApprovalConsumed), "consumed_at_ms": now.UnixMilli(), "revision": 2}).Error; err != nil {
		t.Fatal(err)
	}
	staleBinding := binding
	staleBinding.ApprovalID = stale.ApprovalID
	if err := repository.VerifyMessageApproval(context.Background(), staleBinding); err == nil {
		t.Fatal("expired message approval remained verifiable")
	}
	var stored messageSigningRow
	if err := repository.db.Where("signing_id = ?", record.SigningID).First(&stored).Error; err != nil {
		t.Fatal(err)
	}
	if stored.State != string(evm.MessageSigningSigned) || common.BytesToHash(stored.SignatureHash) != signatureHash {
		t.Fatalf("message signing completion was not persisted: %+v", stored)
	}
	var signatureColumns int64
	if err := repository.db.Raw("SELECT COUNT(*) FROM pragma_table_info('message_signing_records') WHERE name IN ('signature', 'signed_payload', 'message')").Scan(&signatureColumns).Error; err != nil {
		t.Fatal(err)
	}
	if signatureColumns != 0 {
		t.Fatal("message signing schema stores raw signature or payload bytes")
	}
}

func TestMessageSigningRepositoryHasOneConcurrentApprovalConsumer(t *testing.T) {
	repository := newAccountTestRepository(t)
	account := testAccount("11111111-1111-4111-8111-111111111111", "message-concurrent")
	account.State = wallet.AccountStateActive
	if err := repository.CreateAccount(context.Background(), account); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	approval := testMessageApproval(account, now)
	if err := repository.IssueMessageApproval(context.Background(), approval); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	var wait sync.WaitGroup
	for index, signingID := range []string{"71111111-1111-4111-8111-111111111111", "72222222-2222-4222-8222-222222222222"} {
		index, signingID := index, signingID
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			_, err := repository.AuthorizeMessageSigning(context.Background(), evm.AuthorizeMessageSigningRequest{
				SigningID: signingID, ApprovalID: approval.ApprovalID, AccountID: account.AccountID,
				Signer: approval.Signer, Scheme: approval.Scheme, Digest: approval.Digest, IntentHash: approval.IntentHash,
				AuthorizationEpoch: approval.AuthorizationEpoch, AuthorizedAt: now.Add(time.Duration(index) * time.Nanosecond),
			})
			results <- err
		}()
	}
	close(start)
	wait.Wait()
	close(results)
	succeeded := 0
	for err := range results {
		if err == nil {
			succeeded++
		}
	}
	if succeeded != 1 {
		t.Fatalf("message approval had %d concurrent consumers", succeeded)
	}
}
