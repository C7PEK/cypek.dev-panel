package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/C7PEK/cypek.dev-panel/internal/staticsite"
)

func runStatic(args []string) error {
	fs := flag.NewFlagSet("static", flag.ContinueOnError)
	dir := fs.String("dir", "public", "folder with the site files")
	port := fs.Int("port", 8080, "port to listen on")
	bind := fs.String("bind", "0.0.0.0", "address to listen on")
	spa := fs.Bool("spa", false, "serve index.html for unknown paths (React, Vue and similar apps)")
	noCache := fs.Bool("no-cache", false, "ask browsers to check for changes on every load")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *port < 1 || *port > 65535 {
		return fmt.Errorf("port %d is out of range", *port)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	return staticsite.Serve(ctx, staticsite.Options{
		Dir:     *dir,
		Listen:  *bind + ":" + strconv.Itoa(*port),
		SPA:     *spa,
		NoCache: *noCache,
		Log:     os.Stdout,
	})
}
