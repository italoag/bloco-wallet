package wallet

type WalletRepository interface {
	AddWallet(wallet *Wallet) error
	GetAllWallets() ([]Wallet, error)
	DeleteWallet(walletID int) error
	FindBySourceHash(sourceHash string) (*Wallet, error)
	Close() error
}
