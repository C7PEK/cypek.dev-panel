package api

import (
	"context"
	"errors"
	"fmt"
	"github.com/C7PEK/cypek.dev-panel/internal/safefs"
	"path/filepath"
	"strings"
	"time"

	"github.com/C7PEK/cypek.dev-panel/internal/shim"
	"github.com/C7PEK/cypek.dev-panel/internal/supervisor"
)

type scriptRunner struct{ s *Server }

const installScriptFile = "install.sh"

func (r scriptRunner) RunScript(ctx context.Context, serverID, serverDir, script string, env map[string]string, timeout time.Duration, logf func(string, ...any)) error {
	sup := r.s.sup

	scriptRel := filepath.Join(shim.MetaDir, installScriptFile)
	if err := safefs.WriteFile(serverDir, scriptRel, []byte(script), 0o700); err != nil {
		return err
	}
	defer safefs.Remove(serverDir, scriptRel)

	runRel := filepath.Join(shim.MetaDir, shim.RunFile)
	previous, prevErr := safefs.ReadFile(serverDir, runRel, 1<<20)
	defer func() {
		if prevErr == nil {
			_ = safefs.WriteFile(serverDir, runRel, previous, 0o600)
		} else {
			_ = safefs.Remove(serverDir, runRel)
		}
	}()
	if st, err := sup.Status(ctx, serverID); err == nil && st.Active() {
		return errors.New("the server is running; stop it before installing")
	}
	_ = safefs.Remove(serverDir, filepath.Join(shim.MetaDir, shim.ExitFile))
	cfg := &shim.RunConfig{Command: "/bin/bash", Args: []string{filepath.Join(shim.MetaDir, installScriptFile)}, Env: env, Oneshot: true, Stop: shim.StopConfig{TimeoutSec: 10}}
	if err := shim.SaveRunConfig(serverDir, cfg); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if err := sup.Start(ctx, serverID); err != nil {
		return fmt.Errorf("start install script: %w", err)
	}

	var console supervisor.Console
	for i := 0; i < 50 && console == nil; i++ {
		if c, err := sup.Attach(ctx, serverID); err == nil {
			console = c
			break
		}
		if info, _ := shim.ReadExitInfo(serverDir); info != nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if console != nil {
		var partial strings.Builder
		cr := false
		flush := func(line string) {
			line = strings.TrimRight(shim.StripANSI(line), " \t")
			if line != "" {
				logf("  %s", line)
			}
		}
	pump:
		for {
			select {
			case <-ctx.Done():
				logf("Install script timed out after %s; stopping it", timeout)
				_ = sup.Kill(context.Background(), serverID)
				console.Close()
				return fmt.Errorf("install script timed out after %s", timeout)
			case f, ok := <-console.Frames():
				if !ok {
					break pump
				}
				if f.Type != shim.TypeOutput {
					continue
				}
				for _, ch := range f.Data {
					switch {
					case ch == '\n':
						flush(partial.String())
						partial.Reset()
						cr = false
						continue
					case ch == '\r':
						cr = true
						continue
					case cr:
						partial.Reset()
						cr = false
					}
					partial.WriteRune(ch)
				}
			}
		}
		flush(partial.String())
		console.Close()
	}

	deadline := time.Now().Add(15 * time.Second)
	for {
		if info, _ := shim.ReadExitInfo(serverDir); info != nil {
			if st, err := sup.Status(ctx, serverID); err != nil || !st.Active() {
				if info.Error != "" {
					return fmt.Errorf("install script could not start: %s", info.Error)
				}
				if info.ExitCode != 0 {
					return fmt.Errorf("install script failed with exit code %d (see the log above)", info.ExitCode)
				}
				logf("Install script finished")
				return nil
			}
		}
		if time.Now().After(deadline) {
			return errors.New("install script finished but its result was not recorded")
		}
		time.Sleep(200 * time.Millisecond)
	}
}
