package wallet

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testBatchMnemonicJunk    = "test test test test test test test test test test test junk"
	testBatchMnemonicAbandon = "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"
)

func TestImportMnemonicBatchProgressAndResults(t *testing.T) {
	vault, repository, _ := newTestVault(t)
	items := []MnemonicBatchItem{
		{Name: "one", Mnemonic: []byte(testBatchMnemonicJunk)},
		{Name: "two", Mnemonic: []byte(testBatchMnemonicAbandon)},
		{Name: "dup", Mnemonic: []byte(testBatchMnemonicJunk)},
		{Name: "bad", PreflightErr: context.DeadlineExceeded},
	}
	var mu sync.Mutex
	var reports []BatchImportProgress
	results := vault.ImportMnemonicBatch(context.Background(), MnemonicBatchImportRequest{
		Items:                  items,
		StoragePassword:        []byte("Strong vault pass 1!"),
		ConfirmStoragePassword: []byte("Strong vault pass 1!"),
		MaxConcurrency:         1,
		OnProgress: func(p BatchImportProgress) {
			mu.Lock()
			reports = append(reports, p)
			mu.Unlock()
		},
	})
	require.Len(t, results, 4)
	require.Len(t, reports, 5)
	assert.Equal(t, BatchImportProgress{Total: 4}, reports[0])
	last := reports[len(reports)-1]
	assert.Equal(t, 4, last.Completed)
	assert.Equal(t, 2, last.Imported)
	assert.Equal(t, 1, last.AlreadyImported)
	assert.Equal(t, 1, last.Failed)
	assert.Equal(t, last.Imported+last.AlreadyImported+last.Failed, last.Completed)
	for index, result := range results {
		assert.Equal(t, index, result.Index)
	}
	require.NotNil(t, results[0].Summary)
	assert.Equal(t, "0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266", results[0].Summary.Address)
	assert.True(t, results[2].AlreadyImported)
	assert.ErrorIs(t, results[3].Err, context.DeadlineExceeded)

	accounts, err := repository.ListAccounts(context.Background())
	require.NoError(t, err)
	assert.Len(t, accounts, 2)
}

func TestImportMnemonicBatchRejectsInvalidData(t *testing.T) {
	vault, _, _ := newTestVault(t)
	results := vault.ImportMnemonicBatch(context.Background(), MnemonicBatchImportRequest{
		Items: []MnemonicBatchItem{
			{Name: "utf8", Mnemonic: []byte{0xff, 0xfe}},
			{Name: "ok", Mnemonic: []byte(testBatchMnemonicJunk)},
		},
		StoragePassword:        []byte("Strong vault pass 1!"),
		ConfirmStoragePassword: []byte("Strong vault pass 1!"),
	})
	require.Len(t, results, 2)
	assert.ErrorIs(t, results[0].Err, errInvalidMnemonicFile)
	assert.NotContains(t, results[0].Err.Error(), "\xff")
	require.NotNil(t, results[1].Summary)
}

func TestImportMnemonicBatchCancelFromProgress(t *testing.T) {
	vault, repository, _ := newTestVault(t)
	ctx, cancel := context.WithCancel(context.Background())
	items := []MnemonicBatchItem{
		{Name: "one", Mnemonic: []byte(testBatchMnemonicJunk)},
		{Name: "two", Mnemonic: []byte(testBatchMnemonicAbandon)},
		{Name: "three", Mnemonic: []byte(testBatchMnemonicAbandon)},
		{Name: "four", Mnemonic: []byte(testBatchMnemonicJunk)},
	}
	var reports []BatchImportProgress
	results := vault.ImportMnemonicBatch(ctx, MnemonicBatchImportRequest{
		Items:                  items,
		StoragePassword:        []byte("Strong vault pass 1!"),
		ConfirmStoragePassword: []byte("Strong vault pass 1!"),
		MaxConcurrency:         1,
		OnProgress: func(p BatchImportProgress) {
			reports = append(reports, p)
			cancel()
		},
	})
	require.Len(t, results, 4)
	last := reports[len(reports)-1]
	assert.Equal(t, 4, last.Completed)
	assert.Equal(t, last.Imported+last.AlreadyImported+last.Failed, last.Completed)
	cancelled := 0
	for _, result := range results {
		if result.Err != nil {
			assert.ErrorIs(t, result.Err, context.Canceled)
			cancelled++
		}
	}
	assert.Positive(t, cancelled)
	accounts, err := repository.ListAccounts(context.Background())
	require.NoError(t, err)
	assert.LessOrEqual(t, len(accounts), 1)
}
