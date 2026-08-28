package main

import (
	"log"
	"os"

	"blocowallet/internal/evm"
	"blocowallet/internal/signer"
	"blocowallet/internal/storage"
	"blocowallet/internal/wallet"
)

// configureExternalSigners wraps the software signer with a dispatcher and,
// when BLOCO_WALLET_CLOUD_SIGNER_URL is set, attaches a Vault-compatible
// cloud signer whose token comes from BLOCO_WALLET_CLOUD_SIGNER_TOKEN.
// Without configuration the software signer is returned unchanged.
func configureExternalSigners(softwareSigner *wallet.SoftwareSigner, repo *storage.GORMRepository) evm.ApprovedDigestSigner {
	endpoint := os.Getenv("BLOCO_WALLET_CLOUD_SIGNER_URL")
	if endpoint == "" {
		return softwareSigner
	}
	api, err := signer.NewVaultCompatibleAPI(endpoint, func() (string, error) {
		token := os.Getenv("BLOCO_WALLET_CLOUD_SIGNER_TOKEN")
		if token == "" {
			return "", nil
		}
		return token, nil
	})
	if err != nil {
		log.Printf("Cloud signer configuration rejected: %v", err)
		return softwareSigner
	}
	cloudSigner, err := signer.NewCloudSigner(api, repo, repo, repo)
	if err != nil {
		log.Printf("Cloud signer initialization failed: %v", err)
		return softwareSigner
	}
	dispatcher, err := signer.NewSignerDispatcher(softwareSigner, cloudSigner, repo)
	if err != nil {
		log.Printf("Signer dispatcher initialization failed: %v", err)
		return softwareSigner
	}
	log.Printf("Cloud signer attached; external accounts route through the dispatcher")
	return dispatcher
}
