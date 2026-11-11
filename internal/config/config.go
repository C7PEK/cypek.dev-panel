package config

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

const (
	DefaultPort    = 8420
	DefaultPath    = "/etc/homepanel/config.toml"
	DefaultDataDir = "/var/lib/homepanel"
	DefaultLogDir  = "/var/log/homepanel"
	DevDataDir     = ".devdata"
	secretByteLen  = 32
	configFileMode = 0o600
	configDirMode  = 0o750
)

type Config struct {
	Listen string `toml:"listen"`

	DataDir string `toml:"data_dir"`

	LogDir string `toml:"log_dir"`

	SessionSecret string `toml:"session_secret"`

	EncryptionKey string `toml:"encryption_key"`

	Supervisor string `toml:"supervisor"`

	MaxUploadMB int `toml:"max_upload_mb"`

	BackupsDirOverride string `toml:"backups_dir"`

	TLS TLSConfig `toml:"tls"`

	SFTP SFTPConfig `toml:"sftp"`

	Dev bool `toml:"-"`

	Path string `toml:"-"`
}

type TLSConfig struct {
	Enabled  bool   `toml:"enabled"`
	CertFile string `toml:"cert_file"`
	KeyFile  string `toml:"key_file"`

	SelfSigned bool `toml:"self_signed"`
}

type SFTPConfig struct {
	Enabled bool   `toml:"enabled"`
	Listen  string `toml:"listen"`
}

const DefaultSFTPPort = 2022

func Default() *Config {
	return &Config{
		Listen:  fmt.Sprintf(":%d", DefaultPort),
		DataDir: DefaultDataDir,
		LogDir:  DefaultLogDir,
		SFTP:    SFTPConfig{Enabled: true, Listen: fmt.Sprintf(":%d", DefaultSFTPPort)},
	}
}

func Load(path string) (*Config, error) {
	cfg := Default()
	meta, err := toml.DecodeFile(path, cfg)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}
	if undecoded := meta.Undecoded(); len(undecoded) > 0 {
		return nil, fmt.Errorf("config %s: unknown keys: %v", path, undecoded)
	}
	cfg.Path = path
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("config %s: %w", path, err)
	}
	return cfg, nil
}

func LoadOrCreate(path string, base *Config) (*Config, error) {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		if err := Generate(path, base); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, fmt.Errorf("stat config %s: %w", path, err)
	}
	return Load(path)
}

func Generate(path string, base *Config) error {
	if base == nil {
		base = Default()
	}
	cfg := *base
	var err error
	if cfg.SessionSecret == "" {
		if cfg.SessionSecret, err = randomSecret(); err != nil {
			return err
		}
	}
	if cfg.EncryptionKey == "" {
		if cfg.EncryptionKey, err = randomSecret(); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), configDirMode); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, configFileMode)
	if err != nil {
		return fmt.Errorf("create config %s: %w", path, err)
	}
	defer f.Close()
	if _, err := f.WriteString("# HomePanel configuration. Secrets below are generated once; keep this file private.\n\n"); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	if err := toml.NewEncoder(f).Encode(&cfg); err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	return nil
}

func (c *Config) Validate() error {
	if c.Listen == "" {
		return errors.New("listen must not be empty")
	}
	if c.DataDir == "" {
		return errors.New("data_dir must not be empty")
	}
	if err := checkSecret("session_secret", c.SessionSecret); err != nil {
		return err
	}
	if err := checkSecret("encryption_key", c.EncryptionKey); err != nil {
		return err
	}
	if c.Supervisor == "" {
		c.Supervisor = "systemd"
	}
	if c.MaxUploadMB <= 0 {
		c.MaxUploadMB = 1024
	}
	if c.Supervisor != "systemd" && c.Supervisor != "local" {
		return fmt.Errorf("supervisor must be \"systemd\" or \"local\", got %q", c.Supervisor)
	}
	if c.TLS.Enabled && !c.TLS.SelfSigned && (c.TLS.CertFile == "" || c.TLS.KeyFile == "") {
		return errors.New("tls.cert_file and tls.key_file are required when tls.enabled = true (or set tls.self_signed = true)")
	}
	return nil
}

func (c *Config) SecretKeyBytes() ([]byte, error) {
	return base64.StdEncoding.DecodeString(c.EncryptionKey)
}

func (c *Config) DBPath() string { return filepath.Join(c.DataDir, "panel.db") }

func (c *Config) ServersDir() string { return filepath.Join(c.DataDir, "servers") }

func (c *Config) RuntimesDir() string { return filepath.Join(c.DataDir, "runtimes") }

func (c *Config) BackupsDir() string {
	if c.BackupsDirOverride != "" {
		return c.BackupsDirOverride
	}
	return filepath.Join(c.DataDir, "backups")
}

func (c *Config) UserTemplatesDir() string { return filepath.Join(c.DataDir, "templates") }

func (c *Config) LocalRunDir() string { return filepath.Join(c.DataDir, "run") }

func (c *Config) EnsureDirs() error {
	dirs := []string{c.DataDir, c.ServersDir(), c.RuntimesDir(), c.BackupsDir(), c.UserTemplatesDir()}
	if c.Supervisor == "local" {
		dirs = append(dirs, c.LocalRunDir())
	}
	if c.LogDir != "" {
		dirs = append(dirs, c.LogDir)
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, configDirMode); err != nil {
			return fmt.Errorf("create dir %s: %w", d, err)
		}
	}
	return nil
}

func checkSecret(name, v string) error {
	if v == "" {
		return fmt.Errorf("%s must not be empty (delete the file to regenerate it)", name)
	}
	raw, err := base64.StdEncoding.DecodeString(v)
	if err != nil || len(raw) < 16 {
		return fmt.Errorf("%s must be base64 of at least 16 random bytes", name)
	}
	return nil
}

func randomSecret() (string, error) {
	b := make([]byte, secretByteLen)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate secret: %w", err)
	}
	return base64.StdEncoding.EncodeToString(b), nil
}
