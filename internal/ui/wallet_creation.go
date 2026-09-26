package ui

import (
	"fmt"
	"strconv"

	"blocowallet/internal/wallet"
	"blocowallet/pkg/localization"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/lipgloss"
)

type createOptionItem struct {
	title       string
	description string
	value       string
}

func (item createOptionItem) Title() string       { return item.title }
func (item createOptionItem) Description() string { return item.description }
func (item createOptionItem) FilterValue() string { return item.title + " " + item.value }

func (model *CLIModel) configureCreateOptionList(stage int) {
	items := make([]list.Item, 0)
	title := localization.Get("create_select_option")
	switch stage {
	case 0:
		title = localization.Get("create_word_count_title")
		for _, count := range []int{12, 15, 18, 21, 24} {
			items = append(items, createOptionItem{
				title: localization.T("create_words_suffix", map[string]interface{}{"Count": count}), description: mnemonicStrengthDescription(count), value: strconv.Itoa(count),
			})
		}
	case 1:
		title = localization.Get("create_bip39_language_title")
		for _, language := range wallet.SupportedBIP39Languages() {
			items = append(items, createOptionItem{title: localization.Get("bip39_language_" + string(language)), description: localization.Get("create_bip39_wordlist_desc"), value: string(language)})
		}
	case 3:
		title = localization.Get("create_derivation_title")
		items = append(items,
			createOptionItem{title: localization.Get("create_path_account0"), description: localization.Get("create_path_account0_desc"), value: "m/44'/60'/0'/0/0"},
			createOptionItem{title: localization.Get("create_path_account1"), description: localization.Get("create_path_account1_desc"), value: "m/44'/60'/1'/0/0"},
			createOptionItem{title: localization.Get("create_path_next_address"), description: localization.Get("create_path_next_address_desc"), value: "m/44'/60'/0'/0/1"},
			createOptionItem{title: localization.Get("create_path_custom"), description: localization.Get("create_path_custom_desc"), value: "custom"},
		)
	}
	width := model.width - 8
	if width < 44 {
		width = 44
	}
	if width > 76 {
		width = 76
	}
	height := model.height - 14
	if height < 8 {
		height = 8
	}
	if height > 16 {
		height = 16
	}
	selector := list.New(items, list.NewDefaultDelegate(), width, height)
	selector.Title = title
	selector.SetFilteringEnabled(false)
	selector.SetShowStatusBar(false)
	localizeListKeyMap(&selector.KeyMap)
	model.createOptionList = selector
}

// localizeListKeyMap reapplies localized help descriptions on a bubbles list
// KeyMap, preserving each binding's key string and enabled state.
func localizeListKeyMap(km *list.KeyMap) {
	for _, entry := range []struct {
		binding *key.Binding
		label   string
	}{
		{&km.CursorUp, "help_up"},
		{&km.CursorDown, "help_down"},
		{&km.PrevPage, "list_help_prev_page"},
		{&km.NextPage, "list_help_next_page"},
		{&km.GoToStart, "list_help_go_start"},
		{&km.GoToEnd, "list_help_go_end"},
		{&km.Filter, "list_help_filter"},
		{&km.ClearFilter, "list_help_clear_filter"},
		{&km.CancelWhileFiltering, "list_help_cancel_filter"},
		{&km.AcceptWhileFiltering, "list_help_accept_filter"},
		{&km.ShowFullHelp, "list_help_more"},
		{&km.CloseFullHelp, "list_help_close"},
		{&km.Quit, "list_help_quit"},
		{&km.ForceQuit, "list_help_force_quit"},
	} {
		entry.binding.SetHelp(entry.binding.Help().Key, localization.Get(entry.label))
	}
}

func mnemonicStrengthDescription(wordCount int) string {
	switch wordCount {
	case 12:
		return localization.Get("create_entropy_12")
	case 15:
		return localization.Get("create_entropy_15")
	case 18:
		return localization.Get("create_entropy_18")
	case 21:
		return localization.Get("create_entropy_21")
	case 24:
		return localization.Get("create_entropy_24")
	default:
		return localization.Get("create_entropy_default")
	}
}

func renderMnemonicCards(words []string, availableWidth int) string {
	if len(words) == 0 {
		return ""
	}
	cardWidth := 12
	for _, word := range words {
		if width := lipgloss.Width(word) + 4; width > cardWidth {
			cardWidth = width
		}
	}
	if cardWidth > 20 {
		cardWidth = 20
	}
	if availableWidth <= 0 {
		availableWidth = cardWidth
	}
	columns := availableWidth / (cardWidth + 2)
	if columns < 1 {
		columns = 1
	}
	if columns > 6 {
		columns = 6
	}
	cardStyle := lipgloss.NewStyle().
		Width(cardWidth).
		Align(lipgloss.Center).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#7D56F4")).
		Padding(0, 1)
	numberStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#888888")).Bold(true)
	wordStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#F5F5F5")).Bold(true)
	rows := make([]string, 0, (len(words)+columns-1)/columns)
	for start := 0; start < len(words); start += columns {
		end := min(start+columns, len(words))
		cards := make([]string, 0, end-start)
		for index := start; index < end; index++ {
			content := numberStyle.Render(fmt.Sprintf("%02d", index+1)) + "\n" + wordStyle.Render(words[index])
			cards = append(cards, cardStyle.Render(content))
		}
		rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Top, cards...))
	}
	return lipgloss.JoinVertical(lipgloss.Left, rows...)
}
