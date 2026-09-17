// Package eco reads lockfiles from each supported package manager into the
// shared graph model.
package eco

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Mr-hunt-007/depwhy/internal/graph"
)

// Source is a detected lockfile.
type Source struct {
	Ecosystem string // npm, cargo, python, go
	File      string // base name, e.g. Cargo.lock
}

// Ecosystems lists valid --eco values and what they select.
var Ecosystems = map[string]func(Source) bool{
	"npm":    func(s Source) bool { return s.Ecosystem == "npm" },
	"cargo":  func(s Source) bool { return s.Ecosystem == "cargo" },
	"rust":   func(s Source) bool { return s.Ecosystem == "cargo" },
	"python": func(s Source) bool { return s.Ecosystem == "python" },
	"pypi":   func(s Source) bool { return s.Ecosystem == "python" },
	"uv":     func(s Source) bool { return s.File == "uv.lock" },
	"poetry": func(s Source) bool { return s.File == "poetry.lock" },
	"go":     func(s Source) bool { return s.Ecosystem == "go" },
}

// unsupported lockfiles we recognize, to explain rather than ignore.
var unsupported = []string{"yarn.lock", "pnpm-lock.yaml", "bun.lock", "bun.lockb", "Pipfile.lock", "pdm.lock", "Gemfile.lock", "composer.lock"}

// Detect lists supported lockfiles in dir, and recognized but unsupported ones.
func Detect(dir string) (found []Source, other []string) {
	exists := func(name string) bool {
		st, err := os.Stat(filepath.Join(dir, name))
		return err == nil && !st.IsDir()
	}
	// npm reads npm-shrinkwrap.json in preference to package-lock.json.
	if exists("npm-shrinkwrap.json") {
		found = append(found, Source{"npm", "npm-shrinkwrap.json"})
	} else if exists("package-lock.json") {
		found = append(found, Source{"npm", "package-lock.json"})
	}
	if exists("Cargo.lock") {
		found = append(found, Source{"cargo", "Cargo.lock"})
	}
	if exists("uv.lock") {
		found = append(found, Source{"python", "uv.lock"})
	}
	if exists("poetry.lock") {
		found = append(found, Source{"python", "poetry.lock"})
	}
	if exists("go.mod") {
		found = append(found, Source{"go", "go.mod"})
	}
	for _, name := range unsupported {
		if exists(name) {
			other = append(other, name)
		}
	}
	return found, other
}

// ErrGoMissing is returned when go.mod is present but the go command is not.
var ErrGoMissing = errors.New("go.mod found but the go command is not on PATH; depwhy runs `go mod graph` to read the module graph (go.mod alone does not record why a module is required). Install Go, or use --eco to search other ecosystems")

// Load reads one detected source into a graph.
func Load(dir string, s Source) (*graph.Graph, error) {
	return LoadContext(context.Background(), dir, s, 0)
}

// LoadContext is Load with a context and a limit on how long `go mod graph`
// may run (0 means DefaultGoTimeout). Only Go uses them: go may download
// modules, and cancelling ctx or reaching the limit stops it.
func LoadContext(ctx context.Context, dir string, s Source, goTimeout time.Duration) (*graph.Graph, error) {
	read := func(rel string) ([]byte, error) { return os.ReadFile(filepath.Join(dir, rel)) }
	dirName := filepath.Base(absOr(dir))
	switch s.File {
	case "package-lock.json", "npm-shrinkwrap.json":
		lock, err := read(s.File)
		if err != nil {
			return nil, err
		}
		pkg, err := read("package.json")
		if err != nil {
			pkg = nil
		}
		return BuildNPM(s.File, lock, pkg, dirName)
	case "Cargo.lock":
		lock, err := read(s.File)
		if err != nil {
			return nil, err
		}
		members, warnings := CargoMembers(read, func(pattern string) ([]string, error) {
			matches, err := filepath.Glob(filepath.Join(dir, pattern))
			if err != nil {
				return nil, err
			}
			var rel []string
			for _, m := range matches {
				if st, err := os.Stat(m); err == nil && st.IsDir() {
					r, err := filepath.Rel(dir, m)
					if err == nil {
						rel = append(rel, r)
					}
				}
			}
			return rel, nil
		})
		g, err := BuildCargo(s.File, lock, members)
		if err != nil {
			return nil, err
		}
		g.Warnings = append(warnings, g.Warnings...)
		return g, nil
	case "uv.lock", "poetry.lock":
		lock, err := read(s.File)
		if err != nil {
			return nil, err
		}
		var pp *PyProject
		var warnings []string
		if data, err := read("pyproject.toml"); err == nil {
			pp, err = ParsePyProject(data)
			if err != nil {
				warnings = append(warnings, fmt.Sprintf("pyproject.toml: %v; project dependencies unavailable", err))
				pp = nil
			}
		}
		var g *graph.Graph
		if s.File == "uv.lock" {
			g, err = BuildUV(s.File, lock, pp, dirName)
		} else {
			g, err = BuildPoetry(s.File, lock, pp, dirName)
		}
		if err != nil {
			return nil, err
		}
		g.Warnings = append(warnings, g.Warnings...)
		return g, nil
	case "go.mod":
		data, err := read(s.File)
		if err != nil {
			return nil, err
		}
		gm, err := ParseGoMod(data)
		if err != nil {
			return nil, err
		}
		out, err := runGoModGraph(ctx, dir, goTimeout)
		if err != nil {
			return nil, err
		}
		return BuildGo(s.File, out, gm)
	}
	return nil, fmt.Errorf("unsupported source %s", s.File)
}

func absOr(dir string) string {
	if a, err := filepath.Abs(dir); err == nil {
		return a
	}
	return dir
}

// DefaultGoTimeout bounds one `go mod graph` run.
const DefaultGoTimeout = 5 * time.Minute

func runGoModGraph(ctx context.Context, dir string, timeout time.Duration) ([]byte, error) {
	if timeout <= 0 {
		timeout = DefaultGoTimeout
	}
	goBin, err := exec.LookPath("go")
	if err != nil {
		return nil, ErrGoMissing
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, goBin, "mod", "graph")
	cmd.Dir = dir
	// go may start git or other helpers that keep the output pipes open after
	// go itself is killed; do not wait for them.
	cmd.WaitDelay = 5 * time.Second
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, fmt.Errorf("`go mod graph` did not finish within %s (it may be downloading modules; the module cache keeps what was fetched, so a retry can succeed, or skip Go with --eco): %w", timeout, ctx.Err())
		}
		if ctx.Err() != nil {
			return nil, fmt.Errorf("`go mod graph` stopped: %w", ctx.Err())
		}
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("`go mod graph` failed: %s", msg)
	}
	return stdout.Bytes(), nil
}
