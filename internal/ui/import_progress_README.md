# Import Progress Component

## Overview

The Import Progress Component (`ImportProgressModel`) is a BubbleTea-based UI component that provides real-time visual feedback during keystore import operations. It's designed to work seamlessly with the enhanced keystore import workflow in BLOCO Wallet Manager.

## Features

- **Real-time Progress Tracking**: Animated progress bar with percentage completion
- **Current File Display**: Shows which keystore file is currently being processed
- **Error Categorization**: Distinguishes between failed imports and user-skipped files
- **Pause/Resume Support**: Handles interruptions for password input or user intervention
- **Completion Summary**: Detailed statistics including success/failure/skip counts and timing
- **Error History**: Tracks and displays recent errors with context
- **Visual Status Indicators**: Clear indicators for importing, paused, and completed states

## Usage

### Basic Usage

```go
// Create a new import progress model
styles := Styles{
    MenuTitle:    lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205")),
    MenuDesc:     lipgloss.NewStyle().Foreground(lipgloss.Color("241")),
    SuccessStyle: lipgloss.NewStyle().Foreground(lipgloss.Color("46")),
    ErrorStyle:   lipgloss.NewStyle().Foreground(lipgloss.Color("196")),
}

progress := NewImportProgressModel(totalFiles, styles)

// In your BubbleTea Update method
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    switch msg := msg.(type) {
    case ImportProgressMsg:
        var cmd tea.Cmd
        m.progress, cmd = m.progress.Update(msg)
        return m, cmd
    }
    return m, nil
}

// In your BubbleTea View method
func (m Model) View() string {
    return m.progress.View()
}
```

### Sending Progress Updates

```go
// Update current file and progress
progressMsg := ImportProgressMsg{
    CurrentFile:    "wallet.json",
    ProcessedFiles: 3,
    TotalFiles:     10,
    Completed:      false,
    Paused:         false,
}

// Add an error (failed import)
errorMsg := ImportProgressMsg{
    Error: &ImportError{
        File:    "corrupted.json",
        Error:   errors.New("invalid keystore format"),
        Skipped: false, // This is a failure, not a skip
    },
}

// Add a skipped file
skipMsg := ImportProgressMsg{
    Error: &ImportError{
        File:    "skipped.json",
        Error:   errors.New("user chose to skip"),
        Skipped: true, // This is a user skip
    },
}

// Pause for password input
pauseMsg := ImportProgressMsg{
    Paused:      true,
    PauseReason: "Password required for wallet.json",
}

// Resume after password input
resumeMsg := ImportProgressMsg{
    Paused: false,
}

// Mark as completed
completeMsg := ImportProgressMsg{
    Completed: true,
}
```

### Direct API Usage

```go
// Direct progress updates
progress.UpdateProgress("current.json", 5)

// Add errors
progress.AddError("failed.json", errors.New("error"), false) // Failed
progress.AddError("skipped.json", errors.New("skipped"), true) // Skipped

// Pause/Resume
progress.Pause("Waiting for user input")
progress.Resume()

// Complete
progress.Complete()

// Reset for new operation
progress.Reset(newTotalFiles)
```

## API Reference

### Types

```go
type ImportProgressModel struct {
    // Embedded progress.Model from bubbles/progress
    progress.Model
    // ... other fields
}

type ImportProgressMsg struct {
    CurrentFile     string
    ProcessedFiles  int
    TotalFiles      int
    Error           *ImportError
    Completed       bool
    Paused          bool
    PauseReason     string
}

type ImportError struct {
    File    string  // File that caused the error
    Error   error   // The actual error
    Skipped bool    // Whether file was skipped vs failed
}
```

### Constructor

```go
func NewImportProgressModel(totalFiles int, styles Styles) ImportProgressModel
```

### BubbleTea Interface

```go
func (m ImportProgressModel) Init() tea.Cmd
func (m ImportProgressModel) Update(msg tea.Msg) (ImportProgressModel, tea.Cmd)
func (m ImportProgressModel) View() string
```

### Progress Management

```go
func (m *ImportProgressModel) Reset(totalFiles int)
func (m *ImportProgressModel) UpdateProgress(currentFile string, processedFiles int)
func (m *ImportProgressModel) AddError(file string, err error, skipped bool)
func (m *ImportProgressModel) Complete()
```

### State Control

```go
func (m *ImportProgressModel) Pause(reason string)
func (m *ImportProgressModel) Resume()
```

### State Queries

```go
func (m ImportProgressModel) IsCompleted() bool
func (m ImportProgressModel) IsPaused() bool
func (m ImportProgressModel) GetPercentage() float64
func (m ImportProgressModel) GetErrors() []ImportError
func (m ImportProgressModel) GetFailedErrors() []ImportError
func (m ImportProgressModel) GetSkippedErrors() []ImportError
func (m ImportProgressModel) GetSummaryText() string
```

## Visual Design

The component displays the following elements:

1. **Title**: "Import Progress"
2. **Progress Bar**: Animated progress bar with gradient colors
3. **Statistics**: "Progress: X/Y files (Z%)"
4. **Current File**: Shows which file is being processed or paused on
5. **Status Indicator**: 
   - ⏳ Importing... (elapsed: Xs)
   - ⏸ Import paused - reason
   - ✓ Import completed in Xs
6. **Error Summary**: Shows recent errors (up to 3) with categorization
7. **Instructions**: Context-appropriate user instructions

### Status States

- **Active**: Shows current file, elapsed time, animated progress
- **Paused**: Shows pause reason, paused file, pause indicator
- **Completed**: Shows completion time, success/failed/skipped counts
- **Error**: Shows error details categorized as Failed or Skipped

## Integration with Other Components

### Password Popup Integration

```go
// When password is required, pause progress
if passwordRequired {
    progress.Pause("Password required for " + filename)
    // Show password popup
    // After password input, resume
    progress.Resume()
}
```

### Batch Import Service Integration

```go
type BatchImportService struct {
    progress *ImportProgressModel
}

func (s *BatchImportService) ImportKeystores(files []string) error {
    s.progress.Reset(len(files))
    
    for i, file := range files {
        s.progress.UpdateProgress(filepath.Base(file), i)
        
        err := s.importSingleKeystore(file)
        if err != nil {
            if isUserSkip(err) {
                s.progress.AddError(file, err, true) // Skipped
            } else {
                s.progress.AddError(file, err, false) // Failed
            }
        }
        
        s.progress.UpdateProgress(filepath.Base(file), i+1)
    }
    
    s.progress.Complete()
    return nil
}
```

## Testing

### Unit Tests

Run the unit tests:

```bash
go test ./internal/ui/ -run TestImportProgress
```

### Demo Application

Run the interactive demo:

```go
package main

import (
    "log"
    "your-project/internal/ui"
)

func main() {
    if err := ui.RunImportProgressDemo(); err != nil {
        log.Fatal(err)
    }
}
```

Or run the demo test:

```bash
go test ./internal/ui/ -run TestImportProgressDemo
```

### Manual Testing

```go
// Create a test scenario
progress := NewImportProgressModel(3, styles)

// Simulate import workflow
progress.UpdateProgress("file1.json", 0)
progress.UpdateProgress("file1.json", 1)

progress.UpdateProgress("file2.json", 1)
progress.Pause("Password required")
// ... password input ...
progress.Resume()
progress.UpdateProgress("file2.json", 2)

progress.UpdateProgress("file3.json", 2)
progress.AddError("file3.json", errors.New("invalid"), false)
progress.UpdateProgress("file3.json", 3)

progress.Complete()

fmt.Println(progress.GetSummaryText())
```

## Performance Considerations

- **Smooth Animations**: Uses bubbles/progress for 60fps animations
- **Efficient Rendering**: Minimal UI updates for responsive interaction
- **Memory Management**: Bounded error collection (shows last 3 errors)
- **Real-time Updates**: Sub-100ms update response time

## Error Handling

### Error Categories

- **Failed Imports**: Files that encountered processing errors
- **Skipped Files**: Files intentionally skipped by user choice
- **System Errors**: Infrastructure or system-level errors

### Error Display

- Shows up to 3 most recent errors
- Indicates additional errors with "... and X more errors"
- Categorizes errors as "Failed" or "Skipped"
- Displays filename and error message
- Maintains error history for completion summary

## Styling

The component uses the provided `Styles` struct for consistent theming:

```go
type Styles struct {
    MenuTitle    lipgloss.Style  // For the main title
    MenuDesc     lipgloss.Style  // For descriptions and stats
    SuccessStyle lipgloss.Style  // For success messages
    ErrorStyle   lipgloss.Style  // For error messages
}
```

## Future Enhancements

- **File Tree View**: Visual representation of import hierarchy
- **Speed Indicators**: Import rate and estimated completion time
- **Progress History**: Historical progress tracking across sessions
- **Customizable Display**: User-configurable progress display options
- **Sound Notifications**: Audio feedback for completion/errors

## Related Components

- [Password Popup Component](password_popup_README.md)
- [Enhanced File Picker Component](enhanced_file_picker_README.md)
- [Password File Manager](../wallet/password_file_manager.go)

## Support

For issues or questions about the Import Progress Component:

1. Check the unit tests for usage examples
2. Run the demo application for interactive testing
3. Review the integration tests for workflow examples
4. Check the main documentation for architectural context