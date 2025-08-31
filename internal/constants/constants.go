package constants

import "time"

const (
	PasswordCharLimit         = 64
	PasswordWidth             = 30
	PasswordMinLength         = 8
	DefaultView               = "menu"
	SplashView                = "splash"
	CreateWalletNameView      = "create_wallet_name"
	CreateWalletView          = "create_wallet_password"
	ImportWalletView          = "import_wallet"
	ImportWalletPasswordView  = "import_wallet_password"
	ImportMethodSelectionView = "import_method_selection"
	ImportPrivateKeyView      = "import_private_key"
	ImportKeystoreView        = "import_keystore"
	EnhancedImportView        = "enhanced_import"
	ListWalletsView           = "list_wallets"
	WalletPasswordView        = "wallet_password"
	WalletDetailsView         = "wallet_details"
	ConfigurationView         = "configuration"
	NetworkMenuView           = "network_menu"
	LanguageSelectionView     = "language_selection"
	NetworkListView           = "network_list"
	AddNetworkView            = "add_network"
	StyleWidth                = 40
	StyleMargin               = 1
	SplashDuration            = 2 * time.Second
	ErrorFontNotFoundMessage  = "Fonte não encontrada nos diretórios especificados."
	MnemonicWordCount         = 12
)
