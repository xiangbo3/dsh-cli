// Built with AI-assisted development (Deepseek Harness)
// Copyright (C) 2026 xiangbo3

// Package config reads the dsh-cli user configuration: the DSH server
// address lives in ~/.dsh-cli/config.json, alongside the program's other
// local data (locales, cache, usage stats). The file is optional — when
// it is absent or unparseable the built-in default applies, because a
// preference file is a convenience, never a source of boot errors.
package config

import (
	"encoding/json"
	"net"
	"net/url"
	"os"
	"path/filepath"

	"dsh-cli/internal/version"
)

// DefaultURL is the fallback server base (the loopback web server).
const DefaultURL = "http://127.0.0.1:3080"

// File is the config file name inside the data directory.
const File = "config.json"

// Config is the user configuration file shape.
type Config struct {
	// Version records which dsh-cli version owns the file (the _version
	// key): on a version mismatch Load completes the stale file with the
	// options this version knows (missing keys land at their zero value)
	// and re-stamps it, so an upgrade never loses user values — it only
	// fills the file in.
	Version string `json:"_version,omitempty"`
	// URL is the DSH web server base URL (http://host:port). Empty (or
	// omitted from the file): DefaultURL applies.
	URL string `json:"url,omitempty"`
	// Language is the default UI language (a catalog code: "zh", "en",
	// …, as accepted by the /language command). Empty (or omitted):
	// the UI follows the locale environment instead. /language switches
	// write the chosen face back here so the default survives restarts.
	Language string `json:"language,omitempty"`
	// Token is a launch token the web server printed at startup (the
	// ?token= credential). The cookie-gated web build exchanges it for
	// a session cookie on first contact; the token is kept so the
	// exchange can repeat when the cookie goes stale. Empty (or
	// omitted): no launch token — only an un-gated (old) build, or a
	// stored Cookie for the same base, can be reached.
	Token string `json:"token,omitempty"`
	// Cookie is the exchanged session cookie ("name=value"), valid
	// only for the base recorded in CookieFor.
	Cookie string `json:"cookie,omitempty"`
	// CookieFor is the server base the stored cookie was minted for
	// (a cookie binds to the host authority that minted it; another
	// base needs a fresh exchange).
	CookieFor string `json:"cookieFor,omitempty"`
	// TokenInPrice / TokenOutPrice are the per-one-million-tokens costs
	// for input / output tokens (the /status billing section's editable
	// fields, applied live; the currency is implied). Zero: no cost
	// readout anywhere.
	TokenInPrice  float64 `json:"tokenInPrice,omitempty"`
	TokenOutPrice float64 `json:"tokenOutPrice,omitempty"`
	// TokenPrice is the pre-split single price (migrated to the input
	// price on load; dropped from the file on the next save).
	TokenPrice float64 `json:"tokenPrice,omitempty"`
}

// DataDir is the program's data root: ~/.dsh-cli, or the DSH_CLI_HOME env
// var (the test suite points it at a temp dir to stay hermetic against
// the user's real config). Config, locales, cache, and the auto-started
// dsh web's log live here.
func DataDir() (string, error) {
	if root := os.Getenv("DSH_CLI_HOME"); root != "" {
		return root, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".dsh-cli"), nil
}

func path() (string, error) {
	root, err := DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, File), nil
}

// Load reads the configuration file; a missing or unparseable file yields
// an empty config. A parseable file stamped by another dsh-cli version is
// completed with the options this version knows (missing keys at their
// zero value) and re-stamped before it is returned. A syntactically valid
// but wrong URL is not Load's concern — the caller's URL validation
// reports it with a clear message.
func Load() Config {
	p, err := path()
	if err != nil {
		return Config{}
	}
	raw, err := os.ReadFile(p)
	if err != nil {
		return Config{}
	}
	var c Config
	if json.Unmarshal(raw, &c) != nil {
		return Config{}
	}
	// Pre-split price files (one tokenPrice for everything): carry it
	// over to the input price and drop the legacy key.
	if c.TokenPrice > 0 {
		if c.TokenInPrice <= 0 {
			c.TokenInPrice = c.TokenPrice
		}
		c.TokenPrice = 0
	}
	// A stale file (written by another dsh-cli version, or unmarked from
	// before the marker) is completed with the options this version
	// knows and re-stamped: user values are kept, missing keys land at
	// their zero value. Best-effort — a read-only home degrades to the
	// in-memory config, as with any config failure here.
	if c.Version != version.Version {
		c.Version = version.Version
		Save(c)
	}
	return c
}

// ResolveURL picks the server base URL by precedence: an explicit value
// (the --url flag or the DSH_URL environment variable, already merged by
// the caller) wins, then the configuration file, then DefaultURL.
func ResolveURL(explicit string) string {
	if explicit != "" {
		return explicit
	}
	if c := Load(); c.URL != "" {
		return c.URL
	}
	return DefaultURL
}

// Save writes the configuration file atomically (temp + rename),
// creating the data directory when absent. Callers pass the FULL config
// (Load, mutate, Save) so a value the file does not yet have is not lost
// to the write. The file is a convenience: callers that must not boot on
// a write failure treat the returned error as best-effort.
func Save(c Config) error {
	c.Version = version.Version // the file is owned by the program that writes it
	p, err := path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, p); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// PlainHTTP reports whether the server base sends traffic in the clear:
// the http scheme on a non-loopback host (http on localhost/127.0.0.1/::1
// and https anywhere do not). Unparseable or host-less values are not
// plain-http — URL validation elsewhere reports those.
func PlainHTTP(u string) bool {
	p, err := url.Parse(u)
	if err != nil || p.Host == "" || p.Scheme != "http" {
		return false
	}
	h := p.Hostname()
	if h == "localhost" {
		return false
	}
	ip := net.ParseIP(h)
	return !(ip != nil && ip.IsLoopback())
}
