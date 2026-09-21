# Editors

Two editors are configured here. Zed, through `.zed/`, is the editor on the WSL host. Neovim,
through `hack/agent-home/`, is the editor **inside** the agent container. Neither is required — the
gates are `hack/format.sh`, the pre-commit hook and `make fmt vet test`, and they run the same
whether an editor is open or not. Editor config exists only so the editor stops fighting them.

There is deliberately no dev container. See
[Why there is no `.devcontainer`](#why-there-is-no-devcontainer).

## What is checked in

```text
.zed/settings.json               formatters, gopls, the 100-column ruler, scan exclusions
.zed/tasks.json                  the make targets, bound to the task runner
hack/agent-home/.bashrc          the container's interactive shell: ble.sh, starship, aliases
hack/agent-home/.blerc           ble.sh in vi mode
hack/agent-home/.config/starship.toml   the prompt
hack/agent-home/.config/nvim/    LazyVim configuration, copied into the image
```

`.zed/` and `hack/agent-home/` are checked in deliberately. `.vscode/` and `.idea/` stay gitignored
because they collect personal state; Zed keeps personal state in `~/.config/zed/settings.json` and
Neovim keeps its plugin and LSP state in `~/.local/share/nvim`, so the project halves are safe to
share and the container's environment gets reviewed in the same diff as the code.

## Zed, on the host

### Formatting

The repository has one formatting rule set and `hack/format.sh` is its entry point. Zed is
configured to agree with it rather than to replace it:

| Files              | On save in Zed                  | In `hack/format.sh`                      |
| ------------------ | ------------------------------- | ---------------------------------------- |
| `*.go`             | gopls (gofmt + organizeImports) | `gofmt -w`                               |
| `*.md`             | Prettier, reading `.prettierrc` | `markdownlint-cli2 --fix`, then Prettier |
| `*.json`, `*.yaml` | Prettier, reading `.prettierrc` | Prettier                                 |

The one gap is `markdownlint-cli2 --fix`, which has no stdin mode and so cannot be a Zed formatter.
It owns structure — list markers, fence spacing — not whitespace, so a save that skips it produces a
file Prettier already agrees with. The pre-commit hook runs the full `hack/format.sh` over the
staged set and then blocks on `markdownlint-cli2`, so nothing reaches a commit unformatted.

### Tasks

`task: spawn` from the command palette lists the entries in `.zed/tasks.json` — the
`make fmt vet test` gate, `make manifests generate`, `make lint`, a per-package `go test`, the kind
cluster script, and a shell in the agent container. They are the same commands documented elsewhere;
the task list just saves typing them.

## Neovim, in the agent container

`make -f agent.Makefile agent-shell` drops you into a bash shell with ble.sh, starship and Neovim,
and that shell is the IDE. The configuration is the LazyVim setup from the host, vendored into
`hack/agent-home/` and copied into the image at build time — copied rather than mounted, because the
read-only targets mount only the workspace and a shell that loses its prompt and its editor under
`--read-only` is not usable.

The image pins the versions the host runs, so a session inside the container behaves like a session
outside it:

| Tool                      | Pin                              | Why it is not from apt                             |
| ------------------------- | -------------------------------- | -------------------------------------------------- |
| Neovim                    | `NVIM_VERSION=0.12.5`            | Debian stable is far older than LazyVim needs      |
| starship                  | `STARSHIP_VERSION=1.25.0`        | not packaged                                       |
| ble.sh                    | `BLESH_NIGHTLY=20260310+b99cadb` | not packaged; upstream tags releases rarely        |
| gopls, gofumpt, goimports | `v0.23.0`, `v0.12.0`, `v0.50.0`  | `go install`ed so the editor works on first launch |

Override any of them with `--build-arg`. `GITHUB_MIRROR` redirects every release download at once,
for a build network that cannot reach GitHub.com.

### First run

Plugins are not baked into the image. They live in named volumes, for the same reason the Go caches
do — they are large, fetched from the network, and would otherwise be rebuilt by every
`docker run --rm`. Populate them once:

```bash
make -f agent.Makefile agent-nvim
```

That runs `nvim --headless "+Lazy! restore" "+TSUpdateSync" +qa`, so the plugin set matches
`hack/agent-home/.config/nvim/lazy-lock.json` exactly rather than whatever is current upstream.
After it finishes, `agent-shell` and `agent-review` reuse the volumes. To start over:

```bash
make -f agent.Makefile agent-nvim-reset
```

`gopls`, `gofumpt` and `goimports` are already on `PATH` in the image, and `lua/plugins/go.lua`
prefers a `PATH` gopls over Mason's copy, so Go LSP works before any Mason download happens.

### What differs from the host config

One change, in `lua/config/options.lua`: the WSL clipboard provider is guarded on `clip.exe` being
executable. In WSL it behaves exactly as before; in the container, where the Windows binaries do not
exist, an unguarded provider makes every yank fail with a shell error.

`lua/config/system_theme.lua` needs no guard — it already probes for WSL and returns nothing when
the probe fails.

The vendored tree is a copy, not a symlink, so it and `~/.config/nvim` drift independently. Copy in
the direction you mean when you change one.

## Why there is no `.devcontainer`

The repository carried a `.devcontainer/devcontainer.json` and it has been removed. Two reasons, and
the second is the one that matters.

The development host is already WSL. A dev container on top of it is a second Linux userland inside
the first, holding a duplicate of a toolchain that `hack/set-up-cluster.sh` and Go 1.25 under
`~/.local/go` already provide. `agent.Dockerfile` covers the case where a bounded environment is
genuinely wanted, with a boundary that is written down — see
[agent-container.md](agent-container.md).

And it never actually opened. Zed on Windows opens a WSL folder as a **remote project**: the `zed`
shim detects WSL and re-execs `zed.exe --wsl "$USER@$WSL_DISTRO_NAME"`, so the UI runs on Windows
and a headless `zed-remote-server` runs in the distro — architecturally the same as an SSH
connection. Zed's dev container support drives the Docker CLI on the machine the UI is running on,
so it is implemented for local projects only; on a remote project it never looks for
`.devcontainer/devcontainer.json`, which is why there was no prompt rather than an error. Docker
being installed and healthy in the distro is beside the point — it is on the wrong side of the
boundary. Remote dev containers are tracked at
[zed#46320](https://github.com/zed-industries/zed/issues/46320) and discussed at
[zed#56252](https://github.com/zed-industries/zed/discussions/56252), where the WSL case is called
out by name.
