# Enhanced File Picker Component

The Enhanced File Picker is a BubbleTea component that extends the standard filepicker with multi-selection capabilities, directory selection, and advanced filtering options specifically designed for keystore import workflows.

## Features

### Multi-Selection Support
- **Checkbox Interface**: Visual checkboxes (☐/☑) for each file/directory
- **Space Bar Selection**: Toggle selection of individual items
- **Bulk Operations**: Select all (`Ctrl+A`) and clear all (`Ctrl+C`) functionality
- **Mixed Selection**: Support for selecting both files and directories simultaneously

### Directory Navigation
- **Directory Selection**: Select entire directories for batch processing
- **Recursive Navigation**: Navigate into subdirectories with breadcrumb tracking
- **Navigation Stack**: Maintains cursor position when navigating back
- **Visual Indicators**: Clear distinction between files (📄) and directories (📁)

### File Filtering
- **Extension Filtering**: Filter by file extensions (default: `.json` for keystores)
- **Custom Filters**: Support for custom filter functions
- **Case Insensitive**: Extension matching is case-insensitive
- **Hidden Files**: Optional display of hidden files (disabled by default)

### Keyboard Navigation
- **Arrow Keys**: Navigate up/down through file list
- **Page Navigation**: `PgUp`/`PgDown` for quick scrolling
- **Jump Navigation**: `g` (top) and `G` (bottom) for instant positioning
- **Directory Navigation**: `Enter`/`→` to open, `Esc`/`←` to go back

### Visual Design
- **Responsive Layout**: Adapts to terminal size with viewport management
- **Color Coding**: Different colors for files, directories, and selected items
- **Progress Indicators**: Shows selection count in header
- **Status Information**: File sizes and selection status
- **Styled Interface**: Consistent with application theme

## Usage

### Basic Setup

```go
// Create a new enhanced file picker
picker := NewEnhancedFilePicker()

// Configure for keystore import
picker.SetAllowedTypes([]string{".json"})
picker.MultiSelect = true
picker.DirAllowed = true
picker.FileAllowed = true

// Set height (optional, auto-height is enabled by default)
picker.SetHeight(20)
```

### Custom File Filtering

```go
// Set a custom filter function
picker.SetFileFilter(func(filename string) bool {
    // Only allow JSON files larger than 100 bytes
    if !strings.HasSuffix(strings.ToLower(filename), ".json") {
        return false
    }
    
    // Additional validation logic here
    return true
})
```

### Integration with BubbleTea

```go
type MyModel struct {
    picker EnhancedFilePickerModel
    result *FilePickerResult
}

func (m MyModel) Init() tea.Cmd {
    return m.picker.Init()
}

func (m MyModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    var cmd tea.Cmd
    m.picker, cmd = m.picker.Update(msg)
    
    // Check if selection is complete
    if m.picker.IsConfirmed() || m.picker.IsCancelled() {
        result := m.picker.GetResult()
        m.result = &result
        // Handle the result...
    }
    
    return m, cmd
}

func (m MyModel) View() string {
    return m.picker.View()
}
```

### Processing Results

```go
result := picker.GetResult()

if result.Confirmed {
    // Process selected files
    for _, file := range result.Files {
        fmt.Printf("Selected file: %s\n", file)
    }
    
    // Process selected directory
    if result.Directory != "" {
        fmt.Printf("Selected directory: %s\n", result.Directory)
    }
} else if result.Cancelled {
    fmt.Println("Selection was cancelled")
}
```

## Key Bindings

| Key | Action |
|-----|--------|
| `↑`/`k` | Move cursor up |
| `↓`/`j` | Move cursor down |
| `←`/`h`/`Esc` | Go back to parent directory |
| `→`/`l`/`Enter` | Open directory or select file |
| `Space` | Toggle selection (multi-select mode) |
| `Tab` | Confirm selection |
| `Ctrl+A` | Select all valid files |
| `Ctrl+C` | Clear all selections |
| `Ctrl+Q` | Cancel selection |
| `g` | Go to top |
| `G` | Go to bottom |
| `PgUp` | Page up |
| `PgDown` | Page down |

## Configuration Options

### File Selection
```go
picker.FileAllowed = true        // Allow file selection
picker.DirAllowed = true         // Allow directory selection
picker.MultiSelect = true        // Enable multi-selection
picker.ShowHidden = false        // Hide hidden files
```

### Filtering
```go
picker.SetAllowedTypes([]string{".json", ".txt"})  // File extensions
picker.SetFileFilter(customFilterFunc)             // Custom filter function
```

### Visual Styling
```go
// Customize colors and styles
picker.Styles.Directory = lipgloss.NewStyle().Foreground(lipgloss.Color("99"))
picker.Styles.SelectedFile = lipgloss.NewStyle().Foreground(lipgloss.Color("212")).Bold(true)
```

## Requirements Compliance

This implementation satisfies all requirements from the specification:

### Requirement 2.1 ✅
- **File picker interface**: Provides interactive file picker with BubbleTea integration

### Requirement 2.2 ✅  
- **Directory navigation**: Full directory traversal with breadcrumb support

### Requirement 2.3 ✅
- **Multiple file selection**: Checkbox-based multi-selection with visual indicators

### Requirement 2.4 ✅
- **Directory selection**: Can select entire directories for batch processing

### Requirement 2.5 ✅
- **JSON file filtering**: Default `.json` filter with custom filter support

### Requirement 2.6 ✅
- **Selection validation**: Validates files are valid keystore format (extensible)

## Testing

The component includes comprehensive unit tests covering:

- **Basic functionality**: Creation, configuration, and state management
- **File filtering**: Extension-based and custom filter validation
- **Multi-selection**: Toggle, select all, and clear operations
- **Keyboard navigation**: All navigation and selection key bindings
- **Directory operations**: Navigation, selection, and path management
- **Result handling**: Confirmation, cancellation, and result extraction

Run tests with:
```bash
go test ./internal/ui/ -run "Enhanced" -v
```

## Integration Points

The enhanced file picker is designed to integrate seamlessly with:

1. **Batch Import Service**: Provides file lists for batch keystore processing
2. **Password File Manager**: Selected files can be checked for corresponding `.pwd` files
3. **Progress Bar Component**: File count feeds into progress calculation
4. **Error Handling**: Invalid selections are filtered out before processing

## Performance Considerations

- **Lazy Loading**: Directory contents are loaded on-demand
- **Viewport Management**: Only visible items are rendered for large directories
- **Memory Efficient**: Selection state uses maps for O(1) lookup performance
- **Responsive UI**: Non-blocking operations with proper command handling

## Future Enhancements

Potential improvements for future versions:

- **File Preview**: Show keystore file contents in a preview pane
- **Search Functionality**: Filter files by name or content
- **Sorting Options**: Sort by name, size, date, or type
- **Bookmarks**: Save frequently accessed directories
- **Drag & Drop**: Support for drag-and-drop file selection (if terminal supports it)