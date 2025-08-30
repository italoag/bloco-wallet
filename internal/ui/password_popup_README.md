# Password Popup Component

The Password Popup is a BubbleTea component that provides a secure modal interface for collecting keystore passwords during import operations. It features retry mechanisms, error handling, and user-friendly interaction patterns.

## Features

### Secure Password Input
- **Character Masking**: Passwords are displayed as bullet characters (•) for security
- **Input Validation**: Ensures non-empty passwords before confirmation
- **Memory Safety**: Clears password input on errors to prevent exposure
- **Character Limits**: Configurable maximum password length (default: 256 characters)

### Error Handling & Retry Logic
- **Retry Counter**: Tracks failed password attempts with visual feedback
- **Maximum Attempts**: Configurable retry limit to prevent brute force attempts
- **Error Display**: Shows localized error messages for failed attempts
- **Attempt Tracking**: Visual indicator of remaining attempts

### User Interaction
- **Modal Overlay**: Centered popup that overlays the main interface
- **Keyboard Navigation**: Full keyboard control with intuitive key bindings
- **Skip Functionality**: Option to skip individual files during batch operations
- **Cancel Support**: Graceful cancellation with ESC key

### Visual Design
- **Responsive Layout**: Adapts to different terminal sizes
- **Color Coding**: Different colors for errors, warnings, and information
- **Clear Instructions**: On-screen help text for available actions
- **File Context**: Shows the current keystore filename being processed

## Usage

### Basic Setup

```go
// Create a new password popup for a keystore file
popup := NewPasswordPopupModel("wallet.json", 3) // 3 max retries

// Initialize the popup
cmd := popup.Init()
```

### Integration with BubbleTea

```go
type ImportModel struct {
    popup  PasswordPopupModel
    result *PasswordPopupResult
    state  ImportState
}

func (m ImportModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    var cmd tea.Cmd
    
    switch m.state {
    case StatePasswordInput:
        m.popup, cmd = m.popup.Update(msg)
        
        // Check if password input is complete
        if m.popup.IsCompleted() {
            result := m.popup.GetResult()
            m.result = &result
            
            if result.Cancelled {
                // Handle cancellation or skip
                m.state = StateSkipped
            } else {
                // Process the password
                m.state = StateProcessing
            }
        }
    }
    
    return m, cmd
}

func (m ImportModel) View() string {
    switch m.state {
    case StatePasswordInput:
        return m.popup.View()
    default:
        return "Processing..."
    }
}
```

### Handling Results

```go
result := popup.GetResult()

switch {
case result.Cancelled && result.Skip:
    fmt.Println("User chose to skip this file")
case result.Cancelled:
    fmt.Println("User cancelled the operation")
case result.Password != "":
    fmt.Printf("Password provided: %s\n", result.Password)
    // Process keystore with password
}
```

### Error Handling

```go
// Set an error message and increment retry counter
popup.SetError("Invalid password. Please try again.")

// Check if maximum retries exceeded
if popup.HasExceededMaxRetries() {
    fmt.Println("Maximum password attempts reached")
    // Handle max retries scenario
}
```

### Reusing for Multiple Files

```go
// Reset popup for next keystore file
popup.Reset("next-wallet.json")

// Popup is now ready for the next file
cmd := popup.Init()
```

## Key Bindings

| Key | Action |
|-----|--------|
| `Enter` | Confirm password (if non-empty) |
| `Esc` | Cancel operation |
| `Ctrl+C` | Cancel operation |
| `Ctrl+S` | Skip current file |
| `Backspace` | Delete character |
| `Ctrl+U` | Clear entire input |
| `Ctrl+A` | Move to beginning |
| `Ctrl+E` | Move to end |

## Configuration Options

### Password Input Settings
```go
popup := NewPasswordPopupModel("wallet.json", maxRetries)

// Customize input behavior
popup.Model.CharLimit = 512        // Increase character limit
popup.Model.Width = 50             // Adjust input width
popup.Model.EchoCharacter = '*'    // Change masking character
```

### Visual Customization
```go
// Adjust popup dimensions
popup.width = 80
popup.height = 15

// Colors and styling are handled through lipgloss styles
```

## API Reference

### PasswordPopupModel

#### Constructor
```go
func NewPasswordPopupModel(keystoreFile string, maxRetries int) PasswordPopupModel
```

#### Methods
```go
// BubbleTea interface methods
func (m PasswordPopupModel) Init() tea.Cmd
func (m PasswordPopupModel) Update(msg tea.Msg) (PasswordPopupModel, tea.Cmd)
func (m PasswordPopupModel) View() string

// State management
func (m *PasswordPopupModel) SetError(err string)
func (m *PasswordPopupModel) Reset(keystoreFile string)
func (m PasswordPopupModel) GetResult() PasswordPopupResult
func (m PasswordPopupModel) IsCompleted() bool
func (m PasswordPopupModel) HasExceededMaxRetries() bool
```

### PasswordPopupResult

```go
type PasswordPopupResult struct {
    Password  string  // The entered password (empty if cancelled)
    Cancelled bool    // True if user cancelled the operation
    Skip      bool    // True if user chose to skip this file
}
```

## Requirements Compliance

This implementation satisfies all requirements from the specification:

### Requirement 6.1 ✅
- **Modal popup overlay**: Centered modal interface that overlays main UI

### Requirement 6.2 ✅  
- **Secure password input**: Character masking with configurable echo character

### Requirement 6.3 ✅
- **Error message display**: Clear error messages with color coding

### Requirement 6.4 ✅
- **Retry mechanism**: Configurable retry limit with attempt tracking

### Requirement 6.5 ✅
- **Cancel/skip functionality**: ESC to cancel, Ctrl+S to skip files

### Requirement 6.6 ✅
- **Current filename display**: Shows which keystore file is being processed

## Security Considerations

### Password Handling
- **No Persistence**: Passwords are never stored or logged
- **Memory Clearing**: Input is cleared on errors and completion
- **Character Masking**: Visual protection against shoulder surfing
- **Retry Limits**: Prevents brute force attempts

### Error Messages
- **No Sensitive Data**: Error messages don't expose keystore contents
- **Generic Failures**: Consistent error format regardless of failure type
- **Localization Ready**: Error messages support internationalization

## Testing

The component includes comprehensive unit tests covering:

- **Basic functionality**: Creation, initialization, and state management
- **Password input**: Character input, masking, and validation
- **Error handling**: Error display, retry counting, and limit enforcement
- **User interaction**: All key bindings and navigation options
- **State transitions**: Completion, cancellation, and reset operations
- **Result handling**: Proper result generation for all scenarios

Run tests with:
```bash
go test ./internal/ui/ -run "PasswordPopup" -v
```

## Integration Points

The password popup is designed to integrate seamlessly with:

1. **Batch Import Service**: Provides password input during keystore processing
2. **Password File Manager**: Fallback when no `.pwd` file is available
3. **Progress Bar Component**: Pauses progress during password input
4. **Error Handling System**: Displays localized error messages
5. **Enhanced File Picker**: Works with selected keystore files

## Performance Considerations

- **Lightweight Rendering**: Minimal UI updates for responsive interaction
- **Memory Efficient**: Clears sensitive data promptly
- **Non-blocking**: Proper command handling for smooth UI experience
- **Responsive Design**: Adapts to terminal size changes

## Future Enhancements

Potential improvements for future versions:

- **Password Strength Indicator**: Visual feedback on password complexity
- **Remember Choice**: Option to remember skip/cancel choice for batch operations
- **Timeout Handling**: Automatic timeout for security
- **Accessibility**: Enhanced screen reader support
- **Custom Validation**: Pluggable password validation rules