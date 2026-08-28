package ui

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
	"unicode"

	"blocowallet/internal/blockchain"
	"blocowallet/pkg/config"
	"blocowallet/pkg/localization"
	"blocowallet/pkg/logger"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Architecture and logging helpers for UI input handling
var lastAddNetworkID atomic.Uint64

var (
	// archDetector allows tests to mock architecture; defaults to runtime.GOARCH
	archDetector = func() string { return runtime.GOARCH }
	// uiLogger is an optional file-based logger injected from main
	uiLogger logger.Logger
)

// SetLogger allows the main application to inject a file-based logger for UI debug events
func SetLogger(l logger.Logger) { uiLogger = l }

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
	decimalsInput    textinput.Model
	symbolInput      textinput.Model
	nameInput        textinput.Model

	// Form state
	focusIndex         int
	inputs             []textinput.Model
	selectedSuggestion int
	isSearchFocused    bool
	nativeDecimals     int
	nativeDecimalsSet  bool

	// Chain service for suggestions
	chainListService *blockchain.ChainListService

	// Autocomplete data
	suggestions        []blockchain.NetworkSuggestion
	suggestionList     list.Model // Interactive suggestion list
	loadingSuggestions bool
	// lastSearchTerm     string // Currently unused but may be needed for debouncing
	searchGeneration uint64
	operationContext context.Context
	cancelOperations context.CancelFunc
}

// networkSuggestionItem is a wrapper for NetworkSuggestion to implement list.Item
type networkSuggestionItem struct {
	suggestion blockchain.NetworkSuggestion
}

func (i networkSuggestionItem) Title() string {
	return safeShort(i.suggestion.Name)
}

func (i networkSuggestionItem) Description() string {
	return fmt.Sprintf("Chain ID: %d, Symbol: %s", i.suggestion.ChainID, safeShort(i.suggestion.Symbol))
}

func (i networkSuggestionItem) FilterValue() string {
	return safeShort(i.suggestion.Name)
}

// NewAddNetworkComponent creates a new add network component
func NewAddNetworkComponent() AddNetworkComponent {
	operationContext, cancelOperations := context.WithCancel(context.Background())
	c := AddNetworkComponent{
		id:               fmt.Sprintf("add-network-%d", lastAddNetworkID.Add(1)),
		chainListService: getChainListService(),
		operationContext: operationContext,
		cancelOperations: cancelOperations,
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
	c.searchInput.CharLimit = 128
	c.searchInput.ShowSuggestions = true
	c.searchInput.Focus()
	c.isSearchFocused = true

	// Network name input for display
	c.nameInput = textinput.New()
	c.nameInput.Placeholder = localization.Labels["network_name_placeholder"]
	c.nameInput.Width = 60
	c.nameInput.CharLimit = 128

	// Chain ID input
	c.chainIDInput = textinput.New()
	c.chainIDInput.Placeholder = localization.Labels["chain_id_placeholder"]
	c.chainIDInput.Width = 60
	c.chainIDInput.CharLimit = 20

	// Symbol input
	c.symbolInput = textinput.New()
	c.symbolInput.Placeholder = localization.Labels["symbol_placeholder"]
	c.symbolInput.Width = 60
	c.symbolInput.CharLimit = 16

	// Native currency decimals input
	c.decimalsInput = textinput.New()
	c.decimalsInput.Placeholder = "Native currency decimals (0-36)"
	c.decimalsInput.Width = 60
	c.decimalsInput.CharLimit = 2

	// RPC endpoint input
	c.rpcEndpointInput = textinput.New()
	c.rpcEndpointInput.Placeholder = localization.Labels["rpc_endpoint_placeholder"] + " or env:VARIABLE_NAME"
	c.rpcEndpointInput.Width = 60
	c.rpcEndpointInput.CharLimit = 4096

	// Initialize inputs slice for easy navigation
	c.inputs = []textinput.Model{
		c.searchInput,
		c.nameInput,
		c.chainIDInput,
		c.symbolInput,
		c.decimalsInput,
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

func (c *AddNetworkComponent) clearNativeDecimalsProvenance() {
	if c.nativeDecimalsSet {
		c.nativeDecimals = 0
		c.nativeDecimalsSet = false
		c.decimalsInput.SetValue("")
	}
}

// Reset clears all inputs
func (c *AddNetworkComponent) Reset() {
	if c.cancelOperations != nil {
		c.cancelOperations()
	}
	c.operationContext, c.cancelOperations = context.WithCancel(context.Background())
	c.searchGeneration++
	c.searchInput.SetValue("")
	c.nameInput.SetValue("")
	c.chainIDInput.SetValue("")
	c.symbolInput.SetValue("")
	c.decimalsInput.SetValue("")
	c.rpcEndpointInput.SetValue("")
	c.err = nil
	c.adding = false
	c.suggestions = []blockchain.NetworkSuggestion{}
	c.loadingSuggestions = false
	c.focusIndex = 0
	c.selectedSuggestion = -1
	c.isSearchFocused = true
	c.nativeDecimals = 0
	c.nativeDecimalsSet = false
	c.initInputs()
}

// searchNetworks searches for networks based on the query
func (c *AddNetworkComponent) searchNetworks(query string) tea.Cmd {
	operationContext := c.operationContext
	generation := c.searchGeneration
	return func() tea.Msg {
		query = strings.TrimSpace(query)

		// If empty query, return popular networks
		if query == "" {
			popular := []blockchain.NetworkSuggestion{
				{ChainID: 1, Name: "Ethereum Mainnet"},
				{ChainID: 137, Name: "Polygon"},
				{ChainID: 56, Name: "BNB Smart Chain"},
				{ChainID: 42161, Name: "Arbitrum One"},
			}
			// Debug log removed
			return networkSearchResultMsg{generation: generation, suggestions: popular}
		}

		// Debug log removed
		suggestions, err := c.chainListService.SearchNetworksByNameContext(operationContext, query)
		if err != nil {
			// Provide a localized, context-aware error message
			return networkSearchResultMsg{generation: generation, err: err}
		}

		// Debug log removed
		return networkSearchResultMsg{generation: generation, suggestions: suggestions}
	}
}

func (c *AddNetworkComponent) scheduleSearch(query string) tea.Cmd {
	c.searchGeneration++
	generation := c.searchGeneration
	return tea.Tick(300*time.Millisecond, func(time.Time) tea.Msg {
		return debouncedNetworkSearchMsg{query: query, generation: generation}
	})
}

type debouncedNetworkSearchMsg struct {
	query      string
	generation uint64
}

// networkDetailsFetchedMsg carries async fetched RPC details for a suggestion
type networkDetailsFetchedMsg struct {
	Generation  uint64
	Suggestion  blockchain.NetworkSuggestion
	ChainInfo   *blockchain.ChainInfo
	RPCEndpoint string
	Err         error
}

// fetchChainInfoCmd fetches chain info asynchronously
func (c *AddNetworkComponent) fetchChainInfoCmd(suggestion blockchain.NetworkSuggestion) tea.Cmd {
	c.searchGeneration++
	operationContext := c.operationContext
	generation := c.searchGeneration
	return func() tea.Msg {
		chainInfo, rpcURL, err := c.chainListService.GetChainInfoWithRetryContext(operationContext, suggestion.ChainID)
		if err != nil {
			return networkDetailsFetchedMsg{Generation: generation, Suggestion: suggestion, Err: err}
		}
		return networkDetailsFetchedMsg{Generation: generation, Suggestion: suggestion, ChainInfo: chainInfo, RPCEndpoint: rpcURL}
	}
}

// fillNetworkData fills the form with network data when a suggestion is selected
func (c *AddNetworkComponent) fillNetworkData(suggestion blockchain.NetworkSuggestion, rpcURL string) {
	// Update input values directly
	c.nameInput.SetValue(safeShort(suggestion.Name))
	c.chainIDInput.SetValue(strconv.Itoa(suggestion.ChainID))
	c.symbolInput.SetValue(safeShort(suggestion.Symbol))
	if c.nativeDecimalsSet {
		c.decimalsInput.SetValue(strconv.Itoa(c.nativeDecimals))
	} else {
		c.decimalsInput.SetValue("")
	}
	if safeInline(rpcURL) != rpcURL {
		c.err = fmt.Errorf("RPC endpoint contains unsafe display characters")
		c.rpcEndpointInput.SetValue("")
		return
	}
	c.rpcEndpointInput.SetValue(rpcURL)

	// Update search input with the selected name
	c.searchInput.SetValue(safeShort(suggestion.Name))

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

	case debouncedNetworkSearchMsg:
		if msg.generation != c.searchGeneration {
			return c, nil
		}
		c.loadingSuggestions = true
		c.selectedSuggestion = -1
		return c, c.searchNetworks(msg.query)

	case networkSearchResultMsg:
		if msg.generation != c.searchGeneration {
			return c, nil
		}
		if msg.err != nil {
			c.SetError(fmt.Errorf("%s", c.generateErrorMessage(msg.err, "search")))
			c.loadingSuggestions = false
			return c, nil
		}
		c.suggestions = append([]blockchain.NetworkSuggestion(nil), msg.suggestions...)
		c.loadingSuggestions = false
		items := make([]list.Item, 0, len(c.suggestions))
		for _, suggestion := range c.suggestions {
			items = append(items, networkSuggestionItem{suggestion: suggestion})
		}
		c.suggestionList.SetItems(items)
		c.suggestionList.Select(0)
		if len(c.suggestions) > 0 {
			c.selectedSuggestion = 0
		} else {
			c.selectedSuggestion = -1
		}

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
		if msg.Generation != c.searchGeneration {
			return c, nil
		}
		c.loadingSuggestions = false
		if msg.Err != nil {
			c.err = fmt.Errorf("%s: %s", localization.Labels["failed_to_get_network_details"], c.generateErrorMessage(msg.Err, "search"))
			// Prefill what we can; leave RPC empty for manual entry
			c.nameInput.SetValue(safeShort(msg.Suggestion.Name))
			c.chainIDInput.SetValue(strconv.Itoa(msg.Suggestion.ChainID))
			c.symbolInput.SetValue(safeShort(msg.Suggestion.Symbol))
			c.decimalsInput.SetValue("")
			c.nativeDecimals = 0
			c.nativeDecimalsSet = false
			c.rpcEndpointInput.SetValue("")
			return c, nil
		}
		if msg.ChainInfo != nil {
			msg.Suggestion.Name = msg.ChainInfo.Name
			msg.Suggestion.Symbol = msg.ChainInfo.NativeCurrency.Symbol
			c.nativeDecimals = msg.ChainInfo.NativeCurrency.Decimals
			c.nativeDecimalsSet = true
		} else {
			c.nativeDecimals = 0
			c.nativeDecimalsSet = false
		}
		c.fillNetworkData(msg.Suggestion, msg.RPCEndpoint)
		return c, nil

	case addNetworkErrorMsg:
		if msg.componentID != c.id || msg.operationID != c.searchGeneration {
			return c, nil
		}
		c.SetError(msg.err)
		c.loadingSuggestions = false
		c.adding = false

	case errorMsg:
		c.SetError(fmt.Errorf("%s", string(msg)))
		c.loadingSuggestions = false
		c.adding = false

	case tea.KeyMsg:
		// Debug log removed
		if c.adding && msg.String() != "esc" {
			return c, nil
		}

		// Global key handling for navigation and submission
		switch msg.String() {
		case "esc":
			if c.cancelOperations != nil {
				c.cancelOperations()
			}
			c.searchGeneration++
			return c, func() tea.Msg { return BackToNetworkMenuMsg{} }
		case "tab":
			c.nextInput()
			return c, nil
		case "shift+tab":
			c.prevInput()
			return c, nil
		case "enter":
			if c.isSearchFocused && len(c.suggestionList.Items()) > 0 {
				if c.selectedSuggestion < 0 {
					c.selectedSuggestion = 0
					c.suggestionList.Select(0)
				}
				item := c.suggestionList.SelectedItem().(networkSuggestionItem)
				return c, c.fetchChainInfoCmd(item.suggestion)
			}
			if !c.isSearchFocused && c.validateInputs() {
				c.adding = true
				c.searchGeneration++
				operationID := c.searchGeneration
				componentID := c.id
				operationContext := c.operationContext
				enteredEndpoint := c.GetRPCEndpoint()
				name := c.GetNetworkName()
				chainIDValue := c.chainIDInput.Value()
				symbol := c.GetSymbol()
				nativeDecimals := c.nativeDecimals
				nativeDecimalsSet := c.nativeDecimalsSet
				return c, func() tea.Msg {
					rpcURL := enteredEndpoint
					rpcReference := ""
					if strings.HasPrefix(enteredEndpoint, "env:") {
						resolved, err := (config.EnvironmentCredentialProvider{}).Resolve(enteredEndpoint)
						if err != nil {
							return addNetworkErrorMsg{componentID: componentID, operationID: operationID, err: fmt.Errorf("RPC credential reference is unavailable")}
						}
						rpcURL = resolved
						rpcReference = enteredEndpoint
					}
					if err := c.chainListService.ValidateRPCEndpointContext(operationContext, rpcURL); err != nil {
						return addNetworkErrorMsg{componentID: componentID, operationID: operationID, err: fmt.Errorf("RPC endpoint validation failed: %w", err)}
					}
					chainIDStr := chainIDValue
					expectedChainID, err := strconv.ParseInt(chainIDStr, 10, 64)
					if err != nil {
						return addNetworkErrorMsg{componentID: componentID, operationID: operationID, err: fmt.Errorf("invalid chain ID")}
					}
					actualChainID, err := c.chainListService.GetChainIDFromRPCContext(operationContext, rpcURL)
					if err != nil {
						return addNetworkErrorMsg{componentID: componentID, operationID: operationID, err: fmt.Errorf("RPC endpoint validation failed: %w", err)}
					}
					if int64(actualChainID) != expectedChainID {
						return addNetworkErrorMsg{componentID: componentID, operationID: operationID, err: fmt.Errorf("chain ID mismatch: expected %d, got %d", expectedChainID, actualChainID)}
					}
					persistedEndpoint := enteredEndpoint
					if rpcReference != "" {
						persistedEndpoint = ""
					}
					return AddNetworkRequestMsg{
						componentID:       componentID,
						operationID:       operationID,
						Name:              name,
						ChainID:           chainIDValue,
						Symbol:            symbol,
						NativeDecimals:    nativeDecimals,
						NativeDecimalsSet: nativeDecimalsSet,
						RPCEndpoint:       persistedEndpoint,
						RPCEndpointRef:    rpcReference,
					}
				}
			}
		}

		if c.isSearchFocused {
			switch msg.String() {
			case "up", "down":
				// Debug log removed
				var cmd tea.Cmd
				c.suggestionList, cmd = c.suggestionList.Update(msg)
				cmds = append(cmds, cmd)
				c.selectedSuggestion = c.suggestionList.Index()
				// Debug log removed
				return c, tea.Batch(cmds...)
			case "enter":
				// Debug log removed
				if len(c.suggestionList.Items()) > 0 {
					// Ensure we have a valid selection index
					if c.selectedSuggestion < 0 {
						c.selectedSuggestion = 0
						c.suggestionList.Select(0)
						// Debug log removed
					}

					// Get the selected item and fetch details asynchronously
					item := c.suggestionList.SelectedItem().(networkSuggestionItem)
					// Do not set error for status; we'll fetch details quietly
					cmds = append(cmds, c.fetchChainInfoCmd(item.suggestion))
					return c, tea.Batch(cmds...)
				}
			}

			// Handle number key selection for suggestions
			if msg.Type == tea.KeyRunes && len(msg.Runes) == 1 {
				key := string(msg.Runes[0])
				if num, err := strconv.Atoi(key); err == nil && num >= 1 && num <= len(c.suggestions) {
					// Fetch details for the selected suggestion without setting an error status
					cmds = append(cmds, c.fetchChainInfoCmd(c.suggestions[num-1]))
					return c, tea.Batch(cmds...)
				}
			}

			// Update the currently focused input
			var cmd tea.Cmd
			switch c.focusIndex {
			case 0: // Search input
				oldValue := c.searchInput.Value()
				// ARM64-specific handling: manually insert printable runes when Bubble Tea fails to echo runes
				if archDetector() == "arm64" {
					// Handle backspace manually as a fallback
					if msg.String() == "backspace" && len(oldValue) > 0 {
						// remove last rune safely
						newVal := removeLastRune(oldValue)
						c.searchInput.SetValue(newVal)
						if uiLogger != nil {
							uiLogger.Debug("input_key_arm64_backspace",
								logger.Int("length", len([]rune(newVal))),
							)
						}
						c.loadingSuggestions = true
						c.selectedSuggestion = -1
						cmds = append(cmds, c.scheduleSearch(newVal))
						break
					}
					if msg.Type == tea.KeyRunes && len(msg.Runes) > 0 {
						var b strings.Builder
						for _, r := range msg.Runes {
							if r == '\n' || r == '\r' || r == '\t' {
								continue
							}
							if unicode.IsPrint(r) || unicode.IsSpace(r) {
								b.WriteRune(r)
							}
						}
						if b.Len() > 0 {
							newVal := oldValue + b.String()
							if len([]rune(newVal)) > 120 {
								newVal = truncateRunes(newVal, 120)
							}
							c.searchInput.SetValue(newVal)
							if uiLogger != nil {
								uiLogger.Debug("input_key_arm64_runes",
									logger.Int("runes", len(msg.Runes)),
									logger.Int("length", len([]rune(newVal))),
								)
							}
							c.loadingSuggestions = true
							c.selectedSuggestion = -1
							cmds = append(cmds, c.scheduleSearch(newVal))
							break
						}
					}
				}
				// Default handling
				c.searchInput, cmd = c.searchInput.Update(msg)
				newValue := c.searchInput.Value()
				cmds = append(cmds, cmd)

				// Trigger search if value changed
				if oldValue != newValue {
					if uiLogger != nil {
						uiLogger.Debug("input_key_default", logger.Int("length", len([]rune(newValue))))
					}
					// Auto-search after a short delay
					c.loadingSuggestions = true
					c.selectedSuggestion = -1
					cmds = append(cmds, c.scheduleSearch(newValue))
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

			case 4: // Native decimals input
				c.decimalsInput, cmd = c.decimalsInput.Update(msg)
				cmds = append(cmds, cmd)

			case 5: // RPC endpoint input
				c.rpcEndpointInput, cmd = c.rpcEndpointInput.Update(msg)
				cmds = append(cmds, cmd)
			}
		}

		// Ensure non-search fields also receive input updates
		if !c.isSearchFocused {
			var cmd tea.Cmd
			switch c.focusIndex {
			case 1:
				c.nameInput, cmd = c.nameInput.Update(msg)
			case 2:
				oldValue := c.chainIDInput.Value()
				c.chainIDInput, cmd = c.chainIDInput.Update(msg)
				if oldValue != c.chainIDInput.Value() {
					c.clearNativeDecimalsProvenance()
				}
			case 3:
				c.symbolInput, cmd = c.symbolInput.Update(msg)
			case 4:
				c.decimalsInput, cmd = c.decimalsInput.Update(msg)
			case 5:
				oldValue := c.rpcEndpointInput.Value()
				c.rpcEndpointInput, cmd = c.rpcEndpointInput.Update(msg)
				if oldValue != c.rpcEndpointInput.Value() {
					c.clearNativeDecimalsProvenance()
				}
			default:
				c.searchInput, cmd = c.searchInput.Update(msg)
			}
			cmds = append(cmds, cmd)
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
	c.decimalsInput.Blur()
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
		c.decimalsInput.Focus()
	case 5:
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

	decimalsValue := strings.TrimSpace(c.decimalsInput.Value())
	decimals, err := strconv.Atoi(decimalsValue)
	if err != nil || decimals < 0 || decimals > 36 {
		c.err = errors.New("native currency decimals must be between 0 and 36")
		return false
	}
	c.nativeDecimals = decimals
	c.nativeDecimalsSet = true

	if strings.TrimSpace(c.rpcEndpointInput.Value()) == "" {
		c.err = errors.New(localization.Labels["rpc_endpoint_required"])
		return false
	}

	// Basic URL validation
	rpc := strings.TrimSpace(c.rpcEndpointInput.Value())
	if strings.HasPrefix(rpc, "env:") {
		if err := config.ValidateCredentialReference(rpc); err != nil {
			c.err = errors.New(localization.Labels["invalid_rpc_endpoint"])
			return false
		}
	} else if !strings.HasPrefix(rpc, "http://") && !strings.HasPrefix(rpc, "https://") {
		c.err = errors.New(localization.Labels["invalid_rpc_endpoint"])
		return false
	}

	c.err = nil
	return true
}

// View renders the add network component
func (c *AddNetworkComponent) generateErrorMessage(err error, operation string) string {
	// Unwrap our structured error if present to get a cleaner detail
	var opErr *blockchain.NetworkOperationError
	detail := ""
	if errors.As(err, &opErr) {
		if opErr.Cause != nil {
			detail = safeError(opErr.Cause)
		} else if opErr.Message != "" {
			detail = safeInline(opErr.Message)
		}
		if operation == "" {
			operation = opErr.Operation
		}
	} else if err != nil {
		detail = safeError(err)
	}

	switch operation {
	case "search":
		if detail == "" {
			detail = localization.Labels["network_search_failed"]
		}
		return fmt.Sprintf("%s: %s. %s", localization.Labels["network_search_failed"], detail, localization.Labels["network_search_failed_guidance"])
	case "validate":
		if detail == "" {
			detail = localization.Labels["rpc_validation_failed"]
		}
		return fmt.Sprintf("%s: %s. %s", localization.Labels["rpc_validation_failed"], detail, localization.Labels["rpc_validation_failed_guidance"])
	case "select":
		if detail == "" {
			detail = localization.Labels["network_selection_failed"]
		}
		return fmt.Sprintf("%s: %s", localization.Labels["network_selection_failed"], detail)
	default:
		if detail == "" {
			detail = localization.Labels["operation_failed_generic"]
		}
		return fmt.Sprintf("%s: %s", localization.Labels["operation_failed_generic"], detail)
	}
}

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
	searchFieldStyle := fieldStyle
	if c.focusIndex == 0 {
		searchFieldStyle = fieldStyle.
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#874BFD")).
			PaddingLeft(1).PaddingRight(1)
	}
	b.WriteString(searchFieldStyle.Render(c.searchInput.View()))

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
	nameFieldStyle := fieldStyle
	if c.focusIndex == 1 {
		nameFieldStyle = fieldStyle.
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#874BFD")).
			PaddingLeft(1).PaddingRight(1)
	}
	b.WriteString(nameFieldStyle.Render(c.nameInput.View()))
	b.WriteString("\n")

	// Chain ID field
	b.WriteString(labelStyle.Render(localization.Labels["chain_id"] + ":"))
	b.WriteString("\n")
	chainFieldStyle := fieldStyle
	if c.focusIndex == 2 {
		chainFieldStyle = fieldStyle.
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#874BFD")).
			PaddingLeft(1).PaddingRight(1)
	}
	b.WriteString(chainFieldStyle.Render(c.chainIDInput.View()))
	b.WriteString("\n")

	// Symbol field
	b.WriteString(labelStyle.Render(localization.Labels["symbol"] + ":"))
	b.WriteString("\n")
	symbolFieldStyle := fieldStyle
	if c.focusIndex == 3 {
		symbolFieldStyle = fieldStyle.
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#874BFD")).
			PaddingLeft(1).PaddingRight(1)
	}
	b.WriteString(symbolFieldStyle.Render(c.symbolInput.View()))
	b.WriteString("\n")

	// Native decimals field
	b.WriteString(labelStyle.Render("Native currency decimals:"))
	b.WriteString("\n")
	decimalsFieldStyle := fieldStyle
	if c.focusIndex == 4 {
		decimalsFieldStyle = fieldStyle.
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#874BFD")).
			PaddingLeft(1).PaddingRight(1)
	}
	b.WriteString(decimalsFieldStyle.Render(c.decimalsInput.View()))
	b.WriteString("\n")

	// RPC Endpoint field
	b.WriteString(labelStyle.Render(localization.Labels["rpc_endpoint"] + ":"))
	b.WriteString("\n")
	rpcFieldStyle := fieldStyle
	if c.focusIndex == 5 {
		rpcFieldStyle = fieldStyle.
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#874BFD")).
			PaddingLeft(1).PaddingRight(1)
	}
	b.WriteString(rpcFieldStyle.Render(c.rpcEndpointInput.View()))
	b.WriteString("\n")

	// Status messages
	if c.adding {
		b.WriteString("\n")
		b.WriteString(loadingStyle.Render("⏳ " + localization.Labels["adding_network"] + "..."))
	} else if c.err != nil {
		b.WriteString("\n")
		b.WriteString(errorStyle.Render("❌ " + localization.Labels["error_title"] + ": " + safeInline(c.err.Error())))
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
	componentID       string
	operationID       uint64
	Name              string
	ChainID           string
	Symbol            string
	NativeDecimals    int
	NativeDecimalsSet bool
	RPCEndpoint       string
	RPCEndpointRef    string
}

type networkSearchResultMsg struct {
	generation  uint64
	suggestions []blockchain.NetworkSuggestion
	err         error
}

// networkSuggestionsMsg is sent when network suggestions are loaded
type networkSuggestionsMsg []blockchain.NetworkSuggestion

type addNetworkErrorMsg struct {
	componentID string
	operationID uint64
	err         error
}

// errorMsg is sent when an error occurs
type errorMsg string

// --- helpers: rune-safe operations for ARM64 fallback ---
func removeLastRune(s string) string {
	r := []rune(s)
	if len(r) == 0 {
		return s
	}
	return string(r[:len(r)-1])
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if n < 0 {
		return ""
	}
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
