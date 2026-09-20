// Built with AI-assisted development (Deepseek Harness)
// Copyright (C) 2026 xiangbo3

// Package modes is the dsh-cli face of the deployment's mode (agent preset)
// roster: the four modes a DSH deployment ships (Standard, PTC, Minimal,
// Creator), their canonical display copy, and the name resolution that
// /mode and the one-shot --preset flag use.
//
// The deployment's preset files may publish their own names (often localized);
// for the four shipped ids the client uses its own canonical English copy,
// the same rule the web client's preset display follows. User-authored
// presets keep whatever name they published (or fall back to the id).
package modes

import (
	"strings"

	"dsh-cli/internal/protocol"
)

// Info is the canonical copy for one shipped mode id.
type Info struct {
	ID    string
	Name  string
	Alias string
	Desc  string
}

// BuiltIn lists the modes every DSH deployment ships, in the deployment's
// own order. Alias is the short name users type: ptc names the code mode
// (its Code Mode SDK), creator names cordis (its identity).
var BuiltIn = []Info{
	{
		ID:    "standard",
		Name:  "Standard mode",
		Alias: "standard",
		Desc:  "Full coding agent with file editing, shell, file and web search, skills, planning, goals, subagents, and workflows.",
	},
	{
		ID:    "ptc",
		Name:  "PTC mode",
		Alias: "ptc",
		Desc:  "All Standard mode capabilities, with tools exposed through the Code Mode SDK so the model can combine multi-step operations in one TypeScript program.",
	},
	{
		ID:    "minimal",
		Name:  "Minimal mode",
		Alias: "minimal",
		Desc:  "Two-tool coding agent with persistent bash and str_replace_editor.",
	},
	{
		ID:    "cordis",
		Name:  "Creator mode",
		Alias: "creator",
		Desc:  "Built for creating custom agent presets, with all Standard mode capabilities plus runtime inspection, plugin experiments, and preset-authoring guidance.",
	},
}

var byID = func() map[string]Info {
	m := make(map[string]Info, len(BuiltIn))
	for _, inf := range BuiltIn {
		m[inf.ID] = inf
	}
	return m
}()

var byAlias = func() map[string]Info {
	m := make(map[string]Info, len(BuiltIn)*2)
	for _, inf := range BuiltIn {
		m[inf.Alias] = inf
		m[inf.ID] = inf
		m[strings.ToLower(inf.Name)] = inf
	}
	return m
}()

// InfoOf is the canonical copy for a shipped mode id ("", false for others).
func InfoOf(id string) (Info, bool) {
	inf, ok := byID[id]
	return inf, ok
}

// Label renders the short display name for one preset id: the canonical
// name for a shipped mode, the id itself otherwise.
func Label(id string) string {
	if inf, ok := byID[id]; ok {
		return inf.Name
	}
	return id
}

// Short is the table form of Label for narrow columns: the alias for a
// shipped mode (standard / ptc / minimal / creator), the id otherwise.
func Short(id string) string {
	if inf, ok := byID[id]; ok {
		return inf.Alias
	}
	return id
}

// Name renders the display name for one roster row: the canonical copy for
// a shipped mode, the published name (or the id) for everything else.
func Name(p protocol.AgentPresetEntry) string {
	if p.Trust == "system" {
		if inf, ok := byID[p.Id]; ok {
			return inf.Name
		}
	}
	if p.Name != "" {
		return p.Name
	}
	return p.Id
}

// Description is the one-line copy for a roster row, same fallback chain.
func Description(p protocol.AgentPresetEntry) string {
	if p.Trust == "system" {
		if inf, ok := byID[p.Id]; ok {
			return inf.Desc
		}
	}
	return p.Description
}

// StaticID maps a typed name to a shipped preset id without a roster: the
// aliases and canonical names (case-insensitive, "X mode" forms included).
// Unknown names pass through unchanged so the host can resolve them.
func StaticID(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	if inf, ok := byAlias[strings.ToLower(name)]; ok {
		return inf.ID
	}
	if inf, ok := byAlias[stripMode(strings.ToLower(name))]; ok {
		return inf.ID
	}
	return name
}

// stripMode drops a trailing "mode" word ("ptc mode" -> "ptc").
func stripMode(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if i := strings.LastIndex(s, "mode"); i+len("mode") == len(s) {
		s = strings.TrimRight(s[:i], " -")
	}
	return s
}

// Resolve finds one roster row by the name the user typed: a shipped-mode
// alias (ptc, creator, …), a canonical or published name, an exact id, a
// case-insensitive id, or a unique id prefix. The second return is a
// user-facing reason when nothing matched (ambiguity lists the candidates).
func Resolve(presets []protocol.AgentPresetEntry, name string) (*protocol.AgentPresetEntry, string) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, "empty mode name"
	}
	lower := strings.ToLower(name)
	find := func(pred func(p *protocol.AgentPresetEntry) bool) (*protocol.AgentPresetEntry, bool, string) {
		var hit *protocol.AgentPresetEntry
		n := 0
		for i := range presets {
			if pred(&presets[i]) {
				if n > 0 {
					return nil, false, ""
				}
				hit, n = &presets[i], 1
			}
		}
		return hit, n == 1, ""
	}
	list := func(pred func(p *protocol.AgentPresetEntry) bool) string {
		var out []string
		for i := range presets {
			if pred(&presets[i]) {
				out = append(out, presets[i].Id)
			}
		}
		return strings.Join(out, ", ")
	}
	// Shipped-mode aliases and canonical names (roster order preserved:
	// the deployment may not supply every shipped id).
	if inf, ok := byAlias[lower]; ok {
		if h, _, _ := find(func(p *protocol.AgentPresetEntry) bool { return p.Id == inf.ID }); h != nil {
			return h, ""
		}
	}
	if h, ok, _ := find(func(p *protocol.AgentPresetEntry) bool { return strings.ToLower(p.Id) == lower }); ok {
		return h, ""
	}
	if h, ok, _ := find(func(p *protocol.AgentPresetEntry) bool {
		return p.Name != "" && strings.ToLower(p.Name) == lower
	}); ok {
		return h, ""
	}
	if h, ok, _ := find(func(p *protocol.AgentPresetEntry) bool {
		return p.Name != "" && strings.ToLower(p.Name) == stripMode(lower)
	}); ok {
		return h, ""
	}
	if len(name) >= 3 {
		if h, ok, _ := find(func(p *protocol.AgentPresetEntry) bool {
			return strings.HasPrefix(strings.ToLower(p.Id), lower)
		}); ok {
			return h, ""
		}
	}
	if hits := list(func(p *protocol.AgentPresetEntry) bool {
		return strings.Contains(strings.ToLower(p.Id), lower)
	}); hits != "" {
		return nil, "matches " + hits + " — be more specific"
	}
	return nil, "no such mode (try /mode to browse)"
}
