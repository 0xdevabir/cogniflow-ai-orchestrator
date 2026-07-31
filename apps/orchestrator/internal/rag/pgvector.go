package rag

import _ "embed"

//go:embed migrations/0001_init.sql
var initSQL string

// PgMigrations is the list of SQL files applied at startup. Embedded so the
// binary is self-contained.
var PgMigrations = []string{
	initSQL,
}
