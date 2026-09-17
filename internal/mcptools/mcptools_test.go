package mcptools

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Mr-hunt-007/depwhy/internal/mcp"
)

const cargoLock = `version = 4

[[package]]
name = "itoa"
version = "1.0.11"
source = "registry+https://github.com/rust-lang/crates.io-index"

[[package]]
name = "my-crate"
version = "0.1.0"
dependencies = [
 "itoa",
 "serde_json",
]

[[package]]
name = "serde"
version = "1.0.210"
source = "registry+https://github.com/rust-lang/crates.io-index"

[[package]]
name = "serde_json"
version = "1.0.128"
source = "registry+https://github.com/rust-lang/crates.io-index"
dependencies = [
 "itoa",
 "serde",
]
`

const cargoToml = `[package]
name = "my-crate"
version = "0.1.0"
edition = "2021"

[dependencies]
serde_json = "1"

[dev-dependencies]
itoa = "1"
`

const npmLock = `{
  "name": "web",
  "version": "2.0.0",
  "lockfileVersion": 3,
  "packages": {
    "": {"name": "web", "version": "2.0.0", "dependencies": {"a": "1"}, "devDependencies": {"b": "1"}},
    "node_modules/a": {"version": "1.0.0", "dependencies": {"c": "1"}},
    "node_modules/b": {"version": "1.0.0", "dev": true, "dependencies": {"c": "1"}},
    "node_modules/c": {"version": "1.0.0"}
  }
}`

func writeFiles(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func tool(t *testing.T, s *mcp.Server, name string) mcp.Tool {
	t.Helper()
	for _, tl := range s.Tools {
		if tl.Name == name {
			return tl
		}
	}
	t.Fatalf("no tool %s", name)
	return mcp.Tool{}
}

func call(t *testing.T, name string, args any) (mcp.Result, error) {
	t.Helper()
	return callCtx(t, context.Background(), newServer("test", DefaultGoTimeout), name, args)
}

func callCtx(t *testing.T, ctx context.Context, s *mcp.Server, name string, args any) (mcp.Result, error) {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	return tool(t, s, name).Handler(ctx, raw)
}

type report struct {
	Query    string `json:"query"`
	Searched []struct {
		Ecosystem string `json:"ecosystem"`
		Lockfile  string `json:"lockfile"`
	} `json:"searched"`
	Matches []struct {
		Ecosystem     string                `json:"ecosystem"`
		Name          string                `json:"name"`
		Version       string                `json:"version"`
		Root          bool                  `json:"root"`
		TotalPaths    int                   `json:"total_paths"`
		PathsComplete bool                  `json:"paths_complete"`
		Paths         [][]map[string]string `json:"paths"`
	} `json:"matches"`
	Suggestions []string `json:"suggestions"`
	Warnings    []string `json:"warnings"`
}

func decode[T any](t *testing.T, res mcp.Result) T {
	t.Helper()
	var v T
	if err := json.Unmarshal([]byte(res.Text), &v); err != nil {
		t.Fatalf("bad JSON: %v\n%s", err, res.Text)
	}
	if res.Structured == nil {
		t.Error("structured content missing")
	}
	return v
}

func TestExplainCargo(t *testing.T) {
	dir := writeFiles(t, map[string]string{"Cargo.lock": cargoLock, "Cargo.toml": cargoToml})
	res, err := call(t, "depwhy_explain", map[string]any{"package": "itoa", "dir": dir})
	if err != nil {
		t.Fatal(err)
	}
	rep := decode[report](t, res)
	if rep.Query != "itoa" || len(rep.Searched) != 1 || len(rep.Matches) != 1 || len(rep.Warnings) != 0 {
		t.Fatalf("report = %+v", rep)
	}
	m := rep.Matches[0]
	if m.Ecosystem != "cargo" || m.Version != "1.0.11" || m.TotalPaths != 2 || !m.PathsComplete || len(m.Paths) != 2 {
		t.Fatalf("match = %+v", m)
	}
	if m.Paths[0][0]["name"] != "my-crate" || m.Paths[0][1]["kind"] != "dev" || m.Paths[1][1]["name"] != "serde_json" {
		t.Errorf("paths = %+v", m.Paths)
	}
}

func TestExplainCapsAndWarnings(t *testing.T) {
	dir := writeFiles(t, map[string]string{"package-lock.json": npmLock})
	res, err := call(t, "depwhy_explain", map[string]any{"package": "c", "dir": dir, "max_paths": 1})
	if err != nil {
		t.Fatal(err)
	}
	rep := decode[report](t, res)
	if len(rep.Matches) != 1 || len(rep.Matches[0].Paths) != 1 || rep.Matches[0].TotalPaths != 2 {
		t.Fatalf("report = %+v", rep)
	}
	if len(rep.Warnings) != 1 || !strings.Contains(rep.Warnings[0], "showing 1 of 2 paths; raise max_paths") {
		t.Errorf("warnings = %q", rep.Warnings)
	}

	res, err = call(t, "depwhy_explain", map[string]any{"package": "?", "dir": dir, "max_matches": 2})
	if err != nil {
		t.Fatal(err)
	}
	rep = decode[report](t, res)
	if len(rep.Matches) != 2 || rep.Matches[0].Name != "a" || rep.Matches[1].Name != "b" {
		t.Fatalf("matches = %+v", rep.Matches)
	}
	if last := rep.Warnings[len(rep.Warnings)-1]; !strings.Contains(last, "showing 2 of 3 matches") || !strings.Contains(last, "max_matches") {
		t.Errorf("warnings = %q", rep.Warnings)
	}

	res, err = call(t, "depwhy_explain", map[string]any{"package": "?", "dir": dir, "max_matches": 0, "max_paths": 0})
	if err != nil {
		t.Fatal(err)
	}
	if rep = decode[report](t, res); len(rep.Matches) != 3 || len(rep.Warnings) != 0 {
		t.Errorf("uncapped report = %+v", rep)
	}
}

func TestExplainNotFoundIsNotAnError(t *testing.T) {
	dir := writeFiles(t, map[string]string{"package-lock.json": npmLock})
	res, err := call(t, "depwhy_explain", map[string]any{"package": "we", "dir": dir})
	if err != nil {
		t.Fatal(err)
	}
	rep := decode[report](t, res)
	if len(rep.Matches) != 0 || !strings.Contains(res.Text, `"matches": []`) || len(rep.Suggestions) != 1 || rep.Suggestions[0] != "web" {
		t.Errorf("report = %s", res.Text)
	}
}

func TestExplainLoadErrors(t *testing.T) {
	dir := writeFiles(t, map[string]string{"Cargo.lock": "version = 4\n[[package]]\nname = \"oops\n", "package-lock.json": npmLock})
	res, err := call(t, "depwhy_explain", map[string]any{"package": "c", "dir": dir})
	if err != nil {
		t.Fatal(err)
	}
	rep := decode[report](t, res)
	if len(rep.Matches) != 1 || len(rep.Warnings) != 1 || !strings.Contains(rep.Warnings[0], "not searched: Cargo.lock (cargo)") {
		t.Errorf("report = %s", res.Text)
	}
	_, err = call(t, "depwhy_explain", map[string]any{"package": "nothing", "dir": dir})
	if err == nil || !strings.Contains(err.Error(), "could not be read: Cargo.lock (cargo)") || !strings.Contains(err.Error(), "package-lock.json (npm)") {
		t.Errorf("err = %v", err)
	}
}

func TestExplainArgumentErrors(t *testing.T) {
	npm := writeFiles(t, map[string]string{"package-lock.json": npmLock})
	empty := t.TempDir()
	yarn := writeFiles(t, map[string]string{"yarn.lock": ""})
	tests := []struct {
		name string
		args map[string]any
		want string
	}{
		{"missing package", map[string]any{"dir": npm}, "package is required"},
		{"blank package", map[string]any{"package": "  ", "dir": npm}, "package is required"},
		{"negative max_paths", map[string]any{"package": "c", "dir": npm, "max_paths": -1}, "max_paths must be"},
		{"negative max_matches", map[string]any{"package": "c", "dir": npm, "max_matches": -3}, "max_matches must be"},
		{"unknown eco", map[string]any{"package": "c", "dir": npm, "eco": "maven"}, `unknown eco "maven"`},
		{"eco not present", map[string]any{"package": "c", "dir": npm, "eco": "cargo"}, "no cargo lockfile"},
		{"unknown argument", map[string]any{"package": "c", "dir": npm, "max_path": 3}, "unknown field"},
		{"wrong type", map[string]any{"package": "c", "max_paths": "ten"}, "invalid arguments"},
		{"missing dir", map[string]any{"package": "c", "dir": filepath.Join(empty, "nope")}, "is not a directory"},
		{"no lockfile", map[string]any{"package": "c", "dir": empty}, "no supported lockfile"},
		{"unsupported lockfile", map[string]any{"package": "c", "dir": yarn}, "found yarn.lock, which depwhy does not read"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := call(t, "depwhy_explain", tt.args)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("err = %v, want %q", err, tt.want)
			}
		})
	}
}

type ecoReport struct {
	Dir       string `json:"dir"`
	Lockfiles []struct {
		Ecosystem  string   `json:"ecosystem"`
		Lockfile   string   `json:"lockfile"`
		Parsed     bool     `json:"parsed"`
		Error      string   `json:"error"`
		Packages   int      `json:"packages"`
		Roots      []string `json:"roots"`
		RootsTotal int      `json:"roots_total"`
		Warnings   []string `json:"warnings"`
	} `json:"lockfiles"`
	Unsupported []string `json:"unsupported"`
}

func TestEcosystems(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"package-lock.json": npmLock,
		"Cargo.lock":        "version = 4\n[[package]]\nname = \"oops\n",
		"uv.lock":           "version = 1\n\n[[package]]\nname = \"app\"\nversion = \"0.1.0\"\nsource = { virtual = \".\" }\n",
		"pnpm-lock.yaml":    "",
	})
	res, err := call(t, "depwhy_ecosystems", map[string]any{"dir": dir})
	if err != nil {
		t.Fatal(err)
	}
	rep := decode[ecoReport](t, res)
	if !filepath.IsAbs(rep.Dir) || len(rep.Lockfiles) != 3 || len(rep.Unsupported) != 1 || rep.Unsupported[0] != "pnpm-lock.yaml" {
		t.Fatalf("report = %s", res.Text)
	}
	npm, cargo, uv := rep.Lockfiles[0], rep.Lockfiles[1], rep.Lockfiles[2]
	if npm.Ecosystem != "npm" || !npm.Parsed || npm.Packages != 4 || npm.RootsTotal != 1 || len(npm.Roots) != 1 || npm.Roots[0] != "web 2.0.0" || npm.Error != "" {
		t.Errorf("npm = %+v", npm)
	}
	if cargo.Ecosystem != "cargo" || cargo.Parsed || !strings.Contains(cargo.Error, "unterminated string") || cargo.Roots == nil {
		t.Errorf("cargo = %+v", cargo)
	}
	if uv.Ecosystem != "python" || uv.Lockfile != "uv.lock" || !uv.Parsed || uv.Packages != 1 || len(uv.Roots) != 1 || uv.Roots[0] != "app 0.1.0" {
		t.Errorf("uv = %+v", uv)
	}
	if strings.Contains(res.Text, "null") {
		t.Errorf("lists should be [] not null:\n%s", res.Text)
	}
}

func TestEcosystemsEmptyAndErrors(t *testing.T) {
	empty := t.TempDir()
	res, err := call(t, "depwhy_ecosystems", map[string]any{"dir": empty})
	if err != nil {
		t.Fatal(err)
	}
	if rep := decode[ecoReport](t, res); len(rep.Lockfiles) != 0 || rep.Lockfiles == nil || rep.Unsupported == nil {
		t.Errorf("report = %s", res.Text)
	}
	if _, err := call(t, "depwhy_ecosystems", map[string]any{"dir": filepath.Join(empty, "x")}); err == nil || !strings.Contains(err.Error(), "is not a directory") {
		t.Errorf("err = %v", err)
	}
	if _, err := call(t, "depwhy_ecosystems", map[string]any{"path": empty}); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Errorf("err = %v", err)
	}
}

func TestEcosystemsRelativeDir(t *testing.T) {
	dir := writeFiles(t, map[string]string{"sub/package-lock.json": npmLock})
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(wd) })
	res, err := call(t, "depwhy_ecosystems", map[string]any{"dir": "sub"})
	if err != nil {
		t.Fatal(err)
	}
	if rep := decode[ecoReport](t, res); len(rep.Lockfiles) != 1 || filepath.Base(rep.Dir) != "sub" {
		t.Errorf("report = %s", res.Text)
	}
	res, err = call(t, "depwhy_explain", map[string]any{"package": "c", "dir": "sub"})
	if err != nil || !strings.Contains(res.Text, `"name": "c"`) {
		t.Errorf("err=%v text=%s", err, res.Text)
	}
}

// TestGoRealModule runs the real go command on a module with no requirements,
// which needs no network.
func TestGoRealModule(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go command not available")
	}
	t.Setenv("GOPROXY", "off")
	t.Setenv("GOWORK", "off")
	t.Setenv("GOTOOLCHAIN", "local")
	dir := writeFiles(t, map[string]string{"go.mod": "module example.com/app\n\ngo 1.21\n"})
	res, err := call(t, "depwhy_ecosystems", map[string]any{"dir": dir})
	if err != nil {
		t.Fatal(err)
	}
	rep := decode[ecoReport](t, res)
	if len(rep.Lockfiles) != 1 || !rep.Lockfiles[0].Parsed || rep.Lockfiles[0].Roots[0] != "example.com/app" {
		t.Errorf("report = %s", res.Text)
	}
	res, err = call(t, "depwhy_explain", map[string]any{"package": "example.com/app", "dir": dir, "eco": "go"})
	if err != nil || !strings.Contains(res.Text, `"root": true`) {
		t.Errorf("err=%v text=%s", err, res.Text)
	}
}

// fakeGo puts a go command on PATH that never finishes, standing in for a
// `go mod graph` stuck on the network.
func fakeGo(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake go command is a shell script")
	}
	sleep, err := exec.LookPath("sleep")
	if err != nil {
		t.Skip("sleep command not available")
	}
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "go"), []byte("#!/bin/sh\nexec "+sleep+" 30\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	return writeFiles(t, map[string]string{"go.mod": "module example.com/app\n\ngo 1.21\n", "package-lock.json": npmLock})
}

func TestGoTimeoutBecomesToolError(t *testing.T) {
	dir := fakeGo(t)
	s := newServer("test", 300*time.Millisecond)
	start := time.Now()
	res, err := callCtx(t, context.Background(), s, "depwhy_ecosystems", map[string]any{"dir": dir})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Text, "did not finish within 300ms") || !strings.Contains(res.Text, `"parsed": true`) {
		t.Errorf("text = %s", res.Text)
	}
	// Found in npm: the Go failure is a warning.
	res, err = callCtx(t, context.Background(), s, "depwhy_explain", map[string]any{"package": "c", "dir": dir})
	if err != nil || !strings.Contains(res.Text, "not searched: go.mod (go)") {
		t.Errorf("err=%v text=%s", err, res.Text)
	}
	// Only Go searched: the timeout is the error.
	_, err = callCtx(t, context.Background(), s, "depwhy_explain", map[string]any{"package": "x", "dir": dir, "eco": "go"})
	if err == nil || !strings.Contains(err.Error(), "did not finish") {
		t.Errorf("err = %v", err)
	}
	if d := time.Since(start); d > 10*time.Second {
		t.Errorf("took %s", d)
	}
}

func TestCancelStopsGo(t *testing.T) {
	dir := fakeGo(t)
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := callCtx(t, ctx, New("test", false), "depwhy_explain", map[string]any{"package": "x", "dir": dir, "eco": "go"})
	if err == nil {
		t.Fatal("expected an error after cancellation")
	}
	if d := time.Since(start); d > 10*time.Second {
		t.Errorf("cancellation took %s", d)
	}
	_, err = callCtx(t, ctx, New("test", false), "depwhy_ecosystems", map[string]any{"dir": dir})
	if err == nil {
		t.Error("ecosystems: expected an error after cancellation")
	}
}

func TestToolDefinitions(t *testing.T) {
	for _, allow := range []bool{false, true} {
		s := New("9.9.9", allow)
		if s.Name != "depwhy" || s.Version != "9.9.9" || s.Instructions == "" || len(s.Tools) != 2 {
			t.Fatalf("server = %+v", s)
		}
		for _, tl := range s.Tools {
			if !strings.HasPrefix(tl.Name, "depwhy_") || tl.Description == "" || tl.Annotations == nil || tl.Annotations.ReadOnlyHint == nil || !*tl.Annotations.ReadOnlyHint {
				t.Errorf("tool %s is not a described read-only tool", tl.Name)
			}
			props := tl.InputSchema["properties"].(map[string]any)
			for name, p := range props {
				if d, _ := p.(map[string]any)["description"].(string); d == "" {
					t.Errorf("%s.%s has no description", tl.Name, name)
				}
			}
		}
	}
}
