package ui

import (
	"fmt"
	"strconv"
	"strings"

	"blocowallet/internal/wallet"

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
	title := "Select an option"
	switch stage {
	case 0:
		title = "Word count"
		for _, count := range []int{12, 15, 18, 21, 24} {
			items = append(items, createOptionItem{
				title: strconv.Itoa(count) + " words", description: mnemonicStrengthDescription(count), value: strconv.Itoa(count),
			})
		}
	case 1:
		title = "BIP39 language"
		for _, language := range wallet.SupportedBIP39Languages() {
			name := strings.ReplaceAll(string(language), "_", " ")
			items = append(items, createOptionItem{title: strings.ToUpper(name[:1]) + name[1:], description: "Official BIP39 word list", value: string(language)})
		}
	case 3:
		title = "EVM derivation path"
		items = append(items,
			createOptionItem{title: "Standard account 0", description: "Recommended default for the first EVM account", value: "m/44'/60'/0'/0/0"},
			createOptionItem{title: "Standard account 1", description: "Separate BIP44 account index", value: "m/44'/60'/1'/0/0"},
			createOptionItem{title: "Next address", description: "Address index 1 under the default account", value: "m/44'/60'/0'/0/1"},
			createOptionItem{title: "Custom path…", description: "Enter another validated EVM BIP44 path", value: "custom"},
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
	model.createOptionList = selector
}

func mnemonicStrengthDescription(wordCount int) string {
	switch wordCount {
	case 12:
		return "128-bit entropy • recommended for most wallets"
	case 15:
		return "160-bit entropy"
	case 18:
		return "192-bit entropy"
	case 21:
		return "224-bit entropy"
	case 24:
		return "256-bit entropy • longest backup"
	default:
		return "BIP39 mnemonic"
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
