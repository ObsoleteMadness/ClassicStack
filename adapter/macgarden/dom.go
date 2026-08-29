//go:build macgarden || all

package macgarden

// dom is a tiny, dependency-light DOM query layer over golang.org/x/net/html — the
// subset of goquery the MacGarden scraper needs, reimplemented here so the refactor
// ring does not re-add the goquery / cascadia third-party modules it deliberately
// dropped. x/net/html is already a (direct) dependency of the tree.
//
// It supports exactly the CSS selector features the scraper uses, no more:
//   - type selectors            a, h1, dt
//   - id selectors              #paper
//   - class selectors           .title, .note.download (compound)
//   - attribute selectors       [href], [href^='/games/'], [href*='/category/']
//   - descendant combinator     "#paper p"  (whitespace)
//   - child combinator          "#paper > h1"
//   - adjacent-sibling          "br + small"
//   - selector lists            "a[href^='/games/'], a[href^='/apps/']"
//
// A Selection mirrors goquery's: an ordered set of nodes plus the chainable query
// methods (Find/Attr/Text/Each/Eq/First/Last/Length/Contents). The matching is
// document-order, de-duplicated, exactly as goquery returns.

import (
	"io"
	"strings"

	"golang.org/x/net/html"
)

// Document is a parsed HTML document; it is a Selection rooted at the document node.
type Document struct{ *Selection }

// Selection is an ordered, de-duplicated set of matched nodes (goquery-shaped).
type Selection struct {
	nodes []*html.Node
}

// NewDocumentFromReader parses HTML from r into a queryable Document (goquery-shaped:
// accepts any io.Reader).
func NewDocumentFromReader(r io.Reader) (*Document, error) {
	root, err := html.Parse(r)
	if err != nil {
		return nil, err
	}
	return &Document{&Selection{nodes: []*html.Node{root}}}, nil
}

// Find runs a (possibly comma-separated) selector against the descendants of every
// node in the selection and returns the union, in document order, de-duplicated.
func (s *Selection) Find(selector string) *Selection {
	groups := parseSelectorList(selector)
	var out []*html.Node
	seen := map[*html.Node]bool{}
	for _, root := range s.nodes {
		for _, g := range groups {
			for _, n := range g.matchDescendants(root) {
				if !seen[n] {
					seen[n] = true
					out = append(out, n)
				}
			}
		}
	}
	return &Selection{nodes: orderedDedup(out, s.nodes)}
}

// Each calls fn for every node in the selection (index, single-node Selection).
func (s *Selection) Each(fn func(int, *Selection)) {
	for i, n := range s.nodes {
		fn(i, &Selection{nodes: []*html.Node{n}})
	}
}

// Eq returns the i-th node as a single-node selection (empty when out of range).
func (s *Selection) Eq(i int) *Selection {
	if i < 0 || i >= len(s.nodes) {
		return &Selection{}
	}
	return &Selection{nodes: []*html.Node{s.nodes[i]}}
}

// First / Last return the first / last node as a single-node selection.
func (s *Selection) First() *Selection { return s.Eq(0) }
func (s *Selection) Last() *Selection  { return s.Eq(len(s.nodes) - 1) }

// Length returns the number of nodes in the selection.
func (s *Selection) Length() int { return len(s.nodes) }

// Attr returns the value of the named attribute on the FIRST node, and whether it was
// present (goquery semantics).
func (s *Selection) Attr(name string) (string, bool) {
	if len(s.nodes) == 0 {
		return "", false
	}
	return attr(s.nodes[0], name)
}

// Text returns the concatenated text content of every node in the selection (goquery
// concatenates across the set; for a single-node selection that is just its subtree).
func (s *Selection) Text() string {
	var b strings.Builder
	for _, n := range s.nodes {
		collectText(n, &b)
	}
	return b.String()
}

// Contents returns the immediate child nodes (elements AND text) of every node in the
// selection, in order — goquery's .Contents(). The scraper uses .Contents().First()
// (the first child's text) and .Contents().Last() (the trailing text node).
func (s *Selection) Contents() *Selection {
	var out []*html.Node
	for _, n := range s.nodes {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			out = append(out, c)
		}
	}
	return &Selection{nodes: out}
}

// --- selector parsing + matching ----------------------------------------------------

// compound is one compound selector (a single element step): an optional type, plus
// id/class/attribute constraints, plus the combinator joining it to the PREVIOUS step.
type compound struct {
	combinator byte // ' ' descendant, '>' child, '+' adjacent sibling; 0 for the first step
	tag        string
	id         string
	classes    []string
	attrs      []attrMatch
}

type attrMatch struct {
	key string
	op  byte // 0 = presence, '=' exact, '^' prefix, '*' substring
	val string
}

// selectorChain is a sequence of compound steps joined by combinators (a single
// comma-group of a selector list).
type selectorChain []compound

// parseSelectorList splits "a, b > c" into its comma groups, each a selectorChain.
func parseSelectorList(s string) []selectorChain {
	var out []selectorChain
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		out = append(out, parseChain(part))
	}
	return out
}

// parseChain parses one combinator-joined sequence ("#paper > div.box a").
func parseChain(s string) selectorChain {
	// Tokenise on whitespace, keeping '>' and '+' as their own tokens.
	var toks []string
	var cur strings.Builder
	flush := func() {
		if cur.Len() > 0 {
			toks = append(toks, cur.String())
			cur.Reset()
		}
	}
	for _, r := range s {
		switch r {
		case ' ', '\t', '\n':
			flush()
		case '>', '+':
			flush()
			toks = append(toks, string(r))
		default:
			cur.WriteRune(r)
		}
	}
	flush()

	var chain selectorChain
	pendingComb := byte(0) // first step has no combinator
	for _, tok := range toks {
		switch tok {
		case ">":
			pendingComb = '>'
		case "+":
			pendingComb = '+'
		default:
			c := parseCompound(tok)
			if len(chain) == 0 {
				c.combinator = 0
			} else if pendingComb != 0 {
				c.combinator = pendingComb
			} else {
				c.combinator = ' '
			}
			chain = append(chain, c)
			pendingComb = 0
		}
	}
	return chain
}

// parseCompound parses a single compound selector ("dt.title", "a[href^='/x']").
func parseCompound(s string) compound {
	var c compound
	i := 0
	// Leading type selector (letters/digits) before any #/./[.
	for i < len(s) && s[i] != '#' && s[i] != '.' && s[i] != '[' {
		i++
	}
	c.tag = strings.ToLower(s[:i])
	for i < len(s) {
		switch s[i] {
		case '#':
			j := i + 1
			for j < len(s) && s[j] != '.' && s[j] != '[' && s[j] != '#' {
				j++
			}
			c.id = s[i+1 : j]
			i = j
		case '.':
			j := i + 1
			for j < len(s) && s[j] != '.' && s[j] != '[' && s[j] != '#' {
				j++
			}
			c.classes = append(c.classes, s[i+1:j])
			i = j
		case '[':
			j := i + 1
			for j < len(s) && s[j] != ']' {
				j++
			}
			c.attrs = append(c.attrs, parseAttr(s[i+1:j]))
			i = j + 1
		default:
			i++
		}
	}
	return c
}

// parseAttr parses the inside of "[...]" — "href", "href^='/x'", "class*='y'".
func parseAttr(s string) attrMatch {
	for _, op := range []byte{'^', '*', '='} {
		if idx := strings.IndexByte(s, op); idx >= 0 {
			// A '=' may follow '^'/'*' (e.g. ^=); skip the '=' that trails an op char.
			if op == '=' && idx > 0 && (s[idx-1] == '^' || s[idx-1] == '*') {
				continue
			}
			key := s[:idx]
			rest := s[idx+1:]
			if op != '=' && idx+1 < len(s) && s[idx+1] == '=' {
				rest = s[idx+2:]
			}
			return attrMatch{key: key, op: op, val: unquote(rest)}
		}
	}
	return attrMatch{key: strings.TrimSpace(s)}
}

func unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && (s[0] == '\'' || s[0] == '"') && s[len(s)-1] == s[0] {
		return s[1 : len(s)-1]
	}
	return s
}

// matchDescendants returns every node under root (not root itself) that the chain
// matches, in document order.
func (chain selectorChain) matchDescendants(root *html.Node) []*html.Node {
	var out []*html.Node
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.ElementNode && chain.matches(c) {
				out = append(out, c)
			}
			walk(c)
		}
	}
	walk(root)
	return out
}

// matches reports whether node n is the END of the chain — i.e. n satisfies the last
// compound and its ancestor/sibling chain satisfies the preceding steps.
func (chain selectorChain) matches(n *html.Node) bool {
	return chain.matchFrom(n, len(chain)-1)
}

// matchFrom checks that n satisfies compound i and the rest of the chain (0..i-1)
// matches along the appropriate combinator axis.
func (chain selectorChain) matchFrom(n *html.Node, i int) bool {
	if !chain[i].matchNode(n) {
		return false
	}
	if i == 0 {
		return true
	}
	prev := chain[i]
	switch prev.combinator {
	case '>': // immediate parent must match compound i-1
		p := n.Parent
		return p != nil && p.Type == html.ElementNode && chain.matchFrom(p, i-1)
	case '+': // immediately preceding element sibling must match compound i-1
		s := prevElement(n)
		return s != nil && chain.matchFrom(s, i-1)
	default: // descendant: SOME ancestor matches compound i-1 (and its chain)
		for p := n.Parent; p != nil; p = p.Parent {
			if p.Type == html.ElementNode && chain.matchFrom(p, i-1) {
				return true
			}
		}
		return false
	}
}

// matchNode checks a single compound against one element node (no combinators).
func (c compound) matchNode(n *html.Node) bool {
	if n.Type != html.ElementNode {
		return false
	}
	if c.tag != "" && c.tag != "*" && !strings.EqualFold(n.Data, c.tag) {
		return false
	}
	if c.id != "" {
		if v, _ := attr(n, "id"); v != c.id {
			return false
		}
	}
	if len(c.classes) > 0 {
		have := classSet(n)
		for _, want := range c.classes {
			if !have[want] {
				return false
			}
		}
	}
	for _, am := range c.attrs {
		v, ok := attr(n, am.key)
		if !ok {
			return false
		}
		switch am.op {
		case 0: // presence only
		case '=':
			if v != am.val {
				return false
			}
		case '^':
			if !strings.HasPrefix(v, am.val) {
				return false
			}
		case '*':
			if !strings.Contains(v, am.val) {
				return false
			}
		}
	}
	return true
}

// --- html.Node helpers ---------------------------------------------------------------

func attr(n *html.Node, key string) (string, bool) {
	for _, a := range n.Attr {
		if strings.EqualFold(a.Key, key) {
			return a.Val, true
		}
	}
	return "", false
}

func classSet(n *html.Node) map[string]bool {
	v, _ := attr(n, "class")
	out := map[string]bool{}
	for _, f := range strings.Fields(v) {
		out[f] = true
	}
	return out
}

// prevElement returns the immediately preceding ELEMENT sibling of n (skipping text).
func prevElement(n *html.Node) *html.Node {
	for s := n.PrevSibling; s != nil; s = s.PrevSibling {
		if s.Type == html.ElementNode {
			return s
		}
	}
	return nil
}

// collectText appends the text content of n's subtree to b (goquery .Text()).
func collectText(n *html.Node, b *strings.Builder) {
	if n.Type == html.TextNode {
		b.WriteString(n.Data)
		return
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		collectText(c, b)
	}
}

// orderedDedup returns nodes sorted into document order relative to the roots, with
// duplicates already removed by the caller. Document order is the pre-order DFS index;
// we recompute it from the first root's document to keep Find() results stable.
func orderedDedup(nodes []*html.Node, roots []*html.Node) []*html.Node {
	if len(nodes) <= 1 || len(roots) == 0 {
		return nodes
	}
	// Index every node by pre-order position from the document root.
	docRoot := roots[0]
	for docRoot.Parent != nil {
		docRoot = docRoot.Parent
	}
	pos := map[*html.Node]int{}
	idx := 0
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		pos[n] = idx
		idx++
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(docRoot)
	// Insertion sort by position (selections are small).
	out := append([]*html.Node(nil), nodes...)
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && pos[out[j]] < pos[out[j-1]]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}
