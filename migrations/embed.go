// Package migrations embeds the consolidated SQLite DDL baseline distributed
// with the Fireman backend.
package migrations

import "embed"

// FS exposes the DDL-only baseline schema.
//
//go:embed *.sql
var FS embed.FS
