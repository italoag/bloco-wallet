# Progress Implementation Patterns for BLOCO Wallet Manager

## Overview

This document provides reference patterns for implementing robust progress tracking in TUI applications using Bubble Tea, specifically for the BLOCO Wallet Manager's KeyStoreV3 import functionality.

## Architecture Patterns

### 1. Channel-Based Progress Communication

The core pattern uses buffered channels for reliable progress communication between the business logic and UI layers:

```go
// Enhanced channel configuration
progressChan := make(chan wallet.ImportProgress, 500) // Large buffer for burst handling

// Robust progress sending with extended timeout
func sendProgressUpdate(progress ImportProgress, progressChan chan<- ImportProgress) {
    if progressChan == nil {
        return // Graceful handling of nil channels
    }

    select {
    case progressChan <- progress:
        // Successfully sent progress update
    case <-time.After(500 * time.Millisecond): // Extended timeout
        // Log dropped updates for debugging
        log.Printf("Progress update dropped - channel may be blocked (file: %s, progress: %.1f%%)", 
            progress.CurrentFile, progress.Percentage)
    }
}
```

### 2. Message-Driven UI Updates

The TUI uses a message-driven architecture where progress updates flow through specific message types:

```go
// Progress update message structure
type ImportProgressUpdateMsg struct {
    Progress wallet.ImportProgress
}

// Enhanced message handling with command batching
case ImportProgressUpdateMsg:
    // Update progress state
    m.enhancedImportState.UpdateProgress(msg.Progress)
    
    // Collect commands to execute
    var cmds []tea.Cmd
    
    // Continue listening for more updates
    if m.enhancedImportState.GetCurrentPhase() == PhaseImporting {
        cmds = append(cmds, m.listenForProgressUpdates())
    }
    
    // Handle any pending commands from progress update
    if cmd := m.enhancedImportState.GetPendingCommand(); cmd != nil {
        cmds = append(cmds, cmd)
    }
    
    return m, tea.Batch(cmds...)
```

### 3. Progress Validation Pattern

Implement comprehensive validation to ensure data integrity:

```go
func (s *EnhancedImportState) validateProgressUpdate(progress wallet.ImportProgress) error {
    // Basic bounds checking
    if progress.TotalFiles <= 0 {
        return fmt.Errorf("total files must be positive: %d", progress.TotalFiles)
    }
    
    if progress.ProcessedFiles < 0 || progress.ProcessedFiles > progress.TotalFiles {
        return fmt.Errorf("processed files out of range: %d (max: %d)", 
            progress.ProcessedFiles, progress.TotalFiles)
    }
    
    // Percentage consistency validation
    expectedPercentage := float64(progress.ProcessedFiles) / float64(progress.TotalFiles) * 100
    tolerance := 1.0 // Allow 1% tolerance for floating point precision
    
    if abs(progress.Percentage-expectedPercentage) > tolerance {
        return fmt.Errorf("percentage inconsistent: %.2f vs expected %.2f", 
            progress.Percentage, expectedPercentage)
    }
    
    // Monotonic progress validation
    if s.CurrentProgress.TotalFiles > 0 {
        if progress.ProcessedFiles < s.CurrentProgress.ProcessedFiles && progress.ProcessedFiles != 0 {
            return fmt.Errorf("processed files decreased: %d -> %d", 
                s.CurrentProgress.ProcessedFiles, progress.ProcessedFiles)
        }
    }
    
    return nil
}
```

### 4. Command Chaining Pattern

Handle progress bar updates with proper command chaining:

```go
func (s *EnhancedImportState) UpdateProgress(progress wallet.ImportProgress) {
    s.mu.Lock()
    defer s.mu.Unlock()
    
    // Validate before updating
    if err := s.validateProgressUpdate(progress); err != nil {
        log.Printf("Invalid progress update received: %v", err)
        return
    }
    
    s.CurrentProgress = progress
    
    // Update progress bar component
    if s.ProgressBar != nil {
        progressMsg := ImportProgressMsg{
            CurrentFile:    progress.CurrentFile,
            ProcessedFiles: progress.ProcessedFiles,
            TotalFiles:     progress.TotalFiles,
            Completed:      progress.ProcessedFiles >= progress.TotalFiles,
            Paused:         progress.PendingPassword,
        }
        
        // Store the command for later execution
        updatedProgressBar, cmd := s.ProgressBar.Update(progressMsg)
        s.ProgressBar = &updatedProgressBar
        s.pendingCommand = cmd
    }
}
```

### 5. Listener Pattern with Timeout Handling

Implement robust listening with appropriate timeouts:

```go
func (m *CLIModel) listenForProgressUpdates() tea.Cmd {
    if m.enhancedImportState == nil {
        return nil
    }

    return func() tea.Msg {
        progressChan := m.enhancedImportState.GetProgressChan()

        select {
        case progress, ok := <-progressChan:
            if !ok {
                // Channel closed, no more progress updates
                return nil
            }
            return ImportProgressUpdateMsg{Progress: progress}
        case <-time.After(1 * time.Second): // Extended timeout
            // Continue listening by returning a special message
            return ContinueListeningMsg{}
        }
    }
}
```

## Best Practices

### 1. Channel Management

- **Buffer Size**: Use large buffers (500+) for progress channels to handle burst updates
- **Timeout Handling**: Use extended timeouts (500ms+) for progress sends to avoid drops
- **Graceful Degradation**: Continue operation even when progress updates are dropped

### 2. State Management

- **Thread Safety**: Use proper locking for concurrent access to progress state
- **Validation**: Always validate progress data before updating UI state
- **Command Chaining**: Store and execute commands from component updates

### 3. Error Handling

- **Logging**: Log dropped progress updates and validation errors for debugging
- **Graceful Failure**: Continue import operations even when progress tracking fails
- **User Feedback**: Provide fallback progress indicators when real-time updates fail

### 4. Testing Patterns

```go
func TestProgressChannelCommunication(t *testing.T) {
    // Create buffered channels for testing
    progressChan := make(chan wallet.ImportProgress, 100)
    
    // Collect progress updates in a separate goroutine
    var progressUpdates []wallet.ImportProgress
    var mu sync.Mutex
    
    done := make(chan bool)
    go func() {
        defer close(done)
        for progress := range progressChan {
            mu.Lock()
            progressUpdates = append(progressUpdates, progress)
            mu.Unlock()
        }
    }()
    
    // Send test progress updates
    for i := 0; i <= 10; i++ {
        progress := wallet.ImportProgress{
            ProcessedFiles: i,
            TotalFiles:     10,
            Percentage:     float64(i) / 10.0 * 100,
        }
        progressChan <- progress
    }
    
    close(progressChan)
    <-done
    
    // Verify monotonic progress
    mu.Lock()
    defer mu.Unlock()
    for i := 1; i < len(progressUpdates); i++ {
        assert.GreaterOrEqual(t, progressUpdates[i].ProcessedFiles, 
            progressUpdates[i-1].ProcessedFiles)
    }
}
```

## Common Pitfalls and Solutions

### 1. Channel Blocking

**Problem**: Short timeouts cause progress updates to be dropped
**Solution**: Use extended timeouts (500ms+) and large buffers

### 2. Command Chain Breaks

**Problem**: Progress bar updates don't execute because commands aren't chained
**Solution**: Store pending commands and execute them in the update chain

### 3. Invalid Progress Data

**Problem**: Inconsistent progress data corrupts UI state
**Solution**: Implement comprehensive validation before state updates

### 4. Race Conditions

**Problem**: Concurrent access to progress state causes data races
**Solution**: Use proper mutex locking for all state access

## Performance Considerations

- **Buffer Size**: Large buffers (500+) prevent blocking but use more memory
- **Update Frequency**: Throttle updates to avoid overwhelming the UI (every 10-50ms)
- **Validation Cost**: Keep validation lightweight to avoid slowing import process
- **Memory Management**: Clean up channels and goroutines properly

## Integration with Bubble Tea

The patterns work seamlessly with Bubble Tea's MVU architecture:

1. **Model**: Store progress state in the application model
2. **Update**: Handle progress messages and update state
3. **View**: Render progress information from current state

This ensures consistent, reliable progress tracking that integrates well with Bubble Tea's reactive update cycle.