# forge

`forge` is a declarative, TOML-driven build orchestrator. One `forge.toml`
describes any program's setup — apt packages, source fetch + compile, prebuilt
binaries, shell commands, and verification — as a list of `[[components]]`,
each carrying an ordered `ops` list. `forge build` executes it with topological
dependency ordering and parallel steps.

It replaces the `setup.sh` + numbered shell-script pattern used in container
projects (see `container-scripts/docker/postgresql` and `apache-kafka`), and
works on regular projects too.

## Commands

```
forge init [dir]                 Scaffold a new forge.toml
forge validate <manifest.toml>   Validate a manifest without executing
forge build <manifest.toml>      Build a project from a manifest
    --parallel N                 Max concurrent steps (0 = auto)
    --dry-run                    Print the execution plan without running
    --fail-fast=false            Continue past failed steps
    -v, --verbose                Stream live command output
    --var KEY=VALUE              Override a manifest var (repeatable)
    --no-cache                   Disable the build cache
    --cache-dir DIR              Override the build cache directory
```

## Manifest

A manifest is a `[[components]]` list. Each component is an ordered `ops`
list; the order of the ops **is** the build lifecycle. There is no `type`
property, no `[[steps]]`, no `run`/`fetch`/`build`/`install`-as-property, and
no `preparation`/`post` — every action is an op in `ops`.

```toml
[project]
name = "demo"
description = ""

[vars]
VERSION = "1.0"
POSTGRESQL_VERSION = "17"

[[components]]
name = "base"
ops = [
  { apt = { build = ["build-essential", "pkg-config"],
            runtime = ["curl", "ca-certificates"],
            conditional = [{ category = "build",
                             when = { var = "POSTGRESQL_VERSION", gte = "17" },
                             packages = ["bison", "flex"] }] } },
  { user = { name = "app", create_home = true, system = true,
             shell = "/bin/bash" } },
  { mkdir = [{ path = "/opt/app", mode = "0755", owner = "app" }] },
]

[[components]]
name = "app"
ops = [
  { install = { source = { type = "archive",
                           source = "https://example.com/app-${VERSION}.tar.gz",
                           ref = "",
                           strategy = "configure",
                           prefix = "/usr/local/app",
                           flags = ["--with-ssl"],
                           jobs = 4,
                           before = [{ raw = "echo preparing" }],
                           after  = [{ verify = [{ file = "/usr/local/app/bin/app" }] }] } } },
  { copy = [{ from = "conf/app.toml", to = "/etc/app/app.toml", mode = "0644" }] },
  { touch = ["/var/log/app.log"] },
  { chown = [{ path = "/var/log/app.log", owner = "app", group = "app",
               recursive = true }] },
  { chmod = [{ path = "/opt/app/run.sh", mode = "0700" }] },
  { generate = { tool = "protoc-c", input = "proto/msg.proto",
                 out = "gen", flags = ["--proto_path=proto"] } },
  { verify = [{ file = "/usr/local/app/bin/app" }] },
]

[[components]]
name = "metrics"
ops = [
  { install = { binary = { source = "https://example.com/metrics.tar.gz",
                           copy = [{ from = "metrics", to = "/usr/local/bin/metrics",
                                    mode = "0755" }] } } },
  { raw = "systemctl daemon-reload" },
]
```

## Operations

| Operation    | Purpose                                                        |
| ------------ | -------------------------------------------------------------- |
| `install`    | Install software — polymorphic: `apt`, `source`, or `binary`.  |
| `user`       | Create a system/user account.                                  |
| `mkdir`      | Create directories with mode and owner.                        |
| `chown`      | Change ownership of a path.                                    |
| `chmod`      | Change permissions of a path.                                  |
| `copy`       | Copy a file, with optional mode.                               |
| `touch`      | Create an empty file.                                          |
| `apt`        | Install or remove packages (`action` = install/remove).        |
| `generate`   | Generate code (e.g. protoc-c).                                 |
| `verify`     | Check files/dirs exist.                                        |
| `raw`        | Run an arbitrary shell command (last resort).                  |

Each operation is a single-key table in the `ops` list:

- `install = { apt = { build = [...], runtime = [...], conditional = [...] } }`
- `install = { source = { type = "archive"|"git", source, ref, strategy, flags, prefix, jobs, install_target, env, verify, before, after } }`
- `install = { binary = { source, copy = [{ from, to, mode }] } }`
- `user = { name, create_home, system, shell }`
- `mkdir = [{ path, mode, owner }]`
- `chown = [{ path, owner, group, recursive }]`
- `chmod = [{ path, mode }]`
- `copy = [{ from, to, mode }]`
- `touch = ["/path"]`
- `apt = { action, packages }`
- `generate = { tool, input, out, flags }`
- `verify = [{ file }]`
- `raw = "shell command"`

### Apt build/runtime classification

Apt packages are split into `build` (installed, then removed after **all**
components finish) and `runtime` (installed, kept). Forge aggregates every
`build` package across all components and runs the cleanup lifecycle once:
reinstall runtime, remove build, autoremove, clean. Conditional packages use a
structured version condition:

```toml
conditional = [{ category = "build",
                 when = { var = "POSTGRESQL_VERSION", gte = "17" },
                 packages = ["bison", "flex"] }]
```

Validation rejects a package listed in both `build` and `runtime`, or a
conditional package not listed in its category.

### Source install phases

A source install op has `before` (ops run after fetch, before build) and
`after` (ops run after install) — both run in the source dir, deterministically.
`generate` creates its output directory.

## Build strategies (`install.source.strategy`)

- `configure` — `./configure && make`
- `autogen` — `autoreconf -fi && ./configure && make`
- `cmake` — `cmake && make`
- `meson` — `meson setup && ninja`
- `make` — `make && make install`
- `detect` — auto-select from the source tree

## Interpolation

`${VAR}` and `${VAR:-default}` resolve from `[vars]`, the environment, and
`--var` overrides (precedence: vars < env < overrides).

## Build cache

Caching is fully automatic. A component's cache key hashes its ops, the vars
it references, and its dependencies' keys; a hit is trusted only if the step's
outputs still exist. On a hit whose files vanished (e.g. a Docker layer
rebuild reset the filesystem), forge restores from an artifact archive stored
under `<cache-dir>/artifacts/<project>/` before re-executing. With BuildKit,
mount the cache dir (`RUN --mount=type=cache,target=/var/cache/forge
... --cache-dir /var/cache/forge`) so artifacts survive across builds; CI
cache export (`--cache-to type=gha,mode=max`) persists them too.

## Architecture

Clean architecture following the [go-clean-arch](https://github.com/bxcodec/go-clean-arch) conventions

## Development

```
make build      # build ./bin/forge
make test       # run tests
make vet        # go vet
make mocks      # regenerate mockery mocks
```
