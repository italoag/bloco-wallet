package wallet

type WalletRepository interface {
	AddWallet(wallet *Wallet) error
	GetAllWallets() ([]Wallet, error)
	DeleteWallet(walletID int) error
	FindBySourceHash(sourceHash string) (*Wallet, error)
	FindByAddress(address string) ([]Wallet, error)
	FindByAddressAndMethod(address, importMethod string) ([]Wallet, error)
	Close() error
}
