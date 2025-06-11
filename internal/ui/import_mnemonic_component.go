package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
)

// ImportMnemonicComponent represents the mnemonic import component
type ImportMnemonicComponent struct {
	id        string
	form      *huh.Form
	width     int
	height    int
	err       error
	importing bool

	// Form values
	walletName                                  string
	word1, word2, word3, word4, word5, word6    string
	word7, word8, word9, word10, word11, word12 string
	password                                    string
}

// NewImportMnemonicComponent creates a new mnemonic import component
func NewImportMnemonicComponent() ImportMnemonicComponent {
	c := ImportMnemonicComponent{
		id: "import-mnemonic",
	}
	c.initForm()
	return c
}

// initForm initializes the huh form with proper layout for mnemonic words
func (c *ImportMnemonicComponent) initForm() {
	c.form = huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Key("walletName").
				Title("Wallet Name").
				Placeholder("Enter wallet name...").
				Value(&c.walletName),
			huh.NewInput().
				Key("password").
				Title("Password").
				Placeholder("Enter password...").
				EchoMode(huh.EchoModePassword).
				Value(&c.password),
		),
		huh.NewGroup(
			huh.NewInput().
				Key("word1").
				Title("Word 1").
				Placeholder("1st word").
				Value(&c.word1),
			huh.NewInput().
				Key("word2").
				Title("Word 2").
				Placeholder("2nd word").
				Value(&c.word2),
			huh.NewInput().
				Key("word3").
				Title("Word 3").
				Placeholder("3rd word").
				Value(&c.word3),
			huh.NewInput().
				Key("word4").
				Title("Word 4").
				Placeholder("4th word").
				Value(&c.word4),
			huh.NewInput().
				Key("word5").
				Title("Word 5").
				Placeholder("5th word").
				Value(&c.word5),
			huh.NewInput().
				Key("word6").
				Title("Word 6").
				Placeholder("6th word").
				Value(&c.word6),
		),
		huh.NewGroup(
			huh.NewInput().
				Key("word7").
				Title("Word 7").
				Placeholder("7th word").
				Value(&c.word7),
			huh.NewInput().
				Key("word8").
				Title("Word 8").
				Placeholder("8th word").
				Value(&c.word8),
			huh.NewInput().
				Key("word9").
				Title("Word 9").
				Placeholder("9th word").
				Value(&c.word9),
			huh.NewInput().
				Key("word10").
				Title("Word 10").
				Placeholder("10th word").
				Value(&c.word10),
			huh.NewInput().
				Key("word11").
				Title("Word 11").
				Placeholder("11th word").
				Value(&c.word11),
			huh.NewInput().
				Key("word12").
				Title("Word 12").
				Placeholder("12th word").
				Value(&c.word12),
		),
	).WithWidth(80).WithShowHelp(false).WithShowErrors(false).WithLayout(huh.LayoutColumns(2))
}

// SetSize updates the component size
func (c *ImportMnemonicComponent) SetSize(width, height int) {
	c.width = width
	c.height = height
}

// SetError sets an error state
func (c *ImportMnemonicComponent) SetError(err error) {
	c.err = err
	c.importing = false
}

// SetImporting sets the importing state
func (c *ImportMnemonicComponent) SetImporting(importing bool) {
	c.importing = importing
	if importing {
		c.err = nil
	}
}

// GetWalletName returns the entered wallet name
func (c *ImportMnemonicComponent) GetWalletName() string {
	if c.form != nil {
		return strings.TrimSpace(c.form.GetString("walletName"))
	}
	return strings.TrimSpace(c.walletName)
}

// GetMnemonic returns the entered mnemonic
func (c *ImportMnemonicComponent) GetMnemonic() string {
	if c.form != nil {
		// Try to get values from form first
		words := []string{
			c.form.GetString("word1"), c.form.GetString("word2"), c.form.GetString("word3"),
			c.form.GetString("word4"), c.form.GetString("word5"), c.form.GetString("word6"),
			c.form.GetString("word7"), c.form.GetString("word8"), c.form.GetString("word9"),
			c.form.GetString("word10"), c.form.GetString("word11"), c.form.GetString("word12"),
		}
		
		// Check if any form values are present
		hasFormValues := false
		for _, word := range words {
			if strings.TrimSpace(word) != "" {
				hasFormValues = true
				break
			}
		}
		
		// If form has values, use them; otherwise fall back to component variables
		if hasFormValues {
			return strings.Join(words, " ")
		}
	}
	
	// Fallback to component variables (for tests and when form is empty)
	words := []string{
		c.word1, c.word2, c.word3, c.word4, c.word5, c.word6,
		c.word7, c.word8, c.word9, c.word10, c.word11, c.word12,
	}
	return strings.Join(words, " ")
}

// GetPassword returns the entered password
func (c *ImportMnemonicComponent) GetPassword() string {
	if c.form != nil {
		return strings.TrimSpace(c.form.GetString("password"))
	}
	return strings.TrimSpace(c.password)
}

// Reset clears all inputs
func (c *ImportMnemonicComponent) Reset() {
	c.walletName = ""
	c.word1, c.word2, c.word3, c.word4, c.word5, c.word6 = "", "", "", "", "", ""
	c.word7, c.word8, c.word9, c.word10, c.word11, c.word12 = "", "", "", "", "", ""
	c.password = ""
	c.err = nil
	c.importing = false
	c.initForm()
}

// Init initializes the component
func (c *ImportMnemonicComponent) Init() tea.Cmd {
	// Initialize the form so input fields are ready
	return c.form.Init()
}

// Update handles messages for the import mnemonic component
func (c *ImportMnemonicComponent) Update(msg tea.Msg) (*ImportMnemonicComponent, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		c.width = msg.Width
		c.height = msg.Height

	case walletCreatedMsg:
		c.Reset()
		return c, func() tea.Msg { return BackToMenuMsg{} }

	case errorMsg:
		c.SetError(fmt.Errorf("%s", string(msg)))
	}

	// Update the form first (allows typing and internal navigation)
	form, cmd := c.form.Update(msg)
	if f, ok := form.(*huh.Form); ok {
		c.form = f
		cmds = append(cmds, cmd)
	}

	// Only handle escape if form didn't handle it (when form is not focused on input)
	if keyMsg, ok := msg.(tea.KeyMsg); ok && keyMsg.String() == "esc" && c.form.State == huh.StateNormal {
		return c, func() tea.Msg { return BackToMenuMsg{} }
	}

	// Check if form is completed
	if c.form.State == huh.StateCompleted && !c.importing {
		// Get values directly from form instead of variables
		walletName := strings.TrimSpace(c.form.GetString("walletName"))
		password := strings.TrimSpace(c.form.GetString("password"))
		
		// Get mnemonic words from form
		words := []string{
			c.form.GetString("word1"), c.form.GetString("word2"), c.form.GetString("word3"),
			c.form.GetString("word4"), c.form.GetString("word5"), c.form.GetString("word6"),
			c.form.GetString("word7"), c.form.GetString("word8"), c.form.GetString("word9"),
			c.form.GetString("word10"), c.form.GetString("word11"), c.form.GetString("word12"),
		}
		mnemonic := strings.Join(words, " ")

		if c.validateInputsFromForm(walletName, mnemonic, password) {
			c.importing = true
			return c, func() tea.Msg {
				return ImportMnemonicRequestMsg{
					Name:     walletName,
					Mnemonic: mnemonic,
					Password: password,
				}
			}
		}
		// Reset form state if validation failed
		c.form.State = huh.StateNormal
	}

	return c, tea.Batch(cmds...)
}

// validateInputs checks if the inputs are valid (legacy method using component variables)
func (c *ImportMnemonicComponent) validateInputs() bool {
	words := []string{
		c.word1, c.word2, c.word3, c.word4, c.word5, c.word6,
		c.word7, c.word8, c.word9, c.word10, c.word11, c.word12,
	}
	mnemonic := strings.Join(words, " ")
	return c.validateInputsFromForm(c.walletName, mnemonic, c.password)
}

// validateInputsFromForm checks if the provided inputs are valid
func (c *ImportMnemonicComponent) validateInputsFromForm(walletName, mnemonic, password string) bool {
	if strings.TrimSpace(walletName) == "" {
		c.err = fmt.Errorf("Wallet name cannot be empty")
		return false
	}
	if strings.TrimSpace(password) == "" {
		c.err = fmt.Errorf("Password cannot be empty")
		return false
	}

	// Check if all 12 words are filled
	words := strings.Fields(strings.TrimSpace(mnemonic))
	if len(words) != 12 {
		c.err = fmt.Errorf("Mnemonic must contain exactly 12 words, got %d", len(words))
		return false
	}

	for i, word := range words {
		if strings.TrimSpace(word) == "" {
			c.err = fmt.Errorf("All 12 words must be filled (word %d is empty)", i+1)
			return false
		}
	}

	c.err = nil
	return true
}

// View renders the import mnemonic component
func (c *ImportMnemonicComponent) View() string {
	var b strings.Builder

	// Header
	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("86")).
		MarginBottom(1)
	b.WriteString(headerStyle.Render("📥 Import Wallet from Mnemonic"))
	b.WriteString("\n\n")

	// Form
	b.WriteString(c.form.View())

	// Status messages
	if c.importing {
		b.WriteString("\n")
		b.WriteString(LoadingStyle.Render("⏳ Importing wallet..."))
	} else if c.err != nil {
		b.WriteString("\n")
		b.WriteString(ErrorStyle.Render("❌ Error: " + c.err.Error()))
	}

	// Instructions
	b.WriteString("\n\n")
	b.WriteString(WarningStyle.Render("⚠️  Important: Make sure your mnemonic phrase is correct!"))
	b.WriteString("\n")
	b.WriteString(InfoStyle.Render("   Enter each word in the correct order from 1-12."))
	b.WriteString("\n\n")

	// Footer
	b.WriteString(FooterStyle.Render("Tab/Arrow Keys: Navigate • Enter: Import • Esc: Back"))

	return b.String()
}

// ImportMnemonicRequestMsg is sent when the user wants to import a wallet from mnemonic
type ImportMnemonicRequestMsg struct {
	Name     string
	Mnemonic string
	Password string
}
