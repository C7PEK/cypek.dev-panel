package installer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"

	"github.com/C7PEK/cypek.dev-panel/internal/providers"
	"github.com/C7PEK/cypek.dev-panel/internal/templates"
)

// downloadPaper fetches a PaperMC project build (paper, folia, velocity),
// verified with the SHA-256 from the Fill API.
func (c *stepCtx) downloadPaper(ctx context.Context, n int, project, version, dest string) error {
	names := map[string]string{"paper": "Paper", "folia": "Folia", "velocity": "Velocity"}
	c.begin(n, "Downloading "+names[project])
	if c.m.deps.Paper == nil {
		return errors.New("paper provider not configured")
	}
	info, err := c.m.deps.Paper.ForProject(project).Resolve(ctx, version)
	if err != nil {
		return fmt.Errorf("resolve %s %s: %w", names[project], version, err)
	}
	c.logf("%s %s build %d (%s), needs Java %d", names[project], info.Version, info.LatestBuild, info.DownloadName, info.JavaMinimum)
	if err := providers.Download(ctx, info.DownloadURL, dest, info.SHA256, c.progress); err != nil {
		return err
	}
	c.logf("Downloaded and verified %s", filepath.Base(dest))
	if project == "velocity" {
		// A proxy has its own version line, not a Minecraft version.
		c.vars["PROXY_VERSION"] = info.Version
	} else {
		c.vars["MC_VERSION_RESOLVED"] = info.Version
	}
	c.vars["MC_BUILD"] = strconv.Itoa(info.LatestBuild)
	c.vars["JAVA_MIN"] = strconv.Itoa(info.JavaMinimum)
	flags, _ := json.Marshal(info.JVMFlags)
	c.vars[templates.KeyJVMFlags] = string(flags)
	return nil
}

// downloadLoader fetches a mod loader installer (Forge, NeoForge, Quilt).
// The template runs it next with an exec step; Java is prepared for it.
func (c *stepCtx) downloadLoader(ctx context.Context, n int, kind, game, dest string) error {
	var (
		jar *providers.InstallerJar
		err error
	)
	switch kind {
	case "forge":
		c.begin(n, "Downloading the Forge installer")
		f := c.m.deps.Forge
		if f == nil {
			f = providers.NewForge()
		}
		jar, err = f.Resolve(ctx, game)
	case "neoforge":
		c.begin(n, "Downloading the NeoForge installer")
		f := c.m.deps.NeoForge
		if f == nil {
			f = providers.NewNeoForge()
		}
		jar, err = f.Resolve(ctx, game)
	default:
		c.begin(n, "Downloading the Quilt installer")
		q := c.m.deps.Quilt
		if q == nil {
			q = providers.NewQuilt()
		}
		jar, err = q.Installer(ctx, game)
	}
	if err != nil {
		return err
	}
	c.logf("%s %s for Minecraft %s", kind, jar.Version, jar.Game)
	if jar.Sum == "" {
		c.logf("No checksum published for this build; downloading over HTTPS only")
	}
	if err := providers.DownloadVerified(ctx, jar.URL, dest, jar.Hash(), jar.Sum, 0, c.progress); err != nil {
		return err
	}
	if jar.Sum != "" {
		c.logf("Downloaded and verified %s", filepath.Base(dest))
	}
	c.vars["MC_VERSION_RESOLVED"] = jar.Game
	c.vars["LOADER_VERSION"] = jar.Version
	mj := c.m.deps.Mojang
	if mj == nil {
		mj = providers.NewMojang()
	}
	if srv, err := mj.Resolve(ctx, jar.Game); err == nil && srv.JavaMajor > 0 {
		c.vars["JAVA_MIN"] = strconv.Itoa(srv.JavaMajor)
	} else if err != nil {
		c.logf("Could not look up the Java version for Minecraft %s (%v)", jar.Game, err)
	}
	return nil
}
