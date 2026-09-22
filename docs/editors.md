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
hack/agent-home/.bashrc          interactive bash for the host and the container alike
hack/agent-home/.bash_aliases    the git, docker and kubectl shortcuts
hack/agent-home/.blerc           ble.sh in vi mode
hack/agent-home/.config/starship.toml   the prompt
hack/agent-home/.config/nvim/    LazyVim configuration
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
and that shell is the IDE. The configuration lives in `hack/agent-home/` and reaches the container
twice over: copied into the image at build time, so the image works standalone and a `--read-only`
session still has its prompt and its editor, and then bind-mounted over that copy by the run
targets, so editing a file in `hack/agent-home/` takes effect in the next shell without a rebuild.

The mounts are read-only except `.config/nvim`, which is writable in `agent-shell` because lazy.nvim
rewrites `lazy-lock.json` when plugins are updated and that belongs back in the repository as a diff
to review. `agent-review` takes the editor read-only too: a session that cannot change the code
should not change its own editor.

The image pins the versions the host runs, so a session inside the container behaves like a session
outside it:

| Tool                      | Pin                              | Why it is not from apt                             |
| ------------------------- | -------------------------------- | -------------------------------------------------- |
| Neovim                    | `NVIM_VERSION=0.12.5`            | Debian stable is far older than LazyVim needs      |
| starship                  | `STARSHIP_VERSION=1.25.0`        | not packaged                                       |
| ble.sh                    | `BLESH_NIGHTLY=20260310+b99cadb` | not packaged; upstream tags releases rarely        |
| gopls, gofumpt, goimports | `v0.23.0`, `v0.12.0`, `v0.50.0`  | `go install`ed so the editor works on first launch |
| Claude Code               | `CLAUDE_VERSION=2.1.278`         | npm, alongside the other agent CLIs                |

Override any of them with `--build-arg`. `GITHUB_MIRROR` redirects every release download at once,
for a build network that cannot reach GitHub.com, and `GO_PROXY` defaults to the host's
`go env GOPROXY`.

The three Go tools declare `go 1.26.0` while the image is deliberately on Go 1.25 — the toolchain
this repository builds with — so their build layer, and only that layer, sets `GOTOOLCHAIN=auto`.
The fetched toolchain unpacks into the module cache and is deleted with it before the layer is
committed, so the finished image cannot reach for another toolchain and the product build still uses
the base image's Go.

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

### One config, two places

`hack/agent-home/.bashrc` is written to work unchanged on the WSL host and in the container. That is
possible because almost everything in it is already behind an existence guard — nvm, sdkman, cargo,
brew, zoxide and lesspipe simply do not fire in the container — so no branch on the environment is
needed. Three things did need attention:

- ble.sh installs to `~/.local/share/blesh` on the host and `/usr/share/blesh` in the image, so the
  file probes both. It is sourced with `--noattach` and attached on the last line, after starship
  has installed its prompt; attaching first and letting starship rewrite the prompt is the usual way
  those two end up fighting over the cursor.
- `$USER` is unset in a container and ble.sh treats that as an insane environment, so the file
  derives it. Not an image `ENV`, because `agent-cache` enters as root and a baked value would lie.
- `EDITOR` and `VISUAL` come from `~/.bash_env` on the host, which is not vendored, so the file
  supplies them with `:-` and leaves the host's value alone.

In `lua/config/options.lua`, the WSL clipboard provider is guarded on `clip.exe` being executable.
In WSL it behaves exactly as before; in the container, where the Windows binaries do not exist, an
unguarded provider makes every yank fail with a shell error. `lua/config/system_theme.lua` needs no
guard — it already probes for WSL and returns nothing when the probe fails.

### What is deliberately not vendored

- `~/.bash_env` holds real API tokens. It must never reach a git repository, and the `.bashrc` guard
  skips it when absent, which is the container case. Mount it explicitly with `AGENT_AI_MOUNTS` if a
  session genuinely needs one of those tokens.
- `~/.profile` is read by login shells only, and the container's are not login shells.
- `~/.gitconfig` names an identity and a `gh` credential helper that the image does not have. Git
  identity for commits made in the container comes from the mounted checkout's own `.git/config`.
- `~/.tmux.conf`, because tmux is not installed in the image.

The host's own dotfiles are still the originals; `hack/agent-home/` holds copies, so the two drift
independently. Point `~/.bashrc` and `~/.config/nvim` at the repository copies if you would rather
have one source of truth for both.

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
