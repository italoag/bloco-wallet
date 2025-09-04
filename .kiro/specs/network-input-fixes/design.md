# Design Document

## Overview

This design addresses critical issues with the BLOCO Wallet Manager's network addition functionality, focusing on input handling problems on ARM64 systems, error message improvements, and proper logging configuration. The solution involves fixing terminal input compatibility, enhancing error handling with meaningful messages, and redirecting log output to files instead of the terminal interface.

## Architecture

The fix involves modifications to three main components:

1. **Terminal Input System**: Enhance the Bubble Tea text input handling to work consistently across different processor architectures
2. **Error Handling System**: Improve error message generation and display in the network addition workflow
3. **Logging System**: Redirect all log output to files and handle logger sync errors gracefully

## Components and Interfaces

### 1. Enhanced Input Handler

**Location**: `internal/ui/add_network.go`

The current text input system uses Bubble Tea's `textinput.Model` but may have compatibility issues with ARM64 systems. The design will:

- Add explicit input validation and character handling
- Implement fallback input mechanisms for ARM64 compatibility
- Add debug logging for input events (to log files only)
- Ensure consistent focus management across architectures

**Key Changes**:
- Enhanced `Update()` method with architecture-specific input handling
- Improved focus management in `updateFocus()` method
- Better error handling in input validation

### 2. Improved Error Messaging System

**Location**: `internal/ui/add_network.go`, `internal/blockchain/chainlist.go`

The current error handling shows generic messages like `<no value>: searching network...`. The design will:

- Replace placeholder error messages with specific, actionable error descriptions
- Add context-aware error messages based on the operation being performed
- Implement proper error propagation from blockchain service to UI
- Add localized error messages for network operations

**Key Changes**:
- Enhanced error message generation in `fetchChainInfoCmd()`
- Improved error handling in `searchNetworks()` method
- Better error display in the `View()` method
- Context-specific error messages for different failure scenarios

### 3. Logging Configuration System

**Location**: `pkg/logger/logger.go`, `cmd/blocowallet/main.go`

The current logging system outputs to stderr, which appears in the terminal. The design will:

- Configure logger to write to dedicated log files
- Handle logger sync errors gracefully without displaying them to users
- Separate application logs from font loading and initialization messages
- Implement log rotation and proper file management

**Key Changes**:
- Enhanced `NewLogger()` function with file output configuration
- Improved error handling in main application initialization
- Graceful handling of logger sync errors
- Separate log files for different components (app, fonts, network operations)

## Data Models

### Enhanced Error Types

```go
// NetworkOperationError represents errors during network operations
type NetworkOperationError struct {
    Operation string // "search", "validate", "add"
    Message   string
    Cause     error
}

// InputValidationError represents input validation failures
type InputValidationError struct {
    Field   string
    Value   string
    Reason  string
}
```

### Logging Configuration

```go
// LoggingConfig represents logging configuration
type LoggingConfig struct {
    LogDir      string
    LogLevel    string
    MaxFileSize int64
    MaxBackups  int
    MaxAge      int
}
```

## Error Handling

### Network Operation Errors

1. **Search Failures**: When network search fails, display specific error about API connectivity or search terms
2. **Validation Failures**: When RPC validation fails, show clear message about endpoint accessibility
3. **Selection Failures**: When network selection fails, provide guidance on valid selections
4. **Input Failures**: When input is invalid, show field-specific validation messages

### Input Handling Errors

1. **Architecture Compatibility**: Detect ARM64 systems and use appropriate input handling
2. **Focus Management**: Ensure proper focus transitions between input fields
3. **Character Encoding**: Handle special characters and Unicode input properly
4. **Keyboard Events**: Process keyboard events consistently across platforms

### Logging Errors

1. **File Creation**: Handle log file creation failures gracefully
2. **Permission Issues**: Manage file permission problems without crashing
3. **Disk Space**: Handle disk space issues during logging
4. **Sync Failures**: Manage logger sync errors without user impact

## Testing Strategy

### Unit Tests

1. **Input Handler Tests**: Test input handling on different architectures (mocked)
2. **Error Message Tests**: Verify error message generation and localization
3. **Logging Tests**: Test log file creation and error handling
4. **Network Operation Tests**: Test network search and validation error scenarios

### Integration Tests

1. **End-to-End Network Addition**: Test complete network addition workflow
2. **Cross-Platform Compatibility**: Test on both ARM64 and Intel systems
3. **Error Recovery**: Test error scenarios and recovery mechanisms
4. **Logging Integration**: Test log file generation during operations

### Manual Testing

1. **ARM64 MacBook Testing**: Verify input functionality on ARM64 systems
2. **Intel MacBook Testing**: Ensure no regression on Intel systems
3. **Network Connectivity Testing**: Test with various network conditions
4. **Log File Verification**: Verify logs are written to files, not terminal

## Implementation Details

### Input Handling Enhancement

The input handling will be enhanced to detect the system architecture and apply appropriate input processing:

```go
// Enhanced input processing with architecture detection
func (c *AddNetworkComponent) processInput(msg tea.KeyMsg) tea.Cmd {
    // Detect system architecture and apply appropriate handling
    if runtime.GOARCH == "arm64" {
        return c.processInputARM64(msg)
    }
    return c.processInputDefault(msg)
}
```

### Error Message Improvement

Error messages will be enhanced with specific context and actionable guidance:

```go
// Enhanced error message generation
func (c *AddNetworkComponent) generateErrorMessage(err error, operation string) string {
    switch operation {
    case "search":
        return fmt.Sprintf("Network search failed: %s. Please check your internet connection and try again.", err.Error())
    case "validate":
        return fmt.Sprintf("RPC validation failed: %s. Please verify the endpoint URL is correct.", err.Error())
    case "select":
        return fmt.Sprintf("Network selection failed: %s. Please try selecting a different network.", err.Error())
    default:
        return fmt.Sprintf("Operation failed: %s", err.Error())
    }
}
```

### Logging Configuration

The logging system will be configured to write to files with proper error handling:

```go
// Enhanced logger configuration with file output
func NewFileLogger(config LoggingConfig) (Logger, error) {
    // Configure file output with rotation
    cfg := zap.NewProductionConfig()
    cfg.OutputPaths = []string{filepath.Join(config.LogDir, "app.log")}
    cfg.ErrorOutputPaths = []string{filepath.Join(config.LogDir, "error.log")}
    
    // Add file rotation configuration
    // Handle file creation errors gracefully
    
    return &zapLogger{logger: logger}, nil
}
```

## Security Considerations

1. **Log File Permissions**: Ensure log files have appropriate permissions (0644)
2. **Sensitive Data**: Avoid logging sensitive information like private keys or passwords
3. **File Path Validation**: Validate log file paths to prevent directory traversal
4. **Error Message Sanitization**: Ensure error messages don't leak sensitive system information

## Performance Considerations

1. **Input Responsiveness**: Ensure input handling doesn't block the UI thread
2. **Log File Size**: Implement log rotation to prevent excessive disk usage
3. **Network Timeouts**: Use appropriate timeouts for network operations
4. **Memory Usage**: Avoid memory leaks in error handling and logging

## Compatibility Requirements

1. **macOS ARM64**: Full compatibility with Apple Silicon Macs
2. **macOS Intel**: Maintain compatibility with Intel-based Macs
3. **Go Version**: Compatible with Go 1.24.3
4. **Terminal Emulators**: Work with various terminal applications (Terminal.app, iTerm2, etc.)

## Monitoring and Observability

1. **Log File Monitoring**: Enable monitoring of log files for debugging
2. **Error Metrics**: Track error rates for different operations
3. **Performance Metrics**: Monitor input response times and network operation latency
4. **System Health**: Monitor log file sizes and disk usage