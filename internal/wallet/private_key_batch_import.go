package wallet

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
)

const MaxPrivateKeyImportBytes = 4 << 10

var errInvalidPrivateKeyFile = errors.New("private key file data is invalid")

type PrivateKeyBatchItem struct {
	Name         string
	SourcePath   string
	PrivateKey   []byte
	PreflightErr error
}

type PrivateKeyBatchImportRequest struct {
	Items                  []PrivateKeyBatchItem
	StoragePassword        []byte
	ConfirmStoragePassword []byte
	MaxConcurrency         int
	OnProgress             func(BatchImportProgress)
}

func privateKeyFileText(data []byte) (string, error) {
	if len(data) == 0 || len(data) > MaxPrivateKeyImportBytes {
		return "", errInvalidPrivateKeyFile
	}
	trimmed := strings.TrimSpace(string(bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})))
	if trimmed == "" {
		return "", errInvalidPrivateKeyFile
	}
	return trimmed, nil
}

func PreviewPrivateKeyFileImport(data []byte) (ImportPreview, error) {
	text, err := privateKeyFileText(data)
	if err != nil {
		return ImportPreview{}, err
	}
	return PreviewPrivateKeyImport(PrivateKeyImportRequest{PrivateKey: text})
}

func (vault *WalletVault) ImportPrivateKeyBatch(ctx context.Context, request PrivateKeyBatchImportRequest) []BatchImportResult {
	if len(request.Items) > maxCanonicalBatchItems {
		return []BatchImportResult{{Index: -1, Err: fmt.Errorf("batch exceeds %d items", maxCanonicalBatchItems)}}
	}
	items := make([]canonicalBatchWorkItem, len(request.Items))
	for index := range request.Items {
		item := request.Items[index]
		preflightErr := item.PreflightErr
		if preflightErr == nil {
			if _, err := privateKeyFileText(item.PrivateKey); err != nil {
				preflightErr = err
			}
		}
		items[index] = canonicalBatchWorkItem{
			size:         len(item.PrivateKey),
			preflightErr: preflightErr,
			importAccount: func(ctx context.Context) (AccountSummary, error) {
				if err := ctx.Err(); err != nil {
					return AccountSummary{}, err
				}
				text, err := privateKeyFileText(item.PrivateKey)
				if err != nil {
					return AccountSummary{}, err
				}
				return vault.ImportPrivateKey(ctx, PrivateKeyImportRequest{
					Name:                   item.Name,
					PrivateKey:             text,
					StoragePassword:        request.StoragePassword,
					ConfirmStoragePassword: request.ConfirmStoragePassword,
				})
			},
		}
	}
	return runCanonicalBatch(ctx, items, request.StoragePassword, request.ConfirmStoragePassword, request.MaxConcurrency, request.OnProgress)
}
