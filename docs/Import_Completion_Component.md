# Import Completion Component

The Import Completion Component provides a comprehensive summary and action interface for completed keystore import operations in BLOCO Wallet Manager.

## Overview

The `ImportCompletionModel` displays import results with detailed statistics, error information, and retry functionality. It integrates seamlessly with the enhanced import workflow to provide users with clear feedback and recovery options.

## Features

### Summary Display
- **Success/Failure/Skipped Counts**: Clear breakdown of import results
- **Success Rate**: Percentage calculation with color-coded display
- **Timing Information**: Total elapsed time and per-file averages
- **Visual Status Indicators**: Success (green), partial success (orange), or failure (red)

### Error Management
- **Quick Error Summary**: Overview of failed and skipped files (up to 3 shown)
- **Detailed Error View**: Navigate through individual errors with full details
- **Error Categorization**: Distinguishes between failed imports and user-skipped files
- **Suggested Actions**: Context-aware recommendations based on error types

### Retry Functionality
- **Retry Failed Imports**: Retry only files that failed due to errors
- **Retry Skipped Imports**: Retry files that were skipped by user action
- **Retry All**: Retry both failed and skipped files
- **Retry Specific File**: Retry individual files from the error details view

### Navigation Options
- **Return to Main Menu**: Exit the import workflow
- **Select Different Files**: Start a new import with different file selection

## Usage

### Basic Integration

```go
// Create completion model with import results
summary := batchService.GetImportSummary(results)
completion := NewImportCompletionModel(summary, results, startTime, styles)

// Handle updates in your main update loop
completion, cmd := completion.Update(msg)

// Render the completion view
view := completion.View()
```

### Message Handling

The completion component generates several message types that should be handled by the parent component:

```go
switch msg := msg.(type) {
case RetryImportMsg:
    // Handle retry request based on strategy
    files := completion.GetRetryableFiles(msg.Strategy)
    // Restart import process with these files
    
case RetrySpecificFileMsg:
    // Handle retry of specific file
    // Restart import process with single file
    
case ReturnToMenuMsg:
    // Return to main application menu
    
case SelectDifferentFilesMsg:
    // Return to file selection phase
}
```

### Keyboard Controls

#### Main Completion View
- **↑/↓ or j/k**: Navigate between available actions
- **ENTER**: Execute selected action
- **F**: Retry failed imports (if available)
- **K**: Retry skipped imports (if available)
- **A**: Retry all failed/skipped imports (if available)
- **S**: Select different files
- **E**: View detailed error information (if errors exist)
- **ESC/Q**: Return to main menu

#### Error Details View
- **↑/↓ or j/k**: Navigate between errors
- **R**: Retry the currently displayed file
- **ESC/Q**: Return to main completion view

## Component Structure

### ImportCompletionModel
```go
type ImportCompletionModel struct {
    summary     wallet.ImportSummary  // Import statistics
    results     []wallet.ImportResult // Detailed results
    startTime   time.Time            // Import start time
    elapsedTime time.Duration        // Total elapsed time
    
    // UI state
    selectedAction int               // Currently selected action
    showingErrors  bool             // Whether error details are shown
    errorIndex     int              // Current error being viewed
    
    // Available actions based on results
    availableActions []CompletionActionItem
}
```

### Available Actions
Actions are dynamically generated based on import results:

- **Return to Main Menu**: Always available
- **Select Different Files**: Always available
- **Retry Failed Imports**: Available when there are failed imports
- **Retry Skipped Imports**: Available when there are skipped imports
- **Retry All Failed/Skipped**: Available when there are any failures or skips
- **View Error Details**: Available when there are errors

### Error Suggestions
The component provides context-aware suggestions based on error types:

- **Password Errors**: Verify password, check .pwd files, retry with manual input
- **Format Errors**: Verify KeyStore V3 format, check for corruption
- **Permission Errors**: Check file permissions, ensure file isn't locked
- **Skipped Files**: Retry with manual password input
- **Generic Errors**: General troubleshooting advice

## Integration with Enhanced Import State

The completion component is automatically initialized when the enhanced import state transitions to the `PhaseComplete` phase:

```go
// In EnhancedImportState.setupCompletePhase()
if s.BatchService != nil && len(s.Results) > 0 {
    summary := s.BatchService.GetImportSummary(s.Results)
    completion := NewImportCompletionModel(summary, s.Results, s.startTime, styles)
    s.Completion = &completion
}
```

## Testing

The component includes comprehensive tests covering:

- Action initialization based on different result scenarios
- Keyboard navigation and input handling
- Error details view functionality
- Retry file selection logic
- Text wrapping and display formatting
- Integration with the broader import workflow

### Running Tests

```bash
# Run completion component tests
go test ./internal/ui/ -run "TestImportCompletion" -v

# Run demo tests
go test ./internal/ui/ -run "TestImportCompletionDemo" -v
```

### Demo Application

A demo application is available to showcase the completion component:

```go
demo := NewImportCompletionDemo()
// Use with BubbleTea program for interactive demonstration
```

## Styling

The component uses the application's style system for consistent theming:

- **Success indicators**: Green color scheme
- **Warning indicators**: Orange color scheme  
- **Error indicators**: Red color scheme
- **Navigation highlights**: Blue background with white text
- **Borders and separators**: Rounded borders for error details

## Error Recovery

The completion component supports multiple error recovery strategies:

1. **Automatic Retry**: For transient errors that might resolve on retry
2. **Manual Password Input**: For password-related failures
3. **File Validation**: For format or corruption issues
4. **Permission Resolution**: For access-related problems
5. **Selective Retry**: For mixed success/failure scenarios

## Performance Considerations

- **Memory Efficient**: Processes results without duplicating large data structures
- **Responsive UI**: Non-blocking operations with smooth navigation
- **Scalable Display**: Handles large numbers of errors with pagination
- **Text Wrapping**: Automatic text wrapping for long error messages

## Future Enhancements

Potential improvements for future versions:

- **Export Results**: Save import results to file
- **Detailed Logging**: Enhanced logging for troubleshooting
- **Batch Operations**: Group retry operations for efficiency
- **Custom Actions**: User-defined recovery actions
- **Progress Tracking**: Show progress during retry operations