package plaid

import (
	"context"
	"log/slog"
	"time"
)

// minSyncGap skips items the webhook path refreshed recently; the worker is a
// fallback and reconciliation pass, not the primary trigger.
const minSyncGap = 30 * time.Minute

// RunWorker polls every active item on the configured interval until ctx is
// cancelled. It is the fallback for missed webhooks (and the only trigger in
// environments without a public URL). Items are synced sequentially — volumes
// are small, and the per-item advisory lock already guards against overlap
// with webhook-triggered syncs.
func (s *Service) RunWorker(ctx context.Context, interval time.Duration, entitled func(context.Context, int64) bool) {
	if !s.configured || interval <= 0 {
		return
	}
	slog.Info("plaid: polling worker started", "interval", interval)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			slog.Info("plaid: polling worker stopped")
			return
		case <-ticker.C:
			s.pollOnce(ctx, entitled)
		}
	}
}

func (s *Service) pollOnce(ctx context.Context, entitled func(context.Context, int64) bool) {
	items, err := s.store.ListActivePlaidItems(ctx, time.Now().Add(-minSyncGap))
	if err != nil {
		slog.Warn("plaid: worker list items failed", "err", err)
		return
	}
	for _, item := range items {
		if ctx.Err() != nil {
			return
		}
		if entitled != nil && !entitled(ctx, item.UserID) {
			continue
		}
		itemCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		if err := s.SyncItem(itemCtx, item); err != nil {
			logSyncWarn("worker sync failed", item.ID, errorCode(err))
		}
		cancel()
	}
}
