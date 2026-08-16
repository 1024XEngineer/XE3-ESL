// Package ieltsdata embeds the versioned IELTS Speaking content shipped with
// the server binary.
package ieltsdata

import "embed"

const CurrentFile = "2026-05-08-mainland.json"

// Files contains the immutable repository-owned question-bank documents.
//
//go:embed 2026-05-08-mainland.json
var Files embed.FS
