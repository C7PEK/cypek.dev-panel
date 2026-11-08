package shim

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
)

// Main implements `homepanel shim <id> [--dir DIR] [--socket PATH]` and
// returns the process exit code. It lives here so tests can run a real shim
// from the test binary without building the CLI.
func Main(args []string, defaultServersDir string, log *slog.Logger) int {
	fs := flag.NewFlagSet("shim", flag.ContinueOnError)
	dir := fs.String("dir", "", "server directory (default <servers>/<id>)")
	socket := fs.String("socket", "", "console socket path (default /run/homepanel-<id>/console.sock)")
	// Accept the id before or after the flags: systemd runs `shim %i`, the
	// local supervisor passes flags too, and flag.Parse stops at the first
	// positional argument.
	var positional, flags []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "-") {
			flags = append(flags, a)
			if !strings.Contains(a, "=") && i+1 < len(args) {
				flags = append(flags, args[i+1]) // the flag's value
				i++
			}
			continue
		}
		positional = append(positional, a)
	}
	if err := fs.Parse(flags); err != nil {
		return 2
	}
	if len(positional) != 1 {
		fmt.Fprintln(os.Stderr, "usage: homepanel shim <server-id> [--dir DIR] [--socket PATH]")
		return 2
	}
	id := positional[0]
	if *dir == "" {
		*dir = filepath.Join(defaultServersDir, id)
	}
	if *socket == "" {
		*socket = fmt.Sprintf("/run/homepanel-%s/console.sock", id)
	}
	log = log.With("shim", id)

	cfg, err := LoadRunConfig(*dir)
	if err != nil {
		log.Error("load run config", "err", err)
		return ExitConfigError
	}
	s, err := New(cfg, Options{ID: id, ServerDir: *dir, SocketPath: *socket, Log: log})
	if err != nil {
		log.Error("init shim", "err", err)
		return ExitConfigError
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	code, err := s.Run(ctx)
	if err != nil {
		if !errors.Is(err, context.Canceled) {
			log.Error("shim failed", "err", err)
		}
		if code == 0 {
			code = 1
		}
	}
	return code
}
