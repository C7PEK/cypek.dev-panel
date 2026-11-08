package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/cypek019/cypek.dev---panel/internal/files"
	"github.com/cypek019/cypek.dev---panel/internal/providers"
)

// runFetch is a helper for install scripts of imported templates (it runs
// inside the server sandbox): it downloads well-known server jars with
// checksum verification so converted PufferPanel templates need neither jq
// nor hand-written API calls.
//
//	homepanel fetch mojang <version|latest> <target>
//	homepanel fetch paper <version|latest> <target>
//	homepanel fetch fabric-installer <target>
func runFetch(args []string) error {
	if len(args) < 2 {
		return errors.New("usage: homepanel fetch mojang|paper <version> <target> | fabric-installer <target>")
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	target := func(p string) (string, error) {
		dest, err := files.ResolveSafe(cwd, p)
		if err != nil {
			return "", fmt.Errorf("target %q: %w", p, err)
		}
		return dest, nil
	}
	switch args[0] {
	case "mojang":
		if len(args) != 3 {
			return errors.New("usage: homepanel fetch mojang <version|latest> <target>")
		}
		dest, err := target(args[2])
		if err != nil {
			return err
		}
		srv, err := providers.NewMojang().Resolve(ctx, args[1])
		if err != nil {
			return err
		}
		fmt.Printf("Downloading Minecraft %s server\n", srv.Version)
		if err := providers.Download(ctx, srv.URL, dest, "", nil); err != nil {
			return err
		}
		if err := providers.VerifySHA1(dest, srv.SHA1); err != nil {
			_ = os.Remove(dest)
			return err
		}
	case "paper":
		if len(args) != 3 {
			return errors.New("usage: homepanel fetch paper <version|latest> <target>")
		}
		dest, err := target(args[2])
		if err != nil {
			return err
		}
		info, err := providers.NewPaper().Resolve(ctx, args[1])
		if err != nil {
			return err
		}
		fmt.Printf("Downloading Paper %s build %d\n", info.Version, info.LatestBuild)
		if err := providers.Download(ctx, info.DownloadURL, dest, info.SHA256, nil); err != nil {
			return err
		}
	case "fabric-installer":
		dest, err := target(args[1])
		if err != nil {
			return err
		}
		u, err := providers.FabricInstallerURL(ctx)
		if err != nil {
			return err
		}
		fmt.Println("Downloading the Fabric installer")
		if err := providers.Download(ctx, u, dest, "", nil); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown fetch kind %q", args[0])
	}
	fmt.Println("Done")
	return nil
}
