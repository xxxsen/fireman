// Package migrations embeds the SQLite migration SQL files distributed with
// the Fireman backend. Consumers (typically internal/db) read from FS by
// version-prefixed filename.
package migrations

import "embed"

// FS exposes every *.sql file shipped in this package.
//
//go:embed *.sql
var FS embed.FS
