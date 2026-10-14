// Built with AI-assisted development (Deepseek Harness)
// Copyright (C) 2026 xiangbo3

package ui

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"dsh-cli/internal/core"
	"dsh-cli/internal/textutil"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// ---- tool-call title ------------------------------------------------------

// toolSummary is the human one-liner for a tool card: the salient argument
// per tool (the command, the path, the pattern), not the raw JSON blob.
// It mirrors the web client's card titles; unknown tools keep the compact
// JSON so nothing is hidden.
func toolSummary(name, argsJSON string) string {
	if argsJSON == "" {
		return ""
	}
	var m map[string]json.RawMessage
	if json.Unmarshal([]byte(argsJSON), &m) != nil {
		return textutil.TruncateMiddle(strings.ReplaceAll(argsJSON, "\n", " "), 120)
	}
	str := func(keys ...string) string {
		for _, k := range keys {
			var s string
			if raw, ok := m[k]; ok && json.Unmarshal(raw, &s) == nil && s != "" {
				return s
			}
		}
		return ""
	}
	firstLine := func(s string) string {
		if i := strings.IndexByte(s, '\n'); i >= 0 {
			s = s[:i] + " …"
		}
		return textutil.Truncate(strings.TrimSpace(s), 120, "…")
	}
	switch name {
	case "bash", "pwsh", "exec":
		if c := str("command"); c != "" {
			return firstLine(c)
		}
	case "run_code":
		if d := str("description"); d != "" {
			return firstLine(d)
		}
		if c := str("code"); c != "" {
			return firstLine(c)
		}
	case "read":
		if p := str("file_path"); p != "" {
			if raw, ok := m["offset"]; ok {
				var off int
				if json.Unmarshal(raw, &off) == nil && off > 0 {
					return p + ":" + strconv.Itoa(off)
				}
			}
			return p
		}
	case "write", "edit":
		if p := str("file_path"); p != "" {
			return p
		}
	case "glob", "grep":
		pat := str("pattern")
		if root := str("path"); root != "" && pat != "" {
			return pat + "  " + root
		}
		return pat
	case "web_search":
		var qs []string
		if raw, ok := m["queries"]; ok && json.Unmarshal(raw, &qs) == nil && len(qs) > 0 {
			return firstLine(strings.Join(qs, " · "))
		}
	case "web_fetch":
		return str("url")
	case "subagent", "subagent_fork":
		return str("description")
	case "skill":
		return str("name")
	case "job_output", "job_kill":
		return str("job_id")
	}
	// Fallback: the compact JSON, as before.
	return textutil.TruncateMiddle(strings.ReplaceAll(argsJSON, "\n", " "), 120)
}

// ---- structured result meta ----------------------------------------------

// The tool/result event's meta is the tool's private presentation payload
// (the web client's card data). The shapes below mirror what the stock
// tools project; anything unrecognized falls back to the plain result text.

type readMeta struct {
	Path       string `json:"path"`
	Offset     int    `json:"offset"`
	Lines      []struct {
		Number int    `json:"number"`
		Text   string `json:"text"`
	} `json:"lines"`
	TotalLines int    `json:"totalLines"`
	Lang       string `json:"lang"`
}

type diffMeta struct {
	Diffs []struct {
		Path    string `json:"path"`
		OldText string `json:"oldText"`
		NewText string `json:"newText"`
	} `json:"diffs"`
}

type searchMeta struct {
	Shape     string `json:"shape"`
	Truncated bool   `json:"truncated"`
	Total     int    `json:"total"`
	Files     []struct {
		Path    string `json:"path"`
		Matches []struct {
			LineNumber int    `json:"lineNumber"`
			Line       string `json:"line"`
		} `json:"matches"`
	} `json:"files"`
	Paths []string `json:"paths"`
}

type webMeta struct {
	Sources []struct {
		Url   string `json:"url"`
		Title string `json:"title"`
	} `json:"sources"`
	Truncated bool `json:"truncated"`
}

// renderToolMeta paints the structured result card for one tool result.
// It returns nil when the meta is absent or unrecognized (the caller then
// falls back to the plain result text).
func renderToolMeta(th *Theme, tb *core.ToolBlock, width int) []string {
	if len(tb.Meta) == 0 {
		return nil
	}
	switch {
	case looksLikeRead(tb.Meta):
		var m readMeta
		if json.Unmarshal(tb.Meta, &m) == nil && len(m.Lines) > 0 {
			return renderReadMeta(th, &m, width)
		}
	case looksLikeDiff(tb.Meta):
		var m diffMeta
		if json.Unmarshal(tb.Meta, &m) == nil && len(m.Diffs) > 0 {
			return renderDiffMeta(th, &m, width)
		}
	case looksLikeSearch(tb.Meta):
		var m searchMeta
		if json.Unmarshal(tb.Meta, &m) == nil {
			return renderSearchMeta(th, &m, width)
		}
	case looksLikeWeb(tb.Meta):
		var m webMeta
		if json.Unmarshal(tb.Meta, &m) == nil && len(m.Sources) > 0 {
			return renderWebMeta(th, &m, width)
		}
	}
	return nil
}

func looksLikeRead(raw json.RawMessage) bool {
	return json.Valid(raw) && hasKey(raw, "lines") && hasKey(raw, "totalLines")
}
func looksLikeDiff(raw json.RawMessage) bool {
	return json.Valid(raw) && hasKey(raw, "diffs")
}
func looksLikeSearch(raw json.RawMessage) bool {
	return json.Valid(raw) && (hasKey(raw, "shape"))
}
func looksLikeWeb(raw json.RawMessage) bool {
	return json.Valid(raw) && hasKey(raw, "sources")
}

func hasKey(raw json.RawMessage, key string) bool {
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil {
		return false
	}
	_, ok := m[key]
	return ok
}

// renderReadMeta: the read window with file line numbers and syntax
// highlighting (the web client's read card). The window is tokenized
// whole so multi-line strings and comments keep their color.
func renderReadMeta(th *Theme, m *readMeta, width int) []string {
	const cap = 80
	lines := m.Lines
	if len(lines) > cap {
		lines = lines[:cap]
	}
	numW := len(fmt.Sprint(m.Lines[len(m.Lines)-1].Number))
	if numW < 2 {
		numW = 2
	}
	texts := make([]string, 0, len(lines))
	for _, ln := range lines {
		texts = append(texts, ln.Text)
	}
	hl := highlightLines(th, strings.Join(texts, "\n"), m.Lang)
	if len(hl) != len(lines) {
		hl = texts
	}
	var out []string
	for i, ln := range lines {
		num := th.Faint().Render(fmt.Sprintf("%*d", numW, ln.Number))
		bar := th.Faint().Render("│")
		out = append(out, "    "+num+bar+" "+hl[i])
	}
	if len(m.Lines) > cap {
		out = append(out, th.Faint().Render(fmt.Sprintf("    %s… %d 行未显示", strings.Repeat(" ", numW+1), len(m.Lines)-cap)))
	}
	return out
}

// renderDiffMeta: the applied change, deletions in the error color and
// insertions in the ok color (the web client's diff card).
func renderDiffMeta(th *Theme, m *diffMeta, width int) []string {
	const cap = 60
	var out []string
	shown := 0
	for _, d := range m.Diffs {
		out = append(out, "    "+th.Info().Render(d.Path))
		for _, ln := range splitSign(d.OldText, "-") {
			if shown == cap {
				break
			}
			out = append(out, "    "+th.Err().Render(ln))
			shown++
		}
		for _, ln := range splitSign(d.NewText, "+") {
			if shown == cap {
				break
			}
			out = append(out, "    "+th.Ok().Render(ln))
			shown++
		}
	}
	if shown == cap {
		out = append(out, th.Faint().Render("    …"))
	}
	return out
}

func splitSign(text, sign string) []string {
	if text == "" {
		return nil
	}
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	out := make([]string, 0, len(lines))
	for _, ln := range lines {
		out = append(out, sign+" "+ln)
	}
	return out
}

// renderSearchMeta: grep matches grouped by file, or a glob's path list
// (the web client's search card).
func renderSearchMeta(th *Theme, m *searchMeta, width int) []string {
	var out []string
	switch m.Shape {
	case "matches":
		const cap = 60
		shown := 0
		for _, f := range m.Files {
			if shown >= cap {
				break
			}
			out = append(out, "    "+th.Info().Render(f.Path))
			for _, mm := range f.Matches {
				if shown == cap {
					out = append(out, th.Faint().Render(fmt.Sprintf("    … %d 条未显示", len(f.Matches)-mm.LineNumber+1)))
					break
				}
				num := th.Faint().Render(fmt.Sprintf("%5d", mm.LineNumber))
				bar := th.Faint().Render("│")
				out = append(out, "    "+num+bar+" "+th.Code().Render(textutil.Truncate(mm.Line, width-14, "…")))
				shown++
			}
		}
		if m.Truncated && m.Total > shown {
			out = append(out, th.Faint().Render(fmt.Sprintf("    … 共 %d 条", m.Total)))
		}
	case "paths":
		const cap = 24
		for i, p := range m.Paths {
			if i == cap {
				out = append(out, th.Faint().Render(fmt.Sprintf("    … %d 个未显示", len(m.Paths)-cap)))
				break
			}
			out = append(out, "    "+th.Code().Render(p))
		}
		if m.Truncated && m.Total > len(m.Paths) {
			out = append(out, th.Faint().Render(fmt.Sprintf("    … 共 %d 个", m.Total)))
		}
	default:
		return nil
	}
	return out
}

// renderWebMeta: the cited sources of a web search (the web client's
// citation list).
func renderWebMeta(th *Theme, m *webMeta, width int) []string {
	var out []string
	for _, s := range m.Sources {
		title := s.Title
		if title == "" {
			title = s.Url
		}
		line := "  • " + th.Accent().Render(textutil.Truncate(title, width-16, "…"))
		if s.Title != "" && s.Url != "" {
			line += " " + th.Faint().Render(textutil.Truncate(s.Url, width-16-len([]rune(title)), "…"))
		}
		out = append(out, line)
	}
	if m.Truncated {
		out = append(out, th.Faint().Render("  …"))
	}
	return out
}

// ---- result text coloring -------------------------------------------------

// colorizeResult paints plain tool output: a failed result rides the
// error color; a unified diff wears the diff colors; everything else
// the faint result color. The web client renders the same output
// verbatim; the colors are the CLI's reading aid.
func colorizeResult(th *Theme, text string, width int, isErr bool) []string {
	lines := wrapLines(text, width-4)
	diff := looksLikeDiffText(lines)
	var out []string
	for _, ln := range lines {
		switch {
		case isErr:
			out = append(out, "    "+th.Err().Render(ln))
		case !diff:
			out = append(out, "    "+th.Faint().Render(ln))
		case strings.HasPrefix(ln, "+++") || strings.HasPrefix(ln, "---"):
			out = append(out, "    "+th.Faint().Render(ln))
		case strings.HasPrefix(ln, "@@"):
			out = append(out, "    "+th.Info().Render(ln))
		case strings.HasPrefix(ln, "+"):
			out = append(out, "    "+th.Ok().Render(ln))
		case strings.HasPrefix(ln, "-"):
			out = append(out, "    "+th.Err().Render(ln))
		default:
			out = append(out, "    "+th.Faint().Render(ln))
		}
	}
	return out
}

// looksLikeDiffText: a block is a diff when it carries a hunk header or
// file header, or when signed lines clearly dominate. (ls -la lists
// files starting with "-"; those are not deletions.)
func looksLikeDiffText(lines []string) bool {
	if len(lines) == 0 {
		return false
	}
	signs, body := 0, 0
	for _, ln := range lines {
		switch {
		case strings.HasPrefix(ln, "@@") || strings.HasPrefix(ln, "+++") || strings.HasPrefix(ln, "---"):
			return true
		case strings.HasPrefix(ln, "+") || strings.HasPrefix(ln, "-"):
			signs++
		case strings.TrimSpace(ln) != "":
			body++
		}
	}
	return signs >= 2 && signs >= body
}

// ---- syntax highlighting --------------------------------------------------

// highlightLines tokenizes one code window with chroma and paints each
// token with the theme's Braun mapping (the same palette the markdown
// code blocks use). Lines come back without any prefix; the caller adds
// line numbers. Under NoColor/ASCII the lines pass through unstyled.
func highlightLines(th *Theme, code, lang string) []string {
	raw := strings.Split(code, "\n")
	if th.NoColor || th.Profile == termenv.Ascii {
		return raw
	}
	var lexer chroma.Lexer
	if lang != "" {
		lexer = lexers.Get(lang)
	}
	if lexer == nil {
		lexer = lexers.Fallback
	}
	lexer = chroma.Coalesce(lexer)
	it, err := lexer.Tokenise(nil, code)
	if err != nil {
		return raw
	}
	sgr := newSGRMap(th)
	var out []string
	var cur strings.Builder
	for _, tok := range it.Tokens() {
		// Whitespace and plain text ride the body color: write them raw
		// so the stream carries no pointless SGR noise.
		rawTok := tok.Type == chroma.Text || tok.Type == chroma.TextWhitespace
		st := sgr[th.tokenColor(tok.Type)]
		parts := strings.Split(tok.Value, "\n")
		for i, pp := range parts {
			if i > 0 {
				out = append(out, cur.String())
				cur.Reset()
			}
			if pp == "" {
				continue
			}
			if rawTok {
				cur.WriteString(pp)
			} else {
				cur.WriteString(st.Render(pp))
			}
		}
	}
	out = append(out, cur.String())
	if len(out) != len(raw) {
		return raw // tokeniser disagreement: never misalign the numbers
	}
	return out
}

// tokenColor maps a chroma token class onto the Braun palette, matching
// the chroma block of braunMarkStyle so inline code, fenced blocks and
// read cards all speak one color language.
func (th *Theme) tokenColor(t chroma.TokenType) string {
	cat, sub := t.Category(), t.SubCategory()
	switch {
	case cat == chroma.Keyword:
		return th.CAccent
	case cat == chroma.Comment:
		return th.CFaint
	case cat == chroma.Operator || cat == chroma.Punctuation:
		return th.CDim
	case sub == chroma.NameBuiltin || sub == chroma.NameTag || sub == chroma.NameAttribute ||
		sub == chroma.NameConstant || sub == chroma.NameDecorator || sub == chroma.NameException:
		return th.CInfo
	case sub == chroma.LiteralNumber || sub == chroma.LiteralDate || sub == chroma.LiteralStringEscape:
		return th.CInfo
	case sub == chroma.LiteralString:
		return th.CWarn
	case sub == chroma.GenericDeleted || cat == chroma.Error:
		return th.CErr
	case sub == chroma.GenericInserted:
		return th.COK
	default:
		return th.CFG
	}
}

// newSGRMap resolves the palette colors the highlighter actually uses to
// one-argument render styles, once per call.
func newSGRMap(th *Theme) map[string]lipgloss.Style {
	out := map[string]lipgloss.Style{}
	for _, c := range []string{th.CAccent, th.CFaint, th.CDim, th.CInfo, th.CFG, th.CWarn, th.CErr, th.COK} {
		if _, done := out[c]; done {
			continue
		}
		if c == "" || c == "default" {
			out[c] = lipgloss.NewStyle()
			continue
		}
		out[c] = lipgloss.NewStyle().Foreground(lipgloss.Color(c))
	}
	return out
}

// ---- code-region slab ------------------------------------------------------

// isCodeTool: the tools whose card is a stretch of executed code. The
// card region rides the code slab (CCodeBG) so it stands apart from the
// prose and the file cards without a border.
func isCodeTool(name string) bool {
	return name == "run_code"
}

// paintCodeSlab lays the code surface under a card: every line padded
// to the transcript width and merged through cardLine, so the slab
// fills the cells the line leaves bare while styled spans (the diff
// colors, the line numbers) keep their own look. A palette without a
// slab color returns the lines untouched.
//
// Each line closes its own SGR state. The slab spans the pane edge to
// edge, so a live tail colors whatever the renderer writes next on that
// row — the gutter and the whole ctrl+t dock column — and on the row
// after it too (a newline carries no reset), which reads as one slab row
// too many.
func paintCodeSlab(th *Theme, lines []string, width int) []string {
	slab := th.CodeState()
	if len(slab.bg) == 0 {
		return lines
	}
	var bare sgrState
	out := make([]string, 0, len(lines))
	for _, ln := range lines {
		if p := plainWidth(ln); p < width {
			ln += strings.Repeat(" ", width-p)
		}
		out = append(out, cardLine(ln, slab)+bare.marshalOrReset())
	}
	return out
}
