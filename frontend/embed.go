// Package frontend embeds the built settings window (frontend/dist).
// Build it first with `npm ci && npm run build` in this directory.
package frontend

import "embed"

//go:embed all:dist
var Dist embed.FS
