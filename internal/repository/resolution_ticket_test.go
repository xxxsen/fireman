package repository

import (
	"context"
	"testing"
	"time"

	"github.com/fireman/fireman/internal/testutil"
)

func TestResolutionTicketCleanupExpired(t *testing.T) {
	db := testutil.OpenTestDB(t)
	repo := NewResolutionTicketRepo(db)
	ctx := context.Background()
	now := time.Now()
	old := now.Add(-48 * time.Hour).UnixMilli()
	fresh := now.Add(10 * time.Minute).UnixMilli()

	for _, row := range []struct {
		id, consumed string
		expires      int64
	}{
		{"tkt_old_expired", "NULL", old},
		{"tkt_old_consumed", "?", old},
		{"tkt_fresh", "NULL", fresh},
	} {
		var consumed any
		if row.consumed == "?" {
			consumed = old
		}
		if _, err := db.ExecContext(
			ctx, `
			INSERT INTO resolution_tickets (
				id, market, instrument_type, code, provider_symbol, name,
				exchange, instrument_kind, created_at, expires_at, consumed_at
			) VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
			row.id, "CN", "cn_exchange_fund", "510300", "sh510300", "测试",
			"SH", "etf", old, row.expires, consumed,
		); err != nil {
			t.Fatal(err)
		}
	}

	cutoff := now.Add(-24 * time.Hour).UnixMilli()
	n, err := repo.CleanupExpired(ctx, cutoff, 50)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("deleted=%d want 2", n)
	}

	var remaining int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM resolution_tickets`).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != 1 {
		t.Fatalf("remaining=%d want 1", remaining)
	}
}
