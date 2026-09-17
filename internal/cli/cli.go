// Package cli implements the depwhy command line.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/Mr-hunt-007/depwhy/internal/explain"
	"github.com/Mr-hunt-007/depwhy/internal/mcptools"
)

// Version is the release version.
const Version = "0.2.0"

// Exit codes.
const (
	ExitFound    = explain.ExitFound
	ExitNotFound = explain.ExitNotFound
	ExitError    = explain.ExitError
)

// Env describes the process environment the CLI needs.
type Env struct {
	StdoutIsTTY bool
	NoColorEnv  bool      // NO_COLOR is set
	Stdin       io.Reader // read by --mcp
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

MCP server (for AI coding agents):
  --mcp             serve the Model Context Protocol on stdin/stdout instead
                    of running a query; other flags are ignored. Tools:
                    depwhy_explain and depwhy_ecosystems, both read-only
  --allow-destructive
                    accepted with --mcp for consistency with sibling tools;
                    depwhy has no destructive tools, so it changes nothing

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
	mcp      bool

	allowDestructive bool
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
	fs.BoolVar(&o.mcp, "mcp", false, "")
	fs.BoolVar(&o.allowDestructive, "allow-destructive", false, "")
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
	if o.allowDestructive && !o.mcp {
		fmt.Fprintf(stderr, "depwhy: --allow-destructive only applies with --mcp\n")
		return ExitError
	}
	if o.mcp {
		if len(positional) > 0 {
			fmt.Fprintf(stderr, "depwhy: --mcp takes no package name (got %s)\n", strings.Join(positional, " "))
			return ExitError
		}
		return serveMCP(stdout, stderr, env, o.allowDestructive)
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

	res, err := explain.Run(context.Background(), explain.Request{Dir: o.dir, Eco: o.eco, Query: query, MaxPaths: o.maxPaths})
	if err != nil {
		fmt.Fprintf(stderr, "depwhy: %s\n", strings.ReplaceAll(err.Error(), "\n", "\ndepwhy: "))
		return ExitError
	}
	for _, le := range res.LoadErrors {
		fmt.Fprintf(stderr, "depwhy: %v\n", le)
	}
	rep := res.Report
	code := res.ExitCode()

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
	if len(res.Found) == 0 {
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
	for i, m := range res.Found {
		if i > 0 {
			fmt.Fprintln(stdout)
		}
		renderMatch(stdout, c, m)
	}
	return code
}

func serveMCP(stdout, stderr io.Writer, env Env, allowDestructive bool) int {
	stdin := env.Stdin
	if stdin == nil {
		stdin = strings.NewReader("")
	}
	srv := mcptools.New(Version, allowDestructive)
	if err := srv.Serve(context.Background(), stdin, stdout); err != nil {
		fmt.Fprintf(stderr, "depwhy: mcp: %v\n", err)
		return ExitError
	}
	return ExitFound
}
