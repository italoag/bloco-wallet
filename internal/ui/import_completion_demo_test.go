package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
)

func TestNewImportCompletionDemo(t *testing.T) {
	demo := NewImportCompletionDemo()

	assert.False(t, demo.finished)

	completion := demo.GetCompletion()
	assert.Equal(t, 5, completion.summary.TotalFiles)
	assert.Equal(t, 3, completion.summary.SuccessfulImports)
	assert.Equal(t, 1, completion.summary.FailedImports)
	assert.Equal(t, 1, completion.summary.SkippedImports)
	assert.Equal(t, 2, len(completion.summary.Errors))
}

func TestImportCompletionDemo_Init(t *testing.T) {
	demo := NewImportCompletionDemo()
	cmd := demo.Init()
	assert.Nil(t, cmd)
}

func TestImportCompletionDemo_Update_Quit(t *testing.T) {
	demo := NewImportCompletionDemo()

	// Test quit with 'q'
	model, cmd := demo.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	demoModel := model.(ImportCompletionDemoModel)

	assert.True(t, demoModel.finished)
	assert.NotNil(t, cmd)

	// Test quit with Ctrl+C
	demo = NewImportCompletionDemo()
	model, cmd = demo.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	demoModel = model.(ImportCompletionDemoModel)

	assert.True(t, demoModel.finished)
	assert.NotNil(t, cmd)
}

func TestImportCompletionDemo_Update_CompletionActions(t *testing.T) {
	demo := NewImportCompletionDemo()

	// Test ESC key (should trigger return to menu)
	model, cmd := demo.Update(tea.KeyMsg{Type: tea.KeyEsc})
	demoModel := model.(ImportCompletionDemoModel)

	assert.True(t, demoModel.finished)
	assert.NotNil(t, cmd)
}

func TestImportCompletionDemo_Update_Navigation(t *testing.T) {
	demo := NewImportCompletionDemo()
	initialSelection := demo.completion.selectedAction

	// Test navigation down
	model, _ := demo.Update(tea.KeyMsg{Type: tea.KeyDown})
	demoModel := model.(ImportCompletionDemoModel)

	assert.False(t, demoModel.finished)
	assert.Equal(t, initialSelection+1, demoModel.completion.selectedAction)

	// Test navigation up
	model, _ = demoModel.Update(tea.KeyMsg{Type: tea.KeyUp})
	demoModel = model.(ImportCompletionDemoModel)

	assert.False(t, demoModel.finished)
	assert.Equal(t, initialSelection, demoModel.completion.selectedAction)
}

func TestImportCompletionDemo_View(t *testing.T) {
	demo := NewImportCompletionDemo()

	// Test normal view
	view := demo.View()
	assert.Contains(t, view, "Import Completed")
	assert.Contains(t, view, "Total: 5")
	assert.Contains(t, view, "Success: 3")
	assert.Contains(t, view, "Failed: 1")
	assert.Contains(t, view, "Skipped: 1")
	assert.Contains(t, view, "Available Actions")
	assert.Contains(t, view, "Press 'q' to quit")

	// Test finished view
	demo.finished = true
	finishedView := demo.View()
	assert.Contains(t, finishedView, "Demo completed")
	assert.Contains(t, finishedView, "Thanks for trying")
}

func TestImportCompletionDemo_ErrorDetailsView(t *testing.T) {
	demo := NewImportCompletionDemo()

	// Enter error details view
	model, _ := demo.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	demoModel := model.(ImportCompletionDemoModel)

	assert.True(t, demoModel.completion.showingErrors)

	view := demoModel.View()
	assert.Contains(t, view, "Error Details")
	assert.Contains(t, view, "Error 1 of 2")
	assert.Contains(t, view, "wallet_failed.json")
}

func TestImportCompletionDemo_GettersAndSetters(t *testing.T) {
	demo := NewImportCompletionDemo()

	// Test getters
	completion := demo.GetCompletion()
	assert.Equal(t, 5, completion.summary.TotalFiles)
	assert.False(t, demo.IsFinished())

	// Test state change
	demo.finished = true
	assert.True(t, demo.IsFinished())
}

func TestImportCompletionDemo_Integration(t *testing.T) {
	demo := NewImportCompletionDemo()

	// Test full navigation flow
	steps := []tea.KeyMsg{
		{Type: tea.KeyDown},                      // Navigate down
		{Type: tea.KeyDown},                      // Navigate down again
		{Type: tea.KeyUp},                        // Navigate up
		{Type: tea.KeyRunes, Runes: []rune{'e'}}, // View errors
		{Type: tea.KeyDown},                      // Navigate to next error
		{Type: tea.KeyEsc},                       // Exit error view
		{Type: tea.KeyRunes, Runes: []rune{'q'}}, // Quit
	}

	var model tea.Model = demo
	for i, step := range steps {
		var cmd tea.Cmd
		model, cmd = model.Update(step)

		if i < len(steps)-1 {
			// Should not be finished until the last step
			demoModel := model.(ImportCompletionDemoModel)
			if step.String() != "q" {
				assert.False(t, demoModel.finished, "Demo should not be finished at step %d", i)
			}
		} else {
			// Should be finished on the last step
			demoModel := model.(ImportCompletionDemoModel)
			assert.True(t, demoModel.finished, "Demo should be finished after quit")
			assert.NotNil(t, cmd, "Quit command should be returned")
		}
	}
}
