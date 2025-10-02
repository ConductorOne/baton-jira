package cli

import (
	"context"
	"sync"

	"github.com/conductorone/baton-sdk/pkg/types"
)

var _ types.SessionStore = (*LazySessionStore)(nil)

// LazySessionStore implements types.SessionStore interface but only creates the actual session
// when a method is called for the first time.
type LazySessionStore struct {
	constructor types.SessionConstructor
	once        sync.Once
	session     types.SessionStore
	err         error
}

// ensureSession creates the actual session store if it hasn't been created yet.
func (l *LazySessionStore) ensureSession(ctx context.Context) error {
	l.once.Do(func() {
		l.session, l.err = l.constructor(ctx)
	})
	return l.err
}

// Get implements types.SessionStore.
func (l *LazySessionStore) Get(ctx context.Context, key string, opt ...types.SessionOption) ([]byte, bool, error) {
	if err := l.ensureSession(ctx); err != nil {
		return nil, false, err
	}
	return l.session.Get(ctx, key, opt...)
}

// GetMany implements types.SessionStore.
func (l *LazySessionStore) GetMany(ctx context.Context, keys []string, opt ...types.SessionOption) (map[string][]byte, error) {
	if err := l.ensureSession(ctx); err != nil {
		return nil, err
	}
	return l.session.GetMany(ctx, keys, opt...)
}

// Set implements types.SessionStore.
func (l *LazySessionStore) Set(ctx context.Context, key string, value []byte, opt ...types.SessionOption) error {
	if err := l.ensureSession(ctx); err != nil {
		return err
	}
	return l.session.Set(ctx, key, value, opt...)
}

// SetMany implements types.SessionStore.
func (l *LazySessionStore) SetMany(ctx context.Context, values map[string][]byte, opt ...types.SessionOption) error {
	if err := l.ensureSession(ctx); err != nil {
		return err
	}
	return l.session.SetMany(ctx, values, opt...)
}

// Delete implements types.SessionStore.
func (l *LazySessionStore) Delete(ctx context.Context, key string, opt ...types.SessionOption) error {
	if err := l.ensureSession(ctx); err != nil {
		return err
	}
	return l.session.Delete(ctx, key, opt...)
}

// Clear implements types.SessionStore.
func (l *LazySessionStore) Clear(ctx context.Context, opt ...types.SessionOption) error {
	if err := l.ensureSession(ctx); err != nil {
		return err
	}
	return l.session.Clear(ctx, opt...)
}

// GetAll implements types.SessionStore.
func (l *LazySessionStore) GetAll(ctx context.Context, opt ...types.SessionOption) (map[string][]byte, error) {
	if err := l.ensureSession(ctx); err != nil {
		return nil, err
	}
	return l.session.GetAll(ctx, opt...)
}

// CloseStore implements types.SessionStore.
func (l *LazySessionStore) CloseStore(ctx context.Context) error {
	if err := l.ensureSession(ctx); err != nil {
		return err
	}
	return l.session.CloseStore(ctx)
}
