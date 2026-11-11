package main

import (
	"os"
	"path/filepath"

	"github.com/C7PEK/cypek.dev-panel/internal/config"
	"github.com/C7PEK/cypek.dev-panel/internal/shim"
)

func runShim(args []string) error {
	level := "info"
	rest := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		if args[i] == "--log-level" && i+1 < len(args) {
			level = args[i+1]
			i++
			continue
		}
		rest = append(rest, args[i])
	}
	os.Exit(shim.Main(rest, filepath.Join(config.DefaultDataDir, "servers"), newLogger(level)))
	return nil
}
