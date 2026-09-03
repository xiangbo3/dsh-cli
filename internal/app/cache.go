// Boot cache: the previous boot's roster + workspace registry, persisted
// to ~/.dsh-cli so the NEXT boot's chrome (top bar title, status bar
// workspace chip, session window) starts warm instead of waiting for
// session.list — expensive on hosts with large session counts, since the
// host recomputes every session's projection block per call.
//
// Stale-while-revalidate: the cache installs at Start (before the live
// baselines), then the live session.list / workspace.list re-baseline and
// overwrite row by row; the cache is re-persisted once the live baselines
// land, so the file tracks the last good state.
package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"dsh-cli/internal/core"
	"dsh-cli/internal/protocol"
)

const baselineCacheFile = "baseline-cache.json"

// baselineCacheV is the on-disk version; bump on shape change (the loader
// treats any other version as absent).
const baselineCacheV = 1

type baselineCache struct {
	V          int                      `json:"v"`
	Workspaces []protocol.WorkspaceView `json:"workspaces,omitempty"`
	Archived   []string                 `json:"archived,omitempty"`
	Sessions   []core.CacheRow          `json:"sessions,omitempty"`
}

// cacheMu serializes persists (baseline and reconnect re-baseline may
// both want to write).
var cacheMu sync.Mutex

// cachePath resolves the on-disk location: the boot cache lives in the
// program's data directory (~/.dsh-cli, alongside its other local data);
// the DSH_CLI_HOME env var replaces that directory (the test suite points
// it at a temp dir to stay hermetic against the user's real cache).
func cachePath() (string, error) {
	root := os.Getenv("DSH_CLI_HOME")
	if root == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		root = filepath.Join(home, ".dsh-cli")
	}
	return filepath.Join(root, baselineCacheFile), nil
}

// bootCacheEnabled reports whether the boot cache participates: the test
// binaries set DSH_CLI_NO_BOOT_CACHE=1 so fixture rows never load into —
// or persist out of — the shared test data dir (the cache is an
// optimization, never a source of test truth).
func bootCacheEnabled() bool {
	return os.Getenv("DSH_CLI_NO_BOOT_CACHE") == ""
}

// loadBaselineCache installs the previous boot's chrome into the store.
// Any failure (missing file, bad shape) is a silent no-op: the cache is
// an optimization, never a source of errors.
func (a *App) loadBaselineCache() {
	if !bootCacheEnabled() {
		return
	}
	p, err := cachePath()
	if err != nil {
		return
	}
	raw, err := os.ReadFile(p)
	if err != nil {
		return
	}
	var c baselineCache
	if err := json.Unmarshal(raw, &c); err != nil || c.V != baselineCacheV {
		return
	}
	a.st.CacheWorkspaces(c.Workspaces, c.Archived)
	a.st.CacheRoster(c.Sessions)
}

// persistBaselineCache writes the current roster + registry for the next
// boot. Skips when the live baselines have not both landed (the cache
// must not record a half-baselined state as "warm").
func (a *App) persistBaselineCache() {
	if !bootCacheEnabled() {
		return
	}
	if !a.st.RosterBaselined() || !a.st.WorkspacesBaselined() {
		return
	}
	rows, ws, archived := a.st.CacheData()
	c := baselineCache{V: baselineCacheV, Workspaces: ws, Archived: archived, Sessions: rows}
	cacheMu.Lock()
	defer cacheMu.Unlock()
	p, err := cachePath()
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return
	}
	b, err := json.Marshal(&c)
	if err != nil {
		return
	}
	// Atomic-ish replace: temp file in the same directory, then rename.
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return
	}
	_ = os.Rename(tmp, p)
}
