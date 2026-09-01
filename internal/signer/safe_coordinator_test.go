package signer

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"errors"
	"math/big"
	"os"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

func TestSafeCoordinatorThresholdOrderingAndDecodableCalldata(t *testing.T) {
	keys := []*ecdsa.PrivateKey{
		safeCoordinatorTestKey(t, 1),
		safeCoordinatorTestKey(t, 2),
		safeCoordinatorTestKey(t, 3),
	}
	owners := safeCoordinatorEOAOwners(keys)
	transaction := safeCoordinatorTestTransaction(7)
	intent, err := NewSafeTransactionIntent(
		common.HexToAddress("0x9000000000000000000000000000000000000009"),
		31337,
		owners,
		2,
		transaction,
	)
	if err != nil {
		t.Fatal(err)
	}
	coordinator, err := NewSafeCoordinator(intent)
	if err != nil {
		t.Fatal(err)
	}

	if err := coordinator.AddEOASignature(owners[2].Address, intent.Digest, intent.Commitment, safeCoordinatorSign(t, keys[2], intent.Digest)); err != nil {
		t.Fatal(err)
	}
	if coordinator.Ready() {
		t.Fatal("2-of-3 coordinator became ready after one signature")
	}
	if calldata, err := coordinator.ExecTransaction(); !errors.Is(err, ErrSafeInsufficientSignatures) || calldata != nil {
		t.Fatalf("insufficient coordinator produced calldata: %x, %v", calldata, err)
	}
	if err := coordinator.AddEOASignature(owners[0].Address, intent.Digest, intent.Commitment, safeCoordinatorSign(t, keys[0], intent.Digest)); err != nil {
		t.Fatal(err)
	}
	if !coordinator.Ready() || coordinator.SignatureCount() != 2 {
		t.Fatalf("unexpected threshold state: ready=%t count=%d", coordinator.Ready(), coordinator.SignatureCount())
	}

	aggregate, err := coordinator.Signatures()
	if err != nil {
		t.Fatal(err)
	}
	if len(aggregate) != 2*safeStaticSignatureLength {
		t.Fatalf("unexpected EOA aggregate length: %d", len(aggregate))
	}
	expectedOwners := []common.Address{owners[2].Address, owners[0].Address}
	sort.Slice(expectedOwners, func(i, j int) bool {
		return bytes.Compare(expectedOwners[i][:], expectedOwners[j][:]) < 0
	})
	for index, expected := range expectedOwners {
		part := aggregate[index*safeStaticSignatureLength : (index+1)*safeStaticSignatureLength]
		if part[64] != 27 && part[64] != 28 {
			t.Fatalf("signature %d did not use Safe v encoding: %d", index, part[64])
		}
		if recovered := safeCoordinatorRecoverOwner(t, intent.Digest, part); recovered != expected {
			t.Fatalf("signature %d owner = %s, want %s", index, recovered, expected)
		}
	}

	calldata, err := coordinator.ExecTransaction()
	if err != nil {
		t.Fatal(err)
	}
	decodedSignatures := safeCoordinatorAssertExecCalldata(t, calldata, transaction)
	if !bytes.Equal(decodedSignatures, aggregate) {
		t.Fatal("execTransaction calldata changed the ordered aggregate")
	}

	// Caller-owned snapshots and values cannot mutate the coordinator after
	// construction.
	intent.Nonce.SetUint64(999)
	intent.Transaction.Nonce.SetUint64(999)
	intent.Transaction.Value.SetUint64(999)
	intent.Transaction.Data[0] ^= 0xff
	intent.Owners[0] = SafeOwnerSnapshot{Address: common.HexToAddress("0xdead"), Kind: SafeOwnerContract}
	calldataAfterMutation, err := coordinator.ExecTransaction()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(calldataAfterMutation, calldata) {
		t.Fatal("caller mutation changed frozen coordinator calldata")
	}
}

func TestSafeCoordinatorContractOffsetsAndNestedPayloadLayout(t *testing.T) {
	safeAddress := common.HexToAddress("0x9000000000000000000000000000000000000009")
	eoaKey := safeCoordinatorTestKey(t, 11)
	eoa := crypto.PubkeyToAddress(eoaKey.PublicKey)
	contractA := common.HexToAddress("0x1000000000000000000000000000000000000001")
	contractB := common.HexToAddress("0xf00000000000000000000000000000000000000f")
	owners := []SafeOwnerSnapshot{
		{Address: contractB, Kind: SafeOwnerContract},
		{Address: eoa, Kind: SafeOwnerEOA},
		{Address: contractA, Kind: SafeOwnerContract},
	}
	intent, err := NewSafeTransactionIntent(safeAddress, 1, owners, 3, safeCoordinatorTestTransaction(12))
	if err != nil {
		t.Fatal(err)
	}
	coordinator, err := NewSafeCoordinator(intent)
	if err != nil {
		t.Fatal(err)
	}

	nestedOwner := common.HexToAddress("0x2222222222222222222222222222222222222222")
	nestedLeafPayload := bytes.Repeat([]byte{0xab}, 65)
	nestedPayload, err := ComposeSafeContractSignature(nestedOwner, nestedLeafPayload)
	if err != nil {
		t.Fatal(err)
	}
	contractPayloads := map[common.Address][]byte{
		contractA: nestedPayload,
		contractB: bytes.Repeat([]byte{0xcd}, 33),
	}

	// Submit in the opposite of canonical address order. Also prove that an
	// already-Safe-encoded nested signature remains opaque payload data.
	if err := coordinator.AddContractSignature(contractB, intent.Digest, intent.Commitment, contractPayloads[contractB]); err != nil {
		t.Fatal(err)
	}
	eoaSignature := safeCoordinatorSign(t, eoaKey, intent.Digest)
	eoaSignature[64] += 27 // The coordinator accepts either 0/1 or 27/28.
	if err := coordinator.AddEOASignature(eoa, intent.Digest, intent.Commitment, eoaSignature); err != nil {
		t.Fatal(err)
	}
	if err := coordinator.AddContractSignature(contractA, intent.Digest, intent.Commitment, contractPayloads[contractA]); err != nil {
		t.Fatal(err)
	}

	aggregate, err := coordinator.Signatures()
	if err != nil {
		t.Fatal(err)
	}
	sortedOwners := append([]SafeOwnerSnapshot(nil), owners...)
	sort.Slice(sortedOwners, func(i, j int) bool {
		return bytes.Compare(sortedOwners[i].Address[:], sortedOwners[j].Address[:]) < 0
	})
	staticLength := len(sortedOwners) * safeStaticSignatureLength
	expectedDynamicOffset := staticLength
	for index, owner := range sortedOwners {
		part := aggregate[index*safeStaticSignatureLength : (index+1)*safeStaticSignatureLength]
		switch owner.Kind {
		case SafeOwnerEOA:
			if recovered := safeCoordinatorRecoverOwner(t, intent.Digest, part); recovered != owner.Address {
				t.Fatalf("EOA static owner = %s, want %s", recovered, owner.Address)
			}
		case SafeOwnerContract:
			if part[64] != 0 || common.BytesToAddress(part[12:32]) != owner.Address {
				t.Fatalf("malformed contract static part for %s: %x", owner.Address, part)
			}
			offset := new(big.Int).SetBytes(part[32:64]).Uint64()
			if offset != uint64(expectedDynamicOffset) {
				t.Fatalf("contract %s offset = %d, want %d", owner.Address, offset, expectedDynamicOffset)
			}
			payload, paddedLength := safeCoordinatorContractPayloadAt(t, aggregate, expectedDynamicOffset)
			if !bytes.Equal(payload, contractPayloads[owner.Address]) {
				t.Fatalf("contract %s payload changed", owner.Address)
			}
			expectedDynamicOffset += paddedLength
		}
	}
	if expectedDynamicOffset != len(aggregate) {
		t.Fatalf("dynamic layout ended at %d, aggregate length %d", expectedDynamicOffset, len(aggregate))
	}

	// Decode the nested payload independently: its offset remains relative to
	// the start of the nested bytes, not to the outer aggregate.
	if len(nestedPayload) < safeStaticSignatureLength || nestedPayload[64] != 0 {
		t.Fatalf("malformed nested static part: %x", nestedPayload)
	}
	if common.BytesToAddress(nestedPayload[12:32]) != nestedOwner {
		t.Fatal("nested contract owner changed")
	}
	if offset := new(big.Int).SetBytes(nestedPayload[32:64]).Uint64(); offset != safeStaticSignatureLength {
		t.Fatalf("nested contract offset = %d, want %d", offset, safeStaticSignatureLength)
	}
	leaf, paddedLength := safeCoordinatorContractPayloadAt(t, nestedPayload, safeStaticSignatureLength)
	if !bytes.Equal(leaf, nestedLeafPayload) || safeStaticSignatureLength+paddedLength != len(nestedPayload) {
		t.Fatal("nested EIP-1271 payload layout changed")
	}
}

func TestSafeTransactionIntentValidatesOwnerSnapshotAndTampering(t *testing.T) {
	safeAddress := common.HexToAddress("0x9000000000000000000000000000000000000009")
	keys := []*ecdsa.PrivateKey{safeCoordinatorTestKey(t, 21), safeCoordinatorTestKey(t, 22), safeCoordinatorTestKey(t, 23)}
	owners := safeCoordinatorEOAOwners(keys)
	transaction := safeCoordinatorTestTransaction(3)

	invalid := []struct {
		name      string
		safe      common.Address
		chainID   uint64
		owners    []SafeOwnerSnapshot
		threshold uint64
		tx        SafeTransaction
	}{
		{name: "zero safe", chainID: 1, owners: owners, threshold: 2, tx: transaction},
		{name: "zero chain", safe: safeAddress, owners: owners, threshold: 2, tx: transaction},
		{name: "no owners", safe: safeAddress, chainID: 1, threshold: 1, tx: transaction},
		{name: "zero owner", safe: safeAddress, chainID: 1, owners: []SafeOwnerSnapshot{{Kind: SafeOwnerEOA}}, threshold: 1, tx: transaction},
		{name: "self EOA owner", safe: safeAddress, chainID: 1, owners: []SafeOwnerSnapshot{{Address: safeAddress, Kind: SafeOwnerEOA}}, threshold: 1, tx: transaction},
		{name: "self contract owner", safe: safeAddress, chainID: 1, owners: []SafeOwnerSnapshot{{Address: safeAddress, Kind: SafeOwnerContract}}, threshold: 1, tx: transaction},
		{name: "unknown owner kind", safe: safeAddress, chainID: 1, owners: []SafeOwnerSnapshot{{Address: owners[0].Address, Kind: 99}}, threshold: 1, tx: transaction},
		{name: "duplicate owner", safe: safeAddress, chainID: 1, owners: []SafeOwnerSnapshot{owners[0], owners[0]}, threshold: 1, tx: transaction},
		{name: "zero threshold", safe: safeAddress, chainID: 1, owners: owners, tx: transaction},
		{name: "threshold above owners", safe: safeAddress, chainID: 1, owners: owners, threshold: 4, tx: transaction},
	}
	for _, test := range invalid {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewSafeTransactionIntent(test.safe, test.chainID, test.owners, test.threshold, test.tx); !errors.Is(err, ErrSafeInvalidIntent) {
				t.Fatalf("invalid snapshot error = %v", err)
			}
		})
	}

	inputOwners := append([]SafeOwnerSnapshot(nil), owners...)
	inputTransaction := cloneSafeTransaction(transaction)
	intent, err := NewSafeTransactionIntent(safeAddress, 1, inputOwners, 2, inputTransaction)
	if err != nil {
		t.Fatal(err)
	}
	inputOwners[0] = SafeOwnerSnapshot{}
	inputTransaction.Data[0] ^= 0xff
	inputTransaction.Value.SetUint64(999)
	inputTransaction.Nonce.SetUint64(999)
	if err := intent.Validate(); err != nil {
		t.Fatalf("constructor retained caller aliases: %v", err)
	}

	reorderedOwners := []SafeOwnerSnapshot{owners[2], owners[0], owners[1]}
	reordered, err := NewSafeTransactionIntent(safeAddress, 1, reorderedOwners, 2, transaction)
	if err != nil {
		t.Fatal(err)
	}
	if reordered.Commitment != intent.Commitment {
		t.Fatal("owner set commitment depended on input ordering")
	}
	otherThreshold, err := NewSafeTransactionIntent(safeAddress, 1, owners, 1, transaction)
	if err != nil {
		t.Fatal(err)
	}
	if otherThreshold.Digest != intent.Digest || otherThreshold.Commitment == intent.Commitment {
		t.Fatal("threshold was not independently bound by the commitment")
	}
	contractOwners := append([]SafeOwnerSnapshot(nil), owners...)
	contractOwners[0].Kind = SafeOwnerContract
	otherKind, err := NewSafeTransactionIntent(safeAddress, 1, contractOwners, 2, transaction)
	if err != nil {
		t.Fatal(err)
	}
	if otherKind.Digest != intent.Digest || otherKind.Commitment == intent.Commitment {
		t.Fatal("owner kind was not bound by the commitment")
	}

	coordinator, err := NewSafeCoordinator(intent)
	if err != nil {
		t.Fatal(err)
	}
	tamperedCases := []struct {
		name   string
		mutate func(*SafeTransactionIntent)
	}{
		{name: "digest", mutate: func(value *SafeTransactionIntent) { value.Digest[0] ^= 1 }},
		{name: "commitment", mutate: func(value *SafeTransactionIntent) { value.Commitment[0] ^= 1 }},
		{name: "nonce snapshot", mutate: func(value *SafeTransactionIntent) { value.Nonce.Add(value.Nonce, big.NewInt(1)) }},
		{name: "transaction nonce", mutate: func(value *SafeTransactionIntent) {
			value.Transaction.Nonce.Add(value.Transaction.Nonce, big.NewInt(1))
		}},
		{name: "transaction data", mutate: func(value *SafeTransactionIntent) { value.Transaction.Data[0] ^= 1 }},
		{name: "owner address", mutate: func(value *SafeTransactionIntent) { value.Owners[0].Address[0] ^= 1 }},
		{name: "owner kind", mutate: func(value *SafeTransactionIntent) { value.Owners[0].Kind = SafeOwnerContract }},
		{name: "threshold", mutate: func(value *SafeTransactionIntent) { value.Threshold = 1 }},
	}
	for _, test := range tamperedCases {
		t.Run("tampered "+test.name, func(t *testing.T) {
			tampered := coordinator.Intent()
			test.mutate(&tampered)
			if _, err := NewSafeCoordinator(tampered); !errors.Is(err, ErrSafeInvalidIntent) {
				t.Fatalf("tampered intent error = %v", err)
			}
		})
	}
}

func TestSafeCoordinatorRejectsReplayUnknownDuplicateAndMalleableSignatures(t *testing.T) {
	safeAddress := common.HexToAddress("0x9000000000000000000000000000000000000009")
	ownerKey := safeCoordinatorTestKey(t, 31)
	secondKey := safeCoordinatorTestKey(t, 32)
	foreignKey := safeCoordinatorTestKey(t, 33)
	contractOwner := common.HexToAddress("0x4444444444444444444444444444444444444444")
	owners := []SafeOwnerSnapshot{
		{Address: crypto.PubkeyToAddress(ownerKey.PublicKey), Kind: SafeOwnerEOA},
		{Address: crypto.PubkeyToAddress(secondKey.PublicKey), Kind: SafeOwnerEOA},
		{Address: contractOwner, Kind: SafeOwnerContract},
	}
	intent, err := NewSafeTransactionIntent(safeAddress, 1, owners, 2, safeCoordinatorTestTransaction(9))
	if err != nil {
		t.Fatal(err)
	}
	coordinator, err := NewSafeCoordinator(intent)
	if err != nil {
		t.Fatal(err)
	}
	ownerSignature := safeCoordinatorSign(t, ownerKey, intent.Digest)

	foreignOwner := crypto.PubkeyToAddress(foreignKey.PublicKey)
	if err := coordinator.AddEOASignature(foreignOwner, intent.Digest, intent.Commitment, safeCoordinatorSign(t, foreignKey, intent.Digest)); !errors.Is(err, ErrSafeUnknownOwner) {
		t.Fatalf("unknown EOA error = %v", err)
	}
	if err := coordinator.AddContractSignature(foreignOwner, intent.Digest, intent.Commitment, []byte{1}); !errors.Is(err, ErrSafeUnknownOwner) {
		t.Fatalf("unknown contract error = %v", err)
	}
	if err := coordinator.AddEOASignature(contractOwner, intent.Digest, intent.Commitment, ownerSignature); !errors.Is(err, ErrSafeInvalidSignature) {
		t.Fatalf("contract owner accepted as EOA: %v", err)
	}
	if err := coordinator.AddContractSignature(owners[0].Address, intent.Digest, intent.Commitment, []byte{1}); !errors.Is(err, ErrSafeInvalidSignature) {
		t.Fatalf("EOA owner accepted as contract: %v", err)
	}

	wrongCommitment := intent.Commitment
	wrongCommitment[0] ^= 1
	if err := coordinator.AddEOASignature(owners[0].Address, intent.Digest, wrongCommitment, ownerSignature); !errors.Is(err, ErrSafeSignatureBinding) {
		t.Fatalf("wrong commitment error = %v", err)
	}
	oldIntent, err := NewSafeTransactionIntent(safeAddress, 1, owners, 2, safeCoordinatorTestTransaction(8))
	if err != nil {
		t.Fatal(err)
	}
	oldSignature := safeCoordinatorSign(t, ownerKey, oldIntent.Digest)
	if err := coordinator.AddEOASignature(owners[0].Address, oldIntent.Digest, intent.Commitment, oldSignature); !errors.Is(err, ErrSafeSignatureBinding) {
		t.Fatalf("replayed digest error = %v", err)
	}
	if err := coordinator.AddContractSignature(contractOwner, oldIntent.Digest, intent.Commitment, []byte{1, 2, 3}); !errors.Is(err, ErrSafeSignatureBinding) {
		t.Fatalf("replayed contract digest error = %v", err)
	}
	if err := coordinator.AddEOASignature(owners[0].Address, intent.Digest, intent.Commitment, oldSignature); !errors.Is(err, ErrSafeInvalidSignature) {
		t.Fatalf("signature over another digest error = %v", err)
	}

	tampered := append([]byte(nil), ownerSignature...)
	tampered[0] ^= 1
	if err := coordinator.AddEOASignature(owners[0].Address, intent.Digest, intent.Commitment, tampered); !errors.Is(err, ErrSafeInvalidSignature) {
		t.Fatalf("tampered EOA error = %v", err)
	}
	badRecoveryID := append([]byte(nil), ownerSignature...)
	badRecoveryID[64] = 29
	if err := coordinator.AddEOASignature(owners[0].Address, intent.Digest, intent.Commitment, badRecoveryID); !errors.Is(err, ErrSafeInvalidSignature) {
		t.Fatalf("bad recovery id error = %v", err)
	}
	highS := append([]byte(nil), ownerSignature...)
	sValue := new(big.Int).SetBytes(highS[32:64])
	new(big.Int).Sub(crypto.S256().Params().N, sValue).FillBytes(highS[32:64])
	highS[64] ^= 1
	if err := coordinator.AddEOASignature(owners[0].Address, intent.Digest, intent.Commitment, highS); !errors.Is(err, ErrSafeInvalidSignature) {
		t.Fatalf("high-S signature error = %v", err)
	}

	if err := coordinator.AddEOASignature(owners[0].Address, intent.Digest, intent.Commitment, ownerSignature); err != nil {
		t.Fatal(err)
	}
	if err := coordinator.AddEOASignature(owners[0].Address, intent.Digest, intent.Commitment, ownerSignature); !errors.Is(err, ErrSafeDuplicateSignature) {
		t.Fatalf("duplicate EOA error = %v", err)
	}
	if err := coordinator.AddContractSignature(contractOwner, intent.Digest, intent.Commitment, []byte{1, 2, 3}); err != nil {
		t.Fatal(err)
	}
	if err := coordinator.AddContractSignature(contractOwner, intent.Digest, intent.Commitment, []byte{4, 5, 6}); !errors.Is(err, ErrSafeDuplicateSignature) {
		t.Fatalf("duplicate contract error = %v", err)
	}

	oversizedCoordinator, err := NewSafeCoordinator(intent)
	if err != nil {
		t.Fatal(err)
	}
	if err := oversizedCoordinator.AddContractSignature(contractOwner, intent.Digest, intent.Commitment, bytes.Repeat([]byte{1}, (4<<10)+1)); !errors.Is(err, ErrSafeInvalidSignature) {
		t.Fatalf("oversized contract payload error = %v", err)
	}
}

func TestSafeCoordinatorNeverProducesBelowThreshold(t *testing.T) {
	keys := []*ecdsa.PrivateKey{safeCoordinatorTestKey(t, 41), safeCoordinatorTestKey(t, 42), safeCoordinatorTestKey(t, 43)}
	owners := safeCoordinatorEOAOwners(keys)
	intent, err := NewSafeTransactionIntent(
		common.HexToAddress("0x9000000000000000000000000000000000000009"),
		1,
		owners,
		2,
		safeCoordinatorTestTransaction(0),
	)
	if err != nil {
		t.Fatal(err)
	}
	coordinator, err := NewSafeCoordinator(intent)
	if err != nil {
		t.Fatal(err)
	}
	for count := 0; count < 2; count++ {
		if signatures, err := coordinator.Signatures(); !errors.Is(err, ErrSafeInsufficientSignatures) || signatures != nil {
			t.Fatalf("count %d produced signatures: %x, %v", count, signatures, err)
		}
		if calldata, err := coordinator.ExecTransaction(); !errors.Is(err, ErrSafeInsufficientSignatures) || calldata != nil {
			t.Fatalf("count %d produced calldata: %x, %v", count, calldata, err)
		}
		if count == 0 {
			if err := coordinator.AddEOASignature(owners[0].Address, intent.Digest, intent.Commitment, safeCoordinatorSign(t, keys[0], intent.Digest)); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestSafeCoordinatorConcurrentDistinctOwners(t *testing.T) {
	keys := []*ecdsa.PrivateKey{safeCoordinatorTestKey(t, 51), safeCoordinatorTestKey(t, 52), safeCoordinatorTestKey(t, 53)}
	owners := safeCoordinatorEOAOwners(keys)
	intent, err := NewSafeTransactionIntent(
		common.HexToAddress("0x9000000000000000000000000000000000000009"),
		1,
		owners,
		2,
		safeCoordinatorTestTransaction(1),
	)
	if err != nil {
		t.Fatal(err)
	}
	coordinator, err := NewSafeCoordinator(intent)
	if err != nil {
		t.Fatal(err)
	}

	var wait sync.WaitGroup
	errorsByOwner := make(chan error, len(owners))
	for index := range owners {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			errorsByOwner <- coordinator.AddEOASignature(
				owners[index].Address,
				intent.Digest,
				intent.Commitment,
				safeCoordinatorSign(t, keys[index], intent.Digest),
			)
		}(index)
	}
	wait.Wait()
	close(errorsByOwner)
	for err := range errorsByOwner {
		if err != nil {
			t.Fatal(err)
		}
	}
	if coordinator.SignatureCount() != 3 || !coordinator.Ready() {
		t.Fatalf("concurrent collection state: count=%d ready=%t", coordinator.SignatureCount(), coordinator.Ready())
	}
	if _, err := coordinator.ExecTransaction(); err != nil {
		t.Fatal(err)
	}
}

// TestSafeCoordinatorAgainstRealSafe reuses the Anvil client and official Safe
// artifacts from safe_integration_test.go. It is skipped under the same
// BLOCO_WALLET_ANVIL_URL gate and needs no changes to the existing test.
func TestSafeCoordinatorAgainstRealSafe(t *testing.T) {
	anvilURL := os.Getenv("BLOCO_WALLET_ANVIL_URL")
	if anvilURL == "" {
		t.Skip("BLOCO_WALLET_ANVIL_URL not set; skipping Safe coordinator integration")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	rpc := newAnvilRPC(anvilURL)
	if _, err := rpc.blockNumber(ctx); err != nil {
		t.Fatalf("anvil unreachable: %v", err)
	}
	accountsResult, err := rpc.call(ctx, "eth_accounts")
	if err != nil {
		t.Fatal(err)
	}
	var accounts []string
	if err := json.Unmarshal(accountsResult, &accounts); err != nil || len(accounts) < 2 {
		t.Fatalf("anvil needs two unlocked accounts: %v", err)
	}
	deployer := common.HexToAddress(accounts[0])
	recipient := common.HexToAddress(accounts[1])

	singletonTx, err := rpc.sendTransaction(ctx, deployer, nil, common.FromHex(safeBytecode))
	if err != nil {
		t.Fatal(err)
	}
	singleton, err := rpc.waitReceipt(ctx, singletonTx)
	if err != nil {
		t.Fatal(err)
	}
	factoryTx, err := rpc.sendTransaction(ctx, deployer, nil, common.FromHex(safeProxyFactoryBytecode))
	if err != nil {
		t.Fatal(err)
	}
	factory, err := rpc.waitReceipt(ctx, factoryTx)
	if err != nil {
		t.Fatal(err)
	}

	keys := []*ecdsa.PrivateKey{safeCoordinatorTestKey(t, 61), safeCoordinatorTestKey(t, 62), safeCoordinatorTestKey(t, 63)}
	ownerAddresses := make([]common.Address, len(keys))
	for index, key := range keys {
		ownerAddresses[index] = crypto.PubkeyToAddress(key.PublicKey)
	}
	setupData, err := safeCoordinatorEncodeSetup(ownerAddresses, 2)
	if err != nil {
		t.Fatal(err)
	}
	const saltNonce = uint64(0x5afe2)
	proxyTx, err := rpc.sendTransaction(ctx, deployer, &factory, encodeCreateProxyWithNonce(singleton, setupData, saltNonce))
	if err != nil {
		t.Fatal(err)
	}
	creationCodeResult, err := rpc.callContract(ctx, factory, selectorOf("proxyCreationCode()"))
	if err != nil {
		t.Fatal(err)
	}
	creationCode, err := decodeBytesResult(creationCodeResult)
	if err != nil {
		t.Fatal(err)
	}
	safeAddress := safeProxyAddress(factory, singleton, creationCode, setupData, new(big.Int).SetUint64(saltNonce))
	if _, err := rpc.waitReceipt(ctx, proxyTx); err != nil {
		t.Fatal(err)
	}

	funding := big.NewInt(1_000_000)
	fundTx, err := rpc.sendTransactionWithValue(ctx, deployer, safeAddress, funding, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rpc.waitReceipt(ctx, fundTx); err != nil {
		t.Fatal(err)
	}
	balanceBefore, err := rpc.balance(ctx, recipient)
	if err != nil {
		t.Fatal(err)
	}
	nonceBytes, err := rpc.callContract(ctx, safeAddress, selectorOf("nonce()"))
	if err != nil {
		t.Fatal(err)
	}
	if len(nonceBytes) != 32 {
		t.Fatalf("unexpected Safe nonce: %x", nonceBytes)
	}
	transaction := SafeTransaction{
		To: recipient, Value: big.NewInt(1234), Operation: 0,
		SafeTxGas: big.NewInt(0), BaseGas: big.NewInt(0), GasPrice: big.NewInt(0),
		Nonce: new(big.Int).SetBytes(nonceBytes),
	}
	owners := safeCoordinatorEOAOwners(keys)
	intent, err := NewSafeTransactionIntent(safeAddress, 31337, owners, 2, transaction)
	if err != nil {
		t.Fatal(err)
	}
	contractDigest, err := rpc.callContract(ctx, safeAddress, encodeGetTransactionHash(transaction))
	if err != nil {
		t.Fatal(err)
	}
	if common.BytesToHash(contractDigest) != common.BytesToHash(intent.Digest[:]) {
		t.Fatalf("coordinator digest differs from real Safe: local=%x contract=%x", intent.Digest, contractDigest)
	}
	coordinator, err := NewSafeCoordinator(intent)
	if err != nil {
		t.Fatal(err)
	}
	for _, index := range []int{2, 0} {
		if err := coordinator.AddEOASignature(owners[index].Address, intent.Digest, intent.Commitment, safeCoordinatorSign(t, keys[index], intent.Digest)); err != nil {
			t.Fatal(err)
		}
	}
	calldata, err := coordinator.ExecTransaction()
	if err != nil {
		t.Fatal(err)
	}
	execTx, err := rpc.sendTransaction(ctx, deployer, &safeAddress, calldata)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rpc.waitReceipt(ctx, execTx); err != nil {
		t.Fatal(err)
	}
	balanceAfter, err := rpc.balance(ctx, recipient)
	if err != nil {
		t.Fatal(err)
	}
	if new(big.Int).Sub(balanceAfter, balanceBefore).Cmp(transaction.Value) != 0 {
		t.Fatalf("real Safe transfer mismatch: before=%s after=%s", balanceBefore, balanceAfter)
	}
	nonceAfter, err := rpc.callContract(ctx, safeAddress, selectorOf("nonce()"))
	if err != nil {
		t.Fatal(err)
	}
	if new(big.Int).SetBytes(nonceAfter).Cmp(new(big.Int).Add(transaction.Nonce, big.NewInt(1))) != 0 {
		t.Fatalf("real Safe nonce did not advance: %x -> %x", nonceBytes, nonceAfter)
	}
}

func safeCoordinatorTestKey(t *testing.T, scalar int64) *ecdsa.PrivateKey {
	t.Helper()
	key, err := crypto.ToECDSA(common.LeftPadBytes(big.NewInt(scalar).Bytes(), 32))
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func safeCoordinatorEOAOwners(keys []*ecdsa.PrivateKey) []SafeOwnerSnapshot {
	owners := make([]SafeOwnerSnapshot, len(keys))
	for index, key := range keys {
		owners[index] = SafeOwnerSnapshot{Address: crypto.PubkeyToAddress(key.PublicKey), Kind: SafeOwnerEOA}
	}
	return owners
}

func safeCoordinatorTestTransaction(nonce int64) SafeTransaction {
	return SafeTransaction{
		To:             common.HexToAddress("0x7000000000000000000000000000000000000007"),
		Value:          big.NewInt(123456789),
		Data:           []byte{0xde, 0xad, 0xbe, 0xef, 0x01},
		Operation:      1,
		SafeTxGas:      big.NewInt(21000),
		BaseGas:        big.NewInt(1234),
		GasPrice:       big.NewInt(17),
		GasToken:       common.HexToAddress("0x6000000000000000000000000000000000000006"),
		RefundReceiver: common.HexToAddress("0x5000000000000000000000000000000000000005"),
		Nonce:          big.NewInt(nonce),
	}
}

func safeCoordinatorSign(t *testing.T, key *ecdsa.PrivateKey, digest [32]byte) []byte {
	t.Helper()
	signature, err := crypto.Sign(digest[:], key)
	if err != nil {
		t.Fatal(err)
	}
	return signature
}

func safeCoordinatorRecoverOwner(t *testing.T, digest [32]byte, signature []byte) common.Address {
	t.Helper()
	if len(signature) != safeStaticSignatureLength || (signature[64] != 27 && signature[64] != 28) {
		t.Fatalf("invalid Safe EOA signature: %x", signature)
	}
	recoverySignature := append([]byte(nil), signature...)
	recoverySignature[64] -= 27
	publicKey, err := crypto.SigToPub(digest[:], recoverySignature)
	if err != nil {
		t.Fatal(err)
	}
	return crypto.PubkeyToAddress(*publicKey)
}

func safeCoordinatorContractPayloadAt(t *testing.T, aggregate []byte, offset int) ([]byte, int) {
	t.Helper()
	if offset < 0 || offset+32 > len(aggregate) {
		t.Fatalf("contract offset %d outside %d bytes", offset, len(aggregate))
	}
	length := new(big.Int).SetBytes(aggregate[offset : offset+32])
	if !length.IsUint64() || length.Uint64() > uint64(len(aggregate)-offset-32) {
		t.Fatalf("contract payload length outside aggregate: %s", length)
	}
	payloadLength := int(length.Uint64())
	paddedLength := 32 + ((payloadLength+31)/32)*32
	if offset+paddedLength > len(aggregate) {
		t.Fatalf("padded contract payload outside aggregate: %d", paddedLength)
	}
	padding := aggregate[offset+32+payloadLength : offset+paddedLength]
	if !bytes.Equal(padding, make([]byte, len(padding))) {
		t.Fatalf("nonzero contract payload padding: %x", padding)
	}
	return append([]byte(nil), aggregate[offset+32:offset+32+payloadLength]...), paddedLength
}

func safeCoordinatorAssertExecCalldata(t *testing.T, calldata []byte, transaction SafeTransaction) []byte {
	t.Helper()
	method := safeCoordinatorExecMethod()
	if len(calldata) < 4 || !bytes.Equal(calldata[:4], method.ID) {
		t.Fatalf("invalid execTransaction selector: %x", calldata)
	}
	values, err := method.Inputs.Unpack(calldata[4:])
	if err != nil {
		t.Fatalf("decode execTransaction: %v", err)
	}
	if len(values) != 10 {
		t.Fatalf("decoded %d execTransaction values", len(values))
	}
	if values[0].(common.Address) != transaction.To || values[1].(*big.Int).Cmp(transaction.Value) != 0 || !bytes.Equal(values[2].([]byte), transaction.Data) || values[3].(uint8) != transaction.Operation {
		t.Fatal("decoded execTransaction primary fields differ")
	}
	if values[4].(*big.Int).Cmp(transaction.SafeTxGas) != 0 || values[5].(*big.Int).Cmp(transaction.BaseGas) != 0 || values[6].(*big.Int).Cmp(transaction.GasPrice) != 0 {
		t.Fatal("decoded execTransaction gas fields differ")
	}
	if values[7].(common.Address) != transaction.GasToken || values[8].(common.Address) != transaction.RefundReceiver {
		t.Fatal("decoded execTransaction refund fields differ")
	}
	return append([]byte(nil), values[9].([]byte)...)
}

func safeCoordinatorExecMethod() abi.Method {
	return abi.NewMethod(
		"execTransaction",
		"execTransaction",
		abi.Function,
		"nonpayable",
		false,
		false,
		abi.Arguments{
			{Name: "to", Type: mustABIType("address")},
			{Name: "value", Type: mustABIType("uint256")},
			{Name: "data", Type: mustABIType("bytes")},
			{Name: "operation", Type: mustABIType("uint8")},
			{Name: "safeTxGas", Type: mustABIType("uint256")},
			{Name: "baseGas", Type: mustABIType("uint256")},
			{Name: "gasPrice", Type: mustABIType("uint256")},
			{Name: "gasToken", Type: mustABIType("address")},
			{Name: "refundReceiver", Type: mustABIType("address")},
			{Name: "signatures", Type: mustABIType("bytes")},
		},
		abi.Arguments{},
	)
}

func safeCoordinatorEncodeSetup(owners []common.Address, threshold int64) ([]byte, error) {
	method := abi.NewMethod(
		"setup",
		"setup",
		abi.Function,
		"nonpayable",
		false,
		false,
		abi.Arguments{
			{Name: "owners", Type: mustABIType("address[]")},
			{Name: "threshold", Type: mustABIType("uint256")},
			{Name: "to", Type: mustABIType("address")},
			{Name: "data", Type: mustABIType("bytes")},
			{Name: "fallbackHandler", Type: mustABIType("address")},
			{Name: "paymentToken", Type: mustABIType("address")},
			{Name: "payment", Type: mustABIType("uint256")},
			{Name: "paymentReceiver", Type: mustABIType("address")},
		},
		abi.Arguments{},
	)
	packed, err := method.Inputs.Pack(
		owners,
		big.NewInt(threshold),
		common.Address{},
		[]byte(nil),
		common.Address{},
		common.Address{},
		big.NewInt(0),
		common.Address{},
	)
	if err != nil {
		return nil, err
	}
	return append(append([]byte(nil), method.ID...), packed...), nil
}
