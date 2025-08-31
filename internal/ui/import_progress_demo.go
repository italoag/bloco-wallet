package ui

import (
	"errors"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ImportProgressDemoModel demonstrates the Import Progress Component
type ImportProgressDemoModel struct {
	progress     ImportProgressModel
	currentStep  int
	totalSteps   int
	demoSteps    []demoStep
	autoAdvance  bool
	stepDelay    time.Duration
	lastStepTime time.Time
}

type demoStep struct {
	name        string
	description string
	action      func(*ImportProgressDemoModel)
}

// NewImportProgressDemoModel creates a new demo model
func NewImportProgressDemoModel() ImportProgressDemoModel {
	styles := Styles{
		MenuTitle:    lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205")),
		MenuDesc:     lipgloss.NewStyle().Foreground(lipgloss.Color("241")),
		SuccessStyle: lipgloss.NewStyle().Foreground(lipgloss.Color("46")),
		ErrorStyle:   lipgloss.NewStyle().Foreground(lipgloss.Color("196")),
	}

	progress := NewImportProgressModel(5, styles)

	demoSteps := []demoStep{
		{
			name:        "Initialize",
			description: "Starting import of 5 keystore files",
			action: func(m *ImportProgressDemoModel) {
				m.progress.Reset(5)
			},
		},
		{
			name:        "Process File 1",
			description: "Processing wallet1.json",
			action: func(m *ImportProgressDemoModel) {
				m.progress.UpdateProgress("wallet1.json", 0)
			},
		},
		{
			name:        "Complete File 1",
			description: "Successfully imported wallet1.json",
			action: func(m *ImportProgressDemoModel) {
				m.progress.UpdateProgress("wallet1.json", 1)
			},
		},
		{
			name:        "Process File 2",
			description: "Processing wallet2.json - requires password",
			action: func(m *ImportProgressDemoModel) {
				m.progress.UpdateProgress("wallet2.json", 1)
				m.progress.Pause("Password required")
			},
		},
		{
			name:        "Resume File 2",
			description: "Password provided, resuming import",
			action: func(m *ImportProgressDemoModel) {
				m.progress.Resume()
				m.progress.UpdateProgress("wallet2.json", 2)
			},
		},
		{
			name:        "Process File 3",
			description: "Processing wallet3.json",
			action: func(m *ImportProgressDemoModel) {
				m.progress.UpdateProgress("wallet3.json", 2)
			},
		},
		{
			name:        "Error on File 3",
			description: "Failed to import wallet3.json - invalid password",
			action: func(m *ImportProgressDemoModel) {
				err := errors.New("invalid password")
				m.progress.AddError("wallet3.json", err, false)
				m.progress.UpdateProgress("wallet3.json", 3)
			},
		},
		{
			name:        "Process File 4",
			description: "Processing wallet4.json",
			action: func(m *ImportProgressDemoModel) {
				m.progress.UpdateProgress("wallet4.json", 3)
			},
		},
		{
			name:        "Skip File 4",
			description: "User chose to skip wallet4.json",
			action: func(m *ImportProgressDemoModel) {
				err := errors.New("user skipped file")
				m.progress.AddError("wallet4.json", err, true)
				m.progress.UpdateProgress("wallet4.json", 4)
			},
		},
		{
			name:        "Process File 5",
			description: "Processing wallet5.json",
			action: func(m *ImportProgressDemoModel) {
				m.progress.UpdateProgress("wallet5.json", 4)
			},
		},
		{
			name:        "Complete Import",
			description: "Import operation completed",
			action: func(m *ImportProgressDemoModel) {
				m.progress.UpdateProgress("wallet5.json", 5)
				m.progress.Complete()
			},
		},
	}

	return ImportProgressDemoModel{
		progress:     progress,
		currentStep:  0,
		totalSteps:   len(demoSteps),
		demoSteps:    demoSteps,
		autoAdvance:  false,
		stepDelay:    time.Second * 2,
		lastStepTime: time.Now(),
	}
}

// Init initializes the demo model
func (m ImportProgressDemoModel) Init() tea.Cmd {
	return tea.Batch(
		m.progress.Init(),
		tea.Tick(time.Millisecond*100, func(t time.Time) tea.Msg {
			return tickMsg(t)
		}),
	)
}

type tickMsg time.Time

// Update handles demo updates
func (m ImportProgressDemoModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			return m, tea.Quit
		case "r":
			// Reset demo
			return NewImportProgressDemoModel(), tea.Batch(
				tea.Tick(time.Millisecond*100, func(t time.Time) tea.Msg {
					return tickMsg(t)
				}),
			)
		case "a":
			// Toggle auto-advance
			m.autoAdvance = !m.autoAdvance
			m.lastStepTime = time.Now()
		case "enter", " ":
			// Manual step advance
			if m.currentStep < m.totalSteps {
				m.executeCurrentStep()
				m.currentStep++
				m.lastStepTime = time.Now()
			}
		}

	case tickMsg:
		// Auto-advance if enabled
		if m.autoAdvance && time.Since(m.lastStepTime) >= m.stepDelay {
			if m.currentStep < m.totalSteps {
				m.executeCurrentStep()
				m.currentStep++
				m.lastStepTime = time.Now()
			}
		}

		// Continue ticking
		cmds = append(cmds, tea.Tick(time.Millisecond*100, func(t time.Time) tea.Msg {
			return tickMsg(t)
		}))

	case ImportProgressMsg:
		var cmd tea.Cmd
		m.progress, cmd = m.progress.Update(msg)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	}

	// Update progress component
	var cmd tea.Cmd
	m.progress, cmd = m.progress.Update(msg)
	if cmd != nil {
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

// executeCurrentStep executes the current demo step
func (m *ImportProgressDemoModel) executeCurrentStep() {
	if m.currentStep < len(m.demoSteps) {
		step := m.demoSteps[m.currentStep]
		step.action(m)
	}
}

// View renders the demo
func (m ImportProgressDemoModel) View() string {
	var sections []string

	// Demo title
	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("205")).
		Render("🔄 Import Progress Component Demo")
	sections = append(sections, title)

	// Demo controls
	controls := lipgloss.NewStyle().
		Foreground(lipgloss.Color("241")).
		Render("Controls: SPACE/ENTER=Next Step, A=Auto-advance, R=Reset, Q=Quit")
	sections = append(sections, controls)

	// Current step info
	if m.currentStep < len(m.demoSteps) {
		step := m.demoSteps[m.currentStep]
		stepInfo := fmt.Sprintf("Step %d/%d: %s - %s",
			m.currentStep+1, m.totalSteps, step.name, step.description)
		sections = append(sections, lipgloss.NewStyle().
			Foreground(lipgloss.Color("33")).
			Render(stepInfo))
	} else {
		sections = append(sections, lipgloss.NewStyle().
			Foreground(lipgloss.Color("46")).
			Render("Demo completed! Press R to reset or Q to quit."))
	}

	// Auto-advance status
	autoStatus := "Manual"
	if m.autoAdvance {
		autoStatus = "Auto"
	}
	sections = append(sections, lipgloss.NewStyle().
		Foreground(lipgloss.Color("241")).
		Render(fmt.Sprintf("Mode: %s", autoStatus)))

	sections = append(sections, "")

	// Progress component
	progressView := m.progress.View()
	if progressView != "" {
		sections = append(sections, progressView)
	} else {
		sections = append(sections, lipgloss.NewStyle().
			Foreground(lipgloss.Color("241")).
			Render("Progress component will appear here..."))
	}

	// Demo information
	sections = append(sections, "")
	sections = append(sections, lipgloss.NewStyle().
		Foreground(lipgloss.Color("241")).
		Render("This demo showcases the Import Progress Component features:"))

	features := []string{
		"• Real-time progress tracking with animated progress bar",
		"• Current file display during processing",
		"• Pause/resume functionality for password input",
		"• Error categorization (failed vs skipped)",
		"• Completion summary with statistics and timing",
		"• Visual status indicators for different states",
	}

	for _, feature := range features {
		sections = append(sections, lipgloss.NewStyle().
			Foreground(lipgloss.Color("241")).
			Render(feature))
	}

	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

// RunImportProgressDemo runs the import progress component demo
func RunImportProgressDemo() error {
	model := NewImportProgressDemoModel()

	p := tea.NewProgram(model, tea.WithAltScreen())
	_, err := p.Run()
	return err
}
