package main

import (
	"embed"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"

	"github.com/C7PEK/cypek.dev-panel/internal/config"
)

//go:embed systemfiles/*
var systemFiles embed.FS

func runInstallFiles(args []string) error {
	flags := flag.NewFlagSet("install-files", flag.ContinueOnError)
	dryRun := flags.Bool("dry-run", false, "print what would be written")
	if err := flags.Parse(args); err != nil {
		return err
	}
	targets := map[string]string{
		"systemfiles/homepanel.service":                  "/etc/systemd/system/homepanel.service",
		"systemfiles/homepanel-server@.service":          "/etc/systemd/system/homepanel-server@.service",
		"systemfiles/50-homepanel.rules":                 "/etc/polkit-1/rules.d/50-homepanel.rules",
		"systemfiles/homepanel-database-install.service": "/etc/systemd/system/homepanel-database-install.service",
		"systemfiles/homepanel-update.service":           "/etc/systemd/system/homepanel-update.service",
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
	if !*dryRun {
		secureConfigDir()
	}
	return nil
}

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

func secureConfigDir() {
	const dir = "/etc/homepanel"
	if _, err := os.Stat(dir); err != nil {
		return
	}
	u, err := user.Lookup("homepanel")
	if err != nil {
		return
	}
	gid, _ := strconv.Atoi(u.Gid)
	if err := os.Chown(dir, 0, gid); err == nil {
		_ = os.Chmod(dir, 0o750)
		fmt.Println("secured", dir, "(root:homepanel 0750)")
	}
}
