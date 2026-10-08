package ui

import (
	"blocowallet/internal/blockchain"
	"blocowallet/pkg/config"
	"blocowallet/pkg/localization"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type NetworkListKeyMap struct {
	Up         key.Binding
	Down       key.Binding
	Add        key.Binding
	Edit       key.Binding
	Delete     key.Binding
	Refresh    key.Binding
	Revalidate key.Binding
	Back       key.Binding
	ToggleHelp key.Binding
}

func newNetworkListKeyMap() NetworkListKeyMap {
	return NetworkListKeyMap{
		Up:         key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", localization.Get("help_up"))),
		Down:       key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", localization.Get("help_down"))),
		Add:        key.NewBinding(key.WithKeys("a"), key.WithHelp("a", localization.Get("help_add_network"))),
		Edit:       key.NewBinding(key.WithKeys("e"), key.WithHelp("e", localization.Get("help_edit_network"))),
		Delete:     key.NewBinding(key.WithKeys("d"), key.WithHelp("d", localization.Get("help_delete_network"))),
		Refresh:    key.NewBinding(key.WithKeys("r"), key.WithHelp("r", localization.Get("help_refresh"))),
		Revalidate: key.NewBinding(key.WithKeys("v"), key.WithHelp("v", localization.Get("help_revalidate"))),
		Back:       key.NewBinding(key.WithKeys("esc", "backspace"), key.WithHelp("esc", localization.Get("help_back"))),
		ToggleHelp: key.NewBinding(key.WithKeys("?"), key.WithHelp("?", localization.Get("help_more"))),
	}
}

func (keyMap NetworkListKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{keyMap.Add, keyMap.Edit, keyMap.Delete, keyMap.Refresh, keyMap.Revalidate, keyMap.Back}
}

func (keyMap NetworkListKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{keyMap.Up, keyMap.Down}, {keyMap.Add, keyMap.Edit, keyMap.Delete}, {keyMap.Refresh, keyMap.Revalidate}, {keyMap.ToggleHelp, keyMap.Back}}
}

// networkHealthText resolves the stable health code to a localized string at
// render time; unknown runtime codes fall back to a bounded raw diagnostic.
func networkHealthText(info NetworkInfo) string {
	switch info.CurrentHealth {
	case "verified":
		return localization.Get("net_health_verified")
	case "unchecked", "":
		return localization.Get("net_health_unchecked")
	default:
		return safeShort(info.CurrentHealth)
	}
}

// networkPrivacyText renders the raw tracking value with a localized prefix.
func networkPrivacyText(info NetworkInfo) string {
	if info.PrivacyTracking == "" {
		return localization.Get("net_tracking_unknown")
	}
	return localization.T("net_tracking_observed", map[string]interface{}{"Tracking": safeShort(info.PrivacyTracking)})
}

// networkConfidenceText resolves the stable confidence code at render time.
func networkConfidenceText(info NetworkInfo) string {
	switch info.QuorumConfidence {
	case "single_provider", "none", "":
		return localization.Get("net_confidence_none")
	default:
		return safeShort(info.QuorumConfidence)
	}
}

// refreshLocalizedColumns reapplies localized column titles preserving widths.
func (c *NetworkListComponent) refreshLocalizedColumns() {
	columns := c.table.Columns()
	keys := map[int]string{1: "network_name", 2: "network_col_type_identity", 3: "chain_id", 4: "symbol", 5: "status"}
	for i, k := range keys {
		if i < len(columns) {
			columns[i].Title = localization.Get(k)
		}
	}
	c.table.SetColumns(columns)
}

// refreshKeyHelp reapplies localized help descriptions preserving Enabled
// state and keys.
func (c *NetworkListComponent) refreshKeyHelp() {
	type entry struct {
		binding *key.Binding
		key     string
	}
	k := &c.keys
	for _, e := range []entry{
		{&k.Up, "help_up"},
		{&k.Down, "help_down"},
		{&k.Add, "help_add_network"},
		{&k.Edit, "help_edit_network"},
		{&k.Delete, "help_delete_network"},
		{&k.Refresh, "help_refresh"},
		{&k.Revalidate, "help_revalidate"},
		{&k.Back, "help_back"},
		{&k.ToggleHelp, "help_more"},
	} {
		e.binding.SetHelp(e.binding.Help().Key, localization.Get(e.key))
	}
}

// NetworkListComponent represents the network list component
type NetworkListComponent struct {
	id     string
	width  int
	height int
	table  table.Model
	help   help.Model
	keys   NetworkListKeyMap
	err    error
	busy   bool
	status string

	// Cached classification info to avoid network calls during View rendering
	networksInfo map[string]NetworkInfo
	// lastCfg lets locale refresh re-render localized cells without IO
	lastCfg *config.Config

	// Network service
	chainListService *blockchain.ChainListService
}

// NewNetworkListComponent creates a new network list component
func NewNetworkListComponent() NetworkListComponent {
	c := NetworkListComponent{
		id:               "network-list",
		chainListService: getChainListService(),
		networksInfo:     make(map[string]NetworkInfo),
		help:             help.New(),
		keys:             newNetworkListKeyMap(),
	}
	c.initTable()
	return c
}

// initTable initializes the table with empty rows
func (c *NetworkListComponent) initTable() {
	columns := []table.Column{
		{Title: "#", Width: 4},
		{Title: localization.Get("network_name"), Width: 18},
		{Title: localization.Get("network_col_type_identity"), Width: 36},
		{Title: localization.Get("chain_id"), Width: 10},
		{Title: localization.Get("symbol"), Width: 8},
		{Title: localization.Get("status"), Width: 10},
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
	c.help.Width = max(0, width-4)

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
	c.busy = false
}

func (c *NetworkListComponent) SetBusy(status string) {
	c.busy = true
	c.status = status
	c.err = nil
}

func (c *NetworkListComponent) SetStatus(status string) {
	c.busy = false
	c.status = status
	c.err = nil
}

func (c *NetworkListComponent) updateKeyAvailability() {
	hasSelection := len(c.table.Rows()) > 0 && c.GetSelectedNetworkKey() != ""
	c.keys.Edit.SetEnabled(hasSelection && !c.busy)
	c.keys.Delete.SetEnabled(hasSelection && !c.busy)
	c.keys.Revalidate.SetEnabled(hasSelection && !c.busy)
	c.keys.Add.SetEnabled(!c.busy)
	c.keys.Refresh.SetEnabled(!c.busy)
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
	c.UpdateNetworksWithInfo(cfg, networksWithInfo)
}

func (c *NetworkListComponent) UpdateNetworksWithInfo(cfg *config.Config, networksWithInfo map[string]NetworkInfo) {
	if cfg == nil || cfg.Networks == nil {
		return
	}
	// Cache to avoid repeated network calls during table navigation/render
	c.networksInfo = networksWithInfo
	c.lastCfg = cfg

	c.table.SetRows(c.buildRows(cfg))

	// Only set the cursor if there are rows
	if len(c.table.Rows()) > 0 {
		c.table.SetCursor(0)
	}
	c.busy = false
	c.updateKeyAvailability()
}

// buildRows derives display rows from raw config/network metadata so locale
// refreshes can re-render localized cells without reverse-mapping text.
func (c *NetworkListComponent) buildRows(cfg *config.Config) []table.Row {
	var rows []table.Row
	keys := make([]string, 0, len(cfg.Networks))
	for key := range cfg.Networks {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	for index, networkKey := range keys {
		network := cfg.Networks[networkKey]
		status := localization.Get("inactive")
		if network.IsActive {
			status = localization.Get("active")
		}

		// Get network type and source information
		networkType := localization.Get("network_type_custom")
		identity := localization.Get("network_identity_unchecked")
		if networkInfo, exists := c.networksInfo[networkKey]; exists {
			if networkInfo.Type == blockchain.NetworkTypeStandard {
				networkType = localization.Get("network_type_registry_claim")
			}
			if networkInfo.PreviouslyValidated {
				identity = localization.Get("network_identity_observed")
			}
			networkType = fmt.Sprintf("%s / %s / %s:%s", networkType, identity, localization.Get("network_health_label"), networkHealthText(networkInfo))
		} else {
			networkType = fmt.Sprintf("%s / %s", networkType, identity)
		}

		rows = append(rows, table.Row{
			strconv.Itoa(index + 1),
			safeShort(network.Name),
			safeShort(networkType),
			strconv.FormatInt(network.ChainID, 10),
			safeShort(network.Symbol),
			safeShort(status),
			networkKey, // Hidden column for network key
		})
	}
	return rows
}

// refreshLocalizedRows re-renders localized status/type cells from cached raw
// metadata, preserving cursor and selection. No network or config IO.
func (c *NetworkListComponent) refreshLocalizedRows() {
	if c.lastCfg == nil {
		return
	}
	selectedKey := c.GetSelectedNetworkKey()
	cursor := c.table.Cursor()
	c.table.SetRows(c.buildRows(c.lastCfg))
	if len(c.table.Rows()) == 0 {
		return
	}
	c.table.SetCursor(min(cursor, len(c.table.Rows())-1))
	if selectedKey != "" {
		for i, row := range c.table.Rows() {
			if len(row) > 6 && row[6] == selectedKey {
				c.table.SetCursor(i)
				break
			}
		}
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
	if keyMessage, ok := msg.(tea.KeyMsg); ok && key.Matches(keyMessage, c.keys.ToggleHelp) {
		c.help.ShowAll = !c.help.ShowAll
		return c, nil
	}
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
	content = headerStyle.Render(localization.Get("networks"))
	content += "\n\n"

	// Table
	rows := c.table.Rows()
	if len(rows) > 0 {
		content += c.table.View()
	} else {
		content += localization.Get("no_networks_found")
	}
	content += "\n\n"
	if c.busy {
		content += lipgloss.NewStyle().Foreground(lipgloss.Color("#E5C07B")).MarginLeft(2).Render(safeInline(c.status))
		content += "\n\n"
	}

	// Error message
	if c.err != nil {
		errorStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FF0000")).
			MarginLeft(2)
		content += errorStyle.Render(localization.Get("network_op_failed") + ": " + safeInline(c.err.Error()))
		content += "\n"
		content += errorStyle.Render(localization.Get("network_op_hint"))
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

			registryStatus := localization.Get("network_registry_not_listed")
			if selectedNetworkInfo.Type == blockchain.NetworkTypeStandard {
				registryStatus = localization.Get("network_registry_claim")
				if selectedNetworkInfo.ChainInfo != nil {
					registryStatus = localization.T("network_registry_listed", map[string]interface{}{"Name": safeShort(selectedNetworkInfo.ChainInfo.Name)})
				}
			}
			identityStatus := localization.Get("network_identity_not_verified")
			if selectedNetworkInfo.IsValidated {
				identityStatus = localization.Get("network_identity_verified")
			} else if selectedNetworkInfo.PreviouslyValidated {
				identityStatus = localization.Get("network_identity_prev")
			}
			details := strings.Join([]string{
				localization.Get("network_selected"),
				localization.Get("network_detail_source") + ": " + safeShort(selectedNetworkInfo.Source),
				localization.Get("network_detail_registry") + ": " + registryStatus,
				localization.Get("network_detail_chain_identity") + ": " + identityStatus,
				localization.Get("network_detail_health") + ": " + networkHealthText(*selectedNetworkInfo),
				localization.Get("network_detail_privacy") + ": " + networkPrivacyText(*selectedNetworkInfo),
				localization.Get("network_detail_confidence") + ": " + networkConfidenceText(*selectedNetworkInfo),
				localization.Get("network_revalidate_hint"),
			}, "\n")
			content += detailStyle.Render(details)
			content += "\n"
		}
	}

	// Network type legend
	if c.status != "" && !c.busy {
		content += lipgloss.NewStyle().Foreground(lipgloss.Color("#98C379")).MarginLeft(2).Render(safeInline(c.status))
		content += "\n"
	}

	// Instructions
	content += "\n"

	// Footer
	content += c.help.View(c.keys)

	return content
}

// networkAddedMsg is sent when a network is added
type networkAddedMsg struct{}

// BackToNetworkListMsg is sent to return to the network list
type BackToNetworkListMsg struct{}

// BackToNetworkMenuMsg is sent to return to the network menu
type BackToNetworkMenuMsg struct{}
