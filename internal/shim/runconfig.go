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

type RunConfig struct {
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env,omitempty"`

	Stop StopConfig `json:"stop"`

	ReadyRegex string `json:"readyRegex,omitempty"`

	ErrorRegex string `json:"errorRegex,omitempty"`

	Cols uint16 `json:"cols,omitempty"`
	Rows uint16 `json:"rows,omitempty"`

	Dir string `json:"dir,omitempty"`

	Oneshot bool `json:"oneshot,omitempty"`
}

type StopConfig struct {
	Command    string `json:"command,omitempty"`
	TimeoutSec int    `json:"timeoutSec,omitempty"`
}

const (
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

func SaveRunConfig(serverDir string, cfg *RunConfig) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("encode run config: %w", err)
	}

	if err := safefs.WriteFile(serverDir, filepath.Join(MetaDir, RunFile), b, 0o600); err != nil {
		return fmt.Errorf("write run config: %w", err)
	}
	return nil
}

const maxMetaFile = 1 << 20

const MaxStopTimeoutSec = 165

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

type ExitInfo struct {
	ExitCode  int    `json:"exitCode"`
	ExitedAt  string `json:"exitedAt"`
	Requested bool   `json:"requested"`
	Signal    string `json:"signal,omitempty"`

	Error string `json:"error,omitempty"`
}

const ExitConfigError = 78

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
