// Package mcptools exposes depwhy over the Model Context Protocol. Every tool
// runs the same code as the command line and depwhy_explain returns the same
// JSON as `depwhy --json`.
package mcptools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Mr-hunt-007/depwhy/internal/eco"
	"github.com/Mr-hunt-007/depwhy/internal/explain"
	"github.com/Mr-hunt-007/depwhy/internal/mcp"
)

// Defaults for output caps and for how long `go mod graph` may run inside one
// tool call. The CLI allows go five minutes; MCP clients time tool calls out
// much sooner (Codex CLI after 60 seconds by default), and an error the agent
// can act on is better than a call the client abandons.
const (
	DefaultMaxPaths   = 10
	DefaultMaxMatches = 20
	DefaultGoTimeout  = 50 * time.Second
	maxRoots          = 20
)

const instructions = `depwhy answers "why is this dependency in the project?" by reading lockfiles directly: npm (package-lock.json), Cargo (Cargo.lock), Python (uv.lock, poetry.lock) and Go (go.mod via go mod graph). Use it before removing, upgrading or auditing a package (for example a vulnerable transitive dependency) to see which direct dependency pulls it in and whether it is dev-only. Call depwhy_ecosystems first when you do not know which lockfiles a directory has, then depwhy_explain with a package name or glob. All tools are read-only.`

// New builds the server. allowDestructive is accepted for symmetry with the
// sibling tools; depwhy has no destructive tools, so it adds nothing.
func New(version string, allowDestructive bool) *mcp.Server {
	return newServer(version, DefaultGoTimeout)
}

func newServer(version string, goTimeout time.Duration) *mcp.Server {
	return &mcp.Server{
		Name:         "depwhy",
		Title:        "depwhy",
		Version:      version,
		Instructions: instructions,
		Tools:        []mcp.Tool{explainTool(goTimeout), ecosystemsTool(goTimeout)},
	}
}

const goNote = `Go needs the go command on PATH: depwhy runs "go mod graph", which may download module files (network access, or a populated module cache; start the server with GOPROXY=off to forbid downloads). It is stopped after 50 seconds and reported as an error rather than waiting; a retry often succeeds because downloaded modules stay in the cache, or pass eco to skip Go.`

var ecoValues = []string{"npm", "cargo", "python", "go", "uv", "poetry", "pypi", "rust"}

func dirProp() map[string]any {
	return mcp.String("Project directory containing the lockfiles, absolute or relative to the server's working directory. Only this directory is read (not subdirectories). Defaults to the server's working directory.")
}

func explainTool(goTimeout time.Duration) mcp.Tool {
	return mcp.Tool{
		Name:  "depwhy_explain",
		Title: "Explain why a dependency is present",
		Description: `Explain why a package is in a project: every dependency path from the project (or a workspace member, or the Go main module) to the package, shortest first, read from the lockfiles in dir. Use it to find which direct dependency pulls in a transitive package, whether it is only a dev/build/optional dependency, and which versions are installed.

Output is the same JSON as "depwhy --json": matches has one entry per name and version per lockfile ([] when nothing matched, with suggestions holding similar names). Each path is a list of steps starting at a root; later steps carry the kind of the edge that leads to them (normal, dev, build, optional, peer, extra:NAME, group:NAME, indirect). total_paths counts all paths, paths holds at most max_paths of them, and paths_complete is false when the search stopped early (total_paths is then a lower bound). root is true when the package is itself the project or a workspace member; total_paths 0 with root false means the entry is in the lockfile but unreachable (stale or orphaned). flags are lockfile markers on the package (dev, optional, peer, indirect, ...). When output was capped, or a lockfile could not be read while another one could, warnings says so and names the argument that raises the cap. If nothing matched and a lockfile failed to load, the call returns an error.

Limits: environment markers, platforms and Cargo features are not evaluated, so paths that only apply to other platforms or disabled features are shown too. Go paths come from the module requirement graph, not package imports. yarn, pnpm, bun, Pipfile and PDM lockfiles are not read. ` + goNote,
		Annotations: mcp.ReadOnly("Explain why a dependency is present"),
		InputSchema: mcp.Object(map[string]any{
			"package": mcp.String("Package name as it appears in the lockfile, e.g. \"ms\", \"serde\", \"requests\" or \"golang.org/x/text\". * and ? wildcards are allowed (\"@babel/*\"). Python names match after PEP 503 normalization; Cargo treats - and _ as equal."),
			"dir":     dirProp(),
			"eco":     mcp.Enum("Only search one ecosystem. uv and poetry select that Python lockfile; pypi is python and rust is cargo. Omit to search every lockfile found.", ecoValues...),
			"max_paths": withDefault(mcp.Integer("Paths returned per match, shortest first; 0 returns all (can be very large). total_paths always reports the full count."),
				DefaultMaxPaths),
			"max_matches": withDefault(mcp.Integer("Matches returned in total, useful with wide globs; 0 returns all. A warning reports how many were left out."),
				DefaultMaxMatches),
		}, "package"),
		Handler: func(ctx context.Context, raw json.RawMessage) (mcp.Result, error) {
			var a struct {
				Package    string `json:"package"`
				Dir        string `json:"dir"`
				Eco        string `json:"eco"`
				MaxPaths   *int   `json:"max_paths"`
				MaxMatches *int   `json:"max_matches"`
			}
			if err := mcp.Decode(raw, &a); err != nil {
				return mcp.Result{}, err
			}
			if strings.TrimSpace(a.Package) == "" {
				return mcp.Result{}, errors.New("package is required: a name or glob such as \"serde\" or \"@babel/*\"")
			}
			maxPaths, maxMatches := DefaultMaxPaths, DefaultMaxMatches
			if a.MaxPaths != nil {
				maxPaths = *a.MaxPaths
			}
			if a.MaxMatches != nil {
				maxMatches = *a.MaxMatches
			}
			if maxPaths < 0 {
				return mcp.Result{}, errors.New("max_paths must be 0 (all) or more")
			}
			if maxMatches < 0 {
				return mcp.Result{}, errors.New("max_matches must be 0 (all) or more")
			}
			if a.Eco != "" && eco.Ecosystems[strings.ToLower(a.Eco)] == nil {
				return mcp.Result{}, fmt.Errorf("unknown eco %q (use one of %s)", a.Eco, strings.Join(ecoValues, ", "))
			}
			out, err := explain.Run(ctx, explain.Request{Dir: a.Dir, Eco: a.Eco, Query: a.Package, MaxPaths: maxPaths, GoTimeout: goTimeout})
			if err != nil {
				return mcp.Result{}, err
			}
			rep := out.Report
			if out.ExitCode() == explain.ExitError {
				var msgs []string
				for _, le := range out.LoadErrors {
					msgs = append(msgs, le.Error())
				}
				msg := fmt.Sprintf("%q was not found, and some lockfiles could not be read: %s", a.Package, strings.Join(msgs, "; "))
				if len(rep.Searched) > 0 {
					msg += fmt.Sprintf(" (searched without a match: %s)", searchedList(rep.Searched))
				}
				return mcp.Result{}, errors.New(msg)
			}
			for _, le := range out.LoadErrors {
				rep.Warnings = append(rep.Warnings, fmt.Sprintf("not searched: %s", le.Error()))
			}
			totalMatches := len(rep.Matches)
			if maxMatches > 0 && totalMatches > maxMatches {
				rep.Matches = rep.Matches[:maxMatches]
			}
			for _, m := range rep.Matches {
				switch {
				case !m.PathsComplete:
					rep.Warnings = append(rep.Warnings, fmt.Sprintf("%s: path search stopped early on a very large graph; total_paths %d is a lower bound", matchLabel(m), m.TotalPaths))
				case len(m.Paths) < m.TotalPaths:
					rep.Warnings = append(rep.Warnings, fmt.Sprintf("%s: showing %d of %d paths; raise max_paths (0 for all) to see more", matchLabel(m), len(m.Paths), m.TotalPaths))
				}
			}
			if len(rep.Matches) < totalMatches {
				rep.Warnings = append(rep.Warnings, fmt.Sprintf("showing %d of %d matches; narrow the package pattern, set eco, or raise max_matches (0 for all)", len(rep.Matches), totalMatches))
			}
			return mcp.JSONResult(rep)
		},
	}
}

func ecosystemsTool(goTimeout time.Duration) mcp.Tool {
	return mcp.Tool{
		Name:  "depwhy_ecosystems",
		Title: "List lockfiles depwhy can read",
		Description: `List the lockfiles in dir that depwhy reads, whether each one parsed, how many packages it holds and which roots (project, workspace members, Go main module) paths start from. Use it before depwhy_explain when you do not know what a directory contains, or to find out why a lockfile is not being searched.

Output: dir is the absolute directory. lockfiles has one entry per supported lockfile: ecosystem (npm, cargo, python, go; the value to pass as eco), lockfile, parsed, error (only when parsed is false), packages (distinct name and version pairs, roots included), roots ("name version", at most 20, with roots_total), and warnings (for example a missing Cargo.toml, which removes dev/build kinds). An empty lockfiles list is a valid answer, not an error. unsupported names lockfiles depwhy recognizes but does not read (yarn.lock, pnpm-lock.yaml, bun.lock, Pipfile.lock, pdm.lock, Gemfile.lock, composer.lock). If both npm-shrinkwrap.json and package-lock.json exist, only npm-shrinkwrap.json is listed, as npm does. ` + goNote,
		Annotations: mcp.ReadOnly("List lockfiles depwhy can read"),
		InputSchema: mcp.Object(map[string]any{"dir": dirProp()}),
		Handler: func(ctx context.Context, raw json.RawMessage) (mcp.Result, error) {
			var a struct {
				Dir string `json:"dir"`
			}
			if err := mcp.Decode(raw, &a); err != nil {
				return mcp.Result{}, err
			}
			rep, err := ecosystems(ctx, a.Dir, goTimeout)
			if err != nil {
				return mcp.Result{}, err
			}
			return mcp.JSONResult(rep)
		},
	}
}

type lockfileInfo struct {
	Ecosystem  string   `json:"ecosystem"`
	Lockfile   string   `json:"lockfile"`
	Parsed     bool     `json:"parsed"`
	Error      string   `json:"error,omitempty"`
	Packages   int      `json:"packages"`
	Roots      []string `json:"roots"`
	RootsTotal int      `json:"roots_total"`
	Warnings   []string `json:"warnings"`
}

type ecosystemsReport struct {
	Dir         string         `json:"dir"`
	Lockfiles   []lockfileInfo `json:"lockfiles"`
	Unsupported []string       `json:"unsupported"`
}

func ecosystems(ctx context.Context, dir string, goTimeout time.Duration) (ecosystemsReport, error) {
	if dir == "" {
		dir = "."
	}
	st, err := os.Stat(dir)
	if err != nil || !st.IsDir() {
		return ecosystemsReport{}, fmt.Errorf("%s is not a directory", dir)
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		abs = dir
	}
	sources, unsupported := eco.Detect(dir)
	rep := ecosystemsReport{Dir: abs, Lockfiles: []lockfileInfo{}, Unsupported: []string{}}
	rep.Unsupported = append(rep.Unsupported, unsupported...)
	for _, s := range sources {
		if err := ctx.Err(); err != nil {
			return ecosystemsReport{}, err
		}
		info := lockfileInfo{Ecosystem: s.Ecosystem, Lockfile: s.File, Roots: []string{}, Warnings: []string{}}
		g, err := eco.LoadContext(ctx, dir, s, goTimeout)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ecosystemsReport{}, err
			}
			info.Error = err.Error()
			rep.Lockfiles = append(rep.Lockfiles, info)
			continue
		}
		info.Parsed = true
		info.Warnings = append(info.Warnings, g.Warnings...)
		seen := map[string]bool{}
		for _, n := range g.Nodes {
			seen[n.Name+"\x00"+n.Version] = true
		}
		info.Packages = len(seen)
		rootSeen := map[string]bool{}
		var roots []string
		for _, id := range g.Roots() {
			n := g.Nodes[id]
			label := strings.TrimSpace(n.Name + " " + n.Version)
			if !rootSeen[label] {
				rootSeen[label] = true
				roots = append(roots, label)
			}
		}
		sort.Strings(roots)
		info.RootsTotal = len(roots)
		if len(roots) > maxRoots {
			roots = roots[:maxRoots]
		}
		info.Roots = append(info.Roots, roots...)
		rep.Lockfiles = append(rep.Lockfiles, info)
	}
	return rep, nil
}

func withDefault(schema map[string]any, def int) map[string]any {
	schema["default"] = def
	schema["minimum"] = 0
	return schema
}

func matchLabel(m explain.Match) string {
	s := m.Name
	if m.Version != "" {
		s += " " + m.Version
	}
	return s + " (" + m.Lockfile + ")"
}

func searchedList(s []explain.Searched) string {
	var out []string
	for _, x := range s {
		out = append(out, fmt.Sprintf("%s (%s)", x.Lockfile, x.Ecosystem))
	}
	return strings.Join(out, ", ")
}
