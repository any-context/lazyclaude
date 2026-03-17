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

const gcGracePeriod = 10 * time.Second

func (gc *GC) collect(ctx context.Context) {
	if err := gc.mgr.Sync(ctx); err != nil {
		return // tmux might not be running
	}

	now := time.Now()
	sessions := gc.mgr.Sessions()
	for _, s := range sessions {
		if s.Status == StatusDead || s.Status == StatusOrphan {
			// Don't delete sessions created recently — process may still be starting
			if now.Sub(s.CreatedAt) < gcGracePeriod {
				continue
			}
			gc.mgr.Delete(ctx, s.ID)
		}
	}
}
