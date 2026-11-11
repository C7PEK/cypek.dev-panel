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
	CredsPath = "/etc/homepanel/database.json"

	InstallLog = "/var/lib/homepanel/database-install.log"

	InstallUnit = "homepanel-database-install.service"

	PhpMyAdminUnit = "homepanel-phpmyadmin.service"

	AdminUser = "homepanel_admin"

	NamePrefix     = "hp_"
	DefaultSocket  = "/run/mysqld/mysqld.sock"
	DefaultPMAPort = 8081
)

type Creds struct {
	Socket         string `json:"socket"`
	Host           string `json:"host"`
	Port           int    `json:"port"`
	AdminUser      string `json:"adminUser"`
	AdminPassword  string `json:"adminPassword"`
	PhpMyAdminPort int    `json:"phpMyAdminPort"`
	Version        string `json:"version,omitempty"`
}

var ErrNotInstalled = errors.New("the database add-on is not installed")

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

const MaxShortName = 24

func DatabaseName(short string) (string, error) {
	short = strings.ToLower(strings.TrimSpace(short))
	if !shortRe.MatchString(short) {
		return "", fmt.Errorf("name must be 1-24 characters: lowercase letters, digits, underscore, starting with a letter")
	}
	return NamePrefix + short, nil
}

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
	if len(out) > MaxShortName-3 {
		out = strings.TrimRight(out[:MaxShortName-3], "_")
	}
	if out == "" {
		out = "db"
	}
	return out
}

func ValidIdent(s string) bool { return identRe.MatchString(s) }

const pwAlphabet = "abcdefghijkmnopqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789"

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

func ValidPassword(p string) bool { return pwRe.MatchString(p) }
