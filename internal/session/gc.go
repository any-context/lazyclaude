package session

import (
	"context"
	"sync"
	"time"
)

// GC periodically syncs with tmux and removes dead sessions.
type GC struct {
	svc      Service
	interval time.Duration
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	log      func(msg string, args ...any) // optional debug logger
}

// NewGC creates a garbage collector that runs at the given interval.
func NewGC(svc Service, interval time.Duration) *GC {
	return &GC{
		svc:      svc,
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
	if err := gc.svc.Sync(ctx); err != nil {
		gc.debugLog("gc.sync.error", "err", err)
		return
	}

	now := time.Now()
	sessions := gc.svc.Sessions()
	for _, s := range sessions {
		// Only delete Dead sessions (pane has exited). Orphan means the tmux
		// session was temporarily unreachable (e.g. high load causing HasSession
		// to return false), NOT that the window is actually gone. Deleting Orphan
		// sessions was causing state.json wipeout during heavy go test runs.
		if s.Status == StatusDead {
			if now.Sub(s.CreatedAt) < gcGracePeriod {
				gc.debugLog("gc.skip.grace", "name", s.Name, "age", now.Sub(s.CreatedAt))
				continue
			}
			gc.debugLog("gc.delete", "name", s.Name, "id", s.ID[:8], "status", s.Status)
			gc.svc.Delete(ctx, s.ID)
		}
	}
}

func (gc *GC) debugLog(msg string, args ...any) {
	if gc.log != nil {
		gc.log(msg, args...)
	}
}
