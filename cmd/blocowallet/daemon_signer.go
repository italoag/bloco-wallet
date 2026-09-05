package main

import (
	"context"

	"blocowallet/internal/evm"
	"blocowallet/internal/wallet"
)

// daemonStructuredSigner rejects every signing operation: the daemon only
// exposes read-only Safe state and proposal creation, never key material.
type daemonStructuredSigner struct{}

func newDaemonStructuredSigner() evm.StructuredSigner {
	return &daemonStructuredSigner{}
}

func (signer *daemonStructuredSigner) SignTransaction(_ context.Context, _ wallet.CapabilityHandle, _ evm.TransactionSigningIntent) (wallet.SoftwareSigningResult, error) {
	return wallet.SoftwareSigningResult{}, errDaemonSigningUnavailable
}

func (signer *daemonStructuredSigner) SignPersonalMessage(_ context.Context, _ wallet.CapabilityHandle, _ evm.PersonalMessageSigningIntent) (wallet.SoftwareSigningResult, error) {
	return wallet.SoftwareSigningResult{}, errDaemonSigningUnavailable
}

func (signer *daemonStructuredSigner) SignEIP712(_ context.Context, _ wallet.CapabilityHandle, _ evm.EIP712SigningIntent) (wallet.SoftwareSigningResult, error) {
	return wallet.SoftwareSigningResult{}, errDaemonSigningUnavailable
}

func (signer *daemonStructuredSigner) SignSafeOwnerDigest(_ context.Context, _ wallet.CapabilityHandle, _ evm.SafeOwnerDigestRequest) (wallet.SoftwareSigningResult, error) {
	return wallet.SoftwareSigningResult{}, errDaemonSigningUnavailable
}

var errDaemonSigningUnavailable = &daemonSigningError{}

type daemonSigningError struct{}

func (error *daemonSigningError) Error() string {
	return "signing is unavailable in the daemon; use the TUI or CLI"
}
