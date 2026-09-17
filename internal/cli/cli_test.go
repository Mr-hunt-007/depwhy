package cli

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
    "node_modules/c": {"version": "1.0.0"},
    "node_modules/serde": {"version": "9.9.9", "extraneous": true}
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

func run(t *testing.T, env Env, args ...string) (string, string, int) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := Run(args, &stdout, &stderr, env)
	return stdout.String(), stderr.String(), code
}

func TestCargoTextOutput(t *testing.T) {
	dir := writeFiles(t, map[string]string{"Cargo.lock": cargoLock, "Cargo.toml": cargoToml})
	out, errOut, code := run(t, Env{}, "serde", "--dir", dir)
	if code != ExitFound || errOut != "" {
		t.Fatalf("code=%d stderr=%q", code, errOut)
	}
	want := "serde 1.0.210  (cargo, Cargo.lock)\n" +
		"my-crate 0.1.0\n" +
		"└── serde_json 1.0.128\n" +
		"    └── serde 1.0.210\n"
	if out != want {
		t.Errorf("got:\n%s\nwant:\n%s", out, want)
	}

	out, _, _ = run(t, Env{}, "--dir", dir, "itoa")
	want = "itoa 1.0.11  (cargo, Cargo.lock)\n" +
		"my-crate 0.1.0\n" +
		"├── itoa 1.0.11 [dev]\n" +
		"└── serde_json 1.0.128\n" +
		"    └── itoa 1.0.11\n" +
		"2 paths\n"
	if out != want {
		t.Errorf("got:\n%s\nwant:\n%s", out, want)
	}
}

func TestMultipleEcosystemsAndFilter(t *testing.T) {
	dir := writeFiles(t, map[string]string{"Cargo.lock": cargoLock, "package-lock.json": npmLock})
	out, errOut, code := run(t, Env{}, "--dir", dir, "serde")
	if code != ExitFound {
		t.Fatalf("code=%d stderr=%s", code, errOut)
	}
	if !strings.Contains(out, "(npm, package-lock.json)  extraneous") || !strings.Contains(out, "(cargo, Cargo.lock)") {
		t.Errorf("expected both ecosystems:\n%s", out)
	}
	if !strings.Contains(errOut, "Cargo.toml not found") {
		t.Errorf("expected Cargo.toml warning, got %q", errOut)
	}
	if !strings.Contains(out, "not reachable from any root") {
		t.Errorf("extraneous npm package should be unreachable:\n%s", out)
	}

	out, _, code = run(t, Env{}, "--dir", dir, "--eco", "cargo", "serde")
	if code != ExitFound || strings.Contains(out, "npm") {
		t.Errorf("--eco cargo leaked npm:\n%s", out)
	}
	_, errOut, code = run(t, Env{}, "--dir", dir, "--eco", "go", "serde")
	if code != ExitError || !strings.Contains(errOut, "no go lockfile") {
		t.Errorf("code=%d stderr=%q", code, errOut)
	}
	_, errOut, code = run(t, Env{}, "--dir", dir, "--eco", "maven", "serde")
	if code != ExitError || !strings.Contains(errOut, "unknown --eco") {
		t.Errorf("code=%d stderr=%q", code, errOut)
	}
}

func TestJSONOutput(t *testing.T) {
	dir := writeFiles(t, map[string]string{"package-lock.json": npmLock})
	out, errOut, code := run(t, Env{}, "--dir", dir, "--json", "c")
	if code != ExitFound || errOut != "" {
		t.Fatalf("code=%d stderr=%q", code, errOut)
	}
	var rep struct {
		Query    string `json:"query"`
		Searched []struct {
			Ecosystem string `json:"ecosystem"`
			Lockfile  string `json:"lockfile"`
		} `json:"searched"`
		Matches []struct {
			Ecosystem     string                `json:"ecosystem"`
			Lockfile      string                `json:"lockfile"`
			Name          string                `json:"name"`
			Version       string                `json:"version"`
			Flags         []string              `json:"flags"`
			Root          bool                  `json:"root"`
			TotalPaths    int                   `json:"total_paths"`
			PathsComplete bool                  `json:"paths_complete"`
			Paths         [][]map[string]string `json:"paths"`
		} `json:"matches"`
		Warnings []string `json:"warnings"`
	}
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if rep.Query != "c" || len(rep.Searched) != 1 || rep.Searched[0].Ecosystem != "npm" || len(rep.Matches) != 1 {
		t.Fatalf("report = %+v", rep)
	}
	m := rep.Matches[0]
	if m.Ecosystem != "npm" || m.Name != "c" || m.Version != "1.0.0" || m.TotalPaths != 2 || !m.PathsComplete || m.Root || m.Flags == nil {
		t.Errorf("match = %+v", m)
	}
	if len(m.Paths) != 2 || m.Paths[0][0]["name"] != "web" || m.Paths[0][0]["kind"] != "" || m.Paths[0][1]["kind"] != "normal" || m.Paths[1][1]["kind"] != "dev" {
		t.Errorf("paths = %+v", m.Paths)
	}
	if rep.Warnings == nil {
		t.Error("warnings should be an empty array, not null")
	}
}

func TestNotFound(t *testing.T) {
	dir := writeFiles(t, map[string]string{"package-lock.json": npmLock})
	out, errOut, code := run(t, Env{}, "--dir", dir, "serd")
	if code != ExitNotFound || out != "" {
		t.Fatalf("code=%d out=%q", code, out)
	}
	if !strings.Contains(errOut, `"serd" not found in package-lock.json (npm)`) || !strings.Contains(errOut, "similar names: serde") {
		t.Errorf("stderr = %q", errOut)
	}
	out, _, code = run(t, Env{}, "--dir", dir, "--json", "zzz")
	if code != ExitNotFound || !strings.Contains(out, `"matches": []`) {
		t.Errorf("code=%d out=%s", code, out)
	}
}

func TestGlobAndMaxPaths(t *testing.T) {
	dir := writeFiles(t, map[string]string{"package-lock.json": npmLock})
	out, _, code := run(t, Env{}, "--dir", dir, "--max-paths", "1", "?")
	if code != ExitFound {
		t.Fatalf("code=%d", code)
	}
	for _, want := range []string{"a 1.0.0  (npm", "b 1.0.0  (npm", "c 1.0.0  (npm", "showing 1 of 2 paths (use --max-paths 0 to show all)"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	out, _, _ = run(t, Env{}, "--dir", dir, "web")
	if !strings.Contains(out, "this is a root") {
		t.Errorf("root output:\n%s", out)
	}
}

func TestLoadErrors(t *testing.T) {
	broken := "version = 4\n[[package]]\nname = \"oops\n"
	dir := writeFiles(t, map[string]string{"Cargo.lock": broken, "package-lock.json": npmLock})
	out, errOut, code := run(t, Env{}, "--dir", dir, "c")
	if code != ExitFound || !strings.Contains(out, "c 1.0.0") {
		t.Errorf("found in npm despite cargo error: code=%d out=%s", code, out)
	}
	if !strings.Contains(errOut, "Cargo.lock (cargo): Cargo.lock: line 3: unterminated string") {
		t.Errorf("stderr = %q", errOut)
	}
	_, _, code = run(t, Env{}, "--dir", dir, "nothing-here")
	if code != ExitError {
		t.Errorf("not found with a failed source should exit 2, got %d", code)
	}
}

func TestUsageErrors(t *testing.T) {
	empty := t.TempDir()
	tests := []struct {
		name   string
		args   []string
		code   int
		stdout string
		stderr string
	}{
		{"no args", nil, ExitError, "", "missing package name"},
		{"two args", []string{"a", "b"}, ExitError, "", "expected one package name"},
		{"bad flag", []string{"--nope", "x"}, ExitError, "", "flag provided but not defined"},
		{"negative max", []string{"--max-paths", "-1", "x"}, ExitError, "", "--max-paths"},
		{"bad dir", []string{"--dir", filepath.Join(empty, "missing"), "x"}, ExitError, "", "is not a directory"},
		{"no lockfile", []string{"--dir", empty, "x"}, ExitError, "", "no supported lockfile"},
		{"version", []string{"--version"}, ExitFound, "depwhy 0.2.0\n", ""},
		{"help", []string{"-h"}, ExitFound, "Usage:", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, errOut, code := run(t, Env{}, tt.args...)
			if code != tt.code || !strings.Contains(out, tt.stdout) || !strings.Contains(errOut, tt.stderr) {
				t.Errorf("code=%d stdout=%q stderr=%q", code, out, errOut)
			}
		})
	}

	yarn := writeFiles(t, map[string]string{"yarn.lock": ""})
	_, errOut, _ := run(t, Env{}, "--dir", yarn, "x")
	if !strings.Contains(errOut, "found yarn.lock, which depwhy does not read") {
		t.Errorf("stderr = %q", errOut)
	}
}

func TestColor(t *testing.T) {
	dir := writeFiles(t, map[string]string{"package-lock.json": npmLock})
	out, _, _ := run(t, Env{StdoutIsTTY: true}, "--dir", dir, "a")
	if !strings.Contains(out, "\x1b[") {
		t.Error("expected colour on a TTY")
	}
	for _, tc := range []struct {
		env  Env
		args []string
	}{
		{Env{StdoutIsTTY: true, NoColorEnv: true}, nil},
		{Env{StdoutIsTTY: true}, []string{"--no-color"}},
		{Env{}, nil},
	} {
		out, _, _ := run(t, tc.env, append([]string{"--dir", dir, "a"}, tc.args...)...)
		if strings.Contains(out, "\x1b[") {
			t.Errorf("unexpected colour with env=%+v args=%v", tc.env, tc.args)
		}
	}
}

// mcpSession drives `depwhy --mcp` in-process over pipes.
type mcpSession struct {
	t    *testing.T
	in   *io.PipeWriter
	out  *bufio.Scanner
	code chan int
}

func startMCP(t *testing.T, args ...string) *mcpSession {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	s := &mcpSession{t: t, in: inW, out: bufio.NewScanner(outR), code: make(chan int, 1)}
	s.out.Buffer(make([]byte, 1<<20), 1<<20)
	go func() {
		var stderr bytes.Buffer
		s.code <- Run(append([]string{"--mcp"}, args...), outW, &stderr, Env{Stdin: inR})
		outW.Close()
	}()
	t.Cleanup(func() { inW.Close() })
	return s
}

func (s *mcpSession) send(msg string) {
	s.t.Helper()
	if _, err := io.WriteString(s.in, msg+"\n"); err != nil {
		s.t.Fatal(err)
	}
}

func (s *mcpSession) recv() map[string]any {
	s.t.Helper()
	if !s.out.Scan() {
		s.t.Fatalf("no response: %v", s.out.Err())
	}
	var m map[string]any
	if err := json.Unmarshal(s.out.Bytes(), &m); err != nil {
		s.t.Fatalf("bad JSON %q: %v", s.out.Text(), err)
	}
	return m
}

func (s *mcpSession) toolNames() []string {
	s.t.Helper()
	s.send(`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	var names []string
	for _, tl := range s.recv()["result"].(map[string]any)["tools"].([]any) {
		m := tl.(map[string]any)
		ann := m["annotations"].(map[string]any)
		if ann["readOnlyHint"] != true || ann["destructiveHint"] != false {
			s.t.Errorf("%s annotations = %v", m["name"], ann)
		}
		names = append(names, m["name"].(string))
	}
	return names
}

func (s *mcpSession) initialize() {
	s.t.Helper()
	s.send(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"0"}}}`)
	res := s.recv()["result"].(map[string]any)
	info := res["serverInfo"].(map[string]any)
	if res["protocolVersion"] != "2025-06-18" || info["name"] != "depwhy" || info["version"] != Version || res["instructions"] == "" {
		s.t.Errorf("initialize = %v", res)
	}
	s.send(`{"jsonrpc":"2.0","method":"notifications/initialized"}`)
}

func TestMCPEndToEnd(t *testing.T) {
	dir := writeFiles(t, map[string]string{"Cargo.lock": cargoLock, "Cargo.toml": cargoToml, "package-lock.json": npmLock, "yarn.lock": ""})
	s := startMCP(t)
	s.initialize()
	if got := strings.Join(s.toolNames(), ","); got != "depwhy_ecosystems,depwhy_explain" {
		t.Errorf("tools = %s", got)
	}

	args, _ := json.Marshal(map[string]any{"package": "itoa", "dir": dir})
	s.send(`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"depwhy_explain","arguments":` + string(args) + `}}`)
	res := s.recv()["result"].(map[string]any)
	if res["isError"] != nil {
		t.Fatalf("explain error: %v", res)
	}
	text := res["content"].([]any)[0].(map[string]any)["text"].(string)
	cliOut, _, code := run(t, Env{}, "--dir", dir, "--json", "itoa")
	if code != ExitFound || strings.TrimSpace(cliOut) != text {
		t.Errorf("MCP output differs from --json:\n%s\n---\n%s", text, cliOut)
	}
	if sc, ok := res["structuredContent"].(map[string]any); !ok || sc["query"] != "itoa" {
		t.Errorf("structuredContent = %v", res["structuredContent"])
	}

	args, _ = json.Marshal(map[string]any{"dir": dir})
	s.send(`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"depwhy_ecosystems","arguments":` + string(args) + `}}`)
	res = s.recv()["result"].(map[string]any)
	sc := res["structuredContent"].(map[string]any)
	if res["isError"] != nil || len(sc["lockfiles"].([]any)) != 2 || sc["unsupported"].([]any)[0] != "yarn.lock" {
		t.Errorf("ecosystems = %v", res)
	}

	s.send(`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"depwhy_explain","arguments":{"package":"itoa","dir":"` + strings.ReplaceAll(filepath.Join(dir, "missing"), `\`, `\\`) + `"}}}`)
	res = s.recv()["result"].(map[string]any)
	if res["isError"] != true || !strings.Contains(res["content"].([]any)[0].(map[string]any)["text"].(string), "is not a directory") {
		t.Errorf("bad dir = %v", res)
	}

	s.in.Close()
	if c := <-s.code; c != ExitFound {
		t.Errorf("exit code %d", c)
	}
}

func TestMCPAllowDestructiveAndUsage(t *testing.T) {
	// depwhy has no destructive tools: the flag is accepted and changes nothing.
	s := startMCP(t, "--allow-destructive")
	s.initialize()
	if got := strings.Join(s.toolNames(), ","); got != "depwhy_ecosystems,depwhy_explain" {
		t.Errorf("tools = %s", got)
	}
	s.in.Close()
	<-s.code

	for _, args := range [][]string{{"--mcp", "serde"}, {"--allow-destructive", "serde"}} {
		out, errOut, code := run(t, Env{}, args...)
		if code != ExitError || out != "" || errOut == "" {
			t.Errorf("%v: code=%d out=%q err=%q", args, code, out, errOut)
		}
	}
	if out, _, _ := run(t, Env{}, "--help"); !strings.Contains(out, "--mcp") || !strings.Contains(out, "--allow-destructive") {
		t.Error("help does not document --mcp and --allow-destructive")
	}
}
