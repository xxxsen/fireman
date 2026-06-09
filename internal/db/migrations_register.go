package db

import "github.com/fireman/fireman/migrations"

func init() {
	SetMigrations(migrations.FS)
}
