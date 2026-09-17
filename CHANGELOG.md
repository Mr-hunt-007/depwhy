# Changelog

## 0.2.0 (2026-09-17)

- `depwhy --mcp` runs a Model Context Protocol server on stdio with two
  read-only tools: `depwhy_explain` (same JSON as `--json`, with `max_paths`
  and `max_matches` caps reported in `warnings`) and `depwhy_ecosystems`
  (detected lockfiles, whether each parsed, package counts and roots).
- `go mod graph` can be cancelled; in the MCP server it is limited to 50
  seconds and a timeout is reported as an error instead of blocking.
- `--allow-destructive` is accepted with `--mcp` for consistency with sibling
  tools; depwhy has no destructive tools.
- `AGENTS.md`, `CLAUDE.md`, `llms.txt` and an agent skill in `skills/depwhy`.

## 0.1.0

First release.

- `depwhy <package>` prints every path from the project to a package, shortest
  first, in one format for all supported ecosystems.
- npm: `package-lock.json` v1, v2 and v3 and `npm-shrinkwrap.json`, with Node
  module resolution, workspaces, aliases, and dev, optional and peer markers.
- Cargo: `Cargo.lock` (all formats), workspace members and dev/build kinds from
  `Cargo.toml`.
- Python: `uv.lock` and `poetry.lock` with `pyproject.toml` project
  dependencies, extras and dependency groups; PEP 503 name matching.
- Go: `go.mod` with `go mod graph`, collapsed to selected versions, with
  `// indirect` requirements marked.
- Glob patterns, `--eco`, `--max-paths`, `--json`, `--no-color` and `NO_COLOR`.
- Exit codes: 0 found, 1 not found, 2 error.
