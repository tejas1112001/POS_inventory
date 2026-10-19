// Package db embeds the SQL migrations.
package db

import "embed"

// PlatformMigrations holds db/migrations/platform/*.sql.
//
//go:embed migrations/platform/*.sql
var PlatformMigrations embed.FS
