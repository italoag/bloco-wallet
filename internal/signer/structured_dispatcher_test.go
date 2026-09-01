package signer

import (
	"context"
	"math/big"
	"testing"

	"blocowallet/internal/evm"
	"blocowallet/internal/wallet"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

type structuredDigestTestSigner struct {
	key *big.Int
}

func (signer structuredDigestTestSigner) Sign(_ context.Context, _ wallet.CapabilityHandle, request wallet.SoftwareSigningRequest) (wallet.SoftwareSigningResult, error) {
	privateKey, err := crypto.ToECDSA(signer.key.FillBytes(make([]byte, 32)))
	if err != nil {
		return wallet.SoftwareSigningResult{}, err
	}
	signature, err := crypto.Sign(request.Digest[:], privateKey)
	if err != nil {
		return wallet.SoftwareSigningResult{}, err
	}
	return wallet.SoftwareSigningResult{
		AccountID: request.AccountID, Purpose: request.Purpose, MessageScheme: request.MessageScheme,
		ChainID: request.ChainID, Digest: request.Digest, IntentHash: request.IntentHash,
		Signature: signature,
	}, nil
}

func TestStructuredDispatcherRoutesFrozenTransactionToLedger(t *testing.T) {
	ledgerKey := ledgerTransactionTestKey(t, 0x01)
	account := &wallet.Account{
		AccountID: "11111111-1111-4111-8111-111111111111", Name: "Ledger",
		Address:    crypto.PubkeyToAddress(ledgerKey.PublicKey).Hex(),
		SignerKind: wallet.SignerKindHardware, SignerReference: "ledger:v1:" + ledgerTransactionTestPath,
		Capabilities: wallet.CapabilitySignTransaction | wallet.CapabilitySignMessage,
		State:        wallet.AccountStateActive,
	}
	chainID := big.NewInt(1)
	to := common.HexToAddress("0x2222222222222222222222222222222222222222")
	transaction := types.NewTx(&types.LegacyTx{
		Nonce: 3, GasPrice: big.NewInt(4), Gas: 21_000, To: &to, Value: big.NewInt(5),
	})
	encodedForLedger := ledgerTransactionTestEncode(t, transaction, chainID)
	deviceIntent := ledgerTransactionTestIntent(transaction, chainID, encodedForLedger, ledgerKey)
	transport := ledgerTransactionSigningTransport(ledgerKey, deviceIntent, encodedForLedger)
	device := ledgerTransactionTestDevice(t, transport)
	ledger, err := NewLedgerSigner(device, fakeAccountLookup{account: account}, fakeApprovalVerifier{})
	if err != nil {
		t.Fatal(err)
	}
	software, err := evm.NewDigestSignerAdapter(structuredDigestTestSigner{key: big.NewInt(9)})
	if err != nil {
		t.Fatal(err)
	}
	dispatcher, err := NewStructuredDispatcher(software, nil, ledger, nil, fakeAccountLookup{account: account}, fakeApprovalVerifier{}, StructuredDispatcherOptions{})
	if err != nil {
		t.Fatal(err)
	}
	unsigned, err := transaction.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	intent := evm.TransactionSigningIntent{
		AccountID: account.AccountID, From: common.HexToAddress(account.Address), ChainID: 1,
		Digest: types.NewEIP155Signer(chainID).Hash(transaction), PlanHash: [32]byte{1},
		ApprovalID: "21111111-1111-4111-8111-111111111111", UnsignedTransaction: unsigned,
	}
	result, err := dispatcher.SignTransaction(context.Background(), wallet.CapabilityHandle{}, intent)
	if err != nil {
		t.Fatal(err)
	}
	if result.AccountID != intent.AccountID || result.Digest != intent.Digest || result.Purpose != wallet.SigningPurposeTransaction {
		t.Fatalf("unexpected structured result: %+v", result)
	}
	if err := verifyECDSASignature(common.HexToAddress(account.Address), intent.Digest, result.Signature); err != nil {
		t.Fatal(err)
	}
	if len(transport.calls) == 0 || transport.calls[0].ins != ledgerINSSign {
		t.Fatal("structured transaction did not reach Ledger INS 0x04")
	}
}

func TestStructuredDispatcherRoutesFrozenTransactionToTrezor(t *testing.T) {
	trezorKey := ledgerTransactionTestKey(t, 0x31)
	account := &wallet.Account{
		AccountID: "11111111-1111-4111-8111-111111111111", Name: "Trezor",
		Address:    crypto.PubkeyToAddress(trezorKey.PublicKey).Hex(),
		SignerKind: wallet.SignerKindHardware, SignerReference: "trezor:v1:m/44'/60'/0'/0/0",
		Capabilities: wallet.CapabilitySignTransaction | wallet.CapabilitySignMessage,
		State:        wallet.AccountStateActive,
	}
	to := common.HexToAddress("0x3333333333333333333333333333333333333333")
	transaction := types.NewTx(&types.LegacyTx{Nonce: 1, GasPrice: big.NewInt(2), Gas: 21_000, To: &to, Value: big.NewInt(3)})
	digest := types.NewEIP155Signer(big.NewInt(1)).Hash(transaction)
	signature, err := crypto.Sign(digest[:], trezorKey)
	if err != nil {
		t.Fatal(err)
	}
	features := []byte{0x10, 0x01, 0x18, 0x0e, 0x20, 0x01, 0x60, 0x01, 0xaa, 0x01, 0x01, '1'}
	transport := newTrezorTypedDataScriptTransport(
		trezorTypedDataWireMessage{messageType: trezorMessageFeatures, payload: features},
		trezorTransactionSignatureResponse(uint32(signature[64]+37), signature),
	)
	device := &UDPDevice{transport: transport}
	trezorSigner, err := NewTrezorSigner(device, fakeAccountLookup{account: account}, fakeApprovalVerifier{})
	if err != nil {
		t.Fatal(err)
	}
	software, err := evm.NewDigestSignerAdapter(structuredDigestTestSigner{key: big.NewInt(9)})
	if err != nil {
		t.Fatal(err)
	}
	dispatcher, err := NewStructuredDispatcher(software, nil, nil, trezorSigner, fakeAccountLookup{account: account}, fakeApprovalVerifier{}, StructuredDispatcherOptions{})
	if err != nil {
		t.Fatal(err)
	}
	unsigned, err := transaction.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	result, err := dispatcher.SignTransaction(context.Background(), wallet.CapabilityHandle{}, evm.TransactionSigningIntent{
		AccountID: account.AccountID, From: common.HexToAddress(account.Address), ChainID: 1,
		Digest: digest, PlanHash: [32]byte{1}, ApprovalID: "21111111-1111-4111-8111-111111111111",
		UnsignedTransaction: unsigned,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyECDSASignature(common.HexToAddress(account.Address), digest, result.Signature); err != nil {
		t.Fatal(err)
	}
	if len(transport.writes) != 2 || transport.writes[0].messageType != trezorMessageInitialize || transport.writes[1].messageType != trezorMessageEthereumSignTx {
		t.Fatalf("unexpected Trezor structured writes: %+v", transport.writes)
	}
}

func TestStructuredDispatcherNeverFallsBackHardwareToDigest(t *testing.T) {
	account := &wallet.Account{
		AccountID: "11111111-1111-4111-8111-111111111111", Name: "Unknown hardware",
		Address:    "0x1111111111111111111111111111111111111111",
		SignerKind: wallet.SignerKindHardware, SignerReference: "unknown:v1:path",
		Capabilities: wallet.CapabilitySignMessage, State: wallet.AccountStateActive,
	}
	legacy := structuredDigestTestSigner{key: big.NewInt(9)}
	software, err := evm.NewDigestSignerAdapter(legacy)
	if err != nil {
		t.Fatal(err)
	}
	dispatcher, err := NewStructuredDispatcher(software, nil, nil, nil, fakeAccountLookup{account: account}, fakeApprovalVerifier{}, StructuredDispatcherOptions{})
	if err != nil {
		t.Fatal(err)
	}
	message := []byte("hardware message")
	prepared, err := evm.PreparePersonalSign(evm.PreparePersonalSignRequest{
		AccountID: account.AccountID, Signer: common.HexToAddress(account.Address),
		Message: message, Origin: "local-user",
	})
	if err != nil {
		t.Fatal(err)
	}
	preview := prepared.Preview()
	_, err = dispatcher.SignPersonalMessage(context.Background(), wallet.CapabilityHandle{}, evm.PersonalMessageSigningIntent{
		AccountID: account.AccountID, Signer: common.HexToAddress(account.Address),
		Message: message, Origin: preview.Origin, Digest: preview.Digest,
		IntentHash: preview.IntentHash, ApprovalID: "21111111-1111-4111-8111-111111111111",
	})
	if err == nil {
		t.Fatal("unknown hardware route fell back to software digest signing")
	}
}
