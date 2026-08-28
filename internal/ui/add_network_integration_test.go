package ui

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"blocowallet/internal/blockchain"
	"blocowallet/internal/constants"
	"blocowallet/pkg/config"
	"blocowallet/pkg/logger"
)

// helper to create a test RPC server that returns a specified chainID (in hex)
func newRPCServer(t *testing.T, chainHex string, failFirst *bool) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only accept POST with application/json
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		body, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()
		// Minimal check that it's calling eth_chainId
		if !strings.Contains(string(body), "eth_chainId") {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"code":-32601,"message":"method not found"}}`))
			return
		}
		if failFirst != nil && *failFirst {
			// Return wrong chain id once
			*failFirst = false
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":"0x2"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":"` + chainHex + `"}`))
	}))
}

func setupTestLogger(t *testing.T) (logger.Logger, string) {
	t.Helper()
	dir := t.TempDir()
	lg, err := logger.NewFileLogger(logger.LoggingConfig{
		LogDir:      dir,
		LogLevel:    "debug",
		MaxFileSize: 1,
		MaxBackups:  1,
		MaxAge:      1,
	})
	if err != nil {
		t.Fatalf("failed to create test logger: %v", err)
	}
	SetLogger(lg)
	return lg, dir
}

// drive runs msg through Update and then, if cmd is returned, executes it and feeds the message back into Update.
func drive(t *testing.T, c *AddNetworkComponent, msg tea.Msg) {
	t.Helper()
	var cmd tea.Cmd
	c, cmd = c.Update(msg)
	if cmd != nil {
		out := cmd()
		_, _ = c.Update(out)
	}
}

func TestAddNetworkKeyboardFocusReachesDecimalsAndRPCReference(t *testing.T) {
	component := NewAddNetworkComponent()
	for _, step := range []struct {
		value string
		read  func() string
	}{
		{"Custom", func() string { return component.nameInput.Value() }},
		{"123", func() string { return component.chainIDInput.Value() }},
		{"CUS", func() string { return component.symbolInput.Value() }},
		{"6", func() string { return component.decimalsInput.Value() }},
		{"env:BLOCO_TEST_RPC", func() string { return component.rpcEndpointInput.Value() }},
	} {
		_, _ = component.Update(tea.KeyMsg{Type: tea.KeyTab})
		_, _ = component.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(step.value)})
		if step.read() != step.value {
			t.Fatalf("keyboard input did not reach focused field: got %q, want %q", step.read(), step.value)
		}
	}
}

func TestAddNetworkEscapeCancelsAndStaleResultIsIgnored(t *testing.T) {
	component := NewAddNetworkComponent()
	component.adding = true
	component.searchGeneration = 7
	model := &CLIModel{currentView: constants.AddNetworkView, addNetworkComponent: component, styles: createStyles()}
	_, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if model.currentView != constants.NetworkMenuView || model.addNetworkComponent.operationContext.Err() == nil {
		t.Fatal("global escape did not cancel add-network operation")
	}
	model.addNetworkComponent = NewAddNetworkComponent()
	model.currentView = constants.AddNetworkView
	_, _ = model.updateAddNetwork(networkCommitResultMsg{componentID: component.id, operationID: 7, config: &config.Config{}})
	if model.currentView != constants.AddNetworkView {
		t.Fatal("stale add-network result changed current view")
	}
}

func TestAddNetworkClearsAuthoritativeDecimalsWhenIdentityChanges(t *testing.T) {
	component := NewAddNetworkComponent()
	component.isSearchFocused = false
	component.focusIndex = 2
	component.chainIDInput.SetValue("1")
	component.decimalsInput.SetValue("18")
	component.nativeDecimals = 18
	component.nativeDecimalsSet = true
	component.updateFocus()
	_, _ = component.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	if component.nativeDecimalsSet || component.decimalsInput.Value() != "" {
		t.Fatal("stale native decimals survived chain identity edit")
	}
}

func TestAddNetworkAcceptsCredentialReferenceWithoutPersistingSecret(t *testing.T) {
	rpcServer := newRPCServer(t, "0x1", nil)
	defer rpcServer.Close()
	parsedRPC, _ := url.Parse(rpcServer.URL)
	ConfigureRPCGateway(blockchain.NewRPCGateway(blockchain.RPCGatewayOptions{AllowedLocalTargets: []string{parsedRPC.Host}}))
	defer ConfigureRPCGateway(nil)
	t.Setenv("BLOCO_TEST_NETWORK_RPC", rpcServer.URL)
	component := NewAddNetworkComponent()
	component.isSearchFocused = false
	component.focusIndex = 1
	component.nameInput.SetValue("Referenced network")
	component.chainIDInput.SetValue("1")
	component.symbolInput.SetValue("ETH")
	component.decimalsInput.SetValue("18")
	component.rpcEndpointInput.SetValue("env:BLOCO_TEST_NETWORK_RPC")
	_, command := component.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if command == nil {
		t.Fatal("referenced endpoint did not submit")
	}
	message := command()
	request, ok := message.(AddNetworkRequestMsg)
	if !ok {
		t.Fatalf("expected AddNetworkRequestMsg, got %T", message)
	}
	if request.RPCEndpoint != "" || request.RPCEndpointRef != "env:BLOCO_TEST_NETWORK_RPC" {
		t.Fatalf("credential value crossed persistence boundary: %+v", request)
	}
}

func TestIntegration_AddNetwork_EndToEnd_CrossArch(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	cases := []struct {
		name string
		arch string
	}{
		{"amd64 flow", "amd64"},
		{"arm64 flow", "arm64"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Override architecture detector
			oldArch := archDetector
			archDetector = func() string { return tc.arch }
			defer func() { archDetector = oldArch }()

			// Logger to temp dir
			lg, logDir := setupTestLogger(t)
			defer func() { _ = lg.Sync() }()

			// RPC server that always returns chainId 0x1
			rpcSrv := newRPCServer(t, "0x1", nil)
			defer rpcSrv.Close()
			parsedRPC, _ := url.Parse(rpcSrv.URL)
			ConfigureRPCGateway(blockchain.NewRPCGateway(blockchain.RPCGatewayOptions{AllowedLocalTargets: []string{parsedRPC.Host}}))
			defer ConfigureRPCGateway(nil)

			// Build component
			c := NewAddNetworkComponent()

			// Start with Init (popular suggestions generated internally)
			msg := c.Init()()
			_, _ = c.Update(msg)

			// Simulate typing in search to trigger logging paths
			// Use rune input so both arm64/manual and default code paths log
			drive(t, &c, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})

			// Provide suggestions manually (bypass real ChainList HTTP)
			sugg := blockchain.NetworkSuggestion{ChainID: 1, Name: "Ethereum Mainnet", Symbol: "ETH"}
			_, _ = c.Update(networkSuggestionsMsg([]blockchain.NetworkSuggestion{sugg}))

			// Fill network data directly with our test RPC URL
			c.fillNetworkData(sugg, rpcSrv.URL)
			c.decimalsInput.SetValue("18")

			// Move focus off search to enable form submission
			c.isSearchFocused = false
			c.focusIndex = 1
			c.updateFocus()

			// Force a chain ID mismatch for the first attempt
			c.chainIDInput.SetValue("2")

			// First submit: should produce an error due to mismatched chain ID
			_, cmd := c.Update(tea.KeyMsg{Type: tea.KeyEnter})
			if cmd == nil {
				t.Fatalf("expected command on submit")
			}
			out := cmd()
			// Feed the error back to component to set c.err
			_, _ = c.Update(out)

			if c.err == nil {
				t.Fatalf("expected an error on first submit (mismatched chain id)")
			}

			// Correct chain ID and submit again
			c.chainIDInput.SetValue("1")
			_, cmd2 := c.Update(tea.KeyMsg{Type: tea.KeyEnter})
			if cmd2 == nil {
				t.Fatalf("expected command on submit 2")
			}
			out2 := cmd2()

			// Expect AddNetworkRequestMsg on success
			req, ok := out2.(AddNetworkRequestMsg)
			if !ok {
				t.Fatalf("expected AddNetworkRequestMsg, got %T", out2)
			}

			if req.Name != "Ethereum Mainnet" || req.Symbol != "ETH" || req.ChainID != "1" || req.RPCEndpoint != rpcSrv.URL {
				t.Fatalf("unexpected AddNetworkRequestMsg values: %+v", req)
			}

			// Verify logs were written to files
			// Allow a tiny delay for file writes
			_ = lg.Sync()
			time.Sleep(50 * time.Millisecond)

			appLog := filepath.Join(logDir, "app.log")
			errLog := filepath.Join(logDir, "error.log")

			if _, err := os.Stat(appLog); err != nil {
				t.Fatalf("app.log not created: %v", err)
			}
			// app.log should have some content (debug/info)
			if fi, _ := os.Stat(appLog); fi.Size() == 0 {
				t.Fatalf("app.log is empty")
			}

			// error.log may or may not be non-empty depending on branches; ensure file exists
			if _, err := os.Stat(errLog); err != nil {
				t.Fatalf("error.log not created: %v", err)
			}
		})
	}
}
