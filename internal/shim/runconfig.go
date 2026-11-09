package shim

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/C7PEK/cypek.dev-panel/internal/safefs"
	"os"
	"path/filepath"
	"strings"
)

// RunConfig is .homepanel/run.json: everything the shim needs to run one
// server. The panel writes it from the server's template and variables.
type RunConfig struct {
	// Command is an absolute path or a name resolved via PATH; never a shell line.
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env,omitempty"`
	// Stop is sent to stdin on graceful stop. Empty means SIGTERM instead.
	Stop StopConfig `json:"stop"`
	// ReadyRegex marks the switch from "starting" to "running". Empty means
	// the server counts as running as soon as the process starts.
	ReadyRegex string `json:"readyRegex,omitempty"`
	// ErrorRegex marks console lines recorded as error events; empty uses
	// DefaultErrorRegex.
	ErrorRegex string `json:"errorRegex,omitempty"`
	// Cols/Rows size the PTY; zero picks defaults.
	Cols uint16 `json:"cols,omitempty"`
	Rows uint16 `json:"rows,omitempty"`
	// Dir is the working directory relative to the server dir ("" = server dir).
	Dir string `json:"dir,omitempty"`
	// Oneshot marks a task (an install script) rather than a server: the
	// shim always exits 0 so systemd never restarts it, records the real exit
	// code in last-exit.json and writes no lifecycle events.
	Oneshot bool `json:"oneshot,omitempty"`
}

// StopConfig describes graceful shutdown.
type StopConfig struct {
	Command    string `json:"command,omitempty"`
	TimeoutSec int    `json:"timeoutSec,omitempty"`
}

const (
	// MetaDir is the per-server directory holding run.json and shim state.
	MetaDir     = ".homepanel"
	RunFile     = "run.json"
	PIDFile     = "shim.pid"
	ExitFile    = "last-exit.json"
	ShimLogFile = "shim.log"
	LogsDir     = "logs"

	DefaultStopTimeoutSec = 60
	DefaultCols           = 200
	DefaultRows           = 50
)

// LoadRunConfig reads serverDir/.homepanel/run.json.
func LoadRunConfig(serverDir string) (*RunConfig, error) {
	path := filepath.Join(serverDir, MetaDir, RunFile)
	b, err := safefs.ReadFile(serverDir, filepath.Join(MetaDir, RunFile), maxMetaFile)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var cfg RunConfig
	if err := json.Unmarshal(b, &cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &cfg, nil
}

// SaveRunConfig writes run.json atomically.
func SaveRunConfig(serverDir string, cfg *RunConfig) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("encode run config: %w", err)
	}
	// The panel writes this into a folder the game can modify: never follow
	// a planted symlink (safefs works beneath the server folder).
	if err := safefs.WriteFile(serverDir, filepath.Join(MetaDir, RunFile), b, 0o600); err != nil {
		return fmt.Errorf("write run config: %w", err)
	}
	return nil
}

// maxMetaFile bounds .homepanel files the panel reads (a game could point
// them at /dev/zero).
const maxMetaFile = 1 << 20

// MaxStopTimeoutSec stays below the unit's TimeoutStopSec (180) so the shim,
// not systemd, ends a game that ignores its stop command.
const MaxStopTimeoutSec = 165

// Validate checks required fields and fills defaults.
func (c *RunConfig) Validate() error {
	if c.Command == "" {
		return errors.New("command is required")
	}
	if c.Stop.TimeoutSec <= 0 {
		c.Stop.TimeoutSec = DefaultStopTimeoutSec
	}
	if c.Stop.TimeoutSec > MaxStopTimeoutSec {
		c.Stop.TimeoutSec = MaxStopTimeoutSec
	}
	if c.Cols == 0 {
		c.Cols = DefaultCols
	}
	if c.Rows == 0 {
		c.Rows = DefaultRows
	}
	if filepath.IsAbs(c.Dir) || strings.Contains(c.Dir, "..") || strings.ContainsAny(c.Dir, "\x00\n") {
		return errors.New("dir must be a relative path inside the server directory")
	}
	c.Dir = strings.Trim(filepath.ToSlash(filepath.Clean("/"+c.Dir)), "/")
	if c.Dir == "." {
		c.Dir = ""
	}
	return nil
}

// ExitInfo is written to .homepanel/last-exit.json when the process ends, so
// a supervisor can tell a clean stop from a crash after the shim is gone.
type ExitInfo struct {
	ExitCode  int    `json:"exitCode"`
	ExitedAt  string `json:"exitedAt"`
	Requested bool   `json:"requested"` // true when the stop was asked for
	Signal    string `json:"signal,omitempty"`
	// Error explains a failure to start at all (command missing, not
	// executable); shown to the user next to the crashed state.
	Error string `json:"error,omitempty"`
}

// ExitConfigError is the shim's exit status when the command cannot be
// started. The unit lists it in RestartPreventExitStatus.
const ExitConfigError = 78

// ReadExitInfo returns the last exit record, or nil if none.
func ReadExitInfo(serverDir string) (*ExitInfo, error) {
	b, err := safefs.ReadFile(serverDir, filepath.Join(MetaDir, ExitFile), maxMetaFile)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var info ExitInfo
	if err := json.Unmarshal(b, &info); err != nil {
		return nil, err
	}
	return &info, nil
}

func writeExitInfo(serverDir string, info ExitInfo) error {
	b, _ := json.Marshal(info)
	return os.WriteFile(filepath.Join(serverDir, MetaDir, ExitFile), b, 0o600)
}
