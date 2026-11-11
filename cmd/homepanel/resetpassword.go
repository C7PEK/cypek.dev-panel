package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"

	"github.com/C7PEK/cypek.dev-panel/internal/auth"
	"github.com/C7PEK/cypek.dev-panel/internal/config"
	"github.com/C7PEK/cypek.dev-panel/internal/db"
)

func runResetPassword(args []string) error {
	fs := flag.NewFlagSet("reset-password", flag.ContinueOnError)
	configPath := fs.String("config", config.DefaultPath, "path to config.toml")
	dev := fs.Bool("dev", false, "use ./.devdata like `serve --dev`")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: homepanel reset-password <username>")
	}
	username := fs.Arg(0)

	cfg, err := loadConfig(*configPath, *dev)
	if err != nil {
		return err
	}
	sqlDB, err := db.Open(cfg.DBPath())
	if err != nil {
		return err
	}
	defer sqlDB.Close()
	ctx := context.Background()
	if err := db.Migrate(ctx, sqlDB, cfg.DBPath(), newLogger("warn")); err != nil {
		return err
	}
	store := auth.NewStore(sqlDB)
	user, err := store.GetUserByUsername(ctx, username)
	if errors.Is(err, auth.ErrNotFound) {
		return fmt.Errorf("user %q does not exist", username)
	}
	if err != nil {
		return err
	}

	password, err := readPassword()
	if err != nil {
		return err
	}
	if err := store.SetPassword(ctx, user.ID, password); err != nil {
		return fmt.Errorf("set password: %w", err)
	}
	fmt.Printf("Password for %s updated; all their sessions were logged out.\n", user.Username)
	return nil
}

func readPassword() (string, error) {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		line, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil && line == "" {
			return "", fmt.Errorf("read password from stdin: %w", err)
		}
		return strings.TrimRight(line, "\r\n"), nil
	}
	fmt.Fprint(os.Stderr, "New password: ")
	first, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", err
	}
	fmt.Fprint(os.Stderr, "Repeat password: ")
	second, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", err
	}
	if string(first) != string(second) {
		return "", errors.New("passwords do not match")
	}
	return string(first), nil
}
