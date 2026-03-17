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

// gcGracePeriod is the minimum age before GC considers deleting a session.
// Prevents race: Create → Sync (before tmux window is fully ready) → Orphan → Delete.
const gcGracePeriod = 10 * time.Second

func (gc *GC) collect(ctx context.Context) {
	if err := gc.mgr.Sync(ctx); err != nil {
		gc.mgr.log.Debug("gc.sync.error", "err", err)
		return
	}

	now := time.Now()
	sessions := gc.mgr.Sessions()
	for _, s := range sessions {
		if s.Status == StatusDead || s.Status == StatusOrphan {
			if now.Sub(s.CreatedAt) < gcGracePeriod {
				gc.mgr.log.Debug("gc.skip.grace", "name", s.Name, "age", now.Sub(s.CreatedAt))
				continue
			}
			gc.mgr.log.Info("gc.delete", "name", s.Name, "id", s.ID[:8], "status", s.Status)
			gc.mgr.Delete(ctx, s.ID)
		}
	}
}
