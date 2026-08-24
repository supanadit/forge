# forge

`forge` is a declarative, TOML-driven build orchestrator. One `forge.toml`
describes any program's setup — apt packages, source fetch + compile, prebuilt
binaries, shell commands, and verification — and `forge build` executes it with
topological dependency ordering and parallel steps.

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
```

## Manifest

```toml
[project]
name = "demo"
description = ""

[vars]
VERSION = "1.0"

[[includes]]                      # inline splice: steps inserted here
path = "shared/apt-cleanup.toml"

[[includes]]                      # named group: referenced via `use`
path = "shared/locale.toml"
as = "locale"

[[steps]]
name = "install-deps"
run = "apt"
action = "install"
packages = ["curl", "build-essential"]
packages_conditional = [{ condition = "${VERSION%%.*} >= 17", packages = ["bison"] }]

[[steps]]
name = "build-app"
depends_on = ["install-deps"]
run = "source"
fetch = { type = "archive", url = "https://example.com/app-${VERSION}.tar.gz" }
build = { strategy = "configure", prefix = "/usr/local/app", flags = ["--with-ssl"] }
install = true
verify = [{ file = "/usr/local/app/bin/app" }]

[[steps]]
name = "git-source"
run = "source"
fetch = { type = "git", url = "https://github.com/org/repo.git", ref = "v${VERSION}", depth = 1 }
build = { strategy = "cmake" }

[[steps]]
name = "metrics"
run = "binary"
fetch = { type = "archive", url = "https://example.com/metrics.tar.gz" }
install = { copy = [{ from = "metrics", to = "/usr/local/bin/metrics", mode = "0755" }] }

[[steps]]
name = "configure-runtime"
run = "shell"
commands = ["echo 'done'", "ldconfig"]
env = { PATH = "/usr/local/app/bin:${PATH}" }

[[steps]]
name = "locale"
use = "locale"                    # splice a named include group
```

## Step kinds

| `run`    | Purpose                | Key fields                                                          |
| -------- | ---------------------- | ------------------------------------------------------------------- |
| `apt`    | System packages        | `action` (install/remove/purge), `packages`, `packages_conditional` |
| `source` | Fetch + compile source | `fetch`, `build`, `install`, `from`, `dir`, `verify`                |
| `binary` | Prebuilt binary        | `fetch` (archive), `install.copy`                                   |
| `shell`  | Arbitrary commands     | `commands`, `env`, `dir`, `verify`                                  |
| `verify` | File-existence checks  | `checks`                                                            |

## Build strategies (`build.strategy`)

- `configure` — `./configure && make` (autotools)
- `autogen` — `autoreconf -fi && ./configure && make`
- `cmake` — `cmake && make`
- `meson` — `meson setup && ninja`
- `make` — `make && make install`
- `detect` — auto-select from the source tree

## Fetch types (`fetch.type`)

- `archive` — download + extract (`tar.gz`, `tgz`, `tar`, `zip`), optional checksum
- `git` — `git clone` with optional branch/tag and depth

## Interpolation

`${VAR}` and `${VAR:-default}` resolve from `[vars]`, the environment, and
`--var` overrides (precedence: vars < env < overrides). `${step:NAME.source}`
and `${step:NAME.prefix}` reference a prior step's fetched source dir / install
prefix. Shell-style expansions like `${VAR%%.*}` are left verbatim for `bash`
to resolve in `shell` commands and `condition` expressions.

## Build cache

Caching is fully automatic — no `cache_verify` declarations. A step's cache
key hashes its config, the vars it references, and its dependencies' keys; a
hit is trusted only if the step's outputs still exist:

- `apt` install steps verify via dpkg.
- `source` steps verify via their install prefix or `verify` paths.
- `binary` steps verify via their copy destinations or `verify` paths.
- `shell` steps always cache: outputs (files/dirs created or modified) are
  discovered by diffing filesystem snapshots taken around the commands.

On a hit whose files vanished (e.g. a Docker layer rebuild reset the
filesystem), forge restores the step from an artifact archive stored under
`<cache-dir>/artifacts/<project>/` before falling back to re-execution. With
BuildKit, mount the cache dir (`RUN --mount=type=cache,target=/var/cache/forge
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
