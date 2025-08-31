# Password Popup Component

## Overview

The Password Popup Component is a secure modal interface built with BubbleTea that provides password input functionality for keystore import operations. It features retry mechanisms, error handling, and user-friendly interaction patterns designed specifically for the enhanced keystore import workflow in BLOCO Wallet Manager.

## Architecture

### Core Components

1. **PasswordPopupModel**: Main model struct containing state and UI configuration
2. **PasswordPopupResult**: Result structure for password input outcomes
3. **Secure Input Handling**: Character masking and memory safety features
4. **Error Management**: Retry logic and user feedback systems

### Key Features

- **Secure Password Input**: Character masking with bullet characters (•) for visual security
- **Retry Mechanism**: Configurable maximum attempts with visual feedback
- **Error Display**: Clear error messages with color-coded feedback
- **Skip Functionality**: Option to skip individual files during batch operations
- **Modal Overlay**: Centered popup that overlays the main interface
- **Keyboard Navigation**: Full keyboard control with intuitive key bindings

## Integration

### Dependencies

The component uses the following external libraries:

- `github.com/charmbracelet/bubbletea`: Core TUI framework
- `github.com/charmbracelet/bubbles/textinput`: Secure text input component
- `github.com/charmbracelet/lipgloss`: Styling and layout management

### Usage Pattern

```go
// Initialize password popup
popup := NewPasswordPopupModel("wallet.json", 3) // 3 max retries

// BubbleTea integration
func (m ImportModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    var cmd tea.Cmd
    
    switch m.state {
    case StatePasswordInput:
        m.popup, cmd = m.popup.Update(msg)
        
        if m.popup.IsCompleted() {
            result := m.popup.GetResult()
            // Process result...
        }
    }
    
    return m, cmd
}
```

## Configuration Options

### Security Settings
- `CharLimit`: Maximum password length (default: 256 characters)
- `EchoMode`: Password masking mode (default: EchoPassword)
- `EchoCharacter`: Masking character (default: '•')
- `maxRetries`: Maximum password attempts (configurable)

### Visual Customization
- `width`: Popup width (default: 60 characters)
- `height`: Popup height (default: 12 lines)
- Color schemes for different states (normal, error, warning)

### Interaction Behavior
- Automatic input clearing on errors
- Non-empty password validation
- Graceful cancellation handling
- File skipping capability

## API Reference

### Constructor
```go
func NewPasswordPopupModel(keystoreFile string, maxRetries int) PasswordPopupModel
```

### Core Methods
```go
// BubbleTea interface
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

### Result Structure
```go
type PasswordPopupResult struct {
    Password  string  // Entered password (empty if cancelled)
    Cancelled bool    // True if user cancelled
    Skip      bool    // True if user chose to skip file
}
```

## Key Bindings

| Key Combination | Action | Description |
|----------------|--------|-------------|
| `Enter` | Confirm | Submit password (if non-empty) |
| `Esc` | Cancel | Cancel operation entirely |
| `Ctrl+C` | Cancel | Alternative cancel method |
| `Ctrl+S` | Skip | Skip current file in batch |
| `Backspace` | Delete | Delete character |
| `Ctrl+U` | Clear | Clear entire input |
| `Ctrl+A` | Home | Move to beginning |
| `Ctrl+E` | End | Move to end |

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

### Input Validation
- **Non-empty Validation**: Ensures passwords are not empty
- **Length Limits**: Configurable maximum password length
- **Character Encoding**: Proper UTF-8 handling

## Performance Characteristics

- **Lightweight Rendering**: Minimal UI updates for responsive interaction
- **Memory Efficient**: Clears sensitive data promptly
- **Non-blocking**: Proper command handling for smooth UI experience
- **Responsive Design**: Adapts to terminal size changes

## Integration Points

The Password Popup Component integrates seamlessly with:

1. **Enhanced File Picker**: Processes selected keystore files requiring passwords
2. **Password File Manager**: Fallback when no `.pwd` file is available
3. **Batch Import Service**: Provides password input during keystore processing
4. **Progress Bar Component**: Pauses progress during password input
5. **Error Handling System**: Displays localized error messages

## Workflow Integration

### Batch Import Flow
1. **File Selection**: Enhanced File Picker selects keystore files
2. **Password Detection**: Password File Manager checks for `.pwd` files
3. **Manual Input**: Password Popup appears for files without password files
4. **Retry Logic**: Handles incorrect passwords with retry mechanism
5. **Skip Option**: Allows skipping problematic files
6. **Progress Continuation**: Returns to batch processing after input

### Error Handling Flow
1. **Invalid Password**: Display error message and increment retry counter
2. **Retry Limit**: Show maximum attempts reached message
3. **User Choice**: Allow retry, skip, or cancel operations
4. **State Reset**: Clear input and prepare for next attempt

## Testing Strategy

The component includes comprehensive test coverage:

### Unit Tests
- **State Management**: Creation, initialization, and transitions
- **Input Handling**: Character input, masking, and validation
- **Error Scenarios**: Error display, retry counting, limit enforcement
- **User Interaction**: All key bindings and navigation options
- **Result Generation**: Proper result creation for all scenarios

### Integration Tests
- **BubbleTea Integration**: Message handling and command generation
- **Batch Import Integration**: Interaction with import service
- **Error Recovery**: Proper error handling and recovery mechanisms

### Demo Applications
- **Interactive Demo**: Manual testing interface
- **Automated Demo**: Scripted interaction testing
- **Performance Testing**: Response time and memory usage validation

## Future Enhancements

Planned improvements for future versions:

### Security Enhancements
- **Password Strength Indicator**: Visual feedback on password complexity
- **Timeout Handling**: Automatic timeout for security
- **Audit Logging**: Secure logging of password attempts (without passwords)

### User Experience
- **Remember Choice**: Option to remember skip/cancel choice for batch operations
- **Custom Validation**: Pluggable password validation rules
- **Accessibility**: Enhanced screen reader support
- **Internationalization**: Multi-language error messages

### Advanced Features
- **Biometric Integration**: Support for biometric authentication where available
- **Hardware Token**: Integration with hardware security keys
- **Multi-Factor**: Support for additional authentication factors

## Compliance and Standards

### Security Standards
- **OWASP Guidelines**: Follows secure input handling practices
- **Memory Safety**: Proper cleanup of sensitive data
- **Input Validation**: Comprehensive validation and sanitization

### Accessibility
- **Keyboard Navigation**: Full keyboard accessibility
- **Screen Reader**: Compatible with screen reading software
- **Color Contrast**: Sufficient contrast for visual accessibility

### Internationalization
- **Unicode Support**: Proper UTF-8 character handling
- **Localization Ready**: Prepared for multi-language support
- **Cultural Adaptation**: Adaptable to different cultural contexts

## Related Documentation

- [Enhanced File Picker Component](Enhanced_File_Picker.md)
- [Password File Manager](../internal/wallet/password_file_manager.go)
- [Batch Import Service](../internal/ui/batch_import_service.go)
- [Universal KDF Documentation](Universal_KDF.md)

## Support and Maintenance

### Debugging
- Enable verbose logging for troubleshooting
- Use demo applications for manual testing
- Check integration points for compatibility issues

### Performance Monitoring
- Monitor memory usage during password input
- Track response times for user interactions
- Validate proper cleanup of sensitive data

### Updates and Patches
- Regular security reviews and updates
- Compatibility testing with new BubbleTea versions
- User feedback integration for UX improvements