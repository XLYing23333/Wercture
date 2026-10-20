// Package migrations embeds versioned SQL for the server database.
package migrations

import "embed"

// FS contains the ordered schema files.
//
//go:embed *.sql
var FS embed.FS
