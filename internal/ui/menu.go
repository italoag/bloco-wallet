package ui

import (
	"blocowallet/pkg/config"
	"blocowallet/pkg/localization"
)

// menuItem representa uma única opção no menu
type menuItem struct {
	title       string
	description string
}

// Title retorna o título do menuItem
func (i menuItem) Title() string {
	return i.title
}

// Description retorna a descrição do menuItem
func (i menuItem) Description() string {
	return i.description
}

// FilterValue retorna o valor de filtro do menuItem
func (i menuItem) FilterValue() string {
	return i.title
}

// NewMenu cria e retorna uma lista de itens do menu
func NewMenu() []menuItem {
	return []menuItem{
		{title: localization.Labels["create_new_wallet"], description: localization.Labels["create_new_wallet_desc"]},
		{title: localization.Labels["list_wallets"], description: localization.Labels["list_wallets_desc"]},
		{title: localization.Labels["configuration"], description: localization.Labels["configuration_desc"]},
		{title: localization.Labels["exit"], description: localization.Labels["exit_desc"]},
	}
}

// NewImportMenu cria e retorna uma lista de itens do menu de importação
func NewImportMenu() []menuItem {
	return []menuItem{
		{title: localization.Labels["import_mnemonic"], description: localization.Labels["import_mnemonic_desc"]},
		{title: localization.Labels["import_private_key"], description: localization.Labels["import_private_key_desc"]},
		{title: localization.Labels["back_to_menu"], description: localization.Labels["back_to_menu_desc"]},
	}
}

// NewConfigMenu cria e retorna uma lista de itens do menu de configuração
func NewConfigMenu() []menuItem {
	return []menuItem{
		{title: localization.Labels["networks"], description: localization.Labels["networks_desc"]},
		{title: localization.Labels["language"], description: localization.Labels["language_desc"]},
		{title: localization.Labels["back_to_menu"], description: localization.Labels["back_to_menu_desc"]},
	}
}

// NewNetworkMenu cria e retorna uma lista de itens do menu de redes
func NewNetworkMenu() []menuItem {
	return []menuItem{
		{title: localization.Labels["add_network"], description: localization.Labels["add_network_desc"]},
		{title: localization.Labels["network_list"], description: localization.Labels["network_list_desc"]},
		{title: localization.Labels["back_to_menu"], description: localization.Labels["back_to_menu_desc"]},
	}
}

// NewLanguageMenu cria e retorna uma lista de itens do menu de idiomas
func NewLanguageMenu(cfg *config.Config) []menuItem {
	// Get the list of available languages
	languages := localization.GetAvailableLanguages(cfg.LocaleDir)

	// Create menu items for each language
	menuItems := make([]menuItem, 0, len(languages)+1)

	// Add each language as a menu item
	for _, lang := range languages {
		langName := localization.GetLanguageName(lang)
		// Mark the current language with an asterisk
		title := langName
		if lang == cfg.Language {
			title = langName + " ✓ " + localization.Labels["current"]
		}
		menuItems = append(menuItems, menuItem{
			title:       title,
			description: localization.Labels["language"] + ": " + lang,
		})
	}

	// Add a "Back" option
	menuItems = append(menuItems, menuItem{
		title:       localization.Labels["back_to_menu"],
		description: localization.Labels["back_to_menu_desc"],
	})

	return menuItems
}
