// Package cli implements the depwhy command line.
package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/Mr-hunt-007/depwhy/internal/eco"
	"github.com/Mr-hunt-007/depwhy/internal/graph"
)

// Version is the release version.
const Version = "0.1.0"

// Exit codes.
const (
	ExitFound    = 0
	ExitNotFound = 1
	ExitError    = 2
)

// Env describes the process environment the CLI needs.
type Env struct {
	StdoutIsTTY bool
	NoColorEnv  bool // NO_COLOR is set
}

const usage = `depwhy: why is this dependency here?

Usage:
  depwhy [flags] <package>

Finds every lockfile depwhy understands in the directory and prints each path
from your project (or workspace member, or main module) to the package.

Supported:
  npm     package-lock.json (v1, v2, v3), npm-shrinkwrap.json
  cargo   Cargo.lock (+ Cargo.toml for workspace members and dev/build kinds)
  python  uv.lock, poetry.lock (+ pyproject.toml)
  go      go.mod (runs "go mod graph")

Examples:
  depwhy react
  depwhy serde --dir ./backend
  depwhy requests --eco python
  depwhy '@babel/*' --max-paths 3
  depwhy golang.org/x/text --json

Flags:
  --dir DIR         project directory to read (default ".")
  --eco NAME        only search one ecosystem: npm, cargo, python, go
                    (also: uv, poetry, pypi, rust)
  --max-paths N     paths to print per package, 0 for all (default 10)
  --json            print JSON
  --no-color        disable colour (also honours NO_COLOR)
  --version         print version
  -h, --help        show this help

Exit codes:
  0  the package was found
  1  the package is not in any searched lockfile
  2  usage error, or a lockfile could not be read
`

type options struct {
	dir      string
	eco      string
	maxPaths int
	json     bool
	noColor  bool
	version  bool
}

func parseArgs(args []string, stderr io.Writer) (options, []string, error) {
	var o options
	fs := flag.NewFlagSet("depwhy", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&o.dir, "dir", ".", "")
	fs.StringVar(&o.eco, "eco", "", "")
	fs.IntVar(&o.maxPaths, "max-paths", 10, "")
	fs.BoolVar(&o.json, "json", false, "")
	fs.BoolVar(&o.noColor, "no-color", false, "")
	fs.BoolVar(&o.version, "version", false, "")
	var positional []string
	for {
		if err := fs.Parse(args); err != nil {
			return o, nil, err
		}
		rest := fs.Args()
		if len(rest) == 0 {
			break
		}
		positional = append(positional, rest[0])
		args = rest[1:]
	}
	return o, positional, nil
}

// Run executes depwhy and returns the exit code.
func Run(args []string, stdout, stderr io.Writer, env Env) int {
	o, positional, err := parseArgs(args, stderr)
	if errors.Is(err, flag.ErrHelp) {
		fmt.Fprint(stdout, usage)
		return ExitFound
	}
	if err != nil {
		fmt.Fprintf(stderr, "depwhy: %v\nRun 'depwhy --help' for usage.\n", err)
		return ExitError
	}
	if o.version {
		fmt.Fprintf(stdout, "depwhy %s\n", Version)
		return ExitFound
	}
	if len(positional) != 1 {
		if len(positional) == 0 {
			fmt.Fprintf(stderr, "depwhy: missing package name\nRun 'depwhy --help' for usage.\n")
		} else {
			fmt.Fprintf(stderr, "depwhy: expected one package name, got %d (%s)\n", len(positional), strings.Join(positional, " "))
		}
		return ExitError
	}
	if o.maxPaths < 0 {
		fmt.Fprintf(stderr, "depwhy: --max-paths must be 0 or more\n")
		return ExitError
	}
	query := positional[0]
	st, err := os.Stat(o.dir)
	if err != nil || !st.IsDir() {
		fmt.Fprintf(stderr, "depwhy: %s is not a directory\n", o.dir)
		return ExitError
	}

	sources, unsupported := eco.Detect(o.dir)
	if o.eco != "" {
		sel, ok := eco.Ecosystems[strings.ToLower(o.eco)]
		if !ok {
			fmt.Fprintf(stderr, "depwhy: unknown --eco %q (use npm, cargo, python, uv, poetry or go)\n", o.eco)
			return ExitError
		}
		var kept []eco.Source
		for _, s := range sources {
			if sel(s) {
				kept = append(kept, s)
			}
		}
		if len(kept) == 0 {
			fmt.Fprintf(stderr, "depwhy: no %s lockfile in %s\n", o.eco, o.dir)
			return ExitError
		}
		sources = kept
	}
	if len(sources) == 0 {
		msg := fmt.Sprintf("depwhy: no supported lockfile in %s (looked for package-lock.json, npm-shrinkwrap.json, Cargo.lock, uv.lock, poetry.lock, go.mod)", o.dir)
		if len(unsupported) > 0 {
			msg += fmt.Sprintf("\ndepwhy: found %s, which depwhy does not read", strings.Join(unsupported, ", "))
		}
		fmt.Fprintln(stderr, msg)
		return ExitError
	}

	rep := report{Query: query, Searched: []searched{}, Matches: []jsonMatch{}, Warnings: []string{}}
	var matches []match
	loadFailed := false
	for _, s := range sources {
		g, err := eco.Load(o.dir, s)
		if err != nil {
			loadFailed = true
			fmt.Fprintf(stderr, "depwhy: %s (%s): %v\n", s.File, s.Ecosystem, err)
			continue
		}
		rep.Searched = append(rep.Searched, searched{Ecosystem: g.Ecosystem, Lockfile: g.Source})
		rep.Warnings = append(rep.Warnings, g.Warnings...)
		found := findMatches(g, query, o.maxPaths)
		if len(found) == 0 {
			rep.Suggestions = append(rep.Suggestions, g.Suggest(query, 5)...)
		}
		matches = append(matches, found...)
	}
	for _, m := range matches {
		rep.Matches = append(rep.Matches, m.toJSON())
	}
	rep.Suggestions = dedupe(rep.Suggestions)

	code := ExitFound
	if len(matches) == 0 {
		code = ExitNotFound
		if loadFailed {
			code = ExitError
		}
	}

	if o.json {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(rep); err != nil {
			fmt.Fprintf(stderr, "depwhy: %v\n", err)
			return ExitError
		}
		return code
	}

	for _, w := range rep.Warnings {
		fmt.Fprintf(stderr, "depwhy: warning: %s\n", w)
	}
	if len(matches) == 0 {
		if len(rep.Searched) == 0 {
			return code
		}
		var where []string
		for _, s := range rep.Searched {
			where = append(where, fmt.Sprintf("%s (%s)", s.Lockfile, s.Ecosystem))
		}
		fmt.Fprintf(stderr, "depwhy: %q not found in %s\n", query, strings.Join(where, ", "))
		if len(rep.Suggestions) > 0 {
			fmt.Fprintf(stderr, "depwhy: similar names: %s\n", strings.Join(rep.Suggestions, ", "))
		}
		return code
	}
	c := colors{on: env.StdoutIsTTY && !o.noColor && !env.NoColorEnv}
	for i, m := range matches {
		if i > 0 {
			fmt.Fprintln(stdout)
		}
		renderMatch(stdout, c, m)
	}
	return code
}

type match struct {
	eco, source   string
	name, version string
	flags         []string
	root          bool
	result        graph.PathResult
}

// findMatches groups matching nodes by name and version (npm can install the
// same version in several places) and searches paths for each group.
func findMatches(g *graph.Graph, query string, maxPaths int) []match {
	nodes := g.Match(query)
	type key struct{ name, version string }
	var order []key
	groups := map[key][]*graph.Node{}
	for _, n := range nodes {
		k := key{n.Name, n.Version}
		if _, ok := groups[k]; !ok {
			order = append(order, k)
		}
		groups[k] = append(groups[k], n)
	}
	var out []match
	for _, k := range order {
		m := match{eco: g.Ecosystem, source: g.Source, name: k.name, version: k.version, flags: []string{}}
		var ids []string
		seenFlag := map[string]bool{}
		allRoot := true
		for _, n := range groups[k] {
			ids = append(ids, n.ID)
			allRoot = allRoot && n.Root
			for _, f := range n.Flags {
				if !seenFlag[f] {
					seenFlag[f] = true
					m.flags = append(m.flags, f)
				}
			}
		}
		m.root = allRoot
		m.result = g.Paths(ids, maxPaths, 0)
		out = append(out, m)
	}
	return out
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

type report struct {
	Query       string      `json:"query"`
	Searched    []searched  `json:"searched"`
	Matches     []jsonMatch `json:"matches"`
	Suggestions []string    `json:"suggestions,omitempty"`
	Warnings    []string    `json:"warnings"`
}

type searched struct {
	Ecosystem string `json:"ecosystem"`
	Lockfile  string `json:"lockfile"`
}

type jsonStep struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
	Kind    string `json:"kind,omitempty"`
}

type jsonMatch struct {
	Ecosystem     string       `json:"ecosystem"`
	Lockfile      string       `json:"lockfile"`
	Name          string       `json:"name"`
	Version       string       `json:"version,omitempty"`
	Flags         []string     `json:"flags"`
	Root          bool         `json:"root"`
	TotalPaths    int          `json:"total_paths"`
	PathsComplete bool         `json:"paths_complete"`
	Paths         [][]jsonStep `json:"paths"`
}

func (m match) toJSON() jsonMatch {
	j := jsonMatch{
		Ecosystem: m.eco, Lockfile: m.source, Name: m.name, Version: m.version,
		Flags: m.flags, Root: m.root, TotalPaths: m.result.Total, PathsComplete: m.result.Complete,
		Paths: [][]jsonStep{},
	}
	for _, p := range m.result.Paths {
		steps := make([]jsonStep, 0, len(p))
		for i, s := range p {
			st := jsonStep{Name: s.Node.Name, Version: s.Node.Version}
			if i > 0 {
				st.Kind = s.Kind
				if st.Kind == "" {
					st.Kind = "normal"
				}
			}
			steps = append(steps, st)
		}
		j.Paths = append(j.Paths, steps)
	}
	return j
}
