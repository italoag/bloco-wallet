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

type BatchImportProgress struct {
	Total           int
	Completed       int
	Imported        int
	AlreadyImported int
	Failed          int
}

type KeystoreBatchProgress = BatchImportProgress

type KeystoreBatchImportRequest struct {
	Items                  []KeystoreBatchItem
	StoragePassword        []byte
	ConfirmStoragePassword []byte
	MaxConcurrency         int
	OnProgress             func(KeystoreBatchProgress)
}

type BatchImportResult struct {
	Index           int
	Summary         *AccountSummary
	AlreadyImported bool
	Err             error
}

type KeystoreBatchResult = BatchImportResult

type canonicalBatchWorkItem struct {
	size          int
	preflightErr  error
	importAccount func(context.Context) (AccountSummary, error)
}

func runCanonicalBatch(ctx context.Context, items []canonicalBatchWorkItem, storagePassword, confirmation []byte, maxConcurrency int, onProgress func(BatchImportProgress)) []BatchImportResult {
	if len(items) == 0 {
		return nil
	}
	if len(items) > maxCanonicalBatchItems {
		return []BatchImportResult{{Index: -1, Err: fmt.Errorf("batch exceeds %d items", maxCanonicalBatchItems)}}
	}
	totalBytes := 0
	for _, item := range items {
		if item.size > maxCanonicalBatchBytes-totalBytes {
			return []BatchImportResult{{Index: -1, Err: fmt.Errorf("batch exceeds %d bytes", maxCanonicalBatchBytes)}}
		}
		totalBytes += item.size
	}
	if deadline, hasDeadline := ctx.Deadline(); !hasDeadline || time.Until(deadline) > canonicalBatchTimeout {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, canonicalBatchTimeout)
		defer cancel()
	}
	results := make([]BatchImportResult, len(items))
	for index := range results {
		results[index].Index = index
	}
	var progressMu sync.Mutex
	progress := BatchImportProgress{Total: len(items)}
	report := func(index int) {
		if onProgress == nil {
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
		onProgress(progress)
	}
	if onProgress != nil {
		progressMu.Lock()
		onProgress(progress)
		progressMu.Unlock()
	}
	var validationErr error
	if len(storagePassword) != len(confirmation) || subtle.ConstantTimeCompare(storagePassword, confirmation) != 1 {
		validationErr = ErrStoragePasswordConfirmation
	} else if err := validateNewStoragePassword(storagePassword); err != nil {
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
	workers := maxConcurrency
	if workers <= 0 {
		workers = 2
	}
	if workers > 8 {
		workers = 8
	}
	if workers > len(items) {
		workers = len(items)
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
					if items[index].preflightErr != nil {
						results[index].Err = items[index].preflightErr
						return
					}
					if err := ctx.Err(); err != nil {
						results[index].Err = err
						return
					}
					summary, err := items[index].importAccount(ctx)
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
	for index := range items {
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

func (vault *WalletVault) ImportKeystoreBatch(ctx context.Context, request KeystoreBatchImportRequest) []KeystoreBatchResult {
	if len(request.Items) > maxCanonicalBatchItems {
		return []BatchImportResult{{Index: -1, Err: fmt.Errorf("batch exceeds %d items", maxCanonicalBatchItems)}}
	}
	items := make([]canonicalBatchWorkItem, len(request.Items))
	for index := range request.Items {
		item := request.Items[index]
		items[index] = canonicalBatchWorkItem{
			size:         len(item.KeystoreJSON),
			preflightErr: item.PreflightErr,
			importAccount: func(ctx context.Context) (AccountSummary, error) {
				return vault.ImportKeystore(ctx, KeystoreImportRequest{
					Name:                   item.Name,
					KeystoreJSON:           item.KeystoreJSON,
					SourcePassword:         item.SourcePassword,
					StoragePassword:        request.StoragePassword,
					ConfirmStoragePassword: request.ConfirmStoragePassword,
				})
			},
		}
	}
	return runCanonicalBatch(ctx, items, request.StoragePassword, request.ConfirmStoragePassword, request.MaxConcurrency, request.OnProgress)
}
