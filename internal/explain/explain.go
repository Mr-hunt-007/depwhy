// Package explain runs a depwhy query: detect lockfiles, load them, and find
// every path to the matching packages. The CLI and the MCP server both use it,
// so they return the same answers and the same JSON.
package explain

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/Mr-hunt-007/depwhy/internal/eco"
	"github.com/Mr-hunt-007/depwhy/internal/graph"
)

// Request is one query.
type Request struct {
	Dir      string // project directory; "" means "."
	Eco      string // --eco value; "" means every detected lockfile
	Query    string // package name or glob
	MaxPaths int    // paths kept per match; 0 keeps all
	// GoTimeout limits `go mod graph`; 0 means eco.DefaultGoTimeout.
	GoTimeout time.Duration
}

// Report is the --json output. Its shape is a compatibility contract.
type Report struct {
	Query       string     `json:"query"`
	Searched    []Searched `json:"searched"`
	Matches     []Match    `json:"matches"`
	Suggestions []string   `json:"suggestions,omitempty"`
	Warnings    []string   `json:"warnings"`
}

// Searched is a lockfile that was read successfully.
type Searched struct {
	Ecosystem string `json:"ecosystem"`
	Lockfile  string `json:"lockfile"`
}

// Step is one package on a path.
type Step struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
	Kind    string `json:"kind,omitempty"`
}

// Match is one name and version found in one lockfile.
type Match struct {
	Ecosystem     string   `json:"ecosystem"`
	Lockfile      string   `json:"lockfile"`
	Name          string   `json:"name"`
	Version       string   `json:"version,omitempty"`
	Flags         []string `json:"flags"`
	Root          bool     `json:"root"`
	TotalPaths    int      `json:"total_paths"`
	PathsComplete bool     `json:"paths_complete"`
	Paths         [][]Step `json:"paths"`
}

// Found is a match with its raw path result, for text rendering.
type Found struct {
	Ecosystem, Lockfile string
	Name, Version       string
	Flags               []string
	Root                bool
	Result              graph.PathResult
}

// LoadError is a lockfile that was detected but could not be read.
type LoadError struct {
	Source eco.Source
	Err    error
}

func (e LoadError) Error() string {
	return fmt.Sprintf("%s (%s): %v", e.Source.File, e.Source.Ecosystem, e.Err)
}

// Outcome is the result of a query that got as far as searching.
type Outcome struct {
	Report     Report
	Found      []Found
	LoadErrors []LoadError
}

// Exit codes, shared with the CLI.
const (
	ExitFound    = 0
	ExitNotFound = 1
	ExitError    = 2
)

// ExitCode is 0 when something matched, 1 when nothing did, and 2 when
// nothing matched and a lockfile could not be read.
func (o Outcome) ExitCode() int {
	if len(o.Found) > 0 {
		return ExitFound
	}
	if len(o.LoadErrors) > 0 {
		return ExitError
	}
	return ExitNotFound
}

// Selected returns the lockfiles in dir that a query with the given --eco
// value would search, and recognized lockfiles depwhy does not read. The
// error is one a user can act on (bad directory, unknown ecosystem, nothing
// to read).
func Selected(dir, ecoName string) ([]eco.Source, []string, error) {
	if dir == "" {
		dir = "."
	}
	st, err := os.Stat(dir)
	if err != nil || !st.IsDir() {
		return nil, nil, fmt.Errorf("%s is not a directory", dir)
	}
	sources, unsupported := eco.Detect(dir)
	if ecoName != "" {
		sel, ok := eco.Ecosystems[strings.ToLower(ecoName)]
		if !ok {
			return nil, unsupported, fmt.Errorf("unknown --eco %q (use npm, cargo, python, uv, poetry or go)", ecoName)
		}
		var kept []eco.Source
		for _, s := range sources {
			if sel(s) {
				kept = append(kept, s)
			}
		}
		if len(kept) == 0 {
			return nil, unsupported, fmt.Errorf("no %s lockfile in %s", ecoName, dir)
		}
		sources = kept
	}
	if len(sources) == 0 {
		msg := fmt.Sprintf("no supported lockfile in %s (looked for package-lock.json, npm-shrinkwrap.json, Cargo.lock, uv.lock, poetry.lock, go.mod)", dir)
		if len(unsupported) > 0 {
			msg += fmt.Sprintf("\nfound %s, which depwhy does not read", strings.Join(unsupported, ", "))
		}
		return nil, unsupported, errors.New(msg)
	}
	return sources, unsupported, nil
}

// Run executes a query. A returned error means the search did not start
// (see Selected) or ctx was cancelled; lockfiles that fail to load are
// reported in Outcome.LoadErrors and the others are still searched.
func Run(ctx context.Context, r Request) (Outcome, error) {
	if r.Dir == "" {
		r.Dir = "."
	}
	if r.MaxPaths < 0 {
		return Outcome{}, errors.New("max paths must be 0 or more")
	}
	sources, _, err := Selected(r.Dir, r.Eco)
	if err != nil {
		return Outcome{}, err
	}
	out := Outcome{Report: Report{Query: r.Query, Searched: []Searched{}, Matches: []Match{}, Warnings: []string{}}}
	for _, s := range sources {
		if err := ctx.Err(); err != nil {
			return Outcome{}, err
		}
		g, err := eco.LoadContext(ctx, r.Dir, s, r.GoTimeout)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil && errors.Is(err, ctxErr) {
				return Outcome{}, err
			}
			out.LoadErrors = append(out.LoadErrors, LoadError{Source: s, Err: err})
			continue
		}
		out.Report.Searched = append(out.Report.Searched, Searched{Ecosystem: g.Ecosystem, Lockfile: g.Source})
		out.Report.Warnings = append(out.Report.Warnings, g.Warnings...)
		found := findMatches(g, r.Query, r.MaxPaths)
		if len(found) == 0 {
			out.Report.Suggestions = append(out.Report.Suggestions, g.Suggest(r.Query, 5)...)
		}
		out.Found = append(out.Found, found...)
	}
	for _, f := range out.Found {
		out.Report.Matches = append(out.Report.Matches, f.toJSON())
	}
	out.Report.Suggestions = dedupe(out.Report.Suggestions)
	return out, nil
}

// findMatches groups matching nodes by name and version (npm can install the
// same version in several places) and searches paths for each group.
func findMatches(g *graph.Graph, query string, maxPaths int) []Found {
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
	var out []Found
	for _, k := range order {
		m := Found{Ecosystem: g.Ecosystem, Lockfile: g.Source, Name: k.name, Version: k.version, Flags: []string{}}
		var ids []string
		seenFlag := map[string]bool{}
		allRoot := true
		for _, n := range groups[k] {
			ids = append(ids, n.ID)
			allRoot = allRoot && n.Root
			for _, f := range n.Flags {
				if !seenFlag[f] {
					seenFlag[f] = true
					m.Flags = append(m.Flags, f)
				}
			}
		}
		m.Root = allRoot
		m.Result = g.Paths(ids, maxPaths, 0)
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

func (m Found) toJSON() Match {
	j := Match{
		Ecosystem: m.Ecosystem, Lockfile: m.Lockfile, Name: m.Name, Version: m.Version,
		Flags: m.Flags, Root: m.Root, TotalPaths: m.Result.Total, PathsComplete: m.Result.Complete,
		Paths: [][]Step{},
	}
	for _, p := range m.Result.Paths {
		steps := make([]Step, 0, len(p))
		for i, s := range p {
			st := Step{Name: s.Node.Name, Version: s.Node.Version}
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
