package ui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"blocowallet/internal/wallet"
	"blocowallet/pkg/localization"
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
		Label:       localization.Get("ic_return_menu"),
		Description: localization.Get("ic_return_menu_desc"),
		Key:         "ENTER",
		Enabled:     true,
	})

	// Always available: Select different files
	m.availableActions = append(m.availableActions, CompletionActionItem{
		Action:      CompletionActionSelectDifferentFiles,
		Label:       localization.Get("ic_select_files"),
		Description: localization.Get("ic_select_files_desc"),
		Key:         "S",
		Enabled:     true,
	})

	// Retry failed imports (only if there are failed imports)
	if m.summary.FailedImports > 0 {
		m.availableActions = append(m.availableActions, CompletionActionItem{
			Action:      CompletionActionRetryFailed,
			Label:       localization.T("ic_retry_failed", map[string]interface{}{"Count": m.summary.FailedImports}),
			Description: localization.Get("ic_retry_failed_desc"),
			Key:         "F",
			Enabled:     true,
		})
	}

	// Retry skipped imports (only if there are skipped imports)
	if m.summary.SkippedImports > 0 {
		m.availableActions = append(m.availableActions, CompletionActionItem{
			Action:      CompletionActionRetrySkipped,
			Label:       localization.T("ic_retry_skipped", map[string]interface{}{"Count": m.summary.SkippedImports}),
			Description: localization.Get("ic_retry_skipped_desc"),
			Key:         "K",
			Enabled:     true,
		})
	}

	// Retry all failed and skipped (only if there are any failures or skips)
	if m.summary.FailedImports > 0 || m.summary.SkippedImports > 0 {
		totalRetryable := m.summary.FailedImports + m.summary.SkippedImports
		m.availableActions = append(m.availableActions, CompletionActionItem{
			Action:      CompletionActionRetryAll,
			Label:       localization.T("ic_retry_all", map[string]interface{}{"Count": totalRetryable}),
			Description: localization.Get("ic_retry_all_desc"),
			Key:         "A",
			Enabled:     true,
		})
	}

	// View error details (only if there are errors)
	if len(m.summary.Errors) > 0 {
		m.availableActions = append(m.availableActions, CompletionActionItem{
			Action:      CompletionActionViewErrors,
			Label:       localization.T("ic_view_errors", map[string]interface{}{"Count": len(m.summary.Errors)}),
			Description: localization.Get("ic_view_errors_desc"),
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
		title = localization.Get("ic_title_success")
		style = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("70")) // Green
	} else if m.summary.SuccessfulImports > 0 {
		// Partial success
		title = localization.Get("ic_title_issues")
		style = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214")) // Orange
	} else {
		// Complete failure
		title = localization.Get("ic_title_failed")
		style = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("196")) // Red
	}

	return style.Render(title)
}

// renderSummaryStats renders the summary statistics
func (m ImportCompletionModel) renderSummaryStats() string {
	var sections []string

	// Main statistics line
	stats := localization.T("eimp_stats", map[string]interface{}{
		"Total": m.summary.TotalFiles, "Success": m.summary.SuccessfulImports,
		"Failed": m.summary.FailedImports, "Skipped": m.summary.SkippedImports,
	})

	sections = append(sections, stats)

	// Success rate if there were any files processed
	if m.summary.TotalFiles > 0 {
		successRate := float64(m.summary.SuccessfulImports) / float64(m.summary.TotalFiles) * 100
		rateText := localization.T("ic_success_rate", map[string]interface{}{"Rate": fmt.Sprintf("%.1f", successRate)})

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
	timeText := localization.T("ic_completed_in", map[string]interface{}{"Elapsed": m.elapsedTime.Round(time.Second)})

	// Add performance info if we have multiple files
	if m.summary.TotalFiles > 1 {
		avgTime := m.elapsedTime / time.Duration(m.summary.TotalFiles)
		timeText += localization.T("ic_avg_per_file", map[string]interface{}{"Avg": avgTime.Round(time.Millisecond)})
	}

	return timeText
}

// renderQuickErrorSummary renders a quick summary of errors
func (m ImportCompletionModel) renderQuickErrorSummary() string {
	var sections []string

	sections = append(sections, "")

	errorTitle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("196")).Render(localization.Get("ic_issues_title"))
	sections = append(sections, errorTitle)

	// Group errors by type
	failedFiles := []string{}
	skippedFiles := []string{}

	for _, err := range m.summary.Errors {
		if err.Skipped {
			skippedFiles = append(skippedFiles, safeShort(err.File))
		} else {
			failedFiles = append(failedFiles, safeShort(err.File))
		}
	}

	// Show failed files (up to 3)
	if len(failedFiles) > 0 {
		sections = append(sections, lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Render(localization.Get("ic_failed_label")))
		for i, file := range failedFiles {
			if i >= 3 {
				sections = append(sections, localization.T("ic_more", map[string]interface{}{"Count": len(failedFiles) - 3}))
				break
			}
			sections = append(sections, fmt.Sprintf("  • %s", file))
		}
	}

	// Show skipped files (up to 3)
	if len(skippedFiles) > 0 {
		sections = append(sections, lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Render(localization.Get("ic_skipped_label")))
		for i, file := range skippedFiles {
			if i >= 3 {
				sections = append(sections, localization.T("ic_more", map[string]interface{}{"Count": len(skippedFiles) - 3}))
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

	actionsTitle := lipgloss.NewStyle().Bold(true).Render(localization.Get("ic_actions_title"))
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
		localization.Get("ic_nav_actions"),
		localization.Get("ic_enter_action"),
		localization.Get("ic_esc_menu"),
	}

	// Add specific key instructions if actions are available
	if m.hasActionWithKey("E") {
		instructions = append(instructions, localization.Get("ic_press_e"))
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
		return localization.Get("ic_no_errors")
	}

	var sections []string

	// Title
	title := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("196")).Render(localization.Get("ic_error_details"))
	sections = append(sections, title)

	// Error navigation info
	navInfo := localization.T("ic_error_nav", map[string]interface{}{"Index": m.errorIndex + 1, "Total": len(m.summary.Errors)})
	sections = append(sections, navInfo)

	// Current error details
	if m.errorIndex < len(m.summary.Errors) {
		errorDetails := m.renderSingleErrorDetails(m.summary.Errors[m.errorIndex])
		sections = append(sections, errorDetails)
	}

	// Navigation instructions
	sections = append(sections, "")
	instructions := []string{
		localization.Get("ic_nav_errors"),
		localization.Get("ic_retry_file"),
		localization.Get("ic_esc_summary"),
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
	sections = append(sections, fileStyle.Render(localization.T("ic_file_label", map[string]interface{}{"File": safeShort(err.File)})))

	// Error type
	errorType := localization.Get("eimp_error_failed")
	typeStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	if err.Skipped {
		errorType = localization.Get("eimp_error_skipped")
		typeStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	}
	sections = append(sections, typeStyle.Render(localization.T("ic_status_label", map[string]interface{}{"Status": errorType})))

	// Error message
	sections = append(sections, "")
	sections = append(sections, localization.Get("ic_details_label"))

	errorMsg := safeError(err.Error)
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
		sections = append(sections, localization.Get("ic_suggested"))
		for _, suggestion := range suggestions {
			sections = append(sections, fmt.Sprintf("  • %s", suggestion))
		}
	}

	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

// getSuggestedActions returns suggested actions based on the error type
func (m ImportCompletionModel) getSuggestedActions(err wallet.ImportError) []string {
	var suggestions []string

	errorMsg := strings.ToLower(safeError(err.Error))

	if err.Skipped {
		suggestions = append(suggestions, localization.Get("ic_sug_skipped"))
		suggestions = append(suggestions, localization.Get("ic_sug_retry_manual"))
	} else if strings.Contains(errorMsg, "password") || strings.Contains(errorMsg, "decrypt") {
		suggestions = append(suggestions, localization.Get("ic_sug_password"))
		suggestions = append(suggestions, localization.Get("ic_sug_pwdfile"))
		suggestions = append(suggestions, localization.Get("ic_sug_retry_manual"))
	} else if strings.Contains(errorMsg, "format") || strings.Contains(errorMsg, "invalid") {
		suggestions = append(suggestions, localization.Get("ic_sug_format"))
		suggestions = append(suggestions, localization.Get("ic_sug_corrupt"))
	} else if strings.Contains(errorMsg, "permission") || strings.Contains(errorMsg, "access") {
		suggestions = append(suggestions, localization.Get("ic_sug_perms"))
		suggestions = append(suggestions, localization.Get("ic_sug_locked"))
	} else {
		suggestions = append(suggestions, localization.Get("ic_sug_details"))
		suggestions = append(suggestions, localization.Get("ic_sug_accessible"))
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
