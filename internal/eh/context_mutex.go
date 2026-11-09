package eh

import (
	"context"
)

// contextMutex is a single-owner lock whose waiters can leave when their
// request is canceled. A mutex only provides an uninterruptible wait, which
// can keep abandoned homepage preview workers alive behind older requests.
type contextMutex struct {
	token chan struct{}
}

func newContextMutex() *contextMutex {
	m := &contextMutex{token: make(chan struct{}, 1)}
	m.token <- struct{}{}
	return m
}

func (m *contextMutex) Lock(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-m.token:
		if err := ctx.Err(); err != nil {
			m.token <- struct{}{}
			return err
		}
		return nil
	}
}

func (m *contextMutex) Unlock() { m.token <- struct{}{} }
