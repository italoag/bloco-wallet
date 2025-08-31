package ui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"blocowallet/internal/wallet"
)

// CompletionAction represents actions available in the completion phase
type CompletionAction int

const (
	CompletionActionNone CompletionAction = iota
	CompletionActionReturnToMenu
	CompletionActionRetryFailed
	CompletionActionRetrySkipped
	CompletionActionRetryAll
	CompletionActionViewErrors
	CompletionActionSelectDifferentFiles
)

// ImportCompletionModel represents the completion phase UI component
type ImportCompletionModel struct {
	summary     wallet.ImportSummary
	results     []wallet.ImportResult
	startTime   time.Time
	elapsedTime time.Duration
	styles      Styles

	// UI state
	selectedAction int
	showingErrors  bool
	errorIndex     int
	maxErrorIndex  int

	// Available actions based on results
	availableActions []CompletionActionItem
}

// CompletionActionItem represents an available action in the completion phase
type CompletionActionItem struct {
	Action      CompletionAction
	Label       string
	Description string
	Key         string
	Enabled     bool
}

// NewImportCompletionModel creates a new import completion model
func NewImportCompletionModel(summary wallet.ImportSummary, results []wallet.ImportResult, startTime time.Time, styles Styles) ImportCompletionModel {
	elapsedTime := time.Since(startTime)

	model := ImportCompletionModel{
		summary:        summary,
		results:        results,
		startTime:      startTime,
		elapsedTime:    elapsedTime,
		styles:         styles,
		selectedAction: 0,
		showingErrors:  false,
		errorIndex:     0,
	}

	// Initialize available actions based on results
	model.initializeActions()

	return model
}

// initializeActions sets up the available actions based on import results
func (m *ImportCompletionModel) initializeActions() {
	m.availableActions = []CompletionActionItem{}

	// Always available: Return to menu
	m.availableActions = append(m.availableActions, CompletionActionItem{
		Action:      CompletionActionReturnToMenu,
		Label:       "Return to Main Menu",
		Description: "Go back to the main wallet management menu",
		Key:         "ENTER",
		Enabled:     true,
	})

	// Always available: Select different files
	m.availableActions = append(m.availableActions, CompletionActionItem{
		Action:      CompletionActionSelectDifferentFiles,
		Label:       "Select Different Files",
		Description: "Choose different keystore files to import",
		Key:         "S",
		Enabled:     true,
	})

	// Retry failed imports (only if there are failed imports)
	if m.summary.FailedImports > 0 {
		m.availableActions = append(m.availableActions, CompletionActionItem{
			Action:      CompletionActionRetryFailed,
			Label:       fmt.Sprintf("Retry Failed Imports (%d)", m.summary.FailedImports),
			Description: "Retry importing files that failed due to errors",
			Key:         "F",
			Enabled:     true,
		})
	}

	// Retry skipped imports (only if there are skipped imports)
	if m.summary.SkippedImports > 0 {
		m.availableActions = append(m.availableActions, CompletionActionItem{
			Action:      CompletionActionRetrySkipped,
			Label:       fmt.Sprintf("Retry Skipped Imports (%d)", m.summary.SkippedImports),
			Description: "Retry importing files that were skipped (password input cancelled)",
			Key:         "K",
			Enabled:     true,
		})
	}

	// Retry all failed and skipped (only if there are any failures or skips)
	if m.summary.FailedImports > 0 || m.summary.SkippedImports > 0 {
		totalRetryable := m.summary.FailedImports + m.summary.SkippedImports
		m.availableActions = append(m.availableActions, CompletionActionItem{
			Action:      CompletionActionRetryAll,
			Label:       fmt.Sprintf("Retry All Failed/Skipped (%d)", totalRetryable),
			Description: "Retry all files that failed or were skipped",
			Key:         "A",
			Enabled:     true,
		})
	}

	// View error details (only if there are errors)
	if len(m.summary.Errors) > 0 {
		m.availableActions = append(m.availableActions, CompletionActionItem{
			Action:      CompletionActionViewErrors,
			Label:       fmt.Sprintf("View Error Details (%d)", len(m.summary.Errors)),
			Description: "View detailed information about errors that occurred",
			Key:         "E",
			Enabled:     true,
		})
		m.maxErrorIndex = len(m.summary.Errors) - 1
	}
}

// Init initializes the completion model
func (m ImportCompletionModel) Init() tea.Cmd {
	return nil
}

// Update handles completion model updates
func (m ImportCompletionModel) Update(msg tea.Msg) (ImportCompletionModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return (&m).handleKeyPress(msg)
	}

	return m, nil
}

// handleKeyPress handles keyboard input in the completion phase
func (m *ImportCompletionModel) handleKeyPress(msg tea.KeyMsg) (ImportCompletionModel, tea.Cmd) {
	if m.showingErrors {
		return m.handleErrorViewKeyPress(msg)
	}

	switch msg.String() {
	case "up":
		if m.selectedAction > 0 {
			m.selectedAction--
		}

	case "down":
		if m.selectedAction < len(m.availableActions)-1 {
			m.selectedAction++
		}

	case "j":
		if m.selectedAction < len(m.availableActions)-1 {
			m.selectedAction++
		}

	case "k":
		// Check if this is for navigation or retry skipped action
		if m.hasActionWithKey("K") {
			return *m, m.executeActionByKey("K")
		}
		// Otherwise, use for navigation
		if m.selectedAction > 0 {
			m.selectedAction--
		}

	case "enter":
		if m.selectedAction < len(m.availableActions) {
			action := m.availableActions[m.selectedAction]
			return *m, m.executeAction(action.Action)
		}

	case "f", "F":
		return *m, m.executeActionByKey("F")

	case "s", "S":
		return *m, m.executeActionByKey("S")

	case "a", "A":
		return *m, m.executeActionByKey("A")

	case "e", "E":
		if m.hasActionWithKey("E") {
			return *m, m.executeActionByKey("E")
		}

	case "esc", "q":
		// ESC or Q returns to menu
		return *m, m.executeAction(CompletionActionReturnToMenu)
	}

	return *m, nil
}

// handleErrorViewKeyPress handles keyboard input when viewing error details
func (m *ImportCompletionModel) handleErrorViewKeyPress(msg tea.KeyMsg) (ImportCompletionModel, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.errorIndex > 0 {
			m.errorIndex--
		}

	case "down", "j":
		if m.errorIndex < m.maxErrorIndex {
			m.errorIndex++
		}

	case "esc", "q":
		m.showingErrors = false
		m.errorIndex = 0

	case "r", "R":
		// Retry this specific file
		if m.errorIndex < len(m.summary.Errors) {
			errorItem := m.summary.Errors[m.errorIndex]
			return *m, func() tea.Msg {
				return RetrySpecificFileMsg{File: errorItem.File}
			}
		}
	}

	return *m, nil
}

// executeActionByKey executes an action based on its key binding
func (m *ImportCompletionModel) executeActionByKey(key string) tea.Cmd {
	for _, action := range m.availableActions {
		if action.Key == key && action.Enabled {
			return m.executeAction(action.Action)
		}
	}
	return nil
}

// executeAction executes the specified completion action
func (m *ImportCompletionModel) executeAction(action CompletionAction) tea.Cmd {
	switch action {
	case CompletionActionReturnToMenu:
		return func() tea.Msg {
			return ReturnToMenuMsg{}
		}

	case CompletionActionRetryFailed:
		return func() tea.Msg {
			return RetryImportMsg{Strategy: "retry_failed"}
		}

	case CompletionActionRetrySkipped:
		return func() tea.Msg {
			return RetryImportMsg{Strategy: "retry_skipped"}
		}

	case CompletionActionRetryAll:
		return func() tea.Msg {
			return RetryImportMsg{Strategy: "retry_all"}
		}

	case CompletionActionViewErrors:
		m.showingErrors = true
		m.errorIndex = 0
		return nil

	case CompletionActionSelectDifferentFiles:
		return func() tea.Msg {
			return SelectDifferentFilesMsg{}
		}

	default:
		return nil
	}
}

// View renders the completion phase UI
func (m ImportCompletionModel) View() string {
	if m.showingErrors {
		return m.renderErrorDetailsView()
	}

	return m.renderCompletionSummaryView()
}

// renderCompletionSummaryView renders the main completion summary
func (m ImportCompletionModel) renderCompletionSummaryView() string {
	var sections []string

	// Title with completion status
	title := m.renderCompletionTitle()
	sections = append(sections, title)

	// Summary statistics
	stats := m.renderSummaryStats()
	sections = append(sections, stats)

	// Elapsed time
	timeInfo := m.renderTimeInfo()
	sections = append(sections, timeInfo)

	// Quick error summary if there are errors
	if len(m.summary.Errors) > 0 {
		errorSummary := m.renderQuickErrorSummary()
		sections = append(sections, errorSummary)
	}

	// Available actions
	actions := m.renderAvailableActions()
	sections = append(sections, actions)

	// Instructions
	instructions := m.renderInstructions()
	sections = append(sections, instructions)

	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

// renderCompletionTitle renders the completion title with appropriate styling
func (m ImportCompletionModel) renderCompletionTitle() string {
	var title string
	var style lipgloss.Style

	if m.summary.FailedImports == 0 && m.summary.SkippedImports == 0 {
		// Complete success
		title = "✓ Import Completed Successfully"
		style = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("70")) // Green
	} else if m.summary.SuccessfulImports > 0 {
		// Partial success
		title = "⚠ Import Completed with Issues"
		style = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214")) // Orange
	} else {
		// Complete failure
		title = "✗ Import Failed"
		style = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("196")) // Red
	}

	return style.Render(title)
}

// renderSummaryStats renders the summary statistics
func (m ImportCompletionModel) renderSummaryStats() string {
	var sections []string

	// Main statistics line
	stats := fmt.Sprintf("Total: %d | Success: %d | Failed: %d | Skipped: %d",
		m.summary.TotalFiles,
		m.summary.SuccessfulImports,
		m.summary.FailedImports,
		m.summary.SkippedImports)

	sections = append(sections, stats)

	// Success rate if there were any files processed
	if m.summary.TotalFiles > 0 {
		successRate := float64(m.summary.SuccessfulImports) / float64(m.summary.TotalFiles) * 100
		rateText := fmt.Sprintf("Success Rate: %.1f%%", successRate)

		var rateStyle lipgloss.Style
		if successRate >= 90 {
			rateStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("70")) // Green
		} else if successRate >= 70 {
			rateStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("214")) // Orange
		} else {
			rateStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("196")) // Red
		}

		sections = append(sections, rateStyle.Render(rateText))
	}

	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

// renderTimeInfo renders timing information
func (m ImportCompletionModel) renderTimeInfo() string {
	timeText := fmt.Sprintf("Completed in: %v", m.elapsedTime.Round(time.Second))

	// Add performance info if we have multiple files
	if m.summary.TotalFiles > 1 {
		avgTime := m.elapsedTime / time.Duration(m.summary.TotalFiles)
		timeText += fmt.Sprintf(" (avg: %v per file)", avgTime.Round(time.Millisecond))
	}

	return timeText
}

// renderQuickErrorSummary renders a quick summary of errors
func (m ImportCompletionModel) renderQuickErrorSummary() string {
	var sections []string

	sections = append(sections, "")

	errorTitle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("196")).Render("Issues Encountered:")
	sections = append(sections, errorTitle)

	// Group errors by type
	failedFiles := []string{}
	skippedFiles := []string{}

	for _, err := range m.summary.Errors {
		if err.Skipped {
			skippedFiles = append(skippedFiles, err.File)
		} else {
			failedFiles = append(failedFiles, err.File)
		}
	}

	// Show failed files (up to 3)
	if len(failedFiles) > 0 {
		sections = append(sections, lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Render("Failed:"))
		for i, file := range failedFiles {
			if i >= 3 {
				sections = append(sections, fmt.Sprintf("  ... and %d more", len(failedFiles)-3))
				break
			}
			sections = append(sections, fmt.Sprintf("  • %s", file))
		}
	}

	// Show skipped files (up to 3)
	if len(skippedFiles) > 0 {
		sections = append(sections, lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Render("Skipped:"))
		for i, file := range skippedFiles {
			if i >= 3 {
				sections = append(sections, fmt.Sprintf("  ... and %d more", len(skippedFiles)-3))
				break
			}
			sections = append(sections, fmt.Sprintf("  • %s", file))
		}
	}

	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

// renderAvailableActions renders the list of available actions
func (m ImportCompletionModel) renderAvailableActions() string {
	if len(m.availableActions) == 0 {
		return ""
	}

	var sections []string
	sections = append(sections, "")

	actionsTitle := lipgloss.NewStyle().Bold(true).Render("Available Actions:")
	sections = append(sections, actionsTitle)

	for i, action := range m.availableActions {
		if !action.Enabled {
			continue
		}

		var style lipgloss.Style
		if i == m.selectedAction {
			// Highlight selected action
			style = lipgloss.NewStyle().
				Background(lipgloss.Color("62")).
				Foreground(lipgloss.Color("230")).
				Bold(true)
		} else {
			style = lipgloss.NewStyle()
		}

		actionText := fmt.Sprintf("  [%s] %s", action.Key, action.Label)
		sections = append(sections, style.Render(actionText))

		// Add description for selected action
		if i == m.selectedAction {
			descStyle := lipgloss.NewStyle().
				Foreground(lipgloss.Color("244")).
				Italic(true)
			sections = append(sections, descStyle.Render(fmt.Sprintf("      %s", action.Description)))
		}
	}

	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

// renderInstructions renders user instructions
func (m ImportCompletionModel) renderInstructions() string {
	var sections []string
	sections = append(sections, "")

	instructions := []string{
		"Use ↑/↓ or j/k to navigate actions",
		"Press ENTER to execute selected action",
		"Press ESC or Q to return to main menu",
	}

	// Add specific key instructions if actions are available
	if m.hasActionWithKey("E") {
		instructions = append(instructions, "Press E to view detailed error information")
	}

	instructionStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	for _, instruction := range instructions {
		sections = append(sections, instructionStyle.Render(instruction))
	}

	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

// renderErrorDetailsView renders the detailed error view
func (m ImportCompletionModel) renderErrorDetailsView() string {
	if len(m.summary.Errors) == 0 {
		return "No errors to display"
	}

	var sections []string

	// Title
	title := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("196")).Render("Error Details")
	sections = append(sections, title)

	// Error navigation info
	navInfo := fmt.Sprintf("Error %d of %d", m.errorIndex+1, len(m.summary.Errors))
	sections = append(sections, navInfo)

	// Current error details
	if m.errorIndex < len(m.summary.Errors) {
		errorDetails := m.renderSingleErrorDetails(m.summary.Errors[m.errorIndex])
		sections = append(sections, errorDetails)
	}

	// Navigation instructions
	sections = append(sections, "")
	instructions := []string{
		"Use ↑/↓ or j/k to navigate between errors",
		"Press R to retry this specific file",
		"Press ESC or Q to return to summary",
	}

	instructionStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	for _, instruction := range instructions {
		sections = append(sections, instructionStyle.Render(instruction))
	}

	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

// renderSingleErrorDetails renders details for a single error
func (m ImportCompletionModel) renderSingleErrorDetails(err wallet.ImportError) string {
	var sections []string

	// File information
	fileStyle := lipgloss.NewStyle().Bold(true)
	sections = append(sections, fileStyle.Render(fmt.Sprintf("File: %s", err.File)))

	// Error type
	errorType := "Failed"
	typeStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	if err.Skipped {
		errorType = "Skipped"
		typeStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	}
	sections = append(sections, typeStyle.Render(fmt.Sprintf("Status: %s", errorType)))

	// Error message
	sections = append(sections, "")
	sections = append(sections, "Error Details:")

	errorMsg := err.Error.Error()
	// Wrap long error messages
	if len(errorMsg) > 80 {
		errorMsg = m.wrapText(errorMsg, 80)
	}

	errorStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("196")).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("196")).
		Padding(1)

	sections = append(sections, errorStyle.Render(errorMsg))

	// Suggested actions
	suggestions := m.getSuggestedActions(err)
	if len(suggestions) > 0 {
		sections = append(sections, "")
		sections = append(sections, "Suggested Actions:")
		for _, suggestion := range suggestions {
			sections = append(sections, fmt.Sprintf("  • %s", suggestion))
		}
	}

	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

// getSuggestedActions returns suggested actions based on the error type
func (m ImportCompletionModel) getSuggestedActions(err wallet.ImportError) []string {
	var suggestions []string

	errorMsg := strings.ToLower(err.Error.Error())

	if err.Skipped {
		suggestions = append(suggestions, "File was skipped due to user cancellation")
		suggestions = append(suggestions, "Retry with manual password input")
	} else if strings.Contains(errorMsg, "password") || strings.Contains(errorMsg, "decrypt") {
		suggestions = append(suggestions, "Verify the password is correct")
		suggestions = append(suggestions, "Check if a .pwd file exists with the correct password")
		suggestions = append(suggestions, "Retry with manual password input")
	} else if strings.Contains(errorMsg, "format") || strings.Contains(errorMsg, "invalid") {
		suggestions = append(suggestions, "Verify the file is a valid KeyStore V3 format")
		suggestions = append(suggestions, "Check if the file is corrupted")
	} else if strings.Contains(errorMsg, "permission") || strings.Contains(errorMsg, "access") {
		suggestions = append(suggestions, "Check file permissions")
		suggestions = append(suggestions, "Ensure the file is not locked by another process")
	} else {
		suggestions = append(suggestions, "Check the error details above")
		suggestions = append(suggestions, "Verify the file is accessible and valid")
	}

	return suggestions
}

// wrapText wraps text to the specified width
func (m ImportCompletionModel) wrapText(text string, width int) string {
	if len(text) <= width {
		return text
	}

	var lines []string
	words := strings.Fields(text)
	currentLine := ""

	for _, word := range words {
		if len(currentLine)+len(word)+1 <= width {
			if currentLine == "" {
				currentLine = word
			} else {
				currentLine += " " + word
			}
		} else {
			if currentLine != "" {
				lines = append(lines, currentLine)
			}
			currentLine = word
		}
	}

	if currentLine != "" {
		lines = append(lines, currentLine)
	}

	return strings.Join(lines, "\n")
}

// hasActionWithKey checks if there's an enabled action with the specified key
func (m ImportCompletionModel) hasActionWithKey(key string) bool {
	for _, action := range m.availableActions {
		if action.Key == key && action.Enabled {
			return true
		}
	}
	return false
}

// GetSummary returns the import summary
func (m ImportCompletionModel) GetSummary() wallet.ImportSummary {
	return m.summary
}

// GetResults returns the import results
func (m ImportCompletionModel) GetResults() []wallet.ImportResult {
	return m.results
}

// GetElapsedTime returns the elapsed time for the import
func (m ImportCompletionModel) GetElapsedTime() time.Duration {
	return m.elapsedTime
}

// IsShowingErrors returns whether the error details view is active
func (m ImportCompletionModel) IsShowingErrors() bool {
	return m.showingErrors
}

// GetSelectedAction returns the currently selected action
func (m ImportCompletionModel) GetSelectedAction() CompletionAction {
	if m.selectedAction < len(m.availableActions) {
		return m.availableActions[m.selectedAction].Action
	}
	return CompletionActionNone
}

// GetRetryableFiles returns files that can be retried based on the strategy
func (m ImportCompletionModel) GetRetryableFiles(strategy string) []string {
	var files []string

	switch strategy {
	case "retry_failed":
		for _, result := range m.results {
			if !result.Success && !result.Skipped {
				files = append(files, result.Job.KeystorePath)
			}
		}
	case "retry_skipped":
		for _, result := range m.results {
			if result.Skipped {
				files = append(files, result.Job.KeystorePath)
			}
		}
	case "retry_all":
		for _, result := range m.results {
			if !result.Success {
				files = append(files, result.Job.KeystorePath)
			}
		}
	}

	return files
}

// Custom messages for the completion phase
type RetryImportMsg struct {
	Strategy string
}

type RetrySpecificFileMsg struct {
	File string
}

type ReturnToMenuMsg struct{}

type SelectDifferentFilesMsg struct{}
