package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/cypek019/cypek.dev---panel/internal/config"
	"github.com/cypek019/cypek.dev---panel/internal/shim"
)

// runShim is the systemd ExecStart of homepanel-server@<id>.service. It never
// touches the database: everything comes from the server's run.json.
func runShim(args []string) error {
	fs := flag.NewFlagSet("shim", flag.ContinueOnError)
	dir := fs.String("dir", "", "server directory (default /var/lib/homepanel/servers/<id>)")
	socket := fs.String("socket", "", "console socket path (default /run/homepanel-<id>/console.sock)")
	logLevel := fs.String("log-level", "info", "log level")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: homepanel shim <server-id> [--dir DIR] [--socket PATH]")
	}
	id := fs.Arg(0)
	if *dir == "" {
		*dir = filepath.Join(config.DefaultDataDir, "servers", id)
	}
	if *socket == "" {
		*socket = fmt.Sprintf("/run/homepanel-%s/console.sock", id)
	}
	log := newLogger(*logLevel).With("shim", id)

	cfg, err := shim.LoadRunConfig(*dir)
	if err != nil {
		return err
	}
	s, err := shim.New(cfg, shim.Options{ID: id, ServerDir: *dir, SocketPath: *socket, Log: log})
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	code, err := s.Run(ctx)
	if err != nil {
		return err
	}
	// Propagate the game's exit code so systemd's Restart=on-failure sees crashes.
	os.Exit(code)
	return nil
}
