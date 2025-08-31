package ui

import (
	"blocowallet/internal/blockchain"
	"blocowallet/internal/constants"
	"blocowallet/pkg/config"
	"blocowallet/pkg/localization"
	"errors"
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// ensureConfigAndNetworksLoaded ensures that the current configuration and networks are loaded
func (m *CLIModel) ensureConfigAndNetworksLoaded() error {
	// Ensure currentConfig is initialized
	if m.currentConfig == nil {
		cfg, err := loadOrCreateConfig()
		if err != nil {
			return fmt.Errorf("failed to load configuration: %w", err)
		}
		m.currentConfig = cfg
	}

	// Ensure networks are properly loaded using NetworkManager
	networks, err := loadNetworksWithManager()
	if err != nil {
		return fmt.Errorf("failed to load networks: %w", err)
	}

	// Update the current config with loaded networks
	if m.currentConfig.Networks == nil {
		m.currentConfig.Networks = make(map[string]config.Network)
	}
	m.currentConfig.Networks = networks

	return nil
}

// initNetworkList initializes the network list view
func (m *CLIModel) initNetworkList() {
	// Initialize the network list component if it hasn't been initialized yet
	m.networkListComponent = NewNetworkListComponent()

	// Ensure configuration and networks are loaded
	if err := m.ensureConfigAndNetworksLoaded(); err != nil {
		m.err = err
		m.currentView = constants.DefaultView
		return
	}

	// Update the network list with the current networks
	m.networkListComponent.UpdateNetworks(m.currentConfig)

	// Set the current view to the network list view
	m.currentView = constants.NetworkListView
}

// initAddNetwork initializes the add network view
func (m *CLIModel) initAddNetwork() {
	// Initialize the add network component if it hasn't been initialized yet
	m.addNetworkComponent = NewAddNetworkComponent()

	// Ensure configuration and networks are loaded
	if err := m.ensureConfigAndNetworksLoaded(); err != nil {
		m.err = err
		m.currentView = constants.DefaultView
		return
	}

	// Set the current view to the add network view
	m.currentView = constants.AddNetworkView
}

// viewNetworkList renders the network list view
func (m *CLIModel) viewNetworkList() string {
	// Update the component size
	// Only call SetSize if the component is initialized and has rows
	rows := m.networkListComponent.table.Rows()
	if len(rows) > 0 {
		m.networkListComponent.SetSize(m.width, m.height)
	}

	// Render the component
	return m.networkListComponent.View()
}

// viewAddNetwork renders the add network view
func (m *CLIModel) viewAddNetwork() string {
	// Update the component size
	m.addNetworkComponent.SetSize(m.width, m.height)

	// Render the component
	return m.addNetworkComponent.View()
}

// updateNetworkList handles updates to the network list view
func (m *CLIModel) updateNetworkList(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "a":
			// Add a new network
			m.initAddNetwork()
			return m, nil

		case "e":
			// Edit the selected network
			key := m.networkListComponent.GetSelectedNetworkKey()
			if key == "" {
				m.networkListComponent.SetError(errors.New(localization.Labels["no_network_selected"]))
				return m, nil
			}

			// Ensure configuration and networks are loaded
			if err := m.ensureConfigAndNetworksLoaded(); err != nil {
				m.networkListComponent.SetError(fmt.Errorf("failed to load configuration: %v", err))
				return m, nil
			}

			// Check if we have networks
			if len(m.currentConfig.Networks) == 0 {
				m.networkListComponent.SetError(errors.New(localization.Labels["no_network_selected"]))
				return m, nil
			}

			// Get the network to edit
			network, exists := m.currentConfig.Networks[key]
			if !exists {
				m.networkListComponent.SetError(fmt.Errorf("network not found"))
				return m, nil
			}

			// Initialize add network component for editing
			m.addNetworkComponent = NewAddNetworkComponent()

			// Pre-fill the form with existing network data
			m.addNetworkComponent.nameInput.SetValue(network.Name)
			m.addNetworkComponent.chainIDInput.SetValue(strconv.FormatInt(network.ChainID, 10))
			m.addNetworkComponent.symbolInput.SetValue(network.Symbol)
			m.addNetworkComponent.rpcEndpointInput.SetValue(network.RPCEndpoint)

			// Store the key for updating later
			m.editingNetworkKey = key

			// Set the current view to add network (which will function as edit)
			m.currentView = constants.AddNetworkView
			return m, nil

		case "d":
			// Delete the selected network
			key := m.networkListComponent.GetSelectedNetworkKey()
			if key == "" {
				m.networkListComponent.SetError(errors.New(localization.Labels["no_network_selected"]))
				return m, nil
			}

			// Ensure configuration and networks are loaded
			if err := m.ensureConfigAndNetworksLoaded(); err != nil {
				m.networkListComponent.SetError(fmt.Errorf("failed to load configuration: %v", err))
				return m, nil
			}

			// Check if we have networks
			if len(m.currentConfig.Networks) == 0 {
				m.networkListComponent.SetError(errors.New(localization.Labels["no_network_selected"]))
				return m, nil
			}

			// Remove the network using NetworkManager
			err := removeNetworkWithManager(key)
			if err != nil {
				m.networkListComponent.SetError(fmt.Errorf("failed to remove network: %v", err))
				return m, nil
			}

			// Reload configuration to get the updated networks
			if err := m.ensureConfigAndNetworksLoaded(); err != nil {
				m.networkListComponent.SetError(fmt.Errorf("failed to reload configuration: %v", err))
				return m, nil
			}

			// Update the network list
			m.networkListComponent.UpdateNetworks(m.currentConfig)

			return m, nil

		case "esc", "backspace":
			// Return to the network menu
			m.menuItems = NewNetworkMenu()
			m.selectedMenu = 0
			m.currentView = constants.NetworkMenuView
			return m, nil
		}

	case BackToNetworkListMsg:
		// Return to the network list view
		m.currentView = constants.NetworkListView

		// Ensure configuration and networks are loaded
		if err := m.ensureConfigAndNetworksLoaded(); err != nil {
			m.err = fmt.Errorf("failed to load configuration: %v", err)
			m.currentView = constants.DefaultView
			return m, nil
		}

		// Update the network list
		m.networkListComponent.UpdateNetworks(m.currentConfig)

		return m, nil
	}

	// Update the network list component
	networkList, cmd := m.networkListComponent.Update(msg)
	m.networkListComponent = *networkList

	return m, cmd
}

// saveConfigToFile saves the current configuration to the config file using ConfigurationManager
func (m *CLIModel) saveConfigToFile() error {
	if m.currentConfig == nil {
		return fmt.Errorf("no configuration to save")
	}

	// Get the ConfigurationManager
	cm := getConfigurationManager()

	// If the ConfigurationManager hasn't been loaded yet, load it first
	if cm.GetConfigPath() == "" {
		// Try to load the configuration to initialize the ConfigurationManager
		_, err := cm.LoadConfiguration()
		if err != nil {
			return fmt.Errorf("failed to initialize configuration manager: %w", err)
		}
	}

	// Save the configuration
	if err := cm.SaveConfiguration(m.currentConfig); err != nil {
		return fmt.Errorf("failed to save configuration: %w", err)
	}

	return nil
}

// updateAddNetwork handles updates to the add network view
func (m *CLIModel) updateAddNetwork(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case BackToNetworkMenuMsg:
		// Return to the network menu
		m.menuItems = NewNetworkMenu()
		m.selectedMenu = 0
		m.currentView = constants.NetworkMenuView
		return m, nil
	case AddNetworkRequestMsg:
		// Parse and validate chain ID
		chainID, err := strconv.ParseInt(msg.ChainID, 10, 64)
		if err != nil {
			m.addNetworkComponent.SetError(errors.New(localization.Labels["invalid_chain_id"]))
			return m, nil
		}

		// Validate required fields
		if strings.TrimSpace(msg.Name) == "" {
			m.addNetworkComponent.SetError(fmt.Errorf("network name cannot be empty"))
			return m, nil
		}

		if strings.TrimSpace(msg.RPCEndpoint) == "" {
			m.addNetworkComponent.SetError(fmt.Errorf("RPC endpoint cannot be empty"))
			return m, nil
		}

		if strings.TrimSpace(msg.Symbol) == "" {
			m.addNetworkComponent.SetError(fmt.Errorf("symbol cannot be empty"))
			return m, nil
		}

		// Create network configuration
		network := config.Network{
			Name:        strings.TrimSpace(msg.Name),
			RPCEndpoint: strings.TrimSpace(msg.RPCEndpoint),
			ChainID:     chainID,
			Symbol:      strings.TrimSpace(msg.Symbol),
			IsActive:    true,
		}

		// Get the network manager to perform classification and validation
		nm := getNetworkManager()

		// First, validate the network configuration
		if err := nm.ValidateNetwork(network); err != nil {
			m.addNetworkComponent.SetError(fmt.Errorf("%s: %v", localization.Labels["network_validation_failed"], err))
			return m, nil
		}

		// Add the network using NetworkManager with classification info
		classificationInfo, err := addNetworkWithClassificationInfo(network)
		if err != nil {
			m.addNetworkComponent.SetError(fmt.Errorf("failed to add network: %v", err))
			return m, nil
		}

		// Provide user feedback about the classification
		var feedbackMsg string
		switch classificationInfo.Type {
		case blockchain.NetworkTypeStandard:
			if classificationInfo.ChainInfo != nil {
				feedbackMsg = fmt.Sprintf("Network added successfully as standard network (found in ChainList: %s)", classificationInfo.ChainInfo.Name)
			} else {
				feedbackMsg = "Network added successfully as standard network"
			}
		case blockchain.NetworkTypeCustom:
			feedbackMsg = "Network added successfully as custom network (not found in ChainList)"
			if classificationInfo.Source == "manual_offline" {
				feedbackMsg += " - " + localization.Labels["chainlist_unavailable_warning"]
			}
		default:
			feedbackMsg = "Network added successfully"
		}

		// Set success message (if the component supports it)
		if setter, ok := interface{}(m.addNetworkComponent).(interface{ SetSuccessMessage(string) }); ok {
			setter.SetSuccessMessage(feedbackMsg)
		}

		// Reload configuration to get the updated networks
		if err := m.ensureConfigAndNetworksLoaded(); err != nil {
			m.addNetworkComponent.SetError(fmt.Errorf("failed to reload configuration: %v", err))
			return m, nil
		}

		// Initialize the network list component if it hasn't been initialized yet
		if m.networkListComponent.table.Rows() == nil {
			m.networkListComponent = NewNetworkListComponent()
		}

		// Update the network list
		m.networkListComponent.UpdateNetworks(m.currentConfig)

		// Return to the network list view
		m.currentView = constants.NetworkListView

		return m, nil
	}

	// Update the add network component
	addNetwork, cmd := m.addNetworkComponent.Update(msg)
	m.addNetworkComponent = *addNetwork

	return m, cmd
}
