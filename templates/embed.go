// Package templates embeds the built-in game templates (YAML).
package templates

import "embed"

//go:embed *.yaml
var Builtin embed.FS
