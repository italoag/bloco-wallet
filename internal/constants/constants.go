package constants

import "time"

const (
	PasswordCharLimit         = 1024
	PasswordWidth             = 30
	PasswordMinLength         = 15
	DefaultView               = "menu"
	SplashView                = "splash"
	CreateWalletNameView      = "create_wallet_name"
	CreateWalletBackupView    = "create_wallet_backup"
	CreateWalletOptionsView   = "create_wallet_options"
	CreateWalletView          = "create_wallet_password"
	ImportWalletView          = "import_wallet"
	ImportWalletPasswordView  = "import_wallet_password"
	ImportMethodSelectionView = "import_method_selection"
	ImportPrivateKeyView      = "import_private_key"
	ImportKeystoreView        = "import_keystore"
	EnhancedImportView        = "enhanced_import"
	CanonicalImportView       = "canonical_import"
	ListWalletsView           = "list_wallets"
	WalletPasswordView        = "wallet_password"
	WalletDetailsView         = "wallet_details"
	AccountHistoryView        = "account_history"
	PersonalSignView          = "personal_sign"
	EIP712SignView            = "eip712_sign"
	ContractCallView          = "contract_call"
	WalletConnectView         = "walletconnect"
	FIDO2View                 = "fido2"
	NativeTransferView        = "native_transfer"
	RotatePasswordView        = "rotate_password"
	ExportAccountView         = "export_account"
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
