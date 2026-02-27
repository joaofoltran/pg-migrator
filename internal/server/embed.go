package server

import "embed"

// dist holds the built React frontend assets.
// When no build has been done, the directory will be empty and the
// server falls back to a simple JSON-only API mode.
//
//go:embed all:dist
var distFS embed.FS
