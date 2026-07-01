// Package migrations embeds the SQL migration files so the binary can apply
// them on boot without an external migrate tool. The .sql files remain
// compatible with golang-migrate naming (NNNNNN_name.up.sql / .down.sql).
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
