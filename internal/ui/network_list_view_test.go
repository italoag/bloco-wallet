package ui

import (
	"strings"
	"testing"

	"blocowallet/internal/blockchain"
	"blocowallet/pkg/localization"

	"github.com/charmbracelet/bubbles/table"
)

func TestNetworkListUsesSingleActionableHelpArea(t *testing.T) {
	previousLanguage := localization.GetCurrentLanguage()
	localization.SetCurrentLanguage("en")
	t.Cleanup(func() { localization.SetCurrentLanguage(previousLanguage) })
	component := NewNetworkListComponent()
	component.table.SetRows([]table.Row{{"1", "Custom", "Custom / not checked", "123", "CUS", "Active", "custom_123"}})
	component.networksInfo["custom_123"] = NetworkInfo{
		Type: blockchain.NetworkTypeCustom, Source: "manual", CurrentHealth: "unchecked",
		PrivacyTracking: "", QuorumConfidence: "single_provider",
	}
	view := component.View()
	if strings.Contains(view, localization.Get("network_list_instructions")) {
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
