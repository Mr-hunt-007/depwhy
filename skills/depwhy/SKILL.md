---
name: depwhy
description: Explain why a dependency is in a project by printing every path from the project to the package, read from npm, Cargo, uv, Poetry or Go lockfiles. Use when asked "why is X installed", before removing or upgrading a package, or when tracing which direct dependency pulls in a vulnerable transitive package.
---

# depwhy

## When to use

- "Why is `lodash` / `serde` / `urllib3` / `golang.org/x/net` in this project?"
- Before removing or upgrading a dependency: find which direct dependency
  brings in a transitive one, and whether it is only a dev dependency.
- Auditing a vulnerable package: list every version installed and the path to each.

It reads `package-lock.json`, `npm-shrinkwrap.json`, `Cargo.lock`, `uv.lock`,
`poetry.lock` and `go.mod` (via `go mod graph`). It does not read yarn, pnpm,
bun, Pipfile or PDM lockfiles.

## Commands

```
depwhy <package> --json                   # search every lockfile in .
depwhy <package> --json --dir path/to/project
depwhy <package> --json --eco python      # npm, cargo, python, uv, poetry, go
depwhy '@babel/*' --json --max-paths 3    # globs with * and ?
depwhy <package> --json --max-paths 0     # all paths (can be large)
```

Always pass `--json` and check the exit code first.

## Reading the output

- `matches`: one entry per name, version and lockfile. `[]` when nothing
  matched; then `suggestions` lists similar names.
- `paths`: each path is a list of steps from a root (the project, a workspace
  member or the Go main module) to the package, shortest first. The first step
  has no `kind`; later steps have the kind of the edge leading to them:
  `normal`, `dev`, `build`, `optional`, `peer`, `extra:NAME`, `group:NAME`, `indirect`.
- A package is dev-only when every path contains a `dev` edge (use `--max-paths 0` to see them all).
- `total_paths` counts all paths; `paths` holds at most `--max-paths` (default 10).
- `paths_complete: false`: the search stopped early and `total_paths` is a lower bound.
- `root: true`: the package is itself a root. `total_paths: 0` with
  `root: false`: in the lockfile but unreachable (stale or orphaned entry).
- `flags`: markers the lockfile records on the package (`dev`, `optional`, `peer`, `indirect`, ...).
- `warnings`: problems reading manifests (for example no `Cargo.toml`, so no dev/build kinds).

## Exit codes

- 0: found in at least one lockfile
- 1: not found in any searched lockfile
- 2: usage error, no supported lockfile, or a lockfile failed to load and the package was not found elsewhere (details on stderr)

## Caveats

- Platform markers, Python environment markers and Cargo features are not
  evaluated: a path may apply only to another OS or a disabled feature.
- Go shows the module requirement graph, not package imports, and needs the
  `go` command; `go mod graph` may download modules (set `GOPROXY=off` to forbid it).
- depwhy only reads files. It never modifies the project.

## MCP

`depwhy --mcp` serves the same data over MCP with read-only tools
`depwhy_explain` (same JSON as `--json`) and `depwhy_ecosystems` (which
lockfiles exist and whether each parses).
