package wallet

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"sync"
	"time"
)

const (
	maxCanonicalBatchItems = 100
	maxCanonicalBatchBytes = 100 << 20
	canonicalBatchTimeout  = 5 * time.Minute
)

type KeystoreBatchItem struct {
	Name           string
	KeystoreJSON   []byte
	SourcePassword []byte
	PreflightErr   error
}

type KeystoreBatchImportRequest struct {
	Items                  []KeystoreBatchItem
	StoragePassword        []byte
	ConfirmStoragePassword []byte
	MaxConcurrency         int
}

type KeystoreBatchResult struct {
	Index           int
	Summary         *AccountSummary
	AlreadyImported bool
	Err             error
}

func (vault *WalletVault) ImportKeystoreBatch(ctx context.Context, request KeystoreBatchImportRequest) []KeystoreBatchResult {
	if len(request.Items) == 0 {
		return nil
	}
	if len(request.Items) > maxCanonicalBatchItems {
		return []KeystoreBatchResult{{Index: -1, Err: fmt.Errorf("batch exceeds %d items", maxCanonicalBatchItems)}}
	}
	totalBytes := 0
	for _, item := range request.Items {
		if len(item.KeystoreJSON) > maxCanonicalBatchBytes-totalBytes {
			return []KeystoreBatchResult{{Index: -1, Err: fmt.Errorf("batch exceeds %d bytes", maxCanonicalBatchBytes)}}
		}
		totalBytes += len(item.KeystoreJSON)
	}
	if deadline, hasDeadline := ctx.Deadline(); !hasDeadline || time.Until(deadline) > canonicalBatchTimeout {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, canonicalBatchTimeout)
		defer cancel()
	}
	results := make([]KeystoreBatchResult, len(request.Items))
	for index := range results {
		results[index].Index = index
	}
	var validationErr error
	if len(request.StoragePassword) != len(request.ConfirmStoragePassword) || subtle.ConstantTimeCompare(request.StoragePassword, request.ConfirmStoragePassword) != 1 {
		validationErr = ErrStoragePasswordConfirmation
	} else if err := validateNewStoragePassword(request.StoragePassword); err != nil {
		validationErr = err
	} else if err := ctx.Err(); err != nil {
		validationErr = err
	}
	if validationErr != nil {
		for index := range results {
			results[index].Err = validationErr
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
				if request.Items[index].PreflightErr != nil {
					results[index].Err = request.Items[index].PreflightErr
					continue
				}
				if err := ctx.Err(); err != nil {
					results[index].Err = err
					continue
				}
				item := request.Items[index]
				summary, err := vault.ImportKeystore(ctx, KeystoreImportRequest{
					Name:                   item.Name,
					KeystoreJSON:           item.KeystoreJSON,
					SourcePassword:         item.SourcePassword,
					StoragePassword:        request.StoragePassword,
					ConfirmStoragePassword: request.ConfirmStoragePassword,
				})
				if errors.Is(err, ErrAccountConflict) {
					results[index].AlreadyImported = true
					continue
				}
				if err != nil {
					results[index].Err = fmt.Errorf("batch item %d: %w", index, err)
					continue
				}
				results[index].Summary = &summary
			}
		}()
	}
	for index := range request.Items {
		select {
		case jobs <- index:
		case <-ctx.Done():
			results[index].Err = ctx.Err()
		}
	}
	close(jobs)
	waitGroup.Wait()
	return results
}
