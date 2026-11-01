package bootstrap

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestWorker_StopCancelsRun(t *testing.T) {
	stopped := make(chan struct{})
	w := newWorker("loop", func(ctx context.Context) error {
		<-ctx.Done()
		close(stopped)
		return nil
	})

	assert.Equal(t, "loop", w.Name())
	done := make(chan error, 1)
	go func() { done <- w.Start(context.Background()) }()

	assert.NoError(t, w.Stop(context.Background()))
	assert.NoError(t, <-done)
	<-stopped
}

func TestWorker_StopBeforeStartReturnsImmediately(t *testing.T) {
	w := newWorker("loop", func(ctx context.Context) error { <-ctx.Done(); return nil })
	assert.NoError(t, w.Stop(context.Background()))
	assert.NoError(t, w.Start(context.Background()))
}
