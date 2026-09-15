// Package migrations embeds the SQL files in this directory. go:embed patterns can't cross
// out of the directory containing the source file, and services/server/README.md commits to
// keeping the SQL here (not under internal/) — so this one small file lives beside the .sql
// it embeds, and internal/db imports it.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
