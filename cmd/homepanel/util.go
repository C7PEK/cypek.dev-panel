package main

import (
	"embed"
	"io/fs"
)

func mustSub(efs embed.FS, dir string) fs.FS {
	sub, err := fs.Sub(efs, dir)
	if err != nil {
		panic("embedded dir missing: " + dir)
	}
	return sub
}
