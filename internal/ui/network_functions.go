package ui

import (
	"blocowallet/internal/blockchain"
	"blocowallet/internal/constants"
	"blocowallet/pkg/config"
	"blocowallet/pkg/localization"
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
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
	return m.networkListComponent.View()
}

// viewAddNetwork renders the add network view
func (m *CLIModel) viewAddNetwork() string {
	return m.addNetworkComponent.View()
}

type networkListReloadMsg struct {
	config *config.Config
	info   map[string]NetworkInfo
	status string
	err    error
}

func networkListReloadCmd(networkKey string, network *config.Network) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if network != nil {
			if err := getNetworkManager().UpdateNetworkContext(ctx, networkKey, *network); err != nil {
				return networkListReloadMsg{err: err}
			}
		}
		cfg, err := getConfigurationManager().ReloadConfiguration()
		if err != nil {
			return networkListReloadMsg{err: err}
		}
		info, err := getNetworkManager().ListNetworks()
		if err != nil {
			return networkListReloadMsg{err: err}
		}
		status := "Network information refreshed"
		if network != nil {
			for key, networkInfo := range info {
				if networkInfo.Network.ChainID == network.ChainID {
					networkInfo.IsValidated = true
					networkInfo.PreviouslyValidated = true
					networkInfo.CurrentHealth = "reachable and chain ID verified now"
					info[key] = networkInfo
				}
			}
			status = "Network provider and chain identity revalidated"
		}
		return networkListReloadMsg{config: cfg, info: info, status: status}
	}
}

// updateNetworkList handles updates to the network list view
func (m *CLIModel) updateNetworkList(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, m.networkListComponent.keys.Add):
			// Add a new network
			m.initAddNetwork()
			return m, nil

		case key.Matches(msg, m.networkListComponent.keys.Edit):
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
			if network.NativeDecimalsSet {
				m.addNetworkComponent.decimalsInput.SetValue(strconv.Itoa(network.NativeDecimals))
				m.addNetworkComponent.nativeDecimals = network.NativeDecimals
				m.addNetworkComponent.nativeDecimalsSet = true
			}
			endpointValue := network.RPCEndpoint
			if network.RPCEndpointRef != "" {
				endpointValue = network.RPCEndpointRef
			}
			m.addNetworkComponent.rpcEndpointInput.SetValue(endpointValue)

			// Store the key for updating later
			m.editingNetworkKey = key

			// Set the current view to add network (which will function as edit)
			m.currentView = constants.AddNetworkView
			return m, nil

		case key.Matches(msg, m.networkListComponent.keys.Delete):
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

		case key.Matches(msg, m.networkListComponent.keys.Refresh):
			m.networkListComponent.SetBusy("Reloading stored network information...")
			m.networkListComponent.updateKeyAvailability()
			return m, networkListReloadCmd("", nil)

		case key.Matches(msg, m.networkListComponent.keys.Revalidate):
			networkKey := m.networkListComponent.GetSelectedNetworkKey()
			if networkKey == "" || m.currentConfig == nil {
				m.networkListComponent.SetError(errors.New(localization.Labels["no_network_selected"]))
				return m, nil
			}
			network, exists := m.currentConfig.Networks[networkKey]
			if !exists {
				m.networkListComponent.SetError(fmt.Errorf("network not found"))
				return m, nil
			}
			m.networkListComponent.SetBusy("Revalidating provider, chain identity, registry metadata, and privacy information...")
			m.networkListComponent.updateKeyAvailability()
			return m, networkListReloadCmd(networkKey, &network)

		case key.Matches(msg, m.networkListComponent.keys.ToggleHelp):
			networkList, cmd := m.networkListComponent.Update(msg)
			m.networkListComponent = *networkList
			return m, cmd

		case key.Matches(msg, m.networkListComponent.keys.Back):
			// Return to the network menu
			m.menuItems = NewNetworkMenu()
			m.selectedMenu = 0
			m.currentView = constants.NetworkMenuView
			return m, nil
		}

	case networkListReloadMsg:
		if msg.err != nil {
			m.networkListComponent.SetError(msg.err)
			m.networkListComponent.updateKeyAvailability()
			return m, nil
		}
		m.currentConfig = msg.config
		m.balanceConfig = msg.config
		m.networkListComponent.UpdateNetworksWithInfo(msg.config, msg.info)
		m.networkListComponent.SetStatus(msg.status)
		m.networkListComponent.updateKeyAvailability()
		return m, nil

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

type networkCommitResultMsg struct {
	componentID    string
	operationID    uint64
	classification *blockchain.NetworkClassification
	config         *config.Config
	edited         bool
	err            error
}

// updateAddNetwork handles updates to the add network view
func (m *CLIModel) updateAddNetwork(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case BackToNetworkMenuMsg:
		m.editingNetworkKey = ""
		// Return to the network menu
		m.menuItems = NewNetworkMenu()
		m.selectedMenu = 0
		m.currentView = constants.NetworkMenuView
		return m, nil
	case networkCommitResultMsg:
		if msg.componentID != m.addNetworkComponent.id || msg.operationID != m.addNetworkComponent.searchGeneration {
			return m, nil
		}
		m.addNetworkComponent.adding = false
		if msg.err != nil {
			m.addNetworkComponent.SetError(fmt.Errorf("network operation failed: %w", msg.err))
			return m, nil
		}
		feedback := "Network added successfully"
		if msg.edited {
			feedback = "Network updated successfully"
		} else if msg.classification != nil && msg.classification.Type == blockchain.NetworkTypeStandard {
			feedback = "Network added as a registry-listed network"
		} else if msg.classification != nil && msg.classification.Type == blockchain.NetworkTypeCustom {
			feedback = "Network added as a custom network"
		}
		m.lastOperationNotice = feedback
		m.editingNetworkKey = ""
		m.currentConfig = msg.config
		if m.networkListComponent.table.Rows() == nil {
			m.networkListComponent = NewNetworkListComponent()
		}
		m.networkListComponent.UpdateNetworks(m.currentConfig)
		m.currentView = constants.NetworkListView
		return m, nil
	case addNetworkErrorMsg:
		if msg.componentID != m.addNetworkComponent.id || msg.operationID != m.addNetworkComponent.searchGeneration {
			return m, nil
		}
		m.addNetworkComponent.adding = false
		m.addNetworkComponent.SetError(msg.err)
		return m, nil
	case AddNetworkRequestMsg:
		if msg.componentID != m.addNetworkComponent.id || msg.operationID != m.addNetworkComponent.searchGeneration {
			return m, nil
		}
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

		if strings.TrimSpace(msg.RPCEndpoint) == "" && strings.TrimSpace(msg.RPCEndpointRef) == "" {
			m.addNetworkComponent.SetError(fmt.Errorf("RPC endpoint or credential reference cannot be empty"))
			return m, nil
		}

		if strings.TrimSpace(msg.Symbol) == "" {
			m.addNetworkComponent.SetError(fmt.Errorf("symbol cannot be empty"))
			return m, nil
		}

		// Create network configuration
		network := config.Network{
			Name:              strings.TrimSpace(msg.Name),
			RPCEndpoint:       strings.TrimSpace(msg.RPCEndpoint),
			RPCEndpointRef:    strings.TrimSpace(msg.RPCEndpointRef),
			ChainID:           chainID,
			Symbol:            strings.TrimSpace(msg.Symbol),
			NativeDecimals:    msg.NativeDecimals,
			NativeDecimalsSet: msg.NativeDecimalsSet,
			IsActive:          true,
		}
		if m.currentConfig != nil {
			if existing, exists := m.currentConfig.Networks[m.editingNetworkKey]; exists {
				network.IsActive = existing.IsActive
				network.Explorer = existing.Explorer
			}
		}

		networkManager := getNetworkManager()
		operationContext := m.addNetworkComponent.operationContext
		componentID := msg.componentID
		operationID := msg.operationID
		editingKey := m.editingNetworkKey
		return m, func() tea.Msg {
			result := networkCommitResultMsg{componentID: componentID, operationID: operationID, edited: editingKey != ""}
			if editingKey != "" {
				result.err = networkManager.UpdateNetworkContext(operationContext, editingKey, network)
			} else {
				result.classification, result.err = networkManager.AddNetworkWithClassificationContext(operationContext, network)
			}
			if result.err == nil {
				result.config, result.err = networkManager.configManager.LoadConfiguration()
			}
			return result
		}
	}

	// Update the add network component
	addNetwork, cmd := m.addNetworkComponent.Update(msg)
	m.addNetworkComponent = *addNetwork

	return m, cmd
}
