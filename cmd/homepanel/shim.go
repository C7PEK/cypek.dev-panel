package main

import (
	"os"
	"path/filepath"

	"github.com/cypek019/cypek.dev---panel/internal/config"
	"github.com/cypek019/cypek.dev---panel/internal/shim"
)

// runShim is the systemd ExecStart of homepanel-server@<id>.service. It never
// touches the database: everything comes from the server's run.json.
func runShim(args []string) error {
	// Peel off --log-level; the rest is parsed by shim.Main.
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
