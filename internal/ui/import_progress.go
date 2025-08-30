package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/progress"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ImportError represents an error that occurred during import
type ImportError struct {
	File    string
	Error   error
	Skipped bool
}

// ImportProgressModel represents the progress bar component for import operations
type ImportProgressModel struct {
	progress.Model
	currentFile    string
	totalFiles     int
	processedFiles int
	errors         []ImportError
	completed      bool
	paused         bool
	pauseReason    string
	startTime      time.Time
	styles         Styles
}

// ImportProgressMsg represents progress updates
type ImportProgressMsg struct {
	CurrentFile    string
	ProcessedFiles int
	TotalFiles     int
	Error          *ImportError
	Completed      bool
	Paused         bool
	PauseReason    string
}

// NewImportProgressModel creates a new import progress model
func NewImportProgressModel(totalFiles int, styles Styles) ImportProgressModel {
	p := progress.New(
		progress.WithDefaultGradient(),
		progress.WithWidth(60),
		progress.WithoutPercentage(),
	)

	return ImportProgressModel{
		Model:          p,
		totalFiles:     totalFiles,
		processedFiles: 0,
		errors:         make([]ImportError, 0),
		completed:      false,
		paused:         false,
		startTime:      time.Now(),
		styles:         styles,
	}
}

// Init initializes the progress model
func (m ImportProgressModel) Init() tea.Cmd {
	return nil
}

// Update handles progress updates
func (m ImportProgressModel) Update(msg tea.Msg) (ImportProgressModel, tea.Cmd) {
	switch msg := msg.(type) {
	case ImportProgressMsg:
		m.currentFile = msg.CurrentFile
		m.processedFiles = msg.ProcessedFiles
		m.totalFiles = msg.TotalFiles
		m.completed = msg.Completed
		m.paused = msg.Paused
		m.pauseReason = msg.PauseReason

		if msg.Error != nil {
			m.errors = append(m.errors, *msg.Error)
		}

		// Update progress percentage
		if m.totalFiles > 0 {
			percentage := float64(m.processedFiles) / float64(m.totalFiles)
			cmd := m.Model.SetPercent(percentage)
			return m, cmd
		}

	case progress.FrameMsg:
		progressModel, cmd := m.Model.Update(msg)
		m.Model = progressModel.(progress.Model)
		return m, cmd
	}

	return m, nil
}

// View renders the progress bar component
func (m ImportProgressModel) View() string {
	if m.totalFiles == 0 {
		return ""
	}

	var sections []string

	// Title section
	title := m.styles.MenuTitle.Render("Import Progress")
	sections = append(sections, title)

	// Progress bar section
	progressBar := m.Model.View()
	sections = append(sections, progressBar)

	// Progress statistics
	percentage := 0.0
	if m.totalFiles > 0 {
		percentage = float64(m.processedFiles) / float64(m.totalFiles) * 100
	}

	stats := fmt.Sprintf("Progress: %d/%d files (%.1f%%)",
		m.processedFiles, m.totalFiles, percentage)
	sections = append(sections, m.styles.MenuDesc.Render(stats))

	// Current file being processed
	if m.currentFile != "" && !m.completed {
		currentFileText := fmt.Sprintf("Processing: %s", m.currentFile)
		if m.paused {
			currentFileText = fmt.Sprintf("Paused on: %s", m.currentFile)
			if m.pauseReason != "" {
				currentFileText += fmt.Sprintf(" (%s)", m.pauseReason)
			}
		}
		sections = append(sections, m.styles.MenuDesc.Render(currentFileText))
	}

	// Status section
	if m.completed {
		elapsed := time.Since(m.startTime)
		successCount := m.processedFiles - len(m.errors)

		statusText := fmt.Sprintf("✓ Import completed in %v", elapsed.Round(time.Second))
		sections = append(sections, m.styles.SuccessStyle.Render(statusText))

		summaryText := fmt.Sprintf("Success: %d, Failed: %d, Skipped: %d",
			successCount, m.getFailedCount(), m.getSkippedCount())
		sections = append(sections, m.styles.MenuDesc.Render(summaryText))
	} else if m.paused {
		statusText := "⏸ Import paused"
		if m.pauseReason != "" {
			statusText += fmt.Sprintf(" - %s", m.pauseReason)
		}
		sections = append(sections, m.styles.MenuDesc.Render(statusText))
	} else {
		elapsed := time.Since(m.startTime)
		statusText := fmt.Sprintf("⏳ Importing... (elapsed: %v)", elapsed.Round(time.Second))
		sections = append(sections, m.styles.MenuDesc.Render(statusText))
	}

	// Error summary (if any errors occurred)
	if len(m.errors) > 0 {
		sections = append(sections, "")
		errorTitle := m.styles.ErrorStyle.Render("Errors:")
		sections = append(sections, errorTitle)

		// Show up to 3 most recent errors
		errorCount := len(m.errors)
		startIdx := 0
		if errorCount > 3 {
			startIdx = errorCount - 3
			sections = append(sections, m.styles.MenuDesc.Render(fmt.Sprintf("... and %d more errors", startIdx)))
		}

		for i := startIdx; i < errorCount; i++ {
			err := m.errors[i]
			errorType := "Failed"
			if err.Skipped {
				errorType = "Skipped"
			}
			errorText := fmt.Sprintf("• %s: %s - %s", errorType, err.File, err.Error.Error())
			sections = append(sections, m.styles.MenuDesc.Render(errorText))
		}
	}

	// Instructions
	if !m.completed {
		sections = append(sections, "")
		instructions := "Press ESC to cancel import"
		sections = append(sections, m.styles.MenuDesc.Render(instructions))
	} else {
		sections = append(sections, "")
		instructions := "Press ENTER to continue or R to retry failed imports"
		sections = append(sections, m.styles.MenuDesc.Render(instructions))
	}

	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

// GetPercentage returns the current progress percentage
func (m ImportProgressModel) GetPercentage() float64 {
	if m.totalFiles == 0 {
		return 0
	}
	return float64(m.processedFiles) / float64(m.totalFiles)
}

// IsCompleted returns whether the import is completed
func (m ImportProgressModel) IsCompleted() bool {
	return m.completed
}

// IsPaused returns whether the import is paused
func (m ImportProgressModel) IsPaused() bool {
	return m.paused
}

// GetErrors returns all errors that occurred during import
func (m ImportProgressModel) GetErrors() []ImportError {
	return m.errors
}

// GetFailedErrors returns only the failed (non-skipped) errors
func (m ImportProgressModel) GetFailedErrors() []ImportError {
	var failed []ImportError
	for _, err := range m.errors {
		if !err.Skipped {
			failed = append(failed, err)
		}
	}
	return failed
}

// GetSkippedErrors returns only the skipped errors
func (m ImportProgressModel) GetSkippedErrors() []ImportError {
	var skipped []ImportError
	for _, err := range m.errors {
		if err.Skipped {
			skipped = append(skipped, err)
		}
	}
	return skipped
}

// getFailedCount returns the count of failed (non-skipped) imports
func (m ImportProgressModel) getFailedCount() int {
	count := 0
	for _, err := range m.errors {
		if !err.Skipped {
			count++
		}
	}
	return count
}

// getSkippedCount returns the count of skipped imports
func (m ImportProgressModel) getSkippedCount() int {
	count := 0
	for _, err := range m.errors {
		if err.Skipped {
			count++
		}
	}
	return count
}

// Reset resets the progress model for a new import operation
func (m *ImportProgressModel) Reset(totalFiles int) {
	m.totalFiles = totalFiles
	m.processedFiles = 0
	m.currentFile = ""
	m.errors = make([]ImportError, 0)
	m.completed = false
	m.paused = false
	m.pauseReason = ""
	m.startTime = time.Now()
	m.Model.SetPercent(0)
}

// Pause pauses the progress display
func (m *ImportProgressModel) Pause(reason string) {
	m.paused = true
	m.pauseReason = reason
}

// Resume resumes the progress display
func (m *ImportProgressModel) Resume() {
	m.paused = false
	m.pauseReason = ""
}

// UpdateProgress updates the progress with new information
func (m *ImportProgressModel) UpdateProgress(currentFile string, processedFiles int) {
	m.currentFile = currentFile
	m.processedFiles = processedFiles
}

// AddError adds an error to the progress tracking
func (m *ImportProgressModel) AddError(file string, err error, skipped bool) {
	importErr := ImportError{
		File:    file,
		Error:   err,
		Skipped: skipped,
	}
	m.errors = append(m.errors, importErr)
}

// Complete marks the import as completed
func (m *ImportProgressModel) Complete() {
	m.completed = true
	m.paused = false
	m.pauseReason = ""
	m.currentFile = ""
}

// GetSummaryText returns a formatted summary of the import results
func (m ImportProgressModel) GetSummaryText() string {
	if !m.completed {
		return ""
	}

	successCount := m.processedFiles - len(m.errors)
	failedCount := m.getFailedCount()
	skippedCount := m.getSkippedCount()
	elapsed := time.Since(m.startTime)

	var parts []string
	parts = append(parts, fmt.Sprintf("Import completed in %v", elapsed.Round(time.Second)))
	parts = append(parts, fmt.Sprintf("Total files: %d", m.totalFiles))
	parts = append(parts, fmt.Sprintf("Successful: %d", successCount))

	if failedCount > 0 {
		parts = append(parts, fmt.Sprintf("Failed: %d", failedCount))
	}

	if skippedCount > 0 {
		parts = append(parts, fmt.Sprintf("Skipped: %d", skippedCount))
	}

	return strings.Join(parts, "\n")
}
