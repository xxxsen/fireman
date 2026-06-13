package repository

import (
	"context"
	"database/sql"
)

func queryCollect[T any](
	ctx context.Context,
	db *sql.DB,
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
