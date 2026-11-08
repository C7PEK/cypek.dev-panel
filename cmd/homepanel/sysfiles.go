package main

import (
	"embed"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/cypek019/cypek.dev---panel/internal/config"
)

//go:embed systemfiles/*
var systemFiles embed.FS

// runInstallFiles writes the systemd units and the polkit rule shipped inside
// the binary, so install.sh never has to carry copies that could drift.
// Must run as root.
func runInstallFiles(args []string) error {
	flags := flag.NewFlagSet("install-files", flag.ContinueOnError)
	dryRun := flags.Bool("dry-run", false, "print what would be written")
	if err := flags.Parse(args); err != nil {
		return err
	}
	targets := map[string]string{
		"systemfiles/homepanel.service":         "/etc/systemd/system/homepanel.service",
		"systemfiles/homepanel-server@.service": "/etc/systemd/system/homepanel-server@.service",
		"systemfiles/50-homepanel.rules":        "/etc/polkit-1/rules.d/50-homepanel.rules",
	}
	for src, dst := range targets {
		b, err := systemFiles.ReadFile(src)
		if err != nil {
			return err
		}
		if *dryRun {
			fmt.Printf("would write %s (%d bytes)\n", dst, len(b))
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return fmt.Errorf("create %s: %w", filepath.Dir(dst), err)
		}
		if err := os.WriteFile(dst, b, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", dst, err)
		}
		fmt.Println("wrote", dst)
	}
	return nil
}

// runInitConfig creates /etc/homepanel/config.toml with fresh secrets unless
// it already exists. Used by install.sh; idempotent.
func runInitConfig(args []string) error {
	flags := flag.NewFlagSet("init-config", flag.ContinueOnError)
	path := flags.String("config", config.DefaultPath, "config file to create")
	port := flags.Int("port", config.DefaultPort, "HTTP port")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if _, err := os.Stat(*path); err == nil {
		fmt.Println("config exists, keeping", *path)
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	base := config.Default()
	base.Listen = fmt.Sprintf(":%d", *port)
	if err := config.Generate(*path, base); err != nil {
		return err
	}
	fmt.Println("wrote", *path)
	return nil
}
