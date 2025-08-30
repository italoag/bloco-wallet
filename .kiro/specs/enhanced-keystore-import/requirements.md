# Requirements Document

## Introduction

This feature enhances the KeyStore V3 import process in BLOCO Wallet Manager to automatically detect and use password files (.pwd) alongside keystore files (.json), implement an interactive file picker with multi-selection capabilities, and provide visual progress feedback during import operations. The enhancement will streamline the user experience by automating password detection and providing better visual feedback during the import process.

## Requirements

### Requirement 1

**User Story:** As a wallet user, I want the system to automatically detect and use password files (.pwd) when importing keystore files, so that I don't have to manually enter passwords for keystores that have associated password files.

#### Acceptance Criteria

1. WHEN a keystore file is selected for import THEN the system SHALL check for a corresponding .pwd file with the same base name
2. WHEN a .pwd file exists for a keystore THEN the system SHALL automatically read the password from the .pwd file
3. WHEN using a password from a .pwd file THEN the system SHALL validate the keystore using that password
4. IF the password from the .pwd file is invalid THEN the system SHALL prompt the user to enter the password manually via popup
5. WHEN importing multiple keystores THEN the system SHALL check for .pwd files for each keystore individually

### Requirement 2

**User Story:** As a wallet user, I want to use an interactive file picker to select keystore files or directories for import, so that I can easily navigate and select the files I want to import.

#### Acceptance Criteria

1. WHEN starting the keystore import process THEN the system SHALL display a file picker interface
2. WHEN using the file picker THEN the system SHALL allow navigation through directories
3. WHEN selecting files THEN the system SHALL support multiple file selection
4. WHEN selecting a directory THEN the system SHALL import all valid keystore files from that directory
5. WHEN displaying files THEN the system SHALL filter to show only .json files and directories
6. WHEN confirming selection THEN the system SHALL validate that selected files are valid keystore format

### Requirement 3

**User Story:** As a wallet user, I want to see a progress bar during keystore import operations, so that I can track the progress of the import process and know the system is working.

#### Acceptance Criteria

1. WHEN importing multiple keystores THEN the system SHALL display an animated progress bar
2. WHEN processing each keystore THEN the system SHALL update the progress bar to reflect completion percentage
3. WHEN import is in progress THEN the system SHALL display the current file being processed
4. WHEN import completes successfully THEN the system SHALL show a completion message with summary
5. IF import fails for any file THEN the system SHALL display error details while continuing with remaining files

### Requirement 4

**User Story:** As a wallet user, I want the system to handle directory imports efficiently, so that I can import entire directories of keystore files at once.

#### Acceptance Criteria

1. WHEN selecting a directory for import THEN the system SHALL scan for all .json files in the directory
2. WHEN scanning a directory THEN the system SHALL identify valid keystore files based on content structure
3. WHEN processing directory imports THEN the system SHALL check for corresponding .pwd files for each keystore
4. WHEN importing from a directory THEN the system SHALL provide progress feedback for the entire batch
5. IF a directory contains no valid keystores THEN the system SHALL display an appropriate message

### Requirement 5

**User Story:** As a wallet user, I want proper error handling during the enhanced import process, so that I can understand and resolve any issues that occur.

#### Acceptance Criteria

1. WHEN a .pwd file cannot be read THEN the system SHALL log the error and prompt for manual password entry via popup
2. WHEN a keystore file is corrupted THEN the system SHALL display a clear error message and continue with remaining files
3. WHEN file permissions prevent access THEN the system SHALL display appropriate permission error messages
4. WHEN the import process is cancelled THEN the system SHALL clean up any partial imports
5. WHEN errors occur THEN the system SHALL provide localized error messages in the user's preferred language

### Requirement 6

**User Story:** As a wallet user, I want to be prompted for passwords individually for keystore files without .pwd files, so that I can provide the correct password for each file or skip files I cannot unlock.

#### Acceptance Criteria

1. WHEN a keystore file has no corresponding .pwd file THEN the system SHALL display a password input popup
2. WHEN the password popup is displayed THEN the system SHALL show the filename being processed
3. WHEN an incorrect password is entered THEN the system SHALL display an error message and allow retry
4. WHEN the user presses ESC in the password popup THEN the system SHALL skip that specific file and continue with the next
5. WHEN a file is skipped THEN the system SHALL track it separately from failed imports
6. WHEN the password popup is shown THEN the import progress SHALL pause until password is provided or file is skipped