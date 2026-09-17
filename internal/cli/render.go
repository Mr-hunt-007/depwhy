package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/Mr-hunt-007/depwhy/internal/explain"
	"github.com/Mr-hunt-007/depwhy/internal/graph"
)

type colors struct{ on bool }

func (c colors) wrap(code, s string) string {
	if !c.on || s == "" {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}

func (c colors) bold(s string) string   { return c.wrap("1", s) }
func (c colors) dim(s string) string    { return c.wrap("2", s) }
func (c colors) yellow(s string) string { return c.wrap("33", s) }
func (c colors) cyan(s string) string   { return c.wrap("36", s) }

type trieNode struct {
	step     graph.Step
	children []*trieNode
}

func (t *trieNode) child(s graph.Step) *trieNode {
	for _, c := range t.children {
		if c.step.Node.ID == s.Node.ID && c.step.Kind == s.Kind {
			return c
		}
	}
	n := &trieNode{step: s}
	t.children = append(t.children, n)
	return n
}

func label(c colors, s graph.Step, highlight bool) string {
	name := s.Node.Name
	if highlight {
		name = c.bold(name)
	}
	out := name
	if s.Node.Version != "" {
		out += " " + c.dim(s.Node.Version)
	}
	if s.Kind != "" {
		out += " " + c.yellow("["+s.Kind+"]")
	}
	return out
}

func renderMatch(w io.Writer, c colors, m explain.Found) {
	header := c.bold(m.Name)
	if m.Version != "" {
		header += " " + m.Version
	}
	header += "  " + c.dim("("+m.Ecosystem+", "+m.Lockfile+")")
	if len(m.Flags) > 0 {
		header += "  " + c.yellow(strings.Join(m.Flags, ", "))
	}
	fmt.Fprintln(w, header)

	res := m.Result
	if res.Total == 0 && res.Complete {
		fmt.Fprintln(w, c.dim("not reachable from any root (the lockfile may be stale, or this is an orphaned entry)"))
		return
	}
	if m.Root && res.Total == 1 && len(res.Paths) == 1 && len(res.Paths[0]) == 1 {
		fmt.Fprintln(w, c.dim("this is a root: the project itself, a workspace member or the main module"))
		return
	}

	root := &trieNode{}
	for _, p := range res.Paths {
		cur := root
		for _, s := range p {
			cur = cur.child(s)
		}
	}
	for _, top := range root.children {
		fmt.Fprintln(w, label(c, top.step, len(top.children) == 0))
		renderChildren(w, c, top, "")
	}

	shown := len(res.Paths)
	switch {
	case !res.Complete:
		fmt.Fprintln(w, c.dim(fmt.Sprintf("showing %d of at least %d paths (search stopped early; the graph is very large)", shown, res.Total)))
	case shown < res.Total:
		fmt.Fprintln(w, c.dim(fmt.Sprintf("showing %d of %d paths (use --max-paths 0 to show all)", shown, res.Total)))
	case res.Total > 1:
		fmt.Fprintln(w, c.dim(fmt.Sprintf("%d paths", res.Total)))
	}
}

func renderChildren(w io.Writer, c colors, t *trieNode, prefix string) {
	for i, ch := range t.children {
		last := i == len(t.children)-1
		branch, next := "├── ", "│   "
		if last {
			branch, next = "└── ", "    "
		}
		fmt.Fprintln(w, prefix+c.dim(branch)+label(c, ch.step, len(ch.children) == 0))
		renderChildren(w, c, ch, prefix+c.dim(next))
	}
}
