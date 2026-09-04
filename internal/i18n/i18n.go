// Built with AI-assisted development (Deepseek Harness)
// Copyright (C) 2026 xiangbo3

// Package i18n is the dsh-cli UI's language layer: locale detection from
// the user's environment, loading of per-language UI catalogs (JSON
// config files), and string lookup that always falls back to the
// built-in English table — a missing or unreadable catalog degrades to
// the English face, never to a hole in the interface.
//
// Catalog storage lives in ~/.dsh-cli/locales (with the rest of the
// program's local data): at startup Boot generates en.json and zh.json
// there from the built-in English / Chinese tables when absent or when
// they carry another program's version marker (the stored files are the
// live, user-editable faces); catalogs the user adds for other
// languages are read from the SearchDirs paths (the storage dir among
// them).
package i18n

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"dsh-cli/internal/config"
	"dsh-cli/internal/version"
)

// Locale is one UI language: its catalog code and the message table it
// renders with. The table layers over the built-in English catalog (en),
// so a partial translation never leaves an untranslated gap: a key the
// file does not cover falls back to English, then to the key itself.
type Locale struct {
	Lang string
	msgs map[string]string
}

// English returns the built-in locale: the program's default face and
// the safety net of every other catalog (a partial or missing file never
// leaves it behind). The stored ~/.dsh-cli/locales/en.json overrides it
// as the English face when present (see Load); this table is also what
// Boot regenerates the file from on first run.
func English() *Locale { return &Locale{Lang: "en", msgs: en} }

// Name renders the locale's display name for UI messages ("English",
// "中文", the code title-cased for anything else).
func (l *Locale) Name() string {
	if l == nil {
		return "English"
	}
	return DisplayName(l.Lang)
}

// DisplayName renders the display name of one catalog code (the
// /language picker's rows and the Name face share it): "English",
// "中文", the code title-cased for anything else.
func DisplayName(code string) string {
	if n, ok := langNames[code]; ok {
		return n
	}
	if code == "en" {
		return "English"
	}
	if code == "" {
		return code
	}
	return strings.ToUpper(code[:1]) + code[1:]
}

var langNames = map[string]string{
	"en": "English",
	"zh": "中文",
}

// verbRe matches one Sprintf verb ("%%", "%% " and plain percents apart):
// the templates never use positional or star widths.
var verbRe = regexp.MustCompile(`%[-+ #0]*(\d+)?(\.\d+)?([a-zA-Z])`)

// countVerbs counts the Sprintf verbs a template consumes.
func countVerbs(t string) int { return len(verbRe.FindAllString(t, -1)) }

// T looks up key in the locale's table (then the built-in English
// table, then the raw key) and renders it with args (Sprintf semantics;
// an empty template means "hide the line"). A localized template may
// carry fewer verbs than the English one (no plural, optional clause):
// the trailing args beyond its verbs drop off instead of printing
// "%!(EXTRA ...)".
func (l *Locale) T(key string, args ...any) string {
	t := ""
	found := false
	if l != nil {
		if v, ok := l.msgs[key]; ok {
			t, found = v, true
		}
	}
	if !found {
		if v, ok := en[key]; ok {
			t, found = v, true
		}
	}
	if !found {
		return key
	}
	if t == "" || len(args) == 0 {
		return t
	}
	if n := countVerbs(t); n == 0 {
		return t
	} else if n < len(args) {
		args = args[:n]
	}
	return fmt.Sprintf(t, args...)
}

// MissingKeys counts the built-in English table's keys this locale's
// catalog does not cover: a stored locale file behind a newer build
// (those strings fall back to English, so the face reads mixed). 0 =
// current.
func (l *Locale) MissingKeys() int {
	n := 0
	for k := range en {
		if _, ok := l.msgs[k]; !ok {
			n++
		}
	}
	return n
}

// aliases maps the common non-standard spellings of a language name to
// its catalog code (/language arguments and locale values alike).
var aliases = map[string]string{
	"chinese": "zh",
	"cn":      "zh",
	"中文":      "zh",
	"english": "en",

	"英语": "en",
}

// Normalize maps a locale value or /language argument to a catalog code
// ("zh", "en", "fr", …), "" when unrecognized. It tolerates POSIX
// (zh_CN.UTF-8) and BCP47 (zh-Hans-CN) forms by keeping the language
// subtag, and the alias spellings.
func Normalize(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	if s == "" {
		return ""
	}
	if c, ok := aliases[s]; ok {
		return c
	}
	// zh_CN.UTF-8 / zh-CN-1909 / i-chinese: drop the encoding and
	// region/script subtags, keep the language part.
	if i := strings.IndexAny(s, "._-"); i >= 0 {
		s = s[:i]
	}
	if len(s) < 2 || len(s) > 3 {
		return ""
	}
	for _, r := range s {
		if r < 'a' || r > 'z' {
			return ""
		}
	}
	return s
}

// Detect reports the language code the user's locale environment points
// to ("" when unset or the generic C/POSIX locale). Precedence follows
// POSIX: LC_ALL, then LC_MESSAGES, then LANG.
func Detect() string {
	for _, k := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		v := strings.TrimSpace(os.Getenv(k))
		if v == "" {
			continue
		}
		c := Normalize(v)
		if c == "" || c == "c" {
			continue
		}
		return c
	}
	return ""
}

// Dir reports the locale storage directory: ~/.dsh-cli/locales ("" when
// the user home is unresolvable) — catalog storage lives with the rest
// of the program's local data under ~/.dsh-cli.
func Dir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".dsh-cli", "locales")
}

// SearchDirs lists the catalog search paths, in order: an explicit
// DSH_LOCALES directory override, the locale storage dir (seeded by
// Boot), the locales folder beside the executable (the install
// destination of the shipped catalogs), and the working directory
// (development checkouts).
func SearchDirs() []string {
	var out []string
	seen := map[string]bool{}
	add := func(d string) {
		if d == "" || seen[d] {
			return
		}
		seen[d] = true
		out = append(out, d)
	}
	if d := os.Getenv("DSH_LOCALES"); d != "" {
		add(d)
	}
	if d := Dir(); d != "" {
		add(d)
	}
	if exe, err := os.Executable(); err == nil {
		add(filepath.Join(filepath.Dir(exe), "locales"))
	}
	add(filepath.Join(".", "locales"))
	return out
}

// versionKey is the reserved catalog key that records which dsh-cli
// version generated a stored en.json / zh.json: on a version mismatch
// Boot regenerates the file from the built-in table, so the face always
// matches the program (a user edit keeps the marker and survives within
// one version; an upgrade reseeds).
const versionKey = "_version"

// catalogDoc is one built-in table tagged with its generating version.
func catalogDoc(table map[string]string) map[string]string {
	doc := make(map[string]string, len(table)+1)
	for k, v := range table {
		doc[k] = v
	}
	doc[versionKey] = version.Version
	return doc
}

// catalogFile renders one built-in table as the stored catalog bytes.
func catalogFile(table map[string]string) ([]byte, error) {
	b, err := json.MarshalIndent(catalogDoc(table), "", "  ")
	return append(b, '\n'), err
}

// fileVersion reads the generating version recorded in a stored catalog
// ("" when the file is absent, unparseable, or predates the marker).
func fileVersion(p string) string {
	b, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	var doc map[string]string
	if json.Unmarshal(b, &doc) != nil {
		return ""
	}
	return doc[versionKey]
}

// Boot prepares the locale storage dir for a run — the startup hook
// (idempotent, best-effort): it creates ~/.dsh-cli/locales and keeps
// en.json / zh.json in step with the program. A file stamped with this
// dsh-cli version (possibly user-edited since — the marker stays in the
// file) is kept as-is; a file from another version, or an unmarked one
// from an older build, is regenerated from the built-in table. Any
// failure (no home, read-only fs) degrades to the built-in face, never
// to a startup failure.
func Boot() {
	d := Dir()
	if d == "" {
		return
	}
	if err := os.MkdirAll(d, 0o755); err != nil {
		return
	}
	sync := func(name string, table map[string]string) {
		p := filepath.Join(d, name)
		if _, err := os.Stat(p); err == nil && fileVersion(p) == version.Version {
			return
		}
		if b, err := catalogFile(table); err == nil {
			os.WriteFile(p, b, 0o644) // best-effort
		}
	}
	sync("en.json", en)
	sync("zh.json", zh)
}

// Load builds the locale for lang from its catalog file (searched per
// SearchDirs; the full code's file first, then the two-letter base's),
// layered over the built-in English. English loads the stored
// ~/.dsh-cli/locales/en.json when present (Boot seeds it from the
// built-in table on first run, so a user edit there is the live English
// face), the built-in table otherwise; Chinese is the same (the stored
// file, then the built-in Chinese table) — a missing or broken file
// never breaks a face. The returned path identifies the catalog the
// locale came from ("" for the built-in).
func Load(lang string) (*Locale, string, error) {
	c := Normalize(lang)
	if c == "" {
		return nil, "", fmt.Errorf("unrecognized language %q", lang)
	}
	if c == "en" {
		// The English face: the stored ~/.dsh-cli/locales/en.json when
		// it loads (Boot generates it from the built-in table on first
		// run), the built-in table otherwise.
		if d := Dir(); d != "" {
			p := filepath.Join(d, "en.json")
			if b, err := os.ReadFile(p); err == nil {
				msgs := map[string]string{}
				if err := json.Unmarshal(b, &msgs); err == nil {
					return &Locale{Lang: "en", msgs: msgs}, p, nil
				}
			}
		}
		return English(), "", nil
	}
	codes := []string{c}
	if len(c) > 2 {
		codes = append(codes, c[:2])
	}
	for _, dir := range SearchDirs() {
		for _, code := range codes {
			p := filepath.Join(dir, code+".json")
			b, err := os.ReadFile(p)
			if err != nil {
				continue
			}
			msgs := map[string]string{}
			if err := json.Unmarshal(b, &msgs); err != nil {
				return nil, "", fmt.Errorf("bad catalog %s: %v", p, err)
			}
			return &Locale{Lang: code, msgs: msgs}, p, nil
		}
	}
	// Chinese, like English, has a built-in table as the final fallback:
	// a missing or unreadable file degrades to the built-in Chinese face,
	// not to English.
	if c == "zh" {
		return &Locale{Lang: "zh", msgs: zh}, "", nil
	}
	return nil, "", fmt.Errorf("no catalog for %q (looked in %s)", c, strings.Join(SearchDirs(), ", "))
}

// LoadDefault is the boot path: the stored preference (the "language"
// value of ~/.dsh-cli/config.json) when a catalog loads for it, then the
// user's locale environment, then the built-in English. A preference that
// does not load (unrecognized code, no catalog) degrades to the next
// candidate — a preference file is a convenience, never a source of boot
// errors.
func LoadDefault() *Locale {
	for _, c := range []string{config.Load().Language, Detect()} {
		if c == "" {
			continue
		}
		if l, _, err := Load(c); err == nil {
			return l
		}
	}
	return English()
}

// DetectAndLoad keeps the pre-preference boot contract for callers that
// only care about the environment: the environment's language when a
// catalog loads for it, the built-in English otherwise.
func DetectAndLoad() *Locale {
	c := Detect()
	if c == "" {
		return English()
	}
	l, _, err := Load(c)
	if err != nil {
		return English()
	}
	return l
}

// Available lists the catalog codes on disk plus the built-in "en" /
// "zh" (both faces exist without any file), sorted — the /language
// listing.
func Available() []string {
	set := map[string]bool{"en": true, "zh": true}
	for _, dir := range SearchDirs() {
		ents, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range ents {
			n := e.Name()
			if !strings.HasSuffix(strings.ToLower(n), ".json") {
				continue
			}
			base := n[:len(n)-len(".json")]
			if c := Normalize(base); c != "" && c != "en" {
				set[c] = true
			}
		}
	}
	out := make([]string, 0, len(set))
	for c := range set {
		out = append(out, c)
	}
	slices.Sort(out)
	return out
}
