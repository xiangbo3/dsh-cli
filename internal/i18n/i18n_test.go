// Built with AI-assisted development (Deepseek Harness)
// Copyright (C) 2026 xiangbo3

package i18n

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalize(t *testing.T) {
	cases := map[string]string{
		"zh":               "zh",
		"ZH":               "zh",
		"zh_CN":            "zh",
		"zh_CN.UTF-8":      "zh",
		"zh-Hans-CN":       "zh",
		"chinese":          "zh",
		"cn":               "zh",
		"中文":               "zh",
		"en":               "en",
		"en_US.UTF-8":      "en",
		"english":          "en",
		"fr_FR.ISO-8859-1": "fr",
		"c":                "",
		"C.UTF-8":          "",
		"POSIX":            "",
		"":                 "",
		"1x":               "",
		"abcde":            "",
	}
	for in, want := range cases {
		if got := Normalize(in); got != want {
			t.Errorf("Normalize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDetect(t *testing.T) {
	cases := map[string]string{
		"zh_CN.UTF-8": "zh",
		"zh":          "zh",
		"en_US":       "en",
		"":            "",
	}
	for lang, want := range cases {
		t.Setenv("LC_ALL", "")
		t.Setenv("LC_MESSAGES", "")
		t.Setenv("LANG", lang)
		if got := Detect(); got != want {
			t.Errorf("Detect with LANG=%q = %q, want %q", lang, got, want)
		}
	}
	// LC_ALL and LC_MESSAGES outrank LANG (POSIX precedence).
	t.Setenv("LC_ALL", "fr_FR.UTF-8")
	t.Setenv("LANG", "zh_CN.UTF-8")
	if got := Detect(); got != "fr" {
		t.Errorf("Detect with LC_ALL=fr = %q, want fr", got)
	}
	t.Setenv("LC_ALL", "")
	t.Setenv("LC_MESSAGES", "de_DE.UTF-8")
	if got := Detect(); got != "de" {
		t.Errorf("Detect with LC_MESSAGES=de = %q, want de", got)
	}
	t.Setenv("LC_MESSAGES", "")
	// C/POSIX fall through to LANG.
	t.Setenv("LC_ALL", "C.UTF-8")
	t.Setenv("LANG", "zh_CN.UTF-8")
	if got := Detect(); got != "zh" {
		t.Errorf("Detect with LC_ALL=C, LANG=zh = %q, want zh", got)
	}
}

func TestLoadAndFallback(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DSH_LOCALES", dir)
	t.Setenv("HOME", dir) // isolate the ~/.dsh-cli/locales storage dir
	// en needs no file: no stored en.json yet, so the built-in table.
	l, path, err := Load("english")
	if err != nil || path != "" || l.Lang != "en" {
		t.Fatalf("Load(english) = %v %q, %v", l, path, err)
	}
	// A catalog file loads and layers over the English table: keys the
	// file covers are its, uncovered keys fall back to English.
	err = os.WriteFile(filepath.Join(dir, "zh.json"),
		[]byte(`{"no.session":"（无会话）","turn.maxtokens":"最大 token"}`), 0o644)
	if err != nil {
		t.Fatal(err)
	}
	l, path, err = Load("zh_CN.UTF-8")
	if err != nil {
		t.Fatalf("Load(zh) = %v", err)
	}
	if l.Lang != "zh" || filepath.Base(path) != "zh.json" {
		t.Fatalf("Load(zh) lang/path = %v %q", l.Lang, path)
	}
	if got := l.T("no.session"); got != "（无会话）" {
		t.Errorf("T(no.session) = %q", got)
	}
	if got := l.T("top.idle"); got != "· idle" {
		t.Errorf("uncovered key did not fall back to English: %q", got)
	}
	if got := l.T("dock.goal.line", "plan", 1, 3); got != "phase: plan · rounds: 1/3" {
		t.Errorf("formatted fallback = %q", got)
	}
	// Unknown keys print themselves (a visible gap, not a silent one).
	if got := l.T("no.such.key"); got != "no.such.key" {
		t.Errorf("T(unknown) = %q", got)
	}
	// The stored en.json is the English face: when it loads, Load(en)
	// returns the file's copy; keys the file doesn't cover still fall
	// back to the built-in table.
	store := filepath.Join(dir, ".dsh-cli", "locales")
	if err := os.MkdirAll(store, 0o755); err != nil {
		t.Fatal(err)
	}
	err = os.WriteFile(filepath.Join(store, "en.json"),
		[]byte(`{"no.session":"(no sess.)"}`), 0o644)
	if err != nil {
		t.Fatal(err)
	}
	l, path, err = Load("en")
	if err != nil || filepath.Base(path) != "en.json" || l.Lang != "en" {
		t.Fatalf("Load(en) = %v %q, %v", l, path, err)
	}
	if got := l.T("no.session"); got != "(no sess.)" {
		t.Errorf("T(en, no.session) = %q, want the stored value", got)
	}
	if got := l.T("top.idle"); got != "· idle" {
		t.Errorf("uncovered key did not fall back to the built-in: %q", got)
	}
	// A bad catalog is a load error, not a half-locale.
	if err = os.WriteFile(filepath.Join(dir, "fr.json"), []byte(`{broken`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err = Load("fr"); err == nil {
		t.Error("Load(fr) on a broken catalog succeeded")
	}
	// A language with no file anywhere fails.
	if _, _, err := Load("de"); err == nil || !strings.Contains(err.Error(), "no catalog") {
		t.Errorf("Load(de) = %v, want a missing-catalog error", err)
	}
}

func TestVerbTrimming(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DSH_LOCALES", dir)
	t.Setenv("HOME", dir)
	// The English template carries a plural verb the Chinese one drops:
	// the extra arg must not print "%!(EXTRA ...)".
	err := os.WriteFile(filepath.Join(dir, "zh.json"),
		[]byte(`{"ws.row.meta":"%s · %d 个会话"}`), 0o644)
	if err != nil {
		t.Fatal(err)
	}
	l, _, _ := Load("zh")
	if got := l.T("ws.row.meta", "repo", 2, "s"); got != "repo · 2 个会话" {
		t.Errorf("T with trimmed args = %q", got)
	}
	if got := l.T("ws.row.meta", "repo", 1, ""); got != "repo · 1 个会话" {
		t.Errorf("T singular = %q", got)
	}
}

func TestAvailable(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DSH_LOCALES", dir)
	t.Setenv("HOME", dir)
	for _, f := range []string{"zh.json", "fr.json"} {
		if err := os.WriteFile(filepath.Join(dir, f), []byte(`{}`), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "ignore.txt"), []byte(`x`), 0o644); err != nil {
		t.Fatal(err)
	}
	got := Available()
	if len(got) != 3 || got[0] != "en" || got[1] != "fr" || got[2] != "zh" {
		t.Errorf("Available() = %v", got)
	}
}

func TestDetectAndLoadBootPath(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DSH_LOCALES", dir)
	t.Setenv("HOME", dir)
	t.Setenv("DSH_CLI_HOME", t.TempDir()) // no config preference in this test
	if err := os.WriteFile(filepath.Join(dir, "zh.json"), []byte(`{"top.idle":"· 空闲"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LC_ALL", "")
	t.Setenv("LC_MESSAGES", "")
	t.Setenv("LANG", "zh_CN.UTF-8")
	l := DetectAndLoad()
	if l.Lang != "zh" || l.T("top.idle") != "· 空闲" {
		t.Errorf("DetectAndLoad = %v %q", l.Lang, l.T("top.idle"))
	}
	// A zh locale whose catalog file is missing degrades to the
	// built-in Chinese face (like English, it carries a built-in table).
	t.Setenv("DSH_LOCALES", filepath.Join(dir, "empty"))
	if l := DetectAndLoad(); l.Lang != "zh" {
		t.Errorf("DetectAndLoad without catalog = %v, want zh (built-in face)", l.Lang)
	} else if got := l.T("top.idle"); got != "· 空闲" {
		t.Errorf("built-in zh face top.idle = %q", got)
	}
}

// TestLoadDefaultPrecedence pins the startup language ladder: the stored
// preference (config.json "language") beats the locale environment; a
// preference that does not load (unknown code, malformed file) degrades
// to the environment; no preference and no environment: built-in English.
func TestLoadDefaultPrecedence(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("DSH_LOCALES", t.TempDir())
	cfg := t.TempDir()
	t.Setenv("DSH_CLI_HOME", cfg)
	t.Setenv("LC_ALL", "")
	t.Setenv("LC_MESSAGES", "")
	t.Setenv("LANG", "zh_CN.UTF-8")
	writeCfg := func(s string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(cfg, "config.json"), []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	// No config file: the locale environment decides.
	if l := LoadDefault(); l.Lang != "zh" {
		t.Fatalf("no config: LoadDefault = %v, want zh (environment)", l.Lang)
	}
	// A stored preference beats the environment.
	writeCfg(`{"language": "en"}`)
	if l := LoadDefault(); l.Lang != "en" {
		t.Fatalf("config en: LoadDefault = %v, want en (preference wins)", l.Lang)
	}
	// A preference that does not load degrades to the environment.
	writeCfg(`{"language": "xx"}`)
	if l := LoadDefault(); l.Lang != "zh" {
		t.Fatalf("config xx: LoadDefault = %v, want zh (environment fallback)", l.Lang)
	}
	// A malformed file is the same as no file (Load's silent contract).
	writeCfg(`{not json`)
	if l := LoadDefault(); l.Lang != "zh" {
		t.Fatalf("malformed config: LoadDefault = %v, want zh (environment)", l.Lang)
	}
	// No preference and no environment: the built-in English.
	writeCfg(`{}`)
	t.Setenv("LANG", "C")
	if l := LoadDefault(); l.Lang != "en" {
		t.Fatalf("no config, no env: LoadDefault = %v, want en (built-in)", l.Lang)
	}
}

// TestBoot pins the startup seeding and version sync: a fresh home gets
// the storage dir with en.json and zh.json generated from the built-in
// tables (a search-dir catalog is NOT the seed source); a user edit
// within the same program version survives a later Boot; a file stamped
// by another version (or unmarked) is regenerated.
func TestBoot(t *testing.T) {
	home := t.TempDir()
	src := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("DSH_LOCALES", src)
	// An unrelated catalog in a search dir: Boot must seed from the
	// built-in tables, not copy this one.
	zhShipped := []byte(`{"top.idle":"· 空闲","no.session":"（无会话）"}`)
	if err := os.WriteFile(filepath.Join(src, "zh.json"), zhShipped, 0o644); err != nil {
		t.Fatal(err)
	}
	store := filepath.Join(home, ".dsh-cli", "locales")
	Boot()
	enB, err := os.ReadFile(filepath.Join(store, "en.json"))
	if err != nil {
		t.Fatalf("Boot did not generate en.json: %v", err)
	}
	zhB, err := os.ReadFile(filepath.Join(store, "zh.json"))
	if err != nil {
		t.Fatalf("Boot did not generate zh.json: %v", err)
	}
	// The generated en.json mirrors the built-in table exactly (same
	// format as the locales/en.json template, TestDumpENFile).
	want, err := catalogFile(en)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(enB, want) {
		t.Errorf("generated en.json is not the built-in table dump (version marker included)")
	}
	// The generated zh.json mirrors the built-in Chinese table exactly
	// (same format as the locales/zh.json template, TestDumpZHFile) —
	// NOT the unrelated catalog that happens to sit in a search dir.
	wantZH, err := catalogFile(zh)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(zhB, wantZH) {
		t.Errorf("generated zh.json is not the built-in table dump (version marker included)")
	}
	var zhMap map[string]string
	if err := json.Unmarshal(zhB, &zhMap); err != nil {
		t.Fatalf("stored zh.json does not parse: %v", err)
	}
	if zhMap["top.idle"] != "· 空闲" {
		t.Errorf("stored zh.json top.idle = %q", zhMap["top.idle"])
	}
	// The seeded English face loads from the file, not the built-in.
	l, path, err := Load("en")
	if err != nil || filepath.Base(path) != "en.json" {
		t.Fatalf("Load(en) after Boot = %v %q, %v", l, path, err)
	}
	if got := l.T("top.idle"); got != "· idle" {
		t.Errorf("booted English face = %q", got)
	}
	// A user edit to en.json survives a later Boot (the version marker
	// stays in the file) and becomes the face.
	doc := catalogDoc(en)
	doc["no.session"] = "(my edit)"
	user, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	user = append(user, '\n')
	if err := os.WriteFile(filepath.Join(store, "en.json"), user, 0o644); err != nil {
		t.Fatal(err)
	}
	Boot()
	b, err := os.ReadFile(filepath.Join(store, "en.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(b, user) {
		t.Errorf("Boot clobbered a user-edited en.json: %s", b)
	}
	if l, _, err := Load("en"); err != nil || l.T("no.session") != "(my edit)" {
		t.Errorf("Load(en) after edit = %v %q, %v", l, l.T("no.session"), err)
	}
	// A catalog stamped by an older dsh-cli (or one the marker was lost
	// from) is regenerated: the current version lands, stale values go.
	stale := map[string]string{versionKey: "0.0.1", "no.session": "(old build)"}
	staleB, err := json.MarshalIndent(stale, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(store, "en.json"), append(staleB, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	Boot()
	b, err = os.ReadFile(filepath.Join(store, "en.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(b, want) {
		t.Errorf("stale-version en.json was not regenerated: %s", b)
	}
}

// TestMissingKeys pins the locale-lag detection: a catalog behind the
// built-in table reports exactly the uncovered keys; a current table
// reports zero.
func TestMissingKeys(t *testing.T) {
	if got := English().MissingKeys(); got != 0 {
		t.Fatalf("English().MissingKeys() = %d, want 0", got)
	}
	l := &Locale{Lang: "xx", msgs: map[string]string{}}
	if got := l.MissingKeys(); got != len(en) {
		t.Fatalf("empty catalog MissingKeys() = %d, want %d", got, len(en))
	}
	one := &Locale{Lang: "xx", msgs: map[string]string{}}
	k := ""
	for key := range en {
		k = key
		break
	}
	one.msgs[k] = "x"
	if got := one.MissingKeys(); got != len(en)-1 {
		t.Fatalf("one-key catalog MissingKeys() = %d, want %d", got, len(en)-1)
	}
}
