package wiki

import (
	"bytes"
	"fmt"
	"log"
	"regexp"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// TOCEntry represents a single Table of Contents item.
type TOCEntry struct {
	Level  int
	ID     string
	Text   string
	Number string
}

// -------------------------
// Context key for TOC
// -------------------------
var tocKey = parser.NewContextKey()

// -------------------------
// TOC Transformer
// -------------------------
type tocTransformer struct{}

func (t *tocTransformer) Transform(doc *ast.Document, reader text.Reader, pc parser.Context) {
	tocList := pc.Get(tocKey).(*[]TOCEntry)
	ids := make(map[string]bool)
	var counters [7]int // support up to H6

	err := ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering || n.Kind() != ast.KindHeading {
			return ast.WalkContinue, nil
		}

		h := n.(*ast.Heading)
		title := string(h.Text(reader.Source()))

		// Generate unique ID
		id := generateID(title)
		origID := id
		i := 1
		for ids[id] {
			id = fmt.Sprintf("%s-%d", origID, i)
			i++
		}
		ids[id] = true

		// Numbering
		level := h.Level

		// Initialize skipped parent levels
		for i := 1; i < level; i++ {
			if counters[i] == 0 {
				counters[i] = 1
			}
		}

		// Increment current level
		counters[level]++

		// Reset deeper levels
		for i := level + 1; i <= 6; i++ {
			counters[i] = 0
		}

		// Build number
		parts := make([]string, 0, level)
		for i := 1; i <= level; i++ {
			parts = append(parts, fmt.Sprintf("%d", counters[i]))
		}
		number := strings.Join(parts, ".")

		// Attach attributes
		h.SetAttributeString("data-id", []byte(id))
		h.SetAttributeString("data-number", []byte(number))

		// Add to TOC
		*tocList = append(*tocList, TOCEntry{
			Level:  level,
			ID:     id,
			Text:   title,
			Number: number,
		})

		// Insert anchor span before heading
		anchor := &AnchorSpan{ID: id}
		doc.InsertBefore(doc, n, anchor)

		return ast.WalkContinue, nil
	})
	if err != nil {
		log.Println(err)
	}
}

// -------------------------
// Anchor Span Node
// -------------------------
type AnchorSpan struct {
	ast.BaseBlock
	ID string
}

func (n *AnchorSpan) Dump(source []byte, level int) {
	ast.DumpHelper(n, source, level, map[string]string{"ID": n.ID}, nil)
}

func (n *AnchorSpan) Kind() ast.NodeKind {
	return KindAnchorSpan
}

var KindAnchorSpan = ast.NewNodeKind("AnchorSpan")

type anchorSpanRenderer struct{}

func (r *anchorSpanRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(KindAnchorSpan, r.renderAnchorSpan)
}

func (r *anchorSpanRenderer) renderAnchorSpan(w util.BufWriter, source []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
	if entering {
		node := n.(*AnchorSpan)
		_, err := fmt.Fprintf(w, `<span id="%s"></span>`, node.ID)
		if err != nil {
			log.Println(err)
		}
	}
	return ast.WalkContinue, nil
}

// -------------------------
// Heading Renderer
// -------------------------
type headingRenderer struct{}

func (r *headingRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(ast.KindHeading, r.renderHeading)
}

func (r *headingRenderer) renderHeading(
	w util.BufWriter,
	source []byte,
	node ast.Node,
	entering bool,
) (ast.WalkStatus, error) {
	h := node.(*ast.Heading)

	if entering {
		level := h.Level

		// AttributeString returns interface{}, need []byte assertion
		var idStr, numberStr string
		if v, ok := h.AttributeString("data-id"); ok {
			idStr = string(v.([]byte))
		}
		if v, ok := h.AttributeString("data-number"); ok {
			numberStr = string(v.([]byte))
		}

		// Open heading + link
		_, err := fmt.Fprintf(w, "<h%d><a href=\"#%s\">%s ", level, idStr, numberStr)
		if err != nil {
			log.Println(err)
		}

		// Render heading text manually
		for c := h.FirstChild(); c != nil; c = c.NextSibling() {
			err = ast.Walk(c, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
				if text, ok := n.(*ast.Text); ok && entering {
					_, err = w.Write(text.Segment.Value(source))
					if err != nil {
						log.Println(err)
					}
				}
				return ast.WalkContinue, nil
			})
			if err != nil {
				log.Println(err)
			}
		}

		return ast.WalkSkipChildren, nil
	}

	// Close link + heading
	_, err := w.WriteString("</a></h" + fmt.Sprint(h.Level) + ">")
	if err != nil {
		log.Println(err)
	}
	return ast.WalkContinue, nil
}

// -------------------------
// Markdown Renderer
// -------------------------
var mdRenderer = goldmark.New(
	goldmark.WithExtensions(extension.GFM),
	goldmark.WithParserOptions(
		parser.WithASTTransformers(util.Prioritized(&tocTransformer{}, 1000)),
	),
	goldmark.WithRendererOptions(
		html.WithHardWraps(),
		html.WithUnsafe(),
		renderer.WithNodeRenderers(
			util.Prioritized(&anchorSpanRenderer{}, 10),
			util.Prioritized(&headingRenderer{}, 11),
		),
	),
)

// -------------------------
// Utilities
// -------------------------
func generateID(s string) string {
	s = strings.ToLower(s)
	reg, _ := regexp.Compile("[^a-z0-9]+")
	s = reg.ReplaceAllString(s, "-")
	return strings.Trim(s, "-")
}

// -------------------------
// RenderPage
// -------------------------
func RenderPage(raw []byte) ([]byte, []TOCEntry, error) {
	ctx := parser.NewContext()
	var toc []TOCEntry
	ctx.Set(tocKey, &toc)

	var buf bytes.Buffer
	if err := mdRenderer.Convert(raw, &buf, parser.WithContext(ctx)); err != nil {
		return nil, nil, err
	}

	return buf.Bytes(), toc, nil
}
