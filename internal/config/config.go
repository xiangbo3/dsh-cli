// Package config reads the dsh-cli user configuration: the DSH server
// address lives in ~/.dsh-cli/config.json, alongside the program's other
// local data (locales, cache, usage stats). The file is optional — when
// it is absent or unparseable the built-in default applies, because a
// preference file is a convenience, never a source of boot errors.
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// DefaultURL is the fallback server base (the loopback web server).
const DefaultURL = "http://127.0.0.1:3080"

// File is the config file name inside the data directory.
const File = "config.json"

// Config is the user configuration file shape.
type Config struct {
	// URL is the DSH web server base URL (http://host:port). Empty (or
	// omitted from the file): DefaultURL applies.
	URL string `json:"url,omitempty"`
	// Language is the default UI language (a catalog code: "zh", "en",
	// …, as accepted by the /language command). Empty (or omitted):
	// the UI follows the locale environment instead. /language switches
	// write the chosen face back here so the default survives restarts.
	Language string `json:"language,omitempty"`
}

// path resolves the on-disk location; the DSH_CLI_HOME env var replaces
// the data directory (the test suite points it at a temp dir to stay
// hermetic against the user's real config).
func path() (string, error) {
	root := os.Getenv("DSH_CLI_HOME")
	if root == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		root = filepath.Join(home, ".dsh-cli")
	}
	return filepath.Join(root, File), nil
}

// Load reads the configuration file; a missing or unparseable file yields
// an empty config. A syntactically valid but wrong URL is not Load's
// concern — the caller's URL validation reports it with a clear message.
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

// Save writes the configuration file, creating the data directory when
// absent. Callers pass the FULL config (Load, mutate, Save) so a value
// the file does not yet have is not lost to the write. The file is a
// convenience: callers that must not boot on a write failure treat the
// returned error as best-effort.
func Save(c Config) error {
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
	return os.WriteFile(p, append(b, '\n'), 0o644)
}
