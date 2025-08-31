# Implementation Plan

- [x] 1. Remove synthetic mnemonic generation from keystore imports
  - Remove `generateSyntheticMnemonicFromPrivateKey` function calls from `ImportWalletFromKeystoreV3`
  - Set `Mnemonic` field to `nil` for keystore imports
  - Set `HasMnemonic` to `false` in `WalletDetails` for keystore imports
  - Update `ImportMethod` to be consistently set to "keystore" for keystore imports
  - _Requirements: 3.1, 3.2, 3.3, 5.1, 5.2_

- [x] 2. Update localization labels for keystore import type
  - Add new localization labels for "imported_keystore" type in English, Portuguese, and Spanish
  - Add labels for "method_keystore" to distinguish keystore imports in method selection
  - Add "no_mnemonic_keystore" message for when users try to access mnemonic on keystore imports
  - Add progress stage labels for keystore import steps (validating, parsing, decrypting, saving)
  - _Requirements: 6.1, 6.2, 6.4, 4.2, 4.3, 4.4_

- [x] 3. Fix wallet type display logic in UI
  - Update `determineWalletType` function in `tui.go` to use `ImportMethod` as primary source
  - Modify wallet list display to show "Keystore (Private Key)" for keystore imports
  - Update wallet details view to show correct import method information
  - Add fallback logic for backward compatibility with wallets missing `ImportMethod`
  - _Requirements: 1.1, 1.2, 1.3, 1.4, 6.1, 6.2_

- [x] 4. Enhance progress tracking in keystore import
  - Add progress updates at each major stage of `ImportWalletFromKeystoreV3`
  - Send progress messages for: file validation, keystore parsing, decryption, and saving
  - Implement non-blocking channel sends to avoid slowing down import process
  - Add timeout handling for progress updates to prevent UI freezing
  - _Requirements: 2.1, 2.2, 2.3, 2.4, 4.1, 4.2, 4.3, 4.4_
  
- [x] 5. Fix progress bar update mechanism
  - Ensure `ImportProgressModel` receives and processes progress updates correctly
  - Add debugging logs to track progress message flow
  - Implement fallback progress estimation if detailed updates are not available
  - Fix any channel blocking issues that prevent progress updates
  - _Requirements: 2.1, 2.2, 2.3, 2.4, 2.5_

- [x] 6. Update wallet details view consistency
  - Ensure wallet details show "Import Method: Keystore File" for keystore imports
  - Display "Mnemonic: Not available (imported via keystore file)" message
  - Update help text and tooltips to explain differences between import types
  - Ensure consistent terminology across all UI components
  - _Requirements: 6.1, 6.2, 6.3, 6.4, 6.5_

- [ ] 7. Create database migration for existing keystore wallets
  - Write migration script to identify wallets with synthetic mnemonics
  - Update existing keystore imports to have `Mnemonic: NULL` and `ImportMethod: "keystore"`
  - Add database index for `ImportMethod` field for better query performance
  - Ensure migration preserves legitimate mnemonic-based wallets
  - _Requirements: 5.3, 5.4, 5.5_

- [ ] 8. Add comprehensive error handling
  - Create specific error types for display and progress issues
  - Add localized error messages for keystore-specific scenarios
  - Implement graceful fallbacks when import method information is missing
  - Add validation to prevent inconsistent wallet states
  - _Requirements: 4.5, 5.4, 6.4, 6.5_

- [ ] 9. Update unit tests for corrected behavior
  - Test that keystore imports don't generate synthetic mnemonics
  - Test wallet type display logic with different `ImportMethod` values
  - Test progress tracking sends updates at each import stage
  - Test export functionality shows appropriate options for each import type
  - _Requirements: 5.1, 5.2, 5.3, 5.4, 5.5_

- [ ] 10. Create integration tests for UI consistency
  - Test end-to-end keystore import shows correct type in wallet list
  - Test progress bar updates throughout import process
  - Test wallet details view shows appropriate information for keystore imports
  - Test export options are contextually correct for different wallet types
  - _Requirements: 1.1, 1.2, 1.3, 1.4, 1.5, 2.1, 2.2, 2.3, 2.4, 2.5_

- [ ] 11. Add backward compatibility support
  - Implement fallback logic for wallets created before `ImportMethod` field
  - Ensure existing functionality continues to work during migration
  - Add graceful degradation when new localization labels are not available
  - Test migration process with various existing wallet configurations
  - _Requirements: 5.3, 5.4, 5.5, 6.5_