package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/C7PEK/cypek.dev-panel/internal/selfupdate"
)

// runUpdate replaces this binary with the newest GitHub release and restarts
// only homepanel.service (game servers are separate units and keep running).
func runUpdate(args []string) error {
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	check := fs.Bool("check", false, "only report whether an update is available")
	noRestart := fs.Bool("no-restart", false, "do not restart homepanel.service afterwards")
	if err := fs.Parse(args); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	client := selfupdate.New()
	rel, err := client.Latest(ctx)
	if err != nil {
		return err
	}
	fmt.Printf("installed: %s\nlatest:    %s\n", version, rel.Tag)
	if !selfupdate.Newer(version, rel.Tag) {
		fmt.Println("already up to date")
		return nil
	}
	if *check {
		fmt.Println("update available; run: sudo homepanel update")
		return nil
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	if err := client.Apply(ctx, rel, self, func(f string, a ...any) { fmt.Printf(f+"\n", a...) }); err != nil {
		return err
	}
	if *noRestart {
		fmt.Println("restart the panel with: systemctl restart homepanel")
		return nil
	}
	// systemctl here is fine: this runs as root from a terminal, not inside the panel.
	if out, err := exec.CommandContext(ctx, "systemctl", "restart", "homepanel").CombinedOutput(); err != nil {
		return fmt.Errorf("restart homepanel.service: %v: %s", err, out)
	}
	fmt.Println("homepanel.service restarted; game servers were not touched")
	return nil
}
