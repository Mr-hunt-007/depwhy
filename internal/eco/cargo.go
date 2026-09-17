package eco

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Mr-hunt-007/depwhy/internal/graph"
	"github.com/Mr-hunt-007/depwhy/internal/toml"
)

// CargoMember is a workspace member crate read from Cargo.toml, with the
// declared kind of each of its direct dependencies ("" normal, "build", "dev").
type CargoMember struct {
	Name     string
	DepKinds map[string]string // keyed by normalized crate name
}

func cargoNorm(s string) string { return strings.ReplaceAll(s, "-", "_") }

type cargoPkg struct {
	id, name, version, source string
	deps                      []string
}

// BuildCargo builds a graph from Cargo.lock content. members may be nil when
// no Cargo.toml could be read.
func BuildCargo(source string, lock []byte, members []CargoMember) (*graph.Graph, error) {
	doc, err := toml.Parse(lock)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", source, err)
	}
	var pkgs []cargoPkg
	tables := toml.Tables(doc, "package")
	if root := toml.Table(doc, "root"); root != nil { // lockfile format v1
		tables = append(tables, root)
	}
	if len(tables) == 0 {
		return nil, fmt.Errorf("%s: no [[package]] entries", source)
	}
	for i, t := range tables {
		name, _ := t["name"].(string)
		version, _ := t["version"].(string)
		if name == "" {
			return nil, fmt.Errorf("%s: package #%d has no name", source, i+1)
		}
		src, _ := t["source"].(string)
		p := cargoPkg{name: name, version: version, source: src, id: name + " " + version}
		if src != "" {
			p.id += " (" + src + ")"
		}
		for _, d := range toml.Array(t, "dependencies") {
			s, ok := d.(string)
			if !ok {
				return nil, fmt.Errorf("%s: package %s: dependency entry is not a string", source, name)
			}
			p.deps = append(p.deps, s)
		}
		pkgs = append(pkgs, p)
	}

	g := graph.New("cargo", source)
	g.Normalize = cargoNorm
	byName := map[string][]*cargoPkg{}
	for i := range pkgs {
		p := &pkgs[i]
		g.AddNode(&graph.Node{ID: p.id, Name: p.name, Version: p.version})
		byName[p.name] = append(byName[p.name], p)
	}

	memberKinds := map[string]map[string]string{}
	for _, m := range members {
		memberKinds[m.Name] = m.DepKinds
	}
	incoming := map[string]bool{}
	unresolved := 0
	for i := range pkgs {
		p := &pkgs[i]
		for _, dep := range p.deps {
			targets := resolveCargoDep(byName, dep)
			if len(targets) == 0 {
				unresolved++
				continue
			}
			kind := graph.KindNormal
			if p.source == "" {
				if kinds, ok := memberKinds[p.name]; ok {
					kind = kinds[cargoNorm(targets[0].name)]
				}
			}
			for _, t := range targets {
				g.AddEdge(p.id, t.id, kind)
				incoming[t.id] = true
			}
		}
	}
	if unresolved > 0 {
		g.Warnings = append(g.Warnings, fmt.Sprintf("%s: %d dependency entries did not match any package", source, unresolved))
	}

	// Roots: workspace members, plus local crates nothing depends on.
	for i := range pkgs {
		p := &pkgs[i]
		if p.source != "" {
			continue
		}
		if _, ok := memberKinds[p.name]; ok || !incoming[p.id] {
			g.Nodes[p.id].Root = true
		}
	}
	if len(g.Roots()) == 0 {
		for i := range pkgs {
			if pkgs[i].source == "" {
				g.Nodes[pkgs[i].id].Root = true
			}
		}
	}
	return g, nil
}

// resolveCargoDep resolves "name", "name version" or "name version (source)".
func resolveCargoDep(byName map[string][]*cargoPkg, dep string) []*cargoPkg {
	name, rest, _ := strings.Cut(strings.TrimSpace(dep), " ")
	version, src, _ := strings.Cut(strings.TrimSpace(rest), " ")
	src = strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(src), "("), ")")
	var out []*cargoPkg
	for _, p := range byName[name] {
		if version != "" && p.version != version {
			continue
		}
		if src != "" && p.source != src {
			continue
		}
		out = append(out, p)
	}
	return out
}

// CargoMembers reads the root Cargo.toml and every workspace member manifest.
// readFile is relative to the project directory.
func CargoMembers(readFile func(rel string) ([]byte, error), glob func(pattern string) ([]string, error)) ([]CargoMember, []string) {
	var warnings []string
	data, err := readFile("Cargo.toml")
	if err != nil {
		return nil, []string{"Cargo.toml not found; dev and build dependency markers are unavailable and roots are inferred from Cargo.lock"}
	}
	rootDoc, err := toml.Parse(data)
	if err != nil {
		return nil, []string{fmt.Sprintf("Cargo.toml: %v; dev and build dependency markers are unavailable and roots are inferred from Cargo.lock", err)}
	}
	wsDeps := toml.Table(rootDoc, "workspace", "dependencies")
	var members []CargoMember
	if name := toml.String(rootDoc, "package", "name"); name != "" {
		members = append(members, CargoMember{Name: name, DepKinds: cargoDepKinds(rootDoc, wsDeps)})
	}
	excluded := map[string]bool{}
	for _, e := range toml.Strings(rootDoc, "workspace", "exclude") {
		excluded[filepath.Clean(filepath.FromSlash(e))] = true
	}
	seen := map[string]bool{".": true}
	for _, pat := range toml.Strings(rootDoc, "workspace", "members") {
		dirs, err := glob(filepath.FromSlash(pat))
		if err != nil || len(dirs) == 0 {
			warnings = append(warnings, fmt.Sprintf("Cargo.toml: workspace member %q matched no directory", pat))
			continue
		}
		sort.Strings(dirs)
		for _, d := range dirs {
			d = filepath.Clean(d)
			if seen[d] || excluded[d] {
				continue
			}
			seen[d] = true
			rel := filepath.Join(d, "Cargo.toml")
			md, err := readFile(rel)
			if err != nil {
				continue // globs can match directories that are not crates
			}
			doc, err := toml.Parse(md)
			if err != nil {
				warnings = append(warnings, fmt.Sprintf("%s: %v", filepath.ToSlash(rel), err))
				continue
			}
			if name := toml.String(doc, "package", "name"); name != "" {
				members = append(members, CargoMember{Name: name, DepKinds: cargoDepKinds(doc, wsDeps)})
			}
		}
	}
	return members, warnings
}

// cargoDepKinds maps each dependency crate of a manifest to its strongest
// declared kind: normal beats build beats dev.
func cargoDepKinds(doc, wsDeps map[string]any) map[string]string {
	rank := map[string]int{graph.KindNormal: 3, graph.KindBuild: 2, graph.KindDev: 1}
	out := map[string]string{}
	add := func(tbl map[string]any, kind string) {
		for key, v := range tbl {
			name := key
			if t, ok := v.(map[string]any); ok {
				if pkg, ok := t["package"].(string); ok {
					name = pkg
				} else if ws, _ := t["workspace"].(bool); ws {
					if wt, ok := wsDeps[key].(map[string]any); ok {
						if pkg, ok := wt["package"].(string); ok {
							name = pkg
						}
					}
				}
			}
			n := cargoNorm(name)
			if old, ok := out[n]; !ok || rank[kind] > rank[old] {
				out[n] = kind
			}
		}
	}
	sections := []struct{ key, kind string }{
		{"dependencies", graph.KindNormal},
		{"build-dependencies", graph.KindBuild},
		{"build_dependencies", graph.KindBuild},
		{"dev-dependencies", graph.KindDev},
		{"dev_dependencies", graph.KindDev},
	}
	for _, s := range sections {
		add(toml.Table(doc, s.key), s.kind)
	}
	for _, target := range toml.Table(doc, "target") {
		t, ok := target.(map[string]any)
		if !ok {
			continue
		}
		for _, s := range sections {
			add(toml.Table(t, s.key), s.kind)
		}
	}
	return out
}
