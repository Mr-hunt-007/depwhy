package eco

import (
	"bufio"
	"bytes"
	"fmt"
	"strconv"
	"strings"

	"github.com/Mr-hunt-007/depwhy/internal/graph"
)

// GoRequire is one requirement line from go.mod.
type GoRequire struct {
	Version  string
	Indirect bool
}

// GoMod is what depwhy reads from go.mod.
type GoMod struct {
	Module   string
	Requires map[string]GoRequire
}

// ParseGoMod parses the module path and require directives of go.mod.
func ParseGoMod(data []byte) (*GoMod, error) {
	gm := &GoMod{Requires: map[string]GoRequire{}}
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 1024*1024), 1024*1024)
	inBlock := ""
	lineNo := 0
	for sc.Scan() {
		lineNo++
		raw := strings.TrimSpace(sc.Text())
		code, comment, _ := strings.Cut(raw, "//")
		code = strings.TrimSpace(code)
		indirect := strings.TrimSpace(comment) == "indirect" || strings.HasPrefix(strings.TrimSpace(comment), "indirect;")
		if code == "" {
			continue
		}
		if inBlock != "" {
			if code == ")" {
				inBlock = ""
				continue
			}
			if inBlock == "require" {
				if err := gm.addRequire(code, indirect); err != nil {
					return nil, fmt.Errorf("go.mod line %d: %v", lineNo, err)
				}
			}
			continue
		}
		verb, rest, _ := strings.Cut(code, " ")
		rest = strings.TrimSpace(rest)
		switch {
		case rest == "(":
			inBlock = verb
		case verb == "module":
			gm.Module = unquoteGo(rest)
		case verb == "require":
			if err := gm.addRequire(rest, indirect); err != nil {
				return nil, fmt.Errorf("go.mod line %d: %v", lineNo, err)
			}
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if gm.Module == "" {
		return nil, fmt.Errorf("go.mod: no module directive")
	}
	return gm, nil
}

func unquoteGo(s string) string {
	if u, err := strconv.Unquote(s); err == nil {
		return u
	}
	return s
}

func (gm *GoMod) addRequire(s string, indirect bool) error {
	fields := strings.Fields(s)
	if len(fields) != 2 {
		return fmt.Errorf("malformed require %q", s)
	}
	gm.Requires[unquoteGo(fields[0])] = GoRequire{Version: fields[1], Indirect: indirect}
	return nil
}

func splitModVersion(tok string) (path, version string) {
	i := strings.LastIndexByte(tok, '@')
	if i < 0 {
		return tok, ""
	}
	return tok[:i], tok[i+1:]
}

// BuildGo builds a module graph from `go mod graph` output. Each module is
// collapsed to its selected version (the highest version in the graph, as
// minimal version selection picks), and only the requirements of selected
// versions are kept.
func BuildGo(source string, modGraph []byte, gm *GoMod) (*graph.Graph, error) {
	g := graph.New("go", source)
	g.DeferredKind = graph.KindIndirect
	type edge struct{ from, fromVer, to string }
	var edges []edge
	selected := map[string]string{}
	mains := map[string]bool{}
	note := func(p, v string) {
		if v == "" {
			mains[p] = true
			return
		}
		if old, ok := selected[p]; !ok || graph.CompareVersions(v, old) > 0 {
			selected[p] = v
		}
	}
	for i, line := range strings.Split(string(modGraph), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 {
			return nil, fmt.Errorf("go mod graph output line %d: unexpected %q", i+1, line)
		}
		fp, fv := splitModVersion(fields[0])
		tp, tv := splitModVersion(fields[1])
		if tp == "go" || tp == "toolchain" || fp == "go" || fp == "toolchain" {
			continue // Go version pseudo-modules
		}
		note(fp, fv)
		if tv == "" {
			return nil, fmt.Errorf("go mod graph output line %d: requirement without version %q", i+1, fields[1])
		}
		note(tp, tv)
		edges = append(edges, edge{from: fp, fromVer: fv, to: tp})
	}
	if gm != nil {
		mains[gm.Module] = true
	}
	for p := range mains {
		delete(selected, p)
		g.AddNode(&graph.Node{ID: p, Name: p, Root: true})
	}
	for p, v := range selected {
		n := &graph.Node{ID: p, Name: p, Version: v}
		if gm != nil {
			if r, ok := gm.Requires[p]; ok && r.Indirect {
				n.Flags = append(n.Flags, "indirect")
			}
		}
		g.AddNode(n)
	}
	for _, e := range edges {
		if !mains[e.from] && selected[e.from] != e.fromVer {
			continue // requirements of versions MVS did not select
		}
		kind := graph.KindNormal
		if mains[e.from] && gm != nil && e.from == gm.Module {
			if r, ok := gm.Requires[e.to]; ok && r.Indirect {
				kind = graph.KindIndirect
			}
		}
		g.AddEdge(e.from, e.to, kind)
	}
	return g, nil
}
