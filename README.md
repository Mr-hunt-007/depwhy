# depwhy

Why is this dependency here? One command for npm, Cargo, Python (uv, Poetry) and Go.

```
$ depwhy serde
serde 1.0.229  (cargo, Cargo.lock)
my-core 0.2.0
└── serde 1.0.229
my-app 0.1.0
└── serde_json 1.0.151
    └── serde 1.0.229
2 paths
```

## Why

Every package manager has its own answer to "who pulled this in": `npm explain`,
`cargo tree -i`, `uv tree --invert`, `poetry show --why`, `go mod why -m`. They
take different arguments, print different shapes, and some need the toolchain
installed and a network connection. If you work across a few ecosystems, or
review repositories you did not write, you end up relearning each one.

depwhy reads the lockfiles directly and prints every path from your project to
the package in the same format for all of them. Point it at a directory and it
searches every lockfile it finds. (For npm in more depth, see the sibling tool
`whydep`.)

## Install

```
go install github.com/Mr-hunt-007/depwhy@latest
```

`go install` puts the binary in `$(go env GOPATH)/bin` (usually `~/go/bin`). If your shell says `command not found`, add that directory to your `PATH`:

```sh
echo 'export PATH="$PATH:$(go env GOPATH)/bin"' >> ~/.zshrc && source ~/.zshrc   # bash: ~/.bashrc
```

On Windows the Go installer adds `%USERPROFILE%\go\bin` to `PATH` for you.

Or build from source:

```
git clone https://github.com/Mr-hunt-007/depwhy
cd depwhy
go build -o depwhy .
```

No dependencies beyond the Go standard library. Go 1.22 or newer.

## Supported lockfiles

| Ecosystem | Files read | Roots (where paths start) |
|-----------|------------|---------------------------|
| npm | `package-lock.json` v1, v2, v3, or `npm-shrinkwrap.json` | the project and its workspaces (`package.json` is also read for v1 lockfiles) |
| cargo | `Cargo.lock`, plus `Cargo.toml` and member manifests | workspace members; without `Cargo.toml`, local crates nothing depends on |
| python | `uv.lock`, `poetry.lock`, plus `pyproject.toml` | uv: the project package or `[manifest] members`; Poetry: the project in `pyproject.toml` |
| go | `go.mod`, plus the output of `go mod graph` | the main module |

npm edges are resolved the way Node does it: from a package's location, look in
its own `node_modules`, then walk up. Cargo dependency entries of the form
`name`, `name version` and `name version (source)` are all resolved. Python
names are compared after PEP 503 normalization, so `Typing_Extensions`,
`typing-extensions` and `typing.extensions` match. Cargo names treat `-` and `_`
as the same.

Lockfiles that depwhy recognizes but does not read (`yarn.lock`,
`pnpm-lock.yaml`, `bun.lock`, `Pipfile.lock`, `pdm.lock` and a few others) are
named in the error message instead of being ignored silently.

## Usage

```
depwhy [flags] <package>
```

A directory with both `package-lock.json` and `uv.lock`: each package that
matches is printed with the lockfile it came from.

```
$ depwhy ms
ms 2.0.0  (npm, package-lock.json)
webapp 1.0.0
├── debug 2.6.9 [dev]
│   └── ms 2.0.0
└── express 4.21.0
    ├── debug 2.6.9
    │   └── ms 2.0.0
    ├── body-parser 1.20.3
    │   └── debug 2.6.9
    │       └── ms 2.0.0
    ├── finalhandler 1.3.1
    │   └── debug 2.6.9
    │       └── ms 2.0.0
    ├── send 0.19.0
    │   └── debug 2.6.9
    │       └── ms 2.0.0
    └── serve-static 1.16.2
        └── send 0.19.0
            └── debug 2.6.9
                └── ms 2.0.0
6 paths

ms 2.1.3  (npm, package-lock.json)
lib 0.2.0
└── ms 2.1.3
webapp 1.0.0
└── express 4.21.0
    ├── send 0.19.0
    │   └── ms 2.1.3
    └── serve-static 1.16.2
        └── send 0.19.0
            └── ms 2.1.3
3 paths
```

`lib` is an npm workspace, so it is a root of its own.

Edge kinds appear in brackets, and flags the lockfile records about the package
itself appear after the header:

```
$ depwhy react
react 18.3.1  (npm, package-lock.json)  dev, peer
webapp 1.0.0
└── react-dom 18.3.1 [dev]
    └── react 18.3.1 [peer]
```

```
$ depwhy pysocks --eco python
pysocks 1.7.1  (python, uv.lock)
my-app 0.3.0
└── requests 2.34.2
    └── pysocks 1.7.1 [extra:socks]
```

Patterns can use `*` and `?`. Several versions of one package are listed
separately:

```
$ depwhy 'getrand*'
getrandom 0.1.16  (cargo, Cargo.lock)
my-app 0.1.0
└── rand 0.7.3
    ├── getrandom 0.1.16
    ├── rand_core 0.5.1
    │   └── getrandom 0.1.16
    ├── rand_chacha 0.2.2
    │   └── rand_core 0.5.1
    │       └── getrandom 0.1.16
    └── rand_hc 0.2.0
        └── rand_core 0.5.1
            └── getrandom 0.1.16
4 paths

getrandom 0.2.17  (cargo, Cargo.lock)
my-app 0.1.0
└── rand 0.8.8
    ├── rand_core 0.6.4
    │   └── getrandom 0.2.17
    └── rand_chacha 0.3.1
        └── rand_core 0.6.4
            └── getrandom 0.2.17
2 paths
```

Paths are found shortest first. Only the first `--max-paths` are printed, with
the total:

```
$ depwhy golang.org/x/text --max-paths 3
golang.org/x/text v0.15.0  (go, go.mod)  indirect
example.com/app
└── github.com/gin-gonic/gin v1.10.0
    ├── golang.org/x/text v0.15.0
    ├── github.com/go-playground/locales v0.14.1
    │   └── golang.org/x/text v0.15.0
    └── github.com/go-playground/validator/v10 v10.20.0
        └── golang.org/x/text v0.15.0
showing 3 of 18 paths (use --max-paths 0 to show all)
```

```
$ depwhy leftpad
depwhy: "leftpad" not found in package-lock.json (npm), uv.lock (python)
```

### Flags

| Flag | Default | Meaning |
|------|---------|---------|
| `--dir DIR` | `.` | project directory to read |
| `--eco NAME` | all found | only search `npm`, `cargo`, `python` or `go` (also `uv`, `poetry`, `pypi`, `rust`) |
| `--max-paths N` | `10` | paths to print per package; `0` prints all |
| `--json` | off | print JSON instead of trees |
| `--no-color` | off | disable colour; `NO_COLOR` is also honoured, and colour is only used on a terminal |
| `--mcp` | off | run as an MCP server on stdin and stdout (see [Use with AI agents](#use-with-ai-agents)) |
| `--allow-destructive` | off | accepted with `--mcp`; depwhy has no destructive tools, so it changes nothing |
| `--version` | | print the version |
| `-h`, `--help` | | show help |

Flags may come before or after the package name.

### Edge kinds and flags

| Kind | Meaning |
|------|---------|
| (none) | normal dependency (`"kind": "normal"` in JSON) |
| `dev` | npm `devDependencies`, Cargo `[dev-dependencies]`, Poetry `dev` group, uv or PEP 735 `dev` group |
| `build` | Cargo `[build-dependencies]` |
| `optional` | npm `optionalDependencies`, Poetry optional dependency |
| `peer` | npm `peerDependencies` |
| `extra:NAME` | Python extra (`[project.optional-dependencies]`, uv optional dependencies) |
| `group:NAME` | Poetry or PEP 735 dependency group other than `dev` |
| `indirect` | a `// indirect` requirement in `go.mod` (see limitations) |

Package flags come from the lockfile: npm `dev`, `optional`, `devOptional`,
`peer`, `bundled`, `extraneous` and `alias:NAME`; Poetry `optional`, `dev` and
`group:NAMES`; Go `indirect`.

### JSON

`--json` prints one object. Real output from `depwhy itoa --json`:

```json
{
  "query": "itoa",
  "searched": [
    {
      "ecosystem": "cargo",
      "lockfile": "Cargo.lock"
    }
  ],
  "matches": [
    {
      "ecosystem": "cargo",
      "lockfile": "Cargo.lock",
      "name": "itoa",
      "version": "1.0.18",
      "flags": [],
      "root": false,
      "total_paths": 2,
      "paths_complete": true,
      "paths": [
        [
          {
            "name": "my-app",
            "version": "0.1.0"
          },
          {
            "name": "itoa",
            "version": "1.0.18",
            "kind": "dev"
          }
        ],
        [
          {
            "name": "my-app",
            "version": "0.1.0"
          },
          {
            "name": "serde_json",
            "version": "1.0.151",
            "kind": "normal"
          },
          {
            "name": "itoa",
            "version": "1.0.18",
            "kind": "normal"
          }
        ]
      ]
    }
  ],
  "warnings": []
}
```

- `matches` has one entry per name and version per lockfile. It is `[]` when
  nothing matched.
- Each path starts at a root. The first step has no `kind`; every later step
  has the kind of the edge that leads to it.
- `paths` holds at most `--max-paths` paths; `total_paths` counts all of them.
- `paths_complete` is `false` only if the search stopped early (see
  limitations); `total_paths` is then a lower bound.
- `root` is `true` when the package is itself a root. A package with
  `total_paths` of `0` is in the lockfile but not reachable from any root.
- `suggestions` (similar names) appears only when nothing matched.
- `version` is omitted when unknown (roots in Go, a Poetry project without a
  version).

### Exit codes

| Code | Meaning |
|------|---------|
| 0 | the package was found in at least one lockfile |
| 1 | the package is not in any searched lockfile |
| 2 | usage error, no supported lockfile, or a lockfile could not be read and the package was not found elsewhere |

If one lockfile fails to parse, the error is printed and the others are still
searched.

## Use with AI agents

The CLI already works well for agents: `--json` has a stable shape and the exit
codes are documented above.

depwhy also runs as a [Model Context Protocol](https://modelcontextprotocol.io)
server with `depwhy --mcp`, which Claude Code, Codex CLI, Cursor, VS Code and
Gemini CLI can start for you. The command must be on the `PATH` the client
sees. Editors started from a dock or launcher often do not inherit your shell
`PATH`, so use the absolute path if the server fails to start (find it with
`echo "$(go env GOPATH)/bin/depwhy"`).

Claude Code (add `--scope user` to enable it in every project):

```
claude mcp add depwhy -- depwhy --mcp
```

Codex CLI:

```
codex mcp add depwhy -- depwhy --mcp
```

or in `~/.codex/config.toml`:

```toml
[mcp_servers.depwhy]
command = "depwhy"
args = ["--mcp"]
```

Cursor, in `.cursor/mcp.json` (or `~/.cursor/mcp.json` for every project):

```json
{
  "mcpServers": {
    "depwhy": { "command": "depwhy", "args": ["--mcp"] }
  }
}
```

VS Code, in `.vscode/mcp.json`:

```json
{
  "servers": {
    "depwhy": { "type": "stdio", "command": "depwhy", "args": ["--mcp"] }
  }
}
```

Gemini CLI, in `~/.gemini/settings.json` (or `.gemini/settings.json` in a
project):

```json
{
  "mcpServers": {
    "depwhy": { "command": "depwhy", "args": ["--mcp"] }
  }
}
```

Tools:

| Tool | Access | Answers |
|------|--------|---------|
| `depwhy_explain` | read-only | every path from the project to a package (name or glob); arguments `package`, `dir`, `eco`, `max_paths` (default 10), `max_matches` (default 20); returns the same JSON as `--json` |
| `depwhy_ecosystems` | read-only | which lockfiles are in `dir`, whether each parsed, package counts, roots, and recognized lockfiles depwhy does not read |

Paths default to the server's working directory. When output is capped, or a
lockfile could not be read while another one could, `warnings` in the result
says so and names the argument that raises the cap. A query that matches
nothing and hit an unreadable lockfile comes back as a tool error, like exit
code 2.

Go projects: `go mod graph` may need network access to download module files.
Inside the MCP server it is stopped after 50 seconds and reported as an error
(Codex CLI gives up on a tool call after 60 seconds by default); a retry
usually gets further because downloaded modules stay in the module cache.
Start the server with `GOPROXY=off` in its environment to forbid downloads.

There are no destructive tools, so `--allow-destructive` has no effect.

An agent skill with usage notes is in `skills/depwhy`. Install it for Claude
Code with:

```
mkdir -p ~/.claude/skills && cp -r skills/depwhy ~/.claude/skills/
```

and for Codex CLI with:

```
mkdir -p ~/.agents/skills && cp -r skills/depwhy ~/.agents/skills/
```

Agents working on this repository should read [AGENTS.md](AGENTS.md).

## Limitations

- **Environment markers and platforms are not evaluated.** A lockfile records
  every platform and Python version; depwhy shows all of them. uv resolving
  `numpy` 1.26 for Python below 3.12 and 2.x above shows both, and Cargo
  dependencies behind `cfg(windows)` show on every OS.
- **Go shows the module requirement graph**, the same data as `go mod graph`,
  with each module at the version minimal version selection picks. This is not
  the package import graph that `go mod why -m` uses, so depwhy can show a path
  through a module whose packages you never import (test-only requirements of a
  dependency, for example). Since Go 1.17, `go.mod` lists every needed module
  with `// indirect`; that edge only says the module is needed, so depwhy uses it
  only when no other path exists. Reading Go needs the `go` command, and `go mod
  graph` may download module files. `go.work` workspaces are not supported:
  `go mod graph` reads only the module in `--dir`, and fails if it requires a
  workspace module that cannot be downloaded.
- **Paths stop at the first root.** In a workspace, a path from one member
  through another member is shown starting at the second member.
- **Cargo features are not evaluated.** `Cargo.lock` records optional
  dependencies even when no enabled feature turns them on, so depwhy can show a
  path that `cargo tree -i` (which resolves features) does not. `Cargo.lock` also
  records no dev or build distinction; depwhy reads it from the members'
  `Cargo.toml`, so it applies to direct dependencies of workspace members only.
- **Poetry** can lock several versions of one package for different markers.
  Constraints are not evaluated, so an edge to that name points at every locked
  version.
- **npm lockfile v1** does not record the project's own dependencies; depwhy
  reads `package.json` for them, and without it guesses (with a warning).
- **Path counts can grow exponentially** in large graphs. The search stops after
  creating 1,000,000 partial paths; the output then says "at least N".
- yarn, pnpm, bun, Pipfile and PDM lockfiles are not supported.
- The TOML reader covers what lockfiles and manifests use, and accepts TOML 1.1
  newlines and trailing commas in inline tables. Dates and times are rejected
  with a line number rather than misread; a `pyproject.toml` or
  `Cargo.toml` that cannot be read produces a warning and depwhy continues with
  the lockfile alone.

## License

MIT
