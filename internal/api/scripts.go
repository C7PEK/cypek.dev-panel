package api

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cypek019/cypek.dev---panel/internal/shim"
	"github.com/cypek019/cypek.dev---panel/internal/supervisor"
)

// scriptRunner runs install scripts through the server's own supervisor unit
// (homepanel-server@<id>), so a script gets exactly the sandbox the game
// gets: the panel user, write access to the server directory only, private
// /tmp. The unit's run.json is swapped for a oneshot bash run and restored.
type scriptRunner struct{ s *Server }

const installScriptFile = "install.sh"

func (r scriptRunner) RunScript(ctx context.Context, serverID, serverDir, script string, env map[string]string, timeout time.Duration, logf func(string, ...any)) error {
	sup := r.s.sup
	meta := filepath.Join(serverDir, shim.MetaDir)
	if err := os.MkdirAll(meta, 0o750); err != nil {
		return err
	}
	scriptPath := filepath.Join(meta, installScriptFile)
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		return err
	}
	defer os.Remove(scriptPath)

	runPath := filepath.Join(meta, shim.RunFile)
	previous, prevErr := os.ReadFile(runPath)
	defer func() {
		if prevErr == nil {
			_ = os.WriteFile(runPath, previous, 0o600)
		} else {
			_ = os.Remove(runPath)
		}
	}()
	if st, err := sup.Status(ctx, serverID); err == nil && st.Active() {
		return errors.New("the server is running; stop it before installing")
	}
	_ = os.Remove(filepath.Join(meta, shim.ExitFile))
	cfg := &shim.RunConfig{Command: "/bin/bash", Args: []string{filepath.Join(shim.MetaDir, installScriptFile)}, Env: env, Oneshot: true, Stop: shim.StopConfig{TimeoutSec: 10}}
	if err := shim.SaveRunConfig(serverDir, cfg); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if err := sup.Start(ctx, serverID); err != nil {
		return fmt.Errorf("start install script: %w", err)
	}

	// Stream output until the shim closes the console.
	var console supervisor.Console
	for i := 0; i < 50 && console == nil; i++ {
		if c, err := sup.Attach(ctx, serverID); err == nil {
			console = c
			break
		}
		if info, _ := shim.ReadExitInfo(serverDir); info != nil {
			break // finished before we could attach
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
						// A bare \r redraws the line (progress bars): keep
						// only the latest state.
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
	// Wait for the shim to record the exit.
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
