# AI agent development container

Use this container when you want an AI coding agent to work on this repository while keeping its
normal commands scoped to the repository checkout. The setup is included in the repository by
default, but it is not enabled automatically: nothing uses it until you run `hack/agent.sh`.

This is different from the product image in the root `Dockerfile`, and it is also different from
Kubebuilder's generated `.devcontainer/`. The generated devcontainer is the ordinary human
development environment. `agent.Dockerfile` is an agent sandbox with Go, Make, Git, small CLI tools
for editing and testing, and the `claude`, `pi` and `codex` CLIs. It does not bake any
model-provider credentials into the image.

Quick start from the repository root:

```bash
hack/agent.sh image      # build the agent image
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

The agent container below is a separate, stronger sandbox for running AI CLIs. Keep both available:
the devcontainer is the ordinary development environment, while `agent.Dockerfile` is opt-in.

## Boundary

The container is for reducing the agent's **marginal operational risk** compared with running it on
the WSL host. Model calls cost the same whether the agent runs on the host or in this container; the
container boundary is about file, process, Docker, and Kubernetes access.

Allowed in the development shell:

- run the agent process inside the Docker container;
- read and write this repository checkout;
- run local build, format, vet, and unit-test commands;
- use named Go cache volumes created for this repo.

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

`hack/agent.sh image` pulls those tags too, then resolves them to immutable RepoDigests and passes
the digest references into `agent.Dockerfile`. That keeps readable defaults in the Dockerfile while
preventing a moving Docker Hub tag from silently changing the build.

To build only the agent image:

```bash
hack/agent.sh image
```

The build passes your UID/GID into the image so files created by the container belong to your WSL
user. It also passes the host's `go env GOPROXY` and `npm config get registry` through, so the build
uses whatever mirrors the host already trusts; `APT_MIRROR` and `GITHUB_MIRROR` are build args on
the same footing.

To override the base images deliberately:

```bash
AGENT_NODE_IMAGE=node:24-bookworm AGENT_GO_IMAGE=golang:1.25-bookworm hack/agent.sh image
```

Expect around 3GB and a slow first build. `codex` and `claude-code` are thin npm wrappers around
per-platform native binaries pulled in as optional dependencies -- roughly 570MB unpacked between
them -- and that is the bulk of both the image and the build time. Keeping all three agent CLIs is
deliberate: the image is a general-purpose agent sandbox, not a Claude-specific one. The npm cache
is a BuildKit cache mount, so an interrupted build resumes rather than starting the download again.

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

## The shell inside the container

The container is meant to be lived in, not only shelled into, so it gets the shell the host runs:
bash with ble.sh, starship and the usual git/docker/kubectl aliases, vendored at `hack/agent-home/`,
baked into the image so it works standalone and bind-mounted read-only over that copy by the run
targets so a change on the host takes effect in the next shell without a rebuild.

It does not get an IDE. Neovim is installed as `$EDITOR` — `git commit` without `-m` and
`git rebase -i` need one — but without the LazyVim configuration or any language server. Editing
usually happens on the host against the same inodes through the bind mount; the container editor is
there for commit messages and emergency terminal edits, not as the project's IDE policy.

`~/.bash_env` is not vendored and not mounted: it holds real API tokens on the host, and the
`.bashrc` guard skips it when absent. Pass `--mount` if a session genuinely needs one of those
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

The image also bakes in this maintainer's usual Pi extension packages under the container's Pi home:
`@inobit/pi-reader` and `pi-background-tasks` with its packaged background-task and
Anthropic-attribution extensions. This makes the extensions available in a fresh container, but does
not mount or copy any provider credentials.

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

It prints what it handed over, because giving a container a host secret is the one thing this script
does that belongs in the scrollback whether or not anyone asked for `-v`:

```text
agent.sh: mounting pi credential, read-only: /home/you/.pi/agent/auth.json
agent.sh: mounting codex credential, read-only: /home/you/.codex/auth.json
```

Name the ones you want to narrow it further, and the same flag works on `review`:

```bash
hack/agent.sh review --creds codex
```

The two halves behave differently on purpose. A credential named explicitly must exist, because
`--creds codex` quietly opening a shell without it is the failure worth an error message; one that
is simply absent under a bare `--creds` is skipped. The host credentials are read-only without
exception -- the container has no business rewriting a token the host owns, and a Pi refresh written
inside it dies with `--rm` anyway.

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
```

## Why the agents run unattended

`claude` and `codex` in this image are wrapper scripts in `hack/agent-bin/`, copied to
`/usr/local/libexec/agent-bin` and put ahead of npm's `/usr/local/bin` on `PATH`. They exec the real
binary with its interactive gate removed -- `--dangerously-skip-permissions` for Claude Code,
`--dangerously-bypass-approvals-and-sandbox` for Codex -- so an agent works to completion instead of
stopping on every write and every shell command.

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

Either would quietly widen something outside the container's lifetime. A flag is scoped to the
process that was launched.

To get the prompts back for one session, set `AGENT_BYPASS=0`:

```bash
AGENT_BYPASS=0 claude
```

The wrappers also stand aside on their own when the invocation already decides the question --
`claude --permission-mode plan`, `codex --sandbox read-only` -- and when the subcommand does not
take the flag at all, such as `claude mcp list` or `codex login`. They are root-owned and `0755`, so
the agent user cannot edit its own launcher.

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
