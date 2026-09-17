package eco

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Mr-hunt-007/depwhy/internal/graph"
	"github.com/Mr-hunt-007/depwhy/internal/toml"
)

// PEP503 normalizes a Python distribution name: lowercase, with runs of
// "-", "_" and "." collapsed to a single "-".
func PEP503(name string) string {
	var b strings.Builder
	sep := false
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		if r == '-' || r == '_' || r == '.' {
			sep = true
			continue
		}
		if sep && b.Len() > 0 {
			b.WriteByte('-')
		}
		sep = false
		b.WriteRune(r)
	}
	return b.String()
}

// pep508Name extracts the distribution name from a requirement string such as
// "requests[socks]>=2.0; python_version < '3.8'".
func pep508Name(req string) string {
	req = strings.TrimSpace(req)
	end := 0
	for end < len(req) {
		c := req[end]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.' {
			end++
			continue
		}
		break
	}
	return req[:end]
}

func groupKind(group string) string {
	if PEP503(group) == "dev" {
		return graph.KindDev
	}
	return "group:" + group
}

// pyRootDep is a direct dependency of the project declared in pyproject.toml.
type pyRootDep struct {
	name string
	kind string
}

// PyProject holds what depwhy uses from pyproject.toml.
type PyProject struct {
	Name    string
	Version string
	deps    []pyRootDep
}

// ParsePyProject reads project name and direct dependencies from
// pyproject.toml: [project] dependencies and optional-dependencies,
// [tool.poetry] dependencies, dev-dependencies and groups, and PEP 735
// [dependency-groups].
func ParsePyProject(data []byte) (*PyProject, error) {
	doc, err := toml.Parse(data)
	if err != nil {
		return nil, err
	}
	pp := &PyProject{
		Name:    toml.String(doc, "project", "name"),
		Version: toml.String(doc, "project", "version"),
	}
	if pp.Name == "" {
		pp.Name = toml.String(doc, "tool", "poetry", "name")
	}
	if pp.Version == "" {
		pp.Version = toml.String(doc, "tool", "poetry", "version")
	}
	add := func(name, kind string) {
		if name != "" && PEP503(name) != "python" {
			pp.deps = append(pp.deps, pyRootDep{name: name, kind: kind})
		}
	}
	for _, req := range toml.Strings(doc, "project", "dependencies") {
		add(pep508Name(req), graph.KindNormal)
	}
	for _, extra := range sortedKeys(toml.Table(doc, "project", "optional-dependencies")) {
		for _, req := range toml.Strings(doc, "project", "optional-dependencies", extra) {
			add(pep508Name(req), "extra:"+extra)
		}
	}
	poetryTable := func(tbl map[string]any, kind string) {
		for _, name := range sortedKeys(tbl) {
			k := kind
			if t, ok := tbl[name].(map[string]any); ok {
				if opt, _ := t["optional"].(bool); opt && kind == graph.KindNormal {
					k = graph.KindOptional
				}
			}
			add(name, k)
		}
	}
	poetryTable(toml.Table(doc, "tool", "poetry", "dependencies"), graph.KindNormal)
	poetryTable(toml.Table(doc, "tool", "poetry", "dev-dependencies"), graph.KindDev)
	groups := toml.Table(doc, "tool", "poetry", "group")
	for _, g := range sortedKeys(groups) {
		poetryTable(toml.Table(groups, g, "dependencies"), groupKind(g))
	}
	depGroups := toml.Table(doc, "dependency-groups")
	for _, g := range sortedKeys(depGroups) {
		for _, name := range expandDependencyGroup(depGroups, g, map[string]bool{}) {
			add(name, groupKind(g))
		}
	}
	for _, name := range toml.Strings(doc, "tool", "uv", "dev-dependencies") {
		add(pep508Name(name), graph.KindDev)
	}
	return pp, nil
}

// expandDependencyGroup resolves a PEP 735 group, following include-group
// entries without looping.
func expandDependencyGroup(groups map[string]any, name string, visiting map[string]bool) []string {
	key := ""
	for k := range groups {
		if PEP503(k) == PEP503(name) {
			key = k
		}
	}
	if key == "" || visiting[PEP503(name)] {
		return nil
	}
	visiting[PEP503(name)] = true
	defer delete(visiting, PEP503(name))
	var out []string
	list, _ := groups[key].([]any)
	for _, e := range list {
		switch v := e.(type) {
		case string:
			out = append(out, pep508Name(v))
		case map[string]any:
			if inc, ok := v["include-group"].(string); ok {
				out = append(out, expandDependencyGroup(groups, inc, visiting)...)
			}
		}
	}
	return out
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// addPyProjectRoot adds a synthetic root node for the project and edges to its
// declared dependencies.
func addPyProjectRoot(g *graph.Graph, pp *PyProject, byName map[string][]string, dirName string) {
	name := pp.Name
	if name == "" {
		name = dirName
	}
	id := "pyproject:" + name
	g.AddNode(&graph.Node{ID: id, Name: name, Version: pp.Version, Root: true})
	missing := 0
	for _, d := range pp.deps {
		targets := byName[PEP503(d.name)]
		if len(targets) == 0 {
			missing++
		}
		for _, t := range targets {
			g.AddEdge(id, t, d.kind)
		}
	}
	if missing > 0 {
		g.Warnings = append(g.Warnings, fmt.Sprintf("%s: %d dependencies declared in pyproject.toml are not in the lockfile (stale lock, or a platform-specific marker)", g.Source, missing))
	}
}

// markOrphanRoots makes every node without incoming edges a root. It is the
// fallback when the project itself cannot be identified.
func markOrphanRoots(g *graph.Graph) {
	incoming := map[string]bool{}
	for _, edges := range g.Edges {
		for _, e := range edges {
			incoming[e.To] = true
		}
	}
	for id, n := range g.Nodes {
		if !incoming[id] {
			n.Root = true
		}
	}
}

func sourceKey(v any) string {
	t, ok := v.(map[string]any)
	if !ok {
		return ""
	}
	var parts []string
	for _, k := range sortedKeys(t) {
		parts = append(parts, fmt.Sprintf("%s=%v", k, t[k]))
	}
	return strings.Join(parts, ",")
}

// BuildUV builds a graph from uv.lock content. pp may be nil.
func BuildUV(source string, lock []byte, pp *PyProject, dirName string) (*graph.Graph, error) {
	doc, err := toml.Parse(lock)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", source, err)
	}
	tables := toml.Tables(doc, "package")
	if len(tables) == 0 {
		return nil, fmt.Errorf("%s: no [[package]] entries", source)
	}
	g := graph.New("python", source)
	g.Normalize = PEP503

	type uvPkg struct {
		id, name, version, src string
		table                  map[string]any
	}
	var pkgs []uvPkg
	byName := map[string][]string{}
	members := map[string]bool{}
	for _, m := range toml.Strings(doc, "manifest", "members") {
		members[PEP503(m)] = true
	}
	for i, t := range tables {
		name, _ := t["name"].(string)
		if name == "" {
			return nil, fmt.Errorf("%s: package #%d has no name", source, i+1)
		}
		version, _ := t["version"].(string)
		src := sourceKey(t["source"])
		id := PEP503(name) + " " + version + " " + src
		pkgs = append(pkgs, uvPkg{id: id, name: name, version: version, src: src, table: t})
		byName[PEP503(name)] = append(byName[PEP503(name)], id)
		n := g.AddNode(&graph.Node{ID: id, Name: name, Version: version})
		srcTable, _ := t["source"].(map[string]any)
		if members[PEP503(name)] {
			n.Root = true
		} else if len(members) == 0 {
			for _, k := range []string{"editable", "virtual"} {
				if v, ok := srcTable[k].(string); ok && (v == "." || v == "./") {
					n.Root = true
				}
			}
		}
	}
	pkgByID := map[string]*uvPkg{}
	for i := range pkgs {
		pkgByID[pkgs[i].id] = &pkgs[i]
	}
	resolve := func(dep map[string]any) []string {
		name, _ := dep["name"].(string)
		version, hasVersion := dep["version"].(string)
		src := sourceKey(dep["source"])
		var out []string
		for _, id := range byName[PEP503(name)] {
			p := pkgByID[id]
			if hasVersion && p.version != version {
				continue
			}
			if src != "" && p.src != src {
				continue
			}
			out = append(out, id)
		}
		return out
	}
	unresolved := 0
	addDeps := func(from string, list []any, kind string) {
		for _, e := range list {
			dep, ok := e.(map[string]any)
			if !ok {
				continue
			}
			targets := resolve(dep)
			if len(targets) == 0 {
				unresolved++
			}
			for _, t := range targets {
				g.AddEdge(from, t, kind)
			}
		}
	}
	for i := range pkgs {
		p := &pkgs[i]
		addDeps(p.id, toml.Array(p.table, "dependencies"), graph.KindNormal)
		opt := toml.Table(p.table, "optional-dependencies")
		for _, extra := range sortedKeys(opt) {
			addDeps(p.id, toml.Array(opt, extra), "extra:"+extra)
		}
		dev := toml.Table(p.table, "dev-dependencies")
		for _, grp := range sortedKeys(dev) {
			addDeps(p.id, toml.Array(dev, grp), groupKind(grp))
		}
	}
	if unresolved > 0 {
		g.Warnings = append(g.Warnings, fmt.Sprintf("%s: %d dependency entries did not match any package", source, unresolved))
	}
	if len(g.Roots()) == 0 {
		if pp != nil {
			addPyProjectRoot(g, pp, byName, dirName)
		} else {
			g.Warnings = append(g.Warnings, source+": project package not found in lockfile and no pyproject.toml; packages nothing depends on are treated as roots")
			markOrphanRoots(g)
		}
	}
	return g, nil
}

// BuildPoetry builds a graph from poetry.lock content. pp may be nil.
func BuildPoetry(source string, lock []byte, pp *PyProject, dirName string) (*graph.Graph, error) {
	doc, err := toml.Parse(lock)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", source, err)
	}
	tables := toml.Tables(doc, "package")
	g := graph.New("python", source)
	g.Normalize = PEP503
	byName := map[string][]string{}
	for i, t := range tables {
		name, _ := t["name"].(string)
		if name == "" {
			return nil, fmt.Errorf("%s: package #%d has no name", source, i+1)
		}
		version, _ := t["version"].(string)
		id := PEP503(name) + " " + version
		if _, dup := g.Nodes[id]; dup {
			id += fmt.Sprintf(" #%d", i)
		}
		n := g.AddNode(&graph.Node{ID: id, Name: name, Version: version})
		byName[PEP503(name)] = append(byName[PEP503(name)], id)
		if opt, _ := t["optional"].(bool); opt {
			n.Flags = append(n.Flags, "optional")
		}
		if cat, _ := t["category"].(string); cat != "" && cat != "main" {
			n.Flags = append(n.Flags, cat)
		}
		if groups := toml.Strings(t, "groups"); len(groups) > 0 {
			hasMain := false
			for _, gr := range groups {
				if gr == "main" {
					hasMain = true
				}
			}
			if !hasMain {
				if len(groups) == 1 && PEP503(groups[0]) == "dev" {
					n.Flags = append(n.Flags, "dev")
				} else {
					n.Flags = append(n.Flags, "group:"+strings.Join(groups, ","))
				}
			}
		}
	}
	for i, t := range tables {
		name, _ := t["name"].(string)
		version, _ := t["version"].(string)
		from := PEP503(name) + " " + version
		if len(byName[PEP503(name)]) > 1 {
			from = byName[PEP503(name)][countBefore(tables, i, name)]
		}
		deps := toml.Table(t, "dependencies")
		for _, dn := range sortedKeys(deps) {
			kind := graph.KindNormal
			if isOptionalPoetryDep(deps[dn]) {
				kind = graph.KindOptional
			}
			for _, target := range byName[PEP503(dn)] {
				g.AddEdge(from, target, kind)
			}
		}
	}
	if pp != nil {
		addPyProjectRoot(g, pp, byName, dirName)
	} else {
		g.Warnings = append(g.Warnings, source+": pyproject.toml not found; packages nothing depends on are treated as roots")
		markOrphanRoots(g)
	}
	return g, nil
}

// countBefore returns how many earlier packages share the normalized name.
func countBefore(tables []map[string]any, i int, name string) int {
	n := 0
	for j := 0; j < i; j++ {
		if other, _ := tables[j]["name"].(string); PEP503(other) == PEP503(name) {
			n++
		}
	}
	return n
}

// isOptionalPoetryDep reports whether a [package.dependencies] value is only
// installed through an extra. Values are a constraint string, a table, or an
// array of tables for multiple constraints.
func isOptionalPoetryDep(v any) bool {
	switch x := v.(type) {
	case map[string]any:
		opt, _ := x["optional"].(bool)
		return opt
	case []any:
		if len(x) == 0 {
			return false
		}
		for _, e := range x {
			if !isOptionalPoetryDep(e) {
				return false
			}
		}
		return true
	}
	return false
}
