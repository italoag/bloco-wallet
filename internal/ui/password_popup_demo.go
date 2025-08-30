package ui

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
)

// PasswordPopupDemo demonstrates the password popup component
type PasswordPopupDemo struct {
	popup    PasswordPopupModel
	result   *PasswordPopupResult
	finished bool
}

// NewPasswordPopupDemo creates a new demo instance
func NewPasswordPopupDemo() PasswordPopupDemo {
	return PasswordPopupDemo{
		popup: NewPasswordPopupModel("demo-wallet.json", 3),
	}
}

// Init initializes the demo
func (d PasswordPopupDemo) Init() tea.Cmd {
	return d.popup.Init()
}

// Update handles demo updates
func (d PasswordPopupDemo) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if d.finished {
			if msg.String() == "q" || msg.String() == "ctrl+c" {
				return d, tea.Quit
			}
			if msg.String() == "r" {
				// Reset for another demo
				d.popup.Reset("another-wallet.json")
				d.result = nil
				d.finished = false
				return d, d.popup.Init()
			}
		}
	}

	if !d.finished {
		var cmd tea.Cmd
		d.popup, cmd = d.popup.Update(msg)

		if d.popup.IsCompleted() {
			result := d.popup.GetResult()
			d.result = &result
			d.finished = true
		}

		return d, cmd
	}

	return d, nil
}

// View renders the demo
func (d PasswordPopupDemo) View() string {
	if !d.finished {
		return d.popup.View()
	}

	// Show result
	var resultText string
	if d.result.Cancelled {
		resultText = "❌ Password input was cancelled/skipped"
	} else {
		resultText = fmt.Sprintf("✅ Password entered: %s", d.result.Password)
	}

	return fmt.Sprintf(`
%s

Press 'r' to reset and try again
Press 'q' to quit
`, resultText)
}

// RunPasswordPopupDemo runs the password popup demo
func RunPasswordPopupDemo() {
	demo := NewPasswordPopupDemo()

	p := tea.NewProgram(demo, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Printf("Error running demo: %v\n", err)
		os.Exit(1)
	}
}
