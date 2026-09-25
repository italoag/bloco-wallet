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
	SourcePath     string
	PreflightErr   error
}

type KeystoreBatchProgress struct {
	Total           int
	Completed       int
	Imported        int
	AlreadyImported int
	Failed          int
}

type KeystoreBatchImportRequest struct {
	Items                  []KeystoreBatchItem
	StoragePassword        []byte
	ConfirmStoragePassword []byte
	MaxConcurrency         int
	OnProgress             func(KeystoreBatchProgress)
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
	var progressMu sync.Mutex
	progress := KeystoreBatchProgress{Total: len(request.Items)}
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
					summary, err := vault.ImportKeystore(ctx, KeystoreImportRequest{
						Name:                   item.Name,
						KeystoreJSON:           item.KeystoreJSON,
						SourcePassword:         item.SourcePassword,
						StoragePassword:        request.StoragePassword,
						ConfirmStoragePassword: request.ConfirmStoragePassword,
					})
					if errors.Is(err, ErrAccountConflict) {
						results[index].AlreadyImported = true
						return
					}
					if err != nil {
						results[index].Err = fmt.Errorf("batch item %d: %w", index, err)
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
