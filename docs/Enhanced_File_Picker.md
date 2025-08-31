# Enhanced File Picker Component

## Overview

The Enhanced File Picker is a sophisticated BubbleTea component that extends the standard file picker functionality with multi-selection capabilities, directory selection, and advanced filtering options. It's specifically designed for the enhanced keystore import workflow in BLOCO Wallet Manager.

## Architecture

### Core Components

1. **EnhancedFilePickerModel**: Main model struct containing state and configuration
2. **EnhancedFilePickerKeyMap**: Keyboard binding definitions
3. **EnhancedFilePickerStyles**: Visual styling configuration
4. **FilePickerResult**: Result structure for selection outcomes

### Key Features

- **Multi-Selection**: Checkbox-based interface for selecting multiple files
- **Directory Navigation**: Full directory traversal with navigation stack
- **File Filtering**: Extension-based and custom filter support
- **Keyboard Navigation**: Comprehensive keyboard shortcuts
- **Responsive UI**: Viewport management for large directories
- **Visual Feedback**: Clear indicators for selection state and file types

## Integration

### Dependencies

The component uses the following external libraries:

- `github.com/charmbracelet/bubbletea`: Core TUI framework
- `github.com/charmbracelet/bubbles/key`: Keyboard binding management
- `github.com/charmbracelet/lipgloss`: Styling and layout
- `github.com/dustin/go-humanize`: Human-readable file sizes

### Usage Pattern

```go
// Initialize
picker := NewEnhancedFilePicker()
picker.SetAllowedTypes([]string{".json"})

// BubbleTea integration
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    var cmd tea.Cmd
    m.picker, cmd = m.picker.Update(msg)
    
    if m.picker.IsConfirmed() {
        result := m.picker.GetResult()
        // Process selected files...
    }
    
    return m, cmd
}
```

## Configuration Options

### File Selection
- `MultiSelect`: Enable/disable multi-file selection
- `FileAllowed`: Allow individual file selection
- `DirAllowed`: Allow directory selection
- `ShowHidden`: Show/hide hidden files

### Filtering
- `AllowedTypes`: File extension whitelist
- `FileFilter`: Custom filter function

### Visual
- `Styles`: Comprehensive styling configuration
- `height`: Component height (auto-sizing available)

## Performance Characteristics

- **Memory Efficient**: Uses maps for O(1) selection lookup
- **Responsive**: Viewport rendering for large directories
- **Non-blocking**: Asynchronous directory reading
- **Scalable**: Handles directories with thousands of files

## Testing

The component includes comprehensive test coverage:

- Unit tests for all public methods
- Integration tests with BubbleTea
- Demo application for manual testing
- Performance tests for large directories

## Future Enhancements

Planned improvements include:

- File preview functionality
- Search and filtering capabilities
- Sorting options
- Bookmark support
- Enhanced accessibility features

## Related Components

The Enhanced File Picker integrates with:

- **Password File Manager**: Automatic .pwd file detection
- **Batch Import Service**: Provides file lists for processing
- **Progress Bar Component**: File count for progress tracking
- **Error Handling System**: Validation and error reporting