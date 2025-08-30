# Implementation Plan

- [x] 1. Create password file manager component ✅ COMPLETED
  - ✅ Implement password file detection logic for .pwd files with same base name as keystore files
  - ✅ Create secure password file reading functionality with UTF-8 encoding support
  - ✅ Add validation for password file format and size limits (max 1024 bytes for UTF-8 encoding)
  - ✅ Implement error handling for missing, unreadable, or corrupted password files
  - ✅ Add comprehensive error types with localization support
  - ✅ Implement helper methods for password validation and keystore password detection
  - _Requirements: 1.1, 1.2, 5.1_

- [x] 2. Create enhanced file picker component with multi-selection ✅ COMPLETED
  - ✅ Extend BubbleTea filepicker to support multiple file selection using checkbox interface
  - ✅ Implement directory selection capability for batch keystore import
  - ✅ Add JSON file filtering to show only keystore files and directories
  - ✅ Create keyboard navigation (space to select, enter to confirm, arrow keys to navigate)
  - ✅ Add visual indicators for selected files and directories
  - _Requirements: 2.1, 2.2, 2.3, 2.4, 2.5, 2.6_

- [x] 3. Implement password input popup component ✅ COMPLETED
  - ✅ Create modal popup overlay component using BubbleTea textinput
  - ✅ Add secure password input with character masking
  - ✅ Implement error message display for incorrect passwords
  - ✅ Add retry mechanism with attempt counter and maximum retry limit
  - ✅ Create cancel/skip functionality with ESC key handling
  - ✅ Display current keystore filename being processed in popup
  - _Requirements: 6.1, 6.2, 6.3, 6.4, 6.5_

- [x] 4. Create animated progress bar component for import operations ✅ COMPLETED
  - ✅ Implement progress bar using BubbleTea progress component with animations
  - ✅ Add current file display showing which keystore is being processed
  - ✅ Create progress percentage calculation based on processed vs total files
  - ✅ Implement pause/resume functionality for password input interruptions
  - ✅ Add error tracking and display for failed imports
  - ✅ Add comprehensive error categorization (failed vs skipped)
  - ✅ Implement completion summary with timing and statistics
  - ✅ Add visual status indicators and user instructions
  - _Requirements: 3.1, 3.2, 3.3, 6.6_

- [x] 5. Implement batch import service with password integration
  - Create batch import service that processes multiple keystore files sequentially
  - Integrate password file manager to automatically detect and use .pwd files
  - Implement import job creation with keystore path, password path, and manual password fields
  - Add progress reporting through channels for real-time UI updates
  - Create error handling that continues processing remaining files after failures
  - _Requirements: 1.5, 4.1, 4.2, 4.3, 4.4_

- [x] 6. Integrate password popup with batch import flow
  - Modify batch import service to pause when manual password input is required
  - Implement communication channels between import service and password popup
  - Add logic to handle password popup results (password provided, cancelled, or skipped)
  - Create retry mechanism for incorrect passwords with popup redisplay
  - Implement file skipping logic that tracks skipped files separately from failures
  - _Requirements: 6.1, 6.2, 6.3, 6.4, 6.5, 6.6_

- [x] 7. Create enhanced import state management
  - Implement state machine for import phases (file selection, importing, password input, complete)
  - Add state tracking for selected files, import jobs, results, and current progress
  - Create password popup state management within import flow
  - Implement phase transitions and state validation
  - Add cleanup logic for cancelled or interrupted imports
  - _Requirements: 5.4_

- [x] 8. Implement directory scanning and keystore detection
  - Create directory scanning functionality to find all JSON files recursively
  - Add keystore file validation to identify valid KeyStoreV3 format files
  - Implement automatic password file detection for each discovered keystore
  - Create import job generation for directory-based imports
  - Add error handling for directories with no valid keystore files
  - _Requirements: 4.1, 4.2, 4.5_

- [x] 9. Add comprehensive error handling and localization
  - Extend existing KeystoreImportError types to include password file errors
  - Implement localized error messages for all new error conditions
  - Add error recovery strategies for different failure types
  - Create user-friendly error messages that don't expose sensitive information
  - Implement error aggregation and reporting for batch operations
  - _Requirements: 5.1, 5.2, 5.3, 5.5_

- [ ] 10. Create import completion and summary display
  - Implement completion phase UI showing success/failure/skipped counts
  - Add detailed error information display for failed imports
  - Create list of skipped files with reasons for skipping
  - Implement retry functionality for failed or skipped imports
  - Add navigation options to return to main menu or wallet list
  - _Requirements: 3.4, 3.5_

- [ ] 11. Integrate enhanced import with existing TUI
  - Modify existing import keystore view to use enhanced file picker
  - Replace current import flow with new batch import service
  - Integrate progress bar display into existing TUI layout
  - Add password popup overlay to existing view system
  - Update menu navigation to support new import workflow
  - _Requirements: 2.1, 3.1_

- [ ] 12. Add comprehensive unit tests for password file manager
  - Test password file detection with various filename patterns
  - Test password reading with different file encodings and formats
  - Test validation logic for empty, oversized, and corrupted password files
  - Test error handling for permission issues and missing files
  - Create test data with various password file scenarios
  - _Requirements: 1.1, 1.2, 1.3, 5.1_

- [ ] 13. Add unit tests for enhanced file picker component
  - Test multi-file selection functionality with keyboard and mouse interactions
  - Test directory selection and navigation
  - Test file filtering for JSON keystore files
  - Test visual state updates for selected files
  - Test keyboard navigation and selection confirmation
  - _Requirements: 2.2, 2.3, 2.4, 2.5, 2.6_

- [ ] 14. Add unit tests for batch import service
  - Test batch processing with mixed success/failure scenarios
  - Test progress reporting accuracy and timing
  - Test password file integration and fallback to manual input
  - Test error aggregation and continued processing after failures
  - Test import job creation and result tracking
  - _Requirements: 4.3, 4.4, 1.5_

- [ ] 15. Add integration tests for complete import workflow
  - Test end-to-end import flow with password files present
  - Test import flow with manual password input for files without .pwd
  - Test mixed scenarios with some files having password files and others requiring manual input
  - Test error scenarios and recovery mechanisms
  - Test import cancellation and cleanup
  - _Requirements: 1.4, 6.3, 6.4, 6.5, 5.4_

- [ ] 16. Create test data for enhanced import functionality
  - Generate test keystore files with corresponding .pwd files
  - Create test keystores without password files for manual input testing
  - Generate invalid keystore files for error handling tests
  - Create test directories with mixed keystore and non-keystore files
  - Add test password files with various formats and edge cases
  - _Requirements: 4.1, 4.2, 5.2_