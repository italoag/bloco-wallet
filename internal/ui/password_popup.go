package ui

import (
	"fmt"
	"strings"

	"blocowallet/internal/constants"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// PasswordPopupModel represents the password input popup component
type PasswordPopupModel struct {
	textinput.Model
	keystoreFile string
	errorMessage string
	retryCount   int
	maxRetries   int
	cancelled    bool
	skipped      bool
	confirmed    bool
	width        int
	height       int
}

// PasswordPopupResult represents the result of the password popup interaction
type PasswordPopupResult struct {
	Password  string
	Cancelled bool
	Skip      bool
}

// NewPasswordPopupModel creates a new password popup model
func NewPasswordPopupModel(keystoreFile string, maxRetries int) PasswordPopupModel {
	ti := textinput.New()
	ti.Placeholder = "Enter keystore password..."
	ti.Focus()
	ti.CharLimit = constants.PasswordCharLimit
	ti.Width = 40
	ti.EchoMode = textinput.EchoPassword
	ti.EchoCharacter = '•'

	return PasswordPopupModel{
		Model:        ti,
		keystoreFile: keystoreFile,
		maxRetries:   maxRetries,
		width:        60,
		height:       12,
	}
}

// Init initializes the password popup model
func (m PasswordPopupModel) Init() tea.Cmd {
	return textinput.Blink
}

// Update handles messages for the password popup
func (m PasswordPopupModel) Update(msg tea.Msg) (PasswordPopupModel, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			m.cancelled = true
			return m, nil
		case "enter":
			m.confirmed = true
			return m, nil
		case "ctrl+s":
			// Skip this file
			m.skipped = true
			return m, nil
		}
	}

	m.Model, cmd = m.Model.Update(msg)
	return m, cmd
}

// View renders the password popup
func (m PasswordPopupModel) View() string {
	// Create the popup box style
	popupStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("62")).
		Padding(1, 2).
		Width(m.width).
		Align(lipgloss.Center)

	// Title
	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("205")).
		Render("Password Required")

	// Keystore filename
	filename := lipgloss.NewStyle().
		Foreground(lipgloss.Color("241")).
		Render(fmt.Sprintf("File: %s", m.keystoreFile))

	// Retry counter if there have been attempts
	retryInfo := ""
	if m.retryCount > 0 {
		remaining := m.maxRetries - m.retryCount
		if remaining > 0 {
			retryInfo = lipgloss.NewStyle().
				Foreground(lipgloss.Color("214")).
				Render(fmt.Sprintf("Attempts remaining: %d", remaining))
		} else {
			retryInfo = lipgloss.NewStyle().
				Foreground(lipgloss.Color("196")).
				Render("Maximum attempts reached")
		}
	}

	// Error message
	errorMsg := ""
	if m.errorMessage != "" {
		errorMsg = lipgloss.NewStyle().
			Foreground(lipgloss.Color("196")).
			Render(fmt.Sprintf("Error: %s", m.errorMessage))
	}

	// Instructions
	instructions := lipgloss.NewStyle().
		Foreground(lipgloss.Color("244")).
		Render("Enter: Confirm • Esc: Cancel • Ctrl+S: Skip file")

	// Build the content
	content := []string{title, "", filename}

	if retryInfo != "" {
		content = append(content, "", retryInfo)
	}

	if errorMsg != "" {
		content = append(content, "", errorMsg)
	}

	content = append(content, "", m.Model.View(), "", instructions)

	// Join all content
	popupContent := strings.Join(content, "\n")

	// Apply popup style
	popup := popupStyle.Render(popupContent)

	// Center the popup on screen
	return lipgloss.Place(m.width+10, m.height+5, lipgloss.Center, lipgloss.Center, popup)
}

// SetError sets an error message to display in the popup
func (m *PasswordPopupModel) SetError(err string) {
	m.errorMessage = err
	m.retryCount++
	m.SetValue("") // Clear the password input
}

// GetResult returns the result of the popup interaction
func (m PasswordPopupModel) GetResult() PasswordPopupResult {
	if m.skipped {
		return PasswordPopupResult{Skip: true}
	}

	if m.cancelled {
		return PasswordPopupResult{Cancelled: true}
	}

	if m.confirmed {
		return PasswordPopupResult{Password: m.Value()}
	}

	return PasswordPopupResult{}
}

// IsCompleted returns true if the popup interaction is complete
func (m PasswordPopupModel) IsCompleted() bool {
	return m.cancelled || m.skipped || m.confirmed
}

// HasExceededMaxRetries returns true if maximum retry attempts have been exceeded
func (m PasswordPopupModel) HasExceededMaxRetries() bool {
	return m.retryCount >= m.maxRetries
}

// Reset resets the popup for a new keystore file
func (m *PasswordPopupModel) Reset(keystoreFile string) {
	m.keystoreFile = keystoreFile
	m.errorMessage = ""
	m.retryCount = 0
	m.cancelled = false
	m.skipped = false
	m.confirmed = false
	m.SetValue("")
}
