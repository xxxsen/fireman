package repository

import (
	"context"
	"database/sql"
)

// rowQuerier is satisfied by both *sql.DB and *sql.Tx so read helpers can run
// inside or outside a transaction.
type rowQuerier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

func queryCollect[T any](
	ctx context.Context,
	db rowQuerier,
	query string,
	args []any,
	scan func(*sql.Rows) (T, error),
	queryMsg, scanMsg, iterMsg string,
) ([]T, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, wrapSQL(queryMsg, err)
	}
	return collectRows(rows, scan, scanMsg, iterMsg)
}

// queryPage runs the shared COUNT + paged SELECT pattern behind every admin
// listing. countSQL and selectSQL already contain their WHERE clause and
// share args; selectSQL must end with `LIMIT ? OFFSET ?`. A non-positive
// limit falls back to 20.
func queryPage[T any](
	ctx context.Context,
	db *sql.DB,
	countSQL, selectSQL string,
	args []any,
	limit, offset int,
	scan func(*sql.Rows) (T, error),
	countMsg, queryMsg, scanMsg, iterMsg string,
) ([]T, int, error) {
	var total int
	if err := db.QueryRowContext(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, 0, wrapSQL(countMsg, err)
	}
	if limit <= 0 {
		limit = 20
	}
	pagedArgs := append(append([]any{}, args...), limit, offset)
	items, err := queryCollect(ctx, db, selectSQL, pagedArgs, scan, queryMsg, scanMsg, iterMsg)
	if err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func collectRows[T any](
	rows *sql.Rows,
	scan func(*sql.Rows) (T, error),
	scanMsg, iterMsg string,
) ([]T, error) {
	defer func() { _ = rows.Close() }()
	var out []T
	for rows.Next() {
		item, err := scan(rows)
		if err != nil {
			return nil, wrapSQL(scanMsg, err)
		}
		out = append(out, item)
	}
	return out, wrapSQL(iterMsg, rows.Err())
}
