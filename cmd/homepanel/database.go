package main

import (
	"context"
	"flag"
	"os"
	"time"

	"github.com/C7PEK/cypek.dev-panel/internal/database"
)

func runInstallDatabase(args []string) error {
	fs := flag.NewFlagSet("install-database", flag.ContinueOnError)
	port := fs.Int("phpmyadmin-port", database.DefaultPMAPort, "port for phpMyAdmin")
	if err := fs.Parse(args); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	return database.Install(ctx, database.InstallOptions{PhpMyAdminPort: *port}, os.Stdout)
}
