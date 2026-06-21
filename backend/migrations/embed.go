// Package migrations embeds the SQL migration files so the server can apply them
// on startup without an external migration tool.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
