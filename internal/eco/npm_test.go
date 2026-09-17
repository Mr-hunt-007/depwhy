package eco

import (
	"reflect"
	"strings"
	"testing"

	"github.com/Mr-hunt-007/depwhy/internal/graph"
)

// pathsOf renders every path to nodes named name as "a > b[kind] > c".
func pathsOf(t *testing.T, g *graph.Graph, name string) []string {
	t.Helper()
	var ids []string
	for _, n := range g.Match(name) {
		ids = append(ids, n.ID)
	}
	if len(ids) == 0 {
		t.Fatalf("no node named %q", name)
	}
	res := g.Paths(ids, 0, 0)
	var out []string
	for _, p := range res.Paths {
		var parts []string
		for _, s := range p {
			x := s.Node.Name
			if s.Node.Version != "" {
				x += "@" + s.Node.Version
			}
			if s.Kind != "" {
				x += "[" + s.Kind + "]"
			}
			parts = append(parts, x)
		}
		out = append(out, strings.Join(parts, " > "))
	}
	return out
}

// Trimmed from a real npm 11 lockfile (integrity and resolved fields removed).
const npmV3Lock = `{
  "name": "webapp",
  "version": "1.0.0",
  "lockfileVersion": 3,
  "requires": true,
  "packages": {
    "": {
      "name": "webapp",
      "version": "1.0.0",
      "workspaces": ["packages/*"],
      "dependencies": {"express": "4.21.0", "lib": "*", "string-width-cjs": "npm:string-width@^4.2.0"},
      "devDependencies": {"debug": "2.6.9", "react-dom": "18.3.1"},
      "optionalDependencies": {"fsevents": "2.3.3"}
    },
    "node_modules/debug": {"version": "2.6.9", "dependencies": {"ms": "2.0.0"}},
    "node_modules/express": {"version": "4.21.0", "dependencies": {"debug": "2.6.9", "send": "0.19.0"}},
    "node_modules/fsevents": {"version": "2.3.3", "optional": true, "os": ["darwin"]},
    "node_modules/lib": {"resolved": "packages/lib", "link": true},
    "node_modules/loose-envify": {"version": "1.4.0", "dev": true},
    "node_modules/ms": {"version": "2.0.0"},
    "node_modules/react": {"version": "18.3.1", "dev": true, "peer": true, "dependencies": {"loose-envify": "^1.1.0"}},
    "node_modules/react-dom": {"version": "18.3.1", "dev": true, "dependencies": {"loose-envify": "^1.1.0", "scheduler": "^0.23.2"}, "peerDependencies": {"react": "^18.3.1"}},
    "node_modules/scheduler": {"version": "0.23.2", "dev": true, "dependencies": {"loose-envify": "^1.1.0"}},
    "node_modules/send": {"version": "0.19.0", "dependencies": {"debug": "2.6.9", "ms": "2.1.3"}},
    "node_modules/send/node_modules/ms": {"version": "2.1.3"},
    "node_modules/string-width-cjs": {"name": "string-width", "version": "4.2.3"},
    "packages/lib": {"version": "0.2.0", "dependencies": {"ms": "2.1.3", "missing-optional": "1"}},
    "packages/lib/node_modules/ms": {"version": "2.1.3"}
  }
}`

func TestNPMv3(t *testing.T) {
	g, err := BuildNPM("package-lock.json", []byte(npmV3Lock), nil, "dir")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		want []string
	}{
		{"ms", []string{
			// ms@2.1.3 under lib and under send; ms@2.0.0 hoisted.
			"lib@0.2.0 > ms@2.1.3",
			"webapp@1.0.0 > debug@2.6.9[dev] > ms@2.0.0",
			"webapp@1.0.0 > express@4.21.0 > send@0.19.0 > ms@2.1.3",
			"webapp@1.0.0 > express@4.21.0 > debug@2.6.9 > ms@2.0.0",
			"webapp@1.0.0 > express@4.21.0 > send@0.19.0 > debug@2.6.9 > ms@2.0.0",
		}},
		{"react", []string{"webapp@1.0.0 > react-dom@18.3.1[dev] > react@18.3.1[peer]"}},
		{"fsevents", []string{"webapp@1.0.0 > fsevents@2.3.3[optional]"}},
		{"lib", []string{"lib@0.2.0"}},
		{"string-width", []string{"webapp@1.0.0 > string-width-cjs@4.2.3"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pathsOf(t, g, tt.name)
			if !sameSet(got, tt.want) {
				t.Errorf("got  %q\nwant %q", got, tt.want)
			}
		})
	}
	if n := g.Nodes["node_modules/react"]; !reflect.DeepEqual(n.Flags, []string{"dev", "peer"}) {
		t.Errorf("react flags = %v", n.Flags)
	}
	if n := g.Nodes["node_modules/string-width-cjs"]; n.AltName != "string-width" {
		t.Errorf("alias = %+v", n)
	}
	if !g.Nodes["packages/lib"].Root || !g.Nodes[""].Root {
		t.Error("root and workspace should be roots")
	}
}

func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	count := map[string]int{}
	for _, s := range a {
		count[s]++
	}
	for _, s := range b {
		count[s]--
	}
	for _, v := range count {
		if v != 0 {
			return false
		}
	}
	return true
}

func TestNPMResolveWalksUp(t *testing.T) {
	pkgs := map[string]*npmPackage{
		"node_modules/a":                                  {},
		"node_modules/b":                                  {},
		"node_modules/a/node_modules/b":                   {},
		"node_modules/@s/c":                               {},
		"node_modules/@s/c/node_modules/d":                {},
		"node_modules/a/node_modules/b/node_modules/e":    {},
		"packages/w/node_modules/b":                       {},
		"node_modules/a/node_modules/@s/x":                {},
		"node_modules/a/node_modules/@s/x/node_modules/y": {},
	}
	tests := []struct{ from, name, want string }{
		{"", "a", "node_modules/a"},
		{"node_modules/a", "b", "node_modules/a/node_modules/b"},
		{"node_modules/a/node_modules/b", "b", "node_modules/a/node_modules/b"},
		{"node_modules/a/node_modules/b", "a", "node_modules/a"},
		{"node_modules/@s/c", "b", "node_modules/b"},
		{"node_modules/@s/c", "d", "node_modules/@s/c/node_modules/d"},
		{"node_modules/@s/c", "e", ""},
		{"packages/w", "b", "packages/w/node_modules/b"},
		{"packages/w", "a", "node_modules/a"},
		{"node_modules/a/node_modules/@s/x", "b", "node_modules/a/node_modules/b"},
		{"../outside", "a", "node_modules/a"},
	}
	for _, tt := range tests {
		if got := npmResolve(pkgs, tt.from, tt.name); got != tt.want {
			t.Errorf("npmResolve(%q, %q) = %q, want %q", tt.from, tt.name, got, tt.want)
		}
	}
}

// Trimmed from a real lockfileVersion 1 file written by npm with --lockfile-version 1.
const npmV1Lock = `{
  "name": "old",
  "version": "1.0.0",
  "lockfileVersion": 1,
  "requires": true,
  "dependencies": {
    "debug": {"version": "2.6.9", "requires": {"ms": "2.0.0"}},
    "express": {"version": "4.21.0", "requires": {"debug": "2.6.9", "send": "0.19.0"}},
    "fsevents": {"version": "2.3.3", "optional": true},
    "ms": {"version": "2.0.0"},
    "send": {
      "version": "0.19.0",
      "requires": {"debug": "2.6.9", "ms": "2.1.3"},
      "dependencies": {"ms": {"version": "2.1.3"}}
    }
  }
}`

func TestNPMv1WithPackageJSON(t *testing.T) {
	pkg := `{"name":"old","version":"1.0.0","dependencies":{"express":"4.21.0"},"devDependencies":{"debug":"2.6.9"},"optionalDependencies":{"fsevents":"2.3.3"}}`
	g, err := BuildNPM("package-lock.json", []byte(npmV1Lock), []byte(pkg), "dir")
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Warnings) != 0 {
		t.Errorf("warnings: %v", g.Warnings)
	}
	got := pathsOf(t, g, "ms")
	want := []string{
		"old@1.0.0 > debug@2.6.9[dev] > ms@2.0.0",
		"old@1.0.0 > express@4.21.0 > send@0.19.0 > ms@2.1.3",
		"old@1.0.0 > express@4.21.0 > debug@2.6.9 > ms@2.0.0",
		"old@1.0.0 > express@4.21.0 > send@0.19.0 > debug@2.6.9 > ms@2.0.0",
	}
	if !sameSet(got, want) {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestNPMv1WithoutPackageJSON(t *testing.T) {
	g, err := BuildNPM("package-lock.json", []byte(npmV1Lock), nil, "dir")
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Warnings) != 1 || !strings.Contains(g.Warnings[0], "package.json not found") {
		t.Errorf("warnings: %v", g.Warnings)
	}
	got := pathsOf(t, g, "fsevents")
	if !sameSet(got, []string{"old@1.0.0 > fsevents@2.3.3"}) {
		t.Errorf("got %q", got)
	}
}

func TestNPMErrors(t *testing.T) {
	if _, err := BuildNPM("package-lock.json", []byte(`{"x":`), nil, "d"); err == nil || !strings.Contains(err.Error(), "invalid JSON") {
		t.Errorf("err = %v", err)
	}
	if _, err := BuildNPM("package-lock.json", []byte(`{"name":"x"}`), nil, "d"); err == nil || !strings.Contains(err.Error(), "not an npm lockfile") {
		t.Errorf("err = %v", err)
	}
}
