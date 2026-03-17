package session

import (
	"context"
	"sync"
	"time"
)

// GC periodically syncs with tmux and removes dead sessions.
type GC struct {
	mgr      *Manager
	interval time.Duration
	cancel   context.CancelFunc
	wg       sync.WaitGroup
}

// NewGC creates a garbage collector that runs at the given interval.
func NewGC(mgr *Manager, interval time.Duration) *GC {
	return &GC{
		mgr:      mgr,
		interval: interval,
	}
}

// Start begins the background sync loop.
func (gc *GC) Start() {
	ctx, cancel := context.WithCancel(context.Background())
	gc.cancel = cancel

	gc.wg.Add(1)
	go func() {
		defer gc.wg.Done()
		ticker := time.NewTicker(gc.interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				gc.collect(ctx)
			}
		}
	}()
}

// Stop halts the background loop and waits for it to finish.
func (gc *GC) Stop() {
	if gc.cancel != nil {
		gc.cancel()
	}
	gc.wg.Wait()
}

func (gc *GC) collect(ctx context.Context) {
	if err := gc.mgr.Sync(ctx); err != nil {
		gc.mgr.log.Debug("gc.sync.error", "err", err)
		return
	}

	sessions := gc.mgr.Sessions()
	for _, s := range sessions {
		if s.Status == StatusDead || s.Status == StatusOrphan {
			gc.mgr.log.Info("gc.delete", "name", s.Name, "id", s.ID[:8], "status", s.Status)
			gc.mgr.Delete(ctx, s.ID)
		}
	}
}
