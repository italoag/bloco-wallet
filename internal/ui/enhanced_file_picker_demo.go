package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// EnhancedFilePickerDemo demonstrates how to use the enhanced file picker
type EnhancedFilePickerDemo struct {
	picker   EnhancedFilePickerModel
	result   *FilePickerResult
	finished bool
}

// NewEnhancedFilePickerDemo creates a new demo instance
func NewEnhancedFilePickerDemo() *EnhancedFilePickerDemo {
	picker := NewEnhancedFilePicker()

	// Configure for keystore import
	picker.SetAllowedTypes([]string{".json"})
	picker.MultiSelect = true
	picker.DirAllowed = true
	picker.FileAllowed = true

	// Set custom styling
	picker.Styles.Header = picker.Styles.Header.
		Foreground(lipgloss.Color("99")).
		Bold(true).
		Padding(1, 2)

	return &EnhancedFilePickerDemo{
		picker: picker,
	}
}

// Init initializes the demo
func (d *EnhancedFilePickerDemo) Init() tea.Cmd {
	return d.picker.Init()
}

// Update handles messages
func (d *EnhancedFilePickerDemo) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if d.finished {
		if keyMsg, ok := msg.(tea.KeyMsg); ok {
			if keyMsg.String() == "q" || keyMsg.String() == "esc" {
				return d, tea.Quit
			}
		}
		return d, nil
	}

	var cmd tea.Cmd
	d.picker, cmd = d.picker.Update(msg)

	// Check if picker is finished
	if d.picker.IsConfirmed() || d.picker.IsCancelled() {
		result := d.picker.GetResult()
		d.result = &result
		d.finished = true
	}

	return d, cmd
}

// View renders the demo
func (d *EnhancedFilePickerDemo) View() string {
	if d.finished {
		return d.renderResult()
	}

	var content strings.Builder

	// Title
	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("212")).
		Padding(1, 2).
		Render("Enhanced File Picker Demo - Keystore Import")

	content.WriteString(title)
	content.WriteString("\n\n")

	// Instructions
	instructions := lipgloss.NewStyle().
		Foreground(lipgloss.Color("244")).
		Italic(true).
		Padding(0, 2).
		Render("Select keystore files (.json) or directories for batch import")

	content.WriteString(instructions)
	content.WriteString("\n\n")

	// File picker
	content.WriteString(d.picker.View())

	return content.String()
}

// renderResult shows the final selection result
func (d *EnhancedFilePickerDemo) renderResult() string {
	var content strings.Builder

	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("70")).
		Padding(1, 2).
		Render("Selection Result")

	content.WriteString(title)
	content.WriteString("\n\n")

	if d.result.Cancelled {
		cancelled := lipgloss.NewStyle().
			Foreground(lipgloss.Color("1")).
			Render("Selection cancelled by user")
		content.WriteString(cancelled)
	} else if d.result.Confirmed {
		confirmed := lipgloss.NewStyle().
			Foreground(lipgloss.Color("70")).
			Render("Selection confirmed!")
		content.WriteString(confirmed)
		content.WriteString("\n\n")

		if len(d.result.Files) > 0 {
			content.WriteString("Selected files:\n")
			for i, file := range d.result.Files {
				fileStyle := lipgloss.NewStyle().
					Foreground(lipgloss.Color("252")).
					PaddingLeft(2)
				content.WriteString(fileStyle.Render(fmt.Sprintf("%d. %s", i+1, file)))
				content.WriteString("\n")
			}
		}

		if d.result.Directory != "" {
			dirStyle := lipgloss.NewStyle().
				Foreground(lipgloss.Color("99")).
				Bold(true).
				PaddingLeft(2)
			content.WriteString("\nSelected directory:\n")
			content.WriteString(dirStyle.Render(d.result.Directory))
			content.WriteString("\n")
		}

		if len(d.result.Files) == 0 && d.result.Directory == "" {
			noSelection := lipgloss.NewStyle().
				Foreground(lipgloss.Color("244")).
				Italic(true).
				Render("No files or directories selected")
			content.WriteString(noSelection)
		}
	}

	content.WriteString("\n\n")

	exitInstructions := lipgloss.NewStyle().
		Foreground(lipgloss.Color("244")).
		Italic(true).
		Render("Press 'q' or 'esc' to exit")

	content.WriteString(exitInstructions)

	return content.String()
}

// GetSelectedFiles returns the selected files from the result
func (d *EnhancedFilePickerDemo) GetSelectedFiles() []string {
	if d.result != nil {
		return d.result.Files
	}
	return []string{}
}

// GetSelectedDirectory returns the selected directory from the result
func (d *EnhancedFilePickerDemo) GetSelectedDirectory() string {
	if d.result != nil {
		return d.result.Directory
	}
	return ""
}

// IsFinished returns true if the demo is finished
func (d *EnhancedFilePickerDemo) IsFinished() bool {
	return d.finished
}

// WasConfirmed returns true if the selection was confirmed
func (d *EnhancedFilePickerDemo) WasConfirmed() bool {
	return d.result != nil && d.result.Confirmed
}

// WasCancelled returns true if the selection was cancelled
func (d *EnhancedFilePickerDemo) WasCancelled() bool {
	return d.result != nil && d.result.Cancelled
}
