# AGENTS.md

## What this is

depwhy explains why a dependency is in a project. It reads lockfiles directly
(npm `package-lock.json`/`npm-shrinkwrap.json`, `Cargo.lock`, `uv.lock`,
`poetry.lock`, and Go via `go mod graph`) and prints every path from the
project roots to a package, in one format for all ecosystems. Go module
`github.com/Mr-hunt-007/depwhy`, standard library only.

## Layout

- `main.go`: process boundary (stdout, stderr, stdin, TTY and `NO_COLOR` detection).
- `internal/cli`: flag parsing, help text, exit codes, text rendering, `--mcp` startup.
- `internal/explain`: the query core shared by the CLI and the MCP server
  (detect lockfiles, load, match, find paths, build the `--json` report).
- `internal/eco`: one reader per ecosystem (`npm.go`, `cargo.go`, `python.go`,
  `gomod.go`) and `load.go` (detection, `go mod graph` with a context and timeout).
- `internal/graph`: ecosystem-neutral graph, glob matching, version ordering,
  shortest-first path search with a budget.
- `internal/toml`: the minimal TOML reader for lockfiles and manifests.
- `internal/mcp`: the shared stdio MCP server (copied from a sibling project; keep it unchanged).
- `internal/mcptools`: the depwhy MCP tools (`depwhy_explain`, `depwhy_ecosystems`).
- `skills/depwhy/SKILL.md`: agent skill for using the CLI.

## Build and test (as CI runs them)

```
gofmt -l .          # must print nothing
go vet ./...
go test -race ./...
go build -o depwhy .
```

CI runs these on ubuntu-latest, macos-latest and windows-latest.

## Rules for contributors

- Standard library only. No `require` lines in `go.mod`. Go 1.22.
- gofmt clean. Keep logic in pure functions; keep filesystem and process calls thin.
- Tests use real fixtures written in `t.TempDir()`, table-driven where it fits.
  Tests must pass on Windows (use `filepath`, no shell) or skip with a reason.
- README terminal output is pasted from real runs of the binary, never typed by hand.
- No em dashes (U+2014) in code, docs or commit messages.
- The `--json` shape (`query`, `searched`, `matches[].ecosystem`, `lockfile`,
  `name`, `version`, `flags`, `root`, `total_paths`, `paths_complete`,
  `paths`, `suggestions`, `warnings`) is a compatibility contract. Add fields;
  do not rename or remove them. The MCP `depwhy_explain` tool returns the same shape.
- Exit codes: 0 found, 1 not found in any searched lockfile, 2 usage error,
  no supported lockfile, or a lockfile failed to load and nothing matched.
- MCP handlers must never write to stdout, call `os.Exit` or `os.Chdir`.

## Using depwhy as an agent

- `depwhy <package> --json [--dir DIR] [--eco npm|cargo|python|uv|poetry|go] [--max-paths N]`.
  Globs with `*` and `?` work. Check the exit code before parsing.
- `total_paths` counts all paths; `paths` holds at most `--max-paths` (default 10, 0 for all).
  `paths_complete: false` means the count is a lower bound.
- `depwhy --mcp` serves MCP on stdio with read-only tools `depwhy_explain` and
  `depwhy_ecosystems`. Go projects run `go mod graph`, which may download modules.
