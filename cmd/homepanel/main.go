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
	fmt.Fprintf(os.Stderr, `HomePanel %s

Usage:
  homepanel serve [--config PATH] [--dev] [--listen ADDR]   run the web panel
  homepanel shim <server-id>                                 supervise one game server (used by systemd)
  homepanel update                                           update the panel binary
  homepanel reset-password <username>                        set a new password from the terminal
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
	case "reset-password":
		err = runResetPassword(os.Args[2:])
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

func runShim(args []string) error {
	return fmt.Errorf("shim is not implemented yet (milestone M1)")
}

func runUpdate(args []string) error {
	return fmt.Errorf("update is not implemented yet (milestone M6)")
}
