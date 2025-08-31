package ui

import (
	"blocowallet/internal/blockchain"
	"blocowallet/pkg/config"
	"blocowallet/pkg/localization"
	"fmt"
	"strconv"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// NetworkListComponent represents the network list component
type NetworkListComponent struct {
	id     string
	width  int
	height int
	table  table.Model
	err    error

	// Cached classification info to avoid network calls during View rendering
	networksInfo map[string]NetworkInfo

	// Network service
	chainListService *blockchain.ChainListService
}

// NewNetworkListComponent creates a new network list component
func NewNetworkListComponent() NetworkListComponent {
	c := NetworkListComponent{
		id:               "network-list",
		chainListService: blockchain.NewChainListService(),
		networksInfo:     make(map[string]NetworkInfo),
	}
	c.initTable()
	return c
}

// initTable initializes the table with empty rows
func (c *NetworkListComponent) initTable() {
	columns := []table.Column{
		{Title: "#", Width: 4},
		{Title: localization.Labels["network_name"], Width: 18},
		{Title: "Type", Width: 12},
		{Title: localization.Labels["chain_id"], Width: 10},
		{Title: localization.Labels["symbol"], Width: 8},
		{Title: localization.Labels["status"], Width: 10},
		{Title: "Key", Width: 0}, // Hidden column for network key
	}

	var rows []table.Row

	t := table.New(
		table.WithColumns(columns),
		table.WithRows(rows),
		table.WithFocused(true),
		table.WithHeight(10),
	)

	s := table.DefaultStyles()
	s.Header = s.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("240")).
		BorderBottom(true).
		Bold(true)
	s.Selected = s.Selected.
		Foreground(lipgloss.Color("229")).
		Background(lipgloss.Color("57")).
		Bold(true)
	t.SetStyles(s)

	c.table = t
}

// SetSize updates the component size
func (c *NetworkListComponent) SetSize(width, height int) {
	c.width = width
	c.height = height

	// Only set the table height and width if there are rows to display
	// This prevents "index out of range" errors when the table is empty
	rows := c.table.Rows()
	if len(rows) > 0 {
		c.table.SetHeight(height / 3)
		c.table.SetWidth(width - 10)
	}
}

// SetError sets an error state
func (c *NetworkListComponent) SetError(err error) {
	c.err = err
}

// UpdateNetworks updates the table with networks from the configuration
func (c *NetworkListComponent) UpdateNetworks(cfg *config.Config) {
	// Ensure cfg and cfg.Networks are not nil
	if cfg == nil || cfg.Networks == nil {
		return
	}

	// Get network manager to retrieve classification information (once)
	nm := getNetworkManager()
	networksWithInfo, err := nm.ListNetworks()
	if err != nil {
		c.SetError(fmt.Errorf("failed to load network information: %v", err))
		return
	}
	// Cache to avoid repeated network calls during table navigation/render
	c.networksInfo = networksWithInfo

	var rows []table.Row

	i := 1
	for key, network := range cfg.Networks {
		status := localization.Labels["inactive"]
		if network.IsActive {
			status = localization.Labels["active"]
		}

		// Get network type and source information
		networkType := "Custom"
		typeIcon := "🔧"
		if networkInfo, exists := networksWithInfo[key]; exists {
			switch networkInfo.Type {
			case blockchain.NetworkTypeStandard:
				networkType = "Standard"
				typeIcon = "✅"
				if networkInfo.IsValidated {
					networkType = "Standard ✓"
				}
			case blockchain.NetworkTypeCustom:
				networkType = "Custom"
				typeIcon = "🔧"
			}
		}

		rows = append(rows, table.Row{
			strconv.Itoa(i),
			network.Name,
			fmt.Sprintf("%s %s", typeIcon, networkType),
			strconv.FormatInt(network.ChainID, 10),
			network.Symbol,
			status,
			key, // Hidden column for network key
		})
		i++
	}

	c.table.SetRows(rows)

	// Only set the cursor if there are rows
	if len(rows) > 0 {
		c.table.SetCursor(0)
	}
}

// GetSelectedNetworkKey returns the key of the selected network
func (c *NetworkListComponent) GetSelectedNetworkKey() string {
	rows := c.table.Rows()
	if len(rows) == 0 {
		return ""
	}

	selectedRow := c.table.SelectedRow()
	if len(selectedRow) < 7 {
		return ""
	}

	return selectedRow[6] // Network key is stored in the hidden column (now index 6)
}

// GetSelectedNetworkInfo returns detailed information about the selected network
func (c *NetworkListComponent) GetSelectedNetworkInfo() (*NetworkInfo, error) {
	key := c.GetSelectedNetworkKey()
	if key == "" {
		return nil, fmt.Errorf("no network selected")
	}

	// Use cached info to avoid network calls during navigation/render
	if c.networksInfo != nil {
		if networkInfo, exists := c.networksInfo[key]; exists {
			return &networkInfo, nil
		}
	}

	return nil, fmt.Errorf("network information not found")
}

// Init initializes the component
func (c *NetworkListComponent) Init() tea.Cmd {
	return nil
}

// Update handles messages for the network list component
func (c *NetworkListComponent) Update(msg tea.Msg) (*NetworkListComponent, tea.Cmd) {
	var cmd tea.Cmd
	c.table, cmd = c.table.Update(msg)
	return c, cmd
}

// View renders the network list component
func (c *NetworkListComponent) View() string {
	var content string

	// Header
	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#FFF")).
		Background(lipgloss.Color("#874BFD")).
		MarginLeft(2).
		MarginBottom(1)
	content = headerStyle.Render("🌐 " + localization.Labels["networks"])
	content += "\n\n"

	// Table
	rows := c.table.Rows()
	if len(rows) > 0 {
		content += c.table.View()
	} else {
		content += "No networks found. Add a network to get started."
	}
	content += "\n\n"

	// Error message
	if c.err != nil {
		errorStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FF0000")).
			MarginLeft(2)
		content += errorStyle.Render(fmt.Sprintf("❌ %s", c.err.Error()))
		content += "\n\n"
	}

	// Selected network details
	if len(rows) > 0 {
		selectedNetworkInfo, err := c.GetSelectedNetworkInfo()
		if err == nil && selectedNetworkInfo != nil {
			detailStyle := lipgloss.NewStyle().
				Foreground(lipgloss.Color("#DDDDDD")).
				Background(lipgloss.Color("#333333")).
				Padding(0, 1).
				MarginLeft(2).
				MarginBottom(1)

			var details string
			switch selectedNetworkInfo.Type {
			case blockchain.NetworkTypeStandard:
				details = fmt.Sprintf("📋 Selected: Standard Network (Source: %s)", selectedNetworkInfo.Source)
				if selectedNetworkInfo.ChainInfo != nil {
					details += fmt.Sprintf(" • Verified on ChainList as '%s'", selectedNetworkInfo.ChainInfo.Name)
				}
			case blockchain.NetworkTypeCustom:
				details = fmt.Sprintf("📋 Selected: Custom Network (Source: %s)", selectedNetworkInfo.Source)
				if !selectedNetworkInfo.IsValidated {
					details += " • Not verified on ChainList"
				}
			}
			content += detailStyle.Render(details)
			content += "\n"
		}
	}

	// Network type legend
	legendStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#888888")).
		MarginLeft(2).
		MarginBottom(1)

	legend := "Network Types: ✅ Standard (ChainList verified) • 🔧 Custom (Manual configuration)"
	content += legendStyle.Render(legend)
	content += "\n"

	// Instructions
	infoStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#666666")).
		MarginLeft(2)

	content += infoStyle.Render(localization.Labels["network_list_instructions"])
	content += "\n"

	// Footer
	footerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#874BFD")).
		MarginTop(1)

	footer := footerStyle.Render("a: " + localization.Labels["add_network"] + " • ")
	footer += footerStyle.Render("e: " + localization.Labels["edit_network"] + " • ")
	footer += footerStyle.Render("d: " + localization.Labels["delete_network"] + " • ")
	footer += footerStyle.Render("esc: " + localization.Labels["back"])

	content += footer

	return content
}

// networkAddedMsg is sent when a network is added
type networkAddedMsg struct{}

// BackToNetworkListMsg is sent to return to the network list
type BackToNetworkListMsg struct{}

// BackToNetworkMenuMsg is sent to return to the network menu
type BackToNetworkMenuMsg struct{}
