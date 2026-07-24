// Package migrations exposes the ordered SQL migration history as an embedded
// filesystem so every migration command runs the exact files reviewed in Git.
package migrations

import "embed"

// Files contains every up and down migration in this directory.
//
//go:embed *.sql
var Files embed.FS
