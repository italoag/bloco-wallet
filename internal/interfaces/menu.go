package interfaces

import (
	"blocowallet/localization"
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
		{title: localization.Labels["import_wallet"], description: localization.Labels["import_wallet_desc"]},
		{title: localization.Labels["list_wallets"], description: localization.Labels["list_wallets_desc"]},
		{title: localization.Labels["exit"], description: localization.Labels["exit_desc"]},
	}
}
