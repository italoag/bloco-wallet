package main

import (
	"context"
	"log"
	"net"
	"net/url"
	"os"
	"time"

	"blocowallet/internal/blockchain"
	"blocowallet/internal/evm"
	"blocowallet/internal/signer"
	"blocowallet/internal/storage"
	"blocowallet/internal/wallet"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

type configuredSigningBackends struct {
	structured evm.StructuredSigner
	close      func()
}

func hardwareEmulatorLocalTargets() []string {
	var targets []string
	for _, name := range []string{"BLOCO_WALLET_LEDGER_SPECULOS_URL", "BLOCO_WALLET_TREZOR_BRIDGE_URL"} {
		parsed, err := url.ParseRequestURI(os.Getenv(name))
		if err != nil || parsed.Scheme != "http" || parsed.Host == "" {
			continue
		}
		host := parsed.Hostname()
		ip := net.ParseIP(host)
		if host == "localhost" || (ip != nil && ip.IsLoopback()) {
			targets = append(targets, parsed.Host)
		}
	}
	return targets
}

func configuredHardwarePath() string {
	if path := os.Getenv("BLOCO_WALLET_HARDWARE_DERIVATION_PATH"); path != "" {
		return path
	}
	return "m/44'/60'/0'/0/0"
}

func maybeImportDiscoveredHardware(ctx context.Context, repo *storage.GORMRepository, name, deviceKind, path string, publicKey []byte, address common.Address) error {
	if name == "" {
		return nil
	}
	parsedPath, err := wallet.ParseDerivationPath(path)
	if err != nil {
		return err
	}
	canonicalPath := parsedPath.String()
	reference := deviceKind + ":v1:" + canonicalPath
	existing, err := repo.FindAccountsByAddress(ctx, address.Hex())
	if err != nil {
		return err
	}
	for _, account := range existing {
		if account.SignerKind == wallet.SignerKindHardware && account.SignerReference == reference {
			return nil
		}
	}
	_, err = wallet.ImportHardwareSignerAccount(ctx, repo, wallet.HardwareSignerImportRequest{
		Name: name, DeviceKind: deviceKind, DerivationPath: canonicalPath,
		PublicKey: append([]byte(nil), publicKey...), ExpectedAddress: address,
	})
	return err
}

func configureExternalSigners(softwareSigner *wallet.SoftwareSigner, repo *storage.GORMRepository, gateway *blockchain.RPCGateway) (configuredSigningBackends, error) {
	softwareAdapter, err := evm.NewDigestSignerAdapter(softwareSigner)
	if err != nil {
		return configuredSigningBackends{}, err
	}
	var cloudAdapter evm.StructuredSigner
	endpoint := os.Getenv("BLOCO_WALLET_CLOUD_SIGNER_URL")
	if endpoint != "" {
		api, apiErr := signer.NewVaultCompatibleAPI(endpoint, func() (string, error) {
			token := os.Getenv("BLOCO_WALLET_CLOUD_SIGNER_TOKEN")
			if token == "" {
				return "", nil
			}
			return token, nil
		}, gateway)
		if apiErr != nil {
			log.Printf("Cloud signer configuration rejected: %v", apiErr)
		} else if cloudSigner, cloudErr := signer.NewCloudSigner(api, repo, repo, repo); cloudErr != nil {
			log.Printf("Cloud signer initialization failed: %v", cloudErr)
		} else {
			cloudAdapter, err = evm.NewDigestSignerAdapter(cloudSigner)
			if err != nil {
				return configuredSigningBackends{}, err
			}
			log.Printf("Cloud signer attached; external accounts route through structured approvals")
		}
	}

	var ledgerSigner *signer.LedgerSigner
	if speculosURL := os.Getenv("BLOCO_WALLET_LEDGER_SPECULOS_URL"); speculosURL != "" {
		transport, transportErr := signer.NewSpeculosTransport(speculosURL, gateway)
		if transportErr != nil {
			log.Printf("Ledger Speculos configuration rejected: %v", transportErr)
		} else if device, deviceErr := signer.NewLedgerDevice(transport); deviceErr != nil {
			log.Printf("Ledger device initialization failed: %v", deviceErr)
		} else {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			configuration, configurationErr := device.GetAppConfiguration(ctx)
			cancel()
			if configurationErr != nil || !configuration.Secure() {
				log.Printf("Ledger Ethereum app 1.22.3 or newer is required")
			} else {
				path := configuredHardwarePath()
				if importName := os.Getenv("BLOCO_WALLET_LEDGER_IMPORT_NAME"); importName != "" {
					ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
					publicKey, publicKeyErr := device.GetPublicKey(ctx, path)
					if publicKeyErr == nil {
						publicKeyErr = maybeImportDiscoveredHardware(ctx, repo, importName, "ledger", path, publicKey.PublicKey, publicKey.Address)
					}
					cancel()
					if publicKeyErr != nil {
						log.Printf("Ledger hardware account import failed: %v", publicKeyErr)
					}
				}
				if configured, signerErr := signer.NewLedgerSigner(device, repo, repo); signerErr != nil {
					log.Printf("Ledger signer initialization failed: %v", signerErr)
				} else {
					ledgerSigner = configured
					log.Printf("Ledger Speculos signer attached")
				}
			}
		}
	}

	var trezorSigner *signer.TrezorSigner
	var trezorDevice *signer.BridgeDevice
	if bridgeURL := os.Getenv("BLOCO_WALLET_TREZOR_BRIDGE_URL"); bridgeURL != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		device, deviceErr := signer.NewBridgeDevice(ctx, bridgeURL, gateway)
		cancel()
		if deviceErr != nil {
			log.Printf("Trezor Bridge unavailable: %v", deviceErr)
		} else {
			device.SetButtonHandler(func(context.Context) error { return nil })
			path := configuredHardwarePath()
			if importName := os.Getenv("BLOCO_WALLET_TREZOR_IMPORT_NAME"); importName != "" {
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				_, importErr := device.Initialize(ctx)
				var publicKey []byte
				if importErr == nil {
					publicKey, importErr = device.EthereumGetPublicKey(ctx, path)
				}
				var address common.Address
				if importErr == nil {
					parsedPublicKey, parseErr := crypto.DecompressPubkey(publicKey)
					if parseErr != nil {
						importErr = parseErr
					} else {
						address = crypto.PubkeyToAddress(*parsedPublicKey)
					}
				}
				if importErr == nil {
					importErr = maybeImportDiscoveredHardware(ctx, repo, importName, "trezor", path, publicKey, address)
				}
				cancel()
				if importErr != nil {
					log.Printf("Trezor hardware account import failed: %v", importErr)
				}
			}
			configured, signerErr := signer.NewTrezorSigner(device, repo, repo)
			if signerErr != nil {
				_ = device.Close()
				log.Printf("Trezor signer initialization failed: %v", signerErr)
			} else {
				trezorDevice = device
				trezorSigner = configured
				log.Printf("Trezor Bridge signer attached")
			}
		}
	}

	structured, err := signer.NewStructuredDispatcher(
		softwareAdapter, cloudAdapter, ledgerSigner, trezorSigner, repo, repo,
		signer.StructuredDispatcherOptions{AllowBlindHardwareEIP712: os.Getenv("BLOCO_WALLET_ALLOW_HARDWARE_BLIND_EIP712") == "1"},
	)
	if err != nil {
		if trezorDevice != nil {
			_ = trezorDevice.Close()
		}
		return configuredSigningBackends{}, err
	}
	return configuredSigningBackends{
		structured: structured,
		close: func() {
			if trezorDevice != nil {
				_ = trezorDevice.Close()
			}
		},
	}, nil
}
