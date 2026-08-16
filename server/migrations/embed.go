// Package migrations exposes the clean database baseline as an embedded
// filesystem.
package migrations

import "embed"

// Files contains the only executable migration pair.
//
//go:embed 000001_clean_baseline.*.sql
var Files embed.FS
