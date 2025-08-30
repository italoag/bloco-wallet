package ui

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"blocowallet/internal/wallet"
	"blocowallet/pkg/localization"
)

// ImportCompletionModel represents the completion phase UI for import operations
type ImportCompletionModel struct {
	summary         wallet.ImportSummary
	results         []wallet.ImportResult
	startTime       time.Time
	elapsedTime     time.Duration
	selectedAction  int
	showErrorDetail bool
	selectedError   int
	styles          Styles
	width           int
	height          int
}

// CompletionAction represents available actions in the completion phase
type CompletionAction int

const (
	ActionReturnToMenu CompletionAction = iota
	ActionRetryFailed
	ActionRetryWithManualPasswords
	ActionViewErrorDetails
	ActionSelectDifferentFiles
)

// String returns a string representation of the completion action
func (a CompletionAction) String() string {
	switch a {
	case ActionReturnToMenu:
		return localization.GetEnhancedImportErrorMessage("action_return_to_menu")
	case ActionRetryFailed:
		return localization.GetEnhancedImportErrorMessage("action_retry_failed_imports")
	case ActionRetryWithManualPasswords:
		return localization.GetEnhancedImportErrorMessage("action_retry_with_manual_passwords")
	case ActionViewErrorDetails:
		return localization.GetEnhancedImportErrorMessage("action_view_error_details")
	case ActionSelectDifferentFiles:
		return localization.GetEnhancedImportErrorMessage("action_select_different_files")
	default:
		return "Unknown Action"
	}
}

// NewImportCompletionModel creates a new import completion model
func NewImportCompletionModel(
	summary wallet.ImportSummary,
	results []wallet.ImportResult,
	startTime time.Time,
	styles Styles,
) ImportCompletionModel {
	return ImportCompletionModel{
		summary:         summary,
		results:         results,
		startTime:       startTime,
		elapsedTime:     time.Since(startTime),
		selectedAction:  0,
		showErrorDetail: false,
		selectedError:   0,
		styles:          styles,
		width:           80,
		height:          24,
	}
}

// Init initializes the completion model
func (m ImportCompletionModel) Init() tea.Cmd {
	return nil
}

// Update handles user input in the completion phase
func (m ImportCompletionModel) Update(msg tea.Msg) (ImportCompletionModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		if m.showErrorDetail {
			return m.handleErrorDetailInput(msg)
		}
		return m.handleMainInput(msg)
	}

	return m, nil
}

// handleMainInput handles input in the main completion view
func (m ImportCompletionModel) handleMainInput(msg tea.KeyMsg) (ImportCompletionModel, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.selectedAction > 0 {
			m.selectedAction--
		}
		return m, nil

	case "down", "j":
		maxActions := m.getAvailableActionsCount() - 1
		if m.selectedAction < maxActions {
			m.selectedAction++
		}
		return m, nil

	case "enter", " ":
		return m.executeSelectedAction()

	case "r", "R":
		// Quick retry shortcut
		if m.hasRetryableErrors() {
			return m, func() tea.Msg { return RetryImportMsg{Strategy: "retry_failed"} }
		}
		return m, nil

	case "d", "D":
		// Quick error details shortcut
		if len(m.summary.Errors) > 0 {
			m.showErrorDetail = true
			m.selectedError = 0
		}
		return m, nil

	case "esc":
		// Return to menu
		return m, func() tea.Msg { return ReturnToMenuMsg{} }

	case "q", "ctrl+c":
		return m, tea.Quit
	}

	return m, nil
}

// handleErrorDetailInput handles input in the error detail view
func (m ImportCompletionModel) handleErrorDetailInput(msg tea.KeyMsg) (ImportCompletionModel, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.selectedError > 0 {
			m.selectedError--
		}
		return m, nil

	case "down", "j":
		if m.selectedError < len(m.summary.Errors)-1 {
			m.selectedError++
		}
		return m, nil

	case "esc", "q":
		m.showErrorDetail = false
		return m, nil

	case "r", "R":
		// Retry this specific error
		if m.selectedError < len(m.summary.Errors) {
			errorFile := m.summary.Errors[m.selectedError].File
			return m, func() tea.Msg { return RetrySpecificFileMsg{File: errorFile} }
		}
		return m, nil
	}

	return m, nil
}

// executeSelectedAction executes the currently selected action
func (m ImportCompletionModel) executeSelectedAction() (ImportCompletionModel, tea.Cmd) {
	actions := m.getAvailableActions()
	if m.selectedAction >= len(actions) {
		return m, nil
	}

	selectedAction := actions[m.selectedAction]

	switch selectedAction {
	case ActionReturnToMenu:
		return m, func() tea.Msg { return ReturnToMenuMsg{} }

	case ActionRetryFailed:
		return m, func() tea.Msg { return RetryImportMsg{Strategy: "retry_failed"} }

	case ActionRetryWithManualPasswords:
		return m, func() tea.Msg { return RetryImportMsg{Strategy: "manual_passwords"} }

	case ActionViewErrorDetails:
		if len(m.summary.Errors) > 0 {
			m.showErrorDetail = true
			m.selectedError = 0
		}
		return m, nil

	case ActionSelectDifferentFiles:
		return m, func() tea.Msg { return SelectDifferentFilesMsg{} }

	default:
		return m, nil
	}
}

// View renders the completion phase UI
func (m ImportCompletionModel) View() string {
	if m.showErrorDetail {
		return m.renderErrorDetailView()
	}
	return m.renderMainCompletionView()
}

// renderMainCompletionView renders the main completion view
func (m ImportCompletionModel) renderMainCompletionView() string {
	var sections []string

	// Header with completion status
	header := m.renderCompletionHeader()
	sections = append(sections, header)

	// Summary statistics
	summary := m.renderSummaryStatistics()
	sections = append(sections, summary)

	// Error summary (if any errors)
	if len(m.summary.Errors) > 0 {
		errorSummary := m.renderErrorSummary()
		sections = append(sections, errorSummary)
	}

	// Available actions
	actions := m.renderAvailableActions()
	sections = append(sections, actions)

	// Instructions
	instructions := m.renderInstructions()
	sections = append(sections, instructions)

	content := lipgloss.JoinVertical(lipgloss.Left, sections...)

	// Center the content if we have enough space
	if m.width > 0 && m.height > 0 {
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, content)
	}

	return content
}

// renderCompletionHeader renders the completion status header
func (m ImportCompletionModel) renderCompletionHeader() string {
	var title string
	var titleStyle lipgloss.Style

	if m.summary.FailedImports == 0 && m.summary.SkippedImports == 0 {
		// All successful
		title = "✓ " + localization.GetEnhancedImportErrorMessage("import_summary_all_successful")
		titleStyle = m.styles.SuccessStyle.Bold(true)
	} else if m.summary.SuccessfulImports > 0 {
		// Partial success
		title = "⚠ " + localization.GetEnhancedImportErrorMessage("import_summary_partial_success")
		titleStyle = m.styles.MenuTitle.Foreground(lipgloss.Color("214")) // Orange
	} else {
		// All failed
		title = "✗ " + localization.GetEnhancedImportErrorMessage("import_summary_all_failed")
		titleStyle = m.styles.ErrorStyle.Bold(true)
	}

	return titleStyle.Render(title)
}

// renderSummaryStatistics renders the import statistics
func (m ImportCompletionModel) renderSummaryStatistics() string {
	var sections []string

	// Basic statistics
	stats := fmt.Sprintf("Total: %d | Success: %d | Failed: %d | Skipped: %d",
		m.summary.TotalFiles,
		m.summary.SuccessfulImports,
		m.summary.FailedImports,
		m.summary.SkippedImports,
	)
	sections = append(sections, m.styles.MenuDesc.Render(stats))

	// Elapsed time
	elapsed := m.elapsedTime.Round(time.Second)
	timeText := fmt.Sprintf("Completed in: %v", elapsed)
	sections = append(sections, m.styles.MenuDesc.Render(timeText))

	// Success rate
	if m.summary.TotalFiles > 0 {
		successRate := float64(m.summary.SuccessfulImports) / float64(m.summary.TotalFiles) * 100
		rateText := fmt.Sprintf("Success rate: %.1f%%", successRate)
		sections = append(sections, m.styles.MenuDesc.Render(rateText))
	}

	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

// renderErrorSummary renders a summary of errors
func (m ImportCompletionModel) renderErrorSummary() string {
	var sections []string

	sections = append(sections, "")
	errorTitle := m.styles.ErrorStyle.Render("Import Issues:")
	sections = append(sections, errorTitle)

	// Categorize errors
	failedErrors := make([]wallet.ImportError, 0)
	skippedErrors := make([]wallet.ImportError, 0)

	for _, err := range m.summary.Errors {
		if err.Skipped {
			skippedErrors = append(skippedErrors, err)
		} else {
			failedErrors = append(failedErrors, err)
		}
	}

	// Show failed imports
	if len(failedErrors) > 0 {
		sections = append(sections, m.styles.ErrorStyle.Render("Failed Imports:"))
		for i, err := range failedErrors {
			if i >= 5 { // Limit to first 5 errors
				remaining := len(failedErrors) - 5
				sections = append(sections, m.styles.MenuDesc.Render(fmt.Sprintf("  ... and %d more failed imports", remaining)))
				break
			}
			fileName := filepath.Base(err.File)
			errorText := fmt.Sprintf("  • %s: %s", fileName, m.getShortErrorMessage(err.Error))
			sections = append(sections, m.styles.MenuDesc.Render(errorText))
		}
	}

	// Show skipped imports
	if len(skippedErrors) > 0 {
		sections = append(sections, m.styles.MenuDesc.Foreground(lipgloss.Color("214")).Render("Skipped Imports:"))
		for i, err := range skippedErrors {
			if i >= 5 { // Limit to first 5 errors
				remaining := len(skippedErrors) - 5
				sections = append(sections, m.styles.MenuDesc.Render(fmt.Sprintf("  ... and %d more skipped imports", remaining)))
				break
			}
			fileName := filepath.Base(err.File)
			reason := m.getSkipReason(err.Error)
			errorText := fmt.Sprintf("  • %s: %s", fileName, reason)
			sections = append(sections, m.styles.MenuDesc.Render(errorText))
		}
	}

	// Recovery information
	if m.hasRetryableErrors() {
		sections = append(sections, "")
		recoveryText := localization.GetEnhancedImportErrorMessage("import_summary_recoverable_errors")
		sections = append(sections, m.styles.MenuDesc.Foreground(lipgloss.Color("70")).Render(recoveryText))
	}

	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

// renderAvailableActions renders the available actions menu
func (m ImportCompletionModel) renderAvailableActions() string {
	var sections []string

	sections = append(sections, "")
	sections = append(sections, m.styles.MenuTitle.Render("Available Actions:"))

	actions := m.getAvailableActions()
	for i, action := range actions {
		actionText := action.String()

		if i == m.selectedAction {
			// Highlight selected action
			actionText = m.styles.SelectedStyle.Render("▶ " + actionText)
		} else {
			actionText = m.styles.MenuDesc.Render("  " + actionText)
		}

		sections = append(sections, actionText)
	}

	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

// renderInstructions renders user instructions
func (m ImportCompletionModel) renderInstructions() string {
	var instructions []string

	instructions = append(instructions, "")
	instructions = append(instructions, m.styles.MenuDesc.Render("Navigation:"))
	instructions = append(instructions, m.styles.MenuDesc.Render("  ↑/↓ or k/j: Navigate actions"))
	instructions = append(instructions, m.styles.MenuDesc.Render("  Enter/Space: Execute selected action"))

	if m.hasRetryableErrors() {
		instructions = append(instructions, m.styles.MenuDesc.Render("  R: Quick retry failed imports"))
	}

	if len(m.summary.Errors) > 0 {
		instructions = append(instructions, m.styles.MenuDesc.Render("  D: View error details"))
	}

	instructions = append(instructions, m.styles.MenuDesc.Render("  ESC: Return to menu"))
	instructions = append(instructions, m.styles.MenuDesc.Render("  Q/Ctrl+C: Quit"))

	return lipgloss.JoinVertical(lipgloss.Left, instructions...)
}

// renderErrorDetailView renders the detailed error view
func (m ImportCompletionModel) renderErrorDetailView() string {
	var sections []string

	// Header
	header := m.styles.MenuTitle.Render("Error Details")
	sections = append(sections, header)

	if len(m.summary.Errors) == 0 {
		sections = append(sections, m.styles.MenuDesc.Render("No errors to display"))
		return lipgloss.JoinVertical(lipgloss.Left, sections...)
	}

	// Error navigation info
	navInfo := fmt.Sprintf("Error %d of %d", m.selectedError+1, len(m.summary.Errors))
	sections = append(sections, m.styles.MenuDesc.Render(navInfo))

	// Current error details
	if m.selectedError < len(m.summary.Errors) {
		currentError := m.summary.Errors[m.selectedError]
		errorDetail := m.renderSingleErrorDetail(currentError)
		sections = append(sections, errorDetail)
	}

	// Instructions
	sections = append(sections, "")
	sections = append(sections, m.styles.MenuDesc.Render("Navigation:"))
	sections = append(sections, m.styles.MenuDesc.Render("  ↑/↓ or k/j: Navigate errors"))
	sections = append(sections, m.styles.MenuDesc.Render("  R: Retry this specific file"))
	sections = append(sections, m.styles.MenuDesc.Render("  ESC/Q: Return to summary"))

	content := lipgloss.JoinVertical(lipgloss.Left, sections...)

	// Center the content
	if m.width > 0 && m.height > 0 {
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, content)
	}

	return content
}

// renderSingleErrorDetail renders details for a single error
func (m ImportCompletionModel) renderSingleErrorDetail(err wallet.ImportError) string {
	var sections []string

	// File information
	sections = append(sections, "")
	fileInfo := fmt.Sprintf("File: %s", err.File)
	sections = append(sections, m.styles.MenuTitle.Render(fileInfo))

	// Error type and status
	errorType := "Failed"
	statusStyle := m.styles.ErrorStyle
	if err.Skipped {
		errorType = "Skipped"
		statusStyle = m.styles.MenuDesc.Foreground(lipgloss.Color("214"))
	}
	sections = append(sections, statusStyle.Render(fmt.Sprintf("Status: %s", errorType)))

	// Error message
	sections = append(sections, "")
	sections = append(sections, m.styles.MenuDesc.Render("Error Message:"))
	errorMsg := m.getDetailedErrorMessage(err.Error)
	sections = append(sections, m.styles.MenuDesc.Render(errorMsg))

	// Recovery suggestions
	if !err.Skipped {
		sections = append(sections, "")
		sections = append(sections, m.styles.MenuDesc.Render("Suggested Actions:"))
		suggestions := m.getRecoverySuggestions(err.Error)
		for _, suggestion := range suggestions {
			sections = append(sections, m.styles.MenuDesc.Render("  • "+suggestion))
		}
	} else {
		sections = append(sections, "")
		skipReason := m.getSkipReason(err.Error)
		sections = append(sections, m.styles.MenuDesc.Render("Reason: "+skipReason))
	}

	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

// getAvailableActions returns the list of available actions based on current state
func (m ImportCompletionModel) getAvailableActions() []CompletionAction {
	actions := []CompletionAction{ActionReturnToMenu}

	// Add retry options if there are retryable errors
	if m.hasRetryableErrors() {
		actions = append(actions, ActionRetryFailed)
		actions = append(actions, ActionRetryWithManualPasswords)
	}

	// Add error details if there are errors
	if len(m.summary.Errors) > 0 {
		actions = append(actions, ActionViewErrorDetails)
	}

	// Always allow selecting different files
	actions = append(actions, ActionSelectDifferentFiles)

	return actions
}

// getAvailableActionsCount returns the number of available actions
func (m ImportCompletionModel) getAvailableActionsCount() int {
	return len(m.getAvailableActions())
}

// hasRetryableErrors checks if there are errors that can be retried
func (m ImportCompletionModel) hasRetryableErrors() bool {
	for _, err := range m.summary.Errors {
		if !err.Skipped {
			// Check if this is a retryable error type
			if m.isRetryableError(err.Error) {
				return true
			}
		}
	}
	return false
}

// isRetryableError checks if an error can be retried
func (m ImportCompletionModel) isRetryableError(err error) bool {
	if err == nil {
		return false
	}

	errorMsg := strings.ToLower(err.Error())

	// Password-related errors are retryable
	if strings.Contains(errorMsg, "password") ||
		strings.Contains(errorMsg, "incorrect") ||
		strings.Contains(errorMsg, "invalid") ||
		strings.Contains(errorMsg, "decrypt") {
		return true
	}

	// File access errors might be retryable
	if strings.Contains(errorMsg, "permission") ||
		strings.Contains(errorMsg, "access") {
		return true
	}

	// Timeout errors are retryable
	if strings.Contains(errorMsg, "timeout") {
		return true
	}

	return false
}

// getShortErrorMessage returns a shortened version of the error message
func (m ImportCompletionModel) getShortErrorMessage(err error) string {
	if err == nil {
		return "Unknown error"
	}

	msg := err.Error()
	if len(msg) > 60 {
		return msg[:57] + "..."
	}
	return msg
}

// getDetailedErrorMessage returns a detailed error message
func (m ImportCompletionModel) getDetailedErrorMessage(err error) string {
	if err == nil {
		return "Unknown error occurred"
	}

	return err.Error()
}

// getSkipReason returns a user-friendly reason for why a file was skipped
func (m ImportCompletionModel) getSkipReason(err error) string {
	if err == nil {
		return "User chose to skip"
	}

	errorMsg := strings.ToLower(err.Error())

	if strings.Contains(errorMsg, "cancelled") {
		return "User cancelled password input"
	}

	if strings.Contains(errorMsg, "skipped") {
		return "User chose to skip this file"
	}

	if strings.Contains(errorMsg, "timeout") {
		return "Password input timed out"
	}

	return "User action required"
}

// getRecoverySuggestions returns recovery suggestions for an error
func (m ImportCompletionModel) getRecoverySuggestions(err error) []string {
	if err == nil {
		return []string{"Try the operation again"}
	}

	errorMsg := strings.ToLower(err.Error())
	var suggestions []string

	if strings.Contains(errorMsg, "password") || strings.Contains(errorMsg, "decrypt") {
		suggestions = append(suggestions, "Verify the password is correct")
		suggestions = append(suggestions, "Check if a .pwd file exists with the correct password")
		suggestions = append(suggestions, "Try entering the password manually")
	}

	if strings.Contains(errorMsg, "permission") || strings.Contains(errorMsg, "access") {
		suggestions = append(suggestions, "Check file permissions")
		suggestions = append(suggestions, "Ensure the file is not locked by another process")
		suggestions = append(suggestions, "Try running with appropriate permissions")
	}

	if strings.Contains(errorMsg, "not found") {
		suggestions = append(suggestions, "Verify the file path is correct")
		suggestions = append(suggestions, "Ensure the file exists and is accessible")
	}

	if strings.Contains(errorMsg, "format") || strings.Contains(errorMsg, "invalid") {
		suggestions = append(suggestions, "Verify the file is a valid KeyStore V3 format")
		suggestions = append(suggestions, "Check if the file is corrupted")
	}

	if len(suggestions) == 0 {
		suggestions = append(suggestions, "Review the error message and try again")
		suggestions = append(suggestions, "Contact support if the issue persists")
	}

	return suggestions
}

// Custom messages for the completion phase
type RetryImportMsg struct {
	Strategy string // "retry_failed", "manual_passwords", etc.
}

type RetrySpecificFileMsg struct {
	File string
}

type ReturnToMenuMsg struct{}

type SelectDifferentFilesMsg struct{}

// GetSummary returns the import summary
func (m ImportCompletionModel) GetSummary() wallet.ImportSummary {
	return m.summary
}

// GetResults returns the import results
func (m ImportCompletionModel) GetResults() []wallet.ImportResult {
	return m.results
}

// GetSelectedAction returns the currently selected action
func (m ImportCompletionModel) GetSelectedAction() CompletionAction {
	actions := m.getAvailableActions()
	if m.selectedAction >= len(actions) {
		return ActionReturnToMenu
	}
	return actions[m.selectedAction]
}

// SetDimensions sets the display dimensions
func (m *ImportCompletionModel) SetDimensions(width, height int) {
	m.width = width
	m.height = height
}

// GetFailedFiles returns a list of files that failed to import
func (m ImportCompletionModel) GetFailedFiles() []string {
	var failedFiles []string
	for _, err := range m.summary.Errors {
		if !err.Skipped {
			failedFiles = append(failedFiles, err.File)
		}
	}
	return failedFiles
}

// GetSkippedFiles returns a list of files that were skipped
func (m ImportCompletionModel) GetSkippedFiles() []string {
	var skippedFiles []string
	for _, err := range m.summary.Errors {
		if err.Skipped {
			skippedFiles = append(skippedFiles, err.File)
		}
	}
	return skippedFiles
}

// GetRetryableFiles returns a list of files that can be retried
func (m ImportCompletionModel) GetRetryableFiles() []string {
	var retryableFiles []string
	for _, err := range m.summary.Errors {
		if !err.Skipped && m.isRetryableError(err.Error) {
			retryableFiles = append(retryableFiles, err.File)
		}
	}
	return retryableFiles
}
