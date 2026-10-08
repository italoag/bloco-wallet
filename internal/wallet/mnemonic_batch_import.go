package wallet

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

const MaxMnemonicImportBytes = 16 << 10

var errInvalidMnemonicFile = errors.New("mnemonic file data is invalid")

type MnemonicBatchItem struct {
	Name         string
	SourcePath   string
	Mnemonic     []byte
	PreflightErr error
}

type MnemonicBatchImportRequest struct {
	Items                  []MnemonicBatchItem
	StoragePassword        []byte
	ConfirmStoragePassword []byte
	MaxConcurrency         int
	OnProgress             func(BatchImportProgress)
}

func mnemonicFileText(data []byte) (string, error) {
	if len(data) == 0 || len(data) > MaxMnemonicImportBytes {
		return "", errInvalidMnemonicFile
	}
	trimmed := bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})
	if !utf8.Valid(trimmed) {
		return "", errInvalidMnemonicFile
	}
	return strings.Join(strings.Fields(string(trimmed)), " "), nil
}

func PreviewMnemonicFileImport(data []byte) (ImportPreview, error) {
	text, err := mnemonicFileText(data)
	if err != nil {
		return ImportPreview{}, err
	}
	return PreviewMnemonicImport(MnemonicImportRequest{Mnemonic: text})
}

func (vault *WalletVault) ImportMnemonicBatch(ctx context.Context, request MnemonicBatchImportRequest) []BatchImportResult {
	if len(request.Items) > maxCanonicalBatchItems {
		return []BatchImportResult{{Index: -1, Err: fmt.Errorf("batch exceeds %d items", maxCanonicalBatchItems)}}
	}
	items := make([]canonicalBatchWorkItem, len(request.Items))
	for index := range request.Items {
		item := request.Items[index]
		preflightErr := item.PreflightErr
		if preflightErr == nil {
			if _, err := mnemonicFileText(item.Mnemonic); err != nil {
				preflightErr = err
			}
		}
		items[index] = canonicalBatchWorkItem{
			size:         len(item.Mnemonic),
			preflightErr: preflightErr,
			importAccount: func(ctx context.Context) (AccountSummary, error) {
				if err := ctx.Err(); err != nil {
					return AccountSummary{}, err
				}
				text, err := mnemonicFileText(item.Mnemonic)
				if err != nil {
					return AccountSummary{}, err
				}
				return vault.ImportMnemonic(ctx, MnemonicImportRequest{
					Name:                   item.Name,
					Mnemonic:               text,
					StoragePassword:        request.StoragePassword,
					ConfirmStoragePassword: request.ConfirmStoragePassword,
				})
			},
		}
	}
	return runCanonicalBatch(ctx, items, request.StoragePassword, request.ConfirmStoragePassword, request.MaxConcurrency, request.OnProgress)
}
