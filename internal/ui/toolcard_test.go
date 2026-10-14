package ui

import (
	"strings"
	"testing"

	"dsh-cli/internal/core"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

func TestToolSummary(t *testing.T) {
	cases := []struct{ name, args, want string }{
		{"bash", `{"command":"ls -la"}`, "ls -la"},
		{"bash", `{"command":"echo a\necho b"}`, "echo a …"},
		{"read", `{"file_path":"a.go","offset":12}`, "a.go:12"},
		{"edit", `{"file_path":"b.go","old_string":"x"}`, "b.go"},
		{"grep", `{"pattern":"foo","path":"internal"}`, "foo  internal"},
		{"web_search", `{"queries":["a","b"]}`, "a · b"},
		{"web_fetch", `{"url":"https://x.dev"}`, "https://x.dev"},
		{"run_code", `{"code":"const x=1","description":"Count markers"}`, "Count markers"},
		{"subagent", `{"description":"Research X"}`, "Research X"},
		{"mystery", `{"a":1}`, `{"a":1}`},
		{"bash", `not json`, "not json"},
	}
	for _, c := range cases {
		if got := toolSummary(c.name, c.args); got != c.want {
			t.Errorf("toolSummary(%s) = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestLooksLikeDiffText(t *testing.T) {
	if looksLikeDiffText(strings.Split("total 3\n-rw-r--r-- x\ndrwxr-xr-x y", "\n")) {
		t.Error("ls listing must not read as a diff")
	}
	if !looksLikeDiffText(strings.Split("--- a\n+++ b\n@@ -1 +1 @@\n-old\n+new", "\n")) {
		t.Error("unified diff must read as a diff")
	}
	if !looksLikeDiffText(strings.Split("-a\n-b\n+c", "\n")) {
		t.Error("dominated signed lines must read as a diff")
	}
}

func TestHighlightLines(t *testing.T) {
	th := NewTheme()
	th.NoColor = false
	th.Profile = termenv.TrueColor
	lipgloss.SetColorProfile(termenv.TrueColor)
	code := "func f() int {\n\treturn 1 // one\n}"
	hl := highlightLines(th, code, "go")
	if len(hl) != 3 {
		t.Fatalf("lines = %d, want 3", len(hl))
	}
	if !strings.Contains(hl[0], "\x1b[") {
		t.Error("keyword line carries no color")
	}
	// Plain text and unknown languages must still come back line-aligned.
	plain := highlightLines(th, "hello\nworld", "")
	if len(plain) != 2 {
		t.Fatalf("fallback lines = %d", len(plain))
	}
	th.NoColor = true
	if got := highlightLines(th, code, "go"); got[0] != "func f() int {" {
		t.Error("NoColor must pass the code through unstyled")
	}
}

func TestRenderToolMetaShapes(t *testing.T) {
	th := NewTheme()
	th.Profile = termenv.TrueColor
	lipgloss.SetColorProfile(termenv.TrueColor)
	read := &core.ToolBlock{Name: "read", Done: true, Meta: []byte(
		`{"path":"a.go","totalLines":9,"lang":"go","lines":[{"number":1,"text":"package a"},{"number":2,"text":"var x = 1"}]}`)}
	out := renderToolMeta(th, read, 80)
	if len(out) != 2 || !strings.Contains(out[0], "1") || !strings.Contains(out[1], "2") {
		t.Fatalf("read card = %q", out)
	}
	diff := &core.ToolBlock{Name: "edit", Done: true, Meta: []byte(
		`{"diffs":[{"path":"a.go","oldText":"old","newText":"new\nmore"}]}`)}
	out = renderToolMeta(th, diff, 80)
	if len(out) != 4 { // path + "-" + 2x"+"
		t.Fatalf("diff card = %q", out)
	}
	search := &core.ToolBlock{Name: "grep", Done: true, Meta: []byte(
		`{"shape":"matches","total":2,"files":[{"path":"a.go","matches":[{"lineNumber":3,"line":"hit"}]}]}`)}
	out = renderToolMeta(th, search, 80)
	if len(out) != 2 {
		t.Fatalf("search card = %q", out)
	}
	// Unknown meta falls back to nil (the caller paints the plain result).
	other := &core.ToolBlock{Name: "bash", Done: true, Meta: []byte(`{"exitCode":2}`)}
	if out := renderToolMeta(th, other, 80); out != nil {
		t.Fatalf("unknown meta = %q, want nil", out)
	}
}
