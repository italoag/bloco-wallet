package ui

import (
	"blocowallet/internal/constants"

	"github.com/charmbracelet/lipgloss"
)

type Styles struct {
	Header             lipgloss.Style
	Content            lipgloss.Style
	Footer             lipgloss.Style
	TopStrip           lipgloss.Style
	MenuItem           lipgloss.Style
	MenuSelected       lipgloss.Style
	SelectedTitle      lipgloss.Style
	MenuTitle          lipgloss.Style
	MenuDesc           lipgloss.Style
	ErrorStyle         lipgloss.Style
	SuccessStyle       lipgloss.Style
	WalletDetails      lipgloss.Style
	StatusBar          lipgloss.Style
	Splash             lipgloss.Style
	StatusBarLeft      lipgloss.Style
	StatusBarCenter    lipgloss.Style
	StatusBarRight     lipgloss.Style
	Dialog             lipgloss.Style
	DialogButton       lipgloss.Style
	DialogButtonActive lipgloss.Style
	GreenCheck         lipgloss.Style
	RedCross           lipgloss.Style
	SelectedStyle      lipgloss.Style
}

func createStyles() Styles {
	return Styles{
		Header: lipgloss.NewStyle().
			Align(lipgloss.Left).
			Padding(1, 2),

		Content: lipgloss.NewStyle().
			Align(lipgloss.Left).
			Padding(1, 2),

		Footer: lipgloss.NewStyle().
			Align(lipgloss.Left).
			PaddingLeft(1).
			PaddingRight(1).
			Background(lipgloss.Color("#7D56F4")),

		TopStrip: lipgloss.NewStyle().Margin(1, constants.StyleMargin).Padding(0, constants.StyleMargin),
		MenuItem: lipgloss.NewStyle().
			Width(constants.StyleWidth).
			Margin(0, constants.StyleMargin).
			Padding(0, constants.StyleMargin).
			Border(lipgloss.HiddenBorder(), false, false, false, true),
		MenuSelected: lipgloss.NewStyle().
			Foreground(lipgloss.Color("99")).
			Bold(true).
			Margin(0, constants.StyleMargin).
			Padding(0, constants.StyleMargin).
			Border(lipgloss.NormalBorder(), false, false, false, true).
			Width(constants.StyleWidth),
		SelectedTitle: lipgloss.NewStyle().Bold(true).
			Margin(0, constants.StyleMargin).
			Padding(0, constants.StyleMargin).
			Foreground(lipgloss.Color("99")),
		MenuTitle: lipgloss.NewStyle().
			Margin(0, constants.StyleMargin).
			Padding(0, constants.StyleMargin).
			Bold(true),
		MenuDesc: lipgloss.NewStyle().
			Margin(0, constants.StyleMargin).
			Padding(0, constants.StyleMargin).
			Width(constants.StyleWidth).
			Foreground(lipgloss.Color("244")),
		ErrorStyle: lipgloss.NewStyle().
			Padding(1, 2).
			Margin(1, constants.StyleMargin).
			Foreground(lipgloss.Color("#FF0000")),
		SuccessStyle: lipgloss.NewStyle().
			Padding(1, 2).
			Margin(1, constants.StyleMargin).
			Foreground(lipgloss.Color("#00AA00")),
		WalletDetails: lipgloss.NewStyle().
			Margin(1, constants.StyleMargin).
			Padding(1, 2),
		StatusBar: lipgloss.NewStyle().
			Foreground(lipgloss.Color("241")).
			Padding(0, constants.StyleMargin),
		Splash: lipgloss.NewStyle().
			Align(lipgloss.Center).Padding(1, 2),
		StatusBarLeft: lipgloss.NewStyle().
			Background(lipgloss.Color("#7D56F4")).
			PaddingLeft(1).
			PaddingRight(1),
		StatusBarCenter: lipgloss.NewStyle().
			Background(lipgloss.Color("#454544")).
			PaddingLeft(1).
			PaddingRight(1),
		StatusBarRight: lipgloss.NewStyle().
			Background(lipgloss.Color("#CC5C87")).
			PaddingLeft(1).
			PaddingRight(1),
		Dialog: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#7D56F4")).
			Foreground(lipgloss.Color("#F5F5F5")).
			Padding(1, 4).
			Align(lipgloss.Center),
		DialogButton: lipgloss.NewStyle().
			Padding(0, 2).
			Margin(0, 1).
			Bold(true).
			Foreground(lipgloss.Color("#7D56F4")).
			Background(lipgloss.Color("#F5F5F5")).
			Border(lipgloss.HiddenBorder()).
			BorderForeground(lipgloss.Color("#7D56F4")),
		DialogButtonActive: lipgloss.NewStyle().
			Padding(0, 2).
			Margin(0, 1).
			Bold(true).
			Foreground(lipgloss.Color("#F5F5F5")).
			Background(lipgloss.Color("#7D56F4")).
			Border(lipgloss.HiddenBorder()).
			BorderForeground(lipgloss.Color("#7D56F4")),
		GreenCheck: lipgloss.NewStyle().
			Foreground(lipgloss.Color("70")),
		RedCross: lipgloss.NewStyle().
			Foreground(lipgloss.Color("1")),
		SelectedStyle: lipgloss.NewStyle().
			Foreground(lipgloss.Color("99")).
			Bold(true),
	}
}
