package wallet

import (
	"context"
	"sync"
)

type importControlContextKey struct{}

type ImportControl struct {
	mu     sync.Mutex
	ctx    context.Context
	cancel context.CancelFunc
}

func NewImportControl(parent context.Context) *ImportControl {
	if parent == nil {
		parent = context.Background()
	}
	base, cancel := context.WithCancel(parent)
	control := &ImportControl{cancel: cancel}
	control.ctx = context.WithValue(base, importControlContextKey{}, control)
	return control
}

func (c *ImportControl) Context() context.Context {
	return c.ctx
}

func (c *ImportControl) Cancel() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cancel()
}

func (c *ImportControl) commit(operation func() error) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.ctx.Err(); err != nil {
		return err
	}
	return operation()
}

func importControlFromContext(ctx context.Context) *ImportControl {
	if ctx == nil {
		return nil
	}
	control, _ := ctx.Value(importControlContextKey{}).(*ImportControl)
	return control
}
