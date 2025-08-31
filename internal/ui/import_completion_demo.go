package ui

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"blocowallet/internal/wallet"
)

// ImportCompletionDemoModel demonstrates the import completion component
type ImportCompletionDemoModel struct {
	completion ImportCompletionModel
	finished   bool
}

// NewImportCompletionDemo creates a new demo model
func NewImportCompletionDemo() ImportCompletionDemoModel {
	// Create sample import results for demonstration
	summary := wallet.ImportSummary{
		TotalFiles:        5,
		SuccessfulImports: 3,
		FailedImports:     1,
		SkippedImports:    1,
		Errors: []wallet.ImportError{
			{
				File:    "wallet_failed.json",
				Error:   fmt.Errorf("invalid password: could not decrypt keystore"),
				Skipped: false,
			},
			{
				File:    "wallet_skipped.json",
				Error:   fmt.Errorf("user cancelled password input"),
				Skipped: true,
			},
		},
	}

	results := []wallet.ImportResult{
		{
			Job:     wallet.ImportJob{KeystorePath: "wallet1.json", WalletName: "Wallet 1"},
			Success: true,
			Skipped: false,
		},
		{
			Job:     wallet.ImportJob{KeystorePath: "wallet2.json", WalletName: "Wallet 2"},
			Success: true,
			Skipped: false,
		},
		{
			Job:     wallet.ImportJob{KeystorePath: "wallet3.json", WalletName: "Wallet 3"},
			Success: true,
			Skipped: false,
		},
		{
			Job:     wallet.ImportJob{KeystorePath: "wallet_failed.json", WalletName: "Failed Wallet"},
			Success: false,
			Skipped: false,
		},
		{
			Job:     wallet.ImportJob{KeystorePath: "wallet_skipped.json", WalletName: "Skipped Wallet"},
			Success: false,
			Skipped: true,
		},
	}

	startTime := time.Now().Add(-30 * time.Second) // Simulate 30 second import
	completion := NewImportCompletionModel(summary, results, startTime, Styles{})

	return ImportCompletionDemoModel{
		completion: completion,
		finished:   false,
	}
}

// Init initializes the demo
func (m ImportCompletionDemoModel) Init() tea.Cmd {
	return m.completion.Init()
}

// Update handles demo updates
func (m ImportCompletionDemoModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			m.finished = true
			return m, tea.Quit
		}

		// Handle completion-specific messages
		var cmd tea.Cmd
		m.completion, cmd = m.completion.Update(msg)

		// Check for completion actions that would normally be handled by the parent
		if cmd != nil {
			// Execute the command to see what message it would generate
			if cmdMsg := cmd(); cmdMsg != nil {
				switch cmdMsg.(type) {
				case ReturnToMenuMsg:
					m.finished = true
					return m, tea.Quit
				case SelectDifferentFilesMsg:
					m.finished = true
					return m, tea.Quit
				case RetryImportMsg, RetrySpecificFileMsg:
					// In a real implementation, these would trigger new import operations
					// For the demo, we'll just show a message
					m.finished = true
					return m, tea.Quit
				}
			}
		}

		return m, cmd
	}

	return m, nil
}

// View renders the demo
func (m ImportCompletionDemoModel) View() string {
	if m.finished {
		return "Demo completed. Thanks for trying the import completion component!\n"
	}

	return m.completion.View() + "\n\nPress 'q' to quit the demo"
}

// GetCompletion returns the completion model for testing
func (m ImportCompletionDemoModel) GetCompletion() ImportCompletionModel {
	return m.completion
}

// IsFinished returns whether the demo is finished
func (m ImportCompletionDemoModel) IsFinished() bool {
	return m.finished
}
