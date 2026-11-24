package store

import (
	"context"
	"testing"
)

type blockingAuthStore struct {
	Store
	started chan struct{}
	proceed chan struct{}
	result  AuthResult
}

func (b *blockingAuthStore) AuthenticateWithClient(ctx context.Context, token string) (AuthResult, error) {
	close(b.started)
	<-b.proceed
	return b.result, nil
}

func TestCacheClearAuthPreventsStaleInFlightPopulate(t *testing.T) {
	inner := &blockingAuthStore{
		started: make(chan struct{}),
		proceed: make(chan struct{}),
		result:  AuthResult{UserID: 1, ClientID: 2},
	}
	cs := WithCache(inner)

	done := make(chan struct{})
	go func() {
		_, _ = cs.AuthenticateWithClient(context.Background(), "tok-1")
		close(done)
	}()

	<-inner.started
	cs.ClearAuth()
	close(inner.proceed)
	<-done

	cs.mu.Lock()
	n := len(cs.auth)
	cs.mu.Unlock()
	if n != 0 {
		t.Fatalf("stale auth entry resurrected into cache after ClearAuth: len=%d", n)
	}
}
