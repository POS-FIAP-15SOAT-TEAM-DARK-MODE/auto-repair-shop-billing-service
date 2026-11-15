package bootstrap

import (
	"context"
)

// worker adapts a long-running loop to app.Server. Stop cancels the loop's context.
type worker struct {
	name   string
	run    func(ctx context.Context) error
	ctx    context.Context
	cancel context.CancelFunc
}

func newWorker(name string, run func(ctx context.Context) error) *worker {
	ctx, cancel := context.WithCancel(context.Background())
	return &worker{name: name, run: run, ctx: ctx, cancel: cancel}
}

func (w *worker) Start(_ context.Context) error { return w.run(w.ctx) }

func (w *worker) Stop(_ context.Context) error {
	w.cancel()
	return nil
}

func (w *worker) Name() string { return w.name }
