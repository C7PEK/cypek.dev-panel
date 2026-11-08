// Package web embeds the built frontend (web/dist) into the binary.
package web

import "embed"

// Dist holds the Vite build output. It only contains .gitkeep until
// `make build` (or `npm run build` in web/) has run.
//
//go:embed all:dist
var Dist embed.FS
