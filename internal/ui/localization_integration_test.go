package ui

import (
	"context"
	"encoding/hex"
	"strconv"
	"strings"
	"testing"

	"blocowallet/internal/blockchain"
	"blocowallet/internal/constants"
	"blocowallet/internal/wallet"
	"blocowallet/pkg/config"
	"blocowallet/pkg/localization"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// switchLang pins the process locale for the duration of a test.
func switchLang(t *testing.T, lang string) {
	t.Helper()
	previous := localization.GetCurrentLanguage()
	localization.SetCurrentLanguage(lang)
	t.Cleanup(func() { localization.SetCurrentLanguage(previous) })
}

func menuActions(items []menuItem) []string {
	actions := make([]string, 0, len(items))
	for _, item := range items {
		actions = append(actions, item.action)
	}
	return actions
}

func TestMenuStableActionsAcrossLocales(t *testing.T) {
	expectedMain := []string{"create_wallet", "import_wallet", "list_wallets", "safe", "configuration", "exit"}
	expectedConfig := []string{"networks", "language", "keepass", "back"}
	expectedImport := []string{"mnemonic", "private_key", "keystore", "keystore_batch", "bloco_encrypted", "watch_only", "mnemonic_batch", "back"}
	expectedNetwork := []string{"add_network", "network_list", "back"}
	titles := map[string][2]string{
		"en": {"Create New", "Configuration"},
		"pt": {"Criar Nova", "Configuração"},
		"es": {"Crear Nueva", "Configuración"},
	}
	for _, lang := range []string{"en", "pt", "es"} {
		t.Run(lang, func(t *testing.T) {
			switchLang(t, lang)
			assert.Equal(t, expectedMain, menuActions(NewMenu()))
			assert.Equal(t, expectedConfig, menuActions(NewConfigMenu()))
			assert.Equal(t, expectedImport, menuActions(NewImportMenu()))
			assert.Equal(t, expectedNetwork, menuActions(NewNetworkMenu()))
			assert.Equal(t, titles[lang][0], NewMenu()[0].title)
			assert.Equal(t, titles[lang][1], NewMenu()[4].title)

			cfg := &config.Config{Language: "en"}
			langMenu := NewLanguageMenu(cfg)
			require.Len(t, langMenu, len(localization.GetAvailableLanguages(""))+1)
			for _, item := range langMenu[:len(langMenu)-1] {
				assert.Equal(t, "select_language", item.action)
				assert.Contains(t, []string{"en", "pt", "es"}, item.value)
			}
			assert.Equal(t, "back", langMenu[len(langMenu)-1].action)
		})
	}
}

func TestMenuRoutingUsesActionsNotTitles(t *testing.T) {
	switchLang(t, "pt")
	model := &CLIModel{styles: createStyles()}
	model.menuItems = NewMenu()
	for i := range model.menuItems {
		model.menuItems[i].title = "arbitrary " + model.menuItems[i].action
		model.menuItems[i].description = "mutated"
	}
	model.selectedMenu = 4 // configuration
	_, _ = model.updateMenu(tea.KeyMsg{Type: tea.KeyEnter})
	assert.Equal(t, constants.ConfigurationView, model.currentView)
	assert.Equal(t, []string{"networks", "language", "keepass", "back"}, menuActions(model.menuItems))
}

func TestLanguageMenuMarksCurrentWithNormalizedCode(t *testing.T) {
	switchLang(t, "pt")
	cfg := &config.Config{Language: "pt-BR"}
	items := NewLanguageMenu(cfg)
	var ptItem *menuItem
	for i := range items {
		if items[i].value == "pt" {
			ptItem = &items[i]
		}
	}
	require.NotNil(t, ptItem)
	assert.Contains(t, ptItem.title, "✓", "pt-BR config must mark the pt item as current")
}

func TestLanguageSelectionFlowPersistsStableCode(t *testing.T) {
	switchLang(t, "en")
	t.Setenv("BLOCO_WALLET_APP_APP_DIR", t.TempDir())
	previousManager := globalConfigManager
	manager := config.NewConfigurationManager()
	ConfigureConfigurationManager(manager)
	t.Cleanup(func() { globalConfigManager = previousManager })

	cfg, err := manager.LoadConfiguration()
	require.NoError(t, err)
	model := &CLIModel{styles: createStyles(), currentConfig: cfg, menuItems: NewConfigMenu()}
	model.initLanguageSelection()
	require.Equal(t, constants.LanguageSelectionView, model.currentView)

	target := -1
	for i, item := range model.menuItems {
		if item.action == "select_language" && item.value == "es" {
			target = i
		}
	}
	require.GreaterOrEqual(t, target, 0)
	model.selectedMenu = target
	_, _ = model.updateLanguageSelection(tea.KeyMsg{Type: tea.KeyEnter})

	assert.Equal(t, "es", localization.GetCurrentLanguage())
	require.NotNil(t, model.currentConfig)
	assert.Equal(t, "es", localization.NormalizeLanguage(model.currentConfig.Language))
	reloaded, err := manager.ReloadConfiguration()
	require.NoError(t, err)
	assert.Equal(t, "es", reloaded.Language, "config must persist the stable code once")
	assert.Equal(t, constants.ConfigurationView, model.currentView)
}

func TestFooterHintsLocalizedAcrossLocales(t *testing.T) {
	expect := map[string][2]string{
		"en": {"Open", "Delete"},
		"pt": {"Abrir", "Excluir"},
		"es": {"Abrir", "Eliminar"},
	}
	for _, lang := range []string{"en", "pt", "es"} {
		t.Run(lang, func(t *testing.T) {
			switchLang(t, lang)
			model := newRecoveryTestModel(t, wallet.AccountSummary{})
			summary := importRecoveryUIAccount(t, model)
			// Vault setup re-initializes localization to en; pin the target
			// locale again before rendering.
			localization.SetCurrentLanguage(lang)
			model.applyAccountList([]wallet.AccountSummary{summary})
			model.currentView = constants.ListWalletsView

			body := ansi.Strip(model.viewListWallets())
			assert.NotContains(t, body, "Open")
			assert.NotContains(t, body, "Delete")

			footer := ansi.Strip(footerRows(t, model))
			assert.Contains(t, footer, expect[lang][0])
			assert.Contains(t, footer, expect[lang][1])
			assert.Equal(t, 1, strings.Count(footer, expect[lang][0]), "open hint must appear exactly once")
		})
	}
}

func TestHotSwitchPreservesCanonicalFieldsAndTable(t *testing.T) {
	switchLang(t, "en")
	model := &CLIModel{styles: createStyles(), menuItems: NewMenu()}
	model.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	state := newCanonicalImportState(wallet.ImportMethodKeystore)
	require.NotEmpty(t, state.fields)
	state.fields[0].input.SetValue("/tmp/keystore.json")
	model.canonicalImport = state

	// canonical wallet table: 4 columns, second row selected
	columns, rows := accountTableLayout(120, responsiveTableAccounts())
	model.walletTable.SetColumns(columns)
	model.walletTable.SetRows(rows)
	model.walletTable.SetCursor(1)

	localization.SetCurrentLanguage("pt")
	model.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	assert.Equal(t, "/tmp/keystore.json", state.fields[0].input.Value(), "field value must survive locale switch")
	assert.NotEqual(t, "", state.fields[0].input.Placeholder)

	cols := model.walletTable.Columns()
	require.Len(t, cols, 4)
	assert.Equal(t, localization.Get("name"), cols[0].Title)
	assert.Equal(t, localization.Get("ethereum_address"), cols[3].Title)
	assert.NotContains(t, []string{cols[0].Title}, "ID", "canonical table must not gain a UUID column")
	assert.Equal(t, 1, model.walletTable.Cursor(), "cursor preserved")
	assert.Equal(t, len(rows), len(model.walletTable.Rows()))
	assert.Equal(t, localization.Get(state.fields[0].labelKey), state.fields[0].input.Placeholder,
		"placeholder must re-render in the new locale")

	// menu items re-localized but keep actions
	assert.Equal(t, "Criar Nova", model.menuItems[0].title)
	assert.Equal(t, "create_wallet", model.menuItems[0].action)

	localization.SetCurrentLanguage("es")
	model.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	assert.Equal(t, "Crear Nueva", model.menuItems[0].title)
	assert.Equal(t, "/tmp/keystore.json", state.fields[0].input.Value())
}

func TestRecoveryPassphraseLabelKeepsRawBytes(t *testing.T) {
	for _, lang := range []string{"en", "pt", "es"} {
		switchLang(t, lang)
		label := localization.Get("recovery_passphrase_label")
		assert.NotContains(t, label, "{{")
		assert.True(t, strings.HasSuffix(label, ":"))
	}
}

func TestRecoveryPassphraseBytesAcrossLocales(t *testing.T) {
	const passphrase = "test passphrase boundary"
	for _, lang := range []string{"en", "pt", "es"} {
		t.Run(lang, func(t *testing.T) {
			switchLang(t, lang)
			model := newRecoveryTestModel(t, wallet.AccountSummary{})
			summary, err := model.Vault.ImportMnemonic(context.Background(), wallet.MnemonicImportRequest{
				Name: "Passphrase " + lang, Mnemonic: "test test test test test test test test test test test junk",
				BIP39Passphrase:        passphrase,
				StoragePassword:        []byte(recoveryUITestPassword),
				ConfirmStoragePassword: []byte(recoveryUITestPassword),
			})
			require.NoError(t, err)
			model.selectedAccount = &summary
			localization.SetCurrentLanguage(lang)
			model.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
			model.initRecovery()
			state := model.recovery
			for i, action := range state.actions {
				if action.kind == wallet.RecoveryPassphrase && !action.export {
					state.selected = i
				}
			}
			// The ASCII confirmation token must stay untranslated in every locale.
			cmd := recoveryDriveToConfirm(t, model, "REVEAL")
			recoveryDeliverResult(t, model, cmd)
			require.Equal(t, recoveryStageRevealed, state.stage)

			flattened := strings.ReplaceAll(strings.Join(model.recoverySecretBody(), "\n"), "\n", "")
			label := localization.Get("recovery_passphrase_label")
			assert.Contains(t, flattened, label, "localized label must render")
			assert.Contains(t, flattened, strings.ReplaceAll(strconv.Quote(passphrase), "\n", ""),
				"quoted passphrase bytes must be recoverable verbatim")
			if lang != "en" {
				assert.NotContains(t, flattened, "BIP39 passphrase:")
			}
		})
	}
}

func TestPersonalSignEntryPreservesWidget(t *testing.T) {
	for _, lang := range []string{"en", "pt", "es"} {
		t.Run(lang, func(t *testing.T) {
			switchLang(t, lang)
			input := textinput.New()
			input.SetValue("hello widget")
			input.Focus()
			model := &CLIModel{
				styles: createStyles(),
				personalSign: &personalSignState{
					phase:   personalSignEntry,
					account: wallet.AccountSummary{Address: "0x1234"},
					message: input,
				},
			}
			view := model.viewPersonalSign()
			assert.Contains(t, view, model.personalSign.message.View(), "widget output must be embedded verbatim, not sanitized")
			assert.Contains(t, ansi.Strip(view), "hello widget")
		})
	}
}

func TestRenderCalldataLinePreservesPayload(t *testing.T) {
	payload, err := hex.DecodeString(strings.Repeat("ab", 20000))
	require.NoError(t, err)
	for _, lang := range []string{"en", "pt", "es"} {
		t.Run(lang, func(t *testing.T) {
			switchLang(t, lang)
			line := renderCalldataLine(payload)
			label := localization.Get("tx_calldata_line")
			require.True(t, strings.HasPrefix(line, label+" 0x"))
			decoded, err := hex.DecodeString(strings.TrimPrefix(line, label+" 0x"))
			require.NoError(t, err)
			assert.Equal(t, payload, decoded, "calldata must round-trip byte-exact (>8192 hex chars)")
		})
	}
}

func TestEIP712PreviewLocalizedAcrossLocales(t *testing.T) {
	const digest = "0xbe609aee343fb3c4b28e1df9e632fca64fcfaede20f02e86244efddf30957bd2"
	chainPrefix := map[string]string{"en": "Chain:", "pt": "Cadeia:", "es": "Cadena:"}
	for _, lang := range []string{"en", "pt", "es"} {
		t.Run(lang, func(t *testing.T) {
			switchLang(t, lang)
			key, err := crypto.HexToECDSA("4646464646464646464646464646464646464646464646464646464646464646")
			require.NoError(t, err)
			signerAddress := crypto.PubkeyToAddress(key.PublicKey)
			cfg := &config.Config{Networks: map[string]config.Network{"mainnet": {Name: "Mainnet", ChainID: 1, IsActive: true}}}
			model := &CLIModel{
				width: 120, height: 30, styles: createStyles(), currentConfig: cfg,
				transactionAuthorizer: personalSignAuthorizerStub{},
				selectedAccount: &wallet.AccountSummary{
					AccountID: "11111111-1111-4111-8111-111111111111", Name: "Signer",
					Address: signerAddress.Hex(), SignerKind: wallet.SignerKindSoftware, State: wallet.AccountStateActive,
					Capabilities: wallet.CapabilitySignMessage,
				},
			}
			model.ConfigureMessageSigningFactory(func(context.Context) (MessageSigningService, error) {
				return &personalSignServiceStub{signer: signerAddress}, nil
			})
			model.initWalletDetailsComponents()
			model.updateWalletDetailsKeyAvailability()
			_, _ = model.updateWalletDetails(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})
			require.Equal(t, constants.EIP712SignView, model.currentView)
			_, _ = model.updateEIP712Sign(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
			model.eip712Sign.typedData.SetValue(eip712SignUITestFixture)
			_, _ = model.updateEIP712Sign(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})

			view := ansi.Strip(model.viewEIP712Sign())
			assert.Contains(t, view, chainPrefix[lang], "preview heading must be localized")
			for _, fixed := range []string{"Ether Mail", "Cow", "Hello, Bob!", "Digest: " + digest} {
				assert.Contains(t, view, fixed, "signed payload fields must be unchanged")
			}
		})
	}
}

func TestNetworkViewsLocalizedAcrossLocales(t *testing.T) {
	expect := map[string][2]string{
		"en": {"Networks", "No networks found. Add a network to get started."},
		"pt": {"Redes", "Nenhuma rede encontrada. Adicione uma rede para começar."},
		"es": {"Redes", "No se encontraron redes. Añade una red para empezar."},
	}
	for _, lang := range []string{"en", "pt", "es"} {
		t.Run(lang, func(t *testing.T) {
			switchLang(t, lang)
			component := NewNetworkListComponent()
			component.SetSize(120, 40)
			view := ansi.Strip(component.View())
			assert.Contains(t, view, expect[lang][0])
			assert.Contains(t, view, expect[lang][1])
			assert.Contains(t, view, localization.Get("help_add_network"))
			if lang != "en" {
				assert.NotContains(t, view, "No networks found")
			}
		})
	}
}

func TestNetworkListRefreshRerendersLocalizedCells(t *testing.T) {
	switchLang(t, "en")
	component := NewNetworkListComponent()
	cfg := &config.Config{Networks: map[string]config.Network{
		"net1": {Name: "Net One", ChainID: 1, Symbol: "ONE", IsActive: true, Tracking: "partial"},
	}}
	component.UpdateNetworksWithInfo(cfg, map[string]NetworkInfo{
		"net1": {Type: blockchain.NetworkTypeStandard, IsValidated: true, PreviouslyValidated: true,
			CurrentHealth: "verified", PrivacyTracking: "partial", QuorumConfidence: "single_provider",
			Source: "stored_registry_claim"},
	})
	component.table.SetCursor(0)
	enRow := component.table.Rows()[0]
	require.Equal(t, localization.Get("active"), enRow[5])
	assert.Contains(t, enRow[2], localization.Get("net_health_verified"))
	enView := ansi.Strip(component.View())
	assert.Contains(t, enView, "reachable and chain ID verified now")
	assert.Contains(t, enView, "previously observed partial")
	assert.Contains(t, enView, "none (single provider)")

	for _, lang := range []string{"pt", "es"} {
		localization.SetCurrentLanguage(lang)
		component.refreshLocalizedColumns()
		component.refreshLocalizedRows()
		row := component.table.Rows()[0]
		assert.Equal(t, localization.Get("active"), row[5])
		assert.Equal(t, "net1", row[6], "network key mapping preserved")
		assert.Equal(t, "Net One", row[1])
		assert.Contains(t, row[2], localization.Get("net_health_verified"))
		assert.NotContains(t, row[2], "reachable and chain ID verified now", "stale EN health cell")
		view := ansi.Strip(component.View())
		assert.Contains(t, view, localization.T("net_tracking_observed", map[string]interface{}{"Tracking": "partial"}))
		assert.NotContains(t, view, "previously observed")
		assert.NotContains(t, view, "none (single provider)")
	}
}

func TestCatalogsHaveNoRawGoFallbackTables(t *testing.T) {
	// The official catalogs are embedded TOML; Messages must expose the same
	// key set for every bundled locale with no English copies passed off as
	// translations for keys that diverge.
	en := localization.Messages("en")
	pt := localization.Messages("pt")
	es := localization.Messages("es")
	require.NotEmpty(t, en)
	assert.Equal(t, len(en), len(pt))
	assert.Equal(t, len(en), len(es))
	for _, key := range []string{"create_new_wallet", "configuration", "hint_open", "hint_delete"} {
		assert.NotEqual(t, en[key], pt[key], "%s must not be an English copy in pt", key)
		assert.NotEqual(t, en[key], es[key], "%s must not be an English copy in es", key)
	}
}

func TestCreateOptionListHelpAndSelectionAcrossLocales(t *testing.T) {
	switchLang(t, "en")
	model := &CLIModel{styles: createStyles()}
	model.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	model.createOptionsStage = 0
	model.configureCreateOptionList(0)
	require.Len(t, model.createOptionList.Items(), 5)
	model.createOptionList.Select(2) // 18 words

	up := model.createOptionList.KeyMap.CursorUp
	assert.Equal(t, "↑/k", up.Help().Key, "key string must not be localized")
	assert.Equal(t, "up", up.Help().Desc)
	assert.False(t, model.createOptionList.SettingFilter(), "filtering stays disabled")

	localization.SetCurrentLanguage("pt")
	model.refreshCreateOptionList()
	up = model.createOptionList.KeyMap.CursorUp
	assert.Equal(t, "↑/k", up.Help().Key)
	assert.Equal(t, localization.Get("help_up"), up.Help().Desc)
	assert.NotEqual(t, "up", up.Help().Desc, "help description must re-render in pt")
	assert.Equal(t, 2, model.createOptionList.Index(), "selection index preserved")
	item, ok := model.createOptionList.SelectedItem().(createOptionItem)
	require.True(t, ok)
	assert.Equal(t, "18", item.value, "selection stays on the same stable value")

	localization.SetCurrentLanguage("es")
	model.refreshCreateOptionList()
	assert.Equal(t, localization.Get("help_down"), model.createOptionList.KeyMap.CursorDown.Help().Desc)
	item, ok = model.createOptionList.SelectedItem().(createOptionItem)
	require.True(t, ok)
	assert.Equal(t, "18", item.value)
}
