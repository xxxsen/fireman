package repository

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// ResolutionTicket binds a resolve result for one-time import-async consumption.
type ResolutionTicket struct {
	ID             string
	Market         string
	InstrumentType string
	Code           string
	ProviderSymbol string
	Name           string
	Exchange       string
	InstrumentKind string
	CreatedAt      int64
	ExpiresAt      int64
	ConsumedAt     *int64
}

// ResolutionTicketRepo manages resolution_tickets.
type ResolutionTicketRepo struct {
	db *sql.DB
}

func NewResolutionTicketRepo(db *sql.DB) *ResolutionTicketRepo {
	return &ResolutionTicketRepo{db: db}
}

func (r *ResolutionTicketRepo) Create(ctx context.Context, tx *sql.Tx, ticket ResolutionTicket) error {
	exec := r.exec(tx)
	now := time.Now().UnixMilli()
	if ticket.CreatedAt == 0 {
		ticket.CreatedAt = now
	}
	_, err := exec.ExecContext(ctx, `
		INSERT INTO resolution_tickets (
			id, market, instrument_type, code, provider_symbol, name,
			exchange, instrument_kind, created_at, expires_at
		) VALUES (?,?,?,?,?,?,?,?,?,?)`,
		ticket.ID, ticket.Market, ticket.InstrumentType, ticket.Code,
		ticket.ProviderSymbol, ticket.Name, ticket.Exchange, ticket.InstrumentKind,
		ticket.CreatedAt, ticket.ExpiresAt)
	return err
}

func (r *ResolutionTicketRepo) GetByID(ctx context.Context, id string) (ResolutionTicket, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, market, instrument_type, code, provider_symbol, name,
			exchange, instrument_kind, created_at, expires_at, consumed_at
		FROM resolution_tickets WHERE id=?`, id)
	return scanResolutionTicket(row)
}

func (r *ResolutionTicketRepo) Consume(ctx context.Context, tx *sql.Tx, id string) (ResolutionTicket, error) {
	exec := r.exec(tx)
	now := time.Now().UnixMilli()
	res, err := exec.ExecContext(ctx, `
		UPDATE resolution_tickets SET consumed_at=?
		WHERE id=? AND consumed_at IS NULL AND expires_at > ?`,
		now, id, now)
	if err != nil {
		return ResolutionTicket{}, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		ticket, getErr := r.GetByID(ctx, id)
		if getErr != nil {
			if errors.Is(getErr, ErrResolutionTicketNotFound) {
				return ResolutionTicket{}, ErrResolutionTicketNotFound
			}
			return ResolutionTicket{}, getErr
		}
		if ticket.ConsumedAt != nil {
			return ResolutionTicket{}, ErrResolutionTicketConsumed
		}
		if ticket.ExpiresAt <= now {
			return ResolutionTicket{}, ErrResolutionTicketExpired
		}
		return ResolutionTicket{}, ErrResolutionTicketNotFound
	}
	return r.GetByID(ctx, id)
}

func scanResolutionTicket(row *sql.Row) (ResolutionTicket, error) {
	var t ResolutionTicket
	var consumed sql.NullInt64
	err := row.Scan(
		&t.ID, &t.Market, &t.InstrumentType, &t.Code, &t.ProviderSymbol, &t.Name,
		&t.Exchange, &t.InstrumentKind, &t.CreatedAt, &t.ExpiresAt, &consumed,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return ResolutionTicket{}, ErrResolutionTicketNotFound
	}
	if err != nil {
		return ResolutionTicket{}, err
	}
	if consumed.Valid {
		v := consumed.Int64
		t.ConsumedAt = &v
	}
	return t, nil
}

func (r *ResolutionTicketRepo) exec(tx *sql.Tx) dbExec {
	if tx != nil {
		return tx
	}
	return r.db
}

var (
	ErrResolutionTicketNotFound = errors.New("resolution ticket not found")
	ErrResolutionTicketExpired  = errors.New("resolution ticket expired")
	ErrResolutionTicketConsumed = errors.New("resolution ticket consumed")
)

// IsJobUniqueConstraint reports SQLite unique index violations on jobs.
func IsJobUniqueConstraint(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique constraint failed") && strings.Contains(msg, "jobs")
}
