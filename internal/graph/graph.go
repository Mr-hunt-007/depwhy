// Package graph holds the ecosystem-neutral dependency graph and the
// "why is this here" path search.
package graph

import (
	"sort"
	"strings"
)

// Edge kinds. An empty kind is a normal runtime dependency.
const (
	KindNormal   = ""
	KindDev      = "dev"
	KindOptional = "optional"
	KindPeer     = "peer"
	KindBuild    = "build"
	KindIndirect = "indirect"
)

// Node is one resolved package instance.
type Node struct {
	ID      string   // unique within the graph (install location, name@version, ...)
	Name    string   // display name as written in the lockfile
	AltName string   // another name the package is known by (npm aliases), may be empty
	Version string   // resolved version, may be empty
	Flags   []string // lockfile-recorded markers such as "dev", "optional", "indirect"
	Root    bool     // a project, workspace member or main module
}

// Edge is a dependency from one node to another.
type Edge struct {
	To   string
	Kind string
}

// Graph is a dependency graph read from one lockfile.
type Graph struct {
	Ecosystem string // npm, cargo, python, go
	Source    string // lockfile name, e.g. Cargo.lock
	Nodes     map[string]*Node
	Edges     map[string][]Edge
	// Normalize maps a package name to its comparison form. Nil means
	// names compare exactly.
	Normalize func(string) string
	Warnings  []string
	// DeferredKind names an edge kind that is only followed when no path
	// exists without it. Go uses it for "// indirect" requirements of the main
	// module, which record that something else needs the module rather than
	// why.
	DeferredKind string
}

// New returns an empty graph.
func New(eco, source string) *Graph {
	return &Graph{Ecosystem: eco, Source: source, Nodes: map[string]*Node{}, Edges: map[string][]Edge{}}
}

// AddNode adds n unless a node with the same ID exists, and returns the stored node.
func (g *Graph) AddNode(n *Node) *Node {
	if old, ok := g.Nodes[n.ID]; ok {
		return old
	}
	g.Nodes[n.ID] = n
	return n
}

// AddEdge adds an edge, ignoring exact duplicates and self-loops. When the
// same edge is recorded with different kinds, the normal kind wins because a
// normal dependency explains the presence more strongly.
func (g *Graph) AddEdge(from, to, kind string) {
	if from == to {
		return
	}
	for i, e := range g.Edges[from] {
		if e.To == to {
			if kind == KindNormal {
				g.Edges[from][i].Kind = KindNormal
			}
			return
		}
	}
	g.Edges[from] = append(g.Edges[from], Edge{To: to, Kind: kind})
}

// Roots returns root node IDs in a stable order.
func (g *Graph) Roots() []string {
	var out []string
	for id, n := range g.Nodes {
		if n.Root {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

func (g *Graph) norm(s string) string {
	if g.Normalize != nil {
		return g.Normalize(s)
	}
	return s
}

// Match returns nodes whose name matches pattern, which may contain * and ?
// wildcards. Nodes are sorted by name, then version, then ID.
func (g *Graph) Match(pattern string) []*Node {
	pat := g.norm(pattern)
	var out []*Node
	for _, n := range g.Nodes {
		if Glob(pat, g.norm(n.Name)) || (n.AltName != "" && Glob(pat, g.norm(n.AltName))) {
			out = append(out, n)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		if a.Version != b.Version {
			return CompareVersions(a.Version, b.Version) < 0
		}
		return a.ID < b.ID
	})
	return out
}

// Suggest returns up to max distinct names that contain the query as a
// substring (after normalization and lowercasing).
func (g *Graph) Suggest(query string, max int) []string {
	q := strings.ToLower(g.norm(strings.Trim(query, "*?")))
	if q == "" {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, n := range g.Nodes {
		if seen[n.Name] {
			continue
		}
		if strings.Contains(strings.ToLower(g.norm(n.Name)), q) {
			seen[n.Name] = true
			out = append(out, n.Name)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if len(out[i]) != len(out[j]) {
			return len(out[i]) < len(out[j])
		}
		return out[i] < out[j]
	})
	if len(out) > max {
		out = out[:max]
	}
	return out
}

// Glob reports whether name matches pattern. '*' matches any run of
// characters (including '/'), '?' matches exactly one character. There is no
// escaping; package names do not contain those characters.
func Glob(pattern, name string) bool {
	p, n := []rune(pattern), []rune(name)
	pi, ni := 0, 0
	star, mark := -1, 0
	for ni < len(n) {
		switch {
		case pi < len(p) && (p[pi] == '?' || p[pi] == n[ni]):
			pi++
			ni++
		case pi < len(p) && p[pi] == '*':
			star, mark = pi, ni
			pi++
		case star >= 0:
			pi = star + 1
			mark++
			ni = mark
		default:
			return false
		}
	}
	for pi < len(p) && p[pi] == '*' {
		pi++
	}
	return pi == len(p)
}

// IsGlob reports whether s contains wildcard characters.
func IsGlob(s string) bool { return strings.ContainsAny(s, "*?") }
