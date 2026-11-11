package templates

import "embed"

//go:embed *.yaml
var Builtin embed.FS
