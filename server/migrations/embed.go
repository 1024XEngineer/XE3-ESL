// Package migrations exposes the append-only database history as an embedded
// filesystem.
package migrations

import "embed"

// Files contains every reviewed migration pair.
//
//go:embed *.sql
var Files embed.FS
