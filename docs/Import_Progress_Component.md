# Import Progress Component

## Overview

The Import Progress Component is an animated progress tracking interface built with BubbleTea that provides real-time feedback during keystore import operations. It features comprehensive error tracking, pause/resume functionality, and detailed completion summaries designed specifically for the enhanced keystore import workflow in BLOCO Wallet Manager.

## Architecture

### Core Components

1. **ImportProgressModel**: Main model struct containing progress state and UI configuration
2. **ImportProgressMsg**: Message structure for progress updates and state changes
3. **ImportError**: Error tracking structure with categorization support
4. **Animated Progress Bar**: Visual progress indicator with smooth animations

### Key Features

- **Real-time Progress Tracking**: Live updates of import progress with percentage calculation
- **Current File Display**: Shows which keystore file is currently being processed
- **Error Categorization**: Distinguishes between failed imports and skipped files
- **Pause/Resume Support**: Handles interruptions for password input or user intervention
- **Completion Summary**: Detailed statistics and timing information upon completion
- **Visual Status Indicators**: Clear visual feedback for different import states
- **Error History**: Tracks and displays up to recent errors with context

## Integration

### Dependencies

The component uses the following external libraries:

- `github.com/charmbracelet/bubbletea`: Core TUI framework
- `github.com/charmbracelet/bubbles/progress`: Animated progress bar component
- `github.com/charmbracelet/lipgloss`: Styling and layout management

### Usage Pattern

```go
// Initialize progress component
progress := NewImportProgressModel(totalFiles, styles)

// BubbleTea integration
func (m ImportModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    var cmd tea.Cmd
    
    switch m.state {
    case StateImporting:
        m.progress, cmd = m.progress.Update(msg)
        
        if m.progress.IsCompleted() {
            // Handle completion...
        }
    }
    
    return m, cmd
}

// Send progress updates
progressMsg := ImportProgressMsg{
    CurrentFile:    "wallet.json",
    ProcessedFiles: 5,
    TotalFiles:     10,
    Completed:      false,
}
```

## Configuration Options

### Visual Settings
- `width`: Progress bar width (default: 60 characters)
- `gradient`: Progress bar color gradient (default gradient)
- `animations`: Smooth progress bar animations
- Color schemes for different states (normal, paused, completed, error)

### Progress Tracking
- `totalFiles`: Total number of files to process
- `processedFiles`: Number of files completed
- `currentFile`: Currently processing file name
- `errors`: Collection of import errors with categorization

### Error Management
- Error categorization (failed vs skipped)
- Error history with file context
- Error display limiting (shows recent 3 errors)
- Error counting and statistics

## API Reference

### Constructor
```go
func NewImportProgressModel(totalFiles int, styles Styles) ImportProgressModel
```

### Core Methods
```go
// BubbleTea interface
func (m ImportProgressModel) Init() tea.Cmd
func (m ImportProgressModel) Update(msg tea.Msg) (ImportProgressModel, tea.Cmd)
func (m ImportProgressModel) View() string

// Progress management
func (m *ImportProgressModel) Reset(totalFiles int)
func (m *ImportProgressModel) UpdateProgress(currentFile string, processedFiles int)
func (m *ImportProgressModel) AddError(file string, err error, skipped bool)
func (m *ImportProgressModel) Complete()

// State control
func (m *ImportProgressModel) Pause(reason string)
func (m *ImportProgressModel) Resume()

// State queries
func (m ImportProgressModel) IsCompleted() bool
func (m ImportProgressModel) IsPaused() bool
func (m ImportProgressModel) GetPercentage() float64
func (m ImportProgressModel) GetErrors() []ImportError
func (m ImportProgressModel) GetFailedErrors() []ImportError
func (m ImportProgressModel) GetSkippedErrors() []ImportError
func (m ImportProgressModel) GetSummaryText() string
```

### Message Structure
```go
type ImportProgressMsg struct {
    CurrentFile     string        // Currently processing file
    ProcessedFiles  int          // Number of completed files
    TotalFiles      int          // Total files to process
    Error           *ImportError // Error information (if any)
    Completed       bool         // Import completion status
    Paused          bool         // Pause status
    PauseReason     string       // Reason for pause
}
```

### Error Structure
```go
type ImportError struct {
    File    string  // File that caused the error
    Error   error   // The actual error
    Skipped bool    // Whether file was skipped vs failed
}
```

## Visual Design

### Progress Display Elements

1. **Title Section**: "Import Progress" header
2. **Progress Bar**: Animated progress bar with gradient
3. **Statistics**: "Progress: X/Y files (Z%)" display
4. **Current File**: "Processing: filename.json" or "Paused on: filename.json"
5. **Status Indicator**: 
   - ⏳ Importing... (elapsed: Xs)
   - ⏸ Import paused - reason
   - ✓ Import completed in Xs
6. **Error Summary**: Recent errors with categorization
7. **Instructions**: Context-appropriate user instructions

### Status States

- **Active**: Shows current file, elapsed time, progress animation
- **Paused**: Shows pause reason, paused file, pause indicator
- **Completed**: Shows completion time, success/failed/skipped counts
- **Error**: Shows error details without exposing sensitive information

## Error Handling

### Error Categories
- **Failed Imports**: Files that encountered errors during processing
- **Skipped Files**: Files intentionally skipped by user choice
- **Processing Errors**: System-level errors during import operation

### Error Display
- Shows up to 3 most recent errors
- Indicates if more errors occurred ("... and X more errors")
- Categorizes errors as "Failed" or "Skipped"
- Displays filename and error message
- Maintains error history for completion summary

### Error Recovery
- Continues processing remaining files after errors
- Tracks error context for debugging
- Provides retry options for failed imports
- Maintains separation between failed and skipped files

## Performance Characteristics

- **Smooth Animations**: 60fps progress bar updates
- **Efficient Rendering**: Minimal UI updates for responsive interaction
- **Memory Management**: Efficient error collection and display
- **Real-time Updates**: Immediate progress reflection
- **Responsive Design**: Adapts to terminal size changes

## Integration Points

The Import Progress Component integrates seamlessly with:

1. **Batch Import Service**: Receives progress updates during keystore processing
2. **Password Popup Component**: Pauses progress during password input
3. **Enhanced File Picker**: Displays progress for selected files
4. **Password File Manager**: Shows progress for automatic password detection
5. **Error Handling System**: Displays localized error messages
6. **Main TUI**: Integrates into existing view system

## Workflow Integration

### Batch Import Flow
1. **Initialization**: Progress component created with total file count
2. **Processing Updates**: Real-time updates as files are processed
3. **Error Tracking**: Errors added with categorization (failed/skipped)
4. **Pause Handling**: Progress paused during password input
5. **Resume Processing**: Progress resumed after user input
6. **Completion**: Final summary with statistics and timing

### State Transitions
1. **Active → Paused**: When password input is required
2. **Paused → Active**: When password input is completed
3. **Active → Completed**: When all files are processed
4. **Any → Reset**: When starting a new import operation

## User Experience Features

### Visual Feedback
- Animated progress bar with smooth transitions
- Color-coded status indicators
- Clear file-by-file progress tracking
- Comprehensive error categorization

### Information Display
- Real-time progress percentage
- Current file being processed
- Elapsed time tracking
- Success/failure/skip statistics
- Error details with context

### User Instructions
- Context-appropriate instructions based on state
- Clear cancellation options during import
- Retry options for failed imports
- Navigation guidance upon completion

## Testing Strategy

The component includes comprehensive test coverage:

### Unit Tests
- **Progress Calculation**: Percentage calculation accuracy
- **State Management**: State transitions and validation
- **Error Tracking**: Error categorization and counting
- **Message Handling**: Progress update message processing
- **Visual Rendering**: UI component rendering validation

### Integration Tests
- **BubbleTea Integration**: Message handling and command generation
- **Batch Import Integration**: Real-world progress tracking
- **Error Scenarios**: Error handling and display validation
- **Pause/Resume**: State management during interruptions

### Demo Applications
- **Interactive Demo**: Manual testing interface with simulated imports
- **Automated Demo**: Scripted progress simulation
- **Performance Testing**: Animation smoothness and responsiveness

## Future Enhancements

Planned improvements for future versions:

### Enhanced Visualization
- **File Tree View**: Visual representation of import hierarchy
- **Speed Indicators**: Import rate and estimated completion time
- **Progress History**: Historical progress tracking across sessions

### Advanced Error Handling
- **Error Categorization**: More granular error classification
- **Recovery Suggestions**: Automated recovery recommendations
- **Error Export**: Export error logs for debugging

### User Experience
- **Customizable Display**: User-configurable progress display options
- **Sound Notifications**: Audio feedback for completion/errors
- **Background Processing**: Continue imports in background

## Compliance and Standards

### Performance Standards
- **Smooth Animations**: Maintains 60fps animation rate
- **Memory Efficiency**: Bounded error collection and display
- **Responsive Updates**: Sub-100ms update response time

### Accessibility
- **Screen Reader**: Compatible with screen reading software
- **Keyboard Navigation**: Full keyboard accessibility
- **Color Contrast**: Sufficient contrast for visual accessibility

### Internationalization
- **Unicode Support**: Proper UTF-8 filename handling
- **Localization Ready**: Prepared for multi-language support
- **Cultural Adaptation**: Adaptable progress display formats

## Related Documentation

- [Password Popup Component](Password_Popup_Component.md)
- [Enhanced File Picker Component](Enhanced_File_Picker.md)
- [Password File Manager](../internal/wallet/password_file_manager.go)
- [Batch Import Service](../internal/ui/batch_import_service.go)
- [Universal KDF Documentation](Universal_KDF.md)

## Support and Maintenance

### Debugging
- Enable verbose progress logging for troubleshooting
- Use demo applications for manual testing
- Monitor animation performance and responsiveness

### Performance Monitoring
- Track animation frame rates
- Monitor memory usage during long imports
- Validate progress calculation accuracy

### Updates and Patches
- Regular performance optimization reviews
- Compatibility testing with new BubbleTea versions
- User feedback integration for UX improvements