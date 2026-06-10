package app

import (
	"context"
	"log/slog"
	"time"

	"github.com/fireman/fireman/internal/repository"
)

const (
	ticketRetention       = 24 * time.Hour
	ticketCleanupInterval = 1 * time.Hour
	ticketCleanupBatch    = 200
)

func runTicketCleanup(ctx context.Context, tickets *repository.ResolutionTicketRepo, logger *slog.Logger) {
	if tickets == nil {
		return
	}
	if logger == nil {
		logger = slog.Default()
	}
	cleanup := func() {
		before := time.Now().Add(-ticketRetention).UnixMilli()
		for {
			n, err := tickets.CleanupExpired(ctx, before, ticketCleanupBatch)
			if err != nil {
				logger.Error("resolution ticket cleanup failed", "error", err)
				return
			}
			if n < ticketCleanupBatch {
				if n > 0 {
					logger.Info("resolution tickets cleaned", "count", n)
				}
				return
			}
		}
	}
	cleanup()
	go func() {
		ticker := time.NewTicker(ticketCleanupInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				cleanup()
			}
		}
	}()
}
