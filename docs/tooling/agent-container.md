# AI agent development container

Use this container when you want an AI coding agent to work on this repository while keeping its
normal commands scoped to the repository checkout. The setup is included in the repository by
default, but it is not enabled automatically: nothing uses it until you run `hack/agent.sh`.

This is different from the product image in the root `Dockerfile`, and it is also different from
Kubebuilder's generated `.devcontainer/`. The generated devcontainer is the ordinary human
development environment. `agent.Dockerfile` is an agent sandbox with Go, Make, Git, small CLI tools
for editing and testing, and the `claude`, `pi`, `codex` and `opencode` CLIs. It does not bake any
model-provider credentials into the image.

Quick start from the repository root:

```bash
hack/agent.sh image      # build the agent image locally
hack/agent.sh sync       # pull or build, then push so local and Docker Hub agree
hack/agent.sh shell      # open a write-capable repo-scoped shell
hack/agent.sh review     # open a read-only review shell
```

## Kubebuilder devcontainer baseline

Task 0.2 also keeps Kubebuilder's generated `.devcontainer/`, but with pinned tool versions. Use
these contents instead of the generated `latest` downloads.

`.devcontainer/devcontainer.json`:

```json
{
  "name": "k8s-ai-platform",
  "image": "golang:1.25-bookworm",
  "features": {
    "ghcr.io/devcontainers/features/docker-in-docker:2": {},
    "ghcr.io/devcontainers/features/git:1": {}
  },
  "runArgs": ["--network=host"],
  "customizations": {
    "vscode": {
      "settings": {
        "terminal.integrated.defaultProfile.linux": "bash"
      },
      "extensions": ["ms-kubernetes-tools.vscode-kubernetes-tools", "ms-azuretools.vscode-docker"]
    }
  },
  "onCreateCommand": "bash .devcontainer/post-install.sh"
}
```

`.devcontainer/post-install.sh`:

```bash
#!/usr/bin/env bash
set -euo pipefail

KIND_VERSION=${KIND_VERSION:-v0.32.0}
KUBEBUILDER_VERSION=${KUBEBUILDER_VERSION:-4.9.0}
KUBECTL_VERSION=${KUBECTL_VERSION:-v1.34.8}
TARGET_ARCH=${TARGET_ARCH:-amd64}

curl -fsSLo /usr/local/bin/kind \
  "https://kind.sigs.k8s.io/dl/${KIND_VERSION}/kind-linux-${TARGET_ARCH}"
chmod +x /usr/local/bin/kind

curl -fsSLo /tmp/kubebuilder \
  "https://go.kubebuilder.io/dl/${KUBEBUILDER_VERSION}/linux/${TARGET_ARCH}"
chmod +x /tmp/kubebuilder
mv /tmp/kubebuilder /usr/local/bin/kubebuilder

curl -fsSLo /usr/local/bin/kubectl \
  "https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/linux/${TARGET_ARCH}/kubectl"
chmod +x /usr/local/bin/kubectl

docker network create -d bridge --subnet 172.19.0.0/24 kind 2>/dev/null || true

go version
kind version
kubebuilder version
kubectl version --client
docker --version
```

The live `.devcontainer/` has moved on from that baseline in one way worth stating here: it runs as
a non-root user. `ghcr.io/devcontainers/features/common-utils` creates `agent` at uid/gid 1000 — the
same identity `agent.Dockerfile` creates — and `remoteUser` points the editor, its terminals and the
lifecycle commands at it. The workspace is a bind mount, so a devcontainer running as root wrote
root-owned files back to the host; `.git/config`, `.git/HEAD` and `.git/packed-refs` ended up that
way, and `hack/agent.sh`'s container, which runs as uid 1000, could then no longer rewrite them. Two
costs follow from the change and are paid in `post-install.sh`: its installs go through `sudo`, and
it chowns the directories Docker created for the mounts — including `$HOME/.cache`, which is both
the tools volume's parent and where `GOCACHE` lands.

Git pushes in the devcontainer use the host's identity rather than a key copied into the image:
`.devcontainer/devcontainer.json` mounts `~/.ssh` and `~/.gitconfig` read-only at the same paths.
The bind keeps the key's `0600` mode and host uid, which `updateRemoteUserUID` already matches, so
`ssh` accepts it and `git push` authenticates exactly as it does on the host. The mount is read-only
on purpose: the container borrows the identity, it cannot rotate the keys or rewrite the host's
config. Mount changes only reach a container created after the edit, so reopen or rebuild the
devcontainer if a running session still cannot push.

The agent container below is a separate, stronger sandbox for running AI CLIs. Keep both available:
the devcontainer is the ordinary development environment, while `agent.Dockerfile` is opt-in. The
agent container deliberately does not inherit these mounts.

### Networking in the devcontainer

The container runs with `--network=host`, overridable by setting `DEVCONTAINER_NETWORK` on the host
(e.g. `bridge`) before VS Code starts.

Host is the default for the **cluster work**, not for a proxy. `kind` publishes its API server on
the host's `127.0.0.1`, and sharing that network namespace is what makes one `~/.kube/config` valid
from the host and from inside the container at the same time — which is the whole reason
`hack/set-up-cluster.sh` can run in either place. On `bridge`, the mounted kubeconfig points at a
loopback address the container no longer shares and `kubectl` stops reaching a kind cluster. Treat
the override as an escape hatch for a host where host networking is unavailable, not as a tidy-up.

A proxy bound to the host's loopback being reachable is a consequence of the same namespace sharing,
not the reason for it. The build network is separate and separately overridable with
`DEVCONTAINER_BUILD_NETWORK`; on a host that reaches `github.com` and `proxy.golang.org` directly, a
bridge build is fine.

### Proxy settings in the devcontainer

On a network that needs an outbound proxy, the container reads it from `~/.proxy_env` on the host,
bind-mounted read-only and sourced by `hack/agent-home/.zprofile`. Copy the template and set your
port:

```bash
cp hack/proxy-env.example ~/.proxy_env
```

`devcontainer.json` also has `${localEnv:http_proxy}` in `containerEnv`, but that is substituted
from the environment of the **VS Code process on the host**, when the file is parsed. Launch VS Code
from the desktop rather than from a shell that exports the proxy, and those resolve to empty. It is
not `userEnvProbe`, which probes a shell _inside_ the container — nothing in `containerEnv` reads
the host's `~/.profile`.

The login shell is the dependable path precisely because `userEnvProbe` defaults to
`loginInteractiveShell`: what `.zprofile` exports is applied to the terminals **and to the extension
host**. That matters because the two halves of "the extension works" have different network paths.
Installing an extension is done by the VS Code client on the host and pushed into the container, so
extensions install fine with no proxy inside; but an extension that makes its own HTTPS calls —
`github.vscode-github-actions` asking `api.github.com`, for one — runs in the container and fails
with _unable to connect, please check internet connection_.

On a host with no proxy at all, none of this does anything: `~/.proxy_env` stays empty and the guard
in `.zprofile` makes it a no-op. Keep it anyway — it costs a mount and three lines, and it removes a
dependency on how VS Code happened to be launched.

`initializeCommand` runs `touch -a ~/.proxy_env` on the host before the container is created,
because Docker turns a bind mount with a missing source into a directory at that path, which would
then shadow the file written there later. `.zprofile` guards with `-r` and `-f`, so a host with no
proxy, and the agent container (which has no such mount), are both silent no-ops.

Get `no_proxy` right or `kubectl` breaks: the kind API server is on the host's loopback and
in-cluster names only resolve inside the cluster, so both must bypass the proxy. The template's list
covers them.

### Building the devcontainer image

The devcontainer opens a pre-built image rather than building from `.devcontainer/Dockerfile` on
every create, so `kind`, `kubebuilder`, `kubectl`, `k9s`, `helm`, `starship`, the zsh plugins,
`gopls` and `dlv` come out of the layer cache. (`k9s` is the terminal console for the cluster — see
[dashboards.md](dashboards.md).) `hack/devcontainer.sh` is to that image what `hack/agent.sh` is to
the agent image, and shares its registry logic through `hack/lib/registry.sh`:

```bash
hack/devcontainer.sh image     # build locally, tagged with the commit sha and latest
hack/devcontainer.sh sync      # pull or build, then push so local and Docker Hub agree
```

Three things the script does that a hand-typed `docker build` did not:

- **It builds on the host network.** A localhost proxy is unreachable from the bridge network a
  build normally gets, and `github.com` / `proxy.golang.org` are not reachable directly here, so a
  bridge build hangs on the first download. The host's proxy variables are passed through as build
  arguments instead of being written into the repository, so a host with no proxy builds direct.
- **It checks the tag against `devcontainer.json`.** The `image` field is the one name VS Code and
  the build have to agree on. They once drifted apart, and the symptom was that rebuilding changed
  nothing: the build produced a tag nobody opened, and Rebuild Container reused the old image.
- **It stamps `org.opencontainers.image.revision`**, which is what lets `sync` ask the registry "is
  this commit already published?" without pulling the image.

A build only puts the image on disk. VS Code picks it up on the next **Dev Containers: Rebuild
Container** — reopening an existing container does not, and neither does it rerun the lifecycle
commands or install the extensions from `customizations.vscode`.

That last point is worth remembering as a debugging rule: **if `onCreateCommand` fails, the
extensions do not get installed.** Dev Containers runs the lifecycle commands and the extension
install in one `run-user-commands` step, and a non-zero exit aborts the rest of it with
`Skipping any further user-provided commands`. "My extensions are missing" is therefore usually a
`post-install.sh` failure, not an `extensions` list problem — read the **Dev Containers** output
channel from the top, not the bottom.

#### Why `.devcontainer/Dockerfile` builds Go tools in a separate stage

`gopls` and `dlv` are compiled in a `go-tools` stage and copied into the final image as binaries,
the same shape `agent.Dockerfile` uses. Running `go install` directly in the final stage works, but
it leaves root's artifacts in `GOPATH`: `/go/pkg/mod` is a cache mount and disappears, while
`/go/pkg/sumdb` is not, so root's copy of the checksum database stays in the layer as `root:0755`.
The `agent` user that `common-utils` creates afterwards then downloads modules fine and fails
_verifying_ them —

```text
go: open /go/pkg/sumdb/sum.golang.org/latest: permission denied
```

— which failed `onCreateCommand`, which cost the extensions. Building in a separate stage leaves
`/go` in the final image exactly as the `golang` base ships it.

## Boundary

The container is for reducing the agent's **marginal operational risk** compared with running it on
the WSL host. Model calls cost the same whether the agent runs on the host or in this container; the
container boundary is about file, process, Docker, and Kubernetes access.

Allowed in the development shell:

- run the agent process inside the Docker container;
- read and write this repository checkout;
- run local build, format, vet, and unit-test commands;
- use named Go and Neovim cache volumes created for this repo.

Allowed in the review shell:

- read this repository checkout;
- read explicitly mounted AI credentials;
- write only to container-local state and named cache volumes.

Not allowed unless you explicitly mount it:

- host filesystem access outside this checkout, with one exception below;
- model-provider credentials, which `--creds` mounts read-only only when you ask for it;
- host Docker socket access;
- kubeconfig access;
- cloud credentials, SSH keys, or personal secrets.

That means the agent can be given model credentials without also giving it your host filesystem,
Docker daemon, Kubernetes clusters, or cloud accounts. Within those limits the agent runs
unattended, with its own permission prompts turned off -- see
[Why the agents run unattended](#why-the-agents-run-unattended).

The exception is `$GOMODCACHE/cache/download`, mounted read-only. That directory is already laid out
as a module proxy, so `hack/agent.sh` puts it at the front of `GOPROXY` as `file:///hostproxy`
rather than making the container re-download the Kubernetes dependency tree into its own volume:
measured, that is 3.5 seconds instead of fourteen minutes, for three seconds of actual CPU. What it
exposes is public module source, read-only, and nothing else of the host. `GOSUMDB` is passed
through unchanged, so `go.sum` verification is exactly as it is on the host, and the network proxy
stays behind the file one as a fallback for anything missing. `--no-host-mods` turns it off and
restores the strict reading of the rule above.

## Build the agent image

From the repository root in WSL, prepare the usual development dependencies and images:

```bash
hack/agent.sh image
```

This downloads Go modules, builds the agent container image, fixes ownership on the named Go cache
volumes, fills the container Go cache, verifies that the source builds and tests inside the agent
container, and builds the product Docker image. Container Go commands inherit the host
`go env GOPROXY` value by default; override it with `GO_PROXY=...` if your network needs a different
module proxy. Runtime containers also pass through the host proxy environment variables and use host
networking by default, so a proxy bound to `127.0.0.1` on the WSL host still works for model calls;
set `AGENT_RUN_NETWORK=bridge` only if the session does not need a localhost proxy.

To pre-pull the large base images before the first build:

```bash
docker pull node:24-bookworm
docker pull golang:1.25-bookworm
```

`hack/agent.sh image` resolves those tags to immutable RepoDigests and passes the digest references
into `agent.Dockerfile`. That keeps readable defaults in the Dockerfile while preventing a moving
Docker Hub tag from silently changing the build. The digest is read from the image already on disk
when there is one, so a base that has been pulled once is not downloaded again on every build;
`AGENT_PULL=always` restores the refresh for when you want the newest `node:24-bookworm`. That
matters on a network where Docker Hub is slow or unreachable, since the pull otherwise happens
before the build has started and fails it.

To build only the agent image:

```bash
hack/agent.sh image
```

The build passes your UID/GID into the image so files created by the container belong to your WSL
user. It also passes the host's `go env GOPROXY` and `npm config get registry` through, so the build
uses whatever mirrors the host already trusts. `APT_MIRROR`, `GITHUB_MIRROR` and `NVIM_RELEASE_BASE`
work the same way: set them in the environment and `hack/agent.sh` forwards them as build args,
leaving the Dockerfile's defaults in place when they are unset.

On a network where the default registries are the slow part, name the mirrors explicitly:

```bash
NPM_REGISTRY=https://registry.npmmirror.com \
GITHUB_MIRROR=https://gh-proxy.com/https://github.com \
NVIM_RELEASE_BASE=https://mirror.nju.edu.cn/github-release/neovim/neovim/Nvim%200.12.6 \
hack/agent.sh image
```

These are build args, so the values are part of the cache key: changing one invalidates every layer
that used it, including the ~1GB npm install. Keep them stable between builds — putting them in the
host's `~/.zsh_env` is the usual place, since that file is private and already sourced by the shell.
`NVIM_RELEASE_BASE` exists because GitHub proxies cache small assets well and the 11MB Neovim
tarball badly; the NJU mirror carries it at LAN speed under a layout of its own.

To override the base images deliberately:

```bash
AGENT_NODE_IMAGE=node:24-bookworm AGENT_GO_IMAGE=golang:1.25-bookworm hack/agent.sh image
```

Expect around 3GB and a slow first build. `codex` and `claude-code` are thin npm wrappers around
per-platform native binaries pulled in as optional dependencies -- roughly 570MB unpacked between
them -- and `opencode` adds its own 230MB binary, `@opencode/cli-linux-*`. That is the bulk of both
the image and the build time. Keeping all four agent CLIs is deliberate: the image is a
general-purpose agent sandbox, not a Claude-specific one. The npm cache is a BuildKit cache mount,
so an interrupted build resumes rather than starting the download again.

## Sync the published image

`hack/agent.sh sync` keeps the local image and the Docker Hub copy aligned for the current commit.
The commit sha is the tag on both sides, so "does this commit have an image?" is a tag lookup rather
than an image comparison:

```bash
hack/agent.sh sync
```

- If the registry already has `<repo>:<sha>` and the local image is missing, sync pulls it.
- If neither side has the tag, sync builds from this checkout.
- If the local image exists and the registry lacks the tag, sync pushes it.
- `latest` moves only when git history says `HEAD` is at or ahead of the commit `latest` points at.
  Every image records that commit in the `org.opencontainers.image.revision` label, so sync reads it
  from the remote manifest without pulling three gigabytes. A `latest` newer than this checkout is
  left alone, and the local `latest` stays on this checkout's image so `shell` cannot run newer code
  against an older mount; a `latest` that cannot be compared is left alone too, and `--force-latest`
  overrides that guard.

The tag is the commit, so commit the build-input changes first: a dirty tree still syncs the image
for `HEAD`, not the one the working tree would build. An image published before the revision label
existed cannot be ordered against `HEAD`, so the first sync that crosses one asks for
`--force-latest` once. The repository is `chihayaanon0908/k8s-ai-platform-agent` by default;
override it with `AGENT_IMAGE_REMOTE_REPOSITORY=<account>/<repository>`. Sync uses Docker's
credential store, so run `docker login` once. `hack/agent.sh image` stays local and works offline;
`sync` is the only command that talks to a registry.

To fill only the Go module cache used by the container:

```bash
hack/agent.sh deps
```

For host-side development without the container, use:

```bash
go mod download
```

## Run for development

This mode gives the agent write access to this repository only. It is the normal mode for coding
changes after you decide the repo itself is an acceptable write boundary.

```bash
hack/agent.sh shell
```

After the Kubebuilder scaffold exists, inside the container:

```bash
make fmt
make vet
make test
```

Or without opening a shell at all, which is what a hook or a CI step wants — `--` passes the rest
through, and the tty is only allocated when there is one to attach to:

```bash
hack/agent.sh shell -- go test ./internal/...
```

The container gets its own `LOCALBIN` on the `k8s-ai-tools` named volume
(`/home/agent/.cache/k8s-ai-platform/bin`), passed as an environment variable by `hack/agent.sh`.
That is not a convenience. The generated Makefile installs its tools into `$(pwd)/bin` and symlinks
each one with an absolute path from `realpath`, and the checkout has a different absolute path on
the host than in the container (`/workspace/k8s-ai-platform`). Sharing `bin/` through the bind mount
therefore means whichever side ran `make` last leaves the other a dangling link. Separate
directories keep the generated Makefile unmodified, keep each environment's links valid, and stop a
host-built binary from being executed across the boundary. The first container run downloads the
tools into the volume; `docker volume rm k8s-ai-tools` forces a fresh download. `hack/format.sh`
reads the same `LOCALBIN`, so `goimports` remains the single Go formatter in both places.

The image also ships `goimports` on `PATH`, pinned to the same version as the Makefile's
`GOIMPORTS_VERSION`. That copy is only the fallback `hack/format.sh` uses when `LOCALBIN` is still
empty: without it, the first commit in a fresh container formats with plain `gofmt` and silently
leaves imports ungrouped, since the pre-commit hook does not stop for a missing formatter. The two
pins have to move together -- a container that groups imports differently from `make lint` is worse
than one with no formatter at all.

Setup output — the image build and the volume permissions — is captured rather than printed, since
it is noise on every run but the first. `-v` prints it, along with the exact `docker` commands. A
step that fails prints what it captured either way.

If the Go module cache is empty, `make vet` or `make test` may need dependencies that are not yet in
the cache. Fill the cache once with `hack/agent.sh deps`, then return to `hack/agent.sh shell` for
normal work.

Do not mount kubeconfigs, SSH keys, cloud credentials, or broad host directories into the cache-fill
container.

## Formatting and the git hook

The image pins `markdownlint-cli2` and `prettier` alongside the agent CLIs, so `hack/format.sh`
behaves identically inside the container and on the host. Both degrade to a warning when a formatter
is missing, which would otherwise mean the container silently accepted markdown the host rejects.

One part of Prettier's behaviour cannot come from the image: JSON key sorting is a plugin, and
Prettier resolves a plugin named in `.prettierrc` from the repository, not from the global install
that put `prettier` on `PATH`. So `package.json` pins both, `npm ci` restores them into a gitignored
`node_modules/`, and `hack/format.sh` stops with that instruction rather than letting Prettier fail
per file with a resolution error. The devcontainer runs `npm ci` in `post-install.sh`; the agent
container shares the same workspace, so the tree is already there. Both packages are pure
JavaScript, which is what makes one `node_modules/` valid for the host and both containers at once.

OpenCode is configured by the tracked `opencode.json` to run the same `hack/format.sh` after its
`write`, `edit` and `patch` tools change a file. The built-in `gofmt` and `prettier` formatters are
disabled there, so `hack/format.sh` remains the one formatter and handles Go imports, Markdown
linting and Prettier wrapping in a single pass.

`core.hooksPath` lives in `.git/config`, which is not tracked, so a fresh clone starts with the
pre-commit hook disarmed. The image's entrypoint sets it on every entry instead of relying on anyone
remembering `git config core.hooksPath hack/git-hooks`. It also adds the workspace to
`safe.directory`, because the mount is owned by the UID that built the image and git refuses a repo
owned by anyone else.

Arming is best effort. The review and build targets mount the checkout `:ro` and cannot write
`.git/config`; the entrypoint says so on stderr and carries on, since failing to arm a hook must not
fail a build.

To format or lint the tree with the container's pinned versions rather than whatever the host has:

```bash
hack/agent.sh fmt
hack/agent.sh lint
```

## Git identity for commits made inside the container

A container commit must still be attributable to a person, and the image cannot bake in an identity.
It is resolved on the host and carried in, in git's own order of precedence:

1. `AGENT_GIT_USER_NAME` / `AGENT_GIT_USER_EMAIL`, if set.
2. This repository's `.git/config` — a per-repository identity, the usual way to keep a personal
   address off work commits, or the reverse.
3. The host's `--global` config.
4. `AI Platform Agent <agent@k8s-ai-platform.local>`, a deliberately synthetic fallback. Seeing it
   in `git log` means the host had no identity configured at all.

`hack/agent.sh` resolves those four and passes the result as environment variables;
`hack/agent-entrypoint.sh` writes them into `.git/config` on entry, next to `core.hooksPath`.

Two consequences worth knowing:

- The entrypoint writes a key only when it does not already hold the wanted value, and it decides
  that by reading the value back rather than by testing `-w` on `.git/config`. Write permission on
  that file was never what decided whether the write succeeds: git replaces it through
  `.git/config.lock` and a rename, so what matters is the permission on `.git/`. A `-w` test
  therefore reported a disarmed `core.hooksPath` whenever the file merely belonged to another UID,
  including when the hook was armed and the write would have worked.
- `.git/config` is inside the bind-mounted checkout, so that write lands on the host and outlives
  the container. The container does not write `~/.gitconfig`, because it is not on one of the named
  volumes and would disappear with `--rm`.
- Commit signing is not carried in. A host configured with `commit.gpgsign = true` globally will
  produce unsigned commits inside the container.

## The shell inside the container

The container is meant to be lived in, not only shelled into, so it gets the shell the host runs:
zsh in vi mode with starship, zsh-autosuggestions, zsh-syntax-highlighting and the usual
git/docker/kubectl aliases. `.zshenv`, `.zprofile`, `.zshrc` and `.zsh_aliases` are vendored at
`hack/agent-home/`, baked into the image so it works standalone and bind-mounted read-only over that
copy by the run targets so a change on the host takes effect in the next shell without a rebuild.
The interactive shell is a login shell (`zsh -l`), so `.zprofile` sets `EDITOR` and `PATH` the same
way a host terminal does.

The two plugins are not vendored; `agent.Dockerfile` downloads them at the commits the host
installer checked out (`ZSH_AUTOSUGGESTIONS_COMMIT`, `ZSH_SYNTAX_HIGHLIGHTING_COMMIT`) into
`~/.local/share/zsh/plugins`, the same path the host uses, so `.zshrc` needs no container branch.
Bash is still in the image for scripts; it just has no interactive configuration any more.

Neovim is `$EDITOR` and runs the same LazyVim configuration as the host, vendored at
`hack/agent-home/.config/nvim`: `git commit` without `-m`, `git rebase -i` and an agent shelling out
open the editor the owner actually uses. The plugin tree is not baked into the image. LazyVim
installs it on the first start inside the container, into `~/.local/share/nvim`, which
`hack/agent.sh` backs with the `k8s-ai-nvim` named volume — so the download happens once per volume
rather than once per container. `docker volume rm k8s-ai-nvim` forces a clean reinstall. Seeding
that volume from the host's `~/.local/share/nvim` is the way to skip the first download entirely.

Two spots in that configuration are container-guarded rather than shared verbatim with the host,
because the host is WSL and the container is not: the `clip.exe` clipboard provider is only set when
`clip.exe` is on `PATH`, and the system-theme probe skips a command it cannot execute. Both guards
are no-ops on the host.

The container still does not carry the project's IDE policy: there is no language-server install and
no per-project editor setup beyond what LazyVim does on its own. The plugin volume exists so the
same configuration is usable for commit messages and quick edits, not so the container becomes the
place editing happens.

OpenCode's terminal settings are tracked alongside the shell configuration in
`hack/agent-home/.config/opencode/cli.json`. Scrolling and message navigation use vim-style keys:
`ctrl+u` / `ctrl+d` for half a page, `ctrl+k` / `ctrl+j` for one line, `ctrl+alt+k` / `ctrl+alt+j`
for previous / next message, and `ctrl+alt+g` / `ctrl+alt+shift+g` for first / last. The modifiers
are deliberate, because bare `j`/`k`/`g` are text in the prompt rather than navigation.
`which-key.toggle` moves to `ctrl+alt+shift+w` so it does not swallow `ctrl+alt+k`; `ctrl+k` and
`ctrl+j` are freed by disabling the input's delete-to-line-end binding and dropping `ctrl+j` from
`input.newline`. `alt+e` opens the current prompt in `$EDITOR`, after disabling the input's
move-to-visual-line-end binding.

`~/.zsh_env` is not vendored and not mounted: it holds real API tokens on the host, and the
`.zprofile` guard skips it when absent. Pass `--mount` if a session genuinely needs one of those
tokens.

## Run for read-only review

This mode is best when you want the agent to inspect or explain the project but not modify the
checkout.

```bash
hack/agent.sh review
```

## Running an AI CLI inside the container

If the AI agent CLI itself must call a model provider, mount only the credential files it needs,
read-only. The agent still runs inside the container; only the model API call leaves the container.

Mount credential files read-only and keep the rest of the boundary unchanged:

- mount only this repository;
- mount only the AI provider token needed by the agent;
- do not mount `~/.kube`, cloud config directories, SSH keys, or `/var/run/docker.sock`;
- prefer a read-only repo mount for review-only sessions.

Claude Code is the exception, and the easier case. The image sets `CLAUDE_CONFIG_DIR` to `~/.claude`
and `hack/agent.sh` backs that directory with a named volume, so running `claude` inside the
container and logging in once there keeps the credential in the volume across `docker run --rm`. The
host credential is never mounted, and revoking the container's access is
`docker volume rm k8s-ai-claude` rather than a hunt for what was bind-mounted where.

OpenCode gets the same treatment through `k8s-ai-opencode`, mounted at its XDG data directory
`~/.local/share/opencode`: sessions and connected accounts survive `--rm`, and apart from
`--creds opencode`, no host credential is mounted. Its terminal settings, including the
message-navigation keybindings, are tracked in `hack/agent-home/.config/opencode/cli.json` and
mounted read-only over the copy in the image. Clear the volume with
`docker volume rm k8s-ai-opencode`.

`hack/agent.sh` assigns each container a free port from `AGENT_OPENCODE_PORT_RANGE` (default
`49376-49400`) and passes it to the entrypoint, which writes `~/.config/opencode/service.json`
before OpenCode starts. OpenCode's default is 49374, which is also the host's default, and
`hack/agent.sh` runs the container with host networking; without the per-container port the second
service fails to bind. Widen the range if you expect to run more containers than it has ports.

OpenCode's selected model and variant live in `~/.local/state/opencode/model.json`, not in
`cli.json`. The entrypoint copies the host's file into the container's writable state directory, so
a new session starts with the host's recent model and variant. A host global
`~/.config/opencode/opencode.json(c)` is also mounted read-only when it exists. The container's
state directory is ephemeral, so changing `/models` inside a container does not rewrite the host's
selection.

The image also bakes in this maintainer's usual Pi extension packages under the container's Pi home:
`@inobit/pi-reader` and `pi-background-tasks` with its packaged background-task and
Anthropic-attribution extensions. This makes the extensions available in a fresh container, but does
not mount or copy any provider credentials.

Pi's model defaults are not baked into the image. They live in the tracked project settings,
`.pi/settings.json`, which the workspace bind mount already presents to the container: the host and
the container read the same file, so provider, model and thinking level are set once rather than
pinned twice. Project settings load only after project trust is granted, so a fresh container asks
once and keeps the answer in its ephemeral Pi home.

Pi and Codex session history is host-backed by default, without mounting credentials:

```text
~/.pi/agent/sessions       -> /home/agent/.pi/agent/sessions
~/.codex/sessions          -> /home/agent/.codex/sessions
```

Override those host paths with `PI_SESSIONS_DIR` and `CODEX_SESSIONS_DIR` if needed. The script
creates the directories before `docker run` so Docker does not create them as root. These mounts are
for conversation/session continuity; auth remains opt-in.

Pi stores its credential in `~/.pi/agent/auth.json` and Codex in `~/.codex/auth.json`
(`$CODEX_HOME/auth.json` when that is set). `--creds` mounts whichever of them this host has,
read-only, and nothing at all without it. The Pi credential is mounted at `/run/agent-creds` and
copied into the container's writable Pi home by the entrypoint, because Pi may rewrite `auth.json`
while refreshing OAuth-backed providers; the host file is still only read:

```bash
hack/agent.sh shell --creds
```

OpenCode has no equivalent file: saved API keys and OAuth tokens live in its SQLite database
(`~/.local/share/opencode/opencode.db` on the host). `--creds opencode` therefore runs
`opencode auth export --standalone` on the host, writes the JSON to a temporary file, mounts it at
`/run/agent-creds/opencode-auth.json`, and lets the entrypoint import it with `opencode auth import`
into the container's own database. The `k8s-ai-opencode` volume then keeps those imported accounts
across `--rm`. Environment-only connections such as `DEEPSEEK_API_KEY` are not saved accounts, so
they are not exported; set them in the container explicitly, for example with `--mount` of an env
file, or connect inside the container.

It prints what it handed over, because giving a container a host secret is the one thing this script
does that belongs in the scrollback whether or not anyone asked for `-v`:

```text
agent.sh: mounting pi credential, read-only: /home/you/.pi/agent/auth.json
agent.sh: mounting codex credential, read-only: /home/you/.codex/auth.json
agent.sh: mounting opencode credential, read-only: host auth export
```

Name the ones you want to narrow it further, and the same flag works on `review`:

```bash
hack/agent.sh review --creds codex
hack/agent.sh shell --creds opencode
```

The two halves behave differently on purpose. A credential named explicitly must exist, because
`--creds codex` quietly opening a shell without it is the failure worth an error message; one that
is simply absent under a bare `--creds` is skipped. The same applies to OpenCode: `--creds opencode`
fails if the host has no saved accounts, while a bare `--creds` skips it. The host credentials are
read-only without exception -- the container has no business rewriting a token the host owns, and a
Pi refresh written inside it dies with `--rm` anyway. The temporary OpenCode export is removed when
`hack/agent.sh` exits.

`--mount` remains for anything `--creds` does not know about:

```bash
hack/agent.sh shell \
  --mount type=bind,src=$HOME/.config/x/token,dst=/home/agent/.config/x/token,readonly
```

Then run the agent inside that shell, for example:

```bash
claude
# or
pi
# or
codex
# or
opencode
```

## Why the agents run unattended

`claude`, `codex` and `opencode` in this image are wrapper scripts in `hack/agent-bin/`, copied to
`/usr/local/libexec/agent-bin` and put ahead of npm's `/usr/local/bin` on `PATH`. They exec the real
binary with its interactive gate removed -- `--dangerously-skip-permissions` for Claude Code,
`--dangerously-bypass-approvals-and-sandbox` for Codex, `--auto` for OpenCode -- so an agent works
to completion instead of stopping on every write and every shell command.

That is the point of the container. The boundary is the one described under [Boundary](#boundary):
`--cap-drop ALL`, `--security-opt no-new-privileges`, no Docker socket, no kubeconfig, no host home,
and the only writable host path is this checkout. Everything the agent could reach by answering a
prompt with "yes", it can reach anyway; the prompt is a question whose answer is already fixed, and
the honest place to have refused was `docker run`. Withdrawing the trust is `docker rm` and
`docker volume rm k8s-ai-claude`, not an audit of what got approved at 02:00.

The consequence, stated plainly: **an agent in this container can destroy the checkout.** It is a
bind mount of your working tree, not a copy. Commit or push before a long unattended run, and use
`hack/agent.sh review` -- where the checkout is mounted `:ro` -- for anything that only needs to
read. `pi` is left alone; it has no equivalent global flag here.

It is a launch flag rather than configuration, on both sides, and deliberately:

- Claude Code's `permissions.defaultMode` would live in `settings.json` inside the `k8s-ai-claude`
  volume, which outlives any one container and travels with the credential.
- Codex's `approval_policy` and `sandbox_mode` would live in `~/.codex/config.toml`, the directory
  sessions bind-mount `auth.json` into from the host.
- OpenCode's `session.permissions: autoaccept` would live in `cli.json`, which is shared with the
  host and with the tracked container config.

Either would quietly widen something outside the container's lifetime. A flag is scoped to the
process that was launched.

To get the prompts back for one session, set `AGENT_BYPASS=0`:

```bash
AGENT_BYPASS=0 claude
AGENT_BYPASS=0 opencode
```

The wrappers also stand aside on their own when the invocation already decides the question --
`claude --permission-mode plan`, `codex --sandbox read-only`, `opencode --auto` -- and when the
subcommand does not take the flag at all, such as `claude mcp list` or `codex login`. They are
root-owned and `0755`, so the agent user cannot edit its own launcher.

The Claude Code wrapper carries one unrelated fix. Claude Code positions the terminal's own cursor
rather than drawing one, and sets it to a block at startup, so the beam `.zshrc` asks for in insert
mode is gone by the time the input is drawn and the shape stops matching the shell outside the
session. It sets the shape once rather than holding it, so the wrapper writes `\e[5 q` to `/dev/tty`
from a background job about one, two and four seconds in; a write that lands before the first frame
is simply overwritten again. Only the paths that start the interactive UI arm it -- a subcommand
exits before the last write is due, and the escape would reach the shell prompt instead and turn a
vi-normal-mode block back into a beam.

Model usage is controlled by the credentials/account you mount. The repo/container boundary only
prevents the agent's shell commands from reaching host secrets or cloud credentials you did not
mount.

## Kubernetes and Docker work

For this repository, live-cluster work needs Docker, kind, and kubectl access. Giving those to a
container usually requires mounting the host Docker socket, and that is effectively host-level
trust.

Use one of these safer patterns instead:

1. Let the agent edit code and run unit tests in the container, then you run cluster commands from
   WSL after reviewing the diff.
2. Create a separate disposable VM/WSL distro for platform experiments, and run the agent there.
3. Only if you accept host-level trust, start a separate high-trust session with the Docker socket
   and a throwaway local kind cluster. Never include real kubeconfigs or cloud credentials.

For the normal rule you gave — operations scoped to this repo with no extra host risk — use
`agent-shell` or `agent-review` without Docker socket or kubeconfig mounts.
