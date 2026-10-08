package wallet

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"sync"
	"time"
)

// SecretBatchKind identifies which canonical secret type a batch import loads.
type SecretBatchKind string

const (
	SecretBatchMnemonic   SecretBatchKind = "mnemonic"
	SecretBatchPrivateKey SecretBatchKind = "private_key"
)

// SecretBatchItem carries one secret candidate discovered by the caller (the
// TUI scans a directory). SecretData holds the raw mnemonic text or hex
// private key; Passphrase holds an optional per-item BIP39 passphrase (for
// example from a .pwd sidecar file) and only applies to mnemonic batches.
type SecretBatchItem struct {
	Name         string
	SecretData   []byte
	Passphrase   []byte
	SourcePath   string
	PreflightErr error
}

type SecretBatchProgress struct {
	Total           int
	Completed       int
	Imported        int
	AlreadyImported int
	Failed          int
}

type SecretBatchResult struct {
	Index           int
	Summary         *AccountSummary
	AlreadyImported bool
	Err             error
}

type SecretBatchImportRequest struct {
	Kind                   SecretBatchKind
	Items                  []SecretBatchItem
	BIP39Passphrase        string        // optional global BIP39 passphrase (mnemonic kind only)
	BIP39Language          BIP39Language // blank = auto-detect per item (mnemonic kind only)
	DerivationPath         string        // blank = m/44'/60'/0'/0/0 (mnemonic kind only)
	StoragePassword        []byte
	ConfirmStoragePassword []byte
	MaxConcurrency         int
	OnProgress             func(SecretBatchProgress)
}

// ImportSecretBatch imports a batch of canonical secrets (mnemonic phrases or
// private keys), applying the same limits, timeout, storage password checks
// and bounded-concurrency worker pool as ImportKeystoreBatch. Each item is
// imported independently; per-item failures never abort the batch.
func (vault *WalletVault) ImportSecretBatch(ctx context.Context, request SecretBatchImportRequest) []SecretBatchResult {
	if len(request.Items) == 0 {
		return nil
	}
	if len(request.Items) > maxCanonicalBatchItems {
		return []SecretBatchResult{{Index: -1, Err: fmt.Errorf("batch exceeds %d items", maxCanonicalBatchItems)}}
	}
	totalBytes := 0
	for _, item := range request.Items {
		if len(item.SecretData) > maxCanonicalBatchBytes-totalBytes {
			return []SecretBatchResult{{Index: -1, Err: fmt.Errorf("batch exceeds %d bytes", maxCanonicalBatchBytes)}}
		}
		totalBytes += len(item.SecretData)
	}
	if deadline, hasDeadline := ctx.Deadline(); !hasDeadline || time.Until(deadline) > canonicalBatchTimeout {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, canonicalBatchTimeout)
		defer cancel()
	}
	results := make([]SecretBatchResult, len(request.Items))
	for index := range results {
		results[index].Index = index
	}
	var progressMu sync.Mutex
	progress := SecretBatchProgress{Total: len(request.Items)}
	report := func(index int) {
		if request.OnProgress == nil {
			return
		}
		progressMu.Lock()
		defer progressMu.Unlock()
		progress.Completed++
		switch {
		case results[index].Err != nil:
			progress.Failed++
		case results[index].AlreadyImported:
			progress.AlreadyImported++
		default:
			progress.Imported++
		}
		request.OnProgress(progress)
	}
	if request.OnProgress != nil {
		progressMu.Lock()
		request.OnProgress(progress)
		progressMu.Unlock()
	}
	var validationErr error
	if request.Kind != SecretBatchMnemonic && request.Kind != SecretBatchPrivateKey {
		validationErr = fmt.Errorf("unsupported secret batch kind")
	} else if len(request.StoragePassword) != len(request.ConfirmStoragePassword) || subtle.ConstantTimeCompare(request.StoragePassword, request.ConfirmStoragePassword) != 1 {
		validationErr = ErrStoragePasswordConfirmation
	} else if err := validateNewStoragePassword(request.StoragePassword); err != nil {
		validationErr = err
	} else if err := ctx.Err(); err != nil {
		validationErr = err
	}
	if validationErr != nil {
		for index := range results {
			results[index].Err = validationErr
			report(index)
		}
		return results
	}
	workers := request.MaxConcurrency
	if workers <= 0 {
		workers = 2
	}
	if workers > 8 {
		workers = 8
	}
	if workers > len(request.Items) {
		workers = len(request.Items)
	}
	jobs := make(chan int)
	var waitGroup sync.WaitGroup
	waitGroup.Add(workers)
	for range workers {
		go func() {
			defer waitGroup.Done()
			for index := range jobs {
				func() {
					defer report(index)
					if request.Items[index].PreflightErr != nil {
						results[index].Err = request.Items[index].PreflightErr
						return
					}
					if err := ctx.Err(); err != nil {
						results[index].Err = err
						return
					}
					item := request.Items[index]
					var summary AccountSummary
					var itemErr error
					switch request.Kind {
					case SecretBatchMnemonic:
						passphrase := request.BIP39Passphrase
						if len(item.Passphrase) > 0 {
							passphrase = string(item.Passphrase)
						}
						summary, itemErr = vault.ImportMnemonic(ctx, MnemonicImportRequest{
							Name:                   item.Name,
							Mnemonic:               string(item.SecretData),
							BIP39Passphrase:        passphrase,
							BIP39Language:          request.BIP39Language,
							DerivationPath:         request.DerivationPath,
							StoragePassword:        request.StoragePassword,
							ConfirmStoragePassword: request.ConfirmStoragePassword,
						})
					case SecretBatchPrivateKey:
						summary, itemErr = vault.ImportPrivateKey(ctx, PrivateKeyImportRequest{
							Name:                   item.Name,
							PrivateKey:             string(item.SecretData),
							StoragePassword:        request.StoragePassword,
							ConfirmStoragePassword: request.ConfirmStoragePassword,
						})
					}
					if errors.Is(itemErr, ErrAccountConflict) {
						results[index].AlreadyImported = true
						return
					}
					if itemErr != nil {
						results[index].Err = fmt.Errorf("batch item %d: %w", index, itemErr)
						return
					}
					results[index].Summary = &summary
				}()
			}
		}()
	}
	for index := range request.Items {
		select {
		case jobs <- index:
		case <-ctx.Done():
			results[index].Err = ctx.Err()
			report(index)
		}
	}
	close(jobs)
	waitGroup.Wait()
	return results
}
