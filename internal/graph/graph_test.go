package graph

import (
	"reflect"
	"strings"
	"testing"
)

func pathString(p []Step) string {
	var parts []string
	for _, s := range p {
		x := s.Node.Name
		if s.Kind != "" {
			x += "[" + s.Kind + "]"
		}
		parts = append(parts, x)
	}
	return strings.Join(parts, " > ")
}

func build(roots []string, edges ...[3]string) *Graph {
	g := New("test", "test.lock")
	add := func(id string) {
		g.AddNode(&Node{ID: id, Name: id, Version: "1.0.0"})
	}
	for _, r := range roots {
		add(r)
		g.Nodes[r].Root = true
	}
	for _, e := range edges {
		add(e[0])
		add(e[1])
		g.AddEdge(e[0], e[1], e[2])
	}
	return g
}

func TestPathsShortestFirstAndKinds(t *testing.T) {
	g := build([]string{"app"},
		[3]string{"app", "a", ""},
		[3]string{"app", "b", "dev"},
		[3]string{"a", "c", ""},
		[3]string{"b", "c", ""},
		[3]string{"app", "c", "optional"},
		[3]string{"c", "d", ""},
	)
	res := g.Paths([]string{"c"}, 0, 0)
	var got []string
	for _, p := range res.Paths {
		got = append(got, pathString(p))
	}
	want := []string{"app > c[optional]", "app > a > c", "app > b[dev] > c"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %q\nwant %q", got, want)
	}
	if res.Total != 3 || !res.Complete {
		t.Errorf("total=%d complete=%v", res.Total, res.Complete)
	}
}

func TestPathsCycleSafe(t *testing.T) {
	g := build([]string{"root"},
		[3]string{"root", "a", ""},
		[3]string{"a", "b", ""},
		[3]string{"b", "a", ""},
		[3]string{"b", "c", ""},
		[3]string{"c", "b", ""},
	)
	res := g.Paths([]string{"c"}, 0, 0)
	if res.Total != 1 || pathString(res.Paths[0]) != "root > a > b > c" {
		t.Fatalf("got %+v", res)
	}
}

func TestPathsMaxAndTotal(t *testing.T) {
	// Diamond chain: 2^4 paths from root to end.
	g := build([]string{"r"})
	prev := "r"
	for i := 0; i < 4; i++ {
		l, r, j := "l"+string(rune('0'+i)), "r"+string(rune('0'+i)), "j"+string(rune('0'+i))
		for _, e := range [][3]string{{prev, l, ""}, {prev, r, ""}, {l, j, ""}, {r, j, ""}} {
			g.AddNode(&Node{ID: e[0], Name: e[0]})
			g.AddNode(&Node{ID: e[1], Name: e[1]})
			g.AddEdge(e[0], e[1], e[2])
		}
		prev = j
	}
	res := g.Paths([]string{prev}, 3, 0)
	if res.Total != 16 || len(res.Paths) != 3 || !res.Complete {
		t.Fatalf("total=%d shown=%d complete=%v", res.Total, len(res.Paths), res.Complete)
	}
	small := g.Paths([]string{prev}, 3, 5)
	if small.Complete {
		t.Error("expected incomplete result with a tiny budget")
	}
}

func TestPathsRootTargetAndUnreachable(t *testing.T) {
	g := build([]string{"app"}, [3]string{"app", "a", ""}, [3]string{"orphan", "a", ""})
	g.AddNode(&Node{ID: "lonely", Name: "lonely"})
	if res := g.Paths([]string{"app"}, 10, 0); res.Total != 1 || len(res.Paths[0]) != 1 {
		t.Errorf("root target: %+v", res)
	}
	if res := g.Paths([]string{"lonely"}, 10, 0); res.Total != 0 || !res.Complete {
		t.Errorf("unreachable: %+v", res)
	}
	// Paths stop at the first root and never go through non-root orphans.
	if res := g.Paths([]string{"a"}, 10, 0); res.Total != 1 {
		t.Errorf("a: %+v", res)
	}
}

func TestPathsMultipleTargets(t *testing.T) {
	g := build([]string{"app"}, [3]string{"app", "x", ""}, [3]string{"x", "ms1", ""}, [3]string{"app", "ms2", ""})
	res := g.Paths([]string{"ms1", "ms2"}, 0, 0)
	if res.Total != 2 || pathString(res.Paths[0]) != "app > ms2" {
		t.Errorf("got %+v", res)
	}
}

func TestAddEdgeNormalWins(t *testing.T) {
	g := build([]string{"app"})
	g.AddEdge("app", "x", "dev")
	g.AddEdge("app", "x", "")
	g.AddEdge("app", "x", "optional")
	g.AddEdge("app", "app", "")
	if got := g.Edges["app"]; len(got) != 1 || got[0].Kind != "" {
		t.Errorf("edges = %+v", got)
	}
}

func TestGlob(t *testing.T) {
	tests := []struct {
		pat, name string
		want      bool
	}{
		{"react", "react", true},
		{"react", "react-dom", false},
		{"react*", "react-dom", true},
		{"*react*", "preact-render", true},
		{"@babel/*", "@babel/core", true},
		{"@babel/*", "@types/babel", false},
		{"serde?json", "serde_json", true},
		{"serde?json", "serde__json", false},
		{"*", "", true},
		{"a*b*c", "aXXbYYc", true},
		{"a*b*c", "aXXbYY", false},
		{"golang.org/x/*", "golang.org/x/text", true},
	}
	for _, tt := range tests {
		if got := Glob(tt.pat, tt.name); got != tt.want {
			t.Errorf("Glob(%q, %q) = %v, want %v", tt.pat, tt.name, got, tt.want)
		}
	}
}

func TestMatchNormalizeAndSuggest(t *testing.T) {
	g := New("python", "uv.lock")
	g.Normalize = func(s string) string { return strings.ToLower(strings.ReplaceAll(s, "_", "-")) }
	g.AddNode(&Node{ID: "1", Name: "Typing_Extensions", Version: "4.12.2"})
	g.AddNode(&Node{ID: "2", Name: "requests", Version: "2.32.3"})
	g.AddNode(&Node{ID: "3", Name: "requests", Version: "2.31.0"})
	if got := g.Match("typing-extensions"); len(got) != 1 {
		t.Errorf("normalized match: %v", got)
	}
	got := g.Match("req*")
	if len(got) != 2 || got[0].Version != "2.31.0" {
		t.Errorf("sorted match: %+v", got)
	}
	if s := g.Suggest("typing", 5); len(s) != 1 || s[0] != "Typing_Extensions" {
		t.Errorf("suggest: %v", s)
	}
}

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"1.0.0", "1.0.0", 0},
		{"1.0.10", "1.0.9", 1},
		{"v0.3.0", "v0.14.0", -1},
		{"1.0.0-alpha", "1.0.0", -1},
		{"1.0.0-alpha.1", "1.0.0-alpha.beta", -1},
		{"1.0.0-rc.1", "1.0.0-beta.11", 1},
		{"v0.0.0-20230101000000-abcdef123456", "v0.1.0", -1},
		{"v2.0.0+incompatible", "v1.9.9", 1},
		{"2.0.0rc1", "2.0.0rc2", -1},
		{"1.10", "1.9", 1},
		{"1.0", "1.0.1", -1},
	}
	for _, tt := range tests {
		if got := CompareVersions(tt.a, tt.b); got != tt.want {
			t.Errorf("CompareVersions(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestPathsDeferredKind(t *testing.T) {
	g := build([]string{"main"},
		[3]string{"main", "a", ""},
		[3]string{"main", "x", "indirect"},
		[3]string{"a", "x", ""},
		[3]string{"main", "y", "indirect"},
	)
	g.DeferredKind = "indirect"
	res := g.Paths([]string{"x"}, 0, 0)
	if res.Total != 1 || pathString(res.Paths[0]) != "main > a > x" {
		t.Errorf("x: %+v", res)
	}
	res = g.Paths([]string{"y"}, 0, 0)
	if res.Total != 1 || pathString(res.Paths[0]) != "main > y[indirect]" {
		t.Errorf("y: %+v", res)
	}
}

func TestPathsExplosionIsBounded(t *testing.T) {
	// 20 layers of 10 fully connected nodes: 10^19 paths.
	g := New("test", "x")
	g.AddNode(&Node{ID: "root", Name: "root", Root: true})
	prev := []string{"root"}
	for layer := 0; layer < 20; layer++ {
		var cur []string
		for i := 0; i < 10; i++ {
			id := string(rune('a'+layer)) + string(rune('0'+i))
			g.AddNode(&Node{ID: id, Name: id})
			for _, p := range prev {
				g.AddEdge(p, id, "")
			}
			cur = append(cur, id)
		}
		prev = cur
	}
	g.AddNode(&Node{ID: "target", Name: "target"})
	for _, p := range prev {
		g.AddEdge(p, "target", "")
	}
	res := g.Paths([]string{"target"}, 10, 0)
	if res.Complete {
		t.Fatal("expected the search to stop early")
	}
	if len(res.Paths) != 10 || res.Total < 10 {
		t.Errorf("shown=%d total=%d", len(res.Paths), res.Total)
	}
	for _, p := range res.Paths {
		if len(p) != 22 {
			t.Errorf("path length %d, want 22", len(p))
		}
	}
}
