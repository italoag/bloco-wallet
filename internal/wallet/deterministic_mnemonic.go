//go:build never
// +build never

package wallet

// Deprecated: Deterministic mnemonic functionality has been removed.
// - GenerateDeterministicMnemonic
// - ValidateMnemonicMatchesPrivateKey
// - GenerateAndValidateDeterministicMnemonic
//
// Rationale:
// It is not possible to recover the original mnemonic from a private key. The
// application now avoids generating fake mnemonics. For keystore imports only,
// a synthetic mnemonic is generated within wallet_service.go for legacy UI
// behavior, clearly documented as synthetic.
