package worker

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// Pool manages background workers.
type Pool struct {
	wg     sync.WaitGroup
	ctx    context.Context
	cancel context.CancelFunc
	logger *slog.Logger
}

// NewPool creates a worker pool.
func NewPool(logger *slog.Logger) *Pool {
	ctx, cancel := context.WithCancel(context.Background())
	return &Pool{
		ctx:    ctx,
		cancel: cancel,
		logger: logger,
	}
}

// Run starts a named worker that runs fn at the given interval.
func (p *Pool) Run(name string, interval time.Duration, fn func(ctx context.Context) error) {
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()

		p.logger.Info("worker started", "name", name, "interval", interval)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-p.ctx.Done():
				p.logger.Info("worker stopped", "name", name)
				return
			case <-ticker.C:
				if err := fn(p.ctx); err != nil {
					p.logger.Error("worker error", "name", name, "error", err)
				}
			}
		}
	}()
}

// Stop cancels all workers and waits for them to finish.
func (p *Pool) Stop() {
	p.cancel()
	p.wg.Wait()
}
