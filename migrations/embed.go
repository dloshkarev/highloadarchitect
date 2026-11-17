// Package migrations embeds goose SQL migrations so they can be applied at runtime.
package migrations

import "embed"

// FS contains all goose migrations in this directory.
//
//go:embed *.sql
var FS embed.FS
