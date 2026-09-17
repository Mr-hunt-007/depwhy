package eco

import (
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/Mr-hunt-007/depwhy/internal/graph"
)

type npmPackage struct {
	Name                 string            `json:"name"`
	Version              string            `json:"version"`
	Resolved             string            `json:"resolved"`
	Link                 bool              `json:"link"`
	Dev                  bool              `json:"dev"`
	Optional             bool              `json:"optional"`
	DevOptional          bool              `json:"devOptional"`
	Peer                 bool              `json:"peer"`
	Extraneous           bool              `json:"extraneous"`
	InBundle             bool              `json:"inBundle"`
	Workspaces           json.RawMessage   `json:"workspaces"`
	Dependencies         map[string]string `json:"dependencies"`
	DevDependencies      map[string]string `json:"devDependencies"`
	OptionalDependencies map[string]string `json:"optionalDependencies"`
	PeerDependencies     map[string]string `json:"peerDependencies"`
}

type npmV1Dep struct {
	Version      string               `json:"version"`
	Dev          bool                 `json:"dev"`
	Optional     bool                 `json:"optional"`
	Bundled      bool                 `json:"bundled"`
	Requires     map[string]string    `json:"requires"`
	Dependencies map[string]*npmV1Dep `json:"dependencies"`
}

type npmLockfile struct {
	Name            string                 `json:"name"`
	Version         string                 `json:"version"`
	LockfileVersion int                    `json:"lockfileVersion"`
	Packages        map[string]*npmPackage `json:"packages"`
	Dependencies    map[string]*npmV1Dep   `json:"dependencies"`
}

// BuildNPM builds a graph from package-lock.json (or npm-shrinkwrap.json)
// content. pkgJSON is the project's package.json, used for the root's direct
// dependencies of v1 lockfiles; it may be nil. dirName names the root when
// neither file records a name.
func BuildNPM(source string, lock, pkgJSON []byte, dirName string) (*graph.Graph, error) {
	var lf npmLockfile
	if err := json.Unmarshal(lock, &lf); err != nil {
		return nil, fmt.Errorf("%s: invalid JSON: %v", source, err)
	}
	g := graph.New("npm", source)
	if lf.Packages != nil {
		if err := buildNPMPackages(g, &lf, dirName); err != nil {
			return nil, err
		}
		return g, nil
	}
	if lf.Dependencies == nil && lf.LockfileVersion == 0 {
		return nil, fmt.Errorf("%s: no \"packages\" or \"dependencies\" field; not an npm lockfile", source)
	}
	var root *npmPackage
	if pkgJSON != nil {
		root = &npmPackage{}
		if err := json.Unmarshal(pkgJSON, root); err != nil {
			g.Warnings = append(g.Warnings, fmt.Sprintf("package.json: invalid JSON (%v); root dependencies inferred from the lockfile", err))
			root = nil
		}
	}
	lf.Packages = map[string]*npmPackage{}
	flattenV1(lf.Packages, "", lf.Dependencies)
	if root == nil {
		if pkgJSON == nil {
			g.Warnings = append(g.Warnings, "package.json not found next to a lockfile v1; root dependencies inferred as top-level packages nothing else requires")
		}
		root = inferV1Root(&lf)
	}
	if root.Name == "" {
		root.Name = lf.Name
	}
	if root.Version == "" {
		root.Version = lf.Version
	}
	root.Workspaces = nil
	lf.Packages[""] = root
	return g, buildNPMPackages(g, &lf, dirName)
}

func flattenV1(out map[string]*npmPackage, prefix string, deps map[string]*npmV1Dep) {
	for name, d := range deps {
		if d == nil {
			continue
		}
		loc := name
		if prefix != "" {
			loc = prefix + "/node_modules/" + name
		} else {
			loc = "node_modules/" + name
		}
		out[loc] = &npmPackage{
			Version:      d.Version,
			Dev:          d.Dev,
			Optional:     d.Optional,
			InBundle:     d.Bundled,
			Dependencies: d.Requires,
		}
		flattenV1(out, loc, d.Dependencies)
	}
}

func inferV1Root(lf *npmLockfile) *npmPackage {
	required := map[string]bool{}
	for _, p := range lf.Packages {
		for n := range p.Dependencies {
			required[n] = true
		}
	}
	root := &npmPackage{Dependencies: map[string]string{}, DevDependencies: map[string]string{}}
	for name, d := range lf.Dependencies {
		if d == nil || required[name] {
			continue
		}
		if d.Dev {
			root.DevDependencies[name] = d.Version
		} else {
			root.Dependencies[name] = d.Version
		}
	}
	return root
}

// npmNameFromLocation returns the package name installed at a location such as
// "node_modules/a/node_modules/@scope/b".
func npmNameFromLocation(loc string) string {
	i := strings.LastIndex(loc, "node_modules/")
	if i < 0 {
		return path.Base(loc)
	}
	return loc[i+len("node_modules/"):]
}

func isNodeModulesLocation(loc string) bool {
	return strings.HasPrefix(loc, "node_modules/") || strings.Contains(loc, "/node_modules/")
}

// npmResolve applies Node's module resolution: look in from/node_modules,
// then in each ancestor directory's node_modules.
func npmResolve(pkgs map[string]*npmPackage, from, name string) string {
	dir := from
	for {
		if path.Base(dir) != "node_modules" {
			cand := "node_modules/" + name
			if dir != "" {
				cand = dir + "/node_modules/" + name
			}
			if _, ok := pkgs[cand]; ok {
				return cand
			}
		}
		if dir == "" {
			return ""
		}
		dir = path.Dir(dir)
		if dir == "." || dir == "/" || dir == ".." {
			dir = ""
		}
	}
}

func workspacePatterns(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var list []string
	if json.Unmarshal(raw, &list) == nil {
		return list
	}
	var obj struct {
		Packages []string `json:"packages"`
	}
	if json.Unmarshal(raw, &obj) == nil {
		return obj.Packages
	}
	return nil
}

func matchWorkspace(patterns []string, loc string) bool {
	for _, p := range patterns {
		p = strings.TrimSuffix(strings.TrimPrefix(p, "./"), "/")
		if strings.HasSuffix(p, "/**") {
			if strings.HasPrefix(loc, strings.TrimSuffix(p, "**")) {
				return true
			}
			continue
		}
		if ok, _ := path.Match(p, loc); ok {
			return true
		}
	}
	return false
}

func buildNPMPackages(g *graph.Graph, lf *npmLockfile, dirName string) error {
	pkgs := lf.Packages
	rootPkg := pkgs[""]
	var wsPatterns []string
	if rootPkg != nil {
		wsPatterns = workspacePatterns(rootPkg.Workspaces)
	}
	// Names for link targets come from the link location.
	linkName := map[string]string{}
	for loc, p := range pkgs {
		if p != nil && p.Link && p.Resolved != "" {
			linkName[strings.TrimPrefix(p.Resolved, "./")] = npmNameFromLocation(loc)
		}
	}
	locs := make([]string, 0, len(pkgs))
	for loc, p := range pkgs {
		if p != nil && !p.Link {
			locs = append(locs, loc)
		}
	}
	sort.Strings(locs)
	for _, loc := range locs {
		p := pkgs[loc]
		n := &graph.Node{ID: loc, Version: p.Version}
		installName := ""
		switch {
		case loc == "":
			installName = p.Name
			if installName == "" {
				installName = lf.Name
			}
			if installName == "" {
				installName = dirName
			}
			n.Root = true
		case isNodeModulesLocation(loc):
			installName = npmNameFromLocation(loc)
			if p.Name != "" && p.Name != installName {
				n.AltName = p.Name
				n.Flags = append(n.Flags, "alias:"+p.Name)
			}
		default:
			installName = p.Name
			if installName == "" {
				installName = linkName[loc]
			}
			if installName == "" {
				installName = path.Base(loc)
			}
			n.Root = matchWorkspace(wsPatterns, loc)
		}
		n.Name = installName
		if p.Dev {
			n.Flags = append(n.Flags, "dev")
		}
		if p.Optional {
			n.Flags = append(n.Flags, "optional")
		}
		if p.DevOptional {
			n.Flags = append(n.Flags, "devOptional")
		}
		if p.Peer {
			n.Flags = append(n.Flags, "peer")
		}
		if p.InBundle {
			n.Flags = append(n.Flags, "bundled")
		}
		if p.Extraneous {
			n.Flags = append(n.Flags, "extraneous")
		}
		g.AddNode(n)
	}
	for _, loc := range locs {
		p := pkgs[loc]
		lists := []struct {
			deps map[string]string
			kind string
		}{
			{p.Dependencies, graph.KindNormal},
			{p.OptionalDependencies, graph.KindOptional},
			{p.PeerDependencies, graph.KindPeer},
			{p.DevDependencies, graph.KindDev},
		}
		for _, l := range lists {
			names := make([]string, 0, len(l.deps))
			for name := range l.deps {
				names = append(names, name)
			}
			sort.Strings(names)
			for _, name := range names {
				target := npmResolve(pkgs, loc, name)
				if target == "" {
					continue // optional or peer dependency that is not installed
				}
				if tp := pkgs[target]; tp != nil && tp.Link {
					target = strings.TrimPrefix(tp.Resolved, "./")
				}
				if _, ok := g.Nodes[target]; !ok {
					continue
				}
				g.AddEdge(loc, target, l.kind)
			}
		}
	}
	return nil
}
