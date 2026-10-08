package wallet

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testBatchKeyOne = "0x59c6995e998f97a5a0044966f0945389dc9e86dae88c7a8416d9c4e1d9a03f19"
	testBatchKeyTwo = "0x8b3a350cf5c34c9194ca3a545d4f7ea3a1b0b2be2f0fbb5b5b8f7f8f8f8f8f8f"
)

func TestImportPrivateKeyBatchProgressAndResults(t *testing.T) {
	vault, repository, _ := newTestVault(t)
	items := []PrivateKeyBatchItem{
		{Name: "one", PrivateKey: []byte(testBatchKeyOne)},
		{Name: "two", PrivateKey: []byte(testBatchKeyTwo + "\n")},
		{Name: "dup", PrivateKey: []byte(testBatchKeyOne)},
		{Name: "bad", PreflightErr: context.DeadlineExceeded},
	}
	var mu sync.Mutex
	var reports []BatchImportProgress
	results := vault.ImportPrivateKeyBatch(context.Background(), PrivateKeyBatchImportRequest{
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
	for index, result := range results {
		assert.Equal(t, index, result.Index)
	}
	require.NotNil(t, results[0].Summary)
	assert.True(t, results[2].AlreadyImported)
	assert.ErrorIs(t, results[3].Err, context.DeadlineExceeded)

	accounts, err := repository.ListAccounts(context.Background())
	require.NoError(t, err)
	assert.Len(t, accounts, 2)
}

func TestImportPrivateKeyBatchRejectsInvalidData(t *testing.T) {
	vault, _, _ := newTestVault(t)
	results := vault.ImportPrivateKeyBatch(context.Background(), PrivateKeyBatchImportRequest{
		Items: []PrivateKeyBatchItem{
			{Name: "empty", PrivateKey: []byte("   \n")},
			{Name: "nothex", PrivateKey: []byte("0xzz")},
			{Name: "ok", PrivateKey: []byte(testBatchKeyOne)},
		},
		StoragePassword:        []byte("Strong vault pass 1!"),
		ConfirmStoragePassword: []byte("Strong vault pass 1!"),
	})
	require.Len(t, results, 3)
	assert.ErrorIs(t, results[0].Err, errInvalidPrivateKeyFile)
	assert.Error(t, results[1].Err)
	assert.NotContains(t, results[1].Err.Error(), "0xzz")
	require.NotNil(t, results[2].Summary)
}
