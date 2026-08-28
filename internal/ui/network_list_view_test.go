package ui

import (
	"strings"
	"testing"

	"blocowallet/internal/blockchain"
	"blocowallet/pkg/localization"

	"github.com/charmbracelet/bubbles/table"
)

func TestNetworkListUsesSingleActionableHelpArea(t *testing.T) {
	previous := localization.Labels
	localization.Labels = map[string]string{
		"networks": "Networks", "network_name": "Name", "chain_id": "Chain ID", "symbol": "Symbol", "status": "Status",
		"active": "Active", "inactive": "Inactive", "add_network": "Add Network", "edit_network": "Edit Network",
		"delete_network": "Delete Network", "back": "Back", "network_list_instructions": "Use arrow keys to navigate, 'a' to add, 'e' to edit, 'd' to delete, 'esc' to go back.",
	}
	t.Cleanup(func() { localization.Labels = previous })
	component := NewNetworkListComponent()
	component.table.SetRows([]table.Row{{"1", "Custom", "Custom / not checked", "123", "CUS", "Active", "custom_123"}})
	component.networksInfo["custom_123"] = NetworkInfo{
		Type: blockchain.NetworkTypeCustom, Source: "manual", CurrentHealth: "not checked",
		PrivacyTracking: "unknown", QuorumConfidence: "none (single provider)",
	}
	view := component.View()
	if strings.Contains(view, localization.Labels["network_list_instructions"]) {
		t.Fatal("network list retained the duplicated static instruction paragraph")
	}
	for _, action := range []string{"Add Network", "Edit Network", "Delete Network", "Refresh", "Revalidate", "Back"} {
		if strings.Count(view, action) != 1 {
			t.Fatalf("action %q appears %d times: %q", action, strings.Count(view, action), view)
		}
	}
	if !strings.Contains(view, "Press v to revalidate") {
		t.Fatalf("selected network status has no remediation: %q", view)
	}
}
