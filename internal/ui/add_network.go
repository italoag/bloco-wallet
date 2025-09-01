package ui

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"blocowallet/internal/blockchain"
	"blocowallet/pkg/localization"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// AddNetworkComponent represents the add network component
type AddNetworkComponent struct {
	id     string
	width  int
	height int
	err    error
	adding bool

	// Text input fields
	searchInput      textinput.Model
	chainIDInput     textinput.Model
	rpcEndpointInput textinput.Model
	symbolInput      textinput.Model
	nameInput        textinput.Model

	// Form state
	focusIndex         int
	inputs             []textinput.Model
	selectedSuggestion int
	isSearchFocused    bool

	// Chain service for suggestions
	chainListService *blockchain.ChainListService

	// Autocomplete data
	suggestions        []blockchain.NetworkSuggestion
	suggestionList     list.Model // Interactive suggestion list
	loadingSuggestions bool
	// lastSearchTerm     string // Currently unused but may be needed for debouncing
	typingDebounce time.Time
}

// networkSuggestionItem is a wrapper for NetworkSuggestion to implement list.Item
type networkSuggestionItem struct {
	suggestion blockchain.NetworkSuggestion
}

func (i networkSuggestionItem) Title() string {
	return i.suggestion.Name
}

func (i networkSuggestionItem) Description() string {
	return fmt.Sprintf("Chain ID: %d, Symbol: %s", i.suggestion.ChainID, i.suggestion.Symbol)
}

func (i networkSuggestionItem) FilterValue() string {
	return i.suggestion.Name
}

// NewAddNetworkComponent creates a new add network component
func NewAddNetworkComponent() AddNetworkComponent {
	c := AddNetworkComponent{
		id:               "add-network",
		chainListService: blockchain.NewChainListService(),
	}
	c.initInputs()
	return c
}

// initInputs initializes the text input fields
func (c *AddNetworkComponent) initInputs() {
	// Search input for network search
	c.searchInput = textinput.New()
	c.searchInput.Placeholder = localization.Labels["search_networks_placeholder"]
	c.searchInput.Width = 60
	c.searchInput.ShowSuggestions = true
	c.searchInput.Focus()
	c.isSearchFocused = true

	// Network name input for display
	c.nameInput = textinput.New()
	c.nameInput.Placeholder = localization.Labels["network_name_placeholder"]
	c.nameInput.Width = 60

	// Chain ID input
	c.chainIDInput = textinput.New()
	c.chainIDInput.Placeholder = localization.Labels["chain_id_placeholder"]
	c.chainIDInput.Width = 60

	// Symbol input
	c.symbolInput = textinput.New()
	c.symbolInput.Placeholder = localization.Labels["symbol_placeholder"]
	c.symbolInput.Width = 60

	// RPC endpoint input
	c.rpcEndpointInput = textinput.New()
	c.rpcEndpointInput.Placeholder = localization.Labels["rpc_endpoint_placeholder"]
	c.rpcEndpointInput.Width = 60

	// Initialize inputs slice for easy navigation
	c.inputs = []textinput.Model{
		c.searchInput,
		c.nameInput,
		c.chainIDInput,
		c.symbolInput,
		c.rpcEndpointInput,
	}

	// Initialize the suggestion list
	delegate := list.NewDefaultDelegate()
	c.suggestionList = list.New([]list.Item{}, delegate, 60, 5)
	c.suggestionList.SetShowStatusBar(false)
	c.suggestionList.SetShowHelp(false)
	c.suggestionList.SetFilteringEnabled(false)
	c.suggestionList.Title = localization.Labels["suggestions"]

	// Initialize other fields
	c.selectedSuggestion = -1
	c.typingDebounce = time.Time{}
}

// SetSize updates the component size
func (c *AddNetworkComponent) SetSize(width, height int) {
	c.width = width
	c.height = height
}

// SetError sets an error state
func (c *AddNetworkComponent) SetError(err error) {
	c.err = err
	c.adding = false
}

// SetAdding sets the adding state
func (c *AddNetworkComponent) SetAdding(adding bool) {
	c.adding = adding
	if adding {
		c.err = nil
	}
}

// GetNetworkName returns the entered network name
func (c *AddNetworkComponent) GetNetworkName() string {
	return c.nameInput.Value()
}

// GetChainID returns the entered chain ID as integer
func (c *AddNetworkComponent) GetChainID() (int64, error) {
	chainID, err := strconv.ParseInt(strings.TrimSpace(c.chainIDInput.Value()), 10, 64)
	if err != nil {
		return 0, errors.New(localization.Labels["invalid_chain_id"])
	}
	return chainID, nil
}

// GetSymbol returns the entered symbol
func (c *AddNetworkComponent) GetSymbol() string {
	return c.symbolInput.Value()
}

// GetRPCEndpoint returns the entered RPC endpoint
func (c *AddNetworkComponent) GetRPCEndpoint() string {
	return c.rpcEndpointInput.Value()
}

// Reset clears all inputs
func (c *AddNetworkComponent) Reset() {
	c.searchInput.SetValue("")
	c.nameInput.SetValue("")
	c.chainIDInput.SetValue("")
	c.symbolInput.SetValue("")
	c.rpcEndpointInput.SetValue("")
	c.err = nil
	c.adding = false
	c.suggestions = []blockchain.NetworkSuggestion{}
	c.loadingSuggestions = false
	c.focusIndex = 0
	c.selectedSuggestion = -1
	c.isSearchFocused = true
	c.initInputs()
}

// searchNetworks searches for networks based on the query
func (c *AddNetworkComponent) searchNetworks(query string) tea.Cmd {
	return func() tea.Msg {
		query = strings.TrimSpace(query)

		// If empty query, return popular networks
		if query == "" {
			popular := []blockchain.NetworkSuggestion{
				{ChainID: 1, Name: "Ethereum Mainnet", Symbol: "ETH"},
				{ChainID: 137, Name: "Polygon Mainnet", Symbol: "MATIC"},
				{ChainID: 56, Name: "Binance Smart Chain", Symbol: "BNB"},
				{ChainID: 42161, Name: "Arbitrum One", Symbol: "ETH"},
			}
			// Debug log removed
			return networkSuggestionsMsg(popular)
		}

		// Debug log removed
		suggestions, err := c.chainListService.SearchNetworksByName(query)
		if err != nil {
			// Debug log removed
			return errorMsg(err.Error())
		}

		// Debug log removed
		return networkSuggestionsMsg(suggestions)
	}
}

// networkDetailsFetchedMsg carries async fetched RPC details for a suggestion
type networkDetailsFetchedMsg struct {
	Suggestion  blockchain.NetworkSuggestion
	RPCEndpoint string
	Err         string
}

// fetchChainInfoCmd fetches chain info asynchronously
func (c *AddNetworkComponent) fetchChainInfoCmd(suggestion blockchain.NetworkSuggestion) tea.Cmd {
	return func() tea.Msg {
		_, rpcURL, err := c.chainListService.GetChainInfoWithRetry(suggestion.ChainID)
		if err != nil {
			return networkDetailsFetchedMsg{Suggestion: suggestion, Err: fmt.Sprintf("%v", err)}
		}
		return networkDetailsFetchedMsg{Suggestion: suggestion, RPCEndpoint: rpcURL}
	}
}

// fillNetworkData fills the form with network data when a suggestion is selected
func (c *AddNetworkComponent) fillNetworkData(suggestion blockchain.NetworkSuggestion, rpcURL string) {
	// Update input values directly
	c.nameInput.SetValue(suggestion.Name)
	c.chainIDInput.SetValue(strconv.Itoa(suggestion.ChainID))
	c.symbolInput.SetValue(suggestion.Symbol)
	c.rpcEndpointInput.SetValue(rpcURL)

	// Update search input with the selected name
	c.searchInput.SetValue(suggestion.Name)

	// Clear error message
	c.err = nil

	// Move focus to the network name field for possible editing
	c.focusIndex = 1
	c.updateFocus()
}

// Init initializes the component
func (c *AddNetworkComponent) Init() tea.Cmd {
	// Initialize the search input to be focused
	c.focusIndex = 0
	c.searchInput.Focus()
	c.isSearchFocused = true
	c.selectedSuggestion = -1

	// Start with some popular networks
	return c.searchNetworks("")
}

// Update handles messages for the add network component
func (c *AddNetworkComponent) Update(msg tea.Msg) (*AddNetworkComponent, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		c.width = msg.Width
		c.height = msg.Height
		c.suggestionList.SetSize(60, 5)

	case networkAddedMsg:
		c.Reset()
		return c, func() tea.Msg { return BackToNetworkListMsg{} }

	case networkSuggestionsMsg:
		c.suggestions = []blockchain.NetworkSuggestion(msg)
		c.loadingSuggestions = false
		// Update the suggestion list
		items := make([]list.Item, 0, len(c.suggestions))
		for _, s := range c.suggestions {
			items = append(items, networkSuggestionItem{suggestion: s})
		}
		c.suggestionList.SetItems(items)
		c.suggestionList.Select(0)
		// Set the selected suggestion to the first item if there are suggestions
		if len(c.suggestions) > 0 {
			c.selectedSuggestion = 0
		} else {
			c.selectedSuggestion = -1
		}

	case networkDetailsFetchedMsg:
		c.loadingSuggestions = false
		if msg.Err != "" {
			c.err = fmt.Errorf("%s: %s", localization.Labels["failed_to_get_network_details"], msg.Err)
			// Prefill what we can; leave RPC empty for manual entry
			c.nameInput.SetValue(msg.Suggestion.Name)
			c.chainIDInput.SetValue(strconv.Itoa(msg.Suggestion.ChainID))
			c.symbolInput.SetValue(msg.Suggestion.Symbol)
			c.rpcEndpointInput.SetValue("")
			return c, nil
		}
		c.fillNetworkData(msg.Suggestion, msg.RPCEndpoint)
		return c, nil

	case errorMsg:
		c.SetError(fmt.Errorf("%s", string(msg)))
		c.loadingSuggestions = false
		c.adding = false

	case tea.KeyMsg:
		// Debug log removed

		if c.isSearchFocused && len(c.suggestions) > 0 {
			switch msg.String() {
			case "up", "down":
				// Debug log removed
				var cmd tea.Cmd
				c.suggestionList, cmd = c.suggestionList.Update(msg)
				cmds = append(cmds, cmd)
				c.selectedSuggestion = c.suggestionList.Index()
				// Debug log removed
				return c, tea.Batch(cmds...)
			case "enter", "tab":
				// Debug log removed
				if len(c.suggestionList.Items()) > 0 {
					// Ensure we have a valid selection index
					if c.selectedSuggestion < 0 {
						c.selectedSuggestion = 0
						c.suggestionList.Select(0)
						// Debug log removed
					}

					// Debug logs removed

					// Get the selected item and fetch details asynchronously
					item := c.suggestionList.SelectedItem().(networkSuggestionItem)
					c.err = errors.New(localization.Labels["searching_networks"]) // temporary status
					cmds = append(cmds, c.fetchChainInfoCmd(item.suggestion))
					return c, tea.Batch(cmds...)
				}
			}

			// Handle global special keys
			switch msg.String() {
			case "esc":
				return c, func() tea.Msg { return BackToNetworkMenuMsg{} }

			case "enter":
				// Submit form if not in search mode
				if !c.isSearchFocused && c.validateInputs() {
					c.adding = true
					c.err = fmt.Errorf("validating RPC endpoint")

					return c, func() tea.Msg {
						// Validate RPC endpoint before submitting
						rpcURL := c.GetRPCEndpoint()
						if err := c.chainListService.ValidateRPCEndpoint(rpcURL); err != nil {
							return errorMsg(fmt.Sprintf("RPC validation failed: %v", err))
						}

						// Verify chain ID matches
						chainIDStr := c.chainIDInput.Value()
						expectedChainID, err := strconv.ParseInt(chainIDStr, 10, 64)
						if err != nil {
							return errorMsg("Invalid chain ID format")
						}

						actualChainID, err := c.chainListService.GetChainIDFromRPC(rpcURL)
						if err != nil {
							return errorMsg(fmt.Sprintf("Failed to get chain ID from RPC: %v", err))
						}

						if int64(actualChainID) != expectedChainID {
							return errorMsg(fmt.Sprintf("Chain ID mismatch: expected %d, got %d", expectedChainID, actualChainID))
						}

						return AddNetworkRequestMsg{
							Name:        c.GetNetworkName(),
							ChainID:     c.chainIDInput.Value(),
							Symbol:      c.GetSymbol(),
							RPCEndpoint: c.GetRPCEndpoint(),
						}
					}
				}

			case "tab":
				// Move to next input (handled separately if search is focused)
				if !c.isSearchFocused {
					// Debug log removed
					c.nextInput()
					return c, nil
				}

			case "shift+tab":
				// Move to previous input
				// Debug log removed
				c.prevInput()
				return c, nil
			}

			// Handle number key selection for suggestions
			if msg.Type == tea.KeyRunes && len(msg.Runes) == 1 {
				key := string(msg.Runes[0])
				if num, err := strconv.Atoi(key); err == nil && num >= 1 && num <= len(c.suggestions) {
					c.err = errors.New(localization.Labels["searching_networks"]) // temporary status
					cmds = append(cmds, c.fetchChainInfoCmd(c.suggestions[num-1]))
					return c, tea.Batch(cmds...)
				}
			}

			// Update the currently focused input
			var cmd tea.Cmd
			switch c.focusIndex {
			case 0: // Search input
				oldValue := c.searchInput.Value()
				c.searchInput, cmd = c.searchInput.Update(msg)
				newValue := c.searchInput.Value()
				cmds = append(cmds, cmd)

				// Trigger search if value changed
				if oldValue != newValue {
					// Auto-search after a short delay
					c.loadingSuggestions = true
					c.selectedSuggestion = -1
					cmds = append(cmds, c.searchNetworks(newValue))
				}

			case 1: // Name input
				c.nameInput, cmd = c.nameInput.Update(msg)
				cmds = append(cmds, cmd)

			case 2: // Chain ID input
				c.chainIDInput, cmd = c.chainIDInput.Update(msg)
				cmds = append(cmds, cmd)

			case 3: // Symbol input
				c.symbolInput, cmd = c.symbolInput.Update(msg)
				cmds = append(cmds, cmd)

			case 4: // RPC endpoint input
				c.rpcEndpointInput, cmd = c.rpcEndpointInput.Update(msg)
				cmds = append(cmds, cmd)
			}
		}

	}

	return c, tea.Batch(cmds...)
}

// nextInput focuses the next input field
func (c *AddNetworkComponent) nextInput() {
	c.focusIndex = (c.focusIndex + 1) % len(c.inputs)
	c.updateFocus()
}

// prevInput focuses the previous input field
func (c *AddNetworkComponent) prevInput() {
	c.focusIndex--
	if c.focusIndex < 0 {
		c.focusIndex = len(c.inputs) - 1
	}
	c.updateFocus()
}

// updateFocus updates the focus state of all inputs
func (c *AddNetworkComponent) updateFocus() {
	// Blur all inputs
	c.searchInput.Blur()
	c.nameInput.Blur()
	c.chainIDInput.Blur()
	c.symbolInput.Blur()
	c.rpcEndpointInput.Blur()

	// Track if search is focused
	c.isSearchFocused = c.focusIndex == 0

	// Focus the selected input
	switch c.focusIndex {
	case 0:
		c.searchInput.Focus()
	case 1:
		c.nameInput.Focus()
	case 2:
		c.chainIDInput.Focus()
	case 3:
		c.symbolInput.Focus()
	case 4:
		c.rpcEndpointInput.Focus()
	}
}

// validateInputs checks if the inputs are valid
func (c *AddNetworkComponent) validateInputs() bool {
	if strings.TrimSpace(c.nameInput.Value()) == "" {
		c.err = errors.New(localization.Labels["network_name_required"])
		return false
	}

	if strings.TrimSpace(c.chainIDInput.Value()) == "" {
		c.err = errors.New(localization.Labels["chain_id_required"])
		return false
	}

	// Validate chain ID is a number
	if _, err := c.GetChainID(); err != nil {
		c.err = err
		return false
	}

	if strings.TrimSpace(c.symbolInput.Value()) == "" {
		c.err = errors.New(localization.Labels["symbol_required"])
		return false
	}

	if strings.TrimSpace(c.rpcEndpointInput.Value()) == "" {
		c.err = errors.New(localization.Labels["rpc_endpoint_required"])
		return false
	}

	// Basic URL validation
	rpc := strings.TrimSpace(c.rpcEndpointInput.Value())
	if !strings.HasPrefix(rpc, "http://") && !strings.HasPrefix(rpc, "https://") {
		c.err = errors.New(localization.Labels["invalid_rpc_endpoint"])
		return false
	}

	c.err = nil
	return true
}

// View renders the add network component
func (c *AddNetworkComponent) View() string {
	var b strings.Builder

	// Header
	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#FFF")).
		Background(lipgloss.Color("#874BFD")).
		MarginLeft(2).
		MarginBottom(1)
	b.WriteString(headerStyle.Render("🌐 " + localization.Labels["add_network"]))
	b.WriteString("\n\n")

	// Styles
	fieldStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("250")).
		MarginLeft(2).
		MarginBottom(1)

	labelStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#874BFD"))

	searchLabelStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("13"))

	// Search field
	b.WriteString(searchLabelStyle.Render("🔍 " + localization.Labels["search_networks"] + ":"))
	b.WriteString("\n")
	b.WriteString(fieldStyle.Render(c.searchInput.View()))

	// Styles for messages
	infoStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#666666")).
		MarginLeft(2)

	loadingStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#874BFD")).
		MarginLeft(2)

	errorStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FF0000")).
		MarginLeft(2)

	warningStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FFA500")).
		Bold(true).
		MarginLeft(2)

	footerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#874BFD")).
		MarginTop(1)

	// Interactive suggestions
	if c.loadingSuggestions {
		b.WriteString("\n")
		b.WriteString(loadingStyle.Render("🔍 " + localization.Labels["searching_networks"] + "..."))
	} else if len(c.suggestions) > 0 {
		b.WriteString("\n")
		b.WriteString(c.suggestionList.View())
	}

	b.WriteString("\n\n")
	detailHeaderStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#FFF")).
		Background(lipgloss.Color("#874BFD")).
		MarginLeft(2).
		MarginBottom(1)
	b.WriteString(detailHeaderStyle.Render(localization.Labels["network_details"] + ":"))
	b.WriteString("\n\n")

	// Network Name field
	b.WriteString(labelStyle.Render(localization.Labels["network_name"] + ":"))
	b.WriteString("\n")
	b.WriteString(fieldStyle.Render(c.nameInput.View()))
	b.WriteString("\n")

	// Chain ID field
	b.WriteString(labelStyle.Render(localization.Labels["chain_id"] + ":"))
	b.WriteString("\n")
	b.WriteString(fieldStyle.Render(c.chainIDInput.View()))
	b.WriteString("\n")

	// Symbol field
	b.WriteString(labelStyle.Render(localization.Labels["symbol"] + ":"))
	b.WriteString("\n")
	b.WriteString(fieldStyle.Render(c.symbolInput.View()))
	b.WriteString("\n")

	// RPC Endpoint field
	b.WriteString(labelStyle.Render(localization.Labels["rpc_endpoint"] + ":"))
	b.WriteString("\n")
	b.WriteString(fieldStyle.Render(c.rpcEndpointInput.View()))
	b.WriteString("\n")

	// Status messages
	if c.adding {
		b.WriteString("\n")
		b.WriteString(loadingStyle.Render("⏳ " + localization.Labels["adding_network"] + "..."))
	} else if c.err != nil {
		b.WriteString("\n")
		b.WriteString(errorStyle.Render("❌ " + localization.Labels["error"] + ": " + c.err.Error()))
	}

	// Instructions
	b.WriteString("\n\n")
	b.WriteString(warningStyle.Render("💡 " + localization.Labels["tips"] + ":"))
	b.WriteString("\n")
	b.WriteString(infoStyle.Render("  • " + localization.Labels["search_networks_tip"]))
	b.WriteString("\n")
	b.WriteString(infoStyle.Render("  • " + localization.Labels["chain_id_tip"]))
	b.WriteString("\n")
	b.WriteString(infoStyle.Render("  • " + localization.Labels["rpc_endpoint_tip"]))
	b.WriteString("\n\n")

	// Footer
	b.WriteString(footerStyle.Render(localization.Labels["add_network_footer"]))

	return b.String()
}

// AddNetworkRequestMsg is sent when the user wants to add a network
type AddNetworkRequestMsg struct {
	Name        string
	ChainID     string
	Symbol      string
	RPCEndpoint string
}

// networkSuggestionsMsg is sent when network suggestions are loaded
type networkSuggestionsMsg []blockchain.NetworkSuggestion

// errorMsg is sent when an error occurs
type errorMsg string
