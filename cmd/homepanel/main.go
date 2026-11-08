// Command homepanel is the HomePanel binary: the web panel (serve), the
// per-server supervisor shim (shim), self-update and maintenance commands.
package main

import (
	"fmt"
	"os"
)

// version is overridden at build time via -ldflags "-X main.version=...".
var version = "dev"

func usage() {
	fmt.Fprintf(os.Stderr, `panel.cypek.dev %s

Usage:
  homepanel serve [--config PATH] [--dev] [--listen ADDR]   run the web panel
  homepanel shim <server-id>                                 supervise one game server (used by systemd)
  homepanel update [--check]                                 update the panel binary from GitHub releases
  homepanel install-files                                    write systemd units + polkit rule (root)
  homepanel init-config [--port N]                           create /etc/homepanel/config.toml if missing (root)
  homepanel install-database [--phpmyadmin-port 8081]        install MariaDB + phpMyAdmin for game servers (root)
  homepanel reset-password <username>                        set a new password from the terminal
  homepanel fetch mojang|paper|fabric-installer ...          download server jars (used by imported templates)
  homepanel version                                          print the version
`, version)
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "serve":
		err = runServe(os.Args[2:])
	case "shim":
		err = runShim(os.Args[2:])
	case "update":
		err = runUpdate(os.Args[2:])
	case "install-files":
		err = runInstallFiles(os.Args[2:])
	case "init-config":
		err = runInitConfig(os.Args[2:])
	case "install-database":
		err = runInstallDatabase(os.Args[2:])
	case "reset-password":
		err = runResetPassword(os.Args[2:])
	case "fetch":
		err = runFetch(os.Args[2:])
	case "version", "--version", "-v":
		fmt.Println(version)
	case "help", "--help", "-h":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
