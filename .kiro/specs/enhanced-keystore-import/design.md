# Design Document

## Overview

The Enhanced Keystore Import feature extends the existing keystore import functionality in BLOCO Wallet Manager to provide automatic password file detection, interactive file selection with multi-selection capabilities, and visual progress feedback. This enhancement builds upon the existing BubbleTea TUI framework and integrates seamlessly with the current wallet service architecture.

## Architecture

The enhanced import system follows a modular architecture with clear separation of concerns:

```
┌─────────────────────────────────────────────────────────────┐
│                    Enhanced Import UI Layer                 │
├─────────────────────────────────────────────────────────────┤
│  File Picker    │  Progress Bar    │  Password Manager      │
│  Component      │  Component       │  Component             │
└─────────────────────────────────────────────────────────────┘
                              │
┌─────────────────────────────────────────────────────────────┐
│                    Import Service Layer                     │
├─────────────────────────────────────────────────────────────┤
│  Batch Import   │  Password File   │  Progress Tracking     │
│  Manager        │  Handler         │  Manager               │
└─────────────────────────────────────────────────────────────┘
                              │
┌─────────────────────────────────────────────────────────────┐
│                 Existing Wallet Service                     │
├─────────────────────────────────────────────────────────────┤
│  ImportWalletFromKeystoreV3  │  KeystoreValidator           │
└─────────────────────────────────────────────────────────────┘
```

## Components and Interfaces

### 1. Enhanced File Picker Component

**Location**: `internal/ui/enhanced_file_picker.go`

```go
type EnhancedFilePickerModel struct {
    filepicker.Model
    multiSelect     bool
    selectedFiles   []string
    selectedDir     string
    allowDirectory  bool
    fileFilter      func(string) bool
}

type FilePickerResult struct {
    Files     []string
    Directory string
    Cancelled bool
}
```

**Key Features**:
- Multi-file selection with checkbox interface
- Directory selection support
- JSON file filtering for keystore files
- Keyboard navigation (space to select, enter to confirm)
- Visual indicators for selected files

### 2. Progress Bar Component

**Location**: `internal/ui/import_progress.go`

```go
type ImportProgressModel struct {
    progress.Model
    currentFile    string
    totalFiles     int
    processedFiles int
    errors         []ImportError
    completed      bool
    paused         bool
    pauseReason    string
}

type ImportError struct {
    File    string
    Error   error
    Skipped bool
}
```

**Key Features**:
- Animated progress bar using BubbleTea progress component
- Current file display
- Error tracking and display
- Completion summary with success/failure counts
- Pause/resume capability for password input

### 3. Password Input Popup Component

**Location**: `internal/ui/password_popup.go`

```go
type PasswordPopupModel struct {
    textinput.Model
    keystoreFile   string
    errorMessage   string
    retryCount     int
    maxRetries     int
    cancelled      bool
    confirmed      bool
}

type PasswordPopupResult struct {
    Password  string
    Cancelled bool
    Skip      bool
}
```

**Key Features**:
- Modal popup overlay for password input
- Error display for incorrect passwords
- Retry mechanism with attempt counter
- Cancel/skip options for individual files
- Secure password input with masking

### 4. Password File Manager

**Location**: `internal/wallet/password_file_manager.go`

```go
type PasswordFileManager struct{}

func (pfm *PasswordFileManager) FindPasswordFile(keystorePath string) (string, error)
func (pfm *PasswordFileManager) ReadPasswordFile(passwordPath string) (string, error)
func (pfm *PasswordFileManager) ValidatePasswordFile(passwordPath string) error
func (pfm *PasswordFileManager) RequiresManualPassword(keystorePath string) bool
```

**Key Features**:
- Automatic .pwd file detection based on keystore filename
- Secure password file reading with validation
- Error handling for missing or corrupted password files
- Detection of files requiring manual password input

### 5. Batch Import Service

**Location**: `internal/wallet/batch_import_service.go`

```go
type BatchImportService struct {
    walletService *WalletService
    passwordMgr   *PasswordFileManager
}

type ImportJob struct {
    KeystorePath    string
    PasswordPath    string
    ManualPassword  string
    WalletName      string
    RequiresInput   bool
}

type ImportResult struct {
    Job     ImportJob
    Success bool
    Wallet  *WalletDetails
    Error   error
    Skipped bool
}

type ImportProgress struct {
    CurrentFile     string
    TotalFiles      int
    ProcessedFiles  int
    Percentage      float64
    Errors          []ImportError
    PendingPassword bool
    PendingFile     string
}

func (bis *BatchImportService) ImportBatch(jobs []ImportJob, progressChan chan<- ImportProgress, passwordChan <-chan PasswordPopupResult) []ImportResult
```

## Data Models

### Enhanced Import State

```go
type EnhancedImportState struct {
    Phase           ImportPhase
    SelectedFiles   []string
    SelectedDir     string
    ImportJobs      []ImportJob
    Results         []ImportResult
    CurrentProgress ImportProgress
    PasswordPopup   *PasswordPopupModel
    ShowingPopup    bool
}

type ImportPhase int
const (
    PhaseFileSelection ImportPhase = iota
    PhaseImporting
    PhasePasswordInput
    PhaseComplete
)

type ImportProgress struct {
    CurrentFile     string
    TotalFiles      int
    ProcessedFiles  int
    Percentage      float64
    Errors          []ImportError
    PendingPassword bool
    PendingFile     string
}
```

### Password File Structure

Password files (.pwd) contain plain text passwords with the following constraints:
- Single line containing the password
- UTF-8 encoding
- Maximum 1024 characters
- No trailing whitespace (automatically trimmed)

## Error Handling

### Enhanced Error Types

```go
type PasswordFileError struct {
    Type    PasswordFileErrorType
    File    string
    Message string
    Cause   error
}

type PasswordFileErrorType int
const (
    PasswordFileNotFound PasswordFileErrorType = iota
    PasswordFileUnreadable
    PasswordFileEmpty
    PasswordFileInvalid
)
```

### Error Recovery Strategies

1. **Password File Errors**: Fall back to manual password input
2. **Individual Import Failures**: Continue with remaining files, track errors
3. **File Access Errors**: Display clear error messages with file paths
4. **Validation Errors**: Show specific validation failures per file

## Testing Strategy

### Unit Tests

1. **Password File Manager Tests**
   - Test password file detection logic
   - Test password reading and validation
   - Test error handling for various file conditions

2. **Batch Import Service Tests**
   - Test batch processing with mixed success/failure scenarios
   - Test progress reporting accuracy
   - Test error aggregation and reporting

3. **Enhanced File Picker Tests**
   - Test multi-selection functionality
   - Test directory selection
   - Test file filtering

### Integration Tests

1. **End-to-End Import Flow Tests**
   - Test complete import flow with password files
   - Test import flow with manual password input
   - Test mixed scenarios (some files with .pwd, some without)

2. **UI Integration Tests**
   - Test file picker integration with import service
   - Test progress bar updates during import
   - Test error display and user interaction

### Test Data Structure

```
internal/wallet/testdata/enhanced_import/
├── keystores/
│   ├── wallet1.json
│   ├── wallet1.pwd
│   ├── wallet2.json
│   ├── wallet3.json
│   └── wallet3.pwd
├── invalid_keystores/
│   ├── corrupted.json
│   └── invalid_format.json
└── password_files/
    ├── empty.pwd
    ├── invalid_chars.pwd
    └── too_long.pwd
```

## Implementation Flow

### File Selection Phase

1. Display enhanced file picker with multi-select capability
2. Allow user to select individual files or entire directory
3. Filter display to show only .json files and directories
4. Validate selected files are valid keystore format
5. Proceed to import phase

### Import Processing Phase

1. Scan selected files/directory for keystore files
2. For each keystore file, check for corresponding .pwd file
3. Create import jobs with keystore and password file paths
4. Display progress bar and begin batch import
5. Process each import job sequentially:
   - If password file exists, use it automatically
   - If no password file, pause import and show password popup
   - Allow user to enter password, cancel specific file, or retry
   - Continue with next file after password resolution
6. Update progress bar and current file display
7. Handle errors gracefully, continuing with remaining files
8. Track skipped files separately from failed imports

### Completion Phase

1. Display import summary with success/failure/skipped counts
2. Show detailed error information for failed imports
3. List skipped files (user cancelled password input)
4. Provide option to retry failed or skipped imports
5. Return to main menu or wallet list

## Security Considerations

1. **Password File Security**
   - Password files are read once and immediately cleared from memory
   - No password caching or persistence beyond import operation
   - File permissions validation before reading

2. **Error Message Security**
   - Avoid exposing sensitive information in error messages
   - Generic error messages for authentication failures
   - Detailed technical errors only in debug logs

3. **File System Security**
   - Validate file paths to prevent directory traversal
   - Check file permissions before attempting operations
   - Sanitize file names in error messages

## Performance Considerations

1. **Memory Management**
   - Process files sequentially to limit memory usage
   - Clear sensitive data immediately after use
   - Efficient progress tracking without excessive updates

2. **File I/O Optimization**
   - Batch file system operations where possible
   - Validate file existence before processing
   - Use streaming for large file operations

3. **UI Responsiveness**
   - Non-blocking import operations using goroutines
   - Smooth progress bar animations
   - Responsive keyboard input during operations