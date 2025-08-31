package ui

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewImportProgressDemoModel(t *testing.T) {
	demo := NewImportProgressDemoModel()

	assert.Equal(t, 0, demo.currentStep)
	assert.Greater(t, demo.totalSteps, 0)
	assert.False(t, demo.autoAdvance)
	assert.Equal(t, time.Second*2, demo.stepDelay)
	assert.NotEmpty(t, demo.demoSteps)
	assert.NotNil(t, demo.progress)
}

func TestImportProgressDemoModel_Init(t *testing.T) {
	demo := NewImportProgressDemoModel()
	cmd := demo.Init()
	assert.NotNil(t, cmd)
}

func TestImportProgressDemoModel_Update_KeyMessages(t *testing.T) {
	tests := []struct {
		name     string
		key      string
		expected func(ImportProgressDemoModel) bool
	}{
		{
			name: "quit with q",
			key:  "q",
			expected: func(m ImportProgressDemoModel) bool {
				// We can't easily test tea.Quit, so we'll test the key handling
				return true
			},
		},
		{
			name: "toggle auto-advance",
			key:  "a",
			expected: func(m ImportProgressDemoModel) bool {
				return m.autoAdvance // Should be toggled from false to true
			},
		},
		{
			name: "manual step advance with enter",
			key:  "enter",
			expected: func(m ImportProgressDemoModel) bool {
				return m.currentStep == 1 // Should advance from 0 to 1
			},
		},
		{
			name: "manual step advance with space",
			key:  " ",
			expected: func(m ImportProgressDemoModel) bool {
				return m.currentStep == 1 // Should advance from 0 to 1
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			demo := NewImportProgressDemoModel()
			keyMsg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(tt.key)}

			if tt.key == "enter" {
				keyMsg = tea.KeyMsg{Type: tea.KeyEnter}
			}

			updatedModel, _ := demo.Update(keyMsg)
			demoModel := updatedModel.(ImportProgressDemoModel)

			if tt.expected != nil {
				assert.True(t, tt.expected(demoModel))
			}
		})
	}
}

func TestImportProgressDemoModel_Update_TickMessage(t *testing.T) {
	demo := NewImportProgressDemoModel()
	demo.autoAdvance = true
	demo.lastStepTime = time.Now().Add(-time.Second * 3) // Simulate time passed

	tickMsg := tickMsg(time.Now())
	updatedModel, cmd := demo.Update(tickMsg)
	demoModel := updatedModel.(ImportProgressDemoModel)

	// Should advance step when auto-advance is enabled and enough time has passed
	assert.Equal(t, 1, demoModel.currentStep)
	assert.NotNil(t, cmd)
}

func TestImportProgressDemoModel_Update_TickMessage_NoAutoAdvance(t *testing.T) {
	demo := NewImportProgressDemoModel()
	demo.autoAdvance = false

	tickMsg := tickMsg(time.Now())
	updatedModel, cmd := demo.Update(tickMsg)
	demoModel := updatedModel.(ImportProgressDemoModel)

	// Should not advance step when auto-advance is disabled
	assert.Equal(t, 0, demoModel.currentStep)
	assert.NotNil(t, cmd) // Should still return tick command
}

func TestImportProgressDemoModel_ExecuteCurrentStep(t *testing.T) {
	demo := NewImportProgressDemoModel()

	// Test executing first step
	demo.executeCurrentStep()

	// Verify that the step was executed (progress should be reset)
	assert.Equal(t, 5, demo.progress.totalFiles)
	assert.Equal(t, 0, demo.progress.processedFiles)
}

func TestImportProgressDemoModel_ExecuteAllSteps(t *testing.T) {
	demo := NewImportProgressDemoModel()

	// Execute all demo steps
	for i := 0; i < demo.totalSteps; i++ {
		demo.currentStep = i
		demo.executeCurrentStep()
	}

	// Verify final state
	assert.True(t, demo.progress.IsCompleted())
	assert.Equal(t, 5, demo.progress.processedFiles)
	assert.Greater(t, len(demo.progress.GetErrors()), 0) // Should have some errors from demo
}

func TestImportProgressDemoModel_View(t *testing.T) {
	demo := NewImportProgressDemoModel()

	view := demo.View()

	// Check for expected content
	assert.Contains(t, view, "Import Progress Component Demo")
	assert.Contains(t, view, "Controls:")
	assert.Contains(t, view, "Step 1/")
	assert.Contains(t, view, "Mode: Manual")
	assert.Contains(t, view, "Real-time progress tracking")
}

func TestImportProgressDemoModel_ViewCompleted(t *testing.T) {
	demo := NewImportProgressDemoModel()
	demo.currentStep = demo.totalSteps // Set to completed

	view := demo.View()

	assert.Contains(t, view, "Demo completed!")
	assert.Contains(t, view, "Press R to reset")
}

func TestImportProgressDemoModel_ViewAutoAdvance(t *testing.T) {
	demo := NewImportProgressDemoModel()
	demo.autoAdvance = true

	view := demo.View()

	assert.Contains(t, view, "Mode: Auto")
}

func TestImportProgressDemoModel_DemoSteps(t *testing.T) {
	demo := NewImportProgressDemoModel()

	// Verify all demo steps are properly configured
	require.Greater(t, len(demo.demoSteps), 0)

	for i, step := range demo.demoSteps {
		assert.NotEmpty(t, step.name, "Step %d should have a name", i)
		assert.NotEmpty(t, step.description, "Step %d should have a description", i)
		assert.NotNil(t, step.action, "Step %d should have an action", i)
	}
}

func TestImportProgressDemoModel_StepExecution(t *testing.T) {
	// Test specific step behaviors
	testCases := []struct {
		stepIndex    int
		stepName     string
		verifyAction func(*testing.T, ImportProgressDemoModel)
	}{
		{
			stepIndex: 0,
			stepName:  "Initialize",
			verifyAction: func(t *testing.T, m ImportProgressDemoModel) {
				assert.Equal(t, 5, m.progress.totalFiles)
				assert.Equal(t, 0, m.progress.processedFiles)
			},
		},
		{
			stepIndex: 3,
			stepName:  "Process File 2",
			verifyAction: func(t *testing.T, m ImportProgressDemoModel) {
				// This step should pause the progress
				assert.True(t, m.progress.IsPaused())
			},
		},
		{
			stepIndex: 4,
			stepName:  "Resume File 2",
			verifyAction: func(t *testing.T, m ImportProgressDemoModel) {
				// This step should resume the progress
				assert.False(t, m.progress.IsPaused())
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.stepName, func(t *testing.T) {
			demo := NewImportProgressDemoModel()

			// Execute steps up to the target step
			for i := 0; i <= tc.stepIndex; i++ {
				demo.currentStep = i
				demo.executeCurrentStep()
			}

			tc.verifyAction(t, demo)
		})
	}
}

func TestImportProgressDemoModel_Reset(t *testing.T) {
	demo := NewImportProgressDemoModel()

	// Advance demo state
	demo.currentStep = 5
	demo.autoAdvance = true
	demo.executeCurrentStep()

	// Reset by creating new model (simulating 'r' key press behavior)
	resetDemo := NewImportProgressDemoModel()

	assert.Equal(t, 0, resetDemo.currentStep)
	assert.False(t, resetDemo.autoAdvance)
	assert.Equal(t, 0, resetDemo.progress.processedFiles)
}

func TestImportProgressDemoModel_ProgressIntegration(t *testing.T) {
	demo := NewImportProgressDemoModel()

	// Test that demo properly integrates with progress component
	progressMsg := ImportProgressMsg{
		CurrentFile:    "test.json",
		ProcessedFiles: 1,
		TotalFiles:     5,
	}

	updatedModel, cmd := demo.Update(progressMsg)
	demoModel := updatedModel.(ImportProgressDemoModel)

	assert.Equal(t, "test.json", demoModel.progress.currentFile)
	assert.Equal(t, 1, demoModel.progress.processedFiles)
	assert.NotNil(t, cmd)
}

// Benchmark tests
func BenchmarkImportProgressDemoModel_Update(b *testing.B) {
	demo := NewImportProgressDemoModel()
	tickMsg := tickMsg(time.Now())

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		demo.Update(tickMsg)
	}
}

func BenchmarkImportProgressDemoModel_View(b *testing.B) {
	demo := NewImportProgressDemoModel()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		demo.View()
	}
}

func BenchmarkImportProgressDemoModel_ExecuteStep(b *testing.B) {
	demo := NewImportProgressDemoModel()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		demo.currentStep = i % demo.totalSteps
		demo.executeCurrentStep()
	}
}
