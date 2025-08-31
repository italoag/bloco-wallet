package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/dustin/go-humanize"
)

var lastEnhancedID int64

func nextEnhancedID() int {
	return int(atomic.AddInt64(&lastEnhancedID, 1))
}

// EnhancedFilePickerModel represents an enhanced file picker with multi-selection capabilities
type EnhancedFilePickerModel struct {
	id int

	// Current directory being browsed
	CurrentDirectory string

	// Multi-selection support
	MultiSelect   bool
	SelectedFiles []string
	SelectedDir   string
	selectedItems map[string]bool // Track selected items by path

	// File filtering
	AllowedTypes []string
	FileFilter   func(string) bool
	ShowHidden   bool
	DirAllowed   bool
	FileAllowed  bool

	// Navigation state
	files         []os.DirEntry
	cursor        int
	viewportStart int
	viewportEnd   int
	height        int
	autoHeight    bool

	// Visual styling
	KeyMap EnhancedFilePickerKeyMap
	Styles EnhancedFilePickerStyles

	// Navigation stack for directory traversal
	selectedStack []int
	minStack      []int
	maxStack      []int

	// State tracking
	confirmed bool
	cancelled bool
}

// EnhancedFilePickerKeyMap defines key bindings for the enhanced file picker
type EnhancedFilePickerKeyMap struct {
	Up        key.Binding
	Down      key.Binding
	PageUp    key.Binding
	PageDown  key.Binding
	GoToTop   key.Binding
	GoToLast  key.Binding
	Left      key.Binding
	Right     key.Binding
	Enter     key.Binding
	Space     key.Binding
	SelectAll key.Binding
	ClearAll  key.Binding
	Back      key.Binding
	Confirm   key.Binding
	Cancel    key.Binding
}

// DefaultEnhancedFilePickerKeyMap returns the default key bindings
func DefaultEnhancedFilePickerKeyMap() EnhancedFilePickerKeyMap {
	return EnhancedFilePickerKeyMap{
		Up:        key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
		Down:      key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
		PageUp:    key.NewBinding(key.WithKeys("pgup", "ctrl+u"), key.WithHelp("pgup", "page up")),
		PageDown:  key.NewBinding(key.WithKeys("pgdown", "ctrl+d"), key.WithHelp("pgdn", "page down")),
		GoToTop:   key.NewBinding(key.WithKeys("g"), key.WithHelp("g", "top")),
		GoToLast:  key.NewBinding(key.WithKeys("G"), key.WithHelp("G", "bottom")),
		Left:      key.NewBinding(key.WithKeys("left", "h"), key.WithHelp("←/h", "back")),
		Right:     key.NewBinding(key.WithKeys("right", "l"), key.WithHelp("→/l", "open")),
		Enter:     key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open/confirm")),
		Space:     key.NewBinding(key.WithKeys(" "), key.WithHelp("space", "select")),
		SelectAll: key.NewBinding(key.WithKeys("ctrl+a"), key.WithHelp("ctrl+a", "select all")),
		ClearAll:  key.NewBinding(key.WithKeys("ctrl+c"), key.WithHelp("ctrl+c", "clear all")),
		Back:      key.NewBinding(key.WithKeys("backspace", "esc"), key.WithHelp("esc", "back")),
		Confirm:   key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "confirm selection")),
		Cancel:    key.NewBinding(key.WithKeys("ctrl+q"), key.WithHelp("ctrl+q", "cancel")),
	}
}

// EnhancedFilePickerStyles defines styling for the enhanced file picker
type EnhancedFilePickerStyles struct {
	Cursor           lipgloss.Style
	Directory        lipgloss.Style
	File             lipgloss.Style
	DisabledFile     lipgloss.Style
	Selected         lipgloss.Style
	SelectedFile     lipgloss.Style
	SelectedDir      lipgloss.Style
	Checkbox         lipgloss.Style
	CheckboxSelected lipgloss.Style
	FileSize         lipgloss.Style
	Permission       lipgloss.Style
	EmptyDirectory   lipgloss.Style
	Header           lipgloss.Style
	Footer           lipgloss.Style
	Instructions     lipgloss.Style
}

// DefaultEnhancedFilePickerStyles returns default styling
func DefaultEnhancedFilePickerStyles() EnhancedFilePickerStyles {
	return DefaultEnhancedFilePickerStylesWithRenderer(lipgloss.DefaultRenderer())
}

// DefaultEnhancedFilePickerStylesWithRenderer returns default styling with a specific renderer
func DefaultEnhancedFilePickerStylesWithRenderer(r *lipgloss.Renderer) EnhancedFilePickerStyles {
	return EnhancedFilePickerStyles{
		Cursor:           r.NewStyle().Foreground(lipgloss.Color("212")),
		Directory:        r.NewStyle().Foreground(lipgloss.Color("99")).Bold(true),
		File:             r.NewStyle().Foreground(lipgloss.Color("252")),
		DisabledFile:     r.NewStyle().Foreground(lipgloss.Color("243")),
		Selected:         r.NewStyle().Background(lipgloss.Color("238")),
		SelectedFile:     r.NewStyle().Foreground(lipgloss.Color("212")).Bold(true),
		SelectedDir:      r.NewStyle().Foreground(lipgloss.Color("99")).Bold(true).Background(lipgloss.Color("238")),
		Checkbox:         r.NewStyle().Foreground(lipgloss.Color("244")),
		CheckboxSelected: r.NewStyle().Foreground(lipgloss.Color("70")).Bold(true),
		FileSize:         r.NewStyle().Foreground(lipgloss.Color("240")).Width(8).Align(lipgloss.Right),
		Permission:       r.NewStyle().Foreground(lipgloss.Color("244")),
		EmptyDirectory:   r.NewStyle().Foreground(lipgloss.Color("240")).Italic(true),
		Header:           r.NewStyle().Bold(true).Foreground(lipgloss.Color("99")),
		Footer:           r.NewStyle().Foreground(lipgloss.Color("244")),
		Instructions:     r.NewStyle().Foreground(lipgloss.Color("244")).Italic(true),
	}
}

// FilePickerResult represents the result of file picker interaction
type FilePickerResult struct {
	Files     []string
	Directory string
	Confirmed bool
	Cancelled bool
}

// NewEnhancedFilePicker creates a new enhanced file picker
func NewEnhancedFilePicker() EnhancedFilePickerModel {
	return EnhancedFilePickerModel{
		id:               nextEnhancedID(),
		CurrentDirectory: ".",
		MultiSelect:      true,
		SelectedFiles:    []string{},
		selectedItems:    make(map[string]bool),
		AllowedTypes:     []string{".json"},
		ShowHidden:       false,
		DirAllowed:       true,
		FileAllowed:      true,
		height:           20,
		autoHeight:       true,
		KeyMap:           DefaultEnhancedFilePickerKeyMap(),
		Styles:           DefaultEnhancedFilePickerStyles(),
	}
}

// SetHeight sets the height of the file picker
func (m *EnhancedFilePickerModel) SetHeight(height int) {
	m.height = height
	m.updateViewport()
}

// SetFileFilter sets a custom file filter function
func (m *EnhancedFilePickerModel) SetFileFilter(filter func(string) bool) {
	m.FileFilter = filter
}

// SetAllowedTypes sets the allowed file extensions
func (m *EnhancedFilePickerModel) SetAllowedTypes(types []string) {
	m.AllowedTypes = types
}

// IsSelected checks if a file is selected
func (m *EnhancedFilePickerModel) IsSelected(path string) bool {
	return m.selectedItems[path]
}

// ToggleSelection toggles selection of the current item
func (m *EnhancedFilePickerModel) ToggleSelection() {
	if len(m.files) == 0 {
		return
	}

	currentFile := m.files[m.cursor]
	fullPath := filepath.Join(m.CurrentDirectory, currentFile.Name())

	if m.selectedItems[fullPath] {
		delete(m.selectedItems, fullPath)
		// Remove from SelectedFiles slice
		for i, file := range m.SelectedFiles {
			if file == fullPath {
				m.SelectedFiles = append(m.SelectedFiles[:i], m.SelectedFiles[i+1:]...)
				break
			}
		}
	} else {
		if currentFile.IsDir() && m.DirAllowed {
			m.selectedItems[fullPath] = true
			m.SelectedFiles = append(m.SelectedFiles, fullPath)
			m.SelectedDir = fullPath
		} else if !currentFile.IsDir() && m.FileAllowed && m.canSelectFile(currentFile.Name()) {
			m.selectedItems[fullPath] = true
			m.SelectedFiles = append(m.SelectedFiles, fullPath)
		}
	}
}

// SelectAll selects all valid files in the current directory
func (m *EnhancedFilePickerModel) SelectAll() {
	for _, file := range m.files {
		fullPath := filepath.Join(m.CurrentDirectory, file.Name())
		if file.IsDir() && m.DirAllowed {
			m.selectedItems[fullPath] = true
			if !m.containsPath(m.SelectedFiles, fullPath) {
				m.SelectedFiles = append(m.SelectedFiles, fullPath)
			}
		} else if !file.IsDir() && m.FileAllowed && m.canSelectFile(file.Name()) {
			m.selectedItems[fullPath] = true
			if !m.containsPath(m.SelectedFiles, fullPath) {
				m.SelectedFiles = append(m.SelectedFiles, fullPath)
			}
		}
	}
}

// ClearAll clears all selections
func (m *EnhancedFilePickerModel) ClearAll() {
	m.selectedItems = make(map[string]bool)
	m.SelectedFiles = []string{}
	m.SelectedDir = ""
}

// containsPath checks if a path exists in a slice
func (m *EnhancedFilePickerModel) containsPath(paths []string, path string) bool {
	for _, p := range paths {
		if p == path {
			return true
		}
	}
	return false
}

// canSelectFile checks if a file can be selected based on filters
func (m *EnhancedFilePickerModel) canSelectFile(filename string) bool {
	// Apply custom filter if provided
	if m.FileFilter != nil {
		return m.FileFilter(filename)
	}

	// Apply allowed types filter
	if len(m.AllowedTypes) == 0 {
		return true
	}

	for _, ext := range m.AllowedTypes {
		if strings.HasSuffix(strings.ToLower(filename), strings.ToLower(ext)) {
			return true
		}
	}
	return false
}

// updateViewport updates the viewport boundaries
func (m *EnhancedFilePickerModel) updateViewport() {
	if m.height <= 0 {
		return
	}

	// Reserve space for header and footer
	contentHeight := m.height - 4 // Header (2 lines) + Footer (2 lines)
	if contentHeight <= 0 {
		contentHeight = 1
	}

	m.viewportEnd = m.viewportStart + contentHeight - 1
	if m.viewportEnd >= len(m.files) {
		m.viewportEnd = len(m.files) - 1
	}

	// Adjust viewport to keep cursor visible
	if m.cursor < m.viewportStart {
		m.viewportStart = m.cursor
		m.viewportEnd = m.viewportStart + contentHeight - 1
		if m.viewportEnd >= len(m.files) {
			m.viewportEnd = len(m.files) - 1
		}
	} else if m.cursor > m.viewportEnd {
		m.viewportEnd = m.cursor
		m.viewportStart = m.viewportEnd - contentHeight + 1
		if m.viewportStart < 0 {
			m.viewportStart = 0
		}
	}
}

type enhancedReadDirMsg struct {
	id      int
	entries []os.DirEntry
}

type enhancedErrorMsg struct {
	err error
}

// readDir reads the directory contents
func (m EnhancedFilePickerModel) readDir(path string) tea.Cmd {
	return func() tea.Msg {
		entries, err := os.ReadDir(path)
		if err != nil {
			return enhancedErrorMsg{err}
		}

		// Sort entries: directories first, then files, both alphabetically
		sort.Slice(entries, func(i, j int) bool {
			if entries[i].IsDir() != entries[j].IsDir() {
				return entries[i].IsDir()
			}
			return strings.ToLower(entries[i].Name()) < strings.ToLower(entries[j].Name())
		})

		// Filter hidden files if needed
		if !m.ShowHidden {
			var filtered []os.DirEntry
			for _, entry := range entries {
				if !strings.HasPrefix(entry.Name(), ".") {
					filtered = append(filtered, entry)
				}
			}
			entries = filtered
		}

		return enhancedReadDirMsg{id: m.id, entries: entries}
	}
}

// Init initializes the enhanced file picker
func (m EnhancedFilePickerModel) Init() tea.Cmd {
	return m.readDir(m.CurrentDirectory)
}

// Update handles messages and updates the model
func (m EnhancedFilePickerModel) Update(msg tea.Msg) (EnhancedFilePickerModel, tea.Cmd) {
	switch msg := msg.(type) {
	case enhancedReadDirMsg:
		if msg.id != m.id {
			break
		}
		m.files = msg.entries
		m.cursor = 0
		m.viewportStart = 0
		m.updateViewport()

	case tea.WindowSizeMsg:
		if m.autoHeight {
			m.SetHeight(msg.Height - 6) // Reserve space for other UI elements
		}

	case tea.KeyMsg:
		switch {
		case key.Matches(msg, m.KeyMap.Up):
			if m.cursor > 0 {
				m.cursor--
				m.updateViewport()
			}

		case key.Matches(msg, m.KeyMap.Down):
			if m.cursor < len(m.files)-1 {
				m.cursor++
				m.updateViewport()
			}

		case key.Matches(msg, m.KeyMap.PageUp):
			contentHeight := m.height - 4
			m.cursor -= contentHeight
			if m.cursor < 0 {
				m.cursor = 0
			}
			m.updateViewport()

		case key.Matches(msg, m.KeyMap.PageDown):
			contentHeight := m.height - 4
			m.cursor += contentHeight
			if m.cursor >= len(m.files) {
				m.cursor = len(m.files) - 1
			}
			m.updateViewport()

		case key.Matches(msg, m.KeyMap.GoToTop):
			m.cursor = 0
			m.updateViewport()

		case key.Matches(msg, m.KeyMap.GoToLast):
			m.cursor = len(m.files) - 1
			m.updateViewport()

		case key.Matches(msg, m.KeyMap.Space):
			if m.MultiSelect {
				m.ToggleSelection()
			}

		case key.Matches(msg, m.KeyMap.SelectAll):
			if m.MultiSelect {
				m.SelectAll()
			}

		case key.Matches(msg, m.KeyMap.ClearAll):
			if m.MultiSelect {
				m.ClearAll()
			}

		case key.Matches(msg, m.KeyMap.Enter), key.Matches(msg, m.KeyMap.Right):
			if len(m.files) == 0 {
				break
			}

			currentFile := m.files[m.cursor]
			if currentFile.IsDir() {
				// Navigate into directory
				m.selectedStack = append(m.selectedStack, m.cursor)
				m.minStack = append(m.minStack, m.viewportStart)
				m.maxStack = append(m.maxStack, m.viewportEnd)

				m.CurrentDirectory = filepath.Join(m.CurrentDirectory, currentFile.Name())
				return m, m.readDir(m.CurrentDirectory)
			} else if !m.MultiSelect {
				// Single selection mode - select file and confirm
				fullPath := filepath.Join(m.CurrentDirectory, currentFile.Name())
				if m.canSelectFile(currentFile.Name()) {
					m.SelectedFiles = []string{fullPath}
					m.confirmed = true
				}
			}

		case key.Matches(msg, m.KeyMap.Back), key.Matches(msg, m.KeyMap.Left):
			// Navigate back to parent directory
			if m.CurrentDirectory != "." && m.CurrentDirectory != "/" {
				m.CurrentDirectory = filepath.Dir(m.CurrentDirectory)

				// Restore previous cursor position if available
				if len(m.selectedStack) > 0 {
					m.cursor = m.selectedStack[len(m.selectedStack)-1]
					m.viewportStart = m.minStack[len(m.minStack)-1]
					m.viewportEnd = m.maxStack[len(m.maxStack)-1]

					m.selectedStack = m.selectedStack[:len(m.selectedStack)-1]
					m.minStack = m.minStack[:len(m.minStack)-1]
					m.maxStack = m.maxStack[:len(m.maxStack)-1]
				} else {
					m.cursor = 0
					m.viewportStart = 0
				}

				return m, m.readDir(m.CurrentDirectory)
			}

		case key.Matches(msg, m.KeyMap.Confirm):
			m.confirmed = true

		case key.Matches(msg, m.KeyMap.Cancel):
			m.cancelled = true
		}
	}

	return m, nil
}

// View renders the enhanced file picker
func (m EnhancedFilePickerModel) View() string {
	if len(m.files) == 0 {
		return m.Styles.EmptyDirectory.Render("No files found in this directory")
	}

	var content strings.Builder

	// Header
	header := fmt.Sprintf("📁 %s", m.CurrentDirectory)
	if m.MultiSelect && len(m.SelectedFiles) > 0 {
		header += fmt.Sprintf(" (%d selected)", len(m.SelectedFiles))
	}
	content.WriteString(m.Styles.Header.Render(header))
	content.WriteString("\n\n")

	// File list
	for i, file := range m.files {
		if i < m.viewportStart || i > m.viewportEnd {
			continue
		}

		fullPath := filepath.Join(m.CurrentDirectory, file.Name())
		isSelected := m.selectedItems[fullPath]
		isCursor := i == m.cursor

		var line strings.Builder

		// Cursor indicator
		if isCursor {
			line.WriteString(m.Styles.Cursor.Render("> "))
		} else {
			line.WriteString("  ")
		}

		// Checkbox for multi-select
		if m.MultiSelect {
			if isSelected {
				line.WriteString(m.Styles.CheckboxSelected.Render("☑ "))
			} else {
				line.WriteString(m.Styles.Checkbox.Render("☐ "))
			}
		}

		// File/directory icon and name
		var nameStyle lipgloss.Style
		var icon string

		if file.IsDir() {
			icon = "📁 "
			if isCursor && isSelected {
				nameStyle = m.Styles.SelectedDir
			} else if isCursor {
				nameStyle = m.Styles.Directory.Copy().Background(lipgloss.Color("238"))
			} else if isSelected {
				nameStyle = m.Styles.SelectedDir
			} else {
				nameStyle = m.Styles.Directory
			}
		} else {
			icon = "📄 "
			canSelect := m.canSelectFile(file.Name())
			if !canSelect {
				nameStyle = m.Styles.DisabledFile
			} else if isCursor && isSelected {
				nameStyle = m.Styles.SelectedFile
			} else if isCursor {
				nameStyle = m.Styles.File.Copy().Background(lipgloss.Color("238"))
			} else if isSelected {
				nameStyle = m.Styles.SelectedFile
			} else {
				nameStyle = m.Styles.File
			}
		}

		line.WriteString(icon)
		line.WriteString(nameStyle.Render(file.Name()))

		// File size for files
		if !file.IsDir() {
			if info, err := file.Info(); err == nil {
				size := humanize.Bytes(uint64(info.Size()))
				line.WriteString(" ")
				line.WriteString(m.Styles.FileSize.Render(size))
			}
		}

		content.WriteString(line.String())
		content.WriteString("\n")
	}

	// Footer with instructions
	content.WriteString("\n")
	instructions := []string{}
	if m.MultiSelect {
		instructions = append(instructions, "Space: select", "Tab: confirm", "Ctrl+A: select all", "Ctrl+C: clear")
	}
	instructions = append(instructions, "Enter: open", "Esc: back", "Ctrl+Q: cancel")

	content.WriteString(m.Styles.Instructions.Render(strings.Join(instructions, " • ")))

	return content.String()
}

// GetResult returns the current selection result
func (m EnhancedFilePickerModel) GetResult() FilePickerResult {
	return FilePickerResult{
		Files:     m.SelectedFiles,
		Directory: m.SelectedDir,
		Confirmed: m.confirmed,
		Cancelled: m.cancelled,
	}
}

// IsConfirmed returns true if the user confirmed the selection
func (m EnhancedFilePickerModel) IsConfirmed() bool {
	return m.confirmed
}

// IsCancelled returns true if the user cancelled the selection
func (m EnhancedFilePickerModel) IsCancelled() bool {
	return m.cancelled
}
