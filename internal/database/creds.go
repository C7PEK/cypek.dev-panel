// Package database manages the optional MariaDB + phpMyAdmin add-on: the
// root-side installer (`homepanel install-database`, run by a oneshot
// systemd unit the panel may start) and the panel-side manager that creates
// one database and one database user per request for a game server.
package database

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"regexp"
	"strings"
)

const (
	// CredsPath holds the panel's MariaDB admin account (0640 root:homepanel).
	CredsPath = "/etc/homepanel/database.json"
	// InstallLog is written by the installer and read by the panel for progress.
	InstallLog = "/var/lib/homepanel/database-install.log"
	// InstallUnit is the oneshot unit the panel starts to install the add-on.
	InstallUnit = "homepanel-database-install.service"
	// PhpMyAdminUnit serves phpMyAdmin with PHP's built-in web server.
	PhpMyAdminUnit = "homepanel-phpmyadmin.service"
	// AdminUser is the MariaDB account the panel uses.
	AdminUser = "homepanel_admin"
	// NamePrefix scopes everything the panel creates; the admin account only
	// holds privileges on databases with this prefix.
	NamePrefix     = "hp_"
	DefaultSocket  = "/run/mysqld/mysqld.sock"
	DefaultPMAPort = 8081
)

// Creds is what the installer leaves for the panel.
type Creds struct {
	Socket         string `json:"socket"`
	Host           string `json:"host"` // for game servers (TCP)
	Port           int    `json:"port"`
	AdminUser      string `json:"adminUser"`
	AdminPassword  string `json:"adminPassword"`
	PhpMyAdminPort int    `json:"phpMyAdminPort"`
	Version        string `json:"version,omitempty"`
}

// ErrNotInstalled means the add-on has not been installed yet.
var ErrNotInstalled = errors.New("the database add-on is not installed")

// LoadCreds reads the credentials file.
func LoadCreds(path string) (*Creds, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotInstalled
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var c Creds
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if c.AdminUser == "" || c.AdminPassword == "" {
		return nil, fmt.Errorf("%s is incomplete", path)
	}
	if c.Socket == "" {
		c.Socket = DefaultSocket
	}
	if c.Host == "" {
		c.Host = "127.0.0.1"
	}
	if c.Port == 0 {
		c.Port = 3306
	}
	return &c, nil
}

var (
	identRe = regexp.MustCompile(`^[a-z0-9_]{1,64}$`)
	shortRe = regexp.MustCompile(`^[a-z][a-z0-9_]{0,23}$`)
)

// MaxShortName is the longest user-chosen part of a database name.
const MaxShortName = 24

// DatabaseName builds the database (and user) name: hp_<name>. Names are
// short and global, so they read well in phpMyAdmin and server.cfg; the
// panel tracks which server owns which database.
func DatabaseName(short string) (string, error) {
	short = strings.ToLower(strings.TrimSpace(short))
	if !shortRe.MatchString(short) {
		return "", fmt.Errorf("name must be 1-24 characters: lowercase letters, digits, underscore, starting with a letter")
	}
	return NamePrefix + short, nil
}

// Slug turns a server name into a default database name ("Mój ESX #2" -> "moj_esx_2").
func Slug(s string) string {
	repl := strings.NewReplacer("ą", "a", "ć", "c", "ę", "e", "ł", "l", "ń", "n", "ó", "o", "ś", "s", "ź", "z", "ż", "z")
	s = repl.Replace(strings.ToLower(s))
	var b strings.Builder
	lastUnderscore := true
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			b.WriteRune(r)
			lastUnderscore = false
		case !lastUnderscore:
			b.WriteByte('_')
			lastUnderscore = true
		}
	}
	out := strings.Trim(b.String(), "_")
	out = strings.TrimLeft(out, "0123456789_")
	if len(out) > MaxShortName-3 { // leave room for a _N suffix
		out = strings.TrimRight(out[:MaxShortName-3], "_")
	}
	if out == "" {
		out = "db"
	}
	return out
}

// ValidIdent guards every identifier interpolated into SQL (identifiers
// cannot be bound as parameters).
func ValidIdent(s string) bool { return identRe.MatchString(s) }

const pwAlphabet = "abcdefghijkmnopqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789"

// NewPassword returns a random alphanumeric password (safe in SQL literals,
// connection strings and server.cfg without escaping).
func NewPassword(n int) (string, error) {
	var b strings.Builder
	max := big.NewInt(int64(len(pwAlphabet)))
	for i := 0; i < n; i++ {
		k, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		b.WriteByte(pwAlphabet[k.Int64()])
	}
	return b.String(), nil
}

var pwRe = regexp.MustCompile(`^[A-Za-z0-9]{12,64}$`)

// ValidPassword accepts only what NewPassword produces.
func ValidPassword(p string) bool { return pwRe.MatchString(p) }
