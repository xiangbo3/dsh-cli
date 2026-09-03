package ui

import (
	"bytes"

	"github.com/charmbracelet/glamour/ansi"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	feast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// newMdRenderer builds the transcript markdown pipeline: goldmark with GFM
// (tables, strikethrough, autolinks, task lists), definition lists and PHP
// Markdown Extra footnotes (rewritten below for the ANSI backend), rendered
// through glamour's ANSI backend with the Braun palette. The emoji
// shortcode extension is deliberately off: `:name:` in agent output (hex
// dumps, flags, MAC-adjacent tokens) must survive verbatim rather than
// being folded into unicode emoji (which also feeds the regional-pair flag
// ambiguity).
func newMdRenderer(width int, th *Theme, doc string) *mdRenderer {
	md := goldmark.New(
		goldmark.WithExtensions(extension.GFM, extension.DefinitionList, extension.Footnote),
		goldmark.WithParserOptions(
			parser.WithAutoHeadingID(),
			parser.WithASTTransformers(util.Prioritized(footnoteToList{}, 1000)),
		),
	)
	ar := ansi.NewRenderer(ansi.Options{
		WordWrap:     width,
		ColorProfile: th.Profile,
		Styles:       braunMarkStyle(th, doc),
	})
	md.SetRenderer(renderer.NewRenderer(
		renderer.WithNodeRenderers(util.Prioritized(ar, 1000)),
	))
	return &mdRenderer{
		width: width,
		doc:   doc,
		render: func(txt string) (string, error) {
			var buf bytes.Buffer
			if err := md.Convert([]byte(txt), &buf); err != nil {
				return "", err
			}
			return buf.String(), nil
		},
	}
}

// footnoteToList rewrites PHP-Markdown-Extra footnotes into node kinds the
// glamour ANSI backend styles out of the box: the trailing footnote list
// becomes a regular numbered list (in footnote order) and each inline [^n]
// link becomes its literal source text. It runs after the goldmark footnote
// transformer (priority 999 vs 1000); without it the v1.0.0 ANSI backend
// has no element for the footnote node kinds and would drop them, printing
// a warning to stdout.
type footnoteToList struct{}

func (footnoteToList) Transform(doc *ast.Document, reader text.Reader, _ parser.Context) {
	var list *feast.FootnoteList
	for n := doc.FirstChild(); n != nil; n = n.NextSibling() {
		if fl, ok := n.(*feast.FootnoteList); ok {
			list = fl
			break
		}
	}
	if list == nil {
		return
	}

	// Collect the inline nodes first: replacing one breaks the sibling
	// links the walk iterates over.
	var links []*feast.FootnoteLink
	var backlinks []*feast.FootnoteBacklink
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		// Collecting on entering visits only: the walk also visits every
		// node on its way out, and a node replaced on the way in is
		// parentless on the way out.
		if !entering {
			return ast.WalkContinue, nil
		}
		switch tn := n.(type) {
		case *feast.FootnoteLink:
			links = append(links, tn)
		case *feast.FootnoteBacklink:
			backlinks = append(backlinks, tn)
		}
		return ast.WalkContinue, nil
	})

	src := reader.Source()
	// Walk order is source order, so each link consumes the next literal
	// occurrence of "[^label]" in the source — no node positions needed.
	nextOccurrence := map[string]int{}
	for _, ln := range links {
		var fn *feast.Footnote
		for f := list.FirstChild(); f != nil; f = f.NextSibling() {
			if ft, ok := f.(*feast.Footnote); ok && ft.Index == ln.Index {
				fn = ft
				break
			}
		}
		if fn == nil {
			continue
		}
		marker := "[^" + string(fn.Ref) + "]"
		from := nextOccurrence[marker]
		idx := bytes.Index(src[from:], []byte(marker))
		if idx < 0 {
			continue
		}
		start, stop := from+idx, from+idx+len(marker)
		nextOccurrence[marker] = stop
		ln.Parent().ReplaceChild(ln.Parent(), ln, ast.NewTextSegment(text.NewSegment(start, stop)))
	}
	for _, bl := range backlinks {
		bl.Parent().RemoveChild(bl.Parent(), bl)
	}

	// The trailing list becomes a plain ordered list, keeping footnote
	// order (the list children are already index-sorted).
	newList := ast.NewList('.')
	newList.Start = 1
	for fn := list.FirstChild(); fn != nil; {
		next := fn.NextSibling()
		item := ast.NewListItem(0)
		for c := fn.FirstChild(); c != nil; {
			cn := c.NextSibling()
			item.AppendChild(item, c)
			c = cn
		}
		newList.AppendChild(newList, item)
		fn = next
	}
	doc.ReplaceChild(doc, list, newList)
}
