package main

import (
	"embed"
	"io/fs"
)

// mustSub is for embedded directories whose presence is guaranteed at build time.
func mustSub(efs embed.FS, dir string) fs.FS {
	sub, err := fs.Sub(efs, dir)
	if err != nil {
		panic("embedded dir missing: " + dir)
	}
	return sub
}
